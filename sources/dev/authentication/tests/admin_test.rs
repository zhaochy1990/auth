mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::TestApp;
use serial_test::serial;

// ─── Create Application ──────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn create_application_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app(
            "Test App",
            &["https://example.com/cb"],
            &["openid", "profile"],
        )
        .await;

    assert!(!created.id.is_empty());
    assert!(created.client_id.starts_with("app_"));
    assert_eq!(created.client_secret.len(), 64); // 32 bytes hex
}

#[serial]
#[tokio::test]
async fn create_application_missing_auth() {
    let app = TestApp::new().await;

    let body = serde_json::json!({
        "name": "App",
        "redirect_uris": ["https://example.com/cb"],
        "allowed_scopes": ["openid"],
    });

    let req = Request::builder()
        .method("POST")
        .uri("/admin/applications")
        .header("Content-Type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

#[serial]
#[tokio::test]
async fn create_application_invalid_token() {
    let app = TestApp::new().await;

    let body = serde_json::json!({
        "name": "App",
        "redirect_uris": ["https://example.com/cb"],
        "allowed_scopes": ["openid"],
    });

    let req = Request::builder()
        .method("POST")
        .uri("/admin/applications")
        .header("Content-Type", "application/json")
        .header("Authorization", "Bearer invalid.jwt.token")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

// ─── List Applications ───────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn list_applications_empty() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("GET")
        .uri("/admin/applications")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let list: Vec<serde_json::Value> = resp.json();
    // The seed creates an "Admin Dashboard" app, so it's not truly empty
    assert_eq!(list.len(), 1);
}

#[serial]
#[tokio::test]
async fn list_applications_after_create() {
    let app = TestApp::new().await;
    app.admin_create_app("App One", &["https://a.com/cb"], &["openid"])
        .await;
    app.admin_create_app("App Two", &["https://b.com/cb"], &["profile"])
        .await;

    let req = Request::builder()
        .method("GET")
        .uri("/admin/applications")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let list: Vec<serde_json::Value> = resp.json();
    assert_eq!(list.len(), 3); // 1 seed + 2 created
}

// ─── Update Application ─────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn update_application_name() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("Old Name", &["https://a.com/cb"], &["openid"])
        .await;

    let body = serde_json::json!({"name": "New Name"});
    let req = Request::builder()
        .method("PATCH")
        .uri(format!("/admin/applications/{}", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["name"], "New Name");
}

#[serial]
#[tokio::test]
async fn update_application_deactivate() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let body = serde_json::json!({"is_active": false});
    let req = Request::builder()
        .method("PATCH")
        .uri(format!("/admin/applications/{}", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["is_active"], false);
}

#[serial]
#[tokio::test]
async fn update_application_not_found() {
    let app = TestApp::new().await;

    let body = serde_json::json!({"name": "X"});
    let req = Request::builder()
        .method("PATCH")
        .uri("/admin/applications/nonexistent-id")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

// ─── Add Provider ────────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn add_provider_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let body = serde_json::json!({
        "provider_id": "wechat",
        "config": {"appid": "wx123", "secret": "sec456"}
    });

    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["provider_id"], "wechat");
    assert_eq!(json["is_active"], true);
}

#[serial]
#[tokio::test]
async fn add_provider_duplicate() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let body = serde_json::json!({
        "provider_id": "wechat",
        "config": {"appid": "wx123", "secret": "sec456"}
    });

    // First add
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    // Duplicate
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn add_provider_app_not_found() {
    let app = TestApp::new().await;

    let body = serde_json::json!({
        "provider_id": "wechat",
        "config": {"appid": "wx123", "secret": "sec456"}
    });

    let req = Request::builder()
        .method("POST")
        .uri("/admin/applications/nonexistent-id/providers")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

// ─── Remove Provider ─────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn remove_provider_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    // Add a provider first
    let body = serde_json::json!({
        "provider_id": "wechat",
        "config": {"appid": "wx123", "secret": "sec456"}
    });
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Now remove it
    let req = Request::builder()
        .method("DELETE")
        .uri(format!(
            "/admin/applications/{}/providers/wechat",
            created.id
        ))
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["status"], "deleted");
}

#[serial]
#[tokio::test]
async fn remove_provider_not_configured() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let req = Request::builder()
        .method("DELETE")
        .uri(format!(
            "/admin/applications/{}/providers/wechat",
            created.id
        ))
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

// ─── Rotate Secret ───────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn rotate_secret_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/rotate-secret", created.id))
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["client_id"], created.client_id);
    let new_secret = json["client_secret"].as_str().unwrap();
    assert_ne!(new_secret, created.client_secret);
    assert_eq!(new_secret.len(), 64);
}

#[serial]
#[tokio::test]
async fn rotate_secret_app_not_found() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("POST")
        .uri("/admin/applications/nonexistent-id/rotate-secret")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

// ─── Reset User Password ─────────────────────────────────────────────────────

/// Helper: register a fresh user via the auth API and return the user_id.
async fn register_and_get_user_id(
    app: &TestApp,
    client_id: &str,
    email: &str,
    password: &str,
) -> String {
    let resp = app.register_user(client_id, email, password).await;
    resp.assert_status(StatusCode::CREATED);
    let json: serde_json::Value = resp.json();
    json["user_id"].as_str().unwrap().to_string()
}

#[serial]
#[tokio::test]
async fn reset_password_success_changes_credential() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    // Add password provider so login works
    let body = serde_json::json!({"provider_id": "password", "config": {}});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    let user_id =
        register_and_get_user_id(&app, &created.client_id, "reset@test.com", "OldPass1!").await;

    // Reset password
    let body = serde_json::json!({"password": "NewPass1!"});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/users/{user_id}/reset-password"))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["user_id"], user_id);
    assert_eq!(json["revoked_sessions"], true);

    // Old password fails
    let resp = app
        .login_user(&created.client_id, "reset@test.com", "OldPass1!")
        .await;
    resp.assert_status(StatusCode::UNAUTHORIZED);

    // New password works
    let resp = app
        .login_user(&created.client_id, "reset@test.com", "NewPass1!")
        .await;
    resp.assert_status(StatusCode::OK);
}

#[serial]
#[tokio::test]
async fn reset_password_revokes_refresh_tokens_by_default() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let body = serde_json::json!({"provider_id": "password", "config": {}});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    let user_id =
        register_and_get_user_id(&app, &created.client_id, "rt@test.com", "OldPass1!").await;

    // Login to get a refresh token
    let resp = app
        .login_user(&created.client_id, "rt@test.com", "OldPass1!")
        .await;
    resp.assert_status(StatusCode::OK);
    let login_json: serde_json::Value = resp.json();
    let refresh_token = login_json["refresh_token"].as_str().unwrap().to_string();

    // Reset password (default revoke_sessions = true)
    let body = serde_json::json!({"password": "NewPass1!"});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/users/{user_id}/reset-password"))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Refresh token should be invalid now
    let body = serde_json::json!({"refresh_token": refresh_token});
    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/refresh")
        .header("Content-Type", "application/json")
        .header("X-Client-Id", &created.client_id)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    assert_ne!(
        resp.status,
        StatusCode::OK,
        "refresh token should have been revoked"
    );
}

#[serial]
#[tokio::test]
async fn reset_password_keeps_refresh_tokens_when_revoke_false() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let body = serde_json::json!({"provider_id": "password", "config": {}});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    let user_id =
        register_and_get_user_id(&app, &created.client_id, "keep@test.com", "OldPass1!").await;

    // Login to get a refresh token
    let resp = app
        .login_user(&created.client_id, "keep@test.com", "OldPass1!")
        .await;
    resp.assert_status(StatusCode::OK);
    let login_json: serde_json::Value = resp.json();
    let refresh_token = login_json["refresh_token"].as_str().unwrap().to_string();

    // Reset with revoke_sessions = false
    let body = serde_json::json!({"password": "NewPass1!", "revoke_sessions": false});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/users/{user_id}/reset-password"))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["revoked_sessions"], false);

    // Refresh token still works
    let body = serde_json::json!({"refresh_token": refresh_token});
    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/refresh")
        .header("Content-Type", "application/json")
        .header("X-Client-Id", &created.client_id)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
}

#[serial]
#[tokio::test]
async fn reset_password_weak_password() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let body = serde_json::json!({"provider_id": "password", "config": {}});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    let user_id =
        register_and_get_user_id(&app, &created.client_id, "weak@test.com", "OldPass1!").await;

    let body = serde_json::json!({"password": "weak"});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/users/{user_id}/reset-password"))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn reset_password_user_not_found() {
    let app = TestApp::new().await;

    let body = serde_json::json!({"password": "NewPass1!"});
    let req = Request::builder()
        .method("POST")
        .uri("/admin/users/nonexistent-id/reset-password")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

#[serial]
#[tokio::test]
async fn reset_password_user_without_password_account() {
    // The seed admin uses password provider, so create a user via direct
    // repository insert with no password account.
    let app = TestApp::new().await;

    // Insert a user that has no password account
    let user = auth_service::db::models::User {
        id: uuid::Uuid::new_v4().to_string(),
        email: Some("no-password@test.com".to_string()),
        name: None,
        avatar_url: None,
        email_verified: false,
        role: "user".to_string(),
        is_active: true,
        note: None,
        created_at: chrono::Utc::now().naive_utc(),
        updated_at: chrono::Utc::now().naive_utc(),
        last_login_at: None,
        recent_logins: Vec::new(),
        invite_code: None,
    };
    app.state.repo.users().insert(&user).await.unwrap();

    let body = serde_json::json!({"password": "NewPass1!"});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/users/{}/reset-password", user.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn reset_password_missing_auth() {
    let app = TestApp::new().await;

    let body = serde_json::json!({"password": "NewPass1!"});
    let req = Request::builder()
        .method("POST")
        .uri("/admin/users/some-id/reset-password")
        .header("Content-Type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}
