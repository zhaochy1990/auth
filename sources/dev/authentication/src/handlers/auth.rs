use axum::{extract::Path, extract::State, Json};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::auth::middleware::{AuthenticatedUser, ClientApp};
use crate::auth::oauth2 as oauth2_util;
use crate::auth::password::{hash_password, validate_password, verify_password};
use crate::auth::providers;
use crate::db::models::{Account, User};
use crate::db::queries;
use crate::error::AppError;
use crate::AppState;

// --- Request / Response types ---

#[derive(Debug, Deserialize)]
pub struct RegisterRequest {
    pub email: String,
    pub password: String,
    pub name: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct LoginRequest {
    pub email: String,
    pub password: String,
}

#[derive(Debug, Deserialize)]
pub struct ProviderLoginRequest {
    pub credential: serde_json::Value,
}

#[derive(Debug, Deserialize)]
pub struct RefreshRequest {
    pub refresh_token: String,
}

#[derive(Debug, Deserialize)]
pub struct LogoutRequest {
    pub refresh_token: String,
}

#[derive(Debug, Serialize)]
pub struct TokenResponse {
    pub access_token: String,
    pub refresh_token: String,
    pub token_type: String,
    pub expires_in: i64,
}

#[derive(Debug, Serialize)]
pub struct RegisterResponse {
    pub user_id: String,
    pub access_token: String,
    pub refresh_token: String,
    pub token_type: String,
    pub expires_in: i64,
}

// --- Handlers ---

pub async fn register(
    client_app: ClientApp,
    State(state): State<AppState>,
    Json(req): Json<RegisterRequest>,
) -> Result<Json<RegisterResponse>, AppError> {
    // Validate password complexity
    validate_password(&req.password)?;

    // Check if user with this email already exists
    let existing = queries::users::find_by_email(&state.db, &req.email).await?;

    if existing.is_some() {
        return Err(AppError::UserAlreadyExists);
    }

    let now = chrono::Utc::now().naive_utc();
    let user_id = Uuid::new_v4().to_string();

    // Create user
    let user = User {
        id: user_id.clone(),
        email: Some(req.email.clone()),
        name: req.name,
        avatar_url: None,
        email_verified: false,
        role: "user".to_string(),
        is_active: true,
        created_at: now,
        updated_at: now,
    };
    queries::users::insert(&state.db, &user).await?;

    // Create password account
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

    // Issue tokens
    let scopes = client_app.allowed_scopes.clone();
    let access_token =
        state
            .jwt
            .issue_access_token(&user_id, &client_app.client_id, scopes.clone(), "user")?;
    let refresh_token = oauth2_util::generate_refresh_token();

    oauth2_util::store_refresh_token(
        &state.db,
        &user_id,
        &client_app.app_id,
        &refresh_token,
        &scopes,
        None,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await?;

    Ok(Json(RegisterResponse {
        user_id,
        access_token,
        refresh_token,
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
    }))
}

pub async fn login(
    client_app: ClientApp,
    State(state): State<AppState>,
    Json(req): Json<LoginRequest>,
) -> Result<Json<TokenResponse>, AppError> {
    // Find user by email
    let user = queries::users::find_by_email(&state.db, &req.email)
        .await?
        .ok_or(AppError::InvalidCredentials)?;

    if !user.is_active {
        return Err(AppError::UserDisabled);
    }

    // Find password account
    let account = queries::accounts::find_by_user_and_provider(&state.db, &user.id, "password")
        .await?
        .ok_or(AppError::InvalidCredentials)?;

    let credential = account.credential.ok_or(AppError::InvalidCredentials)?;

    if !verify_password(&req.password, &credential)? {
        return Err(AppError::InvalidCredentials);
    }

    // Issue tokens
    let scopes = client_app.allowed_scopes.clone();
    let access_token = state.jwt.issue_access_token(
        &user.id,
        &client_app.client_id,
        scopes.clone(),
        &user.role,
    )?;
    let refresh_token = oauth2_util::generate_refresh_token();

    oauth2_util::store_refresh_token(
        &state.db,
        &user.id,
        &client_app.app_id,
        &refresh_token,
        &scopes,
        None,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await?;

    Ok(Json(TokenResponse {
        access_token,
        refresh_token,
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
    }))
}

pub async fn provider_login(
    client_app: ClientApp,
    State(state): State<AppState>,
    Path(provider_id): Path<String>,
    Json(req): Json<ProviderLoginRequest>,
) -> Result<Json<TokenResponse>, AppError> {
    // Find provider config for this app
    let app_provider = queries::app_providers::find_by_app_and_provider(
        &state.db,
        &client_app.app_id,
        &provider_id,
    )
    .await?
    .ok_or(AppError::ProviderNotConfigured)?;

    if !app_provider.is_active {
        return Err(AppError::ProviderNotConfigured);
    }

    let config: serde_json::Value = serde_json::from_str(&app_provider.config).unwrap_or_default();

    // Create provider and authenticate
    let provider = providers::create_provider(&provider_id, &config)?;
    let provider_info = provider.authenticate(&req.credential).await?;

    // Find or create user
    let now = chrono::Utc::now().naive_utc();

    // Check if this provider account already exists
    let existing_account = queries::accounts::find_by_provider_account(
        &state.db,
        &provider_id,
        &provider_info.provider_account_id,
    )
    .await?;

    let (user_id, user_role) = if let Some(mut account) = existing_account {
        // Existing user — update metadata
        account.provider_metadata =
            serde_json::to_string(&provider_info.metadata).unwrap_or_default();
        account.updated_at = now;
        queries::accounts::update(&state.db, &account).await?;

        // Look up user for role and is_active check
        let user = queries::users::find_by_id(&state.db, &account.user_id)
            .await?
            .ok_or(AppError::UserNotFound)?;
        if !user.is_active {
            return Err(AppError::UserDisabled);
        }
        (account.user_id, user.role)
    } else {
        // New user
        let user_id = Uuid::new_v4().to_string();

        let user = User {
            id: user_id.clone(),
            email: provider_info.email,
            name: provider_info.name,
            avatar_url: provider_info.avatar_url,
            email_verified: false,
            role: "user".to_string(),
            is_active: true,
            created_at: now,
            updated_at: now,
        };
        queries::users::insert(&state.db, &user).await?;

        let account = Account {
            id: Uuid::new_v4().to_string(),
            user_id: user_id.clone(),
            provider_id,
            provider_account_id: Some(provider_info.provider_account_id),
            credential: None,
            provider_metadata: serde_json::to_string(&provider_info.metadata).unwrap_or_default(),
            created_at: now,
            updated_at: now,
        };
        queries::accounts::insert(&state.db, &account).await?;

        (user_id, "user".to_string())
    };

    // Issue tokens
    let scopes = client_app.allowed_scopes.clone();
    let access_token = state.jwt.issue_access_token(
        &user_id,
        &client_app.client_id,
        scopes.clone(),
        &user_role,
    )?;
    let refresh_token = oauth2_util::generate_refresh_token();

    oauth2_util::store_refresh_token(
        &state.db,
        &user_id,
        &client_app.app_id,
        &refresh_token,
        &scopes,
        None,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await?;

    Ok(Json(TokenResponse {
        access_token,
        refresh_token,
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
    }))
}

pub async fn refresh(
    client_app: ClientApp,
    State(state): State<AppState>,
    Json(req): Json<RefreshRequest>,
) -> Result<Json<TokenResponse>, AppError> {
    let (user_id, new_refresh_token, scopes) = oauth2_util::rotate_refresh_token(
        &state.db,
        &req.refresh_token,
        &client_app.app_id,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await?;

    // Look up user for current role
    let user = queries::users::find_by_id(&state.db, &user_id)
        .await?
        .ok_or(AppError::UserNotFound)?;

    if !user.is_active {
        return Err(AppError::UserDisabled);
    }

    let access_token =
        state
            .jwt
            .issue_access_token(&user_id, &client_app.client_id, scopes, &user.role)?;

    Ok(Json(TokenResponse {
        access_token,
        refresh_token: new_refresh_token,
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
    }))
}

pub async fn logout(
    _user: AuthenticatedUser,
    State(state): State<AppState>,
    Json(req): Json<LogoutRequest>,
) -> Result<Json<serde_json::Value>, AppError> {
    oauth2_util::revoke_refresh_token(&state.db, &req.refresh_token).await?;
    Ok(Json(serde_json::json!({"status": "ok"})))
}
