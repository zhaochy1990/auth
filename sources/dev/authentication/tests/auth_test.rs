mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::TestApp;
use serial_test::serial;

// ─── Register ────────────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn register_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid", "profile"])
        .await;

    let resp = app
        .register_user(&created.client_id, "alice@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    assert!(!json["user_id"].as_str().unwrap().is_empty());
    assert!(!json["access_token"].as_str().unwrap().is_empty());
    assert!(!json["refresh_token"].as_str().unwrap().is_empty());
    assert_eq!(json["token_type"], "Bearer");
}

#[serial]
#[tokio::test]
async fn register_duplicate_email() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    app.register_user(&created.client_id, "dup@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    let resp = app
        .register_user(&created.client_id, "dup@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::CONFLICT);
}

#[serial]
#[tokio::test]
async fn register_missing_client_id() {
    let app = TestApp::new().await;

    let body = serde_json::json!({
        "email": "no-client@test.com",
        "password": "Password1!",
    });

    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/register")
        .header("Content-Type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn register_invalid_client_id() {
    let app = TestApp::new().await;

    let resp = app
        .register_user("app_nonexistent00000000", "test@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

#[serial]
#[tokio::test]
async fn register_inactive_app() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    // Deactivate
    let body = serde_json::json!({"is_active": false});
    let req = Request::builder()
        .method("PATCH")
        .uri(format!("/admin/applications/{}", created.id))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    let resp = app
        .register_user(&created.client_id, "test@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::FORBIDDEN);
}

// ─── Login ───────────────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn login_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid", "profile"])
        .await;

    app.register_user(&created.client_id, "login@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    let resp = app
        .login_user(&created.client_id, "login@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    assert!(!json["access_token"].as_str().unwrap().is_empty());
    assert!(!json["refresh_token"].as_str().unwrap().is_empty());
    assert_eq!(json["token_type"], "Bearer");
}

#[serial]
#[tokio::test]
async fn login_wrong_password() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    app.register_user(&created.client_id, "user@test.com", "Correct1!")
        .await
        .assert_status(StatusCode::OK);

    let resp = app
        .login_user(&created.client_id, "user@test.com", "Wrong1!")
        .await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

#[serial]
#[tokio::test]
async fn login_nonexistent_email() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let resp = app
        .login_user(&created.client_id, "ghost@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

#[serial]
#[tokio::test]
async fn login_access_token_valid_jwt() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid", "profile"])
        .await;

    app.register_user(&created.client_id, "jwt@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    let resp = app
        .login_user(&created.client_id, "jwt@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    let token = json["access_token"].as_str().unwrap();

    // Verify JWT is valid by using it on the profile endpoint
    let req = Request::builder()
        .method("GET")
        .uri("/api/users/me")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let profile: serde_json::Value = resp.json();
    assert_eq!(profile["email"], "jwt@test.com");
}

// ─── Refresh ─────────────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn refresh_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "refresh@test.com", "Password1!")
        .await;
    reg_resp.assert_status(StatusCode::OK);
    let reg_json: serde_json::Value = reg_resp.json();
    let refresh_token = reg_json["refresh_token"].as_str().unwrap();

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

    let json: serde_json::Value = resp.json();
    assert!(!json["access_token"].as_str().unwrap().is_empty());
    assert!(!json["refresh_token"].as_str().unwrap().is_empty());
}

#[serial]
#[tokio::test]
async fn refresh_token_rotation() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "rotate@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let old_token = reg_json["refresh_token"].as_str().unwrap().to_string();

    // Use refresh token
    let body = serde_json::json!({"refresh_token": old_token});
    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/refresh")
        .header("Content-Type", "application/json")
        .header("X-Client-Id", &created.client_id)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    let new_token = json["refresh_token"].as_str().unwrap();

    // Old token should be different from new
    assert_ne!(old_token, new_token);

    // Old token should be revoked (cannot reuse)
    let body = serde_json::json!({"refresh_token": old_token});
    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/refresh")
        .header("Content-Type", "application/json")
        .header("X-Client-Id", &created.client_id)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

#[serial]
#[tokio::test]
async fn refresh_invalid_token() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let body = serde_json::json!({"refresh_token": "totally-bogus-token"});
    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/refresh")
        .header("Content-Type", "application/json")
        .header("X-Client-Id", &created.client_id)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

// ─── Logout ──────────────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn logout_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "logout@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let access_token = reg_json["access_token"].as_str().unwrap();
    let refresh_token = reg_json["refresh_token"].as_str().unwrap();

    let body = serde_json::json!({"refresh_token": refresh_token});
    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/logout")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {access_token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
}

#[serial]
#[tokio::test]
async fn logout_requires_auth() {
    let app = TestApp::new().await;

    let body = serde_json::json!({"refresh_token": "some-token"});
    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/logout")
        .header("Content-Type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}
