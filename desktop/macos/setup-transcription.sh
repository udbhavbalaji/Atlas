#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Darwin ]]; then
  echo "Run this setup on the Mac that will use Atlas." >&2
  exit 1
fi
if ! command -v brew >/dev/null; then
  echo "Homebrew is required for whisper.cpp: https://brew.sh" >&2
  exit 1
fi

brew install whisper.cpp
model_dir="$HOME/Library/Application Support/Atlas/models"
model="$model_dir/ggml-base.en.bin"
mkdir -p "$model_dir"
if [[ ! -f "$model" ]]; then
  temporary="$model.download"
  trap 'rm -f "$temporary"' EXIT
  curl --fail --location --retry 2 \
    'https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin' \
    --output "$temporary"
  echo "137c40403d78fd54d454da0f9bd998f78703390c  $temporary" | shasum -a 1 -c -
  mv "$temporary" "$model"
fi
echo "Local transcription is ready: $model"
