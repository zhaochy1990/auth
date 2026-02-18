use axum::{extract::State, Json};
use sea_orm::{ColumnTrait, EntityTrait, QueryFilter};
use serde::{Deserialize, Serialize};

use crate::auth::middleware::AuthenticatedApp;
use crate::auth::oauth2 as oauth2_util;
use crate::auth::password::verify_password;
use crate::error::AppError;
use crate::AppState;

// --- Request / Response types ---

#[derive(Debug, Deserialize)]
pub struct TokenRequest {
    pub grant_type: String,
    // authorization_code flow
    pub code: Option<String>,
    pub redirect_uri: Option<String>,
    pub code_verifier: Option<String>,
    // password flow
    pub username: Option<String>,
    pub password: Option<String>,
    // refresh_token flow
    pub refresh_token: Option<String>,
    // common
    pub scope: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct OAuthTokenResponse {
    pub access_token: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub refresh_token: Option<String>,
    pub token_type: String,
    pub expires_in: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scope: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct RevokeRequest {
    pub token: String,
}

#[derive(Debug, Deserialize)]
pub struct IntrospectRequest {
    pub token: String,
}

#[derive(Debug, Serialize)]
pub struct IntrospectResponse {
    pub active: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sub: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub aud: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub exp: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scope: Option<String>,
}

// --- Handlers ---

pub async fn token(
    auth_app: AuthenticatedApp,
    State(state): State<AppState>,
    Json(req): Json<TokenRequest>,
) -> Result<Json<OAuthTokenResponse>, AppError> {
    match req.grant_type.as_str() {
        "authorization_code" => handle_authorization_code(&state, &auth_app, &req).await,
        "client_credentials" => handle_client_credentials(&state, &auth_app).await,
        "refresh_token" => handle_refresh_token(&state, &auth_app, &req).await,
        "password" => handle_password_grant(&state, &auth_app, &req).await,
        _ => Err(AppError::BadRequest(format!(
            "Unsupported grant_type: {}",
            req.grant_type
        ))),
    }
}

async fn handle_authorization_code(
    state: &AppState,
    auth_app: &AuthenticatedApp,
    req: &TokenRequest,
) -> Result<Json<OAuthTokenResponse>, AppError> {
    let code = req.code.as_deref().ok_or(AppError::BadRequest(
        "Missing 'code' parameter".to_string(),
    ))?;
    let redirect_uri = req.redirect_uri.as_deref().ok_or(AppError::BadRequest(
        "Missing 'redirect_uri' parameter".to_string(),
    ))?;

    let (user_id, scopes) = oauth2_util::exchange_auth_code(
        &state.db,
        code,
        &auth_app.app_id,
        redirect_uri,
        req.code_verifier.as_deref(),
    )
    .await?;

    // Look up user for role
    let user = entity::user::Entity::find_by_id(&user_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::UserNotFound)?;

    if !user.is_active {
        return Err(AppError::Forbidden);
    }

    let access_token = state.jwt.issue_access_token(&user_id, &auth_app.client_id, scopes.clone(), &user.role)?;
    let refresh_token = oauth2_util::generate_refresh_token();

    oauth2_util::store_refresh_token(
        &state.db,
        &user_id,
        &auth_app.app_id,
        &refresh_token,
        &scopes,
        None,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await?;

    Ok(Json(OAuthTokenResponse {
        access_token,
        refresh_token: Some(refresh_token),
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
        scope: Some(scopes.join(" ")),
    }))
}

async fn handle_client_credentials(
    state: &AppState,
    auth_app: &AuthenticatedApp,
) -> Result<Json<OAuthTokenResponse>, AppError> {
    let access_token = state.jwt.issue_app_token(&auth_app.app_id)?;

    Ok(Json(OAuthTokenResponse {
        access_token,
        refresh_token: None,
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
        scope: None,
    }))
}

async fn handle_refresh_token(
    state: &AppState,
    auth_app: &AuthenticatedApp,
    req: &TokenRequest,
) -> Result<Json<OAuthTokenResponse>, AppError> {
    let refresh_token_str = req.refresh_token.as_deref().ok_or(AppError::BadRequest(
        "Missing 'refresh_token' parameter".to_string(),
    ))?;

    let (user_id, new_refresh_token, scopes) = oauth2_util::rotate_refresh_token(
        &state.db,
        refresh_token_str,
        &auth_app.app_id,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await?;

    // Look up user for role
    let user = entity::user::Entity::find_by_id(&user_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::UserNotFound)?;

    if !user.is_active {
        return Err(AppError::Forbidden);
    }

    let access_token = state.jwt.issue_access_token(&user_id, &auth_app.client_id, scopes.clone(), &user.role)?;

    Ok(Json(OAuthTokenResponse {
        access_token,
        refresh_token: Some(new_refresh_token),
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
        scope: Some(scopes.join(" ")),
    }))
}

async fn handle_password_grant(
    state: &AppState,
    auth_app: &AuthenticatedApp,
    req: &TokenRequest,
) -> Result<Json<OAuthTokenResponse>, AppError> {
    let username = req.username.as_deref().ok_or(AppError::BadRequest(
        "Missing 'username' parameter".to_string(),
    ))?;
    let password = req.password.as_deref().ok_or(AppError::BadRequest(
        "Missing 'password' parameter".to_string(),
    ))?;

    // Find user by email
    let user = entity::user::Entity::find()
        .filter(entity::user::Column::Email.eq(username))
        .one(&state.db)
        .await?
        .ok_or(AppError::InvalidCredentials)?;

    // Find password account
    let account = entity::account::Entity::find()
        .filter(entity::account::Column::UserId.eq(&user.id))
        .filter(entity::account::Column::ProviderId.eq("password"))
        .one(&state.db)
        .await?
        .ok_or(AppError::InvalidCredentials)?;

    let credential = account.credential.ok_or(AppError::InvalidCredentials)?;
    if !verify_password(password, &credential)? {
        return Err(AppError::InvalidCredentials);
    }

    // Determine scopes
    let app = entity::application::Entity::find_by_id(&auth_app.app_id)
        .one(&state.db)
        .await?
        .ok_or(AppError::ApplicationNotFound)?;

    let allowed_scopes: Vec<String> =
        serde_json::from_str(&app.allowed_scopes).unwrap_or_default();

    let scopes = if let Some(ref scope_str) = req.scope {
        let requested: Vec<String> = scope_str.split(' ').map(|s| s.to_string()).collect();
        // Filter to only allowed scopes
        requested
            .into_iter()
            .filter(|s| allowed_scopes.contains(s))
            .collect()
    } else {
        allowed_scopes
    };

    if !user.is_active {
        return Err(AppError::Forbidden);
    }

    let access_token = state.jwt.issue_access_token(&user.id, &auth_app.client_id, scopes.clone(), &user.role)?;
    let refresh_token = oauth2_util::generate_refresh_token();

    oauth2_util::store_refresh_token(
        &state.db,
        &user.id,
        &auth_app.app_id,
        &refresh_token,
        &scopes,
        None,
        state.config.jwt_refresh_token_expiry_days,
    )
    .await?;

    Ok(Json(OAuthTokenResponse {
        access_token,
        refresh_token: Some(refresh_token),
        token_type: "Bearer".to_string(),
        expires_in: state.config.jwt_access_token_expiry_secs,
        scope: Some(scopes.join(" ")),
    }))
}

pub async fn revoke(
    _auth_app: AuthenticatedApp,
    State(state): State<AppState>,
    Json(req): Json<RevokeRequest>,
) -> Result<Json<serde_json::Value>, AppError> {
    // Try to revoke as refresh token
    let _ = oauth2_util::revoke_refresh_token(&state.db, &req.token).await;
    // Per RFC 7009, always return 200
    Ok(Json(serde_json::json!({})))
}

pub async fn introspect(
    _auth_app: AuthenticatedApp,
    State(state): State<AppState>,
    Json(req): Json<IntrospectRequest>,
) -> Result<Json<IntrospectResponse>, AppError> {
    // Try to verify as access token
    match state.jwt.verify_access_token(&req.token) {
        Ok(claims) => Ok(Json(IntrospectResponse {
            active: true,
            sub: Some(claims.sub),
            aud: Some(claims.aud),
            exp: Some(claims.exp),
            scope: Some(claims.scopes.join(" ")),
        })),
        Err(_) => Ok(Json(IntrospectResponse {
            active: false,
            sub: None,
            aud: None,
            exp: None,
            scope: None,
        })),
    }
}
