#![allow(dead_code)]

use std::sync::Arc;

use auth_service::auth::jwt::JwtManager;
use auth_service::config::Config;
use auth_service::db::azure_tables::AzureTableRepository;
use auth_service::db::repository::Repository;
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

const AZURITE_CONNECTION_STRING: &str = "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;TableEndpoint=http://127.0.0.1:10002/devstoreaccount1";

pub struct TestApp {
    router: Router,
    pub state: AppState,
    /// Bearer token for an admin user (bootstrapped via seed).
    pub admin_token: String,
}

impl TestApp {
    pub async fn new() -> Self {
        let conn_str = std::env::var("TEST_STORAGE_CONNECTION_STRING")
            .unwrap_or_else(|_| AZURITE_CONNECTION_STRING.to_string());

        let config = Config {
            azure_storage_connection_string: conn_str.clone(),
            jwt_private_key_path: "keys/private.pem".to_string(),
            jwt_public_key_path: "keys/public.pem".to_string(),
            jwt_issuer: "auth-service-test".to_string(),
            jwt_access_token_expiry_secs: 3600,
            jwt_refresh_token_expiry_days: 30,
            server_host: "127.0.0.1".to_string(),
            server_port: 0,
            cors_allowed_origins: "*".to_string(),
        };

        let table_repo =
            AzureTableRepository::new(&conn_str).expect("Failed to create AzureTableRepository");

        // Clear and recreate all tables for test isolation
        table_repo
            .clear_all_tables()
            .await
            .expect("Failed to clear tables");

        let jwt = JwtManager::new(&config).expect("Failed to init JwtManager");

        let repo: Arc<dyn Repository> = Arc::new(table_repo);

        // Bootstrap admin app + admin user via seed
        auth_service::seed::bootstrap(repo.as_ref(), "test-admin@internal", Some("TestAdmin1!"))
            .await
            .expect("Failed to bootstrap admin");

        // Get admin user to issue a token
        let admin_user = repo
            .users()
            .find_by_email("test-admin@internal")
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

        let state = AppState { repo, jwt, config };
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

    // ── Invite code helpers ──────────────────────────────────────────────

    /// Create an invite code via the admin API. Returns the code string.
    pub async fn admin_create_invite_code(&self) -> String {
        let req = Request::builder()
            .method("POST")
            .uri("/admin/invite-codes")
            .header("Authorization", format!("Bearer {}", self.admin_token))
            .body(Body::empty())
            .unwrap();

        let resp = self.request(req).await;
        resp.assert_status(StatusCode::OK);
        let json: serde_json::Value = resp.json();
        json["code"].as_str().unwrap().to_string()
    }

    /// Register a user supplying an invite code.
    pub async fn register_user_with_invite(
        &self,
        client_id: &str,
        email: &str,
        password: &str,
        invite_code: &str,
    ) -> TestResponse {
        let body = serde_json::json!({
            "email": email,
            "password": password,
            "invite_code": invite_code,
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
}
