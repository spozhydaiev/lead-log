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

### Optional environment variables

- `LLM_BASE_URL` — OpenAI-compatible API base URL. Defaults to `https://api.openai.com/v1`.
- `LLM_MODEL` — model name used for parsing and summaries. Defaults to `gpt-4.1-mini`.
- `ALLOWED_TELEGRAM_USER_IDS` — comma-separated Telegram user IDs allowed to use the bot. If empty, all users are allowed.
- `DAILY_SUMMARY_ENABLED` — starts the background daily summary scheduler when set to `true`. Defaults to `false`.
- `DAILY_SUMMARY_TIME` — local time for scheduled daily summaries in `HH:MM` format. Defaults to `18:00`.
- `DAILY_SUMMARY_TIMEZONE` — IANA timezone used for scheduled daily summaries and `/daily` date boundaries. Defaults to `Europe/Warsaw`.

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

Make sure the required environment variables are available to the app service before starting it.

## Migrations

Migrations live in `migrations/*.sql` and are embedded into the Go binary.

On startup, the app:
- creates a `schema_migrations` table if needed;
- takes a Postgres advisory lock to avoid concurrent migration runs;
- applies pending `.sql` files in filename order;
- records applied filenames in `schema_migrations`.

There is no separate migration command at the moment; migrations run automatically before the Telegram bot starts polling.

## Scheduled daily summaries

Set `DAILY_SUMMARY_ENABLED=true` to send the cached or newly generated daily summary automatically to every user listed in `ALLOWED_TELEGRAM_USER_IDS`. The default schedule is 18:00 in `Europe/Warsaw`. The scheduler reuses the same read-only `Service.Daily` flow as `/daily`: it generates and caches the digest from raw notes, but does not create actions or people notes. It records completed sends so a restart around the scheduled time does not send the same user the same day twice.

## Bot commands

- `/start` or `/help` — show help.
- `/note <text>` — save a raw manager note without immediate AI processing.
- Plain text — save a raw manager note without typing `/note` and without immediate AI processing.
- `/now <text>` — save and immediately structure a manager note through the LLM parsing flow.
- `/open` — show open loops created only by explicit `/now` processing.
- `/done <action_id>` — mark an open loop as done.
- `/daily` — generate today’s manager digest from raw notes and cache the response without creating actions or people notes.
- `/daily --refresh` — regenerate today’s digest/cache without creating actions or people notes.
- `/weekly` — generate a digest for the last 7 days.
- `/weekly --refresh` — regenerate the weekly digest instead of using cache.

Out of MVP scope for now: person context, 1:1 agenda generation, performance review packs, Jira integration, calendar integration, monitoring integrations, voice transcription, web UI, multi-user workspaces, and billing. Existing database tables and migrations for older data are left in place for backward compatibility.
