use sea_orm_migration::prelude::*;

use crate::m20260216_000003_create_users::Users;

#[derive(DeriveMigrationName)]
pub struct Migration;

#[async_trait::async_trait]
impl MigrationTrait for Migration {
    async fn up(&self, manager: &SchemaManager) -> Result<(), DbErr> {
        manager
            .create_table(
                Table::create()
                    .table(Accounts::Table)
                    .if_not_exists()
                    .col(
                        ColumnDef::new(Accounts::Id)
                            .string_len(36)
                            .not_null()
                            .primary_key(),
                    )
                    .col(
                        ColumnDef::new(Accounts::UserId)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(Accounts::ProviderId)
                            .string_len(50)
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(Accounts::ProviderAccountId)
                            .string_len(255)
                            .null(),
                    )
                    .col(
                        ColumnDef::new(Accounts::Credential)
                            .text()
                            .null(),
                    )
                    .col(
                        ColumnDef::new(Accounts::ProviderMetadata)
                            .text()
                            .not_null()
                            .default("{}"),
                    )
                    .col(
                        ColumnDef::new(Accounts::CreatedAt)
                            .date_time()
                            .not_null()
                            .default(Expr::current_timestamp()),
                    )
                    .col(
                        ColumnDef::new(Accounts::UpdatedAt)
                            .date_time()
                            .not_null()
                            .default(Expr::current_timestamp()),
                    )
                    .foreign_key(
                        ForeignKey::create()
                            .name("fk-accounts-user_id")
                            .from(Accounts::Table, Accounts::UserId)
                            .to(Users::Table, Users::Id)
                            .on_delete(ForeignKeyAction::Cascade),
                    )
                    .to_owned(),
            )
            .await?;

        // Unique constraint on (user_id, provider_id)
        manager
            .create_index(
                Index::create()
                    .name("idx-accounts-user_id-provider_id")
                    .table(Accounts::Table)
                    .col(Accounts::UserId)
                    .col(Accounts::ProviderId)
                    .unique()
                    .to_owned(),
            )
            .await?;

        // Unique constraint on (provider_id, provider_account_id)
        // SQLite unique indexes naturally ignore NULL values, so rows where
        // provider_account_id IS NULL will not conflict with each other.
        manager
            .create_index(
                Index::create()
                    .name("idx-accounts-provider_id-provider_account_id")
                    .table(Accounts::Table)
                    .col(Accounts::ProviderId)
                    .col(Accounts::ProviderAccountId)
                    .unique()
                    .to_owned(),
            )
            .await
    }

    async fn down(&self, manager: &SchemaManager) -> Result<(), DbErr> {
        manager
            .drop_table(Table::drop().table(Accounts::Table).to_owned())
            .await
    }
}

#[derive(DeriveIden)]
enum Accounts {
    Table,
    Id,
    UserId,
    ProviderId,
    ProviderAccountId,
    Credential,
    ProviderMetadata,
    CreatedAt,
    UpdatedAt,
}
