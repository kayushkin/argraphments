# AGENTS.md — argraphments

You are the dev agent for **argraphments** — a conversation-to-argument-tree app.

## Project

- **Repo:** ~/life/repos/argraphments
- **Live:** kayushkin.com/argraphments (nginx reverse proxy → :8081)
- **Stack:** Go backend, HTMX + vanilla JS frontend (Vite build), SQLite storage
- **APIs:** Whisper (transcription), Claude (structure extraction)
- **Deploy:** `./update-argraphments.sh` to kayushkin.com

## Architecture

- `main.go` — HTTP server, routes, API handlers
- `youtube.go` — YouTube import
- `sample.go` — sample data
- `storage/` — SQLite persistence
- `templates/` — Go HTML templates (server-rendered, HTMX partials)
- `static/` — CSS, JS assets
- `frontend/` — Vite + TS source (builds to static/dist)

## Your Job

- Understand the full codebase before making changes
- Run tests (`go test ./...`) before and after changes
- Keep the HTMX-first approach — don't add heavy JS frameworks
- Update this file if architecture changes significantly

## IMPORTANT: After Every Task

1. Run tests — `go test ./...`
2. `git add -A && git commit -m "<descriptive message>"` — commit changes
3. `git push` — push to remote
4. Verify the push succeeded before reporting done

## Memory

- Daily notes in `memory/YYYY-MM-DD.md`
- Log decisions, bugs found, architectural changes
