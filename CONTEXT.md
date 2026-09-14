# Auth Service Context

The auth backend (`sources/dev/authentication-go`) — an OAuth2 / IDaaS service that issues tokens for STRIDE applications and lets users log in with third-party identities such as WeChat mini-program, or with a mainland-China phone number via SMS verification code.

## Language

**WeChat identity**:
The pair (mini-program appid, openid) that identifies a WeChat user within one mini-program; the `appid` is the mini-program's appid from the application's **WeChat provider config** (below). The same person has different identities in different mini-programs; `unionid` is the cross-mini-program key, present only when the mini-programs share a WeChat Open Platform binding.
_Avoid_: "WeChat account", "微信用户" (the person, not the credential pair)

**WeChat provider config**:
The WeChat mini-program credential (`appid` / `secret`) stored on an application's `auth_app_providers` row (`provider_id='wechat'`), configured via the admin API (`POST /admin/applications/{id}/providers`). It is the source of truth read by the `token_exchange` grant — the only WeChat login path — and it replaced the app-level `wechat_app_id` / `wechat_app_secret` columns, which were removed.
_Avoid_: "WeChat env vars", "app-level WeChat config", "wechat_app_id / wechat_app_secret"

**Binding**:
Linking a WeChat identity to a user account via the token-exchange bind flow (WeChat code + email + password). One account may hold identities from several mini-programs; one identity belongs to exactly one account.
_Avoid_: Linking, attaching

**Bound**:
An account with at least one WeChat identity. Exposed as `wechat_bound` in user responses.
_Avoid_: "has WeChat", "wechat linked"

**needs_binding**:
The outcome when a WeChat identity has no linked account — the login attempt returns `400 wechat_needs_binding` and the client shows the bind flow.
_Avoid_: "not registered", "anonymous"

**Rebind**:
Replacing an account's bound WeChat identity. Out of scope — binding a *different* identity while one is already bound returns `409 wechat_already_bound`; the change-binding flow is not designed yet.
_Avoid_: "switch WeChat", "换绑"

**SMS login**:
Logging in with a mainland-China phone number plus a single-use SMS verification code. A successful verification logs the user in, creating the account on first use — there is no separate registration step.
_Avoid_: "手机登录" (sounds like device login), "验证码登录" (ambiguous)

**SmsCode**:
The short-lived, single-use verification code sent by SMS to prove phone ownership during SMS login. Expires after five minutes and is consumed on the first successful verification.
_Avoid_: "验证码" alone (overloaded), "动态码"

**PhoneNumber**:
A mainland-China mobile number, stored as bare 11 digits (no +86 prefix). At most one account holds a given phone number; phone-only accounts have no email.
_Avoid_: "mobile", "phone", 国际手机号

**品牌 OAuth2 绑定**:
Linking a third-party watch brand account (COROS first; Strava / Suunto / Polar later) to a STRIDE user through the brand's official OAuth2 authorization-code flow. It starts from an authenticated session, returns through the public callback `/oauth/link/{provider_id}/callback`, and stores a revocable token instead of the user's watch password. One brand identity belongs to exactly one account; one account holds at most one identity per brand.
_Avoid_: "手表登录" (it never logs in), "第三方登录", "绑定高驰" (the brand is a parameter, not the concept)

**品牌描述符**:
The compile-time description of one watch brand on the link layer — endpoint paths, default scopes, client-authentication style, token-response field mapping, and where the stable user id lives — held in the single registry keyed by `provider_id`. Structural differences between brands live here, not in configuration, so adding a brand does not touch the callback engine.
_Avoid_: "provider config" (that is the per-application credentials and base URL)

**link state** (授权 state):
The opaque, server-minted, single-use handle carried through a **品牌 OAuth2 绑定**. Its payload — STRIDE user, application, provider, callback URI, created-at — lives in Redis with a 10-minute TTL; the handle itself carries no information. The STRIDE user in the payload comes from the authenticated session, never from the callback, which is what makes the flow CSRF-safe.
_Avoid_: "token" (it authorizes nothing), "code" (that is the brand's authorization code)
