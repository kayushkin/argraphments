# argraphments

Conversations, structured. Upload or record a conversation, get a nested argument tree you can browse.

## Stack

- Go backend
- HTMX + vanilla JS frontend
- Whisper API (transcription)
- Claude API (structure extraction)
- HTML `<details>` for nested collapsible statements

## Setup

```bash
cp .env.example .env  # add your API keys
go run .
```

## Deploy

```bash
./update-argraphments.sh
```

Runs on kayushkin.com/argraphments via nginx reverse proxy to :8081.
