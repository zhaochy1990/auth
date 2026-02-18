use super::pool::Db;

const SCHEMA: &str = include_str!("../../sql/schema.sql");

pub async fn run(pool: &Db) -> Result<(), Box<dyn std::error::Error>> {
    let mut conn = pool.get().await?;
    // Execute each statement separated by semicolons
    for stmt in SCHEMA.split(';') {
        let stmt = stmt.trim();
        if stmt.is_empty() {
            continue;
        }
        conn.execute(stmt, &[]).await?;
    }
    Ok(())
}
