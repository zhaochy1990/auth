# WeChat mini-program credentials configured per Application via the admin API, not env vars

`wechat_app_id` / `wechat_app_secret` live on the Application row and are set through the admin API (`POST|PATCH /admin/applications`), despite the original issue (stride-devops#173) asking for environment variables. Per-application storage is what makes Auth an IDaaS — each client application can point at a different WeChat mini-program — and keeps the secret server-side rather than duplicated across every deployment's env.

Status: accepted

Known trade-off: the WeChat secret is stored in plaintext at rest (WeChat's `code2Session` requires the raw value; by contrast `client_secret` is stored hashed). The admin API never echoes the secret back. Encrypt-at-rest is a documented follow-up if the deployment demands it.
