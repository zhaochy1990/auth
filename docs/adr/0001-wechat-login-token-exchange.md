# WeChat mini-program login via OAuth2 Token Exchange

WeChat mini-program login and account binding are exposed as the RFC 8693 `token_exchange` grant on `POST /oauth/token` — `grant_type=token_exchange`, `subject_token` = the `wx.login()` code, `subject_token_type=wechat_mini_program` — instead of the originally planned custom `/api/auth/wechat-login` / `wechat-bind` endpoints. Auth resolves the calling client application, reads that application's WeChat mini-program credentials, calls WeChat `code2Session`, and issues the standard token pair, keeping the API pure OAuth2 so future third-party identity providers (Google, Apple, …) reuse the same grant vocabulary.

Status: accepted

Considered: custom endpoints guarded by an ad-hoc `X-Client-Secret` header (rejected — the mini-program is a public client, a secret shipped inside client code is extractable, and the `X-Client-Secret` scheme was non-standard; superseded by the token-exchange design in PR #34's rework).

Key behaviors:

- **Bound identity** → `200` standard token response (`access_token`, `refresh_token`, `token_type`, `expires_in`, `scope`). No user object: clients fetch identity via `GET /api/users/me` (which exposes `wechat_bound`).
- **Unbound identity** → `400 {"error":"wechat_needs_binding"}` — the client switches to the bind flow.
- **Bind** reuses the same grant with `email` + `password` extension parameters: wrong credentials → `401`; the identity already bound to another account → `409 wechat_already_bound`; success → tokens (bound and logged in).
- **Rebind is out of scope**: binding a *different* WeChat identity while one is already bound → `409`; changing/replacing a binding is deferred and will be redesigned (no unbind endpoint yet).
- **Client authentication**: public-client style — `client_id` in the body is sufficient for `token_exchange`; HTTP Basic (client_id:client_secret) is still accepted when a confidential caller sends it.

Consequences: the mini-program makes an extra `/api/users/me` call after login; the "login response carries the user object" contract from issue stride-devops#173 is superseded for the WeChat flow.
