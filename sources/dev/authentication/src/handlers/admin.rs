use axum::{
    extract::{Path, Query, State},
    Json,
};
use sea_orm::{
    ActiveModelTrait, ColumnTrait, Condition, EntityTrait, PaginatorTrait, QueryFilter, QueryOrder,
    Set,
};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::auth::middleware::AdminAuth;
use crate::auth::password::hash_password;
use crate::error::AppError;
use crate::AppState;

// --- Request / Response types ---

#[derive(Debug, Deserialize)]
pub struct CreateApplicationRequest {
    pub name: String,
    pub redirect_uris: Vec<String>,
    pub allowed_scopes: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct CreateApplicationResponse {
    pub id: String,
    pub name: String,
    pub client_id: String,
    pub client_secret: String, // Only returned on create
    pub redirect_uris: Vec<String>,
    pub allowed_scopes: Vec<String>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateApplicationRequest {
    pub name: Option<String>,
    pub redirect_uris: Option<Vec<String>>,
    pub allowed_scopes: Option<Vec<String>>,
    pub is_active: Option<bool>,
}

#[derive(Debug, Serialize)]
pub struct ApplicationResponse {
    pub id: String,
    pub name: String,
    pub client_id: String,
    pub redirect_uris: Vec<String>,
    pub allowed_scopes: Vec<String>,
    pub is_active: bool,
    pub created_at: String,
}

#[derive(Debug, Deserialize)]
pub struct AddProviderRequest {
    pub provider_id: String,
    pub config: serde_json::Value,
}

#[derive(Debug, Serialize)]
pub struct ProviderResponse {
    pub id: String,
    pub provider_id: String,
    pub is_active: bool,
    pub created_at: String,
}

#[derive(Debug, Serialize)]
pub struct RotateSecretResponse {
    pub client_id: String,
    pub client_secret: String,
}

#[derive(Debug, Deserialize)]
pub struct ListUsersQuery {
    pub page: Option<u64>,
    pub per_page: Option<u64>,
    pub search: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct UserResponse {
    pub id: String,
    pub email: Option<String>,
    pub name: Option<String>,
    pub avatar_url: Option<String>,
    pub email_verified: bool,
    pub role: String,
    pub is_active: bool,
    pub created_at: String,
    pub updated_at: String,
}

#[derive(Debug, Serialize)]
pub struct UserListResponse {
    pub users: Vec<UserResponse>,
    pub total: u64,
    pub page: u64,
    pub per_page: u64,
}

#[derive(Debug, Deserialize)]
pub struct UpdateUserRequest {
    pub name: Option<String>,
    pub role: Option<String>,
    pub is_active: Option<bool>,
}

#[derive(Debug, Serialize)]
pub struct UserAccountResponse {
    pub id: String,
    pub provider_id: String,
    pub provider_account_id: Option<String>,
    pub created_at: String,
}

#[derive(Debug, Serialize)]
pub struct StatsResponse {
    pub applications: AppStats,
    pub users: UserStats,
}

#[derive(Debug, Serialize)]
pub struct AppStats {
    pub total: u64,
    pub active: u64,
    pub inactive: u64,
}

#[derive(Debug, Serialize)]
pub struct UserStats {
    pub total: u64,
    pub recent: u64,
}

// --- Handlers ---

pub async fn create_application(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Json(req): Json<CreateApplicationRequest>,
) -> Result<Json<CreateApplicationResponse>, AppError> {
    let client_id = generate_client_id();
    let client_secret = generate_client_secret();
    let client_secret_hash = hash_password(&client_secret)?;

    let now = chrono::Utc::now().naive_utc();
    let id = Uuid::new_v4().to_string();

    let model = entity::application::ActiveModel {
        id: Set(id.clone()),
        name: Set(req.name.clone()),
        client_id: Set(client_id.clone()),
        client_secret_hash: Set(client_secret_hash),
        redirect_uris: Set(serde_json::to_string(&req.redirect_uris).unwrap()),
        allowed_scopes: Set(serde_json::to_string(&req.allowed_scopes).unwrap()),
        is_active: Set(true),
        created_at: Set(now),
        updated_at: Set(now),
    };

    model.insert(&state.db).await?;

    Ok(Json(CreateApplicationResponse {
        id,
        name: req.name,
        client_id,
        client_secret,
        redirect_uris: req.redirect_uris,
        allowed_scopes: req.allowed_scopes,
    }))
}

pub async fn list_applications(
    _admin: AdminAuth,
    State(state): State<AppState>,
) -> Result<Json<Vec<ApplicationResponse>>, AppError> {
    let apps = entity::application::Entity::find()
        .all(&state.db)
        .await?;

    let responses: Vec<ApplicationResponse> = apps
        .into_iter()
        .map(|app| ApplicationResponse {
            id: app.id,
            name: app.name,
            client_id: app.client_id,
            redirect_uris: serde_json::from_str(&app.redirect_uris).unwrap_or_default(),
            allowed_scopes: serde_json::from_str(&app.allowed_scopes).unwrap_or_default(),
            is_active: app.is_active,
            created_at: app.created_at.to_string(),
        })
        .collect();

    Ok(Json(responses))
}

pub async fn update_application(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(id): Path<String>,
    Json(req): Json<UpdateApplicationRequest>,
) -> Result<Json<ApplicationResponse>, AppError> {
    let app = entity::application::Entity::find_by_id(&id)
        .one(&state.db)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    let mut active: entity::application::ActiveModel = app.into();

    if let Some(name) = req.name {
        active.name = Set(name);
    }
    if let Some(redirect_uris) = req.redirect_uris {
        active.redirect_uris = Set(serde_json::to_string(&redirect_uris).unwrap());
    }
    if let Some(allowed_scopes) = req.allowed_scopes {
        active.allowed_scopes = Set(serde_json::to_string(&allowed_scopes).unwrap());
    }
    if let Some(is_active) = req.is_active {
        active.is_active = Set(is_active);
    }
    active.updated_at = Set(chrono::Utc::now().naive_utc());

    let updated = active.update(&state.db).await?;

    Ok(Json(ApplicationResponse {
        id: updated.id,
        name: updated.name,
        client_id: updated.client_id,
        redirect_uris: serde_json::from_str(&updated.redirect_uris).unwrap_or_default(),
        allowed_scopes: serde_json::from_str(&updated.allowed_scopes).unwrap_or_default(),
        is_active: updated.is_active,
        created_at: updated.created_at.to_string(),
    }))
}

pub async fn add_provider(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(app_id): Path<String>,
    Json(req): Json<AddProviderRequest>,
) -> Result<Json<ProviderResponse>, AppError> {
    // Verify application exists
    entity::application::Entity::find_by_id(&app_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    // Check if provider already exists for this app
    let existing = entity::app_provider::Entity::find()
        .filter(entity::app_provider::Column::AppId.eq(&app_id))
        .filter(entity::app_provider::Column::ProviderId.eq(&req.provider_id))
        .one(&state.db)
        .await?;

    if existing.is_some() {
        return Err(AppError::BadRequest(
            "Provider already configured for this application".to_string(),
        ));
    }

    let now = chrono::Utc::now().naive_utc();
    let id = Uuid::new_v4().to_string();

    let model = entity::app_provider::ActiveModel {
        id: Set(id.clone()),
        app_id: Set(app_id),
        provider_id: Set(req.provider_id.clone()),
        config: Set(serde_json::to_string(&req.config).unwrap()),
        is_active: Set(true),
        created_at: Set(now),
    };

    model.insert(&state.db).await?;

    Ok(Json(ProviderResponse {
        id,
        provider_id: req.provider_id,
        is_active: true,
        created_at: now.to_string(),
    }))
}

pub async fn remove_provider(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path((app_id, provider_id)): Path<(String, String)>,
) -> Result<Json<serde_json::Value>, AppError> {
    let provider = entity::app_provider::Entity::find()
        .filter(entity::app_provider::Column::AppId.eq(&app_id))
        .filter(entity::app_provider::Column::ProviderId.eq(&provider_id))
        .one(&state.db)
        .await?
        .ok_or(AppError::ProviderNotConfigured)?;

    entity::app_provider::Entity::delete_by_id(&provider.id)
        .exec(&state.db)
        .await?;

    Ok(Json(serde_json::json!({"status": "deleted"})))
}

pub async fn rotate_secret(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> Result<Json<RotateSecretResponse>, AppError> {
    let app = entity::application::Entity::find_by_id(&id)
        .one(&state.db)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    let new_secret = generate_client_secret();
    let new_hash = hash_password(&new_secret)?;

    let mut active: entity::application::ActiveModel = app.into();
    active.client_secret_hash = Set(new_hash);
    active.updated_at = Set(chrono::Utc::now().naive_utc());
    let updated = active.update(&state.db).await?;

    Ok(Json(RotateSecretResponse {
        client_id: updated.client_id,
        client_secret: new_secret,
    }))
}

pub async fn list_providers(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(app_id): Path<String>,
) -> Result<Json<Vec<ProviderResponse>>, AppError> {
    // Verify application exists
    entity::application::Entity::find_by_id(&app_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    let providers = entity::app_provider::Entity::find()
        .filter(entity::app_provider::Column::AppId.eq(&app_id))
        .all(&state.db)
        .await?;

    let responses = providers
        .into_iter()
        .map(|p| ProviderResponse {
            id: p.id,
            provider_id: p.provider_id,
            is_active: p.is_active,
            created_at: p.created_at.to_string(),
        })
        .collect();

    Ok(Json(responses))
}

pub async fn list_users(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Query(query): Query<ListUsersQuery>,
) -> Result<Json<UserListResponse>, AppError> {
    let page = query.page.unwrap_or(1).max(1);
    let per_page = query.per_page.unwrap_or(20).min(100);

    let mut find = entity::user::Entity::find();

    if let Some(ref search) = query.search {
        if !search.is_empty() {
            find = find.filter(
                Condition::any()
                    .add(entity::user::Column::Email.contains(search))
                    .add(entity::user::Column::Name.contains(search)),
            );
        }
    }

    let find = find.order_by_desc(entity::user::Column::CreatedAt);

    let paginator = find.paginate(&state.db, per_page);
    let total = paginator.num_items().await?;
    let users = paginator.fetch_page(page - 1).await?;

    let responses = users
        .into_iter()
        .map(|u| UserResponse {
            id: u.id,
            email: u.email,
            name: u.name,
            avatar_url: u.avatar_url,
            email_verified: u.email_verified,
            role: u.role,
            is_active: u.is_active,
            created_at: u.created_at.to_string(),
            updated_at: u.updated_at.to_string(),
        })
        .collect();

    Ok(Json(UserListResponse {
        users: responses,
        total,
        page,
        per_page,
    }))
}

pub async fn get_user(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> Result<Json<UserResponse>, AppError> {
    let user = entity::user::Entity::find_by_id(&id)
        .one(&state.db)
        .await?
        .ok_or(AppError::UserNotFound)?;

    Ok(Json(UserResponse {
        id: user.id,
        email: user.email,
        name: user.name,
        avatar_url: user.avatar_url,
        email_verified: user.email_verified,
        role: user.role,
        is_active: user.is_active,
        created_at: user.created_at.to_string(),
        updated_at: user.updated_at.to_string(),
    }))
}

pub async fn get_user_accounts(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(user_id): Path<String>,
) -> Result<Json<Vec<UserAccountResponse>>, AppError> {
    // Verify user exists
    entity::user::Entity::find_by_id(&user_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::UserNotFound)?;

    let accounts = entity::account::Entity::find()
        .filter(entity::account::Column::UserId.eq(&user_id))
        .all(&state.db)
        .await?;

    let responses = accounts
        .into_iter()
        .map(|a| UserAccountResponse {
            id: a.id,
            provider_id: a.provider_id,
            provider_account_id: a.provider_account_id,
            created_at: a.created_at.to_string(),
        })
        .collect();

    Ok(Json(responses))
}

pub async fn update_user(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(id): Path<String>,
    Json(req): Json<UpdateUserRequest>,
) -> Result<Json<UserResponse>, AppError> {
    let user = entity::user::Entity::find_by_id(&id)
        .one(&state.db)
        .await?
        .ok_or(AppError::UserNotFound)?;

    let mut active: entity::user::ActiveModel = user.into();

    if let Some(name) = req.name {
        active.name = Set(Some(name));
    }
    if let Some(role) = req.role {
        if role != "user" && role != "admin" {
            return Err(AppError::BadRequest("Role must be 'user' or 'admin'".to_string()));
        }
        active.role = Set(role);
    }
    if let Some(is_active) = req.is_active {
        active.is_active = Set(is_active);
    }
    active.updated_at = Set(chrono::Utc::now().naive_utc());

    let updated = active.update(&state.db).await?;

    Ok(Json(UserResponse {
        id: updated.id,
        email: updated.email,
        name: updated.name,
        avatar_url: updated.avatar_url,
        email_verified: updated.email_verified,
        role: updated.role,
        is_active: updated.is_active,
        created_at: updated.created_at.to_string(),
        updated_at: updated.updated_at.to_string(),
    }))
}

pub async fn admin_unlink_account(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path((user_id, provider_id)): Path<(String, String)>,
) -> Result<Json<serde_json::Value>, AppError> {
    // Verify user exists
    entity::user::Entity::find_by_id(&user_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::UserNotFound)?;

    let account = entity::account::Entity::find()
        .filter(entity::account::Column::UserId.eq(&user_id))
        .filter(entity::account::Column::ProviderId.eq(&provider_id))
        .one(&state.db)
        .await?
        .ok_or(AppError::BadRequest("Account not linked".to_string()))?;

    // Don't allow unlinking the last account
    let count = entity::account::Entity::find()
        .filter(entity::account::Column::UserId.eq(&user_id))
        .count(&state.db)
        .await?;

    if count <= 1 {
        return Err(AppError::CannotUnlinkLastAccount);
    }

    entity::account::Entity::delete_by_id(&account.id)
        .exec(&state.db)
        .await?;

    Ok(Json(serde_json::json!({"status": "unlinked"})))
}

pub async fn stats(
    _admin: AdminAuth,
    State(state): State<AppState>,
) -> Result<Json<StatsResponse>, AppError> {
    let total_apps = entity::application::Entity::find()
        .count(&state.db)
        .await?;
    let active_apps = entity::application::Entity::find()
        .filter(entity::application::Column::IsActive.eq(true))
        .count(&state.db)
        .await?;

    let total_users = entity::user::Entity::find().count(&state.db).await?;

    // Recent users: registered in last 7 days
    let seven_days_ago =
        (chrono::Utc::now() - chrono::Duration::days(7)).naive_utc();
    let recent_users = entity::user::Entity::find()
        .filter(entity::user::Column::CreatedAt.gte(seven_days_ago))
        .count(&state.db)
        .await?;

    Ok(Json(StatsResponse {
        applications: AppStats {
            total: total_apps,
            active: active_apps,
            inactive: total_apps - active_apps,
        },
        users: UserStats {
            total: total_users,
            recent: recent_users,
        },
    }))
}

// --- Helpers ---

fn generate_client_id() -> String {
    format!("app_{}", Uuid::new_v4().to_string().replace('-', "")[..24].to_string())
}

fn generate_client_secret() -> String {
    use rand::Rng;
    let mut rng = rand::thread_rng();
    let bytes: Vec<u8> = (0..32).map(|_| rng.gen()).collect();
    hex::encode(bytes)
}
