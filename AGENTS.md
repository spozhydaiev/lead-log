# AGENTS.md

## Project overview

This is a private Telegram bot for team leads/managers.
It captures manager-provided notes and turns them into reminders, daily summaries, weekly summaries, ticket drafts, and person-specific context.

## Product boundaries

Do not implement:
- employee monitoring
- Slack/Jira/GitHub activity analysis for performance
- meeting recording
- emotion analysis
- performance scores
- employee ranking
- HR decision recommendations
- automated employment decisions

The assistant may:
- summarize notes
- extract action items
- generate ticket drafts
- prepare 1:1 agenda drafts
- group manager-provided people notes
- show source-backed highlights

## Engineering rules

- Keep the code simple.
- Prefer explicit service methods over generic abstractions.
- Add migrations for all DB changes.
- Update README when commands or env variables change.
- Do not commit secrets.
- Do not log raw LLM API keys or Telegram tokens.
- Do not log full sensitive note content unless in local debug mode.

## Testing

When changing parsing, aliases, cache, or migrations, add or update tests where practical.