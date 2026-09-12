#!/bin/sh
# Local Honcho for nanobots — recall-capable memory on your own machine.
#
#   ./docker/honcho/honcho.sh up -d --build    start it (first run builds; a few minutes)
#   ./docker/honcho/honcho.sh logs -f deriver  watch it learn
#   ./docker/honcho/honcho.sh down             stop it
#   ./docker/honcho/honcho.sh down -v          stop it and forget everything
#
# Honcho is not vendored here — this clones it on first use, so you get
# upstream's own Dockerfile and migrations rather than a copy of them that
# goes stale. HONCHO_REF pins what gets checked out.
#
# The Gemini key is read from ~/.secrets/nanobots.env and passed through as
# an environment variable. It is never written into the compose file, an
# .env beside it, or anything else on disk here.
set -e
cd "$(dirname "$0")"

HONCHO_REPO="${HONCHO_REPO:-https://github.com/plastic-labs/honcho.git}"
HONCHO_REF="${HONCHO_REF:-main}"

if [ ! -d src-checkout ]; then
  echo "cloning Honcho ($HONCHO_REF) — one time"
  git clone --depth 1 --branch "$HONCHO_REF" "$HONCHO_REPO" src-checkout
fi

[ -f "$HOME/.secrets/nanobots.env" ] && . "$HOME/.secrets/nanobots.env"
if [ -z "$GEMINI_API_KEY" ]; then
  echo "GEMINI_API_KEY is not set." >&2
  echo "Honcho needs one LLM provider to derive memories and answer questions." >&2
  echo "Get a free key at https://aistudio.google.com/apikey and add it to" >&2
  echo "~/.secrets/nanobots.env as GEMINI_API_KEY=..., then run this again." >&2
  echo "(To use OpenAI or Anthropic instead, edit docker/honcho/config.toml.)" >&2
  exit 1
fi
export GEMINI_API_KEY

exec docker compose "$@"
