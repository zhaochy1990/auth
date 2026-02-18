mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::TestApp;
use serial_test::serial;

/// Helper: create app, register user, return (CreatedApp, access_token, user_id).
async fn setup(app: &TestApp) -> (common::CreatedApp, String, String) {
    let created = app
        .admin_create_app(
            "User App",
            &["https://example.com/cb"],
            &["openid", "profile"],
        )
        .await;

    let resp = app
        .register_user(&created.client_id, "user@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    let access_token = json["access_token"].as_str().unwrap().to_string();
    let user_id = json["user_id"].as_str().unwrap().to_string();

    (created, access_token, user_id)
}

// ─── Get Profile ─────────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn get_profile_success() {
    let app = TestApp::new().await;
    let (_, token, _) = setup(&app).await;

    let req = Request::builder()
        .method("GET")
        .uri("/api/users/me")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["email"], "user@test.com");
    assert_eq!(json["email_verified"], false);
}

#[serial]
#[tokio::test]
async fn get_profile_unauthorized() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("GET")
        .uri("/api/users/me")
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

#[serial]
#[tokio::test]
async fn get_profile_invalid_token() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("GET")
        .uri("/api/users/me")
        .header("Authorization", "Bearer invalid.jwt.token")
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

// ─── Update Profile ──────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn update_profile_name() {
    let app = TestApp::new().await;
    let (_, token, _) = setup(&app).await;

    let body = serde_json::json!({"name": "Alice"});
    let req = Request::builder()
        .method("PATCH")
        .uri("/api/users/me")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["name"], "Alice");
}

#[serial]
#[tokio::test]
async fn update_profile_avatar() {
    let app = TestApp::new().await;
    let (_, token, _) = setup(&app).await;

    let body = serde_json::json!({"avatar_url": "https://example.com/avatar.png"});
    let req = Request::builder()
        .method("PATCH")
        .uri("/api/users/me")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["avatar_url"], "https://example.com/avatar.png");
}

// ─── List Accounts ───────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn list_accounts_after_register() {
    let app = TestApp::new().await;
    let (_, token, _) = setup(&app).await;

    let req = Request::builder()
        .method("GET")
        .uri("/api/users/me/accounts")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let accounts: Vec<serde_json::Value> = resp.json();
    assert_eq!(accounts.len(), 1);
    assert_eq!(accounts[0]["provider_id"], "password");
}

// ─── Unlink Account ──────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn unlink_last_account_rejected() {
    let app = TestApp::new().await;
    let (_, token, _) = setup(&app).await;

    let req = Request::builder()
        .method("DELETE")
        .uri("/api/users/me/accounts/password")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["error"], "cannot_unlink_last_account");
}

// ─── Link + Unlink (test provider) ──────────────────────────────────────────

#[cfg(feature = "test-providers")]
#[serial]
#[tokio::test]
async fn link_and_unlink_account() {
    let app = TestApp::new().await;
    let (created, token, _) = setup(&app).await;

    // Add test provider to the app
    let body = serde_json::json!({
        "provider_id": "test",
        "config": {}
    });
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Link the test provider account
    let body = serde_json::json!({
        "credential": {
            "account_id": "test-account-123",
            "email": "test@provider.com",
            "name": "Test User"
        }
    });
    let req = Request::builder()
        .method("POST")
        .uri("/api/users/me/accounts/test/link")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["provider_id"], "test");

    // Verify two accounts now
    let req = Request::builder()
        .method("GET")
        .uri("/api/users/me/accounts")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let accounts: Vec<serde_json::Value> = resp.json();
    assert_eq!(accounts.len(), 2);

    // Unlink the test provider (now safe — still have password account)
    let req = Request::builder()
        .method("DELETE")
        .uri("/api/users/me/accounts/test")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    // Verify back to one account
    let req = Request::builder()
        .method("GET")
        .uri("/api/users/me/accounts")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    let accounts: Vec<serde_json::Value> = resp.json();
    assert_eq!(accounts.len(), 1);
}
