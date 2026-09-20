# The mini-program binds WeChat with a phone + SMS code, through its own grant

When a WeChat identity has no account (`needs_binding`), the mini-program must
prove an account before binding. Today the only proof is email + password, which
the new phone-first accounts do not have, so a **new grant
`wechat_phone_bind`** takes `subject_token` (the `wx.login` code) plus `phone` +
`code` (scene `bind_phone`), and binds the exchanged WeChat identity to the
account that phone number identifies — creating a **手机号账号** when nobody holds
that number yet. It is a separate grant rather than a third credential shape on
`token_exchange`: that grant's bind branch is defined by "prove an existing
password account, then attach", and overloading it with a second proof would
make one grant mean two unrelated things. The email + password bind branch on
`token_exchange` is unchanged, because those accounts still exist and must be
able to bind.

A verified phone is sufficient authorisation to bind, and this is deliberate:
the code proves ownership of the number, so the number's account is the target.
This is **not** account merging — nothing moves between accounts. A phone number
already held by a different account is rejected, and a WeChat identity already
bound to a different account still returns `409 wechat_already_bound`.

Status: accepted
