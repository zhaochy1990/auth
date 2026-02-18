-- MSSQL schema for auth-service
-- Idempotent: safe to run multiple times

IF OBJECT_ID('dbo.applications', 'U') IS NULL
CREATE TABLE applications (
    id              NVARCHAR(36)   NOT NULL PRIMARY KEY,
    name            NVARCHAR(255)  NOT NULL,
    client_id       NVARCHAR(255)  NOT NULL UNIQUE,
    client_secret_hash NVARCHAR(255) NOT NULL,
    redirect_uris   NVARCHAR(MAX)  NOT NULL,  -- JSON array
    allowed_scopes  NVARCHAR(MAX)  NOT NULL,  -- JSON array
    is_active       BIT            NOT NULL DEFAULT 1,
    created_at      DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME(),
    updated_at      DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME()
);

IF OBJECT_ID('dbo.app_providers', 'U') IS NULL
CREATE TABLE app_providers (
    id              NVARCHAR(36)   NOT NULL PRIMARY KEY,
    app_id          NVARCHAR(36)   NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    provider_id     NVARCHAR(50)   NOT NULL,
    config          NVARCHAR(MAX)  NOT NULL,  -- JSON
    is_active       BIT            NOT NULL DEFAULT 1,
    created_at      DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME()
);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'UQ_app_providers_app_provider')
CREATE UNIQUE INDEX UQ_app_providers_app_provider ON app_providers(app_id, provider_id);

IF OBJECT_ID('dbo.users', 'U') IS NULL
CREATE TABLE users (
    id              NVARCHAR(36)   NOT NULL PRIMARY KEY,
    email           NVARCHAR(255)  NULL,
    name            NVARCHAR(255)  NULL,
    avatar_url      NVARCHAR(MAX)  NULL,
    email_verified  BIT            NOT NULL DEFAULT 0,
    role            NVARCHAR(50)   NOT NULL DEFAULT 'user',
    is_active       BIT            NOT NULL DEFAULT 1,
    created_at      DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME(),
    updated_at      DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME()
);

-- Nullable unique: only enforce uniqueness where email IS NOT NULL
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'UQ_users_email')
CREATE UNIQUE INDEX UQ_users_email ON users(email) WHERE email IS NOT NULL;

IF OBJECT_ID('dbo.accounts', 'U') IS NULL
CREATE TABLE accounts (
    id                  NVARCHAR(36)   NOT NULL PRIMARY KEY,
    user_id             NVARCHAR(36)   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id         NVARCHAR(50)   NOT NULL,
    provider_account_id NVARCHAR(255)  NULL,
    credential          NVARCHAR(MAX)  NULL,
    provider_metadata   NVARCHAR(MAX)  NOT NULL DEFAULT '{}',  -- JSON
    created_at          DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME(),
    updated_at          DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME()
);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'UQ_accounts_user_provider')
CREATE UNIQUE INDEX UQ_accounts_user_provider ON accounts(user_id, provider_id);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'UQ_accounts_provider_account')
CREATE UNIQUE INDEX UQ_accounts_provider_account ON accounts(provider_id, provider_account_id) WHERE provider_account_id IS NOT NULL;

IF OBJECT_ID('dbo.authorization_codes', 'U') IS NULL
CREATE TABLE authorization_codes (
    code                    NVARCHAR(128)  NOT NULL PRIMARY KEY,
    app_id                  NVARCHAR(36)   NOT NULL REFERENCES applications(id),
    user_id                 NVARCHAR(36)   NOT NULL REFERENCES users(id),
    redirect_uri            NVARCHAR(MAX)  NOT NULL,
    scopes                  NVARCHAR(MAX)  NOT NULL,  -- JSON array
    code_challenge          NVARCHAR(128)  NULL,
    code_challenge_method   NVARCHAR(10)   NULL,
    expires_at              DATETIME2      NOT NULL,
    used                    BIT            NOT NULL DEFAULT 0,
    created_at              DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME()
);

IF OBJECT_ID('dbo.refresh_tokens', 'U') IS NULL
CREATE TABLE refresh_tokens (
    id              NVARCHAR(36)   NOT NULL PRIMARY KEY,
    user_id         NVARCHAR(36)   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    app_id          NVARCHAR(36)   NOT NULL REFERENCES applications(id),
    token_hash      NVARCHAR(64)   NOT NULL UNIQUE,
    scopes          NVARCHAR(MAX)  NOT NULL,  -- JSON array
    device_id       NVARCHAR(255)  NULL,
    expires_at      DATETIME2      NOT NULL,
    revoked         BIT            NOT NULL DEFAULT 0,
    created_at      DATETIME2      NOT NULL DEFAULT SYSUTCDATETIME()
);
