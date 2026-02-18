param location string
param swaLocation string = 'eastasia'
param envName string
param githubRepo string
param githubBranch string
param corsAllowedOrigins string
param commonRgName string
param sqlServerFqdn string
param sqlDatabaseName string
param logAnalyticsName string
param storageAccountName string
param fileShareName string

@secure()
param sqlAdminLogin string
@secure()
param sqlAdminPassword string
@secure()
param ghcrPassword string

// --- Cross-RG references to common resources ---
resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2023-09-01' existing = {
  name: logAnalyticsName
  scope: resourceGroup(commonRgName)
}

resource storageAccount 'Microsoft.Storage/storageAccounts@2023-05-01' existing = {
  name: storageAccountName
  scope: resourceGroup(commonRgName)
}

// --- User-Assigned Managed Identity (for GitHub Actions OIDC) ---
resource managedIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: 'id-github-actions'
  location: location
}

resource federatedCredential 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2023-01-31' = {
  parent: managedIdentity
  name: 'github-actions-main'
  properties: {
    issuer: 'https://token.actions.githubusercontent.com'
    subject: 'repo:${githubRepo}:ref:refs/heads/${githubBranch}'
    audiences: ['api://AzureADTokenExchange']
  }
}

resource federatedCredentialEnv 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2023-01-31' = {
  dependsOn: [federatedCredential]
  parent: managedIdentity
  name: 'github-actions-env-production'
  properties: {
    issuer: 'https://token.actions.githubusercontent.com'
    subject: 'repo:${githubRepo}:environment:production'
    audiences: ['api://AzureADTokenExchange']
  }
}

// NOTE: RG-level and subscription-level Contributor roles for the UAMI are
// created during bootstrap (requires Owner). Not managed here.

// --- Container Apps Environment ---
resource cae 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: 'auth-cae-${envName}'
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logAnalytics.properties.customerId
        sharedKey: logAnalytics.listKeys().primarySharedKey
      }
    }
  }
}

// --- Container Apps Environment Storage ---
resource caeStorage 'Microsoft.App/managedEnvironments/storages@2024-03-01' = {
  parent: cae
  name: 'authfilestorage'
  properties: {
    azureFile: {
      accountName: storageAccount.name
      accountKey: storageAccount.listKeys().keys[0].value
      shareName: fileShareName
      accessMode: 'ReadOnly'
    }
  }
}

// --- Static Web App (Frontend) — defined before Container App so hostname is available ---
resource swa 'Microsoft.Web/staticSites@2023-12-01' = {
  name: 'auth-frontend'
  location: swaLocation
  sku: { name: 'Free', tier: 'Free' }
  properties: {}
}

// --- Backend Container App ---
var corsValue = corsAllowedOrigins == '*' ? 'https://${swa.properties.defaultHostname}' : corsAllowedOrigins
var databaseUrl = 'Server=${sqlServerFqdn},1433;User Id=${sqlAdminLogin};Password=${sqlAdminPassword};Database=${sqlDatabaseName};TrustServerCertificate=false;Encrypt=true'

resource backendApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: 'auth-backend'
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${managedIdentity.id}': {}
    }
  }
  properties: {
    managedEnvironmentId: cae.id
    configuration: {
      activeRevisionsMode: 'Single'
      ingress: {
        external: true
        targetPort: 3000
        transport: 'auto'
        allowInsecure: false
      }
      registries: [
        {
          server: 'ghcr.io'
          username: 'zhaochy1990'
          passwordSecretRef: 'ghcr-password'
        }
      ]
      secrets: [
        { name: 'cors-allowed-origins', value: corsValue }
        { name: 'database-url', value: databaseUrl }
        { name: 'ghcr-password', value: ghcrPassword }
      ]
    }
    template: {
      containers: [
        {
          name: 'auth-backend'
          // Placeholder image — CD pipeline will update this
          image: 'mcr.microsoft.com/azuredocs/containerapps-helloworld:latest'
          resources: {
            cpu: json('0.25')
            memory: '0.5Gi'
          }
          env: [
            { name: 'DATABASE_URL', secretRef: 'database-url' }
            { name: 'SERVER_HOST', value: '0.0.0.0' }
            { name: 'SERVER_PORT', value: '3000' }
            { name: 'JWT_PRIVATE_KEY_PATH', value: './keys/private.pem' }
            { name: 'JWT_PUBLIC_KEY_PATH', value: './keys/public.pem' }
            { name: 'CORS_ALLOWED_ORIGINS', secretRef: 'cors-allowed-origins' }
          ]
          volumeMounts: [
            { volumeName: 'auth-keys', mountPath: '/app/keys' }
          ]
        }
      ]
      scale: {
        minReplicas: 1
        maxReplicas: 1
      }
      volumes: [
        {
          name: 'auth-keys'
          storageName: caeStorage.name
          storageType: 'AzureFile'
          mountOptions: 'dir_mode=0500,file_mode=0400'
        }
      ]
    }
  }
}

// --- Outputs ---
output managedIdentityClientId string = managedIdentity.properties.clientId
output managedIdentityPrincipalId string = managedIdentity.properties.principalId
output backendFqdn string = backendApp.properties.configuration.ingress.fqdn
output staticWebAppName string = swa.name
output staticWebAppHostname string = swa.properties.defaultHostname
