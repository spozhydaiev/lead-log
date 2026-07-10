Project: Telegram bot for private manager notes.

Purpose:
- Capture raw manager notes.
- Generate daily/weekly summaries.
- Extract follow-ups, ticket drafts, people highlights.
- No employee monitoring.
- No performance scoring.
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