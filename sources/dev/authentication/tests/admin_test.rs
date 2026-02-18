mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::{TestApp, ADMIN_KEY};

// ─── Create Application ──────────────────────────────────────────────────────

#[tokio::test]
async fn create_application_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("Test App", &["https://example.com/cb"], &["openid", "profile"])
        .await;

    assert!(!created.id.is_empty());
    assert!(created.client_id.starts_with("app_"));
    assert_eq!(created.client_secret.len(), 64); // 32 bytes hex
}

#[tokio::test]
async fn create_application_missing_admin_key() {
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
    resp.assert_status(StatusCode::FORBIDDEN);
}

#[tokio::test]
async fn create_application_wrong_admin_key() {
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
        .header("X-Admin-Key", "wrong-key")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::FORBIDDEN);
}

// ─── List Applications ───────────────────────────────────────────────────────

#[tokio::test]
async fn list_applications_empty() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("GET")
        .uri("/admin/applications")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let list: Vec<serde_json::Value> = resp.json();
    assert!(list.is_empty());
}

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
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let list: Vec<serde_json::Value> = resp.json();
    assert_eq!(list.len(), 2);
}

// ─── Update Application ─────────────────────────────────────────────────────

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
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["name"], "New Name");
}

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
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["is_active"], false);
}

#[tokio::test]
async fn update_application_not_found() {
    let app = TestApp::new().await;

    let body = serde_json::json!({"name": "X"});
    let req = Request::builder()
        .method("PATCH")
        .uri("/admin/applications/nonexistent-id")
        .header("Content-Type", "application/json")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

// ─── Add Provider ────────────────────────────────────────────────────────────

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
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["provider_id"], "wechat");
    assert_eq!(json["is_active"], true);
}

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
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    // Duplicate
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

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
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

// ─── Remove Provider ─────────────────────────────────────────────────────────

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
        .header("X-Admin-Key", ADMIN_KEY)
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
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["status"], "deleted");
}

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
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

// ─── Rotate Secret ───────────────────────────────────────────────────────────

#[tokio::test]
async fn rotate_secret_success() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/rotate-secret", created.id))
        .header("X-Admin-Key", ADMIN_KEY)
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

#[tokio::test]
async fn rotate_secret_app_not_found() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("POST")
        .uri("/admin/applications/nonexistent-id/rotate-secret")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}
