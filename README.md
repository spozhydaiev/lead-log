Project: Telegram bot for private manager notes.

Purpose:
- Capture raw manager notes.
- Generate daily/weekly summaries.
- Generate source-backed daily and weekly recall summaries.
- No employee monitoring.
- No performance scoring.
- No employee ranking.
- No emotion analysis.
- No meeting recording.
- No HR recommendations.

Tech:
- Go
- Telegram Bot API
- Postgres
- OpenAI-compatible LLM API
- Docker Compose

Important product guardrails:
- AI only structures manager-provided notes.
- AI must not invent facts.
- AI must not evaluate employees.
- AI must not recommend HR decisions.

## Configuration

The app reads configuration from environment variables. A local `.env` file is also loaded when present.

### Required environment variables

- `TELEGRAM_BOT_TOKEN` — Telegram bot token from BotFather.
- `DATABASE_URL` — Postgres connection string.
- `LLM_API_KEY` — API key for the configured OpenAI-compatible LLM provider.
- `ALLOWED_TELEGRAM_USER_IDS` — comma-separated Telegram user IDs allowed to use the bot. Startup fails when the parsed allowlist is empty; this prevents accidental public access.

### Optional environment variables

- `LLM_BASE_URL` — OpenAI-compatible API base URL. Defaults to `https://api.openai.com/v1`.
- `LLM_MODEL` — model name used for parsing and summaries. Defaults to `gpt-4.1-mini`.
- `TELEGRAM_UPDATE_PROCESSING_TIMEOUT` — how long an in-flight Telegram update may stay `processing` before a retry can reclaim it. Defaults to `2m`, which leaves headroom above the 45-second LLM HTTP timeout.
- `NOTE_ENRICHMENT_PROCESSING_TIMEOUT` — how long a note enrichment claim may stay `processing` before explicit retry/reprocess or the background worker can reclaim it as stale. Defaults to `3m`, which is longer than the Telegram update timeout and the LLM HTTP timeout.
- `NOTE_ENRICHMENT_WORKER_ENABLED` — enables the PostgreSQL-backed background note enrichment worker. Defaults to `true`; set to `false` to keep captured notes pending until explicit processing.
- `NOTE_ENRICHMENT_POLL_INTERVAL` — worker polling interval. Defaults to `5s`.
- `NOTE_ENRICHMENT_BATCH_SIZE` — maximum notes claimed per worker poll. Defaults to `10`.
- `NOTE_ENRICHMENT_WORKER_CONCURRENCY` — maximum concurrent enrichment jobs per app instance. Defaults to `1`.
- `NOTE_ENRICHMENT_MAX_ATTEMPTS` — maximum automatic enrichment attempts before a note remains failed without automatic retry. Defaults to `3`.
- `RESPONSE_LANGUAGE` — language for agent-generated and static bot responses. Supported values are `en` (English), `uk` (Ukrainian), and `pl` (Polish). Defaults to `en`; unsupported values fail startup instead of falling back.
- `DAILY_SUMMARY_ENABLED` — starts the background daily summary scheduler when set to `true`. Defaults to `false`.
- `DAILY_SUMMARY_TIME` — local time for scheduled daily summaries in `HH:MM` format. Defaults to `08:45`.
- `DAILY_SUMMARY_TIMEZONE` — IANA timezone used for scheduled daily summaries and `/daily` date boundaries. Defaults to `Europe/Warsaw`.
- `DAILY_SUMMARY_MODE` — scheduled summary source-date mode. Supported values are `previous_workday` (default) and `current_day`.
- `LOG_LEVEL` — structured log verbosity: `debug`, `info`, `warn`, or `error`. Defaults to `info`.
- `LOG_FORMAT` — structured log output format: `text` or `json`. Defaults to `text`.


### Response language

Set `RESPONSE_LANGUAGE` to choose the language used for user-facing summaries, headings, action text, warnings, help text, empty-state messages, cache notices, and scheduled daily summaries. English is the default when the variable is omitted.

Supported values:

- `en` — English
- `uk` — Ukrainian
- `pl` — Polish

Every LLM prompt explicitly asks the model to return user-facing text in the configured language, preserve person names, and keep JSON field names exactly as defined by the schema. Cache entries are language-scoped, so changing `RESPONSE_LANGUAGE` does not reuse a summary generated in another language; `/daily --refresh` and `/weekly --refresh` still force regeneration.

## Logging

LeadLog uses Go's standard `log/slog` package for structured development logs. Configure logs with `LOG_LEVEL` and `LOG_FORMAT`; use `LOG_FORMAT=json` when shipping logs to a collector.

Startup logs include only environment-safe configuration values such as the LLM model, LLM base URL host, response language, note enrichment timeout, background worker settings, daily summary schedule, timezone, and selected log settings. Runtime logs include stable `component` and `operation` fields plus metadata such as Telegram user ID, command name, note length, cache hit/miss, source hash prefix, item counts, duration, HTTP status, and failure stage.

Privacy rules for logs:
- Telegram user IDs may be logged for development diagnostics.
- Raw notes, full LLM prompts, full LLM responses, API keys, Telegram tokens, database passwords, and full `DATABASE_URL` values are not logged.
- At debug level, logs still prefer metadata such as lengths, counts, note IDs, and source hash prefixes over sensitive content.

Example text log:

```text
time=2026-07-12T14:00:00.000Z level=INFO msg="application starting" llm_model=gpt-4.1-mini llm_base_host=api.openai.com daily_summary_enabled=false daily_summary_time=08:45 daily_summary_timezone=Europe/Warsaw daily_summary_mode=previous_workday log_level=info log_format=text
```

Example JSON log:

```json
{"time":"2026-07-12T14:00:00Z","level":"INFO","msg":"cache miss","component":"service","operation":"daily","user_id":42,"source_hash_prefix":"abc12345","cache_hit":false}
```

## Run locally

1. Start Postgres or use Docker Compose:

   ```sh
   docker compose up -d
   ```

2. Export the required environment variables, or create a local `.env` file.

3. Run the bot:

   ```sh
   make run
   ```

   Equivalent command:

   ```sh
   go run ./cmd/lead-log
   ```

## Run with Docker Compose

Start services:

```sh
docker compose up -d
```

Stop services:

```sh
docker compose down
```

Make sure the required environment variables, including a non-empty `ALLOWED_TELEGRAM_USER_IDS` allowlist, are available to the app service before starting it.

## Migrations

Migrations live in `migrations/*.sql` and are embedded into the Go binary.

On startup, the app:
- creates a `schema_migrations` table if needed;
- takes a Postgres advisory lock to avoid concurrent migration runs;
- applies pending `.sql` files in filename order;
- records applied filenames in `schema_migrations`.

There is no separate migration command at the moment; migrations run automatically before the Telegram bot starts polling. Migration startup, applied filenames, no-op runs, and failures are logged with structured fields.

## Scheduled daily summaries

Set `DAILY_SUMMARY_ENABLED=true` to send a weekday morning recap automatically to every explicitly allowed user listed in `ALLOWED_TELEGRAM_USER_IDS`. The default schedule is 08:45 in `Europe/Warsaw` with `DAILY_SUMMARY_MODE=previous_workday`. In this mode the scheduler runs only Monday through Friday, skips Saturday and Sunday, and summarizes the previous workday: Tuesday through Friday recap the previous calendar day, while Monday recaps the previous Friday. If the selected source workday has zero notes, the scheduler does not call the LLM, does not send a Telegram message, and logs the skip without recording a send.

Scheduled-send idempotency is keyed by user and source workday, so a restart does not resend the same recap for the same notes day. `DAILY_SUMMARY_MODE=current_day` keeps the older same-day source-date behavior for compatibility, while still using the configured morning time. Manual `/daily` and `/daily --refresh` are unchanged and continue to summarize the current day on demand.

## Bot commands

- `/start` or `/help` — show help.
- `/note <text>` — quickly save a raw manager note as `pending`; the background worker enriches it later without sending a second Telegram message. `/note` without text is rejected with a usage message and does not create a note.
- Plain text — quickly save a raw manager note as `pending` without typing `/note`; the Telegram handler does not call the LLM. Unknown slash commands are rejected with a localized help hint and are not saved as notes.
- `/now <text>` — save and atomically claim the raw manager note, then synchronously structure it through the reusable LLM enrichment flow and return the structured response.
- `/open` — show open loops created only by explicit `/now` processing.
- `/done <action_id>` — mark an open loop as done.
- `/daily` — generate today’s manager digest from raw notes and cache the response without creating actions or people notes.
- `/daily --refresh` — regenerate today’s digest/cache without creating actions or people notes.
- `/weekly` — generate a digest for the last 7 days.
- `/weekly --refresh` — regenerate the weekly digest instead of using cache.
- `/ask <question>` — ask about saved work history with source-backed answers.
- `/ticket <ticket-key>` — show deterministic ticket history from exact ticket mentions and bounded raw-note fallback; no Jira/API/LLM lookup is used.
- `/person <name>` — show deterministic, source-backed context for a canonical person using existing aliases; no LLM lookup or performance judgment is used.
- `/agenda <person-name>` — prepare a bounded, deterministic 1:1 agenda from the same person context (open actions of any age; commitments/follow-ups from 60 days; concerns, questions, decisions, and positive notes from 30 days; completed actions and raw context from 14 days). Structured records take precedence and no LLM is used.

`/agenda` resolves canonical names and aliases through the same read-only, user-scoped resolution and `PersonContext` retrieval as `/person`. It orders high-priority overdue actions and concerns first, then normal-priority open actions, commitments, follow-ups, questions, and active decisions; positive and general context are low priority. Done actions are recognition candidates for 14 days, and superseded/reversed decisions are excluded. Deduplication prefers the first structured representation (actions before people notes, decisions before raw context) using source note plus normalized text. Exact canonical/alias raw-note matches are a 14-day context-only fallback: they are never promoted to concerns or commitments.

Out of MVP scope for now: performance review packs, Jira integration, calendar integration, monitoring integrations, voice transcription, web UI, multi-user workspaces, and billing. Existing database tables and migrations for older data are left in place for backward compatibility.


### Telegram update idempotency

Incoming authorized Telegram text messages are claimed in a persistent PostgreSQL inbox table (`telegram_updates`) before command handling. The table stores Telegram update/message identity, status (`processing`, `processed`, `failed`), timestamps, attempt count, command name, and a safely truncated internal error. Raw message text is not stored in this table.

`telegram_update_id` is unique, with an additional `(telegram_chat_id, telegram_message_id)` unique index as a message-level fallback. Duplicate updates that are already `processed` or currently `processing` are skipped without rerunning service logic, calling the LLM, mutating domain tables, or sending another Telegram response. Failed updates can be reclaimed until the built-in attempt cap is reached; `processing` updates older than `TELEGRAM_UPDATE_PROCESSING_TIMEOUT` can be reclaimed after a crash.

For raw note capture and `/done`, the domain mutation and `processed` marker are committed in one short database transaction after the update has been claimed. `/note` and plain text only save raw `pending` notes; they never call the LLM in the Telegram handler. `/now` creates the note directly in `processing` with an active enrichment claim before calling the LLM, so the background worker cannot claim that note between creation and synchronous enrichment. The LLM HTTP request is outside database transactions; enrichment persistence uses a short transaction that replaces generated actions and people notes owned by that note. Read-only commands and validation responses are marked processed before sending the Telegram response. If the database commit succeeds but sending the Telegram response fails, the update remains processed and automatic replay is intentionally skipped; domain correctness is preferred over response resend for this MVP.

### Background note enrichment

The note enrichment worker uses the `notes` table as a PostgreSQL-backed queue; there is no Redis, Kafka, RabbitMQ, `LISTEN/NOTIFY`, or separate queue table. Each poll atomically claims a bounded batch with `FOR UPDATE SKIP LOCKED`, ordered by `next_processing_at`, `created_at`, and `id`, then marks each claimed note `processing` and increments `processing_attempts`. Multiple app instances can run workers safely because PostgreSQL row locks and the `processing` status are the ownership source of truth.

The worker processes `pending` notes, `failed` notes whose `next_processing_at` is due, and stale `processing` notes older than `NOTE_ENRICHMENT_PROCESSING_TIMEOUT`. It skips `processed` notes, fresh `processing` notes, notes delayed until a future retry time, and notes that have reached `NOTE_ENRICHMENT_MAX_ATTEMPTS`. A successful enrichment clears retry metadata and marks the note `processed`. Failed jobs use fixed backoff by active attempt: about 1 minute after attempt 1, 5 minutes after attempt 2, and 30 minutes after later retryable attempts. At the configured max attempts the note remains `failed` with no `next_processing_at`, preventing infinite automatic LLM calls while still leaving explicit retry/reprocess service methods available.

If `/now` enrichment fails, the saved note remains in the same lifecycle and can be retried later by the background worker without sending another Telegram response. This preserves `/now` as the only synchronous structured-response flow while allowing transient LLM or database failures to recover.

Failure windows are intentionally simple: a crash after claim but before the LLM leaves the note `processing` until stale reclaim; a crash after LLM but before persistence can call the LLM again after stale reclaim; a crash after the persistence commit leaves the note `processed`, and later workers skip it. LLM timeouts mark the note `failed` and schedule retry. If the database is unavailable during polling, the worker logs the error and waits for the next poll interval instead of busy looping or stopping the process.

## Logging and privacy

Production logs are intentionally operational-only. Runtime logs must not include Telegram identifiers, usernames, internal user or record IDs, person names, aliases, ticket keys, entity values, raw notes, questions, summaries, action/decision/people-note text, retrieval snippets, LLM prompts, LLM responses, HTTP bodies, database URLs, API keys, tokens, authorization headers, or raw provider/database error bodies.

Allowed log fields are limited to diagnostic metadata such as component, operation, failure stage, status/result, safe counters, durations, model name, prompt version, cache hit/miss, worker attempt number, batch size, HTTP status, error class, sanitized error message, and operation-scoped correlation IDs.

Each incoming Telegram operation or background job receives a random `operation_id`. It is generated from random bytes, is scoped to a single operation, is not derived from user/chat/message/note IDs, and is not stored as a stable user identifier. Related service, store, worker, scheduler, and LLM logs use this ID when available.

Errors logged through the application use safe error fields. Provider HTTP error bodies and database error messages are not emitted as raw log text; logs keep stable classes such as `llm_http_error`, `database_error`, or `daily_digest_validation` plus safe operational metadata.

Debug logs follow the same privacy rules as info/warn/error logs. There is no production flag that re-enables sensitive logging.

## Private web HTTP API (v1 foundation)

The optional HTTP adapter runs alongside Telegram polling, the scheduler, and enrichment worker and calls the same application service/store boundary. Set `HTTP_ENABLED=false` to disable it. With HTTP enabled (the default), configure `HTTP_ADDRESS` (default `0.0.0.0`), `HTTP_PORT` (`8080`), `HTTP_READ_TIMEOUT` (`10s`), `HTTP_WRITE_TIMEOUT` (`30s`), `HTTP_IDLE_TIMEOUT` (`60s`), `HTTP_ALLOWED_ORIGINS` (comma-separated exact origins), `WEB_API_TOKEN`, and `WEB_API_TELEGRAM_USER_ID`. The mapped Telegram identity must already be in `ALLOWED_TELEGRAM_USER_IDS`; it is resolved server-side and is never accepted from API input or returned by `/me`.

Bearer token authentication is temporary and intended for private MVP use. It should be replaced before public multi-user launch.

The API provides unauthenticated `GET /healthz` and bounded-database-check `GET /readyz`, plus authenticated `/api/v1/me`, notes list/create, and actions list/status-update endpoints. Authenticated responses use `Cache-Control: no-store`; CORS uses exact configured origins, never a wildcard. Lists use opaque cursor pagination ordered by `(created_at DESC, id DESC)`. Public representations are prefixed (`note_123`, `action_31`), but are not a security boundary; every query and mutation is scoped to the backend-derived user.

`POST /api/v1/notes` stores a pending note for the existing worker and does not wait for the LLM. POST /notes is not idempotent in API v1 foundation. Frontend must avoid automatic retries until Idempotency-Key support is added. Distributed rate limiting is also required before a public launch; this private foundation instead bounds request bodies, headers, timeouts, and page sizes. The source-controlled contract is `api/openapi.yaml`.
