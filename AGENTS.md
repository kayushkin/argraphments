# About argraphments

## What it is

Turns a conversation into an argument tree you can browse. A recording or upload is transcribed with Whisper (OpenAI), Claude pulls out the claims and how they answer each other, and the result is a nested tree of HTML `<details>` elements. It is served under `/argraphments` on the personal site, through nginx.

## Where this prompt lives

These sections are stored in agent-store as a project prompt collection and rendered, with identical text, to `AGENTS.md` and `CLAUDE.md` at the root of this repo, so that whichever file a harness reads it gets the same thing. Edit them on dash `/files`, or edit either rendered file: the 15-minute scan carries the edit back into the sections and out to the other file. The host prompt keeps one row for this repo with only what an agent elsewhere needs.

# How it works

## Code layout

`main.go` holds the HTTP server, routes and API handlers; `youtube.go` imports from YouTube; `sample.go` holds sample data; `storage/` keeps everything in SQLite. Pages are server-rendered Go templates (`templates/`) with HTMX partials; `frontend/` is a small Vite and TypeScript build into `static/dist`. Keep it HTMX-first: do not add a heavy JavaScript framework.

# Access and operations

## Running and deploying

The system unit `argraphments.service` runs the installed binary from this repo's directory and listens on `ARGRAPHMENTS_ADDR`. Every setting is declared once in `settings.go` with llm-bridge `servicesettings`; a set `ARGRAPHMENTS_` variable nobody declared stops the service at start. It reads both model keys from auth-store at start, by credential id (`ARGRAPHMENTS_ANTHROPIC_CREDENTIAL_ID`, `ARGRAPHMENTS_OPENAI_CREDENTIAL_ID`), and refuses to start without them. auth-store's URL, its token and those ids live in a host-local, root-only drop-in, `auth-store.conf` beside the live unit, never in this repo; there is no `.env` file. `./deploy.sh` checks the drop-in sets all four, builds, installs the binary and the unit from `argraphments.service` in this repo, restarts it and checks that it answers; the template in the repo is the only copy of the unit, so edit it there, never the installed one.

# Working in this repo

## Tests and builds

`go test ./...` before and after a change. When you change `frontend/`, run `npm run build` there so `static/dist` is current before you deploy.
