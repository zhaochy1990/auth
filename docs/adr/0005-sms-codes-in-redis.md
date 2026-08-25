# SMS verification codes live in Redis, not MySQL, and the service fails closed on Redis outage

The auth service previously had no Redis — MySQL is the only runtime store. SMS verification codes are stored in Redis (`sms:code:{phone}`, `sms:cooldown:{phone}`, `sms:daily:{phone}`) because the code lifecycle is pure TTL + atomic consume, which MySQL would model awkwardly (explicit expiry sweeps, conditional updates). A Redis outage makes `/api/auth/sms/send` and `/verify` return 503 — deliberately no fallback to MySQL or in-memory codes, since two stores for the same credential invites inconsistency bugs that are worse than the outage itself. Production adds a Redis instance alongside the existing CVM/MySQL setup.

Status: accepted

Known trade-off: SMS login now depends on third infrastructure (Redis) in addition to MySQL and Tencent Cloud SMS; if Redis is down, SMS login is down even though MySQL is healthy. Codes are stored as SHA-256 hashes so a Redis dump does not leak usable codes.
