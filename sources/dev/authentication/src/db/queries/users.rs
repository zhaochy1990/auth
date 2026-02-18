use chrono::NaiveDateTime;
use tiberius::Row;

use crate::db::models::User;
use crate::db::pool::Db;
use crate::error::AppError;

fn row_to_user(row: &Row) -> User {
    User {
        id: row.get::<&str, _>("id").unwrap_or_default().to_string(),
        email: row.get::<&str, _>("email").map(|s| s.to_string()),
        name: row.get::<&str, _>("name").map(|s| s.to_string()),
        avatar_url: row.get::<&str, _>("avatar_url").map(|s| s.to_string()),
        email_verified: row.get::<bool, _>("email_verified").unwrap_or_default(),
        role: row.get::<&str, _>("role").unwrap_or("user").to_string(),
        is_active: row.get::<bool, _>("is_active").unwrap_or(true),
        created_at: row
            .get::<NaiveDateTime, _>("created_at")
            .unwrap_or_default(),
        updated_at: row
            .get::<NaiveDateTime, _>("updated_at")
            .unwrap_or_default(),
    }
}

pub async fn find_by_id(pool: &Db, id: &str) -> Result<Option<User>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query("SELECT * FROM users WHERE id = @P1", &[&id])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_user))
}

pub async fn find_by_email(pool: &Db, email: &str) -> Result<Option<User>, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query("SELECT * FROM users WHERE email = @P1", &[&email])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row.as_ref().map(row_to_user))
}

pub async fn insert(pool: &Db, user: &User) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "INSERT INTO users (id, email, name, avatar_url, email_verified, role, is_active, created_at, updated_at) VALUES (@P1, @P2, @P3, @P4, @P5, @P6, @P7, @P8, @P9)",
        &[&user.id.as_str(), &user.email.as_deref(), &user.name.as_deref(), &user.avatar_url.as_deref(), &user.email_verified, &user.role.as_str(), &user.is_active, &user.created_at, &user.updated_at],
    )
    .await
    .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(())
}

pub async fn update(pool: &Db, user: &User) -> Result<(), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    conn.execute(
        "UPDATE users SET email = @P1, name = @P2, avatar_url = @P3, email_verified = @P4, role = @P5, is_active = @P6, updated_at = @P7 WHERE id = @P8",
        &[&user.email.as_deref(), &user.name.as_deref(), &user.avatar_url.as_deref(), &user.email_verified, &user.role.as_str(), &user.is_active, &user.updated_at, &user.id.as_str()],
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
        .query("SELECT COUNT(*) AS cnt FROM users", &[])
        .await
        .map_err(|e| AppError::Database(e.to_string()))?
        .into_row()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    Ok(row
        .map(|r| r.get::<i32, _>("cnt").unwrap_or(0) as u64)
        .unwrap_or(0))
}

pub async fn count_since(pool: &Db, since: NaiveDateTime) -> Result<u64, AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let row = conn
        .query(
            "SELECT COUNT(*) AS cnt FROM users WHERE created_at >= @P1",
            &[&since],
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

pub async fn list_paginated(
    pool: &Db,
    search: Option<&str>,
    offset: u64,
    limit: u64,
) -> Result<(Vec<User>, u64), AppError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|e| AppError::Database(e.to_string()))?;
    let offset_i64 = offset as i64;
    let limit_i64 = limit as i64;

    let (rows, count_row) = if let Some(s) = search {
        if s.is_empty() {
            let rows = conn
                .query(
                    "SELECT * FROM users ORDER BY created_at DESC OFFSET @P1 ROWS FETCH NEXT @P2 ROWS ONLY",
                    &[&offset_i64, &limit_i64],
                )
                .await
                .map_err(|e| AppError::Database(e.to_string()))?
                .into_first_result()
                .await
                .map_err(|e| AppError::Database(e.to_string()))?;
            let count_row = conn
                .query("SELECT COUNT(*) AS cnt FROM users", &[])
                .await
                .map_err(|e| AppError::Database(e.to_string()))?
                .into_row()
                .await
                .map_err(|e| AppError::Database(e.to_string()))?;
            (rows, count_row)
        } else {
            let pattern = format!("%{s}%");
            let rows = conn
                .query(
                    "SELECT * FROM users WHERE email LIKE @P1 OR name LIKE @P1 ORDER BY created_at DESC OFFSET @P2 ROWS FETCH NEXT @P3 ROWS ONLY",
                    &[&pattern.as_str(), &offset_i64, &limit_i64],
                )
                .await
                .map_err(|e| AppError::Database(e.to_string()))?
                .into_first_result()
                .await
                .map_err(|e| AppError::Database(e.to_string()))?;
            let count_row = conn
                .query(
                    "SELECT COUNT(*) AS cnt FROM users WHERE email LIKE @P1 OR name LIKE @P1",
                    &[&pattern.as_str()],
                )
                .await
                .map_err(|e| AppError::Database(e.to_string()))?
                .into_row()
                .await
                .map_err(|e| AppError::Database(e.to_string()))?;
            (rows, count_row)
        }
    } else {
        let rows = conn
            .query(
                "SELECT * FROM users ORDER BY created_at DESC OFFSET @P1 ROWS FETCH NEXT @P2 ROWS ONLY",
                &[&offset_i64, &limit_i64],
            )
            .await
            .map_err(|e| AppError::Database(e.to_string()))?
            .into_first_result()
            .await
            .map_err(|e| AppError::Database(e.to_string()))?;
        let count_row = conn
            .query("SELECT COUNT(*) AS cnt FROM users", &[])
            .await
            .map_err(|e| AppError::Database(e.to_string()))?
            .into_row()
            .await
            .map_err(|e| AppError::Database(e.to_string()))?;
        (rows, count_row)
    };

    let total = count_row
        .map(|r| r.get::<i32, _>("cnt").unwrap_or(0) as u64)
        .unwrap_or(0);
    let users = rows.iter().map(row_to_user).collect();
    Ok((users, total))
}
