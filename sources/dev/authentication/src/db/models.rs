use chrono::NaiveDateTime;
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct InviteCode {
    pub id: String,
    pub code: String,
    pub created_by: String,
    pub created_at: NaiveDateTime,
    pub used_at: Option<NaiveDateTime>,
    pub used_by: Option<String>,
    pub is_revoked: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Application {
    pub id: String,
    pub name: String,
    pub client_id: String,
    pub client_secret_hash: String,
    pub redirect_uris: String,
    pub allowed_scopes: String,
    pub is_active: bool,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AppProvider {
    pub id: String,
    pub app_id: String,
    pub provider_id: String,
    pub config: String,
    pub is_active: bool,
    pub created_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct User {
    pub id: String,
    pub email: Option<String>,
    pub name: Option<String>,
    pub avatar_url: Option<String>,
    pub email_verified: bool,
    pub role: String,
    pub is_active: bool,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Account {
    pub id: String,
    pub user_id: String,
    pub provider_id: String,
    pub provider_account_id: Option<String>,
    pub credential: Option<String>,
    pub provider_metadata: String,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AuthorizationCode {
    pub code: String,
    pub app_id: String,
    pub user_id: String,
    pub redirect_uri: String,
    pub scopes: String,
    pub code_challenge: Option<String>,
    pub code_challenge_method: Option<String>,
    pub expires_at: NaiveDateTime,
    pub used: bool,
    pub created_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct RefreshToken {
    pub id: String,
    pub user_id: String,
    pub app_id: String,
    pub token_hash: String,
    pub scopes: String,
    pub device_id: Option<String>,
    pub expires_at: NaiveDateTime,
    pub revoked: bool,
    pub created_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Team {
    pub id: String,
    pub name: String,
    pub description: Option<String>,
    pub owner_user_id: String,
    pub is_open: bool,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct TeamMembership {
    pub team_id: String,
    pub user_id: String,
    pub role: String, // "owner" | "member"
    pub joined_at: NaiveDateTime,
}
