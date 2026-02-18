use tiberius::Row;

use crate::db::models::AppProvider;
use crate::db::pool::Db;
use crate::error::AppError;

fn row_to_app_provider(row: &Row) -> AppProvider {
    AppProvider {
        id: row.get::<&str, _>("id").unwrap_or_default().to_string(),
        app_id: row.get::<&str, _>("app_id").unwrap_or_default().to_string(),
        provider_id: row
            .get::<&str, _>("provider_id")
            .unwrap_or_default()
            .to_string(),
        config: row.get::<&str, _>("config").unwrap_or("{}").to_string(),
        is_active: row.get::<bool, _>("is_active").unwrap_or_default(),
        created_at: row
            .get::<chrono::NaiveDateTime, _>("created_at")
            .unwrap_or_default(),
    }
}

pub async fn find_by_app_and_provider(
    pool: &Db,
    app_id: &str,
    provider_id: &str,
) -> Result<Option<AppProvider>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT * FROM app_providers WHERE app_id = @P1 AND provider_id = @P2",
            &[&app_id, &provider_id],
        )
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_app_provider))
}

pub async fn find_all_by_app(pool: &Db, app_id: &str) -> Result<Vec<AppProvider>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let rows = conn
        .query("SELECT * FROM app_providers WHERE app_id = @P1", &[&app_id])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_first_result()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(rows.iter().map(row_to_app_provider).collect())
}

pub async fn insert(pool: &Db, ap: &AppProvider) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "INSERT INTO app_providers (id, app_id, provider_id, config, is_active, created_at) VALUES (@P1, @P2, @P3, @P4, @P5, @P6)",
        &[&ap.id.as_str(), &ap.app_id.as_str(), &ap.provider_id.as_str(), &ap.config.as_str(), &ap.is_active, &ap.created_at],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}

pub async fn delete_by_id(pool: &Db, id: &str) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute("DELETE FROM app_providers WHERE id = @P1", &[&id])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}
