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
Linking a WeChat identity to a user account through the token-exchange bind flow, after proving the account with exactly one credential: either email + password, or a **PhoneNumber** + **SmsCode**. One account may hold identities from several mini-programs; one identity belongs to exactly one account.
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
The short-lived, single-use verification code sent by SMS to prove phone ownership. Expires after five minutes and is consumed on the first successful verification. Every code is minted for one **scene** — `login`, `bind_phone` or `reset_password` — and can only be consumed by that scene, so a code a user received for one action can never drive another.
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

**密码账号**:
An account whose login credential is email + password, created by the historical `/api/auth/register` flow. Every pre-SMS account is one, and they keep logging in unchanged; registration stays reachable as the secondary entry point, while the **手机号账号** is the default. It may bind a **PhoneNumber** later, which is what makes **找回密码** available to it.
_Avoid_: "邮箱账号" (email is the identifier, not the account shape), "老用户"

**手机号账号**:
An account created by SMS login on first use, identified by a **PhoneNumber** and holding an `sms` account credential. It is created **without a password** — the verification code is how it logs in — and normally carries no email and no name. Alongside the historical **密码账号** (email + password) it is one of the two account shapes, and it is the default one.
_Avoid_: "手机用户" (the person, not the account), "phone user"

**账号密码**:
The single password an account has, if it has one. It authenticates the account, not an identifier: whichever identifier it was set through (email or **PhoneNumber**), it works for every identifier the account can log in with. **找回密码** replaces it, so a reset through a phone number also changes what the email + password path accepts. A **手机号账号** has none until **找回密码** gives it one.
_Avoid_: "邮箱密码" / "手机密码" (implies one password per identifier), 密码凭证

**绑定手机号**:
Attaching a **PhoneNumber** to an account and proving ownership of it by **SmsCode**, so the account can be logged in by SMS and can use **找回密码**. It happens either from an authenticated session (the account is already known) or through the WeChat mini-program bind grant (the phone identifies the account, since a phone number nobody has registered yet becomes a new one). Binding is *not* account merging: it never moves a phone number away from another account, and a phone number already held by a different account is rejected. Not to be confused with _Rebind_ (moving a phone to a different number), which is out of scope like WeChat rebinding.
_Avoid_: "关联手机", 手机号合并、账号合并

**找回密码**:
Setting a new **账号密码** by proving **PhoneNumber** ownership with an **SmsCode** (`reset_password` scene). It is *set-or-reset*: an account that has no password gains one, an account that has one has it replaced — the client never has to know which, and the result is always success. It is never a way to "find an account", it is unavailable to an account with no bound phone, and it ends every existing session on the account, since a forgotten password is also what a compromised account looks like.
_Avoid_: "找回账号", "手机号找回", 短信找回（未说明找回的是密码还是账号）

**注册邀请码 gate**:
The switch that makes *first-time* registration require an invite code — for a **手机号账号** exactly as for a **密码账号**. It is never required when an account already exists, so it never affects login. A phone number's first verification therefore carries the invite code when the gate is on, and the rejection happens before the **SmsCode** is consumed.
_Avoid_: 邀请注册（ambiguous about who is invited）, "open registration"

**微信手机号授权（getPhoneNumber）**:
The mini-program flow where the user taps a button and WeChat returns a short-lived `code` that the backend exchanges for the **PhoneNumber** WeChat has on file. It would be an alternative way to supply the phone in **绑定手机号**, not a login path of its own. It is **not used**: STRIDE takes phones from **SmsCode** only, and adding WeChat as a second phone source is a separate, later piece of work.
_Avoid_: "一键登录" (ambiguous with WeChat login), "微信手机号登录"
