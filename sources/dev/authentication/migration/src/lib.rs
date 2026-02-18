pub use sea_orm_migration::prelude::*;

mod m20260216_000001_create_applications;
mod m20260216_000002_create_app_providers;
mod m20260216_000003_create_users;
mod m20260216_000004_create_accounts;
mod m20260216_000005_create_authorization_codes;
mod m20260216_000006_create_refresh_tokens;
mod m20260218_000007_add_user_role_and_active;

pub struct Migrator;

#[async_trait::async_trait]
impl MigratorTrait for Migrator {
    fn migrations() -> Vec<Box<dyn MigrationTrait>> {
        vec![
            Box::new(m20260216_000001_create_applications::Migration),
            Box::new(m20260216_000002_create_app_providers::Migration),
            Box::new(m20260216_000003_create_users::Migration),
            Box::new(m20260216_000004_create_accounts::Migration),
            Box::new(m20260216_000005_create_authorization_codes::Migration),
            Box::new(m20260216_000006_create_refresh_tokens::Migration),
            Box::new(m20260218_000007_add_user_role_and_active::Migration),
        ]
    }
}
