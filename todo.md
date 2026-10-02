# Keys to create

Every value goes into `.env` at config time. Code carries the env var *name* only and runs fine with it unset — when it's time to set them, ask the agent to guide you through it.

## Wired now — official source keys

| Env var | Feature | Create at | Notes |
|---|---|---|---|
| `MACRO_TERMINAL_BLS_API_KEY` | BLS: CPI, unemployment, nonfarm payrolls, average hourly earnings, JOLTS job openings | https://data.bls.gov/registrationEngine/ | Key emailed from labstat@bls.gov. Unregistered = 25 queries/day vs our 48/day cadence, so this one matters for steady operation. **Renews yearly.** |
| `MACRO_TERMINAL_BEA_API_KEY` | BEA: national and industry accounts | https://apps.bea.gov/api/signup/ | 36-char UserID, activate via email link. No disposable emails. |
| `MACRO_TERMINAL_EIA_API_KEY` | EIA: energy prices, stocks, production | https://www.eia.gov/opendata/register.php | Key emailed. |
| `ESTAT_APP_ID` | e-Stat Japan: official Japanese statistics | https://www.e-stat.go.jp/api/en/api-info/api-guide → sign up → Mypage → API → Issue Application ID | Max 3 app IDs; localhost URL is accepted. |
| `STATSNZ_API_KEY` | Stats NZ: Aotearoa Data Explorer (SDMX) | https://portal.apis.stats.govt.nz/how-to-subscribe | Azure subscription key; sent as `subscription-key` query parameter, which that gateway accepts. |

## Not wired yet — arrives with the feature

| Env var | Feature | Create at | Notes |
|---|---|---|---|
| Resend API key | Signup OTP email | https://resend.com → Dashboard → API Keys | Email slice. Only email sent by the product is the OTP. |
| New Relic license key | Telemetry / APM (API, worker, providers, DB/Redis) | https://one.newrelic.com → API Keys | Observability slice. |
| Discord webhook URL | Notifications channel | Discord server → Settings → Integrations → Webhooks → New Webhook | Notifications slice; webhook URL, not an API key. |
| Slack webhook URL | Notifications channel | https://api.slack.com/apps → create app → Incoming Webhooks | Notifications slice; webhook URL, not an API key. |
| Gemini API key | AI enrichment, one-shot explanations, unmapped-queue suggestions | https://aistudio.google.com/apikey | AI slice. Deterministic mapping stays the matcher — Gemini is fallback/enrichment only. |

## Rules

- Optional by design: an unset key never breaks the flow — the affected source or feature just runs within free limits (BLS is the exception: free limits sit below our cadence).
- No agent ever sets key *values*; the owner does that at config time, with guidance on request.
- Deployment/auth keys (VPS, Cloudflare, Authlier) are TBD when those slices start — do not guess them.
