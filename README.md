# argraphments

Conversations, structured. Upload or record a conversation, get a nested argument tree you can browse.

## Stack

- Go backend
- HTMX + vanilla JS frontend
- Whisper API (transcription)
- Claude API (structure extraction)
- HTML `<details>` for nested collapsible statements

## Setup

The server reads its Anthropic and OpenAI keys from [auth-store](https://github.com/kayushkin/auth-store) at start, by credential id, and refuses to start without them. Every setting is declared in `settings.go`:

```bash
export ARGRAPHMENTS_AUTH_STORE_URL=http://127.0.0.1:8303
export ARGRAPHMENTS_AUTH_STORE_TOKEN=…                 # auth-store's bearer token
export ARGRAPHMENTS_ANTHROPIC_CREDENTIAL_ID=cred_…     # provider anthropic, field api_key
export ARGRAPHMENTS_OPENAI_CREDENTIAL_ID=cred_…        # provider openai, field api_key
go run .                                               # listens on ARGRAPHMENTS_ADDR, default 127.0.0.1:8086
```

## Deploy

`./deploy.sh` builds, installs the binary and the systemd unit from `argraphments.service`, restarts it and checks that it answers. The auth-store settings come from a host-local drop-in beside the installed unit.
