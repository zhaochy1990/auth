@description('Container Apps managed environment name.')
param name string
param location string
param tags object = {}

@description('Existing Log Analytics workspace name (same resource group).')
param logAnalyticsName string

@description('Existing storage account name backing the JWT-keys file share.')
param storageAccountName string

@description('Name of the file share holding the JWT keys.')
param keysShareName string

@description('Name of the environment storage link the app volume references.')
param keysStorageName string = 'jwtkeys'

resource law 'Microsoft.OperationalInsights/workspaces@2023-09-01' existing = {
  name: logAnalyticsName
}

resource sa 'Microsoft.Storage/storageAccounts@2023-05-01' existing = {
  name: storageAccountName
}

resource env 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: name
  location: location
  tags: tags
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: law.properties.customerId
        sharedKey: law.listKeys().primarySharedKey
      }
    }
  }
}

// Link the JWT-keys file share into the environment so the app can mount it.
resource keysStorage 'Microsoft.App/managedEnvironments/storages@2024-03-01' = {
  parent: env
  name: keysStorageName
  properties: {
    azureFile: {
      accountName: sa.name
      accountKey: sa.listKeys().keys[0].value
      shareName: keysShareName
      accessMode: 'ReadOnly'
    }
  }
}

output environmentId string = env.id
output keysStorageName string = keysStorage.name
