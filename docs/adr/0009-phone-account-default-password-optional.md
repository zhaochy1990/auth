# Phone + SMS is the default account shape; the password becomes an optional, per-account credential

Registration now defaults to phone + SMS code (the existing login-or-register
flow), and email + password registration drops to a secondary entry point rather
than being removed — the API stays, the accounts stay, and the login form keeps
its email + password tab. The interesting half is what happens to passwords: a
**手机号账号** is created without one, and **找回密码** is *set-or-reset*, so it
gives a password to the accounts that lack one and replaces it for the accounts
that have one, always returning success so the client never has to know which
case it is in.

That makes the password an **账号密码** — one per account, not one per
identifier. `/api/auth/login` therefore accepts either an email or a phone
number with the same password, and a reset through a phone number changes what
the email + password path accepts. The rejected alternative was one password per
identifier (an email password and a phone password), which would let a single
person hold two different passwords with no way for support to reason about
them. Writing a password that no login path could ever use was the other
rejected option: the whole point of choosing set-or-reset was that the credential
should be real.

Two consequences fall out and are accepted deliberately:

- **A password reset ends every session on the account**, matching
  `admin reset-password`: a forgotten password and a compromised account look
  the same from here, and the client can tell the user to log in again.
- **Account recovery still needs a bound phone.** A **密码账号** that never bound
  one cannot use 找回密码 and falls back to the admin reset; the clients say so
  instead of looping the user. Binding is the fix, and it is offered from an
  authenticated session (`POST /api/users/me/phone`). Moving a phone to a
  different number (*Rebind*) stays out of scope, exactly like WeChat rebinding.

Status: accepted
