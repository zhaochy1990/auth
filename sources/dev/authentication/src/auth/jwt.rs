use chrono::Utc;
use jsonwebtoken::{decode, encode, Algorithm, DecodingKey, EncodingKey, Header, Validation};
use serde::{Deserialize, Serialize};

use crate::config::Config;
use crate::error::AppError;

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct Claims {
    pub sub: String, // user ID
    pub aud: String, // client_id of the application
    pub iss: String, // issuer
    pub exp: i64,    // expiration
    pub iat: i64,    // issued at
    pub scopes: Vec<String>,
    pub role: String,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct AppClaims {
    pub sub: String, // application ID
    pub iss: String,
    pub exp: i64,
    pub iat: i64,
    pub grant_type: String,
}

#[derive(Clone)]
pub struct JwtManager {
    encoding_key: EncodingKey,
    decoding_key: DecodingKey,
    issuer: String,
    access_token_expiry_secs: i64,
}

impl JwtManager {
    pub fn new(config: &Config) -> Result<Self, AppError> {
        let private_key = std::fs::read(&config.jwt_private_key_path)
            .map_err(|e| AppError::Internal(format!("Failed to read private key: {e}")))?;
        let public_key = std::fs::read(&config.jwt_public_key_path)
            .map_err(|e| AppError::Internal(format!("Failed to read public key: {e}")))?;

        let encoding_key = EncodingKey::from_rsa_pem(&private_key)
            .map_err(|e| AppError::Internal(format!("Invalid private key: {e}")))?;
        let decoding_key = DecodingKey::from_rsa_pem(&public_key)
            .map_err(|e| AppError::Internal(format!("Invalid public key: {e}")))?;

        Ok(Self {
            encoding_key,
            decoding_key,
            issuer: config.jwt_issuer.clone(),
            access_token_expiry_secs: config.jwt_access_token_expiry_secs,
        })
    }

    pub fn issue_access_token(
        &self,
        user_id: &str,
        client_id: &str,
        scopes: Vec<String>,
        role: &str,
    ) -> Result<String, AppError> {
        let now = Utc::now().timestamp();
        let claims = Claims {
            sub: user_id.to_string(),
            aud: client_id.to_string(),
            iss: self.issuer.clone(),
            exp: now + self.access_token_expiry_secs,
            iat: now,
            scopes,
            role: role.to_string(),
        };

        let header = Header::new(Algorithm::RS256);
        encode(&header, &claims, &self.encoding_key).map_err(AppError::Jwt)
    }

    pub fn issue_app_token(&self, app_id: &str) -> Result<String, AppError> {
        let now = Utc::now().timestamp();
        let claims = AppClaims {
            sub: app_id.to_string(),
            iss: self.issuer.clone(),
            exp: now + self.access_token_expiry_secs,
            iat: now,
            grant_type: "client_credentials".to_string(),
        };

        let header = Header::new(Algorithm::RS256);
        encode(&header, &claims, &self.encoding_key).map_err(AppError::Jwt)
    }

    pub fn verify_access_token(&self, token: &str) -> Result<Claims, AppError> {
        let mut validation = Validation::new(Algorithm::RS256);
        validation.set_issuer(&[&self.issuer]);
        validation.set_required_spec_claims(&["sub", "aud", "exp", "iat"]);
        validation.validate_aud = false;

        let token_data = decode::<Claims>(token, &self.decoding_key, &validation)?;
        Ok(token_data.claims)
    }

    pub fn verify_app_token(&self, token: &str) -> Result<AppClaims, AppError> {
        let mut validation = Validation::new(Algorithm::RS256);
        validation.set_issuer(&[&self.issuer]);
        validation.validate_aud = false;

        let token_data = decode::<AppClaims>(token, &self.decoding_key, &validation)?;
        Ok(token_data.claims)
    }
}
