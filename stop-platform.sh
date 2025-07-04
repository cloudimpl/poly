#!/bin/bash
set -e

# Verify required tools
for tool in docker; do
  if ! command -v $tool >/dev/null 2>&1; then
    echo "Error: $tool is not installed or not in PATH."
    exit 1
  fi
done

docker compose -f docker-compose-platform.yml -p polycode-platform down
