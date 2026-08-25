# WeChat identities stored in a `user_wechat_links` table, not on `users`

A user's WeChat identities live in a dedicated link table (`auth_user_wechat_links`: `user_id`, `wechat_app_id`, `openid`, `unionid`, `created_at`; primary key `(user_id, wechat_app_id)`; unique `(wechat_app_id, openid)`), not as columns on `users`. WeChat `openid` is scoped to a mini-program appid — the same WeChat person has *different* openids in *different* mini-programs — so per-app uniqueness is the correct invariant, and one account can hold identities from several mini-programs (the IDaaS case).

Status: accepted

`unionid` is deliberately **not** globally unique: the same user legitimately shares one unionid across mini-programs bound to the same WeChat Open Platform account, so a unique index would block legitimate multi-app binds. Cross-user unionid collisions are enforced in the handler instead, with `(wechat_app_id, openid)` as the database backstop.

Considered: a single `users.wechat_openid` column with a global unique index (rejected — an account could only ever hold one mini-program's identity; binding a second mini-program silently overwrites the first and breaks that login path, and the appid scoping is inexpressible in one column).
