use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde_json::json;

#[derive(Debug, thiserror::Error)]
pub enum AppError {
    #[error("Invalid credentials")]
    InvalidCredentials,

    #[error("User not found")]
    UserNotFound,

    #[error("User already exists")]
    UserAlreadyExists,

    #[error("Application not found")]
    ApplicationNotFound,

    #[error("Application not active")]
    ApplicationNotActive,

    #[error("Provider not supported: {0}")]
    ProviderNotSupported(String),

    #[error("Provider not configured for this application")]
    ProviderNotConfigured,

    #[error("Invalid authorization code")]
    InvalidAuthorizationCode,

    #[error("Authorization code expired")]
    AuthorizationCodeExpired,

    #[error("Invalid redirect URI")]
    InvalidRedirectUri,

    #[error("Invalid PKCE code verifier")]
    InvalidCodeVerifier,

    #[error("Invalid or expired token")]
    InvalidToken,

    #[error("Token revoked")]
    TokenRevoked,

    #[error("Refresh token expired")]
    RefreshTokenExpired,

    #[error("Invalid scope")]
    InvalidScope,

    #[error("Missing X-Client-Id header")]
    MissingClientId,

    #[error("Unauthorized")]
    Unauthorized,

    #[error("Forbidden")]
    Forbidden,

    #[error("User account is disabled")]
    UserDisabled,

    #[error("Account already linked")]
    AccountAlreadyLinked,

    #[error("Cannot unlink last account")]
    CannotUnlinkLastAccount,

    #[error("Bad request: {0}")]
    BadRequest(String),

    #[error("Internal error: {0}")]
    Internal(String),

    #[error("Database error: {0}")]
    Database(#[from] sea_orm::DbErr),

    #[error("JWT error: {0}")]
    Jwt(#[from] jsonwebtoken::errors::Error),

    #[error("HTTP client error: {0}")]
    HttpClient(#[from] reqwest::Error),
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let (status, error_type, message) = match &self {
            AppError::InvalidCredentials => {
                (StatusCode::UNAUTHORIZED, "invalid_credentials", self.to_string())
            }
            AppError::UserNotFound => {
                (StatusCode::NOT_FOUND, "user_not_found", self.to_string())
            }
            AppError::UserAlreadyExists => {
                (StatusCode::CONFLICT, "user_already_exists", self.to_string())
            }
            AppError::ApplicationNotFound => {
                (StatusCode::NOT_FOUND, "application_not_found", self.to_string())
            }
            AppError::ApplicationNotActive => {
                (StatusCode::FORBIDDEN, "application_not_active", self.to_string())
            }
            AppError::ProviderNotSupported(_) => {
                (StatusCode::BAD_REQUEST, "provider_not_supported", self.to_string())
            }
            AppError::ProviderNotConfigured => {
                (StatusCode::BAD_REQUEST, "provider_not_configured", self.to_string())
            }
            AppError::InvalidAuthorizationCode => {
                (StatusCode::BAD_REQUEST, "invalid_authorization_code", self.to_string())
            }
            AppError::AuthorizationCodeExpired => {
                (StatusCode::BAD_REQUEST, "authorization_code_expired", self.to_string())
            }
            AppError::InvalidRedirectUri => {
                (StatusCode::BAD_REQUEST, "invalid_redirect_uri", self.to_string())
            }
            AppError::InvalidCodeVerifier => {
                (StatusCode::BAD_REQUEST, "invalid_code_verifier", self.to_string())
            }
            AppError::InvalidToken => {
                (StatusCode::UNAUTHORIZED, "invalid_token", self.to_string())
            }
            AppError::TokenRevoked => {
                (StatusCode::UNAUTHORIZED, "token_revoked", self.to_string())
            }
            AppError::RefreshTokenExpired => {
                (StatusCode::UNAUTHORIZED, "refresh_token_expired", self.to_string())
            }
            AppError::InvalidScope => {
                (StatusCode::BAD_REQUEST, "invalid_scope", self.to_string())
            }
            AppError::MissingClientId => {
                (StatusCode::BAD_REQUEST, "missing_client_id", self.to_string())
            }
            AppError::Unauthorized => {
                (StatusCode::UNAUTHORIZED, "unauthorized", self.to_string())
            }
            AppError::Forbidden => {
                (StatusCode::FORBIDDEN, "forbidden", self.to_string())
            }
            AppError::UserDisabled => {
                (StatusCode::FORBIDDEN, "user_disabled", self.to_string())
            }
            AppError::AccountAlreadyLinked => {
                (StatusCode::CONFLICT, "account_already_linked", self.to_string())
            }
            AppError::CannotUnlinkLastAccount => {
                (StatusCode::BAD_REQUEST, "cannot_unlink_last_account", self.to_string())
            }
            AppError::BadRequest(msg) => {
                (StatusCode::BAD_REQUEST, "bad_request", msg.clone())
            }
            AppError::Internal(_) => {
                (StatusCode::INTERNAL_SERVER_ERROR, "internal_error", "Internal server error".to_string())
            }
            AppError::Database(e) => {
                tracing::error!("Database error: {e}");
                (StatusCode::INTERNAL_SERVER_ERROR, "internal_error", "Internal server error".to_string())
            }
            AppError::Jwt(_) => {
                (StatusCode::UNAUTHORIZED, "invalid_token", "Invalid token".to_string())
            }
            AppError::HttpClient(e) => {
                tracing::error!("HTTP client error: {e}");
                (StatusCode::BAD_GATEWAY, "provider_error", "External provider error".to_string())
            }
        };

        let body = json!({
            "error": error_type,
            "message": message,
        });

        (status, axum::Json(body)).into_response()
    }
}
