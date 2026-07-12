// Package aztables implements the repository interfaces against Azure Table
// Storage. It preserves the production storage contract: table names,
// PartitionKey/RowKey schemes, secondary index rows, and ETag-based atomic
// invite-code consumption.
package aztables

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
	"github.com/google/uuid"
	pinyin "github.com/mozillazg/go-pinyin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/repository"
	"github.com/zhaochy1990/auth-service/internal/repository/snapshot"
)

// ─── Table names (prefixed for a shared storage account) ─────────────────────

const (
	tableApplications    = "authapplications"
	tableUsers           = "authusers"
	tableUserSortIndexes = "authusersortindexes"
	tableAccounts        = "authaccounts"
	tableAppProviders    = "authappproviders"
	tableAuthCodes       = "authauthcodes"
	tableRefreshTokens   = "authrefreshtokens"
	tableInviteCodes     = "authinvitecodes"
	tableTeams           = "authteams"
	tableTeamMemberships = "authteammemberships"
)

// ─── DateTime helpers ────────────────────────────────────────────────────────

const dtStorageLayout = "2006-01-02T15:04:05.000000"

func fmtDT(t time.Time) string { return t.UTC().Format(dtStorageLayout) }

var dtParseLayouts = []string{
	"2006-01-02T15:04:05.000000",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

func parseDT(s string) time.Time {
	for _, l := range dtParseLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func fmtDTPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := fmtDT(*t)
	return &s
}

func parseDTPtr(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t := parseDT(*s)
	return &t
}

// ─── Error helpers ───────────────────────────────────────────────────────────

func statusOf(err error) (int, string) {
	var re *azcore.ResponseError
	if errors.As(err, &re) {
		return re.StatusCode, re.ErrorCode
	}
	return 0, ""
}

func isNotFound(err error) bool { s, _ := statusOf(err); return s == http.StatusNotFound }
func isConflict(err error) bool { s, _ := statusOf(err); return s == http.StatusConflict }
func isPreconditionFailed(err error) bool {
	s, _ := statusOf(err)
	return s == http.StatusPreconditionFailed
}

func dbErr(err error) error { return apperror.Database(err.Error()) }

// ─── id / code generation ────────────────────────────────────────────────────

func newUUID() string { return uuid.NewString() }

const inviteAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// generateInviteCode returns a 16-character URL-safe alphanumeric code.
func generateInviteCode() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	out := make([]byte, 16)
	for i, v := range b {
		out[i] = inviteAlphabet[int(v)%len(inviteAlphabet)]
	}
	return string(out)
}

// ─── Generic table helpers ───────────────────────────────────────────────────

// addEntity inserts an entity, returning the raw SDK error so callers can
// inspect conflicts.
func addEntity(ctx context.Context, c *aztables.Client, entity any) error {
	b, err := json.Marshal(entity)
	if err != nil {
		return err
	}
	_, err = c.AddEntity(ctx, b, nil)
	return err
}

// getEntity fetches an entity into out. The bool reports existence.
func getEntity(ctx context.Context, c *aztables.Client, pk, rk string, out any) (bool, error) {
	resp, err := c.GetEntity(ctx, pk, rk, nil)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, dbErr(err)
	}
	if err := json.Unmarshal(resp.Value, out); err != nil {
		return false, dbErr(err)
	}
	return true, nil
}

func upsertEntity(ctx context.Context, c *aztables.Client, entity any) error {
	b, err := json.Marshal(entity)
	if err != nil {
		return dbErr(err)
	}
	mode := aztables.UpdateModeReplace
	_, err = c.UpsertEntity(ctx, b, &aztables.UpsertEntityOptions{UpdateMode: mode})
	if err != nil {
		return dbErr(err)
	}
	return nil
}

func deleteEntity(ctx context.Context, c *aztables.Client, pk, rk string) error {
	star := azcore.ETag("*")
	_, err := c.DeleteEntity(ctx, pk, rk, &aztables.DeleteEntityOptions{IfMatch: &star})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return dbErr(err)
	}
	return nil
}

// queryEntities pages through a filter, decoding each entity into a fresh T.
func queryEntities[T any](ctx context.Context, c *aztables.Client, filter string) ([]T, error) {
	f := filter
	pager := c.NewListEntitiesPager(&aztables.ListEntitiesOptions{Filter: &f})
	var out []T
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, dbErr(err)
		}
		for _, raw := range page.Entities {
			var v T
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, dbErr(err)
			}
			out = append(out, v)
		}
	}
	return out, nil
}

// ─── Index entities ──────────────────────────────────────────────────────────

type indexEntity struct {
	PartitionKey string `json:"PartitionKey"`
	RowKey       string `json:"RowKey"`
	TargetID     string `json:"target_id"`
}

const (
	userSortNameAscPK       = "idx_user_sort_name_asc"
	userSortNameDescPK      = "idx_user_sort_name_desc"
	userSortLastLoginAscPK  = "idx_user_sort_last_login_at_asc"
	userSortLastLoginDescPK = "idx_user_sort_last_login_at_desc"
	userSortKeyMaxBytes     = 400
)

type compositeIndexEntity struct {
	PartitionKey string `json:"PartitionKey"`
	RowKey       string `json:"RowKey"`
	PK           string `json:"pk"`
	RK           string `json:"rk"`
}

func providerAccountIndexPK(providerID string) string {
	return "idx_pa_" + hex.EncodeToString([]byte(providerID))
}

func boolPtr(b bool) *bool { return &b }

func defaultUserType(t domain.UserType) domain.UserType {
	if t.Valid() {
		return t
	}
	return domain.UserTypeRegular
}

// boolOr defaults a possibly-absent stored bool.
func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func eqStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ─── Repository ──────────────────────────────────────────────────────────────

// Repository is the Azure Tables implementation of repository.Repository.
type Repository struct {
	svc *aztables.ServiceClient

	applications    *aztables.Client
	users           *aztables.Client
	userSortIndexes *aztables.Client
	accounts        *aztables.Client
	appProviders    *aztables.Client
	authCodes       *aztables.Client
	refreshTokens   *aztables.Client
	inviteCodes     *aztables.Client
	teams           *aztables.Client
	teamMemberships *aztables.Client

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

// New builds a Repository from an Azure Storage connection string (supports the
// Azurite emulator via the TableEndpoint in the connection string).
func New(connectionString string) (*Repository, error) {
	svc, err := aztables.NewServiceClientFromConnectionString(connectionString, nil)
	if err != nil {
		return nil, err
	}
	r := &Repository{
		svc:             svc,
		applications:    svc.NewClient(tableApplications),
		users:           svc.NewClient(tableUsers),
		userSortIndexes: svc.NewClient(tableUserSortIndexes),
		accounts:        svc.NewClient(tableAccounts),
		appProviders:    svc.NewClient(tableAppProviders),
		authCodes:       svc.NewClient(tableAuthCodes),
		refreshTokens:   svc.NewClient(tableRefreshTokens),
		inviteCodes:     svc.NewClient(tableInviteCodes),
		teams:           svc.NewClient(tableTeams),
		teamMemberships: svc.NewClient(tableTeamMemberships),
	}
	r.userRepo = &userRepo{c: r.users, sortIndexes: r.userSortIndexes}
	r.appRepo = &appRepo{c: r.applications}
	r.accountRepo = &accountRepo{c: r.accounts}
	r.appProvRepo = &appProviderRepo{c: r.appProviders}
	r.authCodeRepo = &authCodeRepo{c: r.authCodes}
	r.refreshRepo = &refreshTokenRepo{c: r.refreshTokens}
	r.inviteRepo = &inviteCodeRepo{c: r.inviteCodes}
	r.teamRepo = &teamRepo{c: r.teams}
	r.membershipRepo = &teamMembershipRepo{c: r.teamMemberships}
	return r, nil
}

func (r *Repository) allTables() []*aztables.Client {
	return []*aztables.Client{
		r.applications, r.users, r.userSortIndexes, r.accounts, r.appProviders, r.authCodes,
		r.refreshTokens, r.inviteCodes, r.teams, r.teamMemberships,
	}
}

// EnsureTables creates every table, ignoring "already exists".
func (r *Repository) EnsureTables(ctx context.Context) error {
	for _, t := range r.allTables() {
		if err := createTableRetry(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

func createTableRetry(ctx context.Context, t *aztables.Client) error {
	var lastErr error
	for i := 0; i < 30; i++ {
		_, err := t.CreateTable(ctx, nil)
		if err == nil {
			return nil
		}
		if isConflict(err) {
			if _, code := statusOf(err); code == "TableAlreadyExists" {
				return nil
			}
			// e.g. TableBeingDeleted — wait and retry.
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return err
	}
	return lastErr
}

// ClearAllTables deletes and recreates every table (test isolation only).
func (r *Repository) ClearAllTables(ctx context.Context) error {
	for _, t := range r.allTables() {
		_, _ = t.Delete(ctx, nil) // ignore if missing
	}
	return r.EnsureTables(ctx)
}

// ExportSnapshot returns only primary domain rows, excluding Azure Table
// secondary-index rows such as idx_email, idx_hash, and idx_id.
func (r *Repository) ExportSnapshot(ctx context.Context) (*snapshot.Data, error) {
	apps, err := queryEntities[appEntity](ctx, r.applications, "PartitionKey eq 'app'")
	if err != nil {
		return nil, err
	}
	users, err := queryEntities[userEntity](ctx, r.users, "PartitionKey eq 'user'")
	if err != nil {
		return nil, err
	}
	accounts, err := queryAllEntities[accountEntity](ctx, r.accounts)
	if err != nil {
		return nil, err
	}
	appProviders, err := queryAllEntities[appProviderEntity](ctx, r.appProviders)
	if err != nil {
		return nil, err
	}
	authCodes, err := queryEntities[authCodeEntity](ctx, r.authCodes, "PartitionKey eq 'code'")
	if err != nil {
		return nil, err
	}
	refreshTokens, err := queryEntities[refreshTokenEntity](ctx, r.refreshTokens, "PartitionKey eq 'rt'")
	if err != nil {
		return nil, err
	}
	inviteCodes, err := queryEntities[inviteCodeEntity](ctx, r.inviteCodes, "PartitionKey eq 'invite_code'")
	if err != nil {
		return nil, err
	}
	teams, err := queryEntities[teamEntity](ctx, r.teams, "PartitionKey eq 'team'")
	if err != nil {
		return nil, err
	}
	memberships, err := queryAllEntities[teamMembershipEntity](ctx, r.teamMemberships)
	if err != nil {
		return nil, err
	}

	out := &snapshot.Data{}
	for i := range apps {
		out.Applications = append(out.Applications, *apps[i].toModel())
	}
	for i := range users {
		out.Users = append(out.Users, *users[i].toModel())
	}
	for i := range accounts {
		if strings.HasPrefix(accounts[i].PartitionKey, "idx_") {
			continue
		}
		out.Accounts = append(out.Accounts, *accounts[i].toModel())
	}
	for i := range appProviders {
		if strings.HasPrefix(appProviders[i].PartitionKey, "idx_") {
			continue
		}
		out.AppProviders = append(out.AppProviders, *appProviders[i].toModel())
	}
	for i := range authCodes {
		out.AuthCodes = append(out.AuthCodes, *authCodes[i].toModel())
	}
	for i := range refreshTokens {
		out.RefreshTokens = append(out.RefreshTokens, *refreshTokens[i].toModel())
	}
	for i := range inviteCodes {
		out.InviteCodes = append(out.InviteCodes, *inviteCodes[i].toModel())
	}
	for i := range teams {
		out.Teams = append(out.Teams, *teams[i].toModel())
	}
	for i := range memberships {
		out.TeamMemberships = append(out.TeamMemberships, *memberships[i].toModel())
	}
	return out, nil
}

func queryAllEntities[T any](ctx context.Context, c *aztables.Client) ([]T, error) {
	pager := c.NewListEntitiesPager(nil)
	var out []T
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, dbErr(err)
		}
		for _, raw := range page.Entities {
			var v T
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, dbErr(err)
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func (r *Repository) Users() repository.UserRepository                     { return r.userRepo }
func (r *Repository) Applications() repository.ApplicationRepository       { return r.appRepo }
func (r *Repository) Accounts() repository.AccountRepository               { return r.accountRepo }
func (r *Repository) AppProviders() repository.AppProviderRepository       { return r.appProvRepo }
func (r *Repository) AuthCodes() repository.AuthCodeRepository             { return r.authCodeRepo }
func (r *Repository) RefreshTokens() repository.RefreshTokenRepository     { return r.refreshRepo }
func (r *Repository) InviteCodes() repository.InviteCodeRepository         { return r.inviteRepo }
func (r *Repository) Teams() repository.TeamRepository                     { return r.teamRepo }
func (r *Repository) TeamMemberships() repository.TeamMembershipRepository { return r.membershipRepo }

// ─── User ────────────────────────────────────────────────────────────────────

type loginPersist struct {
	At string `json:"at"`
	IP string `json:"ip"`
}

type userEntity struct {
	PartitionKey        string  `json:"PartitionKey"`
	RowKey              string  `json:"RowKey"`
	Email               *string `json:"email,omitempty"`
	Name                *string `json:"name,omitempty"`
	AvatarURL           *string `json:"avatar_url,omitempty"`
	EmailVerified       bool    `json:"email_verified"`
	Role                string  `json:"role"`
	UserType            string  `json:"user_type"`
	IsActive            *bool   `json:"is_active,omitempty"`
	Note                *string `json:"note,omitempty"`
	CustomAttributes    string  `json:"custom_attributes"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
	LastLoginAt         *string `json:"last_login_at,omitempty"`
	RecentLogins        *string `json:"recent_logins,omitempty"`
	InviteCode          *string `json:"invite_code,omitempty"`
	Membership          string  `json:"membership"`
	MembershipExpiresAt *string `json:"membership_expires_at,omitempty"`
}

func serializeLogins(records []domain.LoginRecord) *string {
	if len(records) == 0 {
		return nil
	}
	persist := make([]loginPersist, 0, len(records))
	for _, r := range records {
		persist = append(persist, loginPersist{At: fmtDT(r.At), IP: r.IP})
	}
	b, err := json.Marshal(persist)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

func deserializeLogins(s *string) []domain.LoginRecord {
	if s == nil {
		return nil
	}
	var persist []loginPersist
	if err := json.Unmarshal([]byte(*s), &persist); err != nil {
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

func deserializeCustomAttributes(s string) map[string]any {
	if s == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func userToEntity(u *domain.User) userEntity {
	membership := string(u.Membership)
	if membership == "" {
		membership = string(domain.MembershipRegular)
	}
	userType := defaultUserType(u.UserType)
	role := u.Role
	if role == "" {
		role = "user"
	}
	return userEntity{
		PartitionKey:        "user",
		RowKey:              u.ID,
		Email:               u.Email,
		Name:                u.Name,
		AvatarURL:           u.AvatarURL,
		EmailVerified:       u.EmailVerified,
		Role:                role,
		UserType:            string(userType),
		IsActive:            boolPtr(u.IsActive),
		Note:                u.Note,
		CustomAttributes:    serializeCustomAttributes(u.CustomAttributes),
		CreatedAt:           fmtDT(u.CreatedAt),
		UpdatedAt:           fmtDT(u.UpdatedAt),
		LastLoginAt:         fmtDTPtr(u.LastLoginAt),
		RecentLogins:        serializeLogins(u.RecentLogins),
		InviteCode:          u.InviteCode,
		Membership:          membership,
		MembershipExpiresAt: fmtDTPtr(u.MembershipExpiresAt),
	}
}

func (e *userEntity) toModel() *domain.User {
	role := e.Role
	if role == "" {
		role = "user"
	}
	membership := e.Membership
	if membership == "" {
		membership = string(domain.MembershipRegular)
	}
	return &domain.User{
		ID:                  e.RowKey,
		Email:               e.Email,
		Name:                e.Name,
		AvatarURL:           e.AvatarURL,
		EmailVerified:       e.EmailVerified,
		Role:                role,
		UserType:            domain.UserTypeFromString(e.UserType),
		IsActive:            boolOr(e.IsActive, true),
		Note:                e.Note,
		CustomAttributes:    deserializeCustomAttributes(e.CustomAttributes),
		CreatedAt:           parseDT(e.CreatedAt),
		UpdatedAt:           parseDT(e.UpdatedAt),
		LastLoginAt:         parseDTPtr(e.LastLoginAt),
		RecentLogins:        deserializeLogins(e.RecentLogins),
		InviteCode:          e.InviteCode,
		Membership:          domain.MembershipFromString(membership),
		MembershipExpiresAt: parseDTPtr(e.MembershipExpiresAt),
	}
}

type userRepo struct {
	c           *aztables.Client
	sortIndexes *aztables.Client
}

func (r *userRepo) FindByID(ctx context.Context, id string) (*domain.User, error) {
	var e userEntity
	ok, err := getEntity(ctx, r.c, "user", id, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var idx indexEntity
	ok, err := getEntity(ctx, r.c, "idx_email", strings.ToLower(email), &idx)
	if err != nil || !ok {
		return nil, err
	}
	return r.FindByID(ctx, idx.TargetID)
}

func (r *userRepo) Insert(ctx context.Context, u *domain.User) error {
	if u.Email != nil {
		idx := indexEntity{PartitionKey: "idx_email", RowKey: strings.ToLower(*u.Email), TargetID: u.ID}
		if err := addEntity(ctx, r.c, &idx); err != nil {
			if isConflict(err) {
				return apperror.Database("Email already exists")
			}
			return dbErr(err)
		}
	}
	e := userToEntity(u)
	if err := addEntity(ctx, r.c, &e); err != nil {
		return dbErr(err)
	}
	if err := r.upsertSortIndexes(ctx, &e); err != nil {
		_ = r.deleteSortIndexes(ctx, &e)
		_ = deleteEntity(ctx, r.c, "user", u.ID)
		if u.Email != nil {
			_ = deleteEntity(ctx, r.c, "idx_email", strings.ToLower(*u.Email))
		}
		return err
	}
	return nil
}

func (r *userRepo) Update(ctx context.Context, u *domain.User) error {
	var current userEntity
	ok, err := getEntity(ctx, r.c, "user", u.ID, &current)
	if err != nil {
		return err
	}
	if ok {
		if err := r.deleteSortIndexes(ctx, &current); err != nil {
			return err
		}

		var oldEmail, newEmail *string
		if current.Email != nil {
			v := strings.ToLower(*current.Email)
			oldEmail = &v
		}
		if u.Email != nil {
			v := strings.ToLower(*u.Email)
			newEmail = &v
		}
		if !eqStrPtr(oldEmail, newEmail) {
			if oldEmail != nil {
				if err := deleteEntity(ctx, r.c, "idx_email", *oldEmail); err != nil {
					return err
				}
			}
			if newEmail != nil {
				idx := indexEntity{PartitionKey: "idx_email", RowKey: *newEmail, TargetID: u.ID}
				if err := upsertEntity(ctx, r.c, &idx); err != nil {
					return err
				}
			}
		}
	}
	e := userToEntity(u)
	if err := upsertEntity(ctx, r.c, &e); err != nil {
		return err
	}
	return r.upsertSortIndexes(ctx, &e)
}

func (r *userRepo) DeleteByID(ctx context.Context, id string) error {
	var e userEntity
	ok, err := getEntity(ctx, r.c, "user", id, &e)
	if err != nil {
		return err
	}
	if ok {
		if err := r.deleteSortIndexes(ctx, &e); err != nil {
			return err
		}
		if e.Email != nil {
			if err := deleteEntity(ctx, r.c, "idx_email", strings.ToLower(*e.Email)); err != nil {
				return err
			}
		}
	}
	return deleteEntity(ctx, r.c, "user", id)
}

func (r *userRepo) CountAll(ctx context.Context) (uint64, error) {
	es, err := queryEntities[userEntity](ctx, r.c, "PartitionKey eq 'user'")
	if err != nil {
		return 0, err
	}
	return uint64(len(es)), nil
}

func (r *userRepo) CountSince(ctx context.Context, since time.Time) (uint64, error) {
	es, err := queryEntities[userEntity](ctx, r.c, "PartitionKey eq 'user'")
	if err != nil {
		return 0, err
	}
	sinceStr := fmtDT(since)
	var n uint64
	for _, e := range es {
		if e.CreatedAt >= sinceStr {
			n++
		}
	}
	return n, nil
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
	return r.Update(ctx, u)
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

func userSortNameKey(e userEntity) []byte {
	if e.Name != nil && strings.TrimSpace(*e.Name) != "" {
		return []byte(normalizeUserSortName(*e.Name))
	}
	if e.Email != nil {
		return []byte(normalizeUserSortName(*e.Email))
	}
	return nil
}

func userLastLoginKey(e userEntity) []byte {
	if e.LastLoginAt == nil {
		return []byte("0")
	}
	return []byte("1" + *e.LastLoginAt)
}

func sortableRowKey(key []byte, targetID string, descending bool) string {
	if len(key) > userSortKeyMaxBytes {
		key = key[:userSortKeyMaxBytes]
	}
	buf := make([]byte, 0, len(key)+1+len(targetID))
	buf = append(buf, key...)
	buf = append(buf, 0)
	buf = append(buf, []byte(targetID)...)
	if descending {
		for i := range buf {
			buf[i] = 255 - buf[i]
		}
	}
	return hex.EncodeToString(buf)
}

func userSortIndexes(e userEntity) []indexEntity {
	id := e.RowKey
	return []indexEntity{
		{PartitionKey: userSortNameAscPK, RowKey: sortableRowKey(userSortNameKey(e), id, false), TargetID: id},
		{PartitionKey: userSortNameDescPK, RowKey: sortableRowKey(userSortNameKey(e), id, true), TargetID: id},
		{PartitionKey: userSortLastLoginAscPK, RowKey: sortableRowKey(userLastLoginKey(e), id, false), TargetID: id},
		{PartitionKey: userSortLastLoginDescPK, RowKey: sortableRowKey(userLastLoginKey(e), id, true), TargetID: id},
	}
}

func userSortPartition(sortSpec repository.UserListSort) string {
	switch {
	case sortSpec.By == repository.UserListSortByName && sortSpec.Order == repository.SortOrderDesc:
		return userSortNameDescPK
	case sortSpec.By == repository.UserListSortByLastLoginAt && sortSpec.Order == repository.SortOrderAsc:
		return userSortLastLoginAscPK
	case sortSpec.By == repository.UserListSortByLastLoginAt && sortSpec.Order == repository.SortOrderDesc:
		return userSortLastLoginDescPK
	default:
		return userSortNameAscPK
	}
}

func userSortPartitions() []string {
	return []string{
		userSortNameAscPK,
		userSortNameDescPK,
		userSortLastLoginAscPK,
		userSortLastLoginDescPK,
	}
}

func (r *userRepo) upsertSortIndexes(ctx context.Context, e *userEntity) error {
	for _, idx := range userSortIndexes(*e) {
		if err := upsertEntity(ctx, r.sortIndexes, &idx); err != nil {
			return err
		}
	}
	return nil
}

func (r *userRepo) deleteSortIndexes(ctx context.Context, e *userEntity) error {
	for _, idx := range userSortIndexes(*e) {
		if err := deleteEntity(ctx, r.sortIndexes, idx.PartitionKey, idx.RowKey); err != nil {
			return err
		}
	}
	return nil
}

func (r *userRepo) ensureSortIndexes(ctx context.Context) error {
	users, err := queryEntities[userEntity](ctx, r.c, "PartitionKey eq 'user'")
	if err != nil {
		return err
	}
	for _, partition := range userSortPartitions() {
		indexed, err := queryEntities[indexEntity](ctx, r.sortIndexes, "PartitionKey eq '"+partition+"'")
		if err != nil {
			return err
		}
		if len(indexed) < len(users) {
			for i := range users {
				if err := r.upsertSortIndexes(ctx, &users[i]); err != nil {
					return err
				}
			}
			return nil
		}
	}

	return nil
}

func matchesUserType(e *userEntity, userType *domain.UserType) bool {
	if userType == nil {
		return true
	}
	return domain.UserTypeFromString(e.UserType) == defaultUserType(*userType)
}

func matchesUserSearch(e *userEntity, lower string) bool {
	if lower == "" {
		return true
	}
	emailMatch := e.Email != nil && strings.Contains(strings.ToLower(*e.Email), lower)
	nameMatch := e.Name != nil && strings.Contains(strings.ToLower(*e.Name), lower)
	return emailMatch || nameMatch
}

func (r *userRepo) ListPaginated(ctx context.Context, search string, userType *domain.UserType, sortSpec repository.UserListSort, offset, limit uint64) ([]domain.User, uint64, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if err := r.ensureSortIndexes(ctx); err != nil {
		return nil, 0, err
	}

	indexes, err := queryEntities[indexEntity](ctx, r.sortIndexes, "PartitionKey eq '"+userSortPartition(sortSpec)+"'")
	if err != nil {
		return nil, 0, err
	}

	lower := strings.ToLower(strings.TrimSpace(search))
	if lower == "" && userType == nil {
		return r.listUnfilteredPage(ctx, indexes, offset, limit)
	}

	out := make([]domain.User, 0)
	end := offset + limit
	if end < offset {
		end = ^uint64(0)
	}
	var total uint64

	for _, idx := range indexes {
		var e userEntity
		ok, err := getEntity(ctx, r.c, "user", idx.TargetID, &e)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			continue
		}
		if !matchesUserType(&e, userType) || !matchesUserSearch(&e, lower) {
			continue
		}
		if total >= offset && total < end {
			out = append(out, *e.toModel())
		}
		total++
	}

	return out, total, nil
}

func (r *userRepo) listUnfilteredPage(ctx context.Context, indexes []indexEntity, offset, limit uint64) ([]domain.User, uint64, error) {
	total := uint64(len(indexes))
	if offset >= total {
		return []domain.User{}, total, nil
	}

	end := offset + limit
	if end < offset || end > total {
		end = total
	}

	out := make([]domain.User, 0, end-offset)
	for _, idx := range indexes[int(offset):int(end)] {
		var e userEntity
		ok, err := getEntity(ctx, r.c, "user", idx.TargetID, &e)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			continue
		}
		out = append(out, *e.toModel())
	}

	return out, total, nil
}

func (r *userRepo) migrateSortIndexes(ctx context.Context) (int, error) {
	es, err := queryEntities[userEntity](ctx, r.c, "PartitionKey eq 'user'")
	if err != nil {
		return 0, err
	}
	for i := range es {
		if err := r.upsertSortIndexes(ctx, &es[i]); err != nil {
			return i, err
		}
	}
	return len(es), nil
}

// ─── Application ─────────────────────────────────────────────────────────────

type appEntity struct {
	PartitionKey     string `json:"PartitionKey"`
	RowKey           string `json:"RowKey"`
	Name             string `json:"name"`
	ClientID         string `json:"client_id"`
	ClientSecretHash string `json:"client_secret_hash"`
	RedirectURIs     string `json:"redirect_uris"`
	AllowedScopes    string `json:"allowed_scopes"`
	IsActive         *bool  `json:"is_active,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

func appToEntity(a *domain.Application) appEntity {
	return appEntity{
		PartitionKey: "app", RowKey: a.ID, Name: a.Name, ClientID: a.ClientID,
		ClientSecretHash: a.ClientSecretHash, RedirectURIs: a.RedirectURIs,
		AllowedScopes: a.AllowedScopes, IsActive: boolPtr(a.IsActive),
		CreatedAt: fmtDT(a.CreatedAt), UpdatedAt: fmtDT(a.UpdatedAt),
	}
}

func (e *appEntity) toModel() *domain.Application {
	return &domain.Application{
		ID: e.RowKey, Name: e.Name, ClientID: e.ClientID,
		ClientSecretHash: e.ClientSecretHash, RedirectURIs: e.RedirectURIs,
		AllowedScopes: e.AllowedScopes, IsActive: boolOr(e.IsActive, false),
		CreatedAt: parseDT(e.CreatedAt), UpdatedAt: parseDT(e.UpdatedAt),
	}
}

type appRepo struct{ c *aztables.Client }

func (r *appRepo) FindByID(ctx context.Context, id string) (*domain.Application, error) {
	var e appEntity
	ok, err := getEntity(ctx, r.c, "app", id, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *appRepo) FindByClientID(ctx context.Context, clientID string) (*domain.Application, error) {
	var idx indexEntity
	ok, err := getEntity(ctx, r.c, "idx_clientid", clientID, &idx)
	if err != nil || !ok {
		return nil, err
	}
	return r.FindByID(ctx, idx.TargetID)
}

func (r *appRepo) FindByName(ctx context.Context, name string) (*domain.Application, error) {
	var idx indexEntity
	ok, err := getEntity(ctx, r.c, "idx_name", name, &idx)
	if err != nil || !ok {
		return nil, err
	}
	return r.FindByID(ctx, idx.TargetID)
}

func (r *appRepo) FindAll(ctx context.Context) ([]domain.Application, error) {
	es, err := queryEntities[appEntity](ctx, r.c, "PartitionKey eq 'app'")
	if err != nil {
		return nil, err
	}
	out := make([]domain.Application, 0, len(es))
	for i := range es {
		out = append(out, *es[i].toModel())
	}
	return out, nil
}

func (r *appRepo) Insert(ctx context.Context, a *domain.Application) error {
	cidIdx := indexEntity{PartitionKey: "idx_clientid", RowKey: a.ClientID, TargetID: a.ID}
	if err := addEntity(ctx, r.c, &cidIdx); err != nil {
		if isConflict(err) {
			return apperror.Database("Client ID already exists")
		}
		return dbErr(err)
	}
	nameIdx := indexEntity{PartitionKey: "idx_name", RowKey: a.Name, TargetID: a.ID}
	_ = addEntity(ctx, r.c, &nameIdx) // best-effort

	e := appToEntity(a)
	if err := addEntity(ctx, r.c, &e); err != nil {
		return dbErr(err)
	}
	return nil
}

func (r *appRepo) Update(ctx context.Context, a *domain.Application) error {
	var current appEntity
	ok, err := getEntity(ctx, r.c, "app", a.ID, &current)
	if err != nil {
		return err
	}
	if ok && current.Name != a.Name {
		if err := deleteEntity(ctx, r.c, "idx_name", current.Name); err != nil {
			return err
		}
		idx := indexEntity{PartitionKey: "idx_name", RowKey: a.Name, TargetID: a.ID}
		if err := upsertEntity(ctx, r.c, &idx); err != nil {
			return err
		}
	}
	e := appToEntity(a)
	return upsertEntity(ctx, r.c, &e)
}

func (r *appRepo) CountAll(ctx context.Context) (uint64, error) {
	es, err := queryEntities[appEntity](ctx, r.c, "PartitionKey eq 'app'")
	if err != nil {
		return 0, err
	}
	return uint64(len(es)), nil
}

func (r *appRepo) CountActive(ctx context.Context) (uint64, error) {
	es, err := queryEntities[appEntity](ctx, r.c, "PartitionKey eq 'app'")
	if err != nil {
		return 0, err
	}
	var n uint64
	for _, e := range es {
		if boolOr(e.IsActive, false) {
			n++
		}
	}
	return n, nil
}

// ─── Account ─────────────────────────────────────────────────────────────────

type accountEntity struct {
	PartitionKey      string  `json:"PartitionKey"` // user_id
	RowKey            string  `json:"RowKey"`       // provider_id
	ID                string  `json:"id"`
	ProviderAccountID *string `json:"provider_account_id,omitempty"`
	Credential        *string `json:"credential,omitempty"`
	ProviderMetadata  string  `json:"provider_metadata"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

func accountToEntity(a *domain.Account) accountEntity {
	meta := a.ProviderMetadata
	if meta == "" {
		meta = "{}"
	}
	return accountEntity{
		PartitionKey: a.UserID, RowKey: a.ProviderID, ID: a.ID,
		ProviderAccountID: a.ProviderAccountID, Credential: a.Credential,
		ProviderMetadata: meta, CreatedAt: fmtDT(a.CreatedAt), UpdatedAt: fmtDT(a.UpdatedAt),
	}
}

func (e *accountEntity) toModel() *domain.Account {
	meta := e.ProviderMetadata
	if meta == "" {
		meta = "{}"
	}
	return &domain.Account{
		ID: e.ID, UserID: e.PartitionKey, ProviderID: e.RowKey,
		ProviderAccountID: e.ProviderAccountID, Credential: e.Credential,
		ProviderMetadata: meta, CreatedAt: parseDT(e.CreatedAt), UpdatedAt: parseDT(e.UpdatedAt),
	}
}

type accountRepo struct{ c *aztables.Client }

func (r *accountRepo) FindByUserAndProvider(ctx context.Context, userID, providerID string) (*domain.Account, error) {
	var e accountEntity
	ok, err := getEntity(ctx, r.c, userID, providerID, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *accountRepo) FindByProviderAccount(ctx context.Context, providerID, providerAccountID string) (*domain.Account, error) {
	var idx compositeIndexEntity
	ok, err := getEntity(ctx, r.c, providerAccountIndexPK(providerID), providerAccountID, &idx)
	if err != nil || !ok {
		return nil, err
	}
	return r.FindByUserAndProvider(ctx, idx.PK, idx.RK)
}

func (r *accountRepo) FindAllByUser(ctx context.Context, userID string) ([]domain.Account, error) {
	es, err := queryEntities[accountEntity](ctx, r.c, "PartitionKey eq '"+userID+"'")
	if err != nil {
		return nil, err
	}
	out := make([]domain.Account, 0, len(es))
	for i := range es {
		out = append(out, *es[i].toModel())
	}
	return out, nil
}

func (r *accountRepo) CountByUser(ctx context.Context, userID string) (uint64, error) {
	accounts, err := r.FindAllByUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	return uint64(len(accounts)), nil
}

func (r *accountRepo) Insert(ctx context.Context, a *domain.Account) error {
	if a.ProviderAccountID != nil {
		idx := compositeIndexEntity{
			PartitionKey: providerAccountIndexPK(a.ProviderID), RowKey: *a.ProviderAccountID,
			PK: a.UserID, RK: a.ProviderID,
		}
		_ = addEntity(ctx, r.c, &idx) // best-effort
	}
	idIdx := compositeIndexEntity{PartitionKey: "idx_id", RowKey: a.ID, PK: a.UserID, RK: a.ProviderID}
	_ = addEntity(ctx, r.c, &idIdx)

	e := accountToEntity(a)
	if err := addEntity(ctx, r.c, &e); err != nil {
		if isConflict(err) {
			return apperror.Database("Account already exists")
		}
		return dbErr(err)
	}
	return nil
}

func (r *accountRepo) Update(ctx context.Context, a *domain.Account) error {
	e := accountToEntity(a)
	return upsertEntity(ctx, r.c, &e)
}

func (r *accountRepo) DeleteByID(ctx context.Context, id string) error {
	var idx compositeIndexEntity
	ok, err := getEntity(ctx, r.c, "idx_id", id, &idx)
	if err != nil || !ok {
		return err
	}
	var e accountEntity
	hasEntity, err := getEntity(ctx, r.c, idx.PK, idx.RK, &e)
	if err != nil {
		return err
	}
	if hasEntity && e.ProviderAccountID != nil {
		if err := deleteEntity(ctx, r.c, providerAccountIndexPK(idx.RK), *e.ProviderAccountID); err != nil {
			return err
		}
	}
	if err := deleteEntity(ctx, r.c, idx.PK, idx.RK); err != nil {
		return err
	}
	return deleteEntity(ctx, r.c, "idx_id", id)
}

func (r *accountRepo) DeleteAllByUser(ctx context.Context, userID string) error {
	accounts, err := r.FindAllByUser(ctx, userID)
	if err != nil {
		return err
	}
	for i := range accounts {
		if err := r.DeleteByID(ctx, accounts[i].ID); err != nil {
			return err
		}
	}
	return nil
}

// ─── AppProvider ─────────────────────────────────────────────────────────────

type appProviderEntity struct {
	PartitionKey string `json:"PartitionKey"` // app_id
	RowKey       string `json:"RowKey"`       // provider_id
	ID           string `json:"id"`
	Config       string `json:"config"`
	IsActive     bool   `json:"is_active"`
	CreatedAt    string `json:"created_at"`
}

func appProviderToEntity(p *domain.AppProvider) appProviderEntity {
	cfg := p.Config
	if cfg == "" {
		cfg = "{}"
	}
	return appProviderEntity{
		PartitionKey: p.AppID, RowKey: p.ProviderID, ID: p.ID,
		Config: cfg, IsActive: p.IsActive, CreatedAt: fmtDT(p.CreatedAt),
	}
}

func (e *appProviderEntity) toModel() *domain.AppProvider {
	cfg := e.Config
	if cfg == "" {
		cfg = "{}"
	}
	return &domain.AppProvider{
		ID: e.ID, AppID: e.PartitionKey, ProviderID: e.RowKey,
		Config: cfg, IsActive: e.IsActive, CreatedAt: parseDT(e.CreatedAt),
	}
}

type appProviderRepo struct{ c *aztables.Client }

func (r *appProviderRepo) FindByAppAndProvider(ctx context.Context, appID, providerID string) (*domain.AppProvider, error) {
	var e appProviderEntity
	ok, err := getEntity(ctx, r.c, appID, providerID, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *appProviderRepo) FindAllByApp(ctx context.Context, appID string) ([]domain.AppProvider, error) {
	es, err := queryEntities[appProviderEntity](ctx, r.c, "PartitionKey eq '"+appID+"'")
	if err != nil {
		return nil, err
	}
	out := make([]domain.AppProvider, 0, len(es))
	for i := range es {
		out = append(out, *es[i].toModel())
	}
	return out, nil
}

func (r *appProviderRepo) Insert(ctx context.Context, ap *domain.AppProvider) error {
	idIdx := compositeIndexEntity{PartitionKey: "idx_id", RowKey: ap.ID, PK: ap.AppID, RK: ap.ProviderID}
	_ = addEntity(ctx, r.c, &idIdx)

	e := appProviderToEntity(ap)
	if err := addEntity(ctx, r.c, &e); err != nil {
		if isConflict(err) {
			return apperror.Database("Provider already configured")
		}
		return dbErr(err)
	}
	return nil
}

func (r *appProviderRepo) DeleteByID(ctx context.Context, id string) error {
	var idx compositeIndexEntity
	ok, err := getEntity(ctx, r.c, "idx_id", id, &idx)
	if err != nil || !ok {
		return err
	}
	if err := deleteEntity(ctx, r.c, idx.PK, idx.RK); err != nil {
		return err
	}
	return deleteEntity(ctx, r.c, "idx_id", id)
}

// ─── AuthCode ────────────────────────────────────────────────────────────────

type authCodeEntity struct {
	PartitionKey        string  `json:"PartitionKey"` // "code"
	RowKey              string  `json:"RowKey"`       // code value
	AppID               string  `json:"app_id"`
	UserID              string  `json:"user_id"`
	RedirectURI         string  `json:"redirect_uri"`
	Scopes              string  `json:"scopes"`
	CodeChallenge       *string `json:"code_challenge,omitempty"`
	CodeChallengeMethod *string `json:"code_challenge_method,omitempty"`
	ExpiresAt           string  `json:"expires_at"`
	Used                bool    `json:"used"`
	CreatedAt           string  `json:"created_at"`
}

func authCodeToEntity(c *domain.AuthorizationCode) authCodeEntity {
	scopes := c.Scopes
	if scopes == "" {
		scopes = "[]"
	}
	return authCodeEntity{
		PartitionKey: "code", RowKey: c.Code, AppID: c.AppID, UserID: c.UserID,
		RedirectURI: c.RedirectURI, Scopes: scopes,
		CodeChallenge: c.CodeChallenge, CodeChallengeMethod: c.CodeChallengeMethod,
		ExpiresAt: fmtDT(c.ExpiresAt), Used: c.Used, CreatedAt: fmtDT(c.CreatedAt),
	}
}

func (e *authCodeEntity) toModel() *domain.AuthorizationCode {
	scopes := e.Scopes
	if scopes == "" {
		scopes = "[]"
	}
	return &domain.AuthorizationCode{
		Code: e.RowKey, AppID: e.AppID, UserID: e.UserID, RedirectURI: e.RedirectURI,
		Scopes: scopes, CodeChallenge: e.CodeChallenge, CodeChallengeMethod: e.CodeChallengeMethod,
		ExpiresAt: parseDT(e.ExpiresAt), Used: e.Used, CreatedAt: parseDT(e.CreatedAt),
	}
}

type authCodeRepo struct{ c *aztables.Client }

func (r *authCodeRepo) FindByCode(ctx context.Context, code string) (*domain.AuthorizationCode, error) {
	var e authCodeEntity
	ok, err := getEntity(ctx, r.c, "code", code, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *authCodeRepo) Insert(ctx context.Context, c *domain.AuthorizationCode) error {
	e := authCodeToEntity(c)
	if err := addEntity(ctx, r.c, &e); err != nil {
		return dbErr(err)
	}
	return nil
}

func (r *authCodeRepo) MarkUsed(ctx context.Context, code string) error {
	var e authCodeEntity
	ok, err := getEntity(ctx, r.c, "code", code, &e)
	if err != nil || !ok {
		return err
	}
	e.Used = true
	return upsertEntity(ctx, r.c, &e)
}

func (r *authCodeRepo) DeleteAllByUser(ctx context.Context, userID string) error {
	es, err := queryEntities[authCodeEntity](ctx, r.c, "PartitionKey eq 'code' and user_id eq '"+userID+"'")
	if err != nil {
		return err
	}
	for _, e := range es {
		if err := deleteEntity(ctx, r.c, "code", e.RowKey); err != nil {
			return err
		}
	}
	return nil
}

// ─── RefreshToken ────────────────────────────────────────────────────────────

type refreshTokenEntity struct {
	PartitionKey string  `json:"PartitionKey"` // "rt"
	RowKey       string  `json:"RowKey"`       // id
	UserID       string  `json:"user_id"`
	AppID        string  `json:"app_id"`
	TokenHash    string  `json:"token_hash"`
	Scopes       string  `json:"scopes"`
	DeviceID     *string `json:"device_id,omitempty"`
	ExpiresAt    string  `json:"expires_at"`
	Revoked      bool    `json:"revoked"`
	CreatedAt    string  `json:"created_at"`
}

func refreshTokenToEntity(t *domain.RefreshToken) refreshTokenEntity {
	scopes := t.Scopes
	if scopes == "" {
		scopes = "[]"
	}
	return refreshTokenEntity{
		PartitionKey: "rt", RowKey: t.ID, UserID: t.UserID, AppID: t.AppID,
		TokenHash: t.TokenHash, Scopes: scopes, DeviceID: t.DeviceID,
		ExpiresAt: fmtDT(t.ExpiresAt), Revoked: t.Revoked, CreatedAt: fmtDT(t.CreatedAt),
	}
}

func (e *refreshTokenEntity) toModel() *domain.RefreshToken {
	scopes := e.Scopes
	if scopes == "" {
		scopes = "[]"
	}
	return &domain.RefreshToken{
		ID: e.RowKey, UserID: e.UserID, AppID: e.AppID, TokenHash: e.TokenHash,
		Scopes: scopes, DeviceID: e.DeviceID, ExpiresAt: parseDT(e.ExpiresAt),
		Revoked: e.Revoked, CreatedAt: parseDT(e.CreatedAt),
	}
}

type refreshTokenRepo struct{ c *aztables.Client }

func (r *refreshTokenRepo) FindByTokenHash(ctx context.Context, hash string) (*domain.RefreshToken, error) {
	var idx indexEntity
	ok, err := getEntity(ctx, r.c, "idx_hash", hash, &idx)
	if err != nil || !ok {
		return nil, err
	}
	var e refreshTokenEntity
	ok, err = getEntity(ctx, r.c, "rt", idx.TargetID, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *refreshTokenRepo) Insert(ctx context.Context, t *domain.RefreshToken) error {
	hashIdx := indexEntity{PartitionKey: "idx_hash", RowKey: t.TokenHash, TargetID: t.ID}
	_ = addEntity(ctx, r.c, &hashIdx)

	e := refreshTokenToEntity(t)
	if err := addEntity(ctx, r.c, &e); err != nil {
		return dbErr(err)
	}
	return nil
}

func (r *refreshTokenRepo) Revoke(ctx context.Context, id string) error {
	var e refreshTokenEntity
	ok, err := getEntity(ctx, r.c, "rt", id, &e)
	if err != nil || !ok {
		return err
	}
	e.Revoked = true
	return upsertEntity(ctx, r.c, &e)
}

func (r *refreshTokenRepo) DeleteAllByUser(ctx context.Context, userID string) error {
	es, err := queryEntities[refreshTokenEntity](ctx, r.c, "PartitionKey eq 'rt' and user_id eq '"+userID+"'")
	if err != nil {
		return err
	}
	for _, e := range es {
		if err := deleteEntity(ctx, r.c, "idx_hash", e.TokenHash); err != nil {
			return err
		}
		if err := deleteEntity(ctx, r.c, "rt", e.RowKey); err != nil {
			return err
		}
	}
	return nil
}

// ─── InviteCode ──────────────────────────────────────────────────────────────

type inviteCodeEntity struct {
	PartitionKey         string  `json:"PartitionKey"` // "invite_code"
	RowKey               string  `json:"RowKey"`       // code value
	ID                   string  `json:"id"`
	CreatedBy            string  `json:"created_by"`
	CreatedAt            string  `json:"created_at"`
	UsedAt               *string `json:"used_at,omitempty"`
	UsedBy               *string `json:"used_by,omitempty"`
	IsRevoked            bool    `json:"is_revoked"`
	Kind                 string  `json:"kind"`
	GrantsMembership     *string `json:"grants_membership,omitempty"`
	GrantsMembershipDays *int64  `json:"grants_membership_days,omitempty"`
	GrantsUserType       *string `json:"grants_user_type,omitempty"`
}

func inviteCodeToEntity(c *domain.InviteCode) inviteCodeEntity {
	kind := string(c.Kind)
	if kind == "" {
		kind = string(domain.InviteSingleUse)
	}
	var grants *string
	if c.GrantsMembership != nil {
		v := string(*c.GrantsMembership)
		grants = &v
	}
	var grantsUserType *string
	if t := normalizeInviteCodeGrantUserType(c.GrantsUserType); t != nil {
		v := string(*t)
		grantsUserType = &v
	}
	return inviteCodeEntity{
		PartitionKey: "invite_code", RowKey: c.Code, ID: c.ID, CreatedBy: c.CreatedBy,
		CreatedAt: fmtDT(c.CreatedAt), UsedAt: fmtDTPtr(c.UsedAt), UsedBy: c.UsedBy,
		IsRevoked: c.IsRevoked, Kind: kind, GrantsMembership: grants,
		GrantsMembershipDays: c.GrantsMembershipDays, GrantsUserType: grantsUserType,
	}
}

func (e *inviteCodeEntity) toModel() *domain.InviteCode {
	var grants *domain.MembershipTier
	if e.GrantsMembership != nil {
		t := domain.MembershipFromString(*e.GrantsMembership)
		grants = &t
	}
	var grantsUserType *domain.UserType
	if e.GrantsUserType != nil {
		t := domain.UserType(*e.GrantsUserType)
		grantsUserType = normalizeInviteCodeGrantUserType(&t)
	}
	return &domain.InviteCode{
		ID: e.ID, Code: e.RowKey, CreatedBy: e.CreatedBy, CreatedAt: parseDT(e.CreatedAt),
		UsedAt: parseDTPtr(e.UsedAt), UsedBy: e.UsedBy, IsRevoked: e.IsRevoked,
		Kind: domain.InviteKindFromString(e.Kind), GrantsMembership: grants,
		GrantsMembershipDays: e.GrantsMembershipDays, GrantsUserType: grantsUserType,
	}
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

type inviteCodeRepo struct{ c *aztables.Client }

func (r *inviteCodeRepo) Create(ctx context.Context, createdBy string, kind domain.InviteCodeKind, grants *domain.MembershipTier, grantDays *int64, grantsUserType *domain.UserType) (*domain.InviteCode, error) {
	code := &domain.InviteCode{
		ID: newUUID(), Code: generateInviteCode(), CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(), IsRevoked: false, Kind: kind,
		GrantsMembership: grants, GrantsMembershipDays: grantDays, GrantsUserType: grantsUserType,
	}
	e := inviteCodeToEntity(code)
	if err := addEntity(ctx, r.c, &e); err != nil {
		return nil, dbErr(err)
	}
	return code, nil
}

func (r *inviteCodeRepo) GetByCode(ctx context.Context, code string) (*domain.InviteCode, error) {
	var e inviteCodeEntity
	ok, err := getEntity(ctx, r.c, "invite_code", code, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *inviteCodeRepo) MarkUsed(ctx context.Context, code, userID string) error {
	resp, err := r.c.GetEntity(ctx, "invite_code", code, nil)
	if err != nil {
		if isNotFound(err) {
			return apperror.InviteCodeNotFound()
		}
		return dbErr(err)
	}
	var e inviteCodeEntity
	if err := json.Unmarshal(resp.Value, &e); err != nil {
		return dbErr(err)
	}
	if e.UsedAt != nil {
		return apperror.InviteCodeAlreadyUsed()
	}
	now := fmtDT(time.Now().UTC())
	e.UsedAt = &now
	e.UsedBy = &userID
	b, err := json.Marshal(&e)
	if err != nil {
		return dbErr(err)
	}
	etag := resp.ETag
	mode := aztables.UpdateModeReplace
	_, err = r.c.UpdateEntity(ctx, b, &aztables.UpdateEntityOptions{IfMatch: &etag, UpdateMode: mode})
	if err != nil {
		if isPreconditionFailed(err) {
			return apperror.InviteCodeAlreadyUsed()
		}
		return dbErr(err)
	}
	return nil
}

func (r *inviteCodeRepo) List(ctx context.Context, usedOnly *bool) ([]domain.InviteCode, error) {
	es, err := queryEntities[inviteCodeEntity](ctx, r.c, "PartitionKey eq 'invite_code'")
	if err != nil {
		return nil, err
	}
	out := make([]domain.InviteCode, 0, len(es))
	for i := range es {
		m := es[i].toModel()
		if usedOnly != nil && *usedOnly != (m.UsedAt != nil) {
			continue
		}
		out = append(out, *m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (r *inviteCodeRepo) Revoke(ctx context.Context, code string) error {
	var e inviteCodeEntity
	ok, err := getEntity(ctx, r.c, "invite_code", code, &e)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.InviteCodeNotFound()
	}
	if e.UsedAt != nil {
		return apperror.InviteCodeAlreadyUsed()
	}
	e.IsRevoked = true
	return upsertEntity(ctx, r.c, &e)
}

// ─── Team ────────────────────────────────────────────────────────────────────

type teamEntity struct {
	PartitionKey string  `json:"PartitionKey"` // "team"
	RowKey       string  `json:"RowKey"`       // team_id
	Name         string  `json:"name"`
	Description  *string `json:"description,omitempty"`
	OwnerUserID  string  `json:"owner_user_id"`
	IsOpen       *bool   `json:"is_open,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func teamToEntity(t *domain.Team) teamEntity {
	return teamEntity{
		PartitionKey: "team", RowKey: t.ID, Name: t.Name, Description: t.Description,
		OwnerUserID: t.OwnerUserID, IsOpen: boolPtr(t.IsOpen),
		CreatedAt: fmtDT(t.CreatedAt), UpdatedAt: fmtDT(t.UpdatedAt),
	}
}

func (e *teamEntity) toModel() *domain.Team {
	return &domain.Team{
		ID: e.RowKey, Name: e.Name, Description: e.Description, OwnerUserID: e.OwnerUserID,
		IsOpen: boolOr(e.IsOpen, true), CreatedAt: parseDT(e.CreatedAt), UpdatedAt: parseDT(e.UpdatedAt),
	}
}

type teamRepo struct{ c *aztables.Client }

func (r *teamRepo) FindByID(ctx context.Context, id string) (*domain.Team, error) {
	var e teamEntity
	ok, err := getEntity(ctx, r.c, "team", id, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *teamRepo) FindAllOpen(ctx context.Context) ([]domain.Team, error) {
	es, err := queryEntities[teamEntity](ctx, r.c, "PartitionKey eq 'team' and is_open eq true")
	if err != nil {
		return nil, err
	}
	out := make([]domain.Team, 0, len(es))
	for i := range es {
		out = append(out, *es[i].toModel())
	}
	return out, nil
}

func (r *teamRepo) FindAllOwnedByUser(ctx context.Context, userID string) ([]domain.Team, error) {
	es, err := queryEntities[teamEntity](ctx, r.c, "PartitionKey eq 'team' and owner_user_id eq '"+userID+"'")
	if err != nil {
		return nil, err
	}
	out := make([]domain.Team, 0, len(es))
	for i := range es {
		out = append(out, *es[i].toModel())
	}
	return out, nil
}

func (r *teamRepo) Insert(ctx context.Context, t *domain.Team) error {
	e := teamToEntity(t)
	if err := addEntity(ctx, r.c, &e); err != nil {
		return dbErr(err)
	}
	return nil
}

func (r *teamRepo) Update(ctx context.Context, t *domain.Team) error {
	e := teamToEntity(t)
	return upsertEntity(ctx, r.c, &e)
}

func (r *teamRepo) DeleteByID(ctx context.Context, id string) error {
	return deleteEntity(ctx, r.c, "team", id)
}

// ─── TeamMembership ──────────────────────────────────────────────────────────

type teamMembershipEntity struct {
	PartitionKey string `json:"PartitionKey"` // team_id
	RowKey       string `json:"RowKey"`       // user_id
	Role         string `json:"role"`
	JoinedAt     string `json:"joined_at"`
}

func teamMembershipToEntity(m *domain.TeamMembership) teamMembershipEntity {
	return teamMembershipEntity{
		PartitionKey: m.TeamID, RowKey: m.UserID, Role: m.Role, JoinedAt: fmtDT(m.JoinedAt),
	}
}

func (e *teamMembershipEntity) toModel() *domain.TeamMembership {
	return &domain.TeamMembership{
		TeamID: e.PartitionKey, UserID: e.RowKey, Role: e.Role, JoinedAt: parseDT(e.JoinedAt),
	}
}

type teamMembershipRepo struct{ c *aztables.Client }

func (r *teamMembershipRepo) FindAllByTeam(ctx context.Context, teamID string) ([]domain.TeamMembership, error) {
	es, err := queryEntities[teamMembershipEntity](ctx, r.c, "PartitionKey eq '"+teamID+"'")
	if err != nil {
		return nil, err
	}
	out := make([]domain.TeamMembership, 0, len(es))
	for i := range es {
		out = append(out, *es[i].toModel())
	}
	return out, nil
}

func (r *teamMembershipRepo) FindAllByUser(ctx context.Context, userID string) ([]domain.TeamMembership, error) {
	es, err := queryEntities[teamMembershipEntity](ctx, r.c, "RowKey eq '"+userID+"'")
	if err != nil {
		return nil, err
	}
	out := make([]domain.TeamMembership, 0, len(es))
	for i := range es {
		out = append(out, *es[i].toModel())
	}
	return out, nil
}

func (r *teamMembershipRepo) Find(ctx context.Context, teamID, userID string) (*domain.TeamMembership, error) {
	var e teamMembershipEntity
	ok, err := getEntity(ctx, r.c, teamID, userID, &e)
	if err != nil || !ok {
		return nil, err
	}
	return e.toModel(), nil
}

func (r *teamMembershipRepo) Insert(ctx context.Context, m *domain.TeamMembership) error {
	e := teamMembershipToEntity(m)
	return upsertEntity(ctx, r.c, &e)
}

func (r *teamMembershipRepo) CountByTeam(ctx context.Context, teamID string) (uint64, error) {
	members, err := r.FindAllByTeam(ctx, teamID)
	if err != nil {
		return 0, err
	}
	return uint64(len(members)), nil
}

func (r *teamMembershipRepo) Delete(ctx context.Context, teamID, userID string) error {
	return deleteEntity(ctx, r.c, teamID, userID)
}

func (r *teamMembershipRepo) DeleteAllByTeam(ctx context.Context, teamID string) error {
	members, err := r.FindAllByTeam(ctx, teamID)
	if err != nil {
		return err
	}
	for _, m := range members {
		if err := deleteEntity(ctx, r.c, teamID, m.UserID); err != nil {
			return err
		}
	}
	return nil
}

func (r *teamMembershipRepo) DeleteAllByUser(ctx context.Context, userID string) error {
	members, err := r.FindAllByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, m := range members {
		if err := deleteEntity(ctx, r.c, m.TeamID, userID); err != nil {
			return err
		}
	}
	return nil
}

// ─── Migrations ──────────────────────────────────────────────────────────────

// MigrateInviteCodeKinds backfills the `kind` field on every invite-code row.
func (r *Repository) MigrateInviteCodeKinds(ctx context.Context) (int, error) {
	es, err := queryEntities[inviteCodeEntity](ctx, r.inviteCodes, "PartitionKey eq 'invite_code'")
	if err != nil {
		return 0, err
	}
	count := 0
	for i := range es {
		if es[i].Kind == "" {
			es[i].Kind = string(domain.InviteSingleUse)
		}
		if err := upsertEntity(ctx, r.inviteCodes, &es[i]); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// MigrateUserInviteCodes backfills users.invite_code from each single-use
// code's used_by linkage.
func (r *Repository) MigrateUserInviteCodes(ctx context.Context) (int, error) {
	codes, err := queryEntities[inviteCodeEntity](ctx, r.inviteCodes, "PartitionKey eq 'invite_code'")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, c := range codes {
		if c.UsedBy == nil {
			continue
		}
		var ue userEntity
		ok, err := getEntity(ctx, r.users, "user", *c.UsedBy, &ue)
		if err != nil {
			return count, err
		}
		if !ok || ue.InviteCode != nil {
			continue
		}
		rowKey := c.RowKey
		ue.InviteCode = &rowKey
		if err := upsertEntity(ctx, r.users, &ue); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// MigrateUserSortIndexes backfills the admin user-list sort indexes.
func (r *Repository) MigrateUserSortIndexes(ctx context.Context) (int, error) {
	return r.userRepo.migrateSortIndexes(ctx)
}

var _ repository.Repository = (*Repository)(nil)
