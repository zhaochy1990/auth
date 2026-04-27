use axum::http::StatusCode;
use axum::response::IntoResponse;
use axum::{extract::Path, extract::State, Json};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::auth::middleware::{AuthenticatedUser, ClientApp};
use crate::auth::oauth2 as oauth2_util;
use crate::auth::password::{hash_password, validate_password, verify_password};
use crate::auth::providers;
use crate::db::models::{Account, User};
use crate::error::AppError;
use crate::AppState;

// --- Request / Response types ---

#[derive(Debug, Deserialize)]
pub struct RegisterRequest {
    pub email: String,
    pub password: String,
    pub name: Option<String>,
    pub invite_code: Option<String>,
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
) -> Result<axum::response::Response, AppError> {
    // Validate password complexity
    validate_password(&req.password)?;

    // Feature-flagged invite code gate
    let require_invite = std::env::var("STRIDE_REQUIRE_INVITE_CODE")
        .map(|v| v.eq_ignore_ascii_case("true"))
        .unwrap_or(false);

    let invite_code_record = if require_invite {
        let code_str = req
            .invite_code
            .as_deref()
            .filter(|s| !s.is_empty())
            .ok_or(AppError::BadRequest("invite_code is required".into()))?;

        let record = state
            .repo
            .invite_codes()
            .get_invite_code_by_code(code_str)
            .await?
            .ok_or(AppError::InviteCodeNotFound)?;

        if record.is_revoked {
            return Err(AppError::InviteCodeNotFound);
        }
        if record.used_at.is_some() {
            return Err(AppError::InviteCodeAlreadyUsed);
        }
        Some(record)
    } else {
        None
    };

    // Check if user with this email already exists
    let existing = state.repo.users().find_by_email(&req.email).await?;

    if existing.is_some() {
        return Err(AppError::UserAlreadyExists);
    }

    let now = chrono::Utc::now().naive_utc();
    let user_id = Uuid::new_v4().to_string();

    // Claim the invite code FIRST (before creating user/account) using ETag-atomic
    // mark_invite_code_used. If the claim fails (race), no orphan rows remain.
    if let Some(ref code_record) = invite_code_record {
        state
            .repo
            .invite_codes()
            .mark_invite_code_used(&code_record.code, &user_id)
            .await?;
    }

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
    // If user creation fails, the invite code stays claimed (used) with our user_id
    // but no user exists; admin can revoke if needed. No earlier rollback to perform.
    state.repo.users().insert(&user).await?;

    // Create password account
    let password_hash = hash_password(&req.password)?;
    let account_id = Uuid::new_v4().to_string();
    let account = Account {
        id: account_id.clone(),
        user_id: user_id.clone(),
        provider_id: "password".to_string(),
        provider_account_id: Some(req.email),
        credential: Some(password_hash),
        provider_metadata: "{}".to_string(),
        created_at: now,
        updated_at: now,
    };
    if let Err(e) = state.repo.accounts().insert(&account).await {
        // Roll back BOTH the user row AND any partially-inserted account state
        // (best-effort; ignore inner errors).
        let _ = state.repo.accounts().delete_by_id(&account_id).await;
        let _ = state.repo.users().delete_by_id(&user_id).await;
        return Err(e);
    }

    // Issue tokens
    let scopes = client_app.allowed_scopes.clone();
    let access_token =
        match state
            .jwt
            .issue_access_token(&user_id, &client_app.client_id, scopes.clone(), "user")
        {
            Ok(t) => t,
            Err(e) => {
                let _ = state.repo.accounts().delete_by_id(&account_id).await;
                let _ = state.repo.users().delete_by_id(&user_id).await;
                return Err(e);
            }
        };
    let refresh_token = oauth2_util::generate_refresh_token();

    if let Err(e) = oauth2_util::store_refresh_token(
        self::repo_ref(&state),
        &user_id,
        &client_app.app_id,
        &refresh_token,
        &scopes,
        None,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await
    {
        let _ = state.repo.accounts().delete_by_id(&account_id).await;
        let _ = state.repo.users().delete_by_id(&user_id).await;
        return Err(e);
    }

    let body = RegisterResponse {
        user_id,
        access_token,
        refresh_token,
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
    };
    Ok((StatusCode::CREATED, Json(body)).into_response())
}

pub async fn login(
    client_app: ClientApp,
    State(state): State<AppState>,
    Json(req): Json<LoginRequest>,
) -> Result<Json<TokenResponse>, AppError> {
    // Find user by email
    let user = state
        .repo
        .users()
        .find_by_email(&req.email)
        .await?
        .ok_or(AppError::InvalidCredentials)?;

    if !user.is_active {
        return Err(AppError::UserDisabled);
    }

    // Find password account
    let account = state
        .repo
        .accounts()
        .find_by_user_and_provider(&user.id, "password")
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
        repo_ref(&state),
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
    let app_provider = state
        .repo
        .app_providers()
        .find_by_app_and_provider(&client_app.app_id, &provider_id)
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
    let existing_account = state
        .repo
        .accounts()
        .find_by_provider_account(&provider_id, &provider_info.provider_account_id)
        .await?;

    let (user_id, user_role) = if let Some(mut account) = existing_account {
        // Existing user — update metadata
        account.provider_metadata =
            serde_json::to_string(&provider_info.metadata).unwrap_or_default();
        account.updated_at = now;
        state.repo.accounts().update(&account).await?;

        // Look up user for role and is_active check
        let user = state
            .repo
            .users()
            .find_by_id(&account.user_id)
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
        state.repo.users().insert(&user).await?;

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
        state.repo.accounts().insert(&account).await?;

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
        repo_ref(&state),
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
        repo_ref(&state),
        &req.refresh_token,
        &client_app.app_id,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await?;

    // Look up user for current role
    let user = state
        .repo
        .users()
        .find_by_id(&user_id)
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
    oauth2_util::revoke_refresh_token(repo_ref(&state), &req.refresh_token).await?;
    Ok(Json(serde_json::json!({"status": "ok"})))
}

/// Helper to get a `&dyn Repository` from `AppState`.
fn repo_ref(state: &AppState) -> &dyn crate::db::repository::Repository {
    state.repo.as_ref()
}
