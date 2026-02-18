use sea_orm::entity::prelude::*;
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, PartialEq, Eq, DeriveEntityModel, Serialize, Deserialize)]
#[sea_orm(table_name = "applications")]
pub struct Model {
    #[sea_orm(primary_key, auto_increment = false)]
    pub id: String,
    pub name: String,
    #[sea_orm(unique)]
    pub client_id: String,
    pub client_secret_hash: String,
    pub redirect_uris: String,
    pub allowed_scopes: String,
    pub is_active: bool,
    pub created_at: chrono::NaiveDateTime,
    pub updated_at: chrono::NaiveDateTime,
}

#[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
pub enum Relation {
    #[sea_orm(has_many = "super::app_provider::Entity")]
    AppProviders,
    #[sea_orm(has_many = "super::authorization_code::Entity")]
    AuthorizationCodes,
    #[sea_orm(has_many = "super::refresh_token::Entity")]
    RefreshTokens,
}

impl Related<super::app_provider::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::AppProviders.def()
    }
}

impl Related<super::authorization_code::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::AuthorizationCodes.def()
    }
}

impl Related<super::refresh_token::Entity> for Entity {
    fn to() -> RelationDef {
        Relation::RefreshTokens.def()
    }
}

impl ActiveModelBehavior for ActiveModel {}
