// Package mysql implements the repository interfaces against MySQL.
package mysql

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	pinyin "github.com/mozillazg/go-pinyin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/repository"
	"github.com/zhaochy1990/auth-service/internal/repository/snapshot"
)

const inviteAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

const tencentTLSConfigName = "auth-service-tencent-ca"

// Options controls MySQL connection setup.
type Options struct {
	TLSCAPEM  string
	TLSCAPath string
}

// Repository is the MySQL implementation of repository.Repository.
type Repository struct {
	db *sql.DB

	userRepo       *userRepo
	appRepo        *appRepo
	accountRepo    *accountRepo
	appProvRepo    *appProviderRepo
	authCodeRepo   *authCodeRepo
	refreshRepo    *refreshTokenRepo
	inviteRepo     *inviteCodeRepo
	teamRepo       *teamRepo
	membershipRepo *teamMembershipRepo
}

type dbConn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var dataTables = []string{
	"auth_team_memberships", "auth_refresh_tokens", "auth_auth_codes", "auth_accounts",
	"auth_app_providers", "auth_invite_codes", "auth_teams", "auth_users", "auth_applications",
}

// New opens a MySQL repository, verifies connectivity, and ensures the schema.
func New(ctx context.Context, dsn string) (*Repository, error) {
	return NewWithOptions(ctx, dsn, Options{})
}

// NewWithOptions opens a MySQL repository with optional connection settings.
func NewWithOptions(ctx context.Context, dsn string, opts Options) (*Repository, error) {
	normalized, err := normalizeDSN(dsn, opts)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", normalized)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	r := &Repository{db: db}
	if err := r.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	r.userRepo = &userRepo{db: db}
	r.appRepo = &appRepo{db: db}
	r.accountRepo = &accountRepo{db: db}
	r.appProvRepo = &appProviderRepo{db: db}
	r.authCodeRepo = &authCodeRepo{db: db}
	r.refreshRepo = &refreshTokenRepo{db: db}
	r.inviteRepo = &inviteCodeRepo{db: db}
	r.teamRepo = &teamRepo{db: db}
	r.membershipRepo = &teamMembershipRepo{db: db}
	return r, nil
}

func normalizeDSN(raw string, opts Options) (string, error) {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		if u.Scheme != "mysql" {
			return "", fmt.Errorf("unsupported MySQL DSN scheme %q", u.Scheme)
		}
		cfg := mysqldriver.NewConfig()
		cfg.User = u.User.Username()
		cfg.Passwd, _ = u.User.Password()
		cfg.Net = "tcp"
		cfg.Addr = u.Host
		cfg.DBName = strings.TrimPrefix(u.Path, "/")
		cfg.ParseTime = true
		cfg.Loc = time.UTC
		cfg.Params = map[string]string{"charset": "utf8mb4"}
		for k, values := range u.Query() {
			if len(values) > 0 {
				cfg.Params[k] = values[len(values)-1]
			}
		}
		if err := applyTLSConfig(cfg, opts); err != nil {
			return "", err
		}
		return cfg.FormatDSN(), nil
	}
	return ensureDriverDSN(raw, opts)
}

func ensureDriverDSN(raw string, opts Options) (string, error) {
	cfg, err := mysqldriver.ParseDSN(raw)
	if err != nil {
		return "", err
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	if _, ok := cfg.Params["charset"]; !ok {
		cfg.Params["charset"] = "utf8mb4"
	}
	if err := applyTLSConfig(cfg, opts); err != nil {
		return "", err
	}
	return cfg.FormatDSN(), nil
}

func applyTLSConfig(cfg *mysqldriver.Config, opts Options) error {
	pem, err := tlsCAPEM(opts)
	if err != nil {
		return err
	}
	if len(pem) == 0 {
		return nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return fmt.Errorf("read MySQL TLS CA: no PEM certificates found")
	}
	if err := mysqldriver.RegisterTLSConfig(tencentTLSConfigName, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("register MySQL TLS config: %w", err)
	}
	cfg.TLSConfig = tencentTLSConfigName
	return nil
}

func tlsCAPEM(opts Options) ([]byte, error) {
	if strings.TrimSpace(opts.TLSCAPEM) != "" {
		return []byte(opts.TLSCAPEM), nil
	}
	if strings.TrimSpace(opts.TLSCAPath) == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(opts.TLSCAPath)
	if err != nil {
		return nil, fmt.Errorf("read MySQL TLS CA: %w", err)
	}
	return pem, nil
}

// Close closes the underlying database pool.
func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) Users() repository.UserRepository                     { return r.userRepo }
func (r *Repository) Applications() repository.ApplicationRepository       { return r.appRepo }
func (r *Repository) Accounts() repository.AccountRepository               { return r.accountRepo }
func (r *Repository) AppProviders() repository.AppProviderRepository       { return r.appProvRepo }
func (r *Repository) AuthCodes() repository.AuthCodeRepository             { return r.authCodeRepo }
func (r *Repository) RefreshTokens() repository.RefreshTokenRepository     { return r.refreshRepo }
func (r *Repository) InviteCodes() repository.InviteCodeRepository         { return r.inviteRepo }
func (r *Repository) Teams() repository.TeamRepository                     { return r.teamRepo }
func (r *Repository) TeamMemberships() repository.TeamMembershipRepository { return r.membershipRepo }

// EnsureSchema creates the MySQL schema used by the auth service.
func (r *Repository) EnsureSchema(ctx context.Context) error {
	for _, stmt := range schemaStatements {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := r.ensureColumn(ctx, "auth_users", "user_type", "VARCHAR(32) NOT NULL DEFAULT 'regular' AFTER role"); err != nil {
		return err
	}
	if err := r.ensureColumn(ctx, "auth_users", "custom_attributes", "TEXT NULL AFTER note"); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, "UPDATE auth_users SET custom_attributes = '{}' WHERE custom_attributes IS NULL OR custom_attributes = ''"); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, "ALTER TABLE auth_users MODIFY COLUMN custom_attributes TEXT NOT NULL"); err != nil {
		return err
	}
	if err := r.ensureColumn(ctx, "auth_invite_codes", "grants_user_type", "VARCHAR(32) NULL AFTER grants_membership_days"); err != nil {
		return err
	}
	return nil
}

func (r *Repository) ensureColumn(ctx context.Context, table, column, definition string) error {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, table, column).Scan(&count)
	if err != nil || count > 0 {
		return err
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

// ClearAllTables removes all data. It is intended for integration tests only.
func (r *Repository) ClearAllTables(ctx context.Context) error {
	return clearTables(ctx, r.db)
}

func clearTables(ctx context.Context, db dbConn) error {
	for _, table := range dataTables {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	return nil
}

// SnapshotCounts returns row counts by logical collection name.
func (r *Repository) SnapshotCounts(ctx context.Context) (map[string]int, error) {
	queries := map[string]string{
		"applications":     "SELECT COUNT(*) FROM auth_applications",
		"users":            "SELECT COUNT(*) FROM auth_users",
		"accounts":         "SELECT COUNT(*) FROM auth_accounts",
		"app_providers":    "SELECT COUNT(*) FROM auth_app_providers",
		"auth_codes":       "SELECT COUNT(*) FROM auth_auth_codes",
		"refresh_tokens":   "SELECT COUNT(*) FROM auth_refresh_tokens",
		"invite_codes":     "SELECT COUNT(*) FROM auth_invite_codes",
		"teams":            "SELECT COUNT(*) FROM auth_teams",
		"team_memberships": "SELECT COUNT(*) FROM auth_team_memberships",
	}
	out := make(map[string]int, len(queries))
	for name, query := range queries {
		var n int
		if err := r.db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS auth_applications (
		id VARCHAR(64) NOT NULL PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		client_id VARCHAR(96) NOT NULL,
		client_secret_hash TEXT NOT NULL,
		redirect_uris TEXT NOT NULL,
		allowed_scopes TEXT NOT NULL,
		is_active BOOLEAN NOT NULL,
		created_at DATETIME(6) NOT NULL,
		updated_at DATETIME(6) NOT NULL,
		UNIQUE KEY uq_auth_applications_client_id (client_id),
		KEY idx_auth_applications_name (name),
		KEY idx_auth_applications_created_at (created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS auth_users (
		id VARCHAR(64) NOT NULL PRIMARY KEY,
		email VARCHAR(320) NULL,
		email_lookup VARCHAR(320) NULL,
		name VARCHAR(255) NULL,
		avatar_url TEXT NULL,
		email_verified BOOLEAN NOT NULL DEFAULT FALSE,
		role VARCHAR(32) NOT NULL DEFAULT 'user',
		user_type VARCHAR(32) NOT NULL DEFAULT 'regular',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		note TEXT NULL,
		custom_attributes TEXT NOT NULL,
		created_at DATETIME(6) NOT NULL,
		updated_at DATETIME(6) NOT NULL,
		last_login_at DATETIME(6) NULL,
		recent_logins TEXT NULL,
		invite_code VARCHAR(64) NULL,
		membership VARCHAR(32) NOT NULL DEFAULT 'regular',
		membership_expires_at DATETIME(6) NULL,
		UNIQUE KEY uq_auth_users_email_lookup (email_lookup),
		KEY idx_auth_users_created_at (created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS auth_accounts (
		id VARCHAR(64) NOT NULL PRIMARY KEY,
		user_id VARCHAR(64) NOT NULL,
		provider_id VARCHAR(96) NOT NULL,
		provider_account_id VARCHAR(512) NULL,
		credential TEXT NULL,
		provider_metadata TEXT NOT NULL,
		created_at DATETIME(6) NOT NULL,
		updated_at DATETIME(6) NOT NULL,
		UNIQUE KEY uq_auth_accounts_user_provider (user_id, provider_id),
		UNIQUE KEY uq_auth_accounts_provider_account (provider_id, provider_account_id),
		KEY idx_auth_accounts_user_id (user_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS auth_app_providers (
		id VARCHAR(64) NOT NULL PRIMARY KEY,
		app_id VARCHAR(64) NOT NULL,
		provider_id VARCHAR(96) NOT NULL,
		config TEXT NOT NULL,
		is_active BOOLEAN NOT NULL,
		created_at DATETIME(6) NOT NULL,
		UNIQUE KEY uq_auth_app_providers_app_provider (app_id, provider_id),
		KEY idx_auth_app_providers_app_id (app_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS auth_auth_codes (
		code VARCHAR(160) NOT NULL PRIMARY KEY,
		app_id VARCHAR(64) NOT NULL,
		user_id VARCHAR(64) NOT NULL,
		redirect_uri TEXT NOT NULL,
		scopes TEXT NOT NULL,
		code_challenge VARCHAR(256) NULL,
		code_challenge_method VARCHAR(32) NULL,
		expires_at DATETIME(6) NOT NULL,
		used BOOLEAN NOT NULL DEFAULT FALSE,
		created_at DATETIME(6) NOT NULL,
		KEY idx_auth_auth_codes_user_id (user_id),
		KEY idx_auth_auth_codes_expires_at (expires_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS auth_refresh_tokens (
		id VARCHAR(64) NOT NULL PRIMARY KEY,
		user_id VARCHAR(64) NOT NULL,
		app_id VARCHAR(64) NOT NULL,
		token_hash VARCHAR(128) NOT NULL,
		scopes TEXT NOT NULL,
		device_id VARCHAR(255) NULL,
		expires_at DATETIME(6) NOT NULL,
		revoked BOOLEAN NOT NULL DEFAULT FALSE,
		created_at DATETIME(6) NOT NULL,
		UNIQUE KEY uq_auth_refresh_tokens_hash (token_hash),
		KEY idx_auth_refresh_tokens_user_id (user_id),
		KEY idx_auth_refresh_tokens_expires_at (expires_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS auth_invite_codes (
		id VARCHAR(64) NOT NULL PRIMARY KEY,
		code VARCHAR(64) NOT NULL,
		created_by VARCHAR(64) NOT NULL,
		created_at DATETIME(6) NOT NULL,
		used_at DATETIME(6) NULL,
		used_by VARCHAR(64) NULL,
		is_revoked BOOLEAN NOT NULL DEFAULT FALSE,
		kind VARCHAR(32) NOT NULL DEFAULT 'single_use',
		grants_membership VARCHAR(32) NULL,
		grants_membership_days BIGINT NULL,
		grants_user_type VARCHAR(32) NULL,
		UNIQUE KEY uq_auth_invite_codes_code (code),
		KEY idx_auth_invite_codes_created_at (created_at),
		KEY idx_auth_invite_codes_used_at (used_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS auth_teams (
		id VARCHAR(64) NOT NULL PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		description TEXT NULL,
		owner_user_id VARCHAR(64) NOT NULL,
		is_open BOOLEAN NOT NULL DEFAULT TRUE,
		created_at DATETIME(6) NOT NULL,
		updated_at DATETIME(6) NOT NULL,
		KEY idx_auth_teams_owner_user_id (owner_user_id),
		KEY idx_auth_teams_is_open (is_open)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS auth_team_memberships (
		team_id VARCHAR(64) NOT NULL,
		user_id VARCHAR(64) NOT NULL,
		role VARCHAR(32) NOT NULL,
		joined_at DATETIME(6) NOT NULL,
		PRIMARY KEY (team_id, user_id),
		KEY idx_auth_team_memberships_user_id (user_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
}

type rowScanner interface{ Scan(dest ...any) error }

func dbErr(err error) error {
	if err == nil {
		return nil
	}
	return apperror.Database(err.Error())
}

func nullString(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func ptrString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func nullTime(p *time.Time) sql.NullTime {
	if p == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: p.UTC(), Valid: true}
}

func nullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func ptrTime(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	v := nt.Time.UTC()
	return &v
}

func emailLookup(email *string) sql.NullString {
	if email == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.ToLower(*email), Valid: true}
}

func defaultJSONObj(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

func defaultJSONArr(s string) string {
	if s == "" {
		return "[]"
	}
	return s
}

type loginPersist struct {
	At string `json:"at"`
	IP string `json:"ip"`
}

func fmtDT(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000") }

func parseDT(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.000000", "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func serializeLogins(records []domain.LoginRecord) sql.NullString {
	if len(records) == 0 {
		return sql.NullString{}
	}
	persist := make([]loginPersist, 0, len(records))
	for _, r := range records {
		persist = append(persist, loginPersist{At: fmtDT(r.At), IP: r.IP})
	}
	b, err := json.Marshal(persist)
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

func deserializeLogins(ns sql.NullString) []domain.LoginRecord {
	if !ns.Valid {
		return nil
	}
	var persist []loginPersist
	if err := json.Unmarshal([]byte(ns.String), &persist); err != nil {
		return nil
	}
	out := make([]domain.LoginRecord, 0, len(persist))
	for _, p := range persist {
		out = append(out, domain.LoginRecord{At: parseDT(p.At), IP: p.IP})
	}
	return out
}

func serializeCustomAttributes(attributes map[string]any) string {
	if attributes == nil {
		return "{}"
	}
	b, err := json.Marshal(attributes)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func deserializeCustomAttributes(ns sql.NullString) map[string]any {
	if !ns.Valid || ns.String == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(ns.String), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func defaultUserType(t domain.UserType) domain.UserType {
	if t.Valid() {
		return t
	}
	return domain.UserTypeRegular
}

func normalizeUserSortName(value string) string {
	a := pinyin.NewArgs()
	a.Style = pinyin.Normal
	a.Fallback = func(r rune, _ pinyin.Args) []string {
		if unicode.IsControl(r) {
			return nil
		}
		return []string{strings.ToLower(string(r))}
	}

	parts := pinyin.LazyConvert(strings.TrimSpace(value), &a)
	return strings.Join(parts, "")
}

func userSortNameKey(u domain.User) string {
	if u.Name != nil && strings.TrimSpace(*u.Name) != "" {
		return normalizeUserSortName(*u.Name)
	}
	if u.Email != nil {
		return normalizeUserSortName(*u.Email)
	}
	return ""
}

func userLastLoginKey(u domain.User) string {
	if u.LastLoginAt == nil {
		return "0"
	}
	return "1" + fmtDT(*u.LastLoginAt)
}

func compareUser(a, b domain.User, sortSpec repository.UserListSort) int {
	var ak, bk string
	switch sortSpec.By {
	case repository.UserListSortByLastLoginAt:
		ak, bk = userLastLoginKey(a), userLastLoginKey(b)
	default:
		ak, bk = userSortNameKey(a), userSortNameKey(b)
	}
	if ak < bk {
		return -1
	}
	if ak > bk {
		return 1
	}
	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}
	return 0
}

func sortUsers(users []domain.User, sortSpec repository.UserListSort) {
	sort.SliceStable(users, func(i, j int) bool {
		cmp := compareUser(users[i], users[j], sortSpec)
		if sortSpec.Order == repository.SortOrderDesc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func isDuplicate(err error) bool {
	var me *mysqldriver.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

const userColumns = `id, email, name, avatar_url, email_verified, role, user_type, is_active, note, custom_attributes, created_at, updated_at, last_login_at, recent_logins, invite_code, membership, membership_expires_at`

type userRepo struct{ db dbConn }

func scanUser(s rowScanner) (*domain.User, error) {
	var u domain.User
	var email, name, avatar, note, customAttrs, recent, invite, membership, userType sql.NullString
	var lastLogin, membershipExpires sql.NullTime
	if err := s.Scan(&u.ID, &email, &name, &avatar, &u.EmailVerified, &u.Role, &userType, &u.IsActive, &note, &customAttrs, &u.CreatedAt, &u.UpdatedAt, &lastLogin, &recent, &invite, &membership, &membershipExpires); err != nil {
		return nil, err
	}
	if u.Role == "" {
		u.Role = "user"
	}
	mem := membership.String
	if !membership.Valid || mem == "" {
		mem = string(domain.MembershipRegular)
	}
	u.Email = ptrString(email)
	u.Name = ptrString(name)
	u.AvatarURL = ptrString(avatar)
	u.UserType = domain.UserTypeFromString(userType.String)
	u.Note = ptrString(note)
	u.CustomAttributes = deserializeCustomAttributes(customAttrs)
	u.LastLoginAt = ptrTime(lastLogin)
	u.RecentLogins = deserializeLogins(recent)
	u.InviteCode = ptrString(invite)
	u.Membership = domain.MembershipFromString(mem)
	u.MembershipExpiresAt = ptrTime(membershipExpires)
	u.CreatedAt = u.CreatedAt.UTC()
	u.UpdatedAt = u.UpdatedAt.UTC()
	return &u, nil
}

func (r *userRepo) FindByID(ctx context.Context, id string) (*domain.User, error) {
	u, err := scanUser(r.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM auth_users WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return u, nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	u, err := scanUser(r.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM auth_users WHERE email_lookup = ?", strings.ToLower(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return u, nil
}

func (r *userRepo) Insert(ctx context.Context, u *domain.User) error {
	role := u.Role
	if role == "" {
		role = "user"
	}
	membership := string(u.Membership)
	if membership == "" {
		membership = string(domain.MembershipRegular)
	}
	userType := string(defaultUserType(u.UserType))
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_users
		(id, email, email_lookup, name, avatar_url, email_verified, role, user_type, is_active, note, custom_attributes, created_at, updated_at, last_login_at, recent_logins, invite_code, membership, membership_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, nullString(u.Email), emailLookup(u.Email), nullString(u.Name), nullString(u.AvatarURL), u.EmailVerified, role, userType, u.IsActive, nullString(u.Note), serializeCustomAttributes(u.CustomAttributes), u.CreatedAt.UTC(), u.UpdatedAt.UTC(), nullTime(u.LastLoginAt), serializeLogins(u.RecentLogins), nullString(u.InviteCode), membership, nullTime(u.MembershipExpiresAt))
	if err != nil {
		if isDuplicate(err) {
			return apperror.Database("user already exists")
		}
		return dbErr(err)
	}
	return nil
}

func (r *userRepo) Update(ctx context.Context, u *domain.User) error {
	role := u.Role
	if role == "" {
		role = "user"
	}
	membership := string(u.Membership)
	if membership == "" {
		membership = string(domain.MembershipRegular)
	}
	userType := string(defaultUserType(u.UserType))
	_, err := r.db.ExecContext(ctx, `UPDATE auth_users SET
		email = ?, email_lookup = ?, name = ?, avatar_url = ?, email_verified = ?, role = ?, user_type = ?, is_active = ?, note = ?, custom_attributes = ?, updated_at = ?, last_login_at = ?, recent_logins = ?, invite_code = ?, membership = ?, membership_expires_at = ?
		WHERE id = ?`,
		nullString(u.Email), emailLookup(u.Email), nullString(u.Name), nullString(u.AvatarURL), u.EmailVerified, role, userType, u.IsActive, nullString(u.Note), serializeCustomAttributes(u.CustomAttributes), u.UpdatedAt.UTC(), nullTime(u.LastLoginAt), serializeLogins(u.RecentLogins), nullString(u.InviteCode), membership, nullTime(u.MembershipExpiresAt), u.ID)
	if err != nil {
		return dbErr(err)
	}
	return nil
}

func (r *userRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_users WHERE id = ?", id)
	return dbErr(err)
}

func (r *userRepo) CountAll(ctx context.Context) (uint64, error) {
	var n uint64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users").Scan(&n); err != nil {
		return 0, dbErr(err)
	}
	return n, nil
}

func (r *userRepo) CountSince(ctx context.Context, since time.Time) (uint64, error) {
	var n uint64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users WHERE created_at >= ?", since.UTC()).Scan(&n); err != nil {
		return 0, dbErr(err)
	}
	return n, nil
}

func (r *userRepo) ListPaginated(ctx context.Context, search, idSearch string, userType *domain.UserType, sortSpec repository.UserListSort, offset, limit uint64) ([]domain.User, uint64, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	where := ""
	args := []any{}
	clauses := []string{}
	if strings.TrimSpace(search) != "" {
		clauses = append(clauses, "(LOWER(COALESCE(email, '')) LIKE ? OR LOWER(COALESCE(name, '')) LIKE ?)")
		pattern := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
		args = append(args, pattern, pattern)
	}
	if strings.TrimSpace(idSearch) != "" {
		clauses = append(clauses, "LOWER(id) LIKE ?")
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(idSearch))+"%")
	}
	if userType != nil {
		clauses = append(clauses, "user_type = ?")
		args = append(args, string(defaultUserType(*userType)))
	}
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+userColumns+" FROM auth_users"+where, args...)
	if err != nil {
		return nil, 0, dbErr(err)
	}
	defer rows.Close()
	all := make([]domain.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, dbErr(err)
		}
		all = append(all, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, dbErr(err)
	}
	sortUsers(all, sortSpec)
	total := uint64(len(all))
	if offset >= total {
		return []domain.User{}, total, nil
	}
	end := offset + limit
	if end < offset || end > total {
		end = total
	}
	return all[int(offset):int(end)], total, nil
}

func (r *userRepo) RecordLogin(ctx context.Context, userID, ip string) error {
	u, err := r.FindByID(ctx, userID)
	if err != nil || u == nil {
		return err
	}
	now := time.Now().UTC()
	records := []domain.LoginRecord{{At: now, IP: ip}}
	records = append(records, u.RecentLogins...)
	if len(records) > 3 {
		records = records[:3]
	}
	u.RecentLogins = records
	u.LastLoginAt = &now
	u.UpdatedAt = now
	return r.Update(ctx, u)
}

const appColumns = `id, name, client_id, client_secret_hash, redirect_uris, allowed_scopes, is_active, created_at, updated_at`

type appRepo struct{ db dbConn }

func scanApp(s rowScanner) (*domain.Application, error) {
	var a domain.Application
	if err := s.Scan(&a.ID, &a.Name, &a.ClientID, &a.ClientSecretHash, &a.RedirectURIs, &a.AllowedScopes, &a.IsActive, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.CreatedAt = a.CreatedAt.UTC()
	a.UpdatedAt = a.UpdatedAt.UTC()
	a.RedirectURIs = defaultJSONArr(a.RedirectURIs)
	a.AllowedScopes = defaultJSONArr(a.AllowedScopes)
	return &a, nil
}

func (r *appRepo) FindByID(ctx context.Context, id string) (*domain.Application, error) {
	a, err := scanApp(r.db.QueryRowContext(ctx, "SELECT "+appColumns+" FROM auth_applications WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return a, nil
}

func (r *appRepo) FindByClientID(ctx context.Context, clientID string) (*domain.Application, error) {
	a, err := scanApp(r.db.QueryRowContext(ctx, "SELECT "+appColumns+" FROM auth_applications WHERE client_id = ?", clientID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return a, nil
}

func (r *appRepo) FindByName(ctx context.Context, name string) (*domain.Application, error) {
	a, err := scanApp(r.db.QueryRowContext(ctx, "SELECT "+appColumns+" FROM auth_applications WHERE name = ? ORDER BY created_at ASC LIMIT 1", name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return a, nil
}

func (r *appRepo) FindAll(ctx context.Context) ([]domain.Application, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+appColumns+" FROM auth_applications ORDER BY created_at ASC")
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]domain.Application, 0)
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, *a)
	}
	return out, dbErr(rows.Err())
}

func (r *appRepo) Insert(ctx context.Context, a *domain.Application) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_applications (id, name, client_id, client_secret_hash, redirect_uris, allowed_scopes, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, a.Name, a.ClientID, a.ClientSecretHash, defaultJSONArr(a.RedirectURIs), defaultJSONArr(a.AllowedScopes), a.IsActive, a.CreatedAt.UTC(), a.UpdatedAt.UTC())
	if err != nil {
		return dbErr(err)
	}
	return nil
}

func (r *appRepo) Update(ctx context.Context, a *domain.Application) error {
	_, err := r.db.ExecContext(ctx, `UPDATE auth_applications SET name = ?, client_id = ?, client_secret_hash = ?, redirect_uris = ?, allowed_scopes = ?, is_active = ?, updated_at = ? WHERE id = ?`, a.Name, a.ClientID, a.ClientSecretHash, defaultJSONArr(a.RedirectURIs), defaultJSONArr(a.AllowedScopes), a.IsActive, a.UpdatedAt.UTC(), a.ID)
	return dbErr(err)
}

func (r *appRepo) CountAll(ctx context.Context) (uint64, error) {
	var n uint64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_applications").Scan(&n)
	return n, dbErr(err)
}
func (r *appRepo) CountActive(ctx context.Context) (uint64, error) {
	var n uint64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_applications WHERE is_active = TRUE").Scan(&n)
	return n, dbErr(err)
}

const accountColumns = `id, user_id, provider_id, provider_account_id, credential, provider_metadata, created_at, updated_at`

type accountRepo struct{ db dbConn }

func scanAccount(s rowScanner) (*domain.Account, error) {
	var a domain.Account
	var providerAccountID, credential sql.NullString
	if err := s.Scan(&a.ID, &a.UserID, &a.ProviderID, &providerAccountID, &credential, &a.ProviderMetadata, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.ProviderAccountID = ptrString(providerAccountID)
	a.Credential = ptrString(credential)
	a.ProviderMetadata = defaultJSONObj(a.ProviderMetadata)
	a.CreatedAt = a.CreatedAt.UTC()
	a.UpdatedAt = a.UpdatedAt.UTC()
	return &a, nil
}

func (r *accountRepo) FindByUserAndProvider(ctx context.Context, userID, providerID string) (*domain.Account, error) {
	a, err := scanAccount(r.db.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM auth_accounts WHERE user_id = ? AND provider_id = ?", userID, providerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return a, nil
}
func (r *accountRepo) FindByProviderAccount(ctx context.Context, providerID, providerAccountID string) (*domain.Account, error) {
	a, err := scanAccount(r.db.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM auth_accounts WHERE provider_id = ? AND provider_account_id = ?", providerID, providerAccountID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return a, nil
}
func (r *accountRepo) FindAllByUser(ctx context.Context, userID string) ([]domain.Account, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+accountColumns+" FROM auth_accounts WHERE user_id = ? ORDER BY created_at ASC", userID)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]domain.Account, 0)
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, *a)
	}
	return out, dbErr(rows.Err())
}
func (r *accountRepo) CountByUser(ctx context.Context, userID string) (uint64, error) {
	var n uint64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_accounts WHERE user_id = ?", userID).Scan(&n)
	return n, dbErr(err)
}
func (r *accountRepo) Insert(ctx context.Context, a *domain.Account) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_accounts (id, user_id, provider_id, provider_account_id, credential, provider_metadata, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, a.UserID, a.ProviderID, nullString(a.ProviderAccountID), nullString(a.Credential), defaultJSONObj(a.ProviderMetadata), a.CreatedAt.UTC(), a.UpdatedAt.UTC())
	if err != nil {
		return dbErr(err)
	}
	return nil
}
func (r *accountRepo) Update(ctx context.Context, a *domain.Account) error {
	_, err := r.db.ExecContext(ctx, `UPDATE auth_accounts SET user_id = ?, provider_id = ?, provider_account_id = ?, credential = ?, provider_metadata = ?, updated_at = ? WHERE id = ?`, a.UserID, a.ProviderID, nullString(a.ProviderAccountID), nullString(a.Credential), defaultJSONObj(a.ProviderMetadata), a.UpdatedAt.UTC(), a.ID)
	return dbErr(err)
}
func (r *accountRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_accounts WHERE id = ?", id)
	return dbErr(err)
}
func (r *accountRepo) DeleteAllByUser(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_accounts WHERE user_id = ?", userID)
	return dbErr(err)
}

const appProviderColumns = `id, app_id, provider_id, config, is_active, created_at`

type appProviderRepo struct{ db dbConn }

func scanAppProvider(s rowScanner) (*domain.AppProvider, error) {
	var p domain.AppProvider
	if err := s.Scan(&p.ID, &p.AppID, &p.ProviderID, &p.Config, &p.IsActive, &p.CreatedAt); err != nil {
		return nil, err
	}
	p.Config = defaultJSONObj(p.Config)
	p.CreatedAt = p.CreatedAt.UTC()
	return &p, nil
}
func (r *appProviderRepo) FindByAppAndProvider(ctx context.Context, appID, providerID string) (*domain.AppProvider, error) {
	p, err := scanAppProvider(r.db.QueryRowContext(ctx, "SELECT "+appProviderColumns+" FROM auth_app_providers WHERE app_id = ? AND provider_id = ?", appID, providerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return p, nil
}
func (r *appProviderRepo) FindAllByApp(ctx context.Context, appID string) ([]domain.AppProvider, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+appProviderColumns+" FROM auth_app_providers WHERE app_id = ? ORDER BY created_at ASC", appID)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]domain.AppProvider, 0)
	for rows.Next() {
		p, err := scanAppProvider(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, *p)
	}
	return out, dbErr(rows.Err())
}
func (r *appProviderRepo) Insert(ctx context.Context, ap *domain.AppProvider) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_app_providers (id, app_id, provider_id, config, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?)`, ap.ID, ap.AppID, ap.ProviderID, defaultJSONObj(ap.Config), ap.IsActive, ap.CreatedAt.UTC())
	return dbErr(err)
}
func (r *appProviderRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_app_providers WHERE id = ?", id)
	return dbErr(err)
}

const authCodeColumns = `code, app_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at, used, created_at`

type authCodeRepo struct{ db dbConn }

func scanAuthCode(s rowScanner) (*domain.AuthorizationCode, error) {
	var c domain.AuthorizationCode
	var challenge, method sql.NullString
	if err := s.Scan(&c.Code, &c.AppID, &c.UserID, &c.RedirectURI, &c.Scopes, &challenge, &method, &c.ExpiresAt, &c.Used, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.CodeChallenge = ptrString(challenge)
	c.CodeChallengeMethod = ptrString(method)
	c.Scopes = defaultJSONArr(c.Scopes)
	c.ExpiresAt = c.ExpiresAt.UTC()
	c.CreatedAt = c.CreatedAt.UTC()
	return &c, nil
}
func (r *authCodeRepo) FindByCode(ctx context.Context, code string) (*domain.AuthorizationCode, error) {
	c, err := scanAuthCode(r.db.QueryRowContext(ctx, "SELECT "+authCodeColumns+" FROM auth_auth_codes WHERE code = ?", code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return c, nil
}
func (r *authCodeRepo) Insert(ctx context.Context, c *domain.AuthorizationCode) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_auth_codes (code, app_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at, used, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.Code, c.AppID, c.UserID, c.RedirectURI, defaultJSONArr(c.Scopes), nullString(c.CodeChallenge), nullString(c.CodeChallengeMethod), c.ExpiresAt.UTC(), c.Used, c.CreatedAt.UTC())
	return dbErr(err)
}
func (r *authCodeRepo) MarkUsed(ctx context.Context, code string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE auth_auth_codes SET used = TRUE WHERE code = ?", code)
	return dbErr(err)
}
func (r *authCodeRepo) DeleteAllByUser(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_auth_codes WHERE user_id = ?", userID)
	return dbErr(err)
}

const refreshTokenColumns = `id, user_id, app_id, token_hash, scopes, device_id, expires_at, revoked, created_at`

type refreshTokenRepo struct{ db dbConn }

func scanRefreshToken(s rowScanner) (*domain.RefreshToken, error) {
	var t domain.RefreshToken
	var device sql.NullString
	if err := s.Scan(&t.ID, &t.UserID, &t.AppID, &t.TokenHash, &t.Scopes, &device, &t.ExpiresAt, &t.Revoked, &t.CreatedAt); err != nil {
		return nil, err
	}
	t.DeviceID = ptrString(device)
	t.Scopes = defaultJSONArr(t.Scopes)
	t.ExpiresAt = t.ExpiresAt.UTC()
	t.CreatedAt = t.CreatedAt.UTC()
	return &t, nil
}
func (r *refreshTokenRepo) FindByTokenHash(ctx context.Context, hash string) (*domain.RefreshToken, error) {
	t, err := scanRefreshToken(r.db.QueryRowContext(ctx, "SELECT "+refreshTokenColumns+" FROM auth_refresh_tokens WHERE token_hash = ?", hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return t, nil
}
func (r *refreshTokenRepo) Insert(ctx context.Context, t *domain.RefreshToken) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_refresh_tokens (id, user_id, app_id, token_hash, scopes, device_id, expires_at, revoked, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.ID, t.UserID, t.AppID, t.TokenHash, defaultJSONArr(t.Scopes), nullString(t.DeviceID), t.ExpiresAt.UTC(), t.Revoked, t.CreatedAt.UTC())
	return dbErr(err)
}
func (r *refreshTokenRepo) Revoke(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE auth_refresh_tokens SET revoked = TRUE WHERE id = ?", id)
	return dbErr(err)
}
func (r *refreshTokenRepo) DeleteAllByUser(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_refresh_tokens WHERE user_id = ?", userID)
	return dbErr(err)
}

const inviteCodeColumns = `id, code, created_by, created_at, used_at, used_by, is_revoked, kind, grants_membership, grants_membership_days, grants_user_type`

type inviteCodeRepo struct{ db dbConn }

func scanInviteCode(s rowScanner) (*domain.InviteCode, error) {
	var c domain.InviteCode
	var usedAt sql.NullTime
	var usedBy, kind, grants, grantsUserType sql.NullString
	var grantDays sql.NullInt64
	if err := s.Scan(&c.ID, &c.Code, &c.CreatedBy, &c.CreatedAt, &usedAt, &usedBy, &c.IsRevoked, &kind, &grants, &grantDays, &grantsUserType); err != nil {
		return nil, err
	}
	c.CreatedAt = c.CreatedAt.UTC()
	c.UsedAt = ptrTime(usedAt)
	c.UsedBy = ptrString(usedBy)
	c.Kind = domain.InviteKindFromString(kind.String)
	if grants.Valid && grants.String != "" {
		tier := domain.MembershipFromString(grants.String)
		c.GrantsMembership = &tier
	}
	if grantDays.Valid {
		v := grantDays.Int64
		c.GrantsMembershipDays = &v
	}
	if grantsUserType.Valid && grantsUserType.String != "" {
		t := domain.UserType(grantsUserType.String)
		c.GrantsUserType = normalizeInviteCodeGrantUserType(&t)
	}
	return &c, nil
}
func generateInviteCode() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	out := make([]byte, 16)
	for i, v := range b {
		out[i] = inviteAlphabet[int(v)%len(inviteAlphabet)]
	}
	return string(out)
}
func normalizeInviteCodeGrantUserType(t *domain.UserType) *domain.UserType {
	if t == nil {
		return nil
	}
	v := domain.UserTypeFromString(string(*t))
	if v == domain.UserTypeRegular {
		return nil
	}
	return &v
}

func userTypeString(t *domain.UserType) *string {
	normalized := normalizeInviteCodeGrantUserType(t)
	if normalized == nil {
		return nil
	}
	v := string(*normalized)
	return &v
}

func (r *inviteCodeRepo) Create(ctx context.Context, createdBy string, kind domain.InviteCodeKind, grants *domain.MembershipTier, grantDays *int64, grantsUserType *domain.UserType) (*domain.InviteCode, error) {
	if kind == "" {
		kind = domain.InviteSingleUse
	}
	c := &domain.InviteCode{ID: uuid.NewString(), Code: generateInviteCode(), CreatedBy: createdBy, CreatedAt: time.Now().UTC(), IsRevoked: false, Kind: kind, GrantsMembership: grants, GrantsMembershipDays: grantDays, GrantsUserType: normalizeInviteCodeGrantUserType(grantsUserType)}
	var grantsStr *string
	if grants != nil {
		v := string(*grants)
		grantsStr = &v
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_invite_codes (id, code, created_by, created_at, used_at, used_by, is_revoked, kind, grants_membership, grants_membership_days, grants_user_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, c.Code, c.CreatedBy, c.CreatedAt, nullTime(c.UsedAt), nullString(c.UsedBy), c.IsRevoked, string(c.Kind), nullString(grantsStr), nullInt64(grantDays), nullString(userTypeString(c.GrantsUserType)))
	if err != nil {
		return nil, dbErr(err)
	}
	return c, nil
}
func (r *inviteCodeRepo) GetByCode(ctx context.Context, code string) (*domain.InviteCode, error) {
	c, err := scanInviteCode(r.db.QueryRowContext(ctx, "SELECT "+inviteCodeColumns+" FROM auth_invite_codes WHERE code = ?", code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return c, nil
}
func (r *inviteCodeRepo) MarkUsed(ctx context.Context, code, userID string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, "UPDATE auth_invite_codes SET used_at = ?, used_by = ? WHERE code = ? AND used_at IS NULL AND is_revoked = FALSE", now, userID, code)
	if err != nil {
		return dbErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return dbErr(err)
	}
	if n == 1 {
		return nil
	}
	c, err := r.GetByCode(ctx, code)
	if err != nil {
		return err
	}
	if c == nil {
		return apperror.InviteCodeNotFound()
	}
	if c.IsRevoked {
		return apperror.InviteCodeNotFound()
	}
	return apperror.InviteCodeAlreadyUsed()
}
func (r *inviteCodeRepo) List(ctx context.Context, usedOnly *bool) ([]domain.InviteCode, error) {
	where := ""
	args := []any{}
	if usedOnly != nil {
		if *usedOnly {
			where = " WHERE used_at IS NOT NULL"
		} else {
			where = " WHERE used_at IS NULL"
		}
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+inviteCodeColumns+" FROM auth_invite_codes"+where+" ORDER BY created_at DESC", args...)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]domain.InviteCode, 0)
	for rows.Next() {
		c, err := scanInviteCode(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, *c)
	}
	return out, dbErr(rows.Err())
}
func (r *inviteCodeRepo) Revoke(ctx context.Context, code string) error {
	c, err := r.GetByCode(ctx, code)
	if err != nil {
		return err
	}
	if c == nil {
		return apperror.InviteCodeNotFound()
	}
	if c.UsedAt != nil {
		return apperror.InviteCodeAlreadyUsed()
	}
	_, err = r.db.ExecContext(ctx, "UPDATE auth_invite_codes SET is_revoked = TRUE WHERE code = ?", code)
	return dbErr(err)
}

const teamColumns = `id, name, description, owner_user_id, is_open, created_at, updated_at`

type teamRepo struct{ db dbConn }

func scanTeam(s rowScanner) (*domain.Team, error) {
	var t domain.Team
	var desc sql.NullString
	if err := s.Scan(&t.ID, &t.Name, &desc, &t.OwnerUserID, &t.IsOpen, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Description = ptrString(desc)
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	return &t, nil
}
func (r *teamRepo) FindByID(ctx context.Context, id string) (*domain.Team, error) {
	t, err := scanTeam(r.db.QueryRowContext(ctx, "SELECT "+teamColumns+" FROM auth_teams WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return t, nil
}
func (r *teamRepo) FindAllOpen(ctx context.Context) ([]domain.Team, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+teamColumns+" FROM auth_teams WHERE is_open = TRUE ORDER BY created_at DESC")
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]domain.Team, 0)
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, *t)
	}
	return out, dbErr(rows.Err())
}
func (r *teamRepo) FindAllOwnedByUser(ctx context.Context, userID string) ([]domain.Team, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+teamColumns+" FROM auth_teams WHERE owner_user_id = ? ORDER BY created_at DESC", userID)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]domain.Team, 0)
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, *t)
	}
	return out, dbErr(rows.Err())
}
func (r *teamRepo) Insert(ctx context.Context, t *domain.Team) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_teams (id, name, description, owner_user_id, is_open, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, t.ID, t.Name, nullString(t.Description), t.OwnerUserID, t.IsOpen, t.CreatedAt.UTC(), t.UpdatedAt.UTC())
	return dbErr(err)
}
func (r *teamRepo) Update(ctx context.Context, t *domain.Team) error {
	_, err := r.db.ExecContext(ctx, `UPDATE auth_teams SET name = ?, description = ?, owner_user_id = ?, is_open = ?, updated_at = ? WHERE id = ?`, t.Name, nullString(t.Description), t.OwnerUserID, t.IsOpen, t.UpdatedAt.UTC(), t.ID)
	return dbErr(err)
}
func (r *teamRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_teams WHERE id = ?", id)
	return dbErr(err)
}

const teamMembershipColumns = `team_id, user_id, role, joined_at`

type teamMembershipRepo struct{ db dbConn }

func scanTeamMembership(s rowScanner) (*domain.TeamMembership, error) {
	var m domain.TeamMembership
	if err := s.Scan(&m.TeamID, &m.UserID, &m.Role, &m.JoinedAt); err != nil {
		return nil, err
	}
	m.JoinedAt = m.JoinedAt.UTC()
	return &m, nil
}
func (r *teamMembershipRepo) FindAllByTeam(ctx context.Context, teamID string) ([]domain.TeamMembership, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+teamMembershipColumns+" FROM auth_team_memberships WHERE team_id = ? ORDER BY joined_at ASC", teamID)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]domain.TeamMembership, 0)
	for rows.Next() {
		m, err := scanTeamMembership(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, *m)
	}
	return out, dbErr(rows.Err())
}
func (r *teamMembershipRepo) FindAllByUser(ctx context.Context, userID string) ([]domain.TeamMembership, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+teamMembershipColumns+" FROM auth_team_memberships WHERE user_id = ? ORDER BY joined_at ASC", userID)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]domain.TeamMembership, 0)
	for rows.Next() {
		m, err := scanTeamMembership(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, *m)
	}
	return out, dbErr(rows.Err())
}
func (r *teamMembershipRepo) Find(ctx context.Context, teamID, userID string) (*domain.TeamMembership, error) {
	m, err := scanTeamMembership(r.db.QueryRowContext(ctx, "SELECT "+teamMembershipColumns+" FROM auth_team_memberships WHERE team_id = ? AND user_id = ?", teamID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return m, nil
}
func (r *teamMembershipRepo) Insert(ctx context.Context, m *domain.TeamMembership) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_team_memberships (team_id, user_id, role, joined_at) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE role = VALUES(role), joined_at = VALUES(joined_at)`, m.TeamID, m.UserID, m.Role, m.JoinedAt.UTC())
	return dbErr(err)
}
func (r *teamMembershipRepo) CountByTeam(ctx context.Context, teamID string) (uint64, error) {
	var n uint64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_team_memberships WHERE team_id = ?", teamID).Scan(&n)
	return n, dbErr(err)
}
func (r *teamMembershipRepo) Delete(ctx context.Context, teamID, userID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_team_memberships WHERE team_id = ? AND user_id = ?", teamID, userID)
	return dbErr(err)
}
func (r *teamMembershipRepo) DeleteAllByTeam(ctx context.Context, teamID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_team_memberships WHERE team_id = ?", teamID)
	return dbErr(err)
}
func (r *teamMembershipRepo) DeleteAllByUser(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM auth_team_memberships WHERE user_id = ?", userID)
	return dbErr(err)
}

// ReplaceWithSnapshot clears existing rows and imports the snapshot in one
// transaction. If any row fails to import, the target data is left unchanged.
func (r *Repository) ReplaceWithSnapshot(ctx context.Context, data snapshot.Data) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := clearTables(ctx, tx); err != nil {
		return fmt.Errorf("clear target: %w", err)
	}
	if err := importSnapshot(ctx, tx, data); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// ImportSnapshot inserts exported rows into an empty MySQL schema atomically.
func (r *Repository) ImportSnapshot(ctx context.Context, data snapshot.Data) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := importSnapshot(ctx, tx, data); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func importSnapshot(ctx context.Context, db dbConn, data snapshot.Data) error {
	apps := &appRepo{db: db}
	users := &userRepo{db: db}
	appProviders := &appProviderRepo{db: db}
	accounts := &accountRepo{db: db}
	authCodes := &authCodeRepo{db: db}
	refreshTokens := &refreshTokenRepo{db: db}
	teams := &teamRepo{db: db}
	teamMemberships := &teamMembershipRepo{db: db}

	for i := range data.Applications {
		if err := apps.Insert(ctx, &data.Applications[i]); err != nil {
			return fmt.Errorf("applications: %w", err)
		}
	}
	for i := range data.Users {
		if err := users.Insert(ctx, &data.Users[i]); err != nil {
			return fmt.Errorf("users: %w", err)
		}
	}
	for i := range data.AppProviders {
		if err := appProviders.Insert(ctx, &data.AppProviders[i]); err != nil {
			return fmt.Errorf("app_providers: %w", err)
		}
	}
	for i := range data.Accounts {
		if err := accounts.Insert(ctx, &data.Accounts[i]); err != nil {
			return fmt.Errorf("accounts: %w", err)
		}
	}
	for i := range data.AuthCodes {
		if err := authCodes.Insert(ctx, &data.AuthCodes[i]); err != nil {
			return fmt.Errorf("auth_codes: %w", err)
		}
	}
	for i := range data.RefreshTokens {
		if err := refreshTokens.Insert(ctx, &data.RefreshTokens[i]); err != nil {
			return fmt.Errorf("refresh_tokens: %w", err)
		}
	}
	for i := range data.InviteCodes {
		if err := insertInviteCode(ctx, db, &data.InviteCodes[i]); err != nil {
			return fmt.Errorf("invite_codes: %w", err)
		}
	}
	for i := range data.Teams {
		if err := teams.Insert(ctx, &data.Teams[i]); err != nil {
			return fmt.Errorf("teams: %w", err)
		}
	}
	for i := range data.TeamMemberships {
		if err := teamMemberships.Insert(ctx, &data.TeamMemberships[i]); err != nil {
			return fmt.Errorf("team_memberships: %w", err)
		}
	}
	return nil
}

func insertInviteCode(ctx context.Context, db dbConn, c *domain.InviteCode) error {
	var grants *string
	if c.GrantsMembership != nil {
		v := string(*c.GrantsMembership)
		grants = &v
	}
	kind := c.Kind
	if kind == "" {
		kind = domain.InviteSingleUse
	}
	_, err := db.ExecContext(ctx, `INSERT INTO auth_invite_codes (id, code, created_by, created_at, used_at, used_by, is_revoked, kind, grants_membership, grants_membership_days, grants_user_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, c.Code, c.CreatedBy, c.CreatedAt.UTC(), nullTime(c.UsedAt), nullString(c.UsedBy), c.IsRevoked, string(kind), nullString(grants), nullInt64(c.GrantsMembershipDays), nullString(userTypeString(c.GrantsUserType)))
	return dbErr(err)
}

var _ repository.Repository = (*Repository)(nil)
