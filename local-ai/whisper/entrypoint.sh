#!/bin/sh
set -eu

model="${WHISPER_MODEL:-base}"
model_path="/models/ggml-${model}.bin"

if [ ! -s "$model_path" ]; then
  /app/models/download-ggml-model.sh "$model" /models
fi

exec /app/server \
  -m "$model_path" \
  -l "${WHISPER_LANGUAGE:-de}" \
  -t "${WHISPER_THREADS:-4}" \
  -nt \
  --host 0.0.0.0 \
  --port 8080 \
  --inference-path /inference \
  --convert
