pub mod password;
#[cfg(feature = "test-providers")]
pub mod test_provider;
pub mod wechat;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use crate::error::AppError;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProviderUserInfo {
    pub provider_account_id: String,
    pub email: Option<String>,
    pub name: Option<String>,
    pub avatar_url: Option<String>,
    pub metadata: serde_json::Value,
}

#[async_trait]
pub trait AuthProvider: Send + Sync {
    fn provider_id(&self) -> &str;
    async fn authenticate(
        &self,
        credential: &serde_json::Value,
    ) -> Result<ProviderUserInfo, AppError>;
}

pub fn create_provider(
    provider_id: &str,
    config: &serde_json::Value,
) -> Result<Box<dyn AuthProvider>, AppError> {
    match provider_id {
        "wechat" => Ok(Box::new(wechat::WeChatProvider::from_config(config)?)),
        #[cfg(feature = "test-providers")]
        "test" => Ok(Box::new(test_provider::TestProvider::new(config))),
        _ => Err(AppError::ProviderNotSupported(provider_id.to_string())),
    }
}
