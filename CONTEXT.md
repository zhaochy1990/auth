# Auth Service Context

The auth backend (`sources/dev/authentication-go`) — an OAuth2 / IDaaS service that issues tokens for STRIDE applications and lets users log in with third-party identities such as WeChat mini-program.

## Language

**WeChat identity**:
The pair (mini-program appid, openid) that identifies a WeChat user within one mini-program. The same person has different identities in different mini-programs; `unionid` is the cross-mini-program key, present only when the mini-programs share a WeChat Open Platform binding.
_Avoid_: "WeChat account", "微信用户" (the person, not the credential pair)

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
