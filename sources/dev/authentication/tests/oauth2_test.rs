mod common;

use auth_service::auth::oauth2 as oauth2_util;
use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::TestApp;
use serial_test::serial;

/// Helper: create an app, register a user, and return everything needed for OAuth2 tests.
async fn setup_app_and_user(app: &TestApp) -> (common::CreatedApp, String, String, String) {
    let created = app
        .admin_create_app(
            "OAuth App",
            &["https://example.com/cb"],
            &["openid", "profile", "email"],
        )
        .await;

    let resp = app
        .register_user(&created.client_id, "oauth@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    let user_id = json["user_id"].as_str().unwrap().to_string();
    let refresh_token = json["refresh_token"].as_str().unwrap().to_string();

    (
        created,
        user_id,
        "oauth@test.com".to_string(),
        refresh_token,
    )
}

// ─── Client Credentials ─────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn client_credentials_grant() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({"grant_type": "client_credentials"});

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert!(!json["access_token"].as_str().unwrap().is_empty());
    assert!(json.get("refresh_token").and_then(|v| v.as_str()).is_none());
    assert_eq!(json["token_type"], "Bearer");
}

#[serial]
#[tokio::test]
async fn client_credentials_invalid_secret() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let auth = TestApp::basic_auth_header(&created.client_id, "wrong-secret");
    let body = serde_json::json!({"grant_type": "client_credentials"});

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

// ─── Password Grant ──────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn password_grant_success() {
    let app = TestApp::new().await;
    let (created, _user_id, email, _) = setup_app_and_user(&app).await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "password",
        "username": email,
        "password": "Password1!",
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert!(!json["access_token"].as_str().unwrap().is_empty());
    assert!(json["refresh_token"].as_str().is_some());
}

#[serial]
#[tokio::test]
async fn password_grant_wrong_password() {
    let app = TestApp::new().await;
    let (created, _, email, _) = setup_app_and_user(&app).await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "password",
        "username": email,
        "password": "WrongPassword!",
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

#[serial]
#[tokio::test]
async fn password_grant_scope_filtering() {
    let app = TestApp::new().await;
    let (created, _, email, _) = setup_app_and_user(&app).await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "password",
        "username": email,
        "password": "Password1!",
        "scope": "openid nonexistent",
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    // Should only include "openid" (not "nonexistent")
    let scope = json["scope"].as_str().unwrap();
    assert!(scope.contains("openid"));
    assert!(!scope.contains("nonexistent"));
}

// ─── Refresh Token Grant ─────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn refresh_token_grant() {
    let app = TestApp::new().await;
    let (created, _, _, refresh_token) = setup_app_and_user(&app).await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert!(!json["access_token"].as_str().unwrap().is_empty());
    let new_refresh = json["refresh_token"].as_str().unwrap();
    assert_ne!(new_refresh, refresh_token);
}

#[serial]
#[tokio::test]
async fn refresh_token_revoked() {
    let app = TestApp::new().await;
    let (created, _, _, refresh_token) = setup_app_and_user(&app).await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);

    // Use the refresh token once (rotates and revokes old)
    let body = serde_json::json!({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
    });
    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Try to reuse the old, now-revoked token
    let body = serde_json::json!({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
    });
    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

#[serial]
#[tokio::test]
async fn refresh_token_wrong_app() {
    let app = TestApp::new().await;
    let (_, _, _, refresh_token) = setup_app_and_user(&app).await;

    // Create a second app
    let other = app
        .admin_create_app("Other App", &["https://b.com/cb"], &["openid"])
        .await;

    let auth = TestApp::basic_auth_header(&other.client_id, &other.client_secret);
    let body = serde_json::json!({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

// ─── Authorization Code Grant ────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn authorization_code_grant() {
    let app = TestApp::new().await;
    let (created, user_id, _, _) = setup_app_and_user(&app).await;

    // Store an auth code directly
    let code = oauth2_util::generate_auth_code();
    oauth2_util::store_auth_code(
        &app.state.db,
        &code,
        &created.id,
        &user_id,
        "https://example.com/cb",
        &["openid".to_string(), "profile".to_string()],
        None,
        None,
    )
    .await
    .unwrap();

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": "https://example.com/cb",
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert!(!json["access_token"].as_str().unwrap().is_empty());
    assert!(json["refresh_token"].as_str().is_some());
}

#[serial]
#[tokio::test]
async fn authorization_code_with_pkce() {
    let app = TestApp::new().await;
    let (created, user_id, _, _) = setup_app_and_user(&app).await;

    let code_verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
    // S256: SHA256(code_verifier) base64url-encoded
    use sha2::Digest;
    let hash = sha2::Sha256::digest(code_verifier.as_bytes());
    let code_challenge =
        base64::Engine::encode(&base64::engine::general_purpose::URL_SAFE_NO_PAD, hash);

    let code = oauth2_util::generate_auth_code();
    oauth2_util::store_auth_code(
        &app.state.db,
        &code,
        &created.id,
        &user_id,
        "https://example.com/cb",
        &["openid".to_string()],
        Some(code_challenge),
        Some("S256".to_string()),
    )
    .await
    .unwrap();

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": "https://example.com/cb",
        "code_verifier": code_verifier,
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
}

#[serial]
#[tokio::test]
async fn authorization_code_pkce_mismatch() {
    let app = TestApp::new().await;
    let (created, user_id, _, _) = setup_app_and_user(&app).await;

    let code = oauth2_util::generate_auth_code();
    oauth2_util::store_auth_code(
        &app.state.db,
        &code,
        &created.id,
        &user_id,
        "https://example.com/cb",
        &["openid".to_string()],
        Some("expected-challenge".to_string()),
        Some("S256".to_string()),
    )
    .await
    .unwrap();

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": "https://example.com/cb",
        "code_verifier": "wrong-verifier",
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn authorization_code_already_used() {
    let app = TestApp::new().await;
    let (created, user_id, _, _) = setup_app_and_user(&app).await;

    let code = oauth2_util::generate_auth_code();
    oauth2_util::store_auth_code(
        &app.state.db,
        &code,
        &created.id,
        &user_id,
        "https://example.com/cb",
        &["openid".to_string()],
        None,
        None,
    )
    .await
    .unwrap();

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": "https://example.com/cb",
    });

    // First exchange — should succeed
    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Second exchange — code already used
    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn authorization_code_wrong_redirect_uri() {
    let app = TestApp::new().await;
    let (created, user_id, _, _) = setup_app_and_user(&app).await;

    let code = oauth2_util::generate_auth_code();
    oauth2_util::store_auth_code(
        &app.state.db,
        &code,
        &created.id,
        &user_id,
        "https://example.com/cb",
        &["openid".to_string()],
        None,
        None,
    )
    .await
    .unwrap();

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": "https://evil.com/cb",
    });

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

// ─── Unsupported Grant Type ──────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn unsupported_grant_type() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({"grant_type": "magic_beans"});

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

// ─── Revoke ──────────────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn revoke_token() {
    let app = TestApp::new().await;
    let (created, _, _, refresh_token) = setup_app_and_user(&app).await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({"token": refresh_token});

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/revoke")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    // Now try to use the revoked token
    let body = serde_json::json!({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
    });
    let req = Request::builder()
        .method("POST")
        .uri("/oauth/token")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}

#[serial]
#[tokio::test]
async fn revoke_invalid_token_still_200() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({"token": "totally-bogus-token"});

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/revoke")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    // Per RFC 7009, revoke should always return 200
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
}

// ─── Introspect ──────────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn introspect_valid() {
    let app = TestApp::new().await;
    let (created, _, email, _) = setup_app_and_user(&app).await;

    // Login to get a fresh access token
    let login_resp = app
        .login_user(&created.client_id, &email, "Password1!")
        .await;
    let login_json: serde_json::Value = login_resp.json();
    let access_token = login_json["access_token"].as_str().unwrap();

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({"token": access_token});

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/introspect")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["active"], true);
    assert!(json["sub"].as_str().is_some());
    assert!(json["aud"].as_str().is_some());
}

#[serial]
#[tokio::test]
async fn introspect_invalid() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let auth = TestApp::basic_auth_header(&created.client_id, &created.client_secret);
    let body = serde_json::json!({"token": "not-a-valid-jwt"});

    let req = Request::builder()
        .method("POST")
        .uri("/oauth/introspect")
        .header("Content-Type", "application/json")
        .header("Authorization", &auth)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["active"], false);
}

#[serial]
#[tokio::test]
async fn introspect_requires_auth() {
    let app = TestApp::new().await;

    let body = serde_json::json!({"token": "some-token"});
    let req = Request::builder()
        .method("POST")
        .uri("/oauth/introspect")
        .header("Content-Type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::UNAUTHORIZED);
}
