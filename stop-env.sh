#!/bin/bash
set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <environment-id>"
  echo "Error: <environment-id> is required."
  exit 1
fi

export ENVIRONMENT_ID="$1"

docker compose -f docker-compose-env.yml -p polycode-env-$ENVIRONMENT_ID down
