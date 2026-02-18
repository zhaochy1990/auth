use tiberius::Row;

use crate::db::models::AuthorizationCode;
use crate::db::pool::Db;
use crate::error::AppError;

fn row_to_auth_code(row: &Row) -> AuthorizationCode {
    AuthorizationCode {
        code: row.get::<&str, _>("code").unwrap_or_default().to_string(),
        app_id: row.get::<&str, _>("app_id").unwrap_or_default().to_string(),
        user_id: row
            .get::<&str, _>("user_id")
            .unwrap_or_default()
            .to_string(),
        redirect_uri: row
            .get::<&str, _>("redirect_uri")
            .unwrap_or_default()
            .to_string(),
        scopes: row.get::<&str, _>("scopes").unwrap_or("[]").to_string(),
        code_challenge: row.get::<&str, _>("code_challenge").map(|s| s.to_string()),
        code_challenge_method: row
            .get::<&str, _>("code_challenge_method")
            .map(|s| s.to_string()),
        expires_at: row
            .get::<chrono::NaiveDateTime, _>("expires_at")
            .unwrap_or_default(),
        used: row.get::<bool, _>("used").unwrap_or_default(),
        created_at: row
            .get::<chrono::NaiveDateTime, _>("created_at")
            .unwrap_or_default(),
    }
}

pub async fn find_by_code(pool: &Db, code: &str) -> Result<Option<AuthorizationCode>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT * FROM authorization_codes WHERE code = @P1",
            &[&code],
        )
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_auth_code))
}

pub async fn insert(pool: &Db, ac: &AuthorizationCode) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "INSERT INTO authorization_codes (code, app_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at, used, created_at) VALUES (@P1, @P2, @P3, @P4, @P5, @P6, @P7, @P8, @P9, @P10)",
        &[&ac.code.as_str(), &ac.app_id.as_str(), &ac.user_id.as_str(), &ac.redirect_uri.as_str(), &ac.scopes.as_str(), &ac.code_challenge.as_deref(), &ac.code_challenge_method.as_deref(), &ac.expires_at, &ac.used, &ac.created_at],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}

pub async fn mark_used(pool: &Db, code: &str) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "UPDATE authorization_codes SET used = 1 WHERE code = @P1",
        &[&code],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}
