use argon2::{
    password_hash::{rand_core::OsRng, PasswordHash, PasswordHasher, PasswordVerifier, SaltString},
    Argon2,
};
use sha2::{Digest, Sha256};

use crate::error::AppError;

pub fn hash_password(password: &str) -> Result<String, AppError> {
    let salt = SaltString::generate(&mut OsRng);
    let argon2 = Argon2::default();
    let hash = argon2
        .hash_password(password.as_bytes(), &salt)
        .map_err(|e| AppError::Internal(format!("Password hashing error: {e}")))?;
    Ok(hash.to_string())
}

pub fn verify_password(password: &str, hash: &str) -> Result<bool, AppError> {
    let parsed_hash = PasswordHash::new(hash)
        .map_err(|e| AppError::Internal(format!("Invalid password hash: {e}")))?;
    Ok(Argon2::default()
        .verify_password(password.as_bytes(), &parsed_hash)
        .is_ok())
}

/// Hash a client secret using SHA-256. Client secrets are high-entropy random
/// strings, so Argon2's brute-force resistance is unnecessary and its ~100ms
/// cost creates a performance bottleneck on every OAuth2 request.
pub fn hash_client_secret(secret: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(secret.as_bytes());
    format!("sha256:{}", hex::encode(hasher.finalize()))
}

/// Verify a client secret. Supports both SHA-256 (new) and Argon2 (legacy).
pub fn verify_client_secret(secret: &str, hash: &str) -> Result<bool, AppError> {
    if let Some(hex_hash) = hash.strip_prefix("sha256:") {
        let mut hasher = Sha256::new();
        hasher.update(secret.as_bytes());
        let computed = hex::encode(hasher.finalize());
        if computed.len() != hex_hash.len() {
            return Ok(false);
        }
        let result = computed
            .as_bytes()
            .iter()
            .zip(hex_hash.as_bytes().iter())
            .fold(0u8, |acc, (a, b)| acc | (a ^ b));
        Ok(result == 0)
    } else {
        // Legacy Argon2 hash — backwards compatible
        verify_password(secret, hash)
    }
}

/// Validate password complexity requirements.
pub fn validate_password(password: &str) -> Result<(), AppError> {
    if password.len() < 8 {
        return Err(AppError::BadRequest(
            "Password must be at least 8 characters".to_string(),
        ));
    }
    if password.len() > 128 {
        return Err(AppError::BadRequest(
            "Password must not exceed 128 characters".to_string(),
        ));
    }
    if !password.chars().any(|c| c.is_uppercase()) {
        return Err(AppError::BadRequest(
            "Password must contain at least one uppercase letter".to_string(),
        ));
    }
    if !password.chars().any(|c| c.is_lowercase()) {
        return Err(AppError::BadRequest(
            "Password must contain at least one lowercase letter".to_string(),
        ));
    }
    if !password.chars().any(|c| c.is_ascii_digit()) {
        return Err(AppError::BadRequest(
            "Password must contain at least one digit".to_string(),
        ));
    }
    if !password.chars().any(|c| !c.is_alphanumeric()) {
        return Err(AppError::BadRequest(
            "Password must contain at least one special character".to_string(),
        ));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_password_valid() {
        assert!(validate_password("Password1!").is_ok());
        assert!(validate_password("Str0ng!Pass").is_ok());
        assert!(validate_password("Ab1!xxxx").is_ok());
    }

    #[test]
    fn validate_password_too_short() {
        let err = validate_password("Ab1!").unwrap_err().to_string();
        assert!(err.contains("at least 8"));
    }

    #[test]
    fn validate_password_too_long() {
        let long = "A".repeat(100) + "a1!" + &"x".repeat(26);
        let err = validate_password(&long).unwrap_err().to_string();
        assert!(err.contains("not exceed 128"));
    }

    #[test]
    fn validate_password_missing_uppercase() {
        let err = validate_password("password1!").unwrap_err().to_string();
        assert!(err.contains("uppercase"));
    }

    #[test]
    fn validate_password_missing_lowercase() {
        let err = validate_password("PASSWORD1!").unwrap_err().to_string();
        assert!(err.contains("lowercase"));
    }

    #[test]
    fn validate_password_missing_digit() {
        let err = validate_password("Password!!").unwrap_err().to_string();
        assert!(err.contains("digit"));
    }

    #[test]
    fn validate_password_missing_special() {
        let err = validate_password("Password11").unwrap_err().to_string();
        assert!(err.contains("special"));
    }

    #[test]
    fn hash_and_verify_client_secret() {
        let secret = "test_secret_value_12345";
        let hash = hash_client_secret(secret);
        assert!(hash.starts_with("sha256:"));
        assert!(verify_client_secret(secret, &hash).unwrap());
        assert!(!verify_client_secret("wrong_secret", &hash).unwrap());
    }

    #[test]
    fn verify_client_secret_legacy_argon2() {
        let secret = "test_secret";
        let argon2_hash = hash_password(secret).unwrap();
        assert!(argon2_hash.starts_with("$argon2"));
        assert!(verify_client_secret(secret, &argon2_hash).unwrap());
        assert!(!verify_client_secret("wrong", &argon2_hash).unwrap());
    }
}
