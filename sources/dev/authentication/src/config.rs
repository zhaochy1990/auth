use std::env;

#[derive(Clone, Debug)]
pub struct Config {
    pub database_url: String,
    pub jwt_private_key_path: String,
    pub jwt_public_key_path: String,
    pub jwt_issuer: String,
    pub jwt_access_token_expiry_secs: i64,
    pub jwt_refresh_token_expiry_days: i64,
    pub server_host: String,
    pub server_port: u16,
    pub cors_allowed_origins: String,
}

impl Config {
    pub fn from_env() -> Result<Self, env::VarError> {
        Ok(Self {
            database_url: env::var("DATABASE_URL")?,
            jwt_private_key_path: env::var("JWT_PRIVATE_KEY_PATH")
                .unwrap_or_else(|_| "keys/private.pem".to_string()),
            jwt_public_key_path: env::var("JWT_PUBLIC_KEY_PATH")
                .unwrap_or_else(|_| "keys/public.pem".to_string()),
            jwt_issuer: env::var("JWT_ISSUER").unwrap_or_else(|_| "auth-service".to_string()),
            jwt_access_token_expiry_secs: env::var("JWT_ACCESS_TOKEN_EXPIRY_SECS")
                .unwrap_or_else(|_| "3600".to_string())
                .parse()
                .unwrap_or(3600),
            jwt_refresh_token_expiry_days: env::var("JWT_REFRESH_TOKEN_EXPIRY_DAYS")
                .unwrap_or_else(|_| "30".to_string())
                .parse()
                .unwrap_or(30),
            server_host: env::var("SERVER_HOST").unwrap_or_else(|_| "127.0.0.1".to_string()),
            server_port: env::var("SERVER_PORT")
                .unwrap_or_else(|_| "3000".to_string())
                .parse()
                .unwrap_or(3000),
            cors_allowed_origins: env::var("CORS_ALLOWED_ORIGINS")
                .unwrap_or_else(|_| "http://localhost:5173,http://localhost:3000".to_string()),
        })
    }
}
