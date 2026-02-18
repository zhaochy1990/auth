param location string
param storageName string

@secure()
param sqlAdminLogin string
@secure()
param sqlAdminPassword string

// --- Azure SQL Server + Database ---
resource sqlServer 'Microsoft.Sql/servers@2023-08-01-preview' = {
  name: 'sql-${uniqueString(resourceGroup().id)}'
  location: location
  properties: {
    administratorLogin: sqlAdminLogin
    administratorLoginPassword: sqlAdminPassword
    version: '12.0'
    minimalTlsVersion: '1.2'
    publicNetworkAccess: 'Enabled'
  }
}

// Allow Azure services to access SQL Server
resource sqlFirewallAzure 'Microsoft.Sql/servers/firewallRules@2023-08-01-preview' = {
  parent: sqlServer
  name: 'AllowAzureServices'
  properties: {
    startIpAddress: '0.0.0.0'
    endIpAddress: '0.0.0.0'
  }
}

resource sqlDatabase 'Microsoft.Sql/servers/databases@2023-08-01-preview' = {
  parent: sqlServer
  name: 'auth-db'
  location: location
  sku: {
    name: 'GP_S_Gen5'
    tier: 'GeneralPurpose'
    family: 'Gen5'
    capacity: 1
  }
  properties: {
    collation: 'SQL_Latin1_General_CP1_CI_AS'
    maxSizeBytes: 34359738368  // 32 GB
    autoPauseDelay: 60
    minCapacity: json('0.5')
    useFreeLimit: true
    freeLimitExhaustionBehavior: 'AutoPause'
  }
}

// --- Log Analytics ---
resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: 'common-logs'
  location: location
  properties: {
    sku: { name: 'PerGB2018' }
    retentionInDays: 30
  }
}

// --- Storage Account + File Share (for JWT keys) ---
resource storageAccount 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: storageName
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
  properties: {
    minimumTlsVersion: 'TLS1_2'
    allowBlobPublicAccess: false
  }
}

resource fileService 'Microsoft.Storage/storageAccounts/fileServices@2023-05-01' = {
  parent: storageAccount
  name: 'default'
}

resource fileShare 'Microsoft.Storage/storageAccounts/fileServices/shares@2023-05-01' = {
  parent: fileService
  name: 'jwt-keys'
  properties: {
    shareQuota: 1
  }
}

// --- Outputs ---
output sqlServerFqdn string = sqlServer.properties.fullyQualifiedDomainName
output sqlDatabaseName string = sqlDatabase.name
output logAnalyticsName string = logAnalytics.name
output storageAccountName string = storageAccount.name
output fileShareName string = fileShare.name
