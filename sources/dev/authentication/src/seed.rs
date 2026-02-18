use crate::db::models::{Account, AppProvider, Application, User};
use crate::db::pool::Db;
use crate::db::queries;
use crate::error::AppError;

/// Result of a bootstrap/seed operation.
#[derive(Debug)]
pub struct SeedResult {
    pub app_client_id: String,
    /// Only set when a new application is created.
    pub app_client_secret: Option<String>,
    /// What happened to the user: "created", "promoted", or "already_admin".
    pub user_action: String,
}

/// Bootstrap the admin dashboard application and admin user.
///
/// - Creates the "Admin Dashboard" application if it doesn't exist.
/// - Creates or promotes the admin user.
/// - `admin_password` is required when the user doesn't exist yet.
pub async fn bootstrap(
    db: &Db,
    admin_email: &str,
    admin_password: Option<&str>,
) -> Result<SeedResult, Box<dyn std::error::Error>> {
    // 1. Create or find Admin Dashboard application
    let existing_app = queries::applications::find_by_name(db, "Admin Dashboard").await?;

    let (app_client_id, app_client_secret) = if let Some(app) = existing_app {
        (app.client_id, None)
    } else {
        let client_id = format!(
            "app_{}",
            uuid::Uuid::new_v4()
                .to_string()
                .replace('-', "")
                .get(..24)
                .unwrap()
        );
        let client_secret = {
            use rand::Rng;
            let mut rng = rand::thread_rng();
            let bytes: Vec<u8> = (0..32).map(|_| rng.gen()).collect();
            hex::encode(bytes)
        };
        let client_secret_hash = crate::auth::password::hash_client_secret(&client_secret);

        let now = chrono::Utc::now().naive_utc();
        let app_id = uuid::Uuid::new_v4().to_string();

        let app = Application {
            id: app_id.clone(),
            name: "Admin Dashboard".to_string(),
            client_id: client_id.clone(),
            client_secret_hash,
            redirect_uris: serde_json::to_string(&["http://localhost:5173"]).unwrap(),
            allowed_scopes: serde_json::to_string(&["admin"]).unwrap(),
            is_active: true,
            created_at: now,
            updated_at: now,
        };
        queries::applications::insert(db, &app).await?;

        // Add password provider to the app
        let provider = AppProvider {
            id: uuid::Uuid::new_v4().to_string(),
            app_id,
            provider_id: "password".to_string(),
            config: "{}".to_string(),
            is_active: true,
            created_at: now,
        };
        queries::app_providers::insert(db, &provider).await?;

        (client_id, Some(client_secret))
    };

    // 2. Create or promote admin user
    let existing_user = queries::users::find_by_email(db, admin_email).await?;

    let user_action = if let Some(mut user) = existing_user {
        if user.role == "admin" {
            "already_admin".to_string()
        } else {
            user.role = "admin".to_string();
            user.updated_at = chrono::Utc::now().naive_utc();
            queries::users::update(db, &user).await?;
            "promoted".to_string()
        }
    } else {
        // Password is required when creating a new user
        let password = admin_password.ok_or_else(|| {
            AppError::BadRequest(
                "Password is required when creating a new admin user. Usage: cargo run -- seed <email> <password>".to_string(),
            )
        })?;

        let password_hash = crate::auth::password::hash_password(password)?;
        let now = chrono::Utc::now().naive_utc();
        let user_id = uuid::Uuid::new_v4().to_string();

        let user = User {
            id: user_id.clone(),
            email: Some(admin_email.to_string()),
            name: Some("Admin".to_string()),
            avatar_url: None,
            email_verified: true,
            role: "admin".to_string(),
            is_active: true,
            created_at: now,
            updated_at: now,
        };
        queries::users::insert(db, &user).await?;

        let account = Account {
            id: uuid::Uuid::new_v4().to_string(),
            user_id,
            provider_id: "password".to_string(),
            provider_account_id: Some(admin_email.to_string()),
            credential: Some(password_hash),
            provider_metadata: "{}".to_string(),
            created_at: now,
            updated_at: now,
        };
        queries::accounts::insert(db, &account).await?;

        "created".to_string()
    };

    Ok(SeedResult {
        app_client_id,
        app_client_secret,
        user_action,
    })
}
