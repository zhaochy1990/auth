mod common;

use auth_service::seed::bootstrap;
use serial_test::serial;

// ─── New user + new app ─────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn seed_creates_app_and_user() {
    let app = common::TestApp::new().await;

    let result = bootstrap(&app.state.db, "admin@seed-test.com", Some("StrongPass1!"))
        .await
        .expect("seed failed");

    // App already existed (created by TestApp::new bootstrap), so no new secret
    // But the user should be "created"
    assert_eq!(result.user_action, "created");

    // Verify user in DB
    let user =
        auth_service::db::queries::users::find_by_email(&app.state.db, "admin@seed-test.com")
            .await
            .unwrap()
            .expect("user not found");

    assert_eq!(user.role, "admin");
    assert!(user.is_active);
    assert!(user.email_verified);

    // Verify password account
    let account = auth_service::db::queries::accounts::find_by_user_and_provider(
        &app.state.db,
        &user.id,
        "password",
    )
    .await
    .unwrap()
    .expect("account not found");

    assert!(account.credential.is_some());
}

// ─── Password required for new user ─────────────────────────────────────────

#[serial]
#[tokio::test]
async fn seed_requires_password_for_new_user() {
    let app = common::TestApp::new().await;

    let result = bootstrap(&app.state.db, "nopass@seed-test.com", None).await;
    assert!(result.is_err());

    let err = result.unwrap_err().to_string();
    assert!(err.contains("Password is required"));
}

// ─── Idempotent re-run ──────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn seed_is_idempotent() {
    let app = common::TestApp::new().await;

    // TestApp::new() already bootstrapped "test-admin@internal"
    // Running again with same email should get "already_admin"
    let r2 = bootstrap(&app.state.db, "test-admin@internal", None)
        .await
        .expect("second seed failed");

    // App already exists, no new secret
    assert!(r2.app_client_secret.is_none());
    // User already admin
    assert_eq!(r2.user_action, "already_admin");
}

// ─── Promote existing non-admin user ────────────────────────────────────────

#[serial]
#[tokio::test]
async fn seed_promotes_existing_user() {
    let app = common::TestApp::new().await;
    let created = app
        .admin_create_app("TestApp", &["https://a.com"], &["openid"])
        .await;

    app.register_user(&created.client_id, "regular@seed-test.com", "Password1!")
        .await;

    // Verify user is "user" role
    let user =
        auth_service::db::queries::users::find_by_email(&app.state.db, "regular@seed-test.com")
            .await
            .unwrap()
            .expect("user not found");
    assert_eq!(user.role, "user");

    // Seed with same email — should promote, not create
    let result = bootstrap(&app.state.db, "regular@seed-test.com", None)
        .await
        .expect("seed failed");

    assert_eq!(result.user_action, "promoted");

    // Verify role changed
    let user =
        auth_service::db::queries::users::find_by_email(&app.state.db, "regular@seed-test.com")
            .await
            .unwrap()
            .expect("user not found");
    assert_eq!(user.role, "admin");
}

// ─── Created admin can log in ───────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn seeded_admin_can_login() {
    let app = common::TestApp::new().await;

    let result = bootstrap(
        &app.state.db,
        "seed-admin@seed-test.com",
        Some("SeedPass1!"),
    )
    .await
    .expect("seed failed");

    assert_eq!(result.user_action, "created");

    // Login with the seeded credentials
    let resp = app
        .login_user(
            &result.app_client_id,
            "seed-admin@seed-test.com",
            "SeedPass1!",
        )
        .await;
    resp.assert_status(axum::http::StatusCode::OK);

    // Verify JWT has admin role
    let json: serde_json::Value = resp.json();
    let token = json["access_token"].as_str().unwrap();
    let claims = app.state.jwt.verify_access_token(token).unwrap();
    assert_eq!(claims.role, "admin");
}
