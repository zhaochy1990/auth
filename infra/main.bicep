// Infrastructure-as-code for the auth service (resource-group scoped).
//
// Resources: Log Analytics, a Storage account (Table service for app data +
// a File Share for the JWT keys), a Container Apps environment, the backend
// Container App, and the frontend Static Web App.
//
// Deploy:
//   az deployment group what-if -g <rg> -f infra/main.bicep -p infra/main.bicepparam -p registryPassword=<pat>
//   az deployment group create  -g <rg> -f infra/main.bicep -p infra/main.bicepparam -p registryPassword=<pat>
targetScope = 'resourceGroup'

@description('Base name used to derive resource names.')
param namePrefix string = 'auth'

@description('Azure region for most resources.')
param location string = resourceGroup().location

@description('Backend container image, e.g. ghcr.io/<owner>/auth-backend:2026.6.2')
param backendImage string

@description('App version surfaced at /health.')
param appVersion string = 'dev'

@description('Allowed CORS origins (comma-separated) for the backend.')
param corsAllowedOrigins string

@description('JWT issuer claim.')
param jwtIssuer string = 'auth-service'

@description('Container registry server.')
param registryServer string = 'ghcr.io'

@description('Registry username for a private image. Empty = public image (no auth).')
param registryUsername string = ''

@description('Registry token/PAT (read:packages). Pass securely at deploy time; never commit.')
@secure()
param registryPassword string = ''

@description('Backend CPU cores (e.g. "0.5").')
param backendCpu string = '0.5'

@description('Backend memory (e.g. "1.0Gi").')
param backendMemory string = '1.0Gi'

@description('Backend minimum replicas.')
param backendMinReplicas int = 1

@description('Backend maximum replicas.')
param backendMaxReplicas int = 3

@description('Region for the Static Web App (supported in a limited set of regions).')
param frontendLocation string = 'eastasia'

@description('Static Web App SKU.')
@allowed([
  'Free'
  'Standard'
])
param frontendSku string = 'Free'

var tags = {
  application: namePrefix
  managedBy: 'bicep'
}

// Storage account names must be 3-24 chars, lowercase alphanumeric.
var storageAccountName = take(toLower(replace('${namePrefix}st${uniqueString(resourceGroup().id)}', '-', '')), 24)
var keysStorageName = 'jwtkeys'

module logs 'modules/logAnalytics.bicep' = {
  name: 'logAnalytics'
  params: {
    name: '${namePrefix}-logs'
    location: location
    tags: tags
  }
}

module storage 'modules/storage.bicep' = {
  name: 'storage'
  params: {
    name: storageAccountName
    location: location
    tags: tags
    keysShareName: 'jwt-keys'
  }
}

module env 'modules/containerAppEnv.bicep' = {
  name: 'containerAppEnv'
  params: {
    name: '${namePrefix}-env'
    location: location
    tags: tags
    logAnalyticsName: logs.outputs.name
    storageAccountName: storage.outputs.accountName
    keysShareName: storage.outputs.keysShareName
    keysStorageName: keysStorageName
  }
}

module backend 'modules/containerApp.bicep' = {
  name: 'backend'
  params: {
    name: '${namePrefix}-backend'
    location: location
    tags: tags
    environmentId: env.outputs.environmentId
    image: backendImage
    appVersion: appVersion
    corsAllowedOrigins: corsAllowedOrigins
    jwtIssuer: jwtIssuer
    storageAccountName: storage.outputs.accountName
    keysStorageName: env.outputs.keysStorageName
    registryServer: registryServer
    registryUsername: registryUsername
    registryPassword: registryPassword
    cpu: backendCpu
    memory: backendMemory
    minReplicas: backendMinReplicas
    maxReplicas: backendMaxReplicas
  }
}

module frontend 'modules/staticWebApp.bicep' = {
  name: 'frontend'
  params: {
    name: '${namePrefix}-dashboard'
    location: frontendLocation
    tags: tags
    sku: frontendSku
  }
}

@description('Backend public FQDN.')
output backendFqdn string = backend.outputs.fqdn

@description('Backend health URL.')
output backendHealthUrl string = 'https://${backend.outputs.fqdn}/health'

@description('Storage account holding app tables + the JWT-keys file share.')
output storageAccountName string = storage.outputs.accountName

@description('Static Web App default hostname.')
output frontendHostname string = frontend.outputs.defaultHostname
