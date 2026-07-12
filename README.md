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

Startup logs include only environment-safe configuration values such as the LLM model, LLM base URL host, response language, daily summary schedule, timezone, and selected log settings. Runtime logs include stable `component` and `operation` fields plus metadata such as Telegram user ID, command name, note length, cache hit/miss, source hash prefix, item counts, duration, HTTP status, and failure stage.

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
- `/note <text>` — save a raw manager note without immediate AI processing. `/note` without text is rejected with a usage message and does not create a note.
- Plain text — save a raw manager note without typing `/note` and without immediate AI processing. Unknown slash commands are rejected with a localized help hint and are not saved as notes.
- `/now <text>` — save and immediately structure a manager note through the LLM parsing flow.
- `/open` — show open loops created only by explicit `/now` processing.
- `/done <action_id>` — mark an open loop as done.
- `/daily` — generate today’s manager digest from raw notes and cache the response without creating actions or people notes.
- `/daily --refresh` — regenerate today’s digest/cache without creating actions or people notes.
- `/weekly` — generate a digest for the last 7 days.
- `/weekly --refresh` — regenerate the weekly digest instead of using cache.

Out of MVP scope for now: person context, 1:1 agenda generation, performance review packs, Jira integration, calendar integration, monitoring integrations, voice transcription, web UI, multi-user workspaces, and billing. Existing database tables and migrations for older data are left in place for backward compatibility.
