use sea_orm_migration::prelude::*;

use crate::m20260216_000001_create_applications::Applications;
use crate::m20260216_000003_create_users::Users;

#[derive(DeriveMigrationName)]
pub struct Migration;

#[async_trait::async_trait]
impl MigrationTrait for Migration {
    async fn up(&self, manager: &SchemaManager) -> Result<(), DbErr> {
        manager
            .create_table(
                Table::create()
                    .table(AuthorizationCodes::Table)
                    .if_not_exists()
                    .col(
                        ColumnDef::new(AuthorizationCodes::Code)
                            .string_len(128)
                            .not_null()
                            .primary_key(),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::AppId)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::UserId)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::RedirectUri)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::Scopes)
                            .text()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::CodeChallenge)
                            .string_len(128)
                            .null(),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::CodeChallengeMethod)
                            .string_len(10)
                            .null(),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::ExpiresAt)
                            .date_time()
                            .not_null(),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::Used)
                            .boolean()
                            .not_null()
                            .default(false),
                    )
                    .col(
                        ColumnDef::new(AuthorizationCodes::CreatedAt)
                            .date_time()
                            .not_null()
                            .default(Expr::current_timestamp()),
                    )
                    .foreign_key(
                        ForeignKey::create()
                            .name("fk-authorization_codes-app_id")
                            .from(AuthorizationCodes::Table, AuthorizationCodes::AppId)
                            .to(Applications::Table, Applications::Id),
                    )
                    .foreign_key(
                        ForeignKey::create()
                            .name("fk-authorization_codes-user_id")
                            .from(AuthorizationCodes::Table, AuthorizationCodes::UserId)
                            .to(Users::Table, Users::Id),
                    )
                    .to_owned(),
            )
            .await
    }

    async fn down(&self, manager: &SchemaManager) -> Result<(), DbErr> {
        manager
            .drop_table(
                Table::drop()
                    .table(AuthorizationCodes::Table)
                    .to_owned(),
            )
            .await
    }
}

#[derive(DeriveIden)]
enum AuthorizationCodes {
    Table,
    Code,
    AppId,
    UserId,
    RedirectUri,
    Scopes,
    CodeChallenge,
    CodeChallengeMethod,
    ExpiresAt,
    Used,
    CreatedAt,
}
