#![allow(dead_code)]

use auth_service::auth::jwt::JwtManager;
use auth_service::config::Config;
use auth_service::db::queries;
use auth_service::routes::create_router;
use auth_service::AppState;
use axum::body::Body;
use axum::http::{Request, StatusCode};
use axum::Router;
use base64::Engine;
use http_body_util::BodyExt;
use tower::ServiceExt;

// ─── TestResponse ────────────────────────────────────────────────────────────

pub struct TestResponse {
    pub status: StatusCode,
    body_bytes: Vec<u8>,
}

impl TestResponse {
    pub fn text(&self) -> String {
        String::from_utf8_lossy(&self.body_bytes).to_string()
    }

    pub fn json<T: serde::de::DeserializeOwned>(&self) -> T {
        serde_json::from_slice(&self.body_bytes).unwrap_or_else(|e| {
            panic!(
                "Failed to deserialize response as {}: {e}\nBody: {}",
                std::any::type_name::<T>(),
                self.text()
            )
        })
    }

    pub fn assert_status(&self, expected: StatusCode) {
        assert_eq!(
            self.status,
            expected,
            "Expected status {expected}, got {}. Body: {}",
            self.status,
            self.text()
        );
    }
}

// ─── CreatedApp ──────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
pub struct CreatedApp {
    pub id: String,
    pub client_id: String,
    pub client_secret: String,
}

// ─── TestApp ─────────────────────────────────────────────────────────────────

pub struct TestApp {
    router: Router,
    pub state: AppState,
    /// Bearer token for an admin user (bootstrapped via seed).
    pub admin_token: String,
}

impl TestApp {
    pub async fn new() -> Self {
        let database_url = std::env::var("TEST_DATABASE_URL")
            .expect("TEST_DATABASE_URL must be set for integration tests");

        let config = Config {
            database_url: database_url.clone(),
            jwt_private_key_path: "keys/private.pem".to_string(),
            jwt_public_key_path: "keys/public.pem".to_string(),
            jwt_issuer: "auth-service-test".to_string(),
            jwt_access_token_expiry_secs: 3600,
            jwt_refresh_token_expiry_days: 30,
            server_host: "127.0.0.1".to_string(),
            server_port: 0,
            cors_allowed_origins: "*".to_string(),
        };

        let db = auth_service::db::pool::connect(&config.database_url)
            .await
            .expect("Failed to connect to MSSQL test database");

        // Run migrations
        auth_service::db::migration::run(&db)
            .await
            .expect("Failed to run migrations");

        // Truncate all tables (in FK dependency order)
        {
            let mut conn = db
                .get()
                .await
                .expect("Failed to get connection for truncation");
            // Delete in reverse FK order
            conn.execute("DELETE FROM refresh_tokens", &[]).await.ok();
            conn.execute("DELETE FROM authorization_codes", &[])
                .await
                .ok();
            conn.execute("DELETE FROM accounts", &[]).await.ok();
            conn.execute("DELETE FROM app_providers", &[]).await.ok();
            conn.execute("DELETE FROM users", &[]).await.ok();
            conn.execute("DELETE FROM applications", &[]).await.ok();
        }

        let jwt = JwtManager::new(&config).expect("Failed to init JwtManager");

        // Bootstrap admin app + admin user via seed
        auth_service::seed::bootstrap(&db, "test-admin@internal", Some("TestAdmin1!"))
            .await
            .expect("Failed to bootstrap admin");

        // Get admin user to issue a token
        let admin_user = queries::users::find_by_email(&db, "test-admin@internal")
            .await
            .unwrap()
            .expect("Admin user not found");

        let admin_token = jwt
            .issue_access_token(
                &admin_user.id,
                "test-internal",
                vec!["admin".to_string()],
                "admin",
            )
            .expect("Failed to issue admin token");

        let state = AppState { db, jwt, config };
        let router = create_router(state.clone());

        Self {
            router,
            state,
            admin_token,
        }
    }

    pub async fn request(&self, req: Request<Body>) -> TestResponse {
        let resp = self
            .router
            .clone()
            .oneshot(req)
            .await
            .expect("oneshot failed");

        let status = resp.status();
        let body_bytes = resp
            .into_body()
            .collect()
            .await
            .expect("failed to read body")
            .to_bytes()
            .to_vec();

        TestResponse { status, body_bytes }
    }

    // ── Admin helpers ────────────────────────────────────────────────────

    pub async fn admin_create_app(&self, name: &str, uris: &[&str], scopes: &[&str]) -> CreatedApp {
        let body = serde_json::json!({
            "name": name,
            "redirect_uris": uris,
            "allowed_scopes": scopes,
        });

        let req = Request::builder()
            .method("POST")
            .uri("/admin/applications")
            .header("Content-Type", "application/json")
            .header("Authorization", format!("Bearer {}", self.admin_token))
            .body(Body::from(serde_json::to_vec(&body).unwrap()))
            .unwrap();

        let resp = self.request(req).await;
        resp.assert_status(StatusCode::OK);
        let json: serde_json::Value = resp.json();

        CreatedApp {
            id: json["id"].as_str().unwrap().to_string(),
            client_id: json["client_id"].as_str().unwrap().to_string(),
            client_secret: json["client_secret"].as_str().unwrap().to_string(),
        }
    }

    // ── Auth helpers ─────────────────────────────────────────────────────

    pub async fn register_user(
        &self,
        client_id: &str,
        email: &str,
        password: &str,
    ) -> TestResponse {
        let body = serde_json::json!({
            "email": email,
            "password": password,
        });

        let req = Request::builder()
            .method("POST")
            .uri("/api/auth/register")
            .header("Content-Type", "application/json")
            .header("X-Client-Id", client_id)
            .body(Body::from(serde_json::to_vec(&body).unwrap()))
            .unwrap();

        self.request(req).await
    }

    pub async fn login_user(&self, client_id: &str, email: &str, password: &str) -> TestResponse {
        let body = serde_json::json!({
            "email": email,
            "password": password,
        });

        let req = Request::builder()
            .method("POST")
            .uri("/api/auth/login")
            .header("Content-Type", "application/json")
            .header("X-Client-Id", client_id)
            .body(Body::from(serde_json::to_vec(&body).unwrap()))
            .unwrap();

        self.request(req).await
    }

    pub fn basic_auth_header(client_id: &str, secret: &str) -> String {
        let raw = format!("{client_id}:{secret}");
        let encoded = base64::engine::general_purpose::STANDARD.encode(raw);
        format!("Basic {encoded}")
    }
}
