# Stockroom

[![CI](https://github.com/Timo348/Stockroom/actions/workflows/ci.yml/badge.svg)](https://github.com/Timo348/Stockroom/actions/workflows/ci.yml)
[![Docker Hub](https://img.shields.io/docker/v/timo348/stockroom?label=Docker%20Hub)](https://hub.docker.com/r/timo348/stockroom)
[![Live demo](https://img.shields.io/badge/demo-GitHub%20Pages-2d6a4f)](https://timo348.github.io/Stockroom/)

Stockroom is a private, single-household inventory for everyday supplies. It combines a responsive English UI, a versioned REST API, and a persistent SQLite database in one lightweight container.

## What it covers

- Add, edit, delete, search, and filter household items.
- Adjust quantities atomically from the UI or API.
- Track custom units and locations, unit prices, low-stock thresholds, expiry dates, notes, and total inventory value.
- Mark short-lived purchases as `Use soon`, filter them, set them to used with one click, or let their quantity automatically become zero after seven days.
- Choose from 133 locally hosted household icons with automatic suggestions for common item names.
- Capture text, voice, or up to 10 receipts, images, PDFs, audio files, and short videos in one AI inbox batch.
- Inspect health, metadata, summaries, and an OpenAPI 3.1 document.
- Persist data in SQLite with WAL mode and automatic schema initialization.

Accounts, authentication, barcode scanning, and cloud sync are intentionally outside this version. The home page is the fast AI capture entry; `/inbox` separates Processing, Ready to apply, and Needs review. New batches analyze automatically by default when local AI is configured, and the setting affects only future uploads. Ready suggestions never change inventory until `Apply X ready changes` is confirmed. Low confidence, ambiguity, and inventory conflicts stay editable in Needs review. AI assigns every suggestion one supported category; new items inherit its matching category and icon. Text inputs accept 100,000 characters and long shopping lists split automatically into AI-safe parts, up to 500 detected suggestions. Each inbox entry applies atomically.

## Try the live demo

Open [Stockroom on GitHub Pages](https://timo348.github.io/Stockroom/). It is an interactive, browser-only product demo: its seed data and changes stay in your browser, and it never uploads files or calls a model. Run the Docker image for the real API, SQLite persistence, media processing, and local-AI inbox.

## Production quick start

Requirements: Docker Engine or Docker Desktop with Compose.

```powershell
Copy-Item .env.example .env
$env:STOCKROOM_IMAGE = "timo348/stockroom:1.0.0"
docker compose pull
docker compose up -d --no-build
docker compose ps
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). `stockroom_data` retains the SQLite database across image updates. To use the newest release instead, set `STOCKROOM_IMAGE=timo348/stockroom:latest`; pin a numbered tag for predictable production upgrades.

## Run with Docker

Requirements: Docker Desktop or Docker Engine with Compose.

```powershell
Copy-Item .env.example .env
docker compose up -d --build
docker compose ps
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). The Compose health check verifies the HTTP service and database.

For repeated bulk counts, open the unlinked internal helper at [http://127.0.0.1:8080/kiquicklist](http://127.0.0.1:8080/kiquicklist). It loads existing quantities, turns only changed rows into a JSON preview, and imports them through the regular item API. It has no separate authentication and therefore shares the security boundary described below.

## Optional local AI

The bundled [`local-ai`](local-ai/) runtime stays separate from Stockroom and runs independently. Start it first, then join Stockroom to its private Docker network:

~~~powershell
Set-Location .\local-ai
Copy-Item .env.example .env
docker compose -f compose.yaml -f compose.nvidia.yaml up -d --build
docker compose -f compose.yaml -f compose.nvidia.yaml --profile bootstrap run --rm model

Set-Location ..
docker compose -f compose.yaml -f compose.ai.yaml up -d --build
~~~

Optional overlay configures AI_BASE_URL as http://ollama:11434, AI_MODEL as qwen3-vl:8b-instruct, and AI_TRANSCRIPTION_URL as http://whisper:8080/inference. Keep AI_MODEL in this project's `.env` equal to the selected `LOCAL_AI_MODEL`: `qwen3-vl:2b-instruct` for server or `qwen3-vl:8b-instruct` for desktop. `POST /api/v1/intake/batches` stages a batch and schedules analysis when enabled; `POST /api/v1/intake/batches/{id}/apply-ready` applies only its ready changes. The older single-entry and complete-inbox endpoints remain available for compatibility.

Stop the app without deleting data:

```powershell
docker compose down
```

Delete the app and its database volume:

```powershell
docker compose down --volumes
```

## Security boundary

Stockroom has no authentication by design. Compose therefore binds to `127.0.0.1` by default. Do not publish it directly to the internet.

To access it from a trusted LAN, set `BIND_ADDRESS=0.0.0.0` in `.env` and protect access at the network or reverse-proxy layer. The API does not enable browser CORS; server-to-server integrations can still call it normally.

The container runs as an unprivileged user, drops Linux capabilities, uses a read-only root filesystem, and writes only to `/data` plus a small temporary filesystem.

## API

- API index: `GET /api/v1`
- OpenAPI: `GET /api/v1/openapi.json`
- Health: `GET /healthz`
- Items: `GET|POST /api/v1/items`
- Item: `GET|PATCH|DELETE /api/v1/items/{id}`
- Atomic quantity change: `POST /api/v1/items/{id}/adjust`
- Hide empty inventory rows: `GET /api/v1/items?hide_out=true`
- AI inbox: `GET|POST /api/v1/intake`
- Intake entry: `GET|PATCH /api/v1/intake/{id}`
- Inbox analysis: `POST /api/v1/intake/{id}/analysis`
- Batch capture: `POST /api/v1/intake/batches`
- Analyze a batch: `POST /api/v1/intake/batches/{id}/analyze`
- Apply ready batch changes: `POST /api/v1/intake/batches/{id}/apply-ready`
- Apply all ready inbox changes: `POST /api/v1/intake/apply-ready`
- Inbox settings: `GET|PATCH /api/v1/intake/settings`
- Correct, apply, or discard a review: `POST /api/v1/intake/{id}/resolve`
- Analyze complete inbox: `POST /api/v1/intake/analyze-all`
- Add all saved analyses: `POST /api/v1/intake/add-all`
- Analyze and add complete inbox: `POST /api/v1/intake/analyze-and-add-all`
- Intake attachment: `GET /api/v1/intake/{id}/attachments/{attachment_id}`
- Summary and UI choices: `GET /api/v1/summary`, `GET /api/v1/meta`

Create an item from PowerShell:

```powershell
$body = @{
  name = "Dish soap"
  category = "cleaning"
  icon = "soap"
  quantity = 2
  unit = "bottles"
  unit_price_cents = 299
  location = "Utility closet"
  low_stock_threshold = 1
} | ConvertTo-Json

Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/v1/items `
  -ContentType application/json `
  -Body $body
```

Decrease a known item by one:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/api/v1/items/ITEM_ID/adjust `
  -ContentType application/json `
  -Body '{"delta":-1}'
```

All error responses include a stable error code and request ID. Unknown JSON fields are rejected so automation mistakes fail visibly.

An item created or patched with `"short_lived": true` receives a seven-day `auto_consume_at` timestamp while its quantity is positive. A minute worker sets quantity to zero after that timestamp; API reads provide an additional fallback. The item itself is retained for reuse. Set `short_lived` to `false` to cancel, or patch quantity to zero with `clear_auto_consume_at` for an immediate “used now” action. Filter these items with `GET /api/v1/items?status=short_lived`.

AI inbox batches accept optional shared text plus up to 10 files, at most 250 MB total; each file remains limited to 100 MB. Supported inputs are common image, PDF, audio, and short-video formats. Images go directly to Qwen3-VL; visual analysis inventories every distinct visible product, not only the most legible package. PDFs render up to three pages and videos up to three frames before analysis. Audio is transcribed by the private Whisper container. New files live outside SQLite in `/data/intake-tmp`; applying or discarding an entry deletes its file and stored analysis immediately, and pending uploads expire after 24 hours. Analysis results for expired files remain visible for review. Microphone access requires explicit browser permission and recording stops after 60 seconds. Unit conflicts, missing inventory for reductions, and reductions below zero move a ready entry to an editable review instead of changing inventory.

## Data and backup

The database lives at `/data/stockroom.db` in the `stockroom_data` Compose volume. Inventory and staged AI inbox metadata live in SQLite; temporary upload files live in `/data/intake-tmp` and are intentionally excluded from the database backup. SQLite also creates `-wal` and `-shm` files while running.

For a simple consistent file backup, stop writes first:

```powershell
docker compose stop app
docker compose cp app:/data/stockroom.db ./stockroom-backup.db
docker compose start app
```

Keep the backup outside the repository. Restore only while the app is stopped, and retain a copy of the current database before replacing it.

## Releases and automation

- `CI` runs Go tests, `go vet`, and a container build for pull requests and `main`.
- `Publish Docker image` publishes tags such as `v1.0.0` as `1.0.0`, `1.0`, `latest`, and a commit SHA tag. Add repository secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` before using this GitHub Actions workflow.
- `Deploy GitHub Pages demo` publishes only `demo/`; it cannot expose the API, database, or local AI.
- Dependabot checks Go modules, Docker base images, and GitHub Actions weekly.

To build and push a release from a trusted local machine:

```powershell
docker build --build-arg VERSION=1.0.0 -t timo348/stockroom:1.0.0 -t timo348/stockroom:latest .
docker push timo348/stockroom:1.0.0
docker push timo348/stockroom:latest
```

## License

Stockroom is licensed under [Apache License 2.0](LICENSE). Third-party icon
attribution remains in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Development and validation

The runtime is Go 1.25+ with no frontend build step. Static assets are embedded into the binary.

```powershell
go test ./...
go vet ./...
go run ./cmd/stockroom
```

When Go is not installed locally, run the same checks in Docker:

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.25-alpine go test ./...
docker compose build
```

## Third-party asset

The self-hosted icon subset uses [Google Material Symbols](https://developers.google.com/fonts/docs/material_symbols), licensed under Apache License 2.0. The complete license is included in `MATERIAL_SYMBOLS_LICENSE.txt`. No external font or image request is made by the running app.
