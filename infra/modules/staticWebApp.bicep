@description('Static Web App name.')
param name string

@description('Static Web App region (supported in a limited set of regions).')
param location string
param tags object = {}

@allowed([
  'Free'
  'Standard'
])
param sku string = 'Free'

resource swa 'Microsoft.Web/staticSites@2023-12-01' = {
  name: name
  location: location
  tags: tags
  sku: {
    name: sku
    tier: sku
  }
  properties: {
    // Content is built and uploaded by the Deploy workflow
    // (Azure/static-web-apps-deploy), not from a linked repo.
    allowConfigFileUpdates: true
  }
}

output defaultHostname string = swa.properties.defaultHostname
output name string = swa.name
