@description('Container App name.')
param name string
param location string
param tags object = {}

@description('Container Apps managed environment resource id.')
param environmentId string

@description('Container image (e.g. ghcr.io/<owner>/auth-backend:<version>).')
param image string

@description('App version surfaced at /health.')
param appVersion string = 'dev'

@description('Allowed CORS origins (comma-separated).')
param corsAllowedOrigins string

@description('JWT issuer claim.')
param jwtIssuer string = 'auth-service'

@description('Existing storage account name (the connection string is composed from its key).')
param storageAccountName string

@description('Environment storage link name for the JWT-keys volume.')
param keysStorageName string = 'jwtkeys'

param registryServer string = 'ghcr.io'
param registryUsername string = ''
@secure()
param registryPassword string = ''

param cpu string = '0.5'
param memory string = '1.0Gi'
param minReplicas int = 1
param maxReplicas int = 3

resource sa 'Microsoft.Storage/storageAccounts@2023-05-01' existing = {
  name: storageAccountName
}

var storageConnectionString = 'DefaultEndpointsProtocol=https;AccountName=${sa.name};AccountKey=${sa.listKeys().keys[0].value};EndpointSuffix=${environment().suffixes.storage}'

var useRegistryAuth = !empty(registryUsername)

var secrets = concat(
  [
    {
      name: 'storage-connection-string'
      value: storageConnectionString
    }
  ],
  useRegistryAuth ? [
    {
      name: 'registry-password'
      value: registryPassword
    }
  ] : []
)

var registries = useRegistryAuth ? [
  {
    server: registryServer
    username: registryUsername
    passwordSecretRef: 'registry-password'
  }
] : []

resource app 'Microsoft.App/containerApps@2024-03-01' = {
  name: name
  location: location
  tags: tags
  properties: {
    managedEnvironmentId: environmentId
    configuration: {
      ingress: {
        external: true
        targetPort: 3000
        transport: 'auto'
        allowInsecure: false
      }
      secrets: secrets
      registries: registries
    }
    template: {
      containers: [
        {
          name: 'auth-backend'
          image: image
          resources: {
            cpu: json(cpu)
            memory: memory
          }
          env: [
            {
              name: 'AZURE_STORAGE_CONNECTION_STRING'
              secretRef: 'storage-connection-string'
            }
            {
              name: 'SERVER_HOST'
              value: '0.0.0.0'
            }
            {
              name: 'SERVER_PORT'
              value: '3000'
            }
            {
              name: 'JWT_ISSUER'
              value: jwtIssuer
            }
            {
              name: 'JWT_PRIVATE_KEY_PATH'
              value: '/app/keys/private.pem'
            }
            {
              name: 'JWT_PUBLIC_KEY_PATH'
              value: '/app/keys/public.pem'
            }
            {
              name: 'CORS_ALLOWED_ORIGINS'
              value: corsAllowedOrigins
            }
            {
              name: 'APP_VERSION'
              value: appVersion
            }
            {
              name: 'LOG_FORMAT'
              value: 'json'
            }
            {
              name: 'LOG_LEVEL'
              value: 'info'
            }
          ]
          volumeMounts: [
            {
              volumeName: 'jwt-keys'
              mountPath: '/app/keys'
            }
          ]
          probes: [
            {
              type: 'Liveness'
              httpGet: {
                path: '/health'
                port: 3000
              }
              initialDelaySeconds: 5
              periodSeconds: 30
            }
            {
              type: 'Readiness'
              httpGet: {
                path: '/health'
                port: 3000
              }
              initialDelaySeconds: 3
              periodSeconds: 10
            }
          ]
        }
      ]
      volumes: [
        {
          name: 'jwt-keys'
          storageType: 'AzureFile'
          storageName: keysStorageName
        }
      ]
      scale: {
        minReplicas: minReplicas
        maxReplicas: maxReplicas
      }
    }
  }
}

output fqdn string = app.properties.configuration.ingress.fqdn
output name string = app.name
