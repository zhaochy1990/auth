use axum::{
    extract::{Path, Query, State},
    Json,
};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::auth::middleware::AdminAuth;
use crate::auth::password::{hash_client_secret, hash_password, validate_password};
use crate::db::models::{Account, AppProvider, Application, User};
use crate::db::queries;
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

#[derive(Debug, Deserialize)]
pub struct CreateUserRequest {
    pub email: String,
    pub password: String,
    pub name: Option<String>,
    pub role: Option<String>,
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
    let client_secret_hash = hash_client_secret(&client_secret);

    let now = chrono::Utc::now().naive_utc();
    let id = Uuid::new_v4().to_string();

    let app = Application {
        id: id.clone(),
        name: req.name.clone(),
        client_id: client_id.clone(),
        client_secret_hash,
        redirect_uris: serde_json::to_string(&req.redirect_uris).unwrap(),
        allowed_scopes: serde_json::to_string(&req.allowed_scopes).unwrap(),
        is_active: true,
        created_at: now,
        updated_at: now,
    };

    queries::applications::insert(&state.db, &app).await?;

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
    let apps = queries::applications::find_all(&state.db).await?;

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
    let mut app = queries::applications::find_by_id(&state.db, &id)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    if let Some(name) = req.name {
        app.name = name;
    }
    if let Some(redirect_uris) = req.redirect_uris {
        app.redirect_uris = serde_json::to_string(&redirect_uris).unwrap();
    }
    if let Some(allowed_scopes) = req.allowed_scopes {
        app.allowed_scopes = serde_json::to_string(&allowed_scopes).unwrap();
    }
    if let Some(is_active) = req.is_active {
        app.is_active = is_active;
    }
    app.updated_at = chrono::Utc::now().naive_utc();

    queries::applications::update(&state.db, &app).await?;

    Ok(Json(ApplicationResponse {
        id: app.id,
        name: app.name,
        client_id: app.client_id,
        redirect_uris: serde_json::from_str(&app.redirect_uris).unwrap_or_default(),
        allowed_scopes: serde_json::from_str(&app.allowed_scopes).unwrap_or_default(),
        is_active: app.is_active,
        created_at: app.created_at.to_string(),
    }))
}

pub async fn add_provider(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(app_id): Path<String>,
    Json(req): Json<AddProviderRequest>,
) -> Result<Json<ProviderResponse>, AppError> {
    // Verify application exists
    queries::applications::find_by_id(&state.db, &app_id)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    // Check if provider already exists for this app
    let existing =
        queries::app_providers::find_by_app_and_provider(&state.db, &app_id, &req.provider_id)
            .await?;

    if existing.is_some() {
        return Err(AppError::BadRequest(
            "Provider already configured for this application".to_string(),
        ));
    }

    let now = chrono::Utc::now().naive_utc();
    let id = Uuid::new_v4().to_string();

    let ap = AppProvider {
        id: id.clone(),
        app_id,
        provider_id: req.provider_id.clone(),
        config: serde_json::to_string(&req.config).unwrap(),
        is_active: true,
        created_at: now,
    };

    queries::app_providers::insert(&state.db, &ap).await?;

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
    let provider =
        queries::app_providers::find_by_app_and_provider(&state.db, &app_id, &provider_id)
            .await?
            .ok_or(AppError::ProviderNotConfigured)?;

    queries::app_providers::delete_by_id(&state.db, &provider.id).await?;

    Ok(Json(serde_json::json!({"status": "deleted"})))
}

pub async fn rotate_secret(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> Result<Json<RotateSecretResponse>, AppError> {
    let mut app = queries::applications::find_by_id(&state.db, &id)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    let new_secret = generate_client_secret();
    let new_hash = hash_client_secret(&new_secret);

    app.client_secret_hash = new_hash;
    app.updated_at = chrono::Utc::now().naive_utc();
    queries::applications::update(&state.db, &app).await?;

    Ok(Json(RotateSecretResponse {
        client_id: app.client_id,
        client_secret: new_secret,
    }))
}

pub async fn list_providers(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(app_id): Path<String>,
) -> Result<Json<Vec<ProviderResponse>>, AppError> {
    // Verify application exists
    queries::applications::find_by_id(&state.db, &app_id)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    let providers = queries::app_providers::find_all_by_app(&state.db, &app_id).await?;

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
    let offset = (page - 1) * per_page;

    let (users, total) =
        queries::users::list_paginated(&state.db, query.search.as_deref(), offset, per_page)
            .await?;

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
    let user = queries::users::find_by_id(&state.db, &id)
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
    queries::users::find_by_id(&state.db, &user_id)
        .await?
        .ok_or(AppError::UserNotFound)?;

    let accounts = queries::accounts::find_all_by_user(&state.db, &user_id).await?;

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

pub async fn create_user(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Json(req): Json<CreateUserRequest>,
) -> Result<Json<UserResponse>, AppError> {
    validate_password(&req.password)?;

    let role = req.role.unwrap_or_else(|| "user".to_string());
    if role != "user" && role != "admin" {
        return Err(AppError::BadRequest(
            "Role must be 'user' or 'admin'".to_string(),
        ));
    }

    let existing = queries::users::find_by_email(&state.db, &req.email).await?;
    if existing.is_some() {
        return Err(AppError::UserAlreadyExists);
    }

    let now = chrono::Utc::now().naive_utc();
    let user_id = Uuid::new_v4().to_string();

    let user = User {
        id: user_id.clone(),
        email: Some(req.email.clone()),
        name: req.name,
        avatar_url: None,
        email_verified: false,
        role,
        is_active: true,
        created_at: now,
        updated_at: now,
    };
    queries::users::insert(&state.db, &user).await?;

    let password_hash = hash_password(&req.password)?;
    let account = Account {
        id: Uuid::new_v4().to_string(),
        user_id: user_id.clone(),
        provider_id: "password".to_string(),
        provider_account_id: Some(req.email),
        credential: Some(password_hash),
        provider_metadata: "{}".to_string(),
        created_at: now,
        updated_at: now,
    };
    queries::accounts::insert(&state.db, &account).await?;

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

pub async fn update_user(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path(id): Path<String>,
    Json(req): Json<UpdateUserRequest>,
) -> Result<Json<UserResponse>, AppError> {
    let mut user = queries::users::find_by_id(&state.db, &id)
        .await?
        .ok_or(AppError::UserNotFound)?;

    if let Some(name) = req.name {
        user.name = Some(name);
    }
    if let Some(role) = req.role {
        if role != "user" && role != "admin" {
            return Err(AppError::BadRequest(
                "Role must be 'user' or 'admin'".to_string(),
            ));
        }
        user.role = role;
    }
    if let Some(is_active) = req.is_active {
        user.is_active = is_active;
    }
    user.updated_at = chrono::Utc::now().naive_utc();

    queries::users::update(&state.db, &user).await?;

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

pub async fn admin_unlink_account(
    _admin: AdminAuth,
    State(state): State<AppState>,
    Path((user_id, provider_id)): Path<(String, String)>,
) -> Result<Json<serde_json::Value>, AppError> {
    // Verify user exists
    queries::users::find_by_id(&state.db, &user_id)
        .await?
        .ok_or(AppError::UserNotFound)?;

    let account = queries::accounts::find_by_user_and_provider(&state.db, &user_id, &provider_id)
        .await?
        .ok_or(AppError::BadRequest("Account not linked".to_string()))?;

    // Don't allow unlinking the last account
    let count = queries::accounts::count_by_user(&state.db, &user_id).await?;

    if count <= 1 {
        return Err(AppError::CannotUnlinkLastAccount);
    }

    queries::accounts::delete_by_id(&state.db, &account.id).await?;

    Ok(Json(serde_json::json!({"status": "unlinked"})))
}

pub async fn stats(
    _admin: AdminAuth,
    State(state): State<AppState>,
) -> Result<Json<StatsResponse>, AppError> {
    let total_apps = queries::applications::count_all(&state.db).await?;
    let active_apps = queries::applications::count_active(&state.db).await?;

    let total_users = queries::users::count_all(&state.db).await?;

    // Recent users: registered in last 7 days
    let seven_days_ago = (chrono::Utc::now() - chrono::Duration::days(7)).naive_utc();
    let recent_users = queries::users::count_since(&state.db, seven_days_ago).await?;

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
    format!("app_{}", &Uuid::new_v4().to_string().replace('-', "")[..24])
}

fn generate_client_secret() -> String {
    use rand::Rng;
    let mut rng = rand::thread_rng();
    let bytes: Vec<u8> = (0..32).map(|_| rng.gen()).collect();
    hex::encode(bytes)
}
