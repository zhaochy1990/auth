use bb8::Pool;
use bb8_tiberius::ConnectionManager;
use tiberius::Config;

pub type Db = Pool<ConnectionManager>;

pub async fn connect(connection_string: &str) -> Result<Db, Box<dyn std::error::Error>> {
    let config = Config::from_ado_string(connection_string)?;
    let mgr = ConnectionManager::new(config);
    let pool = Pool::builder().max_size(5).build(mgr).await?;
    Ok(pool)
}
