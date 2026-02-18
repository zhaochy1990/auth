use sea_orm_migration::prelude::*;

#[derive(DeriveMigrationName)]
pub struct Migration;

#[async_trait::async_trait]
impl MigrationTrait for Migration {
    async fn up(&self, manager: &SchemaManager) -> Result<(), DbErr> {
        manager
            .create_table(
                Table::create()
                    .table(Applications::Table)
                    .if_not_exists()
                    .col(
                        ColumnDef::new(Applications::Id)
                            .string_len(36)
                            .not_null()
                            .primary_key(),
                    )
                    .col(
                        ColumnDef::new(Applications::Name)
                            .string_len(255)
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(Applications::ClientId)
                            .string_len(64)
                            .not_null()
                            .unique_key(),
                    )
                    .col(
                        ColumnDef::new(Applications::ClientSecretHash)
                            .string_len(255)
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(Applications::RedirectUris)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(Applications::AllowedScopes)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(Applications::IsActive)
                            .boolean()
                            .not_null()
                            .default(true),
                    )
                    .col(
                        ColumnDef::new(Applications::CreatedAt)
                            .date_time()
                            .not_null()
                            .default(Expr::current_timestamp()),
                    )
                    .col(
                        ColumnDef::new(Applications::UpdatedAt)
                            .date_time()
                            .not_null()
                            .default(Expr::current_timestamp()),
                    )
                    .to_owned(),
            )
            .await
    }

    async fn down(&self, manager: &SchemaManager) -> Result<(), DbErr> {
        manager
            .drop_table(Table::drop().table(Applications::Table).to_owned())
            .await
    }
}

#[derive(DeriveIden)]
pub enum Applications {
    Table,
    Id,
    Name,
    ClientId,
    ClientSecretHash,
    RedirectUris,
    AllowedScopes,
    IsActive,
    CreatedAt,
    UpdatedAt,
}
