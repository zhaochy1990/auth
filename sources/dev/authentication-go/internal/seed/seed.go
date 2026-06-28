// Package seed bootstraps the Admin Dashboard application and an admin user,
// ported from the Rust seed.rs. It is idempotent: re-running promotes an
// existing user or reports already_admin; the client secret is only returned on
// first creation.
package seed

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/domain"
	"github.com/zhaochy1990/auth-service/internal/repository"
)

// Result describes the outcome of a bootstrap.
type Result struct {
	AppClientID     string
	AppClientSecret *string // only set when a new application is created
	UserAction      string  // "created" | "promoted" | "already_admin"
}

// Bootstrap creates/finds the Admin Dashboard app and creates/promotes the
// admin user. A password is required only when creating a new user.
func Bootstrap(ctx context.Context, repo repository.Repository, adminEmail string, adminPassword *string) (*Result, error) {
	existingApp, err := repo.Applications().FindByName(ctx, "Admin Dashboard")
	if err != nil {
		return nil, err
	}

	var appClientID string
	var appClientSecret *string
	if existingApp != nil {
		appClientID = existingApp.ClientID
	} else {
		clientID := auth.GenerateClientID()
		secret := auth.RandomHex(32)
		now := time.Now().UTC()
		appID := uuid.NewString()
		redirect, _ := json.Marshal([]string{"http://localhost:5173"})
		scopes, _ := json.Marshal([]string{"admin"})
		app := &domain.Application{
			ID:               appID,
			Name:             "Admin Dashboard",
			ClientID:         clientID,
			ClientSecretHash: auth.HashClientSecret(secret),
			RedirectURIs:     string(redirect),
			AllowedScopes:    string(scopes),
			IsActive:         true,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := repo.Applications().Insert(ctx, app); err != nil {
			return nil, err
		}
		provider := &domain.AppProvider{
			ID: uuid.NewString(), AppID: appID, ProviderID: "password",
			Config: "{}", IsActive: true, CreatedAt: now,
		}
		if err := repo.AppProviders().Insert(ctx, provider); err != nil {
			return nil, err
		}
		appClientID = clientID
		appClientSecret = &secret
	}

	existingUser, err := repo.Users().FindByEmail(ctx, adminEmail)
	if err != nil {
		return nil, err
	}

	var userAction string
	if existingUser != nil {
		if existingUser.Role == "admin" {
			userAction = "already_admin"
		} else {
			existingUser.Role = "admin"
			existingUser.UpdatedAt = time.Now().UTC()
			if err := repo.Users().Update(ctx, existingUser); err != nil {
				return nil, err
			}
			userAction = "promoted"
		}
	} else {
		if adminPassword == nil {
			return nil, apperror.BadRequest("Password is required when creating a new admin user. Usage: auth-service seed <email> <password>")
		}
		hash, err := auth.HashPassword(*adminPassword)
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		userID := uuid.NewString()
		name := "Admin"
		email := adminEmail
		user := &domain.User{
			ID:            userID,
			Email:         &email,
			Name:          &name,
			EmailVerified: true,
			Role:          "admin",
			IsActive:      true,
			CreatedAt:     now,
			UpdatedAt:     now,
			Membership:    domain.MembershipRegular,
		}
		if err := repo.Users().Insert(ctx, user); err != nil {
			return nil, err
		}
		account := &domain.Account{
			ID:                uuid.NewString(),
			UserID:            userID,
			ProviderID:        "password",
			ProviderAccountID: &email,
			Credential:        &hash,
			ProviderMetadata:  "{}",
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := repo.Accounts().Insert(ctx, account); err != nil {
			return nil, err
		}
		userAction = "created"
	}

	return &Result{AppClientID: appClientID, AppClientSecret: appClientSecret, UserAction: userAction}, nil
}
