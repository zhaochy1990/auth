use sea_orm_migration::prelude::*;

use crate::m20260216_000001_create_applications::Applications;

#[derive(DeriveMigrationName)]
pub struct Migration;

#[async_trait::async_trait]
impl MigrationTrait for Migration {
    async fn up(&self, manager: &SchemaManager) -> Result<(), DbErr> {
        manager
            .create_table(
                Table::create()
                    .table(AppProviders::Table)
                    .if_not_exists()
                    .col(
                        ColumnDef::new(AppProviders::Id)
                            .string_len(36)
                            .not_null()
                            .primary_key(),
                    )
                    .col(
                        ColumnDef::new(AppProviders::AppId)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(AppProviders::ProviderId)
                            .string_len(50)
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(AppProviders::Config)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(AppProviders::IsActive)
                            .boolean()
                            .not_null()
                            .default(true),
                    )
                    .col(
                        ColumnDef::new(AppProviders::CreatedAt)
                            .date_time()
                            .not_null()
                            .default(Expr::current_timestamp()),
                    )
                    .foreign_key(
                        ForeignKey::create()
                            .name("fk-app_providers-app_id")
                            .from(AppProviders::Table, AppProviders::AppId)
                            .to(Applications::Table, Applications::Id)
                            .on_delete(ForeignKeyAction::Cascade),
                    )
                    .to_owned(),
            )
            .await?;

        manager
            .create_index(
                Index::create()
                    .name("idx-app_providers-app_id-provider_id")
                    .table(AppProviders::Table)
                    .col(AppProviders::AppId)
                    .col(AppProviders::ProviderId)
                    .unique()
                    .to_owned(),
            )
            .await
    }

    async fn down(&self, manager: &SchemaManager) -> Result<(), DbErr> {
        manager
            .drop_table(Table::drop().table(AppProviders::Table).to_owned())
            .await
    }
}

#[derive(DeriveIden)]
enum AppProviders {
    Table,
    Id,
    AppId,
    ProviderId,
    Config,
    IsActive,
    CreatedAt,
}
