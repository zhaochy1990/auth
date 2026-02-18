#!/usr/bin/env bash
set -euo pipefail

# Bootstrap script for initial Azure infrastructure + GitHub configuration
# Run once from repo root: ./infra/scripts/bootstrap.sh

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# --- Prerequisites ---
for cmd in az gh openssl jq; do
  command -v "$cmd" &>/dev/null || error "'$cmd' is required but not installed"
done

az account show &>/dev/null || error "Not logged in to Azure CLI. Run 'az login' first."
gh auth status &>/dev/null || error "Not logged in to GitHub CLI. Run 'gh auth login' first."

SUBSCRIPTION_ID=$(az account show --query id -o tsv)
TENANT_ID=$(az account show --query tenantId -o tsv)
GITHUB_REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)

info "Azure subscription: $SUBSCRIPTION_ID"
info "Azure tenant:       $TENANT_ID"
info "GitHub repo:        $GITHUB_REPO"

# --- Collect secrets ---
read -rp "SQL admin login: " SQL_ADMIN_LOGIN
read -rsp "SQL admin password: " SQL_ADMIN_PASSWORD
echo
read -rsp "GHCR PAT (read:packages): " GHCR_PAT
echo

[[ -z "$SQL_ADMIN_LOGIN" ]] && error "SQL admin login cannot be empty"
[[ -z "$SQL_ADMIN_PASSWORD" ]] && error "SQL admin password cannot be empty"
[[ -z "$GHCR_PAT" ]] && error "GHCR PAT cannot be empty"

# --- Deploy Bicep ---
info "Deploying infrastructure (this may take several minutes)..."

DEPLOY_OUTPUT=$(az deployment sub create \
  --location southeastasia \
  --template-file infra/main.bicep \
  --parameters \
    location='southeastasia' \
    envName='prod' \
    githubRepo='zhaochy1990/auth' \
    githubBranch='main' \
    storageName='authstorage2026' \
    sqlAdminLogin="$SQL_ADMIN_LOGIN" \
    sqlAdminPassword="$SQL_ADMIN_PASSWORD" \
    ghcrPassword="$GHCR_PAT" \
  --query properties.outputs -o json)

# --- Extract outputs ---
COMMON_RG=$(echo "$DEPLOY_OUTPUT" | jq -r '.commonResourceGroupName.value')
AUTH_RG=$(echo "$DEPLOY_OUTPUT" | jq -r '.authResourceGroupName.value')
IDENTITY_CLIENT_ID=$(echo "$DEPLOY_OUTPUT" | jq -r '.managedIdentityClientId.value')
BACKEND_FQDN=$(echo "$DEPLOY_OUTPUT" | jq -r '.backendFqdn.value')
SWA_NAME=$(echo "$DEPLOY_OUTPUT" | jq -r '.staticWebAppName.value')
SWA_HOSTNAME=$(echo "$DEPLOY_OUTPUT" | jq -r '.staticWebAppHostname.value')
SQL_FQDN=$(echo "$DEPLOY_OUTPUT" | jq -r '.sqlServerFqdn.value')
STORAGE_ACCOUNT="authstorage2026"

info "Common RG:       $COMMON_RG"
info "Auth RG:         $AUTH_RG"
info "Backend FQDN:    $BACKEND_FQDN"
info "SWA hostname:    $SWA_HOSTNAME"
info "SQL Server FQDN: $SQL_FQDN"

# --- Generate JWT key pair ---
info "Generating RSA key pair..."
TMPDIR=$(mktemp -d)
openssl genpkey -algorithm RSA -out "$TMPDIR/private.pem" -pkeyopt rsa_keygen_bits:2048 2>/dev/null
openssl rsa -pubout -in "$TMPDIR/private.pem" -out "$TMPDIR/public.pem" 2>/dev/null

# --- Upload keys to Azure File Share (storage is in common RG) ---
info "Uploading JWT keys to Azure File Share..."
STORAGE_KEY=$(az storage account keys list \
  --account-name "$STORAGE_ACCOUNT" \
  --resource-group "$COMMON_RG" \
  --query '[0].value' -o tsv)

az storage file upload \
  --share-name jwt-keys \
  --source "$TMPDIR/private.pem" \
  --path private.pem \
  --account-name "$STORAGE_ACCOUNT" \
  --account-key "$STORAGE_KEY" \
  --output none

az storage file upload \
  --share-name jwt-keys \
  --source "$TMPDIR/public.pem" \
  --path public.pem \
  --account-name "$STORAGE_ACCOUNT" \
  --account-key "$STORAGE_KEY" \
  --output none

rm -rf "$TMPDIR"
info "JWT keys uploaded to file share 'jwt-keys'"

# --- Get SWA deploy token (SWA is in auth RG) ---
SWA_DEPLOY_TOKEN=$(az staticwebapp secrets list \
  --name "$SWA_NAME" \
  --resource-group "$AUTH_RG" \
  --query properties.apiKey -o tsv)

# --- Configure GitHub secrets ---
info "Configuring GitHub secrets..."
gh secret set SQL_ADMIN_LOGIN --body "$SQL_ADMIN_LOGIN"
gh secret set SQL_ADMIN_PASSWORD --body "$SQL_ADMIN_PASSWORD"
gh secret set GHCR_PAT --body "$GHCR_PAT"
gh secret set SWA_DEPLOY_TOKEN --body "$SWA_DEPLOY_TOKEN"

# --- Configure GitHub variables ---
info "Configuring GitHub variables..."
gh variable set AZURE_CLIENT_ID --body "$IDENTITY_CLIENT_ID"
gh variable set AZURE_TENANT_ID --body "$TENANT_ID"
gh variable set AZURE_SUBSCRIPTION_ID --body "$SUBSCRIPTION_ID"
gh variable set VITE_API_BASE_URL --body "https://$BACKEND_FQDN"

info "Bootstrap complete!"
echo ""
echo "=========================================="
echo " Next Steps"
echo "=========================================="
echo ""
echo "1. Create a GitHub environment named 'production':"
echo "   https://github.com/$GITHUB_REPO/settings/environments"
echo ""
echo "2. Trigger the first deploy:"
echo "   gh workflow run deploy.yml"
echo ""
echo "3. After backend is running, seed the admin user:"
echo "   az containerapp exec --name auth-backend --resource-group $AUTH_RG \\"
echo "     --command './auth-service seed admin@example.com YourPassword'"
echo ""
echo "4. Set the client ID from seed output as a secret:"
echo "   gh secret set VITE_API_CLIENT_ID --body '<client-id-from-step-3>'"
echo ""
echo "5. Re-deploy frontend to pick up the client ID:"
echo "   gh workflow run deploy.yml"
echo ""
