# SMS auto-registration respects the invite-code gate when registration is invite-only

SMS login auto-registers on first successful verification (matching the WeChat provider pattern), but when `AUTH_REQUIRE_INVITE_CODE` is on, creating a new user via SMS requires a valid `invite_code` in the `/verify` request — reuse of the existing email-registration gate, including single-use atomic consumption and membership/user-type grants. Deliberately not an open registration backdoor: an invite-only deployment that lets SMS bypass the gate would defeat the gate. `invite_code` is ignored when the gate is off, and never required for existing users logging in.

Status: accepted

Known trade-off: the SMS verify request carries an extra optional field whose meaning depends on global config, which is surprising until you know the gate exists. The alternative — a separate env flag controlling SMS registration independently — was rejected to avoid two registration gates drifting apart.
