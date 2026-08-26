# WeChat mini-program credentials configured per Application via the admin API, not env vars

`wechat_app_id` / `wechat_app_secret` live on the Application row and [were]
set through the admin API (`POST|PATCH /admin/applications`), despite the
original issue (stride-devops#173) asking for environment variables.
Per-application storage is what makes Auth an IDaaS — each client application
can point at a different WeChat mini-program — and keeps the secret server-side
rather than duplicated across every deployment's env.

Status: superseded by stride-devops#185

### Update (stride-devops#185)

The OAuth2 `token_exchange` WeChat flow now reads the WeChat app credential
(`appid` / `secret`) from the **calling application's WeChat provider config** —
the `auth_app_providers` row with `provider_id='wechat'`, managed via
`POST|DELETE /admin/applications/{id}/providers` with
`config: {"appid":"...","secret":"..."}` — rather than from the application's
own `wechat_app_id` / `wechat_app_secret` columns. The resolved `appid` is also
used as the WeChat identity key for `auth_user_wechat_links` (identity lookup
and binding). A missing or incomplete provider config (no `appid` or `secret`)
makes `token_exchange` return `400 wechat_not_configured`.

The `wechat_app_id` / `wechat_app_secret` columns are retained for backwards
compatibility (the admin API still accepts and echoes them), but the
`token_exchange` flow no longer reads them.

Known trade-off (unchanged): the WeChat secret is stored in plaintext at rest
(WeChat's `code2Session` requires the raw value; by contrast `client_secret` is
stored hashed). The admin API never echoes the secret back. Encrypt-at-rest is
a documented follow-up if the deployment demands it.
