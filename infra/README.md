# Infrastructure (Bicep)

Infrastructure-as-code for the auth service's Azure resources, in
resource-group scope.

## Resources

| Module | Resource | Purpose |
|--------|----------|---------|
| `logAnalytics.bicep` | Log Analytics workspace | Container Apps logs |
| `storage.bicep` | Storage account | Table service (app data) + File Share `jwt-keys` |
| `containerAppEnv.bicep` | Container Apps managed environment | Hosts the backend; links the keys file share |
| `containerApp.bicep` | Container App `auth-backend` | The Go backend (ingress :3000, `/health` probes) |
| `staticWebApp.bicep` | Static Web App | Admin dashboard frontend |

The backend's `AZURE_STORAGE_CONNECTION_STRING` is composed from the storage
account key inside `containerApp.bicep` and stored as a Container App **secret**
— it is never a plaintext env value or committed anywhere. JWT keys are **not**
provisioned here; upload `private.pem`/`public.pem` to the `jwt-keys` file share
(see below) — the app mounts them read-only at `/app/keys`.

## Parameters

Edit `main.bicepparam`. Before a real apply you **must** set:

- `backendImage` — pin to a released tag, e.g. `ghcr.io/<owner>/auth-backend:2026.6.2`
- `corsAllowedOrigins` — the real frontend origin(s)
- `registryUsername` + `GHCR_TOKEN` env — only if the GHCR image is private

## Deploy

```bash
RG=rg-auth-prod
export GHCR_TOKEN=<ghcr-pat-if-private-image>   # optional

# Preview (read-only) — always run this first and read the diff carefully.
az deployment group what-if -g "$RG" --parameters infra/main.bicepparam

# Apply
az deployment group create  -g "$RG" --parameters infra/main.bicepparam
```

> ⚠️ These templates describe the **desired** state. Run `what-if` against the
> live resource group and reconcile any drift before applying — a blind apply
> can reconfigure or replace existing production resources.

### Upload the JWT keys to the file share (one-time)

```bash
ACCOUNT=$(az deployment group show -g "$RG" -n main --query properties.outputs.storageAccountName.value -o tsv)
KEY=$(az storage account keys list -g "$RG" -n "$ACCOUNT" --query "[0].value" -o tsv)
az storage file upload --account-name "$ACCOUNT" --account-key "$KEY" \
  --share-name jwt-keys --source ./keys/private.pem
az storage file upload --account-name "$ACCOUNT" --account-key "$KEY" \
  --share-name jwt-keys --source ./keys/public.pem
```

## CI

`.github/workflows/infra.yml`:
- **Pull requests** touching `infra/**` run `what-if` (read-only preview).
- **Manual** `workflow_dispatch` with `apply = true` runs `az deployment group create`.

Both authenticate to Azure via OIDC (`AZURE_CLIENT_ID` / `AZURE_TENANT_ID` /
`AZURE_SUBSCRIPTION_ID` repo vars) and read the optional `GHCR_TOKEN` secret.
