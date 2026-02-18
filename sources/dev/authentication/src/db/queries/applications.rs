use tiberius::Row;

use crate::db::models::Application;
use crate::db::pool::Db;
use crate::error::AppError;

fn row_to_application(row: &Row) -> Application {
    Application {
        id: row.get::<&str, _>("id").unwrap_or_default().to_string(),
        name: row.get::<&str, _>("name").unwrap_or_default().to_string(),
        client_id: row
            .get::<&str, _>("client_id")
            .unwrap_or_default()
            .to_string(),
        client_secret_hash: row
            .get::<&str, _>("client_secret_hash")
            .unwrap_or_default()
            .to_string(),
        redirect_uris: row
            .get::<&str, _>("redirect_uris")
            .unwrap_or_default()
            .to_string(),
        allowed_scopes: row
            .get::<&str, _>("allowed_scopes")
            .unwrap_or_default()
            .to_string(),
        is_active: row.get::<bool, _>("is_active").unwrap_or_default(),
        created_at: row
            .get::<chrono::NaiveDateTime, _>("created_at")
            .unwrap_or_default(),
        updated_at: row
            .get::<chrono::NaiveDateTime, _>("updated_at")
            .unwrap_or_default(),
    }
}

pub async fn find_by_id(pool: &Db, id: &str) -> Result<Option<Application>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query("SELECT * FROM applications WHERE id = @P1", &[&id])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_application))
}

pub async fn find_by_client_id(
    pool: &Db,
    client_id: &str,
) -> Result<Option<Application>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT * FROM applications WHERE client_id = @P1",
            &[&client_id],
        )
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_application))
}

pub async fn find_by_name(pool: &Db, name: &str) -> Result<Option<Application>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query("SELECT * FROM applications WHERE name = @P1", &[&name])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_application))
}

pub async fn find_all(pool: &Db) -> Result<Vec<Application>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let rows = conn
        .query("SELECT * FROM applications", &[])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_first_result()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(rows.iter().map(row_to_application).collect())
}

pub async fn insert(pool: &Db, app: &Application) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "INSERT INTO applications (id, name, client_id, client_secret_hash, redirect_uris, allowed_scopes, is_active, created_at, updated_at) VALUES (@P1, @P2, @P3, @P4, @P5, @P6, @P7, @P8, @P9)",
        &[&app.id.as_str(), &app.name.as_str(), &app.client_id.as_str(), &app.client_secret_hash.as_str(), &app.redirect_uris.as_str(), &app.allowed_scopes.as_str(), &app.is_active, &app.created_at, &app.updated_at],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}

pub async fn update(pool: &Db, app: &Application) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "UPDATE applications SET name = @P1, client_secret_hash = @P2, redirect_uris = @P3, allowed_scopes = @P4, is_active = @P5, updated_at = @P6 WHERE id = @P7",
        &[&app.name.as_str(), &app.client_secret_hash.as_str(), &app.redirect_uris.as_str(), &app.allowed_scopes.as_str(), &app.is_active, &app.updated_at, &app.id.as_str()],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}

pub async fn count_all(pool: &Db) -> Result<u64, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query("SELECT COUNT(*) AS cnt FROM applications", &[])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row
        .map(|r| r.get::<i32, _>("cnt").unwrap_or(0) as u64)
        .unwrap_or(0))
}

pub async fn count_active(pool: &Db) -> Result<u64, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT COUNT(*) AS cnt FROM applications WHERE is_active = 1",
            &[],
        )
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row
        .map(|r| r.get::<i32, _>("cnt").unwrap_or(0) as u64)
        .unwrap_or(0))
}
