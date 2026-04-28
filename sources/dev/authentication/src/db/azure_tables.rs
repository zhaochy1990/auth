use async_trait::async_trait;
use azure_core::StatusCode;
use azure_data_tables::clients::TableServiceClientBuilder;
use azure_data_tables::operations::InsertEntityResponse;
use azure_data_tables::prelude::*;
use azure_storage::{CloudLocation, ConnectionString};
use chrono::NaiveDateTime;
use futures::StreamExt;
use serde::{Deserialize, Serialize};

use crate::db::models::{
    Account, AppProvider, Application, AuthorizationCode, InviteCode, RefreshToken, Team,
    TeamMembership, User,
};
use crate::db::repository::{
    AccountRepository, AppProviderRepository, ApplicationRepository, AuthCodeRepository,
    InviteCodeRepository, RefreshTokenRepository, Repository, TeamMembershipRepository,
    TeamRepository, UserRepository,
};
use crate::error::AppError;

// ─── Table names (prefixed for shared storage account) ──────────────────────

const TABLE_APPLICATIONS: &str = "authapplications";
const TABLE_USERS: &str = "authusers";
const TABLE_ACCOUNTS: &str = "authaccounts";
const TABLE_APP_PROVIDERS: &str = "authappproviders";
const TABLE_AUTH_CODES: &str = "authauthcodes";
const TABLE_REFRESH_TOKENS: &str = "authrefreshtokens";
const TABLE_INVITE_CODES: &str = "authinvitecodes";
const TABLE_TEAMS: &str = "authteams";
const TABLE_TEAM_MEMBERSHIPS: &str = "authteammemberships";

// ─── DateTime helpers ───────────────────────────────────────────────────────

fn fmt_dt(dt: &NaiveDateTime) -> String {
    dt.format("%Y-%m-%dT%H:%M:%S%.6f").to_string()
}

fn parse_dt(s: &str) -> NaiveDateTime {
    NaiveDateTime::parse_from_str(s, "%Y-%m-%dT%H:%M:%S%.6f")
        .or_else(|_| NaiveDateTime::parse_from_str(s, "%Y-%m-%dT%H:%M:%S%.f"))
        .or_else(|_| NaiveDateTime::parse_from_str(s, "%Y-%m-%dT%H:%M:%S"))
        .or_else(|_| NaiveDateTime::parse_from_str(s, "%Y-%m-%d %H:%M:%S%.f"))
        .or_else(|_| NaiveDateTime::parse_from_str(s, "%Y-%m-%d %H:%M:%S"))
        .unwrap_or_default()
}

// ─── Error helpers ──────────────────────────────────────────────────────────

fn is_not_found(e: &azure_core::Error) -> bool {
    e.as_http_error()
        .map(|h| h.status() == StatusCode::NotFound)
        .unwrap_or(false)
}

fn is_conflict(e: &azure_core::Error) -> bool {
    e.as_http_error()
        .map(|h| h.status() == StatusCode::Conflict)
        .unwrap_or(false)
}

fn db_err(e: impl std::fmt::Display) -> AppError {
    AppError::Database(e.to_string())
}

// ─── Generic table helpers ──────────────────────────────────────────────────

async fn insert_entity<E: Serialize>(
    table: &TableClient,
    entity: &E,
) -> Result<(), azure_core::Error> {
    let _: InsertEntityResponse<serde_json::Value> = table.insert(entity)?.await?;
    Ok(())
}

async fn get_entity<T: serde::de::DeserializeOwned + Send>(
    table: &TableClient,
    pk: &str,
    rk: &str,
) -> Result<Option<T>, AppError> {
    match table
        .partition_key_client(pk)
        .entity_client(rk)
        .get::<T>()
        .await
    {
        Ok(resp) => Ok(Some(resp.entity)),
        Err(e) if is_not_found(&e) => Ok(None),
        Err(e) => Err(db_err(e)),
    }
}

async fn query_entities<T: serde::de::DeserializeOwned + Send + Sync>(
    table: &TableClient,
    filter: &str,
) -> Result<Vec<T>, AppError> {
    let mut stream = table.query().filter(filter.to_string()).into_stream::<T>();
    let mut results = Vec::new();
    while let Some(resp) = stream.next().await {
        let resp = resp.map_err(db_err)?;
        results.extend(resp.entities);
    }
    Ok(results)
}

async fn delete_entity(table: &TableClient, pk: &str, rk: &str) -> Result<(), AppError> {
    match table
        .partition_key_client(pk)
        .entity_client(rk)
        .delete()
        .await
    {
        Ok(_) => Ok(()),
        Err(e) if is_not_found(&e) => Ok(()),
        Err(e) => Err(db_err(e)),
    }
}

async fn upsert_entity<E: Serialize>(
    table: &TableClient,
    pk: &str,
    rk: &str,
    entity: &E,
) -> Result<(), AppError> {
    table
        .partition_key_client(pk)
        .entity_client(rk)
        .insert_or_replace(entity)
        .map_err(db_err)?
        .await
        .map_err(db_err)?;
    Ok(())
}

// ─── Table entity structs ───────────────────────────────────────────────────

#[derive(Debug, Serialize, Deserialize)]
struct AppEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String,
    #[serde(rename = "RowKey")]
    row_key: String,
    name: String,
    client_id: String,
    client_secret_hash: String,
    redirect_uris: String,
    allowed_scopes: String,
    is_active: bool,
    created_at: String,
    updated_at: String,
}

impl AppEntity {
    fn from_model(a: &Application) -> Self {
        Self {
            partition_key: "app".into(),
            row_key: a.id.clone(),
            name: a.name.clone(),
            client_id: a.client_id.clone(),
            client_secret_hash: a.client_secret_hash.clone(),
            redirect_uris: a.redirect_uris.clone(),
            allowed_scopes: a.allowed_scopes.clone(),
            is_active: a.is_active,
            created_at: fmt_dt(&a.created_at),
            updated_at: fmt_dt(&a.updated_at),
        }
    }
    fn to_model(&self) -> Application {
        Application {
            id: self.row_key.clone(),
            name: self.name.clone(),
            client_id: self.client_id.clone(),
            client_secret_hash: self.client_secret_hash.clone(),
            redirect_uris: self.redirect_uris.clone(),
            allowed_scopes: self.allowed_scopes.clone(),
            is_active: self.is_active,
            created_at: parse_dt(&self.created_at),
            updated_at: parse_dt(&self.updated_at),
        }
    }
}

#[derive(Debug, Serialize, Deserialize)]
struct UserEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String,
    #[serde(rename = "RowKey")]
    row_key: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    email: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    avatar_url: Option<String>,
    #[serde(default)]
    email_verified: bool,
    #[serde(default = "default_role")]
    role: String,
    #[serde(default = "default_true")]
    is_active: bool,
    #[serde(default)]
    created_at: String,
    #[serde(default)]
    updated_at: String,
}

fn default_role() -> String {
    "user".into()
}
fn default_true() -> bool {
    true
}

impl UserEntity {
    fn from_model(u: &User) -> Self {
        Self {
            partition_key: "user".into(),
            row_key: u.id.clone(),
            email: u.email.clone(),
            name: u.name.clone(),
            avatar_url: u.avatar_url.clone(),
            email_verified: u.email_verified,
            role: u.role.clone(),
            is_active: u.is_active,
            created_at: fmt_dt(&u.created_at),
            updated_at: fmt_dt(&u.updated_at),
        }
    }
    fn to_model(&self) -> User {
        User {
            id: self.row_key.clone(),
            email: self.email.clone(),
            name: self.name.clone(),
            avatar_url: self.avatar_url.clone(),
            email_verified: self.email_verified,
            role: self.role.clone(),
            is_active: self.is_active,
            created_at: parse_dt(&self.created_at),
            updated_at: parse_dt(&self.updated_at),
        }
    }
}

#[derive(Debug, Serialize, Deserialize)]
struct AccountEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String, // user_id
    #[serde(rename = "RowKey")]
    row_key: String, // provider_id
    id: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    provider_account_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    credential: Option<String>,
    #[serde(default = "default_json_obj")]
    provider_metadata: String,
    #[serde(default)]
    created_at: String,
    #[serde(default)]
    updated_at: String,
}

fn default_json_obj() -> String {
    "{}".into()
}

impl AccountEntity {
    fn from_model(a: &Account) -> Self {
        Self {
            partition_key: a.user_id.clone(),
            row_key: a.provider_id.clone(),
            id: a.id.clone(),
            provider_account_id: a.provider_account_id.clone(),
            credential: a.credential.clone(),
            provider_metadata: a.provider_metadata.clone(),
            created_at: fmt_dt(&a.created_at),
            updated_at: fmt_dt(&a.updated_at),
        }
    }
    fn to_model(&self) -> Account {
        Account {
            id: self.id.clone(),
            user_id: self.partition_key.clone(),
            provider_id: self.row_key.clone(),
            provider_account_id: self.provider_account_id.clone(),
            credential: self.credential.clone(),
            provider_metadata: self.provider_metadata.clone(),
            created_at: parse_dt(&self.created_at),
            updated_at: parse_dt(&self.updated_at),
        }
    }
}

#[derive(Debug, Serialize, Deserialize)]
struct AppProviderEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String, // app_id
    #[serde(rename = "RowKey")]
    row_key: String, // provider_id
    id: String,
    #[serde(default = "default_json_obj")]
    config: String,
    #[serde(default)]
    is_active: bool,
    #[serde(default)]
    created_at: String,
}

impl AppProviderEntity {
    fn from_model(p: &AppProvider) -> Self {
        Self {
            partition_key: p.app_id.clone(),
            row_key: p.provider_id.clone(),
            id: p.id.clone(),
            config: p.config.clone(),
            is_active: p.is_active,
            created_at: fmt_dt(&p.created_at),
        }
    }
    fn to_model(&self) -> AppProvider {
        AppProvider {
            id: self.id.clone(),
            app_id: self.partition_key.clone(),
            provider_id: self.row_key.clone(),
            config: self.config.clone(),
            is_active: self.is_active,
            created_at: parse_dt(&self.created_at),
        }
    }
}

#[derive(Debug, Serialize, Deserialize)]
struct AuthCodeEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String, // "code"
    #[serde(rename = "RowKey")]
    row_key: String, // the code value
    app_id: String,
    user_id: String,
    redirect_uri: String,
    #[serde(default = "default_json_arr")]
    scopes: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    code_challenge: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    code_challenge_method: Option<String>,
    #[serde(default)]
    expires_at: String,
    #[serde(default)]
    used: bool,
    #[serde(default)]
    created_at: String,
}

fn default_json_arr() -> String {
    "[]".into()
}

impl AuthCodeEntity {
    fn from_model(c: &AuthorizationCode) -> Self {
        Self {
            partition_key: "code".into(),
            row_key: c.code.clone(),
            app_id: c.app_id.clone(),
            user_id: c.user_id.clone(),
            redirect_uri: c.redirect_uri.clone(),
            scopes: c.scopes.clone(),
            code_challenge: c.code_challenge.clone(),
            code_challenge_method: c.code_challenge_method.clone(),
            expires_at: fmt_dt(&c.expires_at),
            used: c.used,
            created_at: fmt_dt(&c.created_at),
        }
    }
    fn to_model(&self) -> AuthorizationCode {
        AuthorizationCode {
            code: self.row_key.clone(),
            app_id: self.app_id.clone(),
            user_id: self.user_id.clone(),
            redirect_uri: self.redirect_uri.clone(),
            scopes: self.scopes.clone(),
            code_challenge: self.code_challenge.clone(),
            code_challenge_method: self.code_challenge_method.clone(),
            expires_at: parse_dt(&self.expires_at),
            used: self.used,
            created_at: parse_dt(&self.created_at),
        }
    }
}

#[derive(Debug, Serialize, Deserialize)]
struct RefreshTokenEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String, // "rt"
    #[serde(rename = "RowKey")]
    row_key: String, // id
    user_id: String,
    app_id: String,
    token_hash: String,
    #[serde(default = "default_json_arr")]
    scopes: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    device_id: Option<String>,
    #[serde(default)]
    expires_at: String,
    #[serde(default)]
    revoked: bool,
    #[serde(default)]
    created_at: String,
}

impl RefreshTokenEntity {
    fn from_model(t: &RefreshToken) -> Self {
        Self {
            partition_key: "rt".into(),
            row_key: t.id.clone(),
            user_id: t.user_id.clone(),
            app_id: t.app_id.clone(),
            token_hash: t.token_hash.clone(),
            scopes: t.scopes.clone(),
            device_id: t.device_id.clone(),
            expires_at: fmt_dt(&t.expires_at),
            revoked: t.revoked,
            created_at: fmt_dt(&t.created_at),
        }
    }
    fn to_model(&self) -> RefreshToken {
        RefreshToken {
            id: self.row_key.clone(),
            user_id: self.user_id.clone(),
            app_id: self.app_id.clone(),
            token_hash: self.token_hash.clone(),
            scopes: self.scopes.clone(),
            device_id: self.device_id.clone(),
            expires_at: parse_dt(&self.expires_at),
            revoked: self.revoked,
            created_at: parse_dt(&self.created_at),
        }
    }
}

/// Lightweight index entity pointing to a primary entity.
#[derive(Debug, Serialize, Deserialize)]
struct IndexEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String,
    #[serde(rename = "RowKey")]
    row_key: String,
    #[serde(default)]
    target_id: String,
}

/// Index entity for composite-key lookups (accounts, app_providers).
#[derive(Debug, Serialize, Deserialize)]
struct CompositeIndexEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String,
    #[serde(rename = "RowKey")]
    row_key: String,
    #[serde(default)]
    pk: String,
    #[serde(default)]
    rk: String,
}

// ─── InviteCodeEntity ───────────────────────────────────────────────────────
// PK = "invite_code", RK = code value

#[derive(Debug, Serialize, Deserialize)]
struct InviteCodeEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String,
    #[serde(rename = "RowKey")]
    row_key: String,
    id: String,
    created_by: String,
    #[serde(default)]
    created_at: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    used_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    used_by: Option<String>,
    #[serde(default)]
    is_revoked: bool,
}

impl InviteCodeEntity {
    fn from_model(c: &InviteCode) -> Self {
        Self {
            partition_key: "invite_code".into(),
            row_key: c.code.clone(),
            id: c.id.clone(),
            created_by: c.created_by.clone(),
            created_at: fmt_dt(&c.created_at),
            used_at: c.used_at.as_ref().map(fmt_dt),
            used_by: c.used_by.clone(),
            is_revoked: c.is_revoked,
        }
    }
    fn to_model(&self) -> InviteCode {
        InviteCode {
            id: self.id.clone(),
            code: self.row_key.clone(),
            created_by: self.created_by.clone(),
            created_at: parse_dt(&self.created_at),
            used_at: self.used_at.as_deref().map(parse_dt),
            used_by: self.used_by.clone(),
            is_revoked: self.is_revoked,
        }
    }
}

// ─── TeamEntity ─────────────────────────────────────────────────────────────
// PK = "team", RK = team_id

#[derive(Debug, Serialize, Deserialize)]
struct TeamEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String,
    #[serde(rename = "RowKey")]
    row_key: String,
    name: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    description: Option<String>,
    owner_user_id: String,
    #[serde(default = "default_true")]
    is_open: bool,
    #[serde(default)]
    created_at: String,
    #[serde(default)]
    updated_at: String,
}

impl TeamEntity {
    fn from_model(t: &Team) -> Self {
        Self {
            partition_key: "team".into(),
            row_key: t.id.clone(),
            name: t.name.clone(),
            description: t.description.clone(),
            owner_user_id: t.owner_user_id.clone(),
            is_open: t.is_open,
            created_at: fmt_dt(&t.created_at),
            updated_at: fmt_dt(&t.updated_at),
        }
    }
    fn to_model(&self) -> Team {
        Team {
            id: self.row_key.clone(),
            name: self.name.clone(),
            description: self.description.clone(),
            owner_user_id: self.owner_user_id.clone(),
            is_open: self.is_open,
            created_at: parse_dt(&self.created_at),
            updated_at: parse_dt(&self.updated_at),
        }
    }
}

// ─── TeamMembershipEntity ───────────────────────────────────────────────────
// PK = team_id, RK = user_id

#[derive(Debug, Serialize, Deserialize)]
struct TeamMembershipEntity {
    #[serde(rename = "PartitionKey")]
    partition_key: String, // team_id
    #[serde(rename = "RowKey")]
    row_key: String, // user_id
    role: String,
    #[serde(default)]
    joined_at: String,
}

impl TeamMembershipEntity {
    fn from_model(m: &TeamMembership) -> Self {
        Self {
            partition_key: m.team_id.clone(),
            row_key: m.user_id.clone(),
            role: m.role.clone(),
            joined_at: fmt_dt(&m.joined_at),
        }
    }
    fn to_model(&self) -> TeamMembership {
        TeamMembership {
            team_id: self.partition_key.clone(),
            user_id: self.row_key.clone(),
            role: self.role.clone(),
            joined_at: parse_dt(&self.joined_at),
        }
    }
}

// ─── AzureTableRepository ───────────────────────────────────────────────────

pub struct AzureTableRepository {
    applications: TableClient,
    users: TableClient,
    accounts: TableClient,
    app_providers: TableClient,
    auth_codes: TableClient,
    refresh_tokens: TableClient,
    invite_codes: TableClient,
    teams: TableClient,
    team_memberships: TableClient,
}

impl AzureTableRepository {
    pub fn new(connection_string: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let cs = ConnectionString::new(connection_string)?;
        let account = cs
            .account_name
            .ok_or("Missing AccountName in connection string")?;
        let creds = cs.storage_credentials()?;

        // Detect emulator/custom endpoint from TableEndpoint in connection string
        let svc = if let Some(table_endpoint) = cs.table_endpoint {
            // Custom endpoint (Azurite or other non-standard)
            let location = if account == "devstoreaccount1" {
                // Parse host:port from the endpoint URL for Azurite
                let url = azure_core::Url::parse(table_endpoint)?;
                let address = url.host_str().unwrap_or("127.0.0.1").to_string();
                let port = url.port().unwrap_or(10002);
                CloudLocation::Emulator { address, port }
            } else {
                CloudLocation::Custom {
                    account: account.to_string(),
                    uri: table_endpoint.to_string(),
                }
            };
            TableServiceClientBuilder::with_location(location, creds).build()
        } else {
            TableServiceClient::new(account, creds)
        };
        Ok(Self {
            applications: svc.table_client(TABLE_APPLICATIONS),
            users: svc.table_client(TABLE_USERS),
            accounts: svc.table_client(TABLE_ACCOUNTS),
            app_providers: svc.table_client(TABLE_APP_PROVIDERS),
            auth_codes: svc.table_client(TABLE_AUTH_CODES),
            refresh_tokens: svc.table_client(TABLE_REFRESH_TOKENS),
            invite_codes: svc.table_client(TABLE_INVITE_CODES),
            teams: svc.table_client(TABLE_TEAMS),
            team_memberships: svc.table_client(TABLE_TEAM_MEMBERSHIPS),
        })
    }

    pub async fn ensure_tables(&self) -> Result<(), Box<dyn std::error::Error>> {
        for table in self.all_tables() {
            match table.create().await {
                Ok(_) => {}
                Err(e) if is_conflict(&e) => {} // already exists
                Err(e) => return Err(e.into()),
            }
        }
        Ok(())
    }

    /// Delete and recreate all tables. Used for test isolation with Azurite.
    pub async fn clear_all_tables(&self) -> Result<(), Box<dyn std::error::Error>> {
        for table in self.all_tables() {
            let _ = table.delete().await; // ignore errors if table doesn't exist
        }
        self.ensure_tables().await
    }

    fn all_tables(&self) -> [&TableClient; 9] {
        [
            &self.applications,
            &self.users,
            &self.accounts,
            &self.app_providers,
            &self.auth_codes,
            &self.refresh_tokens,
            &self.invite_codes,
            &self.teams,
            &self.team_memberships,
        ]
    }
}

// ─── Repository trait ───────────────────────────────────────────────────────

impl Repository for AzureTableRepository {
    fn users(&self) -> &dyn UserRepository {
        self
    }
    fn applications(&self) -> &dyn ApplicationRepository {
        self
    }
    fn accounts(&self) -> &dyn AccountRepository {
        self
    }
    fn app_providers(&self) -> &dyn AppProviderRepository {
        self
    }
    fn auth_codes(&self) -> &dyn AuthCodeRepository {
        self
    }
    fn refresh_tokens(&self) -> &dyn RefreshTokenRepository {
        self
    }
    fn invite_codes(&self) -> &dyn InviteCodeRepository {
        self
    }
    fn teams(&self) -> &dyn TeamRepository {
        self
    }
    fn team_memberships(&self) -> &dyn TeamMembershipRepository {
        self
    }
}

// ─── UserRepository ─────────────────────────────────────────────────────────

#[async_trait]
impl UserRepository for AzureTableRepository {
    async fn find_by_id(&self, id: &str) -> Result<Option<User>, AppError> {
        let entity: Option<UserEntity> = get_entity(&self.users, "user", id).await?;
        Ok(entity.map(|e| e.to_model()))
    }

    async fn find_by_email(&self, email: &str) -> Result<Option<User>, AppError> {
        let idx: Option<IndexEntity> =
            get_entity(&self.users, "idx_email", &email.to_lowercase()).await?;
        match idx {
            Some(idx) => UserRepository::find_by_id(self, &idx.target_id).await,
            None => Ok(None),
        }
    }

    async fn insert(&self, user: &User) -> Result<(), AppError> {
        // Insert email index first for uniqueness check
        if let Some(ref email) = user.email {
            let idx = IndexEntity {
                partition_key: "idx_email".into(),
                row_key: email.to_lowercase(),
                target_id: user.id.clone(),
            };
            insert_entity(&self.users, &idx).await.map_err(|e| {
                if is_conflict(&e) {
                    AppError::Database("Email already exists".into())
                } else {
                    db_err(e)
                }
            })?;
        }

        let entity = UserEntity::from_model(user);
        insert_entity(&self.users, &entity).await.map_err(db_err)?;
        Ok(())
    }

    async fn update(&self, user: &User) -> Result<(), AppError> {
        // Check if email changed and update index
        let current: Option<UserEntity> = get_entity(&self.users, "user", &user.id).await?;
        if let Some(ref current) = current {
            let old_email = current.email.as_deref().map(str::to_lowercase);
            let new_email = user.email.as_deref().map(str::to_lowercase);
            if old_email != new_email {
                if let Some(ref old) = old_email {
                    delete_entity(&self.users, "idx_email", old).await?;
                }
                if let Some(ref new) = new_email {
                    let idx = IndexEntity {
                        partition_key: "idx_email".into(),
                        row_key: new.clone(),
                        target_id: user.id.clone(),
                    };
                    // Use upsert for the new index to avoid conflict if re-setting same email
                    upsert_entity(&self.users, "idx_email", new, &idx).await?;
                }
            }
        }

        let entity = UserEntity::from_model(user);
        upsert_entity(&self.users, "user", &user.id, &entity).await
    }

    async fn delete_by_id(&self, id: &str) -> Result<(), AppError> {
        // Remove email index first
        let entity: Option<UserEntity> = get_entity(&self.users, "user", id).await?;
        if let Some(ref e) = entity {
            if let Some(ref email) = e.email {
                delete_entity(&self.users, "idx_email", &email.to_lowercase()).await?;
            }
        }
        delete_entity(&self.users, "user", id).await
    }

    async fn count_all(&self) -> Result<u64, AppError> {
        let entities: Vec<UserEntity> =
            query_entities(&self.users, "PartitionKey eq 'user'").await?;
        Ok(entities.len() as u64)
    }

    async fn count_since(&self, since: NaiveDateTime) -> Result<u64, AppError> {
        let entities: Vec<UserEntity> =
            query_entities(&self.users, "PartitionKey eq 'user'").await?;
        let since_str = fmt_dt(&since);
        Ok(entities
            .iter()
            .filter(|e| e.created_at >= since_str)
            .count() as u64)
    }

    async fn list_paginated(
        &self,
        search: Option<&str>,
        offset: u64,
        limit: u64,
    ) -> Result<(Vec<User>, u64), AppError> {
        let mut entities: Vec<UserEntity> =
            query_entities(&self.users, "PartitionKey eq 'user'").await?;

        // Client-side search filter
        if let Some(s) = search {
            if !s.is_empty() {
                let lower = s.to_lowercase();
                entities.retain(|e| {
                    e.email
                        .as_deref()
                        .map(|v| v.to_lowercase().contains(&lower))
                        .unwrap_or(false)
                        || e.name
                            .as_deref()
                            .map(|v| v.to_lowercase().contains(&lower))
                            .unwrap_or(false)
                });
            }
        }

        let total = entities.len() as u64;

        // Sort by created_at DESC
        entities.sort_by(|a, b| b.created_at.cmp(&a.created_at));

        // Paginate
        let users: Vec<User> = entities
            .into_iter()
            .skip(offset as usize)
            .take(limit as usize)
            .map(|e| e.to_model())
            .collect();

        Ok((users, total))
    }
}

// ─── ApplicationRepository ──────────────────────────────────────────────────

#[async_trait]
impl ApplicationRepository for AzureTableRepository {
    async fn find_by_id(&self, id: &str) -> Result<Option<Application>, AppError> {
        let entity: Option<AppEntity> = get_entity(&self.applications, "app", id).await?;
        Ok(entity.map(|e| e.to_model()))
    }

    async fn find_by_client_id(&self, client_id: &str) -> Result<Option<Application>, AppError> {
        let idx: Option<IndexEntity> =
            get_entity(&self.applications, "idx_clientid", client_id).await?;
        match idx {
            Some(idx) => ApplicationRepository::find_by_id(self, &idx.target_id).await,
            None => Ok(None),
        }
    }

    async fn find_by_name(&self, name: &str) -> Result<Option<Application>, AppError> {
        let idx: Option<IndexEntity> = get_entity(&self.applications, "idx_name", name).await?;
        match idx {
            Some(idx) => ApplicationRepository::find_by_id(self, &idx.target_id).await,
            None => Ok(None),
        }
    }

    async fn find_all(&self) -> Result<Vec<Application>, AppError> {
        let entities: Vec<AppEntity> =
            query_entities(&self.applications, "PartitionKey eq 'app'").await?;
        Ok(entities.iter().map(|e| e.to_model()).collect())
    }

    async fn insert(&self, app: &Application) -> Result<(), AppError> {
        // Insert client_id index for uniqueness
        let cid_idx = IndexEntity {
            partition_key: "idx_clientid".into(),
            row_key: app.client_id.clone(),
            target_id: app.id.clone(),
        };
        insert_entity(&self.applications, &cid_idx)
            .await
            .map_err(|e| {
                if is_conflict(&e) {
                    AppError::Database("Client ID already exists".into())
                } else {
                    db_err(e)
                }
            })?;

        // Insert name index
        let name_idx = IndexEntity {
            partition_key: "idx_name".into(),
            row_key: app.name.clone(),
            target_id: app.id.clone(),
        };
        let _ = insert_entity(&self.applications, &name_idx).await; // best-effort

        // Insert primary entity
        let entity = AppEntity::from_model(app);
        insert_entity(&self.applications, &entity)
            .await
            .map_err(db_err)?;
        Ok(())
    }

    async fn update(&self, app: &Application) -> Result<(), AppError> {
        // Handle name index changes
        let current: Option<AppEntity> = get_entity(&self.applications, "app", &app.id).await?;
        if let Some(ref current) = current {
            if current.name != app.name {
                delete_entity(&self.applications, "idx_name", &current.name).await?;
                let idx = IndexEntity {
                    partition_key: "idx_name".into(),
                    row_key: app.name.clone(),
                    target_id: app.id.clone(),
                };
                upsert_entity(&self.applications, "idx_name", &app.name, &idx).await?;
            }
        }

        let entity = AppEntity::from_model(app);
        upsert_entity(&self.applications, "app", &app.id, &entity).await
    }

    async fn count_all(&self) -> Result<u64, AppError> {
        let entities: Vec<AppEntity> =
            query_entities(&self.applications, "PartitionKey eq 'app'").await?;
        Ok(entities.len() as u64)
    }

    async fn count_active(&self) -> Result<u64, AppError> {
        let entities: Vec<AppEntity> =
            query_entities(&self.applications, "PartitionKey eq 'app'").await?;
        Ok(entities.iter().filter(|e| e.is_active).count() as u64)
    }
}

// ─── AccountRepository ──────────────────────────────────────────────────────

#[async_trait]
impl AccountRepository for AzureTableRepository {
    async fn find_by_user_and_provider(
        &self,
        user_id: &str,
        provider_id: &str,
    ) -> Result<Option<Account>, AppError> {
        let entity: Option<AccountEntity> =
            get_entity(&self.accounts, user_id, provider_id).await?;
        Ok(entity.map(|e| e.to_model()))
    }

    async fn find_by_provider_account(
        &self,
        provider_id: &str,
        provider_account_id: &str,
    ) -> Result<Option<Account>, AppError> {
        let pk = format!("idx_pa#{provider_id}");
        let idx: Option<CompositeIndexEntity> =
            get_entity(&self.accounts, &pk, provider_account_id).await?;
        match idx {
            Some(idx) => self.find_by_user_and_provider(&idx.pk, &idx.rk).await,
            None => Ok(None),
        }
    }

    async fn find_all_by_user(&self, user_id: &str) -> Result<Vec<Account>, AppError> {
        let filter = format!("PartitionKey eq '{user_id}'");
        let entities: Vec<AccountEntity> = query_entities(&self.accounts, &filter).await?;
        Ok(entities.iter().map(|e| e.to_model()).collect())
    }

    async fn count_by_user(&self, user_id: &str) -> Result<u64, AppError> {
        let accounts = AccountRepository::find_all_by_user(self, user_id).await?;
        Ok(accounts.len() as u64)
    }

    async fn insert(&self, account: &Account) -> Result<(), AppError> {
        // Insert provider-account index if provider_account_id exists
        if let Some(ref pa_id) = account.provider_account_id {
            let pk = format!("idx_pa#{}", account.provider_id);
            let idx = CompositeIndexEntity {
                partition_key: pk.clone(),
                row_key: pa_id.clone(),
                pk: account.user_id.clone(),
                rk: account.provider_id.clone(),
            };
            let _ = insert_entity(&self.accounts, &idx).await; // best-effort
        }

        // Insert ID index
        let id_idx = CompositeIndexEntity {
            partition_key: "idx_id".into(),
            row_key: account.id.clone(),
            pk: account.user_id.clone(),
            rk: account.provider_id.clone(),
        };
        let _ = insert_entity(&self.accounts, &id_idx).await;

        // Insert primary entity
        let entity = AccountEntity::from_model(account);
        insert_entity(&self.accounts, &entity).await.map_err(|e| {
            if is_conflict(&e) {
                AppError::Database("Account already exists".into())
            } else {
                db_err(e)
            }
        })?;
        Ok(())
    }

    async fn update(&self, account: &Account) -> Result<(), AppError> {
        let entity = AccountEntity::from_model(account);
        upsert_entity(
            &self.accounts,
            &account.user_id,
            &account.provider_id,
            &entity,
        )
        .await
    }

    async fn delete_by_id(&self, id: &str) -> Result<(), AppError> {
        // Look up composite key from ID index
        let idx: Option<CompositeIndexEntity> = get_entity(&self.accounts, "idx_id", id).await?;
        let Some(idx) = idx else { return Ok(()) };

        // Get full entity for provider_account_id cleanup
        let entity: Option<AccountEntity> = get_entity(&self.accounts, &idx.pk, &idx.rk).await?;

        // Delete provider-account index if exists
        if let Some(ref entity) = entity {
            if let Some(ref pa_id) = entity.provider_account_id {
                let pk = format!("idx_pa#{}", idx.rk);
                delete_entity(&self.accounts, &pk, pa_id).await?;
            }
        }

        // Delete primary entity and ID index
        delete_entity(&self.accounts, &idx.pk, &idx.rk).await?;
        delete_entity(&self.accounts, "idx_id", id).await?;
        Ok(())
    }
}

// ─── AppProviderRepository ──────────────────────────────────────────────────

#[async_trait]
impl AppProviderRepository for AzureTableRepository {
    async fn find_by_app_and_provider(
        &self,
        app_id: &str,
        provider_id: &str,
    ) -> Result<Option<AppProvider>, AppError> {
        let entity: Option<AppProviderEntity> =
            get_entity(&self.app_providers, app_id, provider_id).await?;
        Ok(entity.map(|e| e.to_model()))
    }

    async fn find_all_by_app(&self, app_id: &str) -> Result<Vec<AppProvider>, AppError> {
        let filter = format!("PartitionKey eq '{app_id}'");
        let entities: Vec<AppProviderEntity> = query_entities(&self.app_providers, &filter).await?;
        Ok(entities.iter().map(|e| e.to_model()).collect())
    }

    async fn insert(&self, ap: &AppProvider) -> Result<(), AppError> {
        // Insert ID index
        let id_idx = CompositeIndexEntity {
            partition_key: "idx_id".into(),
            row_key: ap.id.clone(),
            pk: ap.app_id.clone(),
            rk: ap.provider_id.clone(),
        };
        let _ = insert_entity(&self.app_providers, &id_idx).await;

        // Insert primary entity
        let entity = AppProviderEntity::from_model(ap);
        insert_entity(&self.app_providers, &entity)
            .await
            .map_err(|e| {
                if is_conflict(&e) {
                    AppError::Database("Provider already configured".into())
                } else {
                    db_err(e)
                }
            })?;
        Ok(())
    }

    async fn delete_by_id(&self, id: &str) -> Result<(), AppError> {
        let idx: Option<CompositeIndexEntity> =
            get_entity(&self.app_providers, "idx_id", id).await?;
        let Some(idx) = idx else { return Ok(()) };

        delete_entity(&self.app_providers, &idx.pk, &idx.rk).await?;
        delete_entity(&self.app_providers, "idx_id", id).await?;
        Ok(())
    }
}

// ─── AuthCodeRepository ─────────────────────────────────────────────────────

#[async_trait]
impl AuthCodeRepository for AzureTableRepository {
    async fn find_by_code(&self, code: &str) -> Result<Option<AuthorizationCode>, AppError> {
        let entity: Option<AuthCodeEntity> = get_entity(&self.auth_codes, "code", code).await?;
        Ok(entity.map(|e| e.to_model()))
    }

    async fn insert(&self, code: &AuthorizationCode) -> Result<(), AppError> {
        let entity = AuthCodeEntity::from_model(code);
        insert_entity(&self.auth_codes, &entity)
            .await
            .map_err(db_err)?;
        Ok(())
    }

    async fn mark_used(&self, code: &str) -> Result<(), AppError> {
        let entity: Option<AuthCodeEntity> = get_entity(&self.auth_codes, "code", code).await?;
        if let Some(mut entity) = entity {
            entity.used = true;
            upsert_entity(&self.auth_codes, "code", code, &entity).await?;
        }
        Ok(())
    }
}

// ─── RefreshTokenRepository ─────────────────────────────────────────────────

#[async_trait]
impl RefreshTokenRepository for AzureTableRepository {
    async fn find_by_token_hash(&self, hash: &str) -> Result<Option<RefreshToken>, AppError> {
        let idx: Option<IndexEntity> = get_entity(&self.refresh_tokens, "idx_hash", hash).await?;
        match idx {
            Some(idx) => {
                let entity: Option<RefreshTokenEntity> =
                    get_entity(&self.refresh_tokens, "rt", &idx.target_id).await?;
                Ok(entity.map(|e| e.to_model()))
            }
            None => Ok(None),
        }
    }

    async fn insert(&self, token: &RefreshToken) -> Result<(), AppError> {
        // Insert token hash index
        let hash_idx = IndexEntity {
            partition_key: "idx_hash".into(),
            row_key: token.token_hash.clone(),
            target_id: token.id.clone(),
        };
        let _ = insert_entity(&self.refresh_tokens, &hash_idx).await;

        // Insert primary entity
        let entity = RefreshTokenEntity::from_model(token);
        insert_entity(&self.refresh_tokens, &entity)
            .await
            .map_err(db_err)?;
        Ok(())
    }

    async fn revoke(&self, id: &str) -> Result<(), AppError> {
        let entity: Option<RefreshTokenEntity> = get_entity(&self.refresh_tokens, "rt", id).await?;
        if let Some(mut entity) = entity {
            entity.revoked = true;
            upsert_entity(&self.refresh_tokens, "rt", id, &entity).await?;
        }
        Ok(())
    }
}

// ─── InviteCodeRepository ───────────────────────────────────────────────────

/// Generate a 16-character URL-safe alphanumeric invite code.
fn generate_invite_code() -> String {
    use rand::distributions::{Alphanumeric, DistString};
    Alphanumeric.sample_string(&mut rand::thread_rng(), 16)
}

#[async_trait]
impl InviteCodeRepository for AzureTableRepository {
    async fn create_invite_code(&self, created_by: &str) -> Result<InviteCode, AppError> {
        let code = InviteCode {
            id: uuid::Uuid::new_v4().to_string(),
            code: generate_invite_code(),
            created_by: created_by.to_string(),
            created_at: chrono::Utc::now().naive_utc(),
            used_at: None,
            used_by: None,
            is_revoked: false,
        };
        let entity = InviteCodeEntity::from_model(&code);
        insert_entity(&self.invite_codes, &entity)
            .await
            .map_err(db_err)?;
        tracing::info!(invite_code_id = %code.id, created_by = %created_by, "invite code created");
        Ok(code)
    }

    async fn get_invite_code_by_code(&self, code: &str) -> Result<Option<InviteCode>, AppError> {
        let entity: Option<InviteCodeEntity> =
            get_entity(&self.invite_codes, "invite_code", code).await?;
        Ok(entity.map(|e| e.to_model()))
    }

    /// Atomically marks the invite code as used using Azure Tables If-Match ETag.
    /// `code` is the RowKey value, fetched directly via PK/RK lookup (no scan).
    /// Returns AppError::InviteCodeAlreadyUsed on a race condition (412 Precondition Failed).
    async fn mark_invite_code_used(&self, code: &str, user_id: &str) -> Result<(), AppError> {
        let ec = self
            .invite_codes
            .partition_key_client("invite_code")
            .entity_client(code);

        let resp = ec.get::<InviteCodeEntity>().await.map_err(|e| {
            if is_not_found(&e) {
                AppError::InviteCodeNotFound
            } else {
                db_err(e)
            }
        })?;

        if resp.entity.used_at.is_some() {
            return Err(AppError::InviteCodeAlreadyUsed);
        }

        let etag = resp.etag;
        let mut updated = resp.entity;
        updated.used_at = Some(fmt_dt(&chrono::Utc::now().naive_utc()));
        updated.used_by = Some(user_id.to_string());

        ec.update(
            updated,
            azure_data_tables::prelude::IfMatchCondition::Etag(etag),
        )
        .map_err(db_err)?
        .await
        .map_err(|e| {
            if e.as_http_error()
                .map(|h| h.status() == azure_core::StatusCode::PreconditionFailed)
                .unwrap_or(false)
            {
                AppError::InviteCodeAlreadyUsed
            } else {
                db_err(e)
            }
        })?;

        Ok(())
    }

    async fn list_invite_codes(
        &self,
        used_only: Option<bool>,
    ) -> Result<Vec<InviteCode>, AppError> {
        let entities: Vec<InviteCodeEntity> =
            query_entities(&self.invite_codes, "PartitionKey eq 'invite_code'").await?;
        let mut codes: Vec<InviteCode> = entities.iter().map(|e| e.to_model()).collect();
        if let Some(used) = used_only {
            codes.retain(|c| used == c.used_at.is_some());
        }
        codes.sort_by_key(|c| std::cmp::Reverse(c.created_at));
        Ok(codes)
    }

    /// `code` is the RowKey value, fetched directly via PK/RK lookup (no scan).
    async fn revoke_invite_code(&self, code: &str) -> Result<(), AppError> {
        let entity: InviteCodeEntity = get_entity(&self.invite_codes, "invite_code", code)
            .await?
            .ok_or(AppError::InviteCodeNotFound)?;

        if entity.used_at.is_some() {
            return Err(AppError::InviteCodeAlreadyUsed);
        }

        let mut updated = entity;
        updated.is_revoked = true;
        let code_val = updated.row_key.clone();
        upsert_entity(&self.invite_codes, "invite_code", &code_val, &updated).await
    }
}

// ─── TeamRepository ─────────────────────────────────────────────────────────

#[async_trait]
impl TeamRepository for AzureTableRepository {
    async fn find_by_id(&self, id: &str) -> Result<Option<Team>, AppError> {
        let entity: Option<TeamEntity> = get_entity(&self.teams, "team", id).await?;
        Ok(entity.map(|e| e.to_model()))
    }

    async fn find_all_open(&self) -> Result<Vec<Team>, AppError> {
        let entities: Vec<TeamEntity> =
            query_entities(&self.teams, "PartitionKey eq 'team' and is_open eq true").await?;
        Ok(entities.iter().map(|e| e.to_model()).collect())
    }

    async fn insert(&self, team: &Team) -> Result<(), AppError> {
        let entity = TeamEntity::from_model(team);
        insert_entity(&self.teams, &entity).await.map_err(db_err)?;
        Ok(())
    }

    async fn update(&self, team: &Team) -> Result<(), AppError> {
        let entity = TeamEntity::from_model(team);
        upsert_entity(&self.teams, "team", &team.id, &entity).await
    }

    async fn delete_by_id(&self, id: &str) -> Result<(), AppError> {
        delete_entity(&self.teams, "team", id).await
    }
}

// ─── TeamMembershipRepository ───────────────────────────────────────────────

#[async_trait]
impl TeamMembershipRepository for AzureTableRepository {
    async fn find_all_by_team(&self, team_id: &str) -> Result<Vec<TeamMembership>, AppError> {
        let filter = format!("PartitionKey eq '{team_id}'");
        let entities: Vec<TeamMembershipEntity> =
            query_entities(&self.team_memberships, &filter).await?;
        Ok(entities.iter().map(|e| e.to_model()).collect())
    }

    async fn find_all_by_user(&self, user_id: &str) -> Result<Vec<TeamMembership>, AppError> {
        let filter = format!("RowKey eq '{user_id}'");
        let entities: Vec<TeamMembershipEntity> =
            query_entities(&self.team_memberships, &filter).await?;
        Ok(entities.iter().map(|e| e.to_model()).collect())
    }

    async fn find(
        &self,
        team_id: &str,
        user_id: &str,
    ) -> Result<Option<TeamMembership>, AppError> {
        let entity: Option<TeamMembershipEntity> =
            get_entity(&self.team_memberships, team_id, user_id).await?;
        Ok(entity.map(|e| e.to_model()))
    }

    async fn insert(&self, m: &TeamMembership) -> Result<(), AppError> {
        let entity = TeamMembershipEntity::from_model(m);
        upsert_entity(&self.team_memberships, &m.team_id, &m.user_id, &entity).await
    }

    async fn count_by_team(&self, team_id: &str) -> Result<u64, AppError> {
        let members = self.find_all_by_team(team_id).await?;
        Ok(members.len() as u64)
    }

    async fn delete(&self, team_id: &str, user_id: &str) -> Result<(), AppError> {
        delete_entity(&self.team_memberships, team_id, user_id).await
    }
}
