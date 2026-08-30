# Local AI runtime

This optional stack runs private Ollama and Whisper inference services for
Stockroom. It owns model files only: it does not mount Stockroom data, receive
Stockroom credentials, or execute application tools.

It creates the stable Docker network `local-ai`. Stockroom joins it through
`compose.ai.yaml` and reaches `http://ollama:11434` and
`http://whisper:8080/inference` only inside Docker.

## Start

```powershell
Copy-Item .env.example .env
docker compose up -d --build
docker compose --profile bootstrap run --rm model
.\scripts\smoke-test.ps1 -Model qwen3-vl:8b-instruct
```

Model data stays in the `ollama_data` Docker volume, so normal restarts do not
download it again.

## Hardware presets

For lower-power servers:

```powershell
docker compose --env-file .env --env-file profiles/server.env up -d
docker compose --env-file .env --env-file profiles/server.env --profile bootstrap run --rm model
.\scripts\smoke-test.ps1 -Model qwen3-vl:2b-instruct
```

For NVIDIA Docker hosts, use the RTX 3080 preset and CUDA overlay:

```powershell
docker compose --env-file .env --env-file profiles/desktop-rtx3080.env -f compose.yaml -f compose.nvidia.yaml up -d
docker compose --env-file .env --env-file profiles/desktop-rtx3080.env -f compose.yaml -f compose.nvidia.yaml --profile bootstrap run --rm model
.\scripts\smoke-test.ps1 -Model qwen3-vl:8b-instruct
```

`compose.nvidia.yaml` requires Docker GPU support. Run the smoke test before
relying on hardware acceleration.

## Connect Stockroom

From repository root, set `AI_MODEL` in `.env` to the same selected
`LOCAL_AI_MODEL`, then run:

```powershell
docker compose -f compose.yaml -f compose.ai.yaml up -d --build
```

The AI inbox keeps uncertain results in review. It does not mutate inventory
until a user explicitly applies a ready or corrected suggestion.

## Remote access

Do not expose raw Ollama to the internet. Add authenticated TLS protection
before any cross-machine use.
