targetScope = 'subscription'

@description('Azure region for all resources')
param location string = 'southeastasia'

@description('Environment name')
param envName string = 'prod'

@description('GitHub repository (owner/repo)')
param githubRepo string = 'zhaochy1990/auth'

@description('GitHub branch for federated credential')
param githubBranch string = 'main'

@description('Storage account name (globally unique)')
param storageName string = 'authstorage2026'

@description('CORS allowed origins (comma-separated)')
param corsAllowedOrigins string = '*'

@description('SQL Server admin login')
@secure()
param sqlAdminLogin string

@description('SQL Server admin password')
@secure()
param sqlAdminPassword string

@description('GitHub Container Registry PAT (read:packages)')
@secure()
param ghcrPassword string

// --- Resource Groups ---
resource commonRg 'Microsoft.Resources/resourceGroups@2024-03-01' = {
  name: 'rg-common-${envName}'
  location: location
}

resource authRg 'Microsoft.Resources/resourceGroups@2024-03-01' = {
  name: 'rg-auth-${envName}'
  location: location
}

// --- Shared resources (SQL, Log Analytics, Storage) ---
module common 'modules/common.bicep' = {
  scope: commonRg
  name: 'common-resources'
  params: {
    location: location
    storageName: storageName
    sqlAdminLogin: sqlAdminLogin
    sqlAdminPassword: sqlAdminPassword
  }
}

// --- Auth-specific resources (Container App, SWA, UAMI) ---
module auth 'modules/auth.bicep' = {
  scope: authRg
  name: 'auth-resources'
  params: {
    location: location
    envName: envName
    githubRepo: githubRepo
    githubBranch: githubBranch
    corsAllowedOrigins: corsAllowedOrigins
    commonRgName: commonRg.name
    sqlServerFqdn: common.outputs.sqlServerFqdn
    sqlAdminLogin: sqlAdminLogin
    sqlAdminPassword: sqlAdminPassword
    sqlDatabaseName: common.outputs.sqlDatabaseName
    logAnalyticsName: common.outputs.logAnalyticsName
    storageAccountName: common.outputs.storageAccountName
    fileShareName: common.outputs.fileShareName
    ghcrPassword: ghcrPassword
  }
}

// NOTE: Subscription-level Contributor role for the UAMI is created during
// bootstrap (requires Owner). Not included here as infra.yml runs as the UAMI
// which lacks Microsoft.Authorization/roleAssignments/write permission.

// --- Outputs ---
output commonResourceGroupName string = commonRg.name
output authResourceGroupName string = authRg.name
output managedIdentityClientId string = auth.outputs.managedIdentityClientId
output managedIdentityPrincipalId string = auth.outputs.managedIdentityPrincipalId
output backendFqdn string = auth.outputs.backendFqdn
output staticWebAppName string = auth.outputs.staticWebAppName
output staticWebAppHostname string = auth.outputs.staticWebAppHostname
output sqlServerFqdn string = common.outputs.sqlServerFqdn
