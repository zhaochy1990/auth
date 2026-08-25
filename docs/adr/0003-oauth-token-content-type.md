# `/oauth/token` accepts both form-urlencoded and JSON

`POST /oauth/token` accepts `application/x-www-form-urlencoded` — the format RFC 6749/8693 mandate, so standard OAuth2 client libraries work unmodified — and, for backward compatibility, `application/json`, which is what the service's existing grants have always used. gin's `ShouldBind` dispatches on Content-Type, so no legacy caller changes.

Status: accepted

Rationale: the token endpoint is the one place standard clients touch; JSON-only would force every off-the-shelf OAuth2 library to be customised, which is the opposite of an IDaaS. The JSON path is kept because the existing `authorization_code` / `password` / `refresh_token` / `client_credentials` grants and this repo's integration tests already send JSON. Do not "simplify" this to a single format without checking both caller classes.
