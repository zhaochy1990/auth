using './main.bicep'

param namePrefix = 'auth'

// Pin to a released tag in production (e.g. ':2026.6.2'); 'latest' tracks the
// newest pushed image.
param backendImage = 'ghcr.io/zhaochy1990/auth-backend:latest'
param appVersion = 'latest'

// MUST be set to the real frontend origin(s) before applying.
param corsAllowedOrigins = 'https://your-frontend.example.com'

param jwtIssuer = 'auth-service'

// Private GHCR image: set the username here and provide the PAT via the
// GHCR_TOKEN environment variable at deploy time (never commit the token).
// Leave both empty for a public image.
param registryUsername = ''
param registryPassword = readEnvironmentVariable('GHCR_TOKEN', '')

param frontendLocation = 'eastasia'
param frontendSku = 'Free'
