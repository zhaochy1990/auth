use axum::{extract::Path, extract::State, http::StatusCode, Json};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::auth::middleware::AuthenticatedUser;
use crate::auth::providers;
use crate::db::models::{Account, MembershipTier};
use crate::error::AppError;
use crate::handlers::resolve_membership;
use crate::AppState;

// --- Request / Response types ---

#[derive(Debug, Serialize)]
pub struct UserProfileResponse {
    pub id: String,
    pub email: Option<String>,
    pub name: Option<String>,
    pub avatar_url: Option<String>,
    pub email_verified: bool,
    /// Effective membership tier (an expired paid tier reads as `regular`).
    pub membership: MembershipTier,
    /// ISO 8601 expiry of a paid membership, or null when there is none.
    pub membership_expires_at: Option<String>,
    pub created_at: String,
}

#[derive(Debug, Deserialize)]
pub struct UpdateProfileRequest {
    pub name: Option<String>,
    pub avatar_url: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct AccountResponse {
    pub provider_id: String,
    pub provider_account_id: Option<String>,
    pub created_at: String,
}

#[derive(Debug, Deserialize)]
pub struct LinkAccountRequest {
    pub credential: serde_json::Value,
}

// --- Handlers ---

pub async fn get_profile(
    user: AuthenticatedUser,
    State(state): State<AppState>,
) -> Result<Json<UserProfileResponse>, AppError> {
    let mut db_user = state
        .repo
        .users()
        .find_by_id(&user.user_id)
        .await?
        .ok_or(AppError::UserNotFound)?;

    // Surface the effective tier, lazily downgrading an expired paid membership.
    let membership = resolve_membership(state.repo.as_ref(), &mut db_user).await;

    Ok(Json(UserProfileResponse {
        id: db_user.id,
        email: db_user.email,
        name: db_user.name,
        avatar_url: db_user.avatar_url,
        email_verified: db_user.email_verified,
        membership,
        membership_expires_at: db_user.membership_expires_at.map(|d| d.to_string()),
        created_at: db_user.created_at.to_string(),
    }))
}

pub async fn update_profile(
    user: AuthenticatedUser,
    State(state): State<AppState>,
    Json(req): Json<UpdateProfileRequest>,
) -> Result<Json<UserProfileResponse>, AppError> {
    let mut db_user = state
        .repo
        .users()
        .find_by_id(&user.user_id)
        .await?
        .ok_or(AppError::UserNotFound)?;

    if let Some(name) = req.name {
        db_user.name = Some(name);
    }
    if let Some(avatar_url) = req.avatar_url {
        db_user.avatar_url = Some(avatar_url);
    }
    let now = chrono::Utc::now().naive_utc();
    db_user.updated_at = now;

    state.repo.users().update(&db_user).await?;

    let membership = db_user.effective_membership(now);
    Ok(Json(UserProfileResponse {
        id: db_user.id,
        email: db_user.email,
        name: db_user.name,
        avatar_url: db_user.avatar_url,
        email_verified: db_user.email_verified,
        membership,
        membership_expires_at: db_user.membership_expires_at.map(|d| d.to_string()),
        created_at: db_user.created_at.to_string(),
    }))
}

pub async fn list_accounts(
    user: AuthenticatedUser,
    State(state): State<AppState>,
) -> Result<Json<Vec<AccountResponse>>, AppError> {
    let accounts = state
        .repo
        .accounts()
        .find_all_by_user(&user.user_id)
        .await?;

    let responses: Vec<AccountResponse> = accounts
        .into_iter()
        .map(|a| AccountResponse {
            provider_id: a.provider_id,
            provider_account_id: a.provider_account_id,
            created_at: a.created_at.to_string(),
        })
        .collect();

    Ok(Json(responses))
}

pub async fn link_account(
    user: AuthenticatedUser,
    State(state): State<AppState>,
    Path(provider_id): Path<String>,
    Json(req): Json<LinkAccountRequest>,
) -> Result<Json<AccountResponse>, AppError> {
    // Check if this provider is already linked
    let existing = state
        .repo
        .accounts()
        .find_by_user_and_provider(&user.user_id, &provider_id)
        .await?;

    if existing.is_some() {
        return Err(AppError::AccountAlreadyLinked);
    }

    // Find provider config scoped to the user's current app
    let app = state
        .repo
        .applications()
        .find_by_client_id(&user.client_id)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    let app_provider = state
        .repo
        .app_providers()
        .find_by_app_and_provider(&app.id, &provider_id)
        .await?
        .ok_or(AppError::ProviderNotConfigured)?;

    let config: serde_json::Value = serde_json::from_str(&app_provider.config).unwrap_or_default();

    let provider = providers::create_provider(&provider_id, &config)?;
    let provider_info = provider.authenticate(&req.credential).await?;

    // Check if this provider account is already linked to another user
    let already_linked = state
        .repo
        .accounts()
        .find_by_provider_account(&provider_id, &provider_info.provider_account_id)
        .await?;

    if already_linked.is_some() {
        return Err(AppError::AccountAlreadyLinked);
    }

    let now = chrono::Utc::now().naive_utc();
    let account = Account {
        id: Uuid::new_v4().to_string(),
        user_id: user.user_id,
        provider_id: provider_id.clone(),
        provider_account_id: Some(provider_info.provider_account_id.clone()),
        credential: None,
        provider_metadata: serde_json::to_string(&provider_info.metadata).unwrap_or_default(),
        created_at: now,
        updated_at: now,
    };

    state.repo.accounts().insert(&account).await?;

    Ok(Json(AccountResponse {
        provider_id,
        provider_account_id: Some(provider_info.provider_account_id),
        created_at: now.to_string(),
    }))
}

pub async fn unlink_account(
    user: AuthenticatedUser,
    State(state): State<AppState>,
    Path(provider_id): Path<String>,
) -> Result<Json<serde_json::Value>, AppError> {
    // Count user's accounts
    let accounts = state
        .repo
        .accounts()
        .find_all_by_user(&user.user_id)
        .await?;

    if accounts.len() <= 1 {
        return Err(AppError::CannotUnlinkLastAccount);
    }

    let account = accounts
        .into_iter()
        .find(|a| a.provider_id == provider_id)
        .ok_or(AppError::BadRequest("Account not linked".to_string()))?;

    state.repo.accounts().delete_by_id(&account.id).await?;

    Ok(Json(serde_json::json!({"status": "unlinked"})))
}

pub async fn delete_me(
    user: AuthenticatedUser,
    State(state): State<AppState>,
) -> Result<StatusCode, AppError> {
    delete_user_account(&state, &user.user_id).await?;

    tracing::info!(user_id = %user.user_id, "user self-deleted account");
    Ok(StatusCode::NO_CONTENT)
}

pub async fn delete_user_account(state: &AppState, user_id: &str) -> Result<(), AppError> {
    state
        .repo
        .users()
        .find_by_id(user_id)
        .await?
        .ok_or(AppError::UserNotFound)?;

    let owned_teams = state.repo.teams().find_all_owned_by_user(user_id).await?;
    if !owned_teams.is_empty() {
        return Err(AppError::UserOwnsTeams(owned_teams.len()));
    }

    state
        .repo
        .refresh_tokens()
        .delete_all_by_user(user_id)
        .await?;
    state.repo.auth_codes().delete_all_by_user(user_id).await?;
    state.repo.accounts().delete_all_by_user(user_id).await?;
    state
        .repo
        .team_memberships()
        .delete_all_by_user(user_id)
        .await?;
    state.repo.users().delete_by_id(user_id).await?;

    Ok(())
}
