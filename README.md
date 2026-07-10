Project: Telegram bot for private manager notes.

Purpose:
- Capture raw manager notes.
- Generate daily/weekly summaries.
- Extract follow-ups, ticket drafts, people highlights.
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

## Bot commands

- `/start` or `/help` — show help.
- `/note <text>` — save and structure a manager note.
- Plain text — save and structure a manager note without typing `/note`.
- `/open` — show open loops.
- `/done <action_id>` — mark an open loop as done.
- `/person <name>` — show person-specific context for the last 90 days.
- `/person <name> --refresh` — regenerate person context instead of using cache.
- `/ticket <context>` — generate a Jira-style ticket draft.
- `/daily` — generate today’s manager digest.
- `/daily --refresh` — regenerate today’s digest instead of using cache.
- `/weekly` — generate a digest for the last 7 days.
- `/weekly --refresh` — regenerate the weekly digest instead of using cache.
