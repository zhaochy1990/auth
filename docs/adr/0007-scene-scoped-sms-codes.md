# SMS verification codes are scoped to a scene, but their rate limits are not

ADR `0005` stored one code per phone (`sms:code:{phone}`), which was fine while
SMS meant one thing: logging in. Three flows now mint codes — `login`,
`bind_phone` (attaching a phone to the WeChat-bound account) and
`reset_password` — so the key becomes `sms:code:{phone}:{scene}` and both
`/api/auth/sms/send` and the consuming endpoint must pass the same scene. A code
texted for one action can never drive another: the message the user reads
("you are resetting your password") and the action it authorises always agree,
which is what closes the social-engineering gap where an attacker talks a user
into reading out a code they think is for something harmless.

Deliberately **not** scoped: the 60-second resend cooldown and the 10-per-day
cap stay keyed by phone alone (`sms:cooldown:{phone}`, `sms:daily:{phone}`).
Scoping those per scene would silently triple a number's SMS budget and its
abuse surface, and the scene dimension is about replay, not about cost.

Status: accepted (amends ADR `0005`)

Consequences:

- One Tencent Cloud template per scene (the login template `2716979` stays the
  `login` one; `bind_phone` and `reset_password` templates are applied for
  separately), so `tencent_sms_template_id` becomes a per-scene mapping.
- Existing Redis keys change shape, so codes in flight at deploy time (≤ 5
  minutes) are simply lost — no migration.
