# Macro Terminal API

Complete HTTP surface of the Macro Terminal server. The machine-readable contract is the generated OpenAPI document at `GET /openapi.json` (Swagger UI served alongside it); this file is the human-readable map.

## Conventions

- Everything app-facing is under `/api/v1`; health probes are at `/health/live` and `/health/ready`.
- Authentication is an HttpOnly session cookie issued by Authlier. `/auth/*` and `/account/*` are reverse-proxied to Authlier unchanged; the only local account endpoints are `GET/PATCH /api/v1/account`.
- Pagination is keyset-only: pass `limit` and an opaque `cursor`; never offset.
- Every error response is `{"error":{"code":"...","message":"..."}}` with the route's documented code.
- Admin routes require an admin (or super-admin) session; user routes require a signed-in session.

## Health

### `GET /health/live`
Reports whether the API process is alive.

### `GET /health/ready`
Reports whether the API instance can reach PostgreSQL and is ready to serve traffic.

## Authentication (proxied to Authlier)

These paths are forwarded verbatim to the Authlier service; contracts live with Authlier.

- `POST /auth/sign-up` — Create an account and send a verification email.
- `POST /auth/sign-in` — Sign in and receive the session cookie.
- `POST /auth/verify-email` — Verify an email address with a code.
- `POST /auth/resend-verification` — Resend the verification email.
- `POST /auth/forgot-password` — Request a password-reset email.
- `POST /auth/reset-password` — Reset a password with a valid token.
- `POST /auth/sign-out` — Invalidate the session cookie.

## Account

### `GET /api/v1/account`
Get the signed-in account.

### `PATCH /api/v1/account`
Set the account username.

## Feed and articles

### `GET /api/v1/articles`
List the feed (keyset-paginated; `sort=latest|relevance`, optional entity/pair and time filters).

### `GET /api/v1/articles/{id}`
Get one article, including hydrated body, tags, and entity links.

### `GET /api/v1/articles/{id}/related`
Get articles related to one article.

### `POST /api/v1/articles/{id}/explain`
Explain one article with AI (macro context, plain language).

## Search

### `GET /api/v1/search/articles`
Search articles by meaning (semantic vector search, keyset-paginated).

## Watch list and pairs

### `GET /api/v1/assets`
List the watch list.

### `POST /api/v1/assets`
Follow an asset pair.

### `DELETE /api/v1/assets/{entityPairId}`
Unfollow an asset pair.

### `GET /api/v1/entity-pairs`
List available asset pairs.

## Calendar

### `GET /api/v1/calendar-events`
List calendar events (scope/from/to/country/importance/watch filters, keyset-paginated).

### `POST /api/v1/calendar-events/{id}/explain`
Explain one calendar event with AI (why it matters, what to watch).

## Market data

### `GET /api/v1/candles`
List market candles for a pair and timeframe (`1min 5min 15min 1h 4h 1day 1week 1month`; larger frames are aggregated server-side).

## Live feed

### `GET /api/v1/events`
Stream live feed invalidation signals over SSE (`event: feed`, payload `{"type":"feed"}`); clients refetch their current query.

## Notifications

### `GET /api/v1/notifications`
List the signed-in user's notifications (keyset-paginated).

### `GET /api/v1/notifications/unread-count`
Count unread notifications.

### `POST /api/v1/notifications/{id}/read`
Mark one notification read.

### `POST /api/v1/notifications/read-all`
Mark all notifications read.

### `GET /api/v1/notification-settings`
Get notification settings: the in-app kill switch, connected webhook channels with masked endpoints, and the pair ids with alerts enabled.

### `PUT /api/v1/notification-channels/in-app`
Set the in-app channel switch.

### `POST /api/v1/notification-channels`
Connect a webhook channel (`channel=slack|discord`, `webhookUrl`). Stores the URL encrypted, never returns it again, and subscribes the channel to every pair that already has alerts.

### `DELETE /api/v1/notification-channels/{channel}`
Disconnect a notification channel.

### `PUT /api/v1/notification-alerts`
Set a pair's alert preference (on/off for the signed-in user).

## Admin

All admin routes require an admin session.

### Users

#### `GET /api/v1/admin/users`
List user accounts.

#### `POST /api/v1/admin/users/{id}/promote`
Promote a user to administrator.

#### `POST /api/v1/admin/users/{id}/demote`
Demote an administrator to a regular user.

#### `POST /api/v1/admin/users/{id}/suspend`
Suspend a user account.

#### `POST /api/v1/admin/users/{id}/reactivate`
Reactivate a suspended user account.

### Sources and configuration

#### `GET /api/v1/admin/sources`
List sources.

#### `POST /api/v1/admin/sources`
Create a source.

#### `GET /api/v1/admin/source-configurations`
List source configurations.

#### `POST /api/v1/admin/source-configurations`
Create a source configuration (cadence, selectors, provider keys).

#### `PATCH /api/v1/admin/source-configurations/{id}`
Replace a source configuration payload.

### Knowledge graph

#### `GET /api/v1/admin/entities`
List entities.

#### `POST /api/v1/admin/entities`
Create an entity.

#### `GET /api/v1/admin/entity-pairs`
List entity pairs.

#### `POST /api/v1/admin/entity-pairs`
Create an entity pair.

#### `GET /api/v1/admin/indicators`
List economic indicators.

#### `POST /api/v1/admin/indicators`
Create an economic indicator.

#### `GET /api/v1/admin/knowledge-terms`
List knowledge terms.

#### `POST /api/v1/admin/knowledge-terms`
Create a knowledge term.

### Calendar curation

#### `GET /api/v1/admin/calendar-events`
List calendar events (admin view, unfiltered).

#### `POST /api/v1/admin/calendar-events`
Create a calendar event.

#### `PATCH /api/v1/admin/calendar-events/{id}`
Update a calendar event.

#### `POST /api/v1/admin/calendar-events/{id}/archive`
Archive a calendar event.

#### `POST /api/v1/admin/calendar-events/{id}/restore`
Restore an archived calendar event.

### Jobs

#### `GET /api/v1/admin/jobs/failed`
List failed jobs.

#### `POST /api/v1/admin/jobs/{id}/replay`
Replay a failed job.
