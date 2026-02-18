mod common;

use auth_service::seed::bootstrap;
use sea_orm::{ColumnTrait, Database, EntityTrait, QueryFilter};

async fn test_db() -> sea_orm::DatabaseConnection {
    let db = Database::connect("sqlite::memory:")
        .await
        .expect("Failed to connect");
    use migration::MigratorTrait;
    migration::Migrator::up(&db, None)
        .await
        .expect("Failed to run migrations");
    db
}

// ─── New user + new app ─────────────────────────────────────────────────────

#[tokio::test]
async fn seed_creates_app_and_user() {
    let db = test_db().await;

    let result = bootstrap(&db, "admin@test.com", Some("StrongPass1!"))
        .await
        .expect("seed failed");

    // App created
    assert!(result.app_client_id.starts_with("app_"));
    assert!(result.app_client_secret.is_some());
    assert_eq!(result.user_action, "created");

    // Verify user in DB
    let user = entity::user::Entity::find()
        .filter(entity::user::Column::Email.eq("admin@test.com"))
        .one(&db)
        .await
        .unwrap()
        .expect("user not found");

    assert_eq!(user.role, "admin");
    assert!(user.is_active);
    assert!(user.email_verified);

    // Verify password account
    let account = entity::account::Entity::find()
        .filter(entity::account::Column::UserId.eq(&user.id))
        .filter(entity::account::Column::ProviderId.eq("password"))
        .one(&db)
        .await
        .unwrap()
        .expect("account not found");

    assert!(account.credential.is_some());

    // Verify app in DB
    let app = entity::application::Entity::find()
        .filter(entity::application::Column::Name.eq("Admin Dashboard"))
        .one(&db)
        .await
        .unwrap()
        .expect("app not found");

    assert!(app.is_active);
    assert_eq!(app.client_id, result.app_client_id);

    // Verify password provider added to app
    let provider = entity::app_provider::Entity::find()
        .filter(entity::app_provider::Column::AppId.eq(&app.id))
        .filter(entity::app_provider::Column::ProviderId.eq("password"))
        .one(&db)
        .await
        .unwrap();

    assert!(provider.is_some());
}

// ─── Password required for new user ─────────────────────────────────────────

#[tokio::test]
async fn seed_requires_password_for_new_user() {
    let db = test_db().await;

    let result = bootstrap(&db, "admin@test.com", None).await;
    assert!(result.is_err());

    let err = result.unwrap_err().to_string();
    assert!(err.contains("Password is required"));
}

// ─── Idempotent re-run ──────────────────────────────────────────────────────

#[tokio::test]
async fn seed_is_idempotent() {
    let db = test_db().await;

    // First run
    let r1 = bootstrap(&db, "admin@test.com", Some("Pass1!"))
        .await
        .expect("first seed failed");

    assert!(r1.app_client_secret.is_some());
    assert_eq!(r1.user_action, "created");

    // Second run — same email
    let r2 = bootstrap(&db, "admin@test.com", None)
        .await
        .expect("second seed failed");

    // App already exists, no new secret
    assert!(r2.app_client_secret.is_none());
    // Same client_id
    assert_eq!(r2.app_client_id, r1.app_client_id);
    // User already admin
    assert_eq!(r2.user_action, "already_admin");

    // Only 1 app and 1 user in DB
    let apps = entity::application::Entity::find()
        .all(&db)
        .await
        .unwrap();
    assert_eq!(apps.len(), 1);

    let users = entity::user::Entity::find().all(&db).await.unwrap();
    assert_eq!(users.len(), 1);
}

// ─── Promote existing non-admin user ────────────────────────────────────────

#[tokio::test]
async fn seed_promotes_existing_user() {
    // Create a regular user first via the test app helper
    let app = common::TestApp::new().await;
    app.register_user(
        &app.admin_create_app("TestApp", &["https://a.com"], &["openid"])
            .await
            .client_id,
        "regular@test.com",
        "Password1!",
    )
    .await;

    // Verify user is "user" role
    let user = entity::user::Entity::find()
        .filter(entity::user::Column::Email.eq("regular@test.com"))
        .one(&app.state.db)
        .await
        .unwrap()
        .expect("user not found");
    assert_eq!(user.role, "user");

    // Seed with same email — should promote, not create
    let result = bootstrap(&app.state.db, "regular@test.com", None)
        .await
        .expect("seed failed");

    assert_eq!(result.user_action, "promoted");

    // Verify role changed
    let user = entity::user::Entity::find()
        .filter(entity::user::Column::Email.eq("regular@test.com"))
        .one(&app.state.db)
        .await
        .unwrap()
        .expect("user not found");
    assert_eq!(user.role, "admin");
}

// ─── Created admin can log in ───────────────────────────────────────────────

#[tokio::test]
async fn seeded_admin_can_login() {
    let app = common::TestApp::new().await;

    let result = bootstrap(&app.state.db, "seed-admin@test.com", Some("SeedPass1!"))
        .await
        .expect("seed failed");

    assert_eq!(result.user_action, "created");

    // Login with the seeded credentials
    let resp = app
        .login_user(&result.app_client_id, "seed-admin@test.com", "SeedPass1!")
        .await;
    resp.assert_status(axum::http::StatusCode::OK);

    // Verify JWT has admin role
    let json: serde_json::Value = resp.json();
    let token = json["access_token"].as_str().unwrap();
    let claims = app.state.jwt.verify_access_token(token).unwrap();
    assert_eq!(claims.role, "admin");
}
