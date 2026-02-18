mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::{TestApp, ADMIN_KEY};
use sea_orm::{ActiveModelTrait, ColumnTrait, EntityTrait, QueryFilter, Set};

// ─── Helper: create admin user and get Bearer token ────────────────────────

async fn create_admin_user_and_login(app: &TestApp, client_id: &str) -> String {
    // Register a normal user
    let resp = app
        .register_user(client_id, "admin@test.com", "AdminPass1!")
        .await;
    resp.assert_status(StatusCode::OK);

    // Promote to admin via DB
    let user = entity::user::Entity::find()
        .filter(entity::user::Column::Email.eq("admin@test.com"))
        .one(&app.state.db)
        .await
        .unwrap()
        .unwrap();

    let mut active: entity::user::ActiveModel = user.into();
    active.role = Set("admin".to_string());
    active.update(&app.state.db).await.unwrap();

    // Login again to get a token with admin role
    let resp = app
        .login_user(client_id, "admin@test.com", "AdminPass1!")
        .await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    json["access_token"].as_str().unwrap().to_string()
}

// ─── JWT role claim ─────────────────────────────────────────────────────────

#[tokio::test]
async fn login_jwt_contains_role_claim() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    app.register_user(&created.client_id, "role@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    let resp = app
        .login_user(&created.client_id, "role@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    let token = json["access_token"].as_str().unwrap();

    // Decode JWT and check role
    let claims = app.state.jwt.verify_access_token(token).unwrap();
    assert_eq!(claims.role, "user");
}

#[tokio::test]
async fn admin_user_jwt_has_admin_role() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let admin_token = create_admin_user_and_login(&app, &created.client_id).await;
    let claims = app.state.jwt.verify_access_token(&admin_token).unwrap();
    assert_eq!(claims.role, "admin");
}

// ─── AdminAuth dual-mode: Bearer token with admin role ──────────────────────

#[tokio::test]
async fn admin_api_with_bearer_token_admin_role() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let admin_token = create_admin_user_and_login(&app, &created.client_id).await;

    // Use Bearer token to call admin API
    let req = Request::builder()
        .method("GET")
        .uri("/admin/applications")
        .header("Authorization", format!("Bearer {admin_token}"))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let list: Vec<serde_json::Value> = resp.json();
    assert_eq!(list.len(), 1); // The app we created
}

#[tokio::test]
async fn admin_api_with_bearer_token_non_admin_rejected() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    // Register and login as normal user
    app.register_user(&created.client_id, "normie@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    let resp = app
        .login_user(&created.client_id, "normie@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    let user_token = json["access_token"].as_str().unwrap();

    // Try to use normal user's Bearer token for admin API
    let req = Request::builder()
        .method("GET")
        .uri("/admin/applications")
        .header("Authorization", format!("Bearer {user_token}"))
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::FORBIDDEN);
}

#[tokio::test]
async fn admin_api_x_admin_key_still_works() {
    let app = TestApp::new().await;

    // Original X-Admin-Key still works
    let req = Request::builder()
        .method("GET")
        .uri("/admin/applications")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
}

// ─── User is_active check ───────────────────────────────────────────────────

#[tokio::test]
async fn disabled_user_cannot_login() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    app.register_user(&created.client_id, "disabled@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    // Disable user via admin API
    let user = entity::user::Entity::find()
        .filter(entity::user::Column::Email.eq("disabled@test.com"))
        .one(&app.state.db)
        .await
        .unwrap()
        .unwrap();

    let mut active: entity::user::ActiveModel = user.into();
    active.is_active = Set(false);
    active.update(&app.state.db).await.unwrap();

    // Login should fail
    let resp = app
        .login_user(&created.client_id, "disabled@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::FORBIDDEN);

    let json: serde_json::Value = resp.json();
    assert_eq!(json["error"], "user_disabled");
}

// ─── GET /admin/stats ───────────────────────────────────────────────────────

#[tokio::test]
async fn stats_endpoint() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App1", &["https://a.com/cb"], &["openid"])
        .await;
    app.admin_create_app("App2", &["https://b.com/cb"], &["profile"])
        .await;

    // Register users
    app.register_user(&created.client_id, "u1@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);
    app.register_user(&created.client_id, "u2@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    let req = Request::builder()
        .method("GET")
        .uri("/admin/stats")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    assert_eq!(json["applications"]["total"], 2);
    assert_eq!(json["applications"]["active"], 2);
    assert_eq!(json["applications"]["inactive"], 0);
    assert_eq!(json["users"]["total"], 2);
    assert_eq!(json["users"]["recent"], 2); // both registered just now
}

// ─── GET /admin/users (paginated) ───────────────────────────────────────────

#[tokio::test]
async fn list_users_paginated() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    // Create 3 users
    for i in 1..=3 {
        app.register_user(
            &created.client_id,
            &format!("user{i}@test.com"),
            "Password1!",
        )
        .await
        .assert_status(StatusCode::OK);
    }

    // List page 1, per_page 2
    let req = Request::builder()
        .method("GET")
        .uri("/admin/users?page=1&per_page=2")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    assert_eq!(json["total"], 3);
    assert_eq!(json["page"], 1);
    assert_eq!(json["per_page"], 2);
    assert_eq!(json["users"].as_array().unwrap().len(), 2);

    // List page 2
    let req = Request::builder()
        .method("GET")
        .uri("/admin/users?page=2&per_page=2")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    assert_eq!(json["total"], 3);
    assert_eq!(json["users"].as_array().unwrap().len(), 1);
}

#[tokio::test]
async fn list_users_search() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    app.register_user(&created.client_id, "alice@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);
    app.register_user(&created.client_id, "bob@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    // Search for alice
    let req = Request::builder()
        .method("GET")
        .uri("/admin/users?search=alice")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    assert_eq!(json["total"], 1);
    assert_eq!(json["users"][0]["email"], "alice@test.com");
}

#[tokio::test]
async fn list_users_response_has_role_and_is_active() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    app.register_user(&created.client_id, "fields@test.com", "Password1!")
        .await
        .assert_status(StatusCode::OK);

    let req = Request::builder()
        .method("GET")
        .uri("/admin/users")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    let user = &json["users"][0];
    assert_eq!(user["role"], "user");
    assert_eq!(user["is_active"], true);
    assert!(user["created_at"].as_str().is_some());
    assert!(user["updated_at"].as_str().is_some());
}

// ─── GET /admin/users/:id ───────────────────────────────────────────────────

#[tokio::test]
async fn get_user_detail() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "detail@test.com", "Password1!")
        .await;
    reg_resp.assert_status(StatusCode::OK);
    let reg_json: serde_json::Value = reg_resp.json();
    let user_id = reg_json["user_id"].as_str().unwrap();

    let req = Request::builder()
        .method("GET")
        .uri(format!("/admin/users/{user_id}"))
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    assert_eq!(json["id"], user_id);
    assert_eq!(json["email"], "detail@test.com");
    assert_eq!(json["role"], "user");
    assert_eq!(json["is_active"], true);
}

#[tokio::test]
async fn get_user_not_found() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("GET")
        .uri("/admin/users/nonexistent-id")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

// ─── PATCH /admin/users/:id ─────────────────────────────────────────────────

#[tokio::test]
async fn update_user_role() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "promote@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let user_id = reg_json["user_id"].as_str().unwrap();

    // Promote to admin
    let body = serde_json::json!({"role": "admin"});
    let req = Request::builder()
        .method("PATCH")
        .uri(format!("/admin/users/{user_id}"))
        .header("Content-Type", "application/json")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let json: serde_json::Value = resp.json();
    assert_eq!(json["role"], "admin");

    // Login again — JWT should have admin role
    let resp = app
        .login_user(&created.client_id, "promote@test.com", "Password1!")
        .await;
    let json: serde_json::Value = resp.json();
    let token = json["access_token"].as_str().unwrap();
    let claims = app.state.jwt.verify_access_token(token).unwrap();
    assert_eq!(claims.role, "admin");
}

#[tokio::test]
async fn update_user_disable() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "toggle@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let user_id = reg_json["user_id"].as_str().unwrap();

    // Disable
    let body = serde_json::json!({"is_active": false});
    let req = Request::builder()
        .method("PATCH")
        .uri(format!("/admin/users/{user_id}"))
        .header("Content-Type", "application/json")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["is_active"], false);

    // Login should now fail
    let resp = app
        .login_user(&created.client_id, "toggle@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::FORBIDDEN);
}

#[tokio::test]
async fn update_user_invalid_role_rejected() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "badrole@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let user_id = reg_json["user_id"].as_str().unwrap();

    let body = serde_json::json!({"role": "superadmin"});
    let req = Request::builder()
        .method("PATCH")
        .uri(format!("/admin/users/{user_id}"))
        .header("Content-Type", "application/json")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

// ─── GET /admin/users/:id/accounts ──────────────────────────────────────────

#[tokio::test]
async fn get_user_accounts() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "accts@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let user_id = reg_json["user_id"].as_str().unwrap();

    let req = Request::builder()
        .method("GET")
        .uri(format!("/admin/users/{user_id}/accounts"))
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let accounts: Vec<serde_json::Value> = resp.json();
    assert_eq!(accounts.len(), 1);
    assert_eq!(accounts[0]["provider_id"], "password");
    assert_eq!(accounts[0]["provider_account_id"], "accts@test.com");
}

#[tokio::test]
async fn get_user_accounts_user_not_found() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("GET")
        .uri("/admin/users/nonexistent-id/accounts")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

// ─── DELETE /admin/users/:id/accounts/:provider_id ──────────────────────────

#[tokio::test]
async fn admin_unlink_account() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    // Add test provider to the app
    let body = serde_json::json!({
        "provider_id": "test",
        "config": {}
    });
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Register user (has password account)
    let reg_resp = app
        .register_user(&created.client_id, "unlink@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let user_id = reg_json["user_id"].as_str().unwrap();
    let access_token = reg_json["access_token"].as_str().unwrap();

    // Link a test account
    let body = serde_json::json!({"credential": {"account_id": "test-account-123"}});
    let req = Request::builder()
        .method("POST")
        .uri("/api/users/me/accounts/test/link")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {access_token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Now admin unlinks the test account
    let req = Request::builder()
        .method("DELETE")
        .uri(format!("/admin/users/{user_id}/accounts/test"))
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["status"], "unlinked");

    // Verify only 1 account remains
    let req = Request::builder()
        .method("GET")
        .uri(format!("/admin/users/{user_id}/accounts"))
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let accounts: Vec<serde_json::Value> = resp.json();
    assert_eq!(accounts.len(), 1);
    assert_eq!(accounts[0]["provider_id"], "password");
}

#[tokio::test]
async fn admin_unlink_last_account_rejected() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "last@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let user_id = reg_json["user_id"].as_str().unwrap();

    // Try to unlink the only account
    let req = Request::builder()
        .method("DELETE")
        .uri(format!("/admin/users/{user_id}/accounts/password"))
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

// ─── GET /admin/applications/:id/providers ──────────────────────────────────

#[tokio::test]
async fn list_providers_for_app() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    // Add a provider
    let body = serde_json::json!({
        "provider_id": "wechat",
        "config": {"appid": "wx123"}
    });
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Content-Type", "application/json")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // List providers
    let req = Request::builder()
        .method("GET")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let providers: Vec<serde_json::Value> = resp.json();
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0]["provider_id"], "wechat");
}

#[tokio::test]
async fn list_providers_empty() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let req = Request::builder()
        .method("GET")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    let providers: Vec<serde_json::Value> = resp.json();
    assert!(providers.is_empty());
}

#[tokio::test]
async fn list_providers_app_not_found() {
    let app = TestApp::new().await;

    let req = Request::builder()
        .method("GET")
        .uri("/admin/applications/nonexistent-id/providers")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::empty())
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
}

// ─── Bearer token auth for all new admin endpoints ──────────────────────────

#[tokio::test]
async fn bearer_token_works_for_all_new_admin_endpoints() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let admin_token = create_admin_user_and_login(&app, &created.client_id).await;

    // GET /admin/stats
    let req = Request::builder()
        .method("GET")
        .uri("/admin/stats")
        .header("Authorization", format!("Bearer {admin_token}"))
        .body(Body::empty())
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // GET /admin/users
    let req = Request::builder()
        .method("GET")
        .uri("/admin/users")
        .header("Authorization", format!("Bearer {admin_token}"))
        .body(Body::empty())
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // GET /admin/applications/:id/providers
    let req = Request::builder()
        .method("GET")
        .uri(format!("/admin/applications/{}/providers", created.id))
        .header("Authorization", format!("Bearer {admin_token}"))
        .body(Body::empty())
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // POST /admin/applications (create another app)
    let body = serde_json::json!({
        "name": "App2",
        "redirect_uris": ["https://b.com/cb"],
        "allowed_scopes": ["profile"],
    });
    let req = Request::builder()
        .method("POST")
        .uri("/admin/applications")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {admin_token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);
}

// ─── Refresh disabled user ──────────────────────────────────────────────────

#[tokio::test]
async fn refresh_token_fails_for_disabled_user() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let reg_resp = app
        .register_user(&created.client_id, "refresh-dis@test.com", "Password1!")
        .await;
    let reg_json: serde_json::Value = reg_resp.json();
    let user_id = reg_json["user_id"].as_str().unwrap();
    let refresh_token = reg_json["refresh_token"].as_str().unwrap();

    // Disable user
    let body = serde_json::json!({"is_active": false});
    let req = Request::builder()
        .method("PATCH")
        .uri(format!("/admin/users/{user_id}"))
        .header("Content-Type", "application/json")
        .header("X-Admin-Key", ADMIN_KEY)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Refresh should fail
    let body = serde_json::json!({"refresh_token": refresh_token});
    let req = Request::builder()
        .method("POST")
        .uri("/api/auth/refresh")
        .header("Content-Type", "application/json")
        .header("X-Client-Id", &created.client_id)
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();

    let resp = app.request(req).await;
    resp.assert_status(StatusCode::FORBIDDEN);
}
