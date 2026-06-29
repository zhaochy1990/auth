@description('Storage account name (3-24 chars, lowercase alphanumeric).')
param name string
param location string
param tags object = {}

@description('File share holding the JWT keys mounted into the backend.')
param keysShareName string = 'jwt-keys'

resource sa 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: name
  location: location
  tags: tags
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
  properties: {
    minimumTlsVersion: 'TLS1_2'
    allowBlobPublicAccess: false
    supportsHttpsTrafficOnly: true
  }
}

// Table service holds the app's data (authusers, authapplications, ...).
resource tableServices 'Microsoft.Storage/storageAccounts/tableServices@2023-05-01' = {
  parent: sa
  name: 'default'
}

resource fileServices 'Microsoft.Storage/storageAccounts/fileServices@2023-05-01' = {
  parent: sa
  name: 'default'
}

resource keysShare 'Microsoft.Storage/storageAccounts/fileServices/shares@2023-05-01' = {
  parent: fileServices
  name: keysShareName
  properties: {
    shareQuota: 1
  }
}

output accountName string = sa.name
output keysShareName string = keysShareName
