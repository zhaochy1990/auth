use tiberius::Row;

use crate::db::models::Account;
use crate::db::pool::Db;
use crate::error::AppError;

fn row_to_account(row: &Row) -> Account {
    Account {
        id: row.get::<&str, _>("id").unwrap_or_default().to_string(),
        user_id: row
            .get::<&str, _>("user_id")
            .unwrap_or_default()
            .to_string(),
        provider_id: row
            .get::<&str, _>("provider_id")
            .unwrap_or_default()
            .to_string(),
        provider_account_id: row
            .get::<&str, _>("provider_account_id")
            .map(|s| s.to_string()),
        credential: row.get::<&str, _>("credential").map(|s| s.to_string()),
        provider_metadata: row
            .get::<&str, _>("provider_metadata")
            .unwrap_or("{}")
            .to_string(),
        created_at: row
            .get::<chrono::NaiveDateTime, _>("created_at")
            .unwrap_or_default(),
        updated_at: row
            .get::<chrono::NaiveDateTime, _>("updated_at")
            .unwrap_or_default(),
    }
}

pub async fn find_by_user_and_provider(
    pool: &Db,
    user_id: &str,
    provider_id: &str,
) -> Result<Option<Account>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT * FROM accounts WHERE user_id = @P1 AND provider_id = @P2",
            &[&user_id, &provider_id],
        )
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_account))
}

pub async fn find_by_provider_account(
    pool: &Db,
    provider_id: &str,
    provider_account_id: &str,
) -> Result<Option<Account>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT * FROM accounts WHERE provider_id = @P1 AND provider_account_id = @P2",
            &[&provider_id, &provider_account_id],
        )
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_account))
}

pub async fn find_all_by_user(pool: &Db, user_id: &str) -> Result<Vec<Account>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let rows = conn
        .query("SELECT * FROM accounts WHERE user_id = @P1", &[&user_id])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_first_result()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(rows.iter().map(row_to_account).collect())
}

pub async fn count_by_user(pool: &Db, user_id: &str) -> Result<u64, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT COUNT(*) AS cnt FROM accounts WHERE user_id = @P1",
            &[&user_id],
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

pub async fn insert(pool: &Db, account: &Account) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "INSERT INTO accounts (id, user_id, provider_id, provider_account_id, credential, provider_metadata, created_at, updated_at) VALUES (@P1, @P2, @P3, @P4, @P5, @P6, @P7, @P8)",
        &[&account.id.as_str(), &account.user_id.as_str(), &account.provider_id.as_str(), &account.provider_account_id.as_deref(), &account.credential.as_deref(), &account.provider_metadata.as_str(), &account.created_at, &account.updated_at],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}

pub async fn update(pool: &Db, account: &Account) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "UPDATE accounts SET provider_metadata = @P1, credential = @P2, updated_at = @P3 WHERE id = @P4",
        &[&account.provider_metadata.as_str(), &account.credential.as_deref(), &account.updated_at, &account.id.as_str()],
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
    conn.execute("DELETE FROM accounts WHERE id = @P1", &[&id])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}
