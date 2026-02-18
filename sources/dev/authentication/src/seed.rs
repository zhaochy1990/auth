use sea_orm::{ActiveModelTrait, ColumnTrait, DatabaseConnection, EntityTrait, QueryFilter, Set};

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
    db: &DatabaseConnection,
    admin_email: &str,
    admin_password: Option<&str>,
) -> Result<SeedResult, Box<dyn std::error::Error>> {
    // 1. Create or find Admin Dashboard application
    let existing_app = entity::application::Entity::find()
        .filter(entity::application::Column::Name.eq("Admin Dashboard"))
        .one(db)
        .await?;

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
        let client_secret_hash = crate::auth::password::hash_password(&client_secret)?;

        let now = chrono::Utc::now().naive_utc();
        let app_id = uuid::Uuid::new_v4().to_string();
        let app = entity::application::ActiveModel {
            id: Set(app_id.clone()),
            name: Set("Admin Dashboard".to_string()),
            client_id: Set(client_id.clone()),
            client_secret_hash: Set(client_secret_hash),
            redirect_uris: Set(serde_json::to_string(&["http://localhost:5173"]).unwrap()),
            allowed_scopes: Set(serde_json::to_string(&["admin"]).unwrap()),
            is_active: Set(true),
            created_at: Set(now),
            updated_at: Set(now),
        };
        app.insert(db).await?;

        // Add password provider to the app
        let provider = entity::app_provider::ActiveModel {
            id: Set(uuid::Uuid::new_v4().to_string()),
            app_id: Set(app_id),
            provider_id: Set("password".to_string()),
            config: Set("{}".to_string()),
            is_active: Set(true),
            created_at: Set(now),
        };
        provider.insert(db).await?;

        (client_id, Some(client_secret))
    };

    // 2. Create or promote admin user
    let existing_user = entity::user::Entity::find()
        .filter(entity::user::Column::Email.eq(admin_email))
        .one(db)
        .await?;

    let user_action = if let Some(user) = existing_user {
        if user.role == "admin" {
            "already_admin".to_string()
        } else {
            let mut active: entity::user::ActiveModel = user.into();
            active.role = Set("admin".to_string());
            active.updated_at = Set(chrono::Utc::now().naive_utc());
            active.update(db).await?;
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

        let user = entity::user::ActiveModel {
            id: Set(user_id.clone()),
            email: Set(Some(admin_email.to_string())),
            name: Set(Some("Admin".to_string())),
            avatar_url: Set(None),
            email_verified: Set(true),
            role: Set("admin".to_string()),
            is_active: Set(true),
            created_at: Set(now),
            updated_at: Set(now),
        };
        user.insert(db).await?;

        let account = entity::account::ActiveModel {
            id: Set(uuid::Uuid::new_v4().to_string()),
            user_id: Set(user_id),
            provider_id: Set("password".to_string()),
            provider_account_id: Set(Some(admin_email.to_string())),
            credential: Set(Some(password_hash)),
            provider_metadata: Set("{}".to_string()),
            created_at: Set(now),
            updated_at: Set(now),
        };
        account.insert(db).await?;

        "created".to_string()
    };

    Ok(SeedResult {
        app_client_id,
        app_client_secret,
        user_action,
    })
}
