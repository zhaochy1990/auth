use async_trait::async_trait;
use axum::{
    extract::FromRequestParts,
    http::{header, request::Parts},
};
use base64::Engine;

use crate::auth::jwt::Claims;
use crate::db::queries;
use crate::error::AppError;

/// Extracts the authenticated user from a Bearer token.
#[derive(Debug, Clone)]
pub struct AuthenticatedUser {
    pub user_id: String,
    pub client_id: String,
    pub scopes: Vec<String>,
}

#[async_trait]
impl<S> FromRequestParts<S> for AuthenticatedUser
where
    S: Send + Sync + AsRef<crate::AppState>,
{
    type Rejection = AppError;

    async fn from_request_parts(parts: &mut Parts, state: &S) -> Result<Self, Self::Rejection> {
        let app_state: &crate::AppState = state.as_ref();

        let auth_header = parts
            .headers
            .get(header::AUTHORIZATION)
            .and_then(|v| v.to_str().ok())
            .ok_or(AppError::Unauthorized)?;

        let token = auth_header
            .strip_prefix("Bearer ")
            .ok_or(AppError::Unauthorized)?;

        let claims: Claims = app_state.jwt.verify_access_token(token)?;

        Ok(AuthenticatedUser {
            user_id: claims.sub,
            client_id: claims.aud,
            scopes: claims.scopes,
        })
    }
}

/// Extracts the application from X-Client-Id header.
#[derive(Debug, Clone)]
pub struct ClientApp {
    pub app_id: String,
    pub client_id: String,
    pub allowed_scopes: Vec<String>,
}

#[async_trait]
impl<S> FromRequestParts<S> for ClientApp
where
    S: Send + Sync + AsRef<crate::AppState>,
{
    type Rejection = AppError;

    async fn from_request_parts(parts: &mut Parts, state: &S) -> Result<Self, Self::Rejection> {
        let app_state: &crate::AppState = state.as_ref();

        let client_id = parts
            .headers
            .get("X-Client-Id")
            .and_then(|v| v.to_str().ok())
            .ok_or(AppError::MissingClientId)?
            .to_string();

        let app = queries::applications::find_by_client_id(&app_state.db, &client_id)
            .await?
            .ok_or(AppError::ApplicationNotFound)?;

        if !app.is_active {
            return Err(AppError::ApplicationNotActive);
        }

        let allowed_scopes: Vec<String> =
            serde_json::from_str(&app.allowed_scopes).unwrap_or_default();

        Ok(ClientApp {
            app_id: app.id,
            client_id: app.client_id,
            allowed_scopes,
        })
    }
}

/// Authenticates a client application using client_id + client_secret (Basic auth).
#[derive(Debug, Clone)]
pub struct AuthenticatedApp {
    pub app_id: String,
    pub client_id: String,
}

#[async_trait]
impl<S> FromRequestParts<S> for AuthenticatedApp
where
    S: Send + Sync + AsRef<crate::AppState>,
{
    type Rejection = AppError;

    async fn from_request_parts(parts: &mut Parts, state: &S) -> Result<Self, Self::Rejection> {
        let app_state: &crate::AppState = state.as_ref();

        // Try Basic auth header
        let auth_header = parts
            .headers
            .get(header::AUTHORIZATION)
            .and_then(|v| v.to_str().ok());

        let (client_id, client_secret) = if let Some(header) = auth_header {
            if let Some(encoded) = header.strip_prefix("Basic ") {
                let decoded = base64::engine::general_purpose::STANDARD
                    .decode(encoded)
                    .map_err(|_| AppError::InvalidCredentials)?;
                let decoded_str =
                    String::from_utf8(decoded).map_err(|_| AppError::InvalidCredentials)?;
                let mut split = decoded_str.splitn(2, ':');
                let id = split
                    .next()
                    .ok_or(AppError::InvalidCredentials)?
                    .to_string();
                let secret = split
                    .next()
                    .ok_or(AppError::InvalidCredentials)?
                    .to_string();
                (id, secret)
            } else {
                return Err(AppError::InvalidCredentials);
            }
        } else {
            return Err(AppError::InvalidCredentials);
        };

        let app = queries::applications::find_by_client_id(&app_state.db, &client_id)
            .await?
            .ok_or(AppError::ApplicationNotFound)?;

        if !app.is_active {
            return Err(AppError::ApplicationNotActive);
        }

        // Verify client secret (supports SHA-256 and legacy Argon2)
        if !crate::auth::password::verify_client_secret(&client_secret, &app.client_secret_hash)? {
            return Err(AppError::InvalidCredentials);
        }

        Ok(AuthenticatedApp {
            app_id: app.id,
            client_id: app.client_id,
        })
    }
}

/// Admin auth — requires a Bearer token with admin role.
pub struct AdminAuth;

#[async_trait]
impl<S> FromRequestParts<S> for AdminAuth
where
    S: Send + Sync + AsRef<crate::AppState>,
{
    type Rejection = AppError;

    async fn from_request_parts(parts: &mut Parts, state: &S) -> Result<Self, Self::Rejection> {
        let app_state: &crate::AppState = state.as_ref();

        let auth_header = parts
            .headers
            .get(header::AUTHORIZATION)
            .and_then(|v| v.to_str().ok())
            .ok_or(AppError::Unauthorized)?;

        let token = auth_header
            .strip_prefix("Bearer ")
            .ok_or(AppError::Unauthorized)?;

        let claims = app_state.jwt.verify_access_token(token)?;
        if claims.role != "admin" {
            return Err(AppError::Forbidden);
        }

        Ok(AdminAuth)
    }
}
