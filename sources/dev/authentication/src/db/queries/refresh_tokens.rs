use tiberius::Row;

use crate::db::models::RefreshToken;
use crate::db::pool::Db;
use crate::error::AppError;

fn row_to_refresh_token(row: &Row) -> RefreshToken {
    RefreshToken {
        id: row.get::<&str, _>("id").unwrap_or_default().to_string(),
        user_id: row
            .get::<&str, _>("user_id")
            .unwrap_or_default()
            .to_string(),
        app_id: row.get::<&str, _>("app_id").unwrap_or_default().to_string(),
        token_hash: row
            .get::<&str, _>("token_hash")
            .unwrap_or_default()
            .to_string(),
        scopes: row.get::<&str, _>("scopes").unwrap_or("[]").to_string(),
        device_id: row.get::<&str, _>("device_id").map(|s| s.to_string()),
        expires_at: row
            .get::<chrono::NaiveDateTime, _>("expires_at")
            .unwrap_or_default(),
        revoked: row.get::<bool, _>("revoked").unwrap_or_default(),
        created_at: row
            .get::<chrono::NaiveDateTime, _>("created_at")
            .unwrap_or_default(),
    }
}

pub async fn find_by_token_hash(
    pool: &Db,
    token_hash: &str,
) -> Result<Option<RefreshToken>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT * FROM refresh_tokens WHERE token_hash = @P1",
            &[&token_hash],
        )
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_refresh_token))
}

pub async fn insert(pool: &Db, rt: &RefreshToken) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "INSERT INTO refresh_tokens (id, user_id, app_id, token_hash, scopes, device_id, expires_at, revoked, created_at) VALUES (@P1, @P2, @P3, @P4, @P5, @P6, @P7, @P8, @P9)",
        &[&rt.id.as_str(), &rt.user_id.as_str(), &rt.app_id.as_str(), &rt.token_hash.as_str(), &rt.scopes.as_str(), &rt.device_id.as_deref(), &rt.expires_at, &rt.revoked, &rt.created_at],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}

pub async fn revoke(pool: &Db, id: &str) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "UPDATE refresh_tokens SET revoked = 1 WHERE id = @P1",
        &[&id],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}
