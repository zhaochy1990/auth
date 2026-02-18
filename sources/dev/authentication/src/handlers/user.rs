use axum::{extract::Path, extract::State, Json};
use sea_orm::{ActiveModelTrait, ColumnTrait, EntityTrait, QueryFilter, Set};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::auth::middleware::AuthenticatedUser;
use crate::auth::providers;
use crate::error::AppError;
use crate::AppState;

// --- Request / Response types ---

#[derive(Debug, Serialize)]
pub struct UserProfileResponse {
    pub id: String,
    pub email: Option<String>,
    pub name: Option<String>,
    pub avatar_url: Option<String>,
    pub email_verified: bool,
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
    let db_user = entity::user::Entity::find_by_id(&user.user_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::UserNotFound)?;

    Ok(Json(UserProfileResponse {
        id: db_user.id,
        email: db_user.email,
        name: db_user.name,
        avatar_url: db_user.avatar_url,
        email_verified: db_user.email_verified,
        created_at: db_user.created_at.to_string(),
    }))
}

pub async fn update_profile(
    user: AuthenticatedUser,
    State(state): State<AppState>,
    Json(req): Json<UpdateProfileRequest>,
) -> Result<Json<UserProfileResponse>, AppError> {
    let db_user = entity::user::Entity::find_by_id(&user.user_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::UserNotFound)?;

    let mut active: entity::user::ActiveModel = db_user.into();

    if let Some(name) = req.name {
        active.name = Set(Some(name));
    }
    if let Some(avatar_url) = req.avatar_url {
        active.avatar_url = Set(Some(avatar_url));
    }
    active.updated_at = Set(chrono::Utc::now().naive_utc());

    let updated = active.update(&state.db).await?;

    Ok(Json(UserProfileResponse {
        id: updated.id,
        email: updated.email,
        name: updated.name,
        avatar_url: updated.avatar_url,
        email_verified: updated.email_verified,
        created_at: updated.created_at.to_string(),
    }))
}

pub async fn list_accounts(
    user: AuthenticatedUser,
    State(state): State<AppState>,
) -> Result<Json<Vec<AccountResponse>>, AppError> {
    let accounts = entity::account::Entity::find()
        .filter(entity::account::Column::UserId.eq(&user.user_id))
        .all(&state.db)
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
    let existing = entity::account::Entity::find()
        .filter(entity::account::Column::UserId.eq(&user.user_id))
        .filter(entity::account::Column::ProviderId.eq(&provider_id))
        .one(&state.db)
        .await?;

    if existing.is_some() {
        return Err(AppError::AccountAlreadyLinked);
    }

    // Find provider config from app
    let app_provider = entity::app_provider::Entity::find()
        .filter(entity::app_provider::Column::ProviderId.eq(&provider_id))
        .one(&state.db)
        .await?
        .ok_or(AppError::ProviderNotConfigured)?;

    let config: serde_json::Value =
        serde_json::from_str(&app_provider.config).unwrap_or_default();

    let provider = providers::create_provider(&provider_id, &config)?;
    let provider_info = provider.authenticate(&req.credential).await?;

    // Check if this provider account is already linked to another user
    let already_linked = entity::account::Entity::find()
        .filter(entity::account::Column::ProviderId.eq(&provider_id))
        .filter(
            entity::account::Column::ProviderAccountId
                .eq(Some(provider_info.provider_account_id.clone())),
        )
        .one(&state.db)
        .await?;

    if already_linked.is_some() {
        return Err(AppError::AccountAlreadyLinked);
    }

    let now = chrono::Utc::now().naive_utc();
    let account = entity::account::ActiveModel {
        id: Set(Uuid::new_v4().to_string()),
        user_id: Set(user.user_id),
        provider_id: Set(provider_id.clone()),
        provider_account_id: Set(Some(provider_info.provider_account_id.clone())),
        credential: Set(None),
        provider_metadata: Set(
            serde_json::to_string(&provider_info.metadata).unwrap_or_default(),
        ),
        created_at: Set(now),
        updated_at: Set(now),
    };

    account.insert(&state.db).await?;

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
    let accounts = entity::account::Entity::find()
        .filter(entity::account::Column::UserId.eq(&user.user_id))
        .all(&state.db)
        .await?;

    if accounts.len() <= 1 {
        return Err(AppError::CannotUnlinkLastAccount);
    }

    let account = accounts
        .into_iter()
        .find(|a| a.provider_id == provider_id)
        .ok_or(AppError::BadRequest("Account not linked".to_string()))?;

    entity::account::Entity::delete_by_id(&account.id)
        .exec(&state.db)
        .await?;

    Ok(Json(serde_json::json!({"status": "unlinked"})))
}
