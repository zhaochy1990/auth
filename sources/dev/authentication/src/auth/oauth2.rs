use chrono::{Duration, Utc};
use rand::Rng;
use sha2::{Digest, Sha256};

use crate::db::models::{AuthorizationCode, RefreshToken};
use crate::db::pool::Db;
use crate::db::queries;
use crate::error::AppError;

/// Generate a cryptographically random authorization code.
pub fn generate_auth_code() -> String {
    let mut rng = rand::thread_rng();
    let bytes: Vec<u8> = (0..64).map(|_| rng.gen()).collect();
    hex::encode(bytes)
}

/// Generate a cryptographically random refresh token.
pub fn generate_refresh_token() -> String {
    let mut rng = rand::thread_rng();
    let bytes: Vec<u8> = (0..32).map(|_| rng.gen()).collect();
    hex::encode(bytes)
}

/// Hash a token with SHA-256 for storage.
pub fn hash_token(token: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(token.as_bytes());
    hex::encode(hasher.finalize())
}

/// Verify a PKCE code_verifier against a code_challenge.
pub fn verify_pkce(code_verifier: &str, code_challenge: &str, code_challenge_method: &str) -> bool {
    match code_challenge_method {
        "S256" => {
            let mut hasher = Sha256::new();
            hasher.update(code_verifier.as_bytes());
            let hash = hasher.finalize();
            let computed =
                base64::Engine::encode(&base64::engine::general_purpose::URL_SAFE_NO_PAD, hash);
            computed == code_challenge
        }
        "plain" => code_verifier == code_challenge,
        _ => false,
    }
}

/// Store an authorization code in the database.
#[allow(clippy::too_many_arguments)]
pub async fn store_auth_code(
    db: &Db,
    code: &str,
    app_id: &str,
    user_id: &str,
    redirect_uri: &str,
    scopes: &[String],
    code_challenge: Option<String>,
    code_challenge_method: Option<String>,
) -> Result<(), AppError> {
    let now = Utc::now().naive_utc();
    let expires_at = (Utc::now() + Duration::minutes(10)).naive_utc();

    let ac = AuthorizationCode {
        code: code.to_string(),
        app_id: app_id.to_string(),
        user_id: user_id.to_string(),
        redirect_uri: redirect_uri.to_string(),
        scopes: serde_json::to_string(scopes).unwrap_or_default(),
        code_challenge,
        code_challenge_method,
        expires_at,
        used: false,
        created_at: now,
    };

    queries::auth_codes::insert(db, &ac).await?;
    Ok(())
}

/// Exchange an authorization code for user info. Validates and marks as used.
pub async fn exchange_auth_code(
    db: &Db,
    code: &str,
    app_id: &str,
    redirect_uri: &str,
    code_verifier: Option<&str>,
) -> Result<(String, Vec<String>), AppError> {
    let auth_code = queries::auth_codes::find_by_code(db, code)
        .await?
        .ok_or(AppError::InvalidAuthorizationCode)?;

    if auth_code.used {
        return Err(AppError::InvalidAuthorizationCode);
    }

    if auth_code.app_id != app_id {
        return Err(AppError::InvalidAuthorizationCode);
    }

    if auth_code.redirect_uri != redirect_uri {
        return Err(AppError::InvalidRedirectUri);
    }

    let now = Utc::now().naive_utc();
    if auth_code.expires_at < now {
        return Err(AppError::AuthorizationCodeExpired);
    }

    // Verify PKCE if code_challenge was set
    if let Some(ref challenge) = auth_code.code_challenge {
        let method = auth_code
            .code_challenge_method
            .as_deref()
            .unwrap_or("plain");
        let verifier = code_verifier.ok_or(AppError::InvalidCodeVerifier)?;
        if !verify_pkce(verifier, challenge, method) {
            return Err(AppError::InvalidCodeVerifier);
        }
    }

    // Mark as used
    queries::auth_codes::mark_used(db, code).await?;

    let scopes: Vec<String> = serde_json::from_str(&auth_code.scopes).unwrap_or_default();

    Ok((auth_code.user_id, scopes))
}

/// Store a refresh token in the database.
pub async fn store_refresh_token(
    db: &Db,
    user_id: &str,
    app_id: &str,
    token: &str,
    scopes: &[String],
    device_id: Option<String>,
    expiry_days: i64,
) -> Result<(), AppError> {
    let now = Utc::now().naive_utc();
    let expires_at = (Utc::now() + Duration::days(expiry_days)).naive_utc();

    let rt = RefreshToken {
        id: uuid::Uuid::new_v4().to_string(),
        user_id: user_id.to_string(),
        app_id: app_id.to_string(),
        token_hash: hash_token(token),
        scopes: serde_json::to_string(scopes).unwrap_or_default(),
        device_id,
        expires_at,
        revoked: false,
        created_at: now,
    };

    queries::refresh_tokens::insert(db, &rt).await?;
    Ok(())
}

/// Validate and rotate a refresh token.
pub async fn rotate_refresh_token(
    db: &Db,
    token: &str,
    app_id: &str,
    expiry_days: i64,
) -> Result<(String, String, Vec<String>), AppError> {
    let token_hash = hash_token(token);

    let stored = queries::refresh_tokens::find_by_token_hash(db, &token_hash)
        .await?
        .ok_or(AppError::InvalidToken)?;

    if stored.revoked {
        return Err(AppError::TokenRevoked);
    }

    if stored.app_id != app_id {
        return Err(AppError::InvalidToken);
    }

    let now = Utc::now().naive_utc();
    if stored.expires_at < now {
        return Err(AppError::RefreshTokenExpired);
    }

    // Revoke old token
    queries::refresh_tokens::revoke(db, &stored.id).await?;

    // Issue new refresh token
    let new_token = generate_refresh_token();
    let scopes: Vec<String> = serde_json::from_str(&stored.scopes).unwrap_or_default();

    store_refresh_token(
        db,
        &stored.user_id,
        app_id,
        &new_token,
        &scopes,
        stored.device_id.clone(),
        expiry_days,
    )
    .await?;

    Ok((stored.user_id, new_token, scopes))
}

/// Revoke a refresh token by its raw value.
pub async fn revoke_refresh_token(db: &Db, token: &str) -> Result<(), AppError> {
    let token_hash = hash_token(token);

    let stored = queries::refresh_tokens::find_by_token_hash(db, &token_hash)
        .await?
        .ok_or(AppError::InvalidToken)?;

    queries::refresh_tokens::revoke(db, &stored.id).await?;

    Ok(())
}
