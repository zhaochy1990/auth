package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/repository/snapshot"
)

const defaultTestMySQLAdminDSN = "mysql://root:root_password@127.0.0.1:3306/"

func testMySQLAdminDSN() string {
	if v := os.Getenv("TEST_MYSQL_ADMIN_DSN"); v != "" {
		return v
	}
	return defaultTestMySQLAdminDSN
}

func newTestRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	ctx := context.Background()
	adminDSN := testMySQLAdminDSN()
	normalizedAdminDSN, err := normalizeDSN(adminDSN)
	if err != nil {
		t.Fatalf("invalid TEST_MYSQL_ADMIN_DSN: %v", err)
	}
	adminDB, err := sql.Open("mysql", normalizedAdminDSN)
	if err != nil {
		t.Fatalf("open admin MySQL connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.PingContext(ctx); err != nil {
		t.Skipf("MySQL admin connection unavailable: %v", err)
	}

	dbName := fmt.Sprintf("auth_repo_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+dbName+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create isolated test database: %v", err)
	}
	t.Cleanup(func() { _, _ = adminDB.ExecContext(context.Background(), "DROP DATABASE "+dbName) })

	repo, err := New(ctx, databaseDSN(adminDSN, dbName))
	if err != nil {
		t.Fatalf("open isolated MySQL repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo, ctx
}

func databaseDSN(adminDSN, dbName string) string {
	if strings.Contains(adminDSN, "://") {
		u, err := url.Parse(adminDSN)
		if err == nil {
			u.Path = "/" + dbName
			return u.String()
		}
	}
	cfg, err := mysqldriver.ParseDSN(adminDSN)
	if err != nil {
		return adminDSN
	}
	cfg.DBName = dbName
	return cfg.FormatDSN()
}

func TestImportSnapshotPreservesMigrationFields(t *testing.T) {
	repo, ctx := newTestRepository(t)

	createdAt := time.Date(2026, 7, 12, 8, 30, 0, 123456000, time.UTC)
	updatedAt := createdAt.Add(15 * time.Minute)
	lastLogin := createdAt.Add(30 * time.Minute)
	expiresAt := createdAt.Add(30 * 24 * time.Hour)
	grantDays := int64(30)
	membership := domain.MembershipVip1
	userType := domain.UserTypeTesting
	usedAt := createdAt.Add(time.Hour)
	usedBy := "user-1"
	email := "migrated@example.com"
	name := "迁移用户"
	providerAccountID := "password-account"
	credential := "hashed-password"
	deviceID := "device-1"
	teamDescription := "migration team"

	data := snapshot.Data{
		Applications: []domain.Application{{
			ID: "app-1", Name: "Migrated App", ClientID: "client-1", ClientSecretHash: "secret-hash",
			RedirectURIs: `["https://app.example.com/callback"]`, AllowedScopes: `["openid","profile"]`,
			IsActive: true, CreatedAt: createdAt, UpdatedAt: updatedAt,
		}},
		Users: []domain.User{{
			ID: "user-1", Email: &email, Name: &name, EmailVerified: true, Role: "user",
			UserType: domain.UserTypeTesting, IsActive: true,
			CustomAttributes: map[string]any{
				"birthday":  "1990-01-01",
				"height_cm": 180,
			},
			CreatedAt: createdAt, UpdatedAt: updatedAt, LastLoginAt: &lastLogin,
			RecentLogins: []domain.LoginRecord{{At: lastLogin, IP: "127.0.0.1"}},
			InviteCode:   &usedBy, Membership: domain.MembershipVip1, MembershipExpiresAt: &expiresAt,
		}},
		Accounts: []domain.Account{{
			ID: "account-1", UserID: "user-1", ProviderID: "password", ProviderAccountID: &providerAccountID,
			Credential: &credential, ProviderMetadata: `{"source":"snapshot"}`, CreatedAt: createdAt, UpdatedAt: updatedAt,
		}},
		AppProviders: []domain.AppProvider{{
			ID: "provider-1", AppID: "app-1", ProviderID: "password", Config: `{}`, IsActive: true, CreatedAt: createdAt,
		}},
		AuthCodes: []domain.AuthorizationCode{{
			Code: "auth-code-1", AppID: "app-1", UserID: "user-1", RedirectURI: "https://app.example.com/callback",
			Scopes: `["openid"]`, ExpiresAt: expiresAt, CreatedAt: createdAt,
		}},
		RefreshTokens: []domain.RefreshToken{{
			ID: "refresh-1", UserID: "user-1", AppID: "app-1", TokenHash: "token-hash", Scopes: `["openid"]`,
			DeviceID: &deviceID, ExpiresAt: expiresAt, CreatedAt: createdAt,
		}},
		InviteCodes: []domain.InviteCode{{
			ID: "invite-1", Code: "invite-code-1", CreatedBy: "admin-1", CreatedAt: createdAt,
			UsedAt: &usedAt, UsedBy: &usedBy, Kind: domain.InviteLongTerm,
			GrantsMembership: &membership, GrantsMembershipDays: &grantDays, GrantsUserType: &userType,
		}},
		Teams: []domain.Team{{
			ID: "team-1", Name: "Migrated Team", Description: &teamDescription, OwnerUserID: "user-1",
			IsOpen: true, CreatedAt: createdAt, UpdatedAt: updatedAt,
		}},
		TeamMemberships: []domain.TeamMembership{{
			TeamID: "team-1", UserID: "user-1", Role: "owner", JoinedAt: createdAt,
		}},
	}

	if err := repo.ImportSnapshot(ctx, data); err != nil {
		t.Fatalf("import snapshot: %v", err)
	}

	counts, err := repo.SnapshotCounts(ctx)
	if err != nil {
		t.Fatalf("snapshot counts: %v", err)
	}
	if want := data.Counts(); !reflect.DeepEqual(counts, want) {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}

	gotUser, err := repo.Users().FindByID(ctx, "user-1")
	if err != nil || gotUser == nil {
		t.Fatalf("find imported user: user=%+v err=%v", gotUser, err)
	}
	if gotUser.UserType != domain.UserTypeTesting || gotUser.Membership != domain.MembershipVip1 {
		t.Fatalf("user grants not preserved: %+v", gotUser)
	}
	if gotUser.CustomAttributes["birthday"] != "1990-01-01" || gotUser.CustomAttributes["height_cm"] != float64(180) {
		t.Fatalf("custom attributes not preserved: %+v", gotUser.CustomAttributes)
	}
	if gotUser.LastLoginAt == nil || !gotUser.LastLoginAt.Equal(lastLogin) || len(gotUser.RecentLogins) != 1 {
		t.Fatalf("login fields not preserved: %+v", gotUser)
	}

	gotInvite, err := repo.InviteCodes().GetByCode(ctx, "invite-code-1")
	if err != nil || gotInvite == nil {
		t.Fatalf("find imported invite code: invite=%+v err=%v", gotInvite, err)
	}
	if gotInvite.GrantsUserType == nil || *gotInvite.GrantsUserType != domain.UserTypeTesting {
		t.Fatalf("invite user-type grant not preserved: %+v", gotInvite)
	}
	if gotInvite.GrantsMembership == nil || *gotInvite.GrantsMembership != domain.MembershipVip1 {
		t.Fatalf("invite membership grant not preserved: %+v", gotInvite)
	}
	if gotInvite.UsedBy == nil || *gotInvite.UsedBy != usedBy || gotInvite.UsedAt == nil || !gotInvite.UsedAt.Equal(usedAt) {
		t.Fatalf("invite usage not preserved: %+v", gotInvite)
	}
}
