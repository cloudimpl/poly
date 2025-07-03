#!/bin/bash
set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <app-folder>"
  exit 1
fi

APP_PATH="$1"
APP_NAME=$(basename "$APP_PATH")
echo "App folder: $APP_PATH"
echo "App name: $APP_NAME"

# Find git root
GIT_ROOT=$(git rev-parse --show-toplevel)
echo "Git root: $GIT_ROOT"

# Build SERVICE_IDS from services folder
SERVICE_IDS=$(find "$APP_PATH/services" -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | paste -sd "," -)
echo "Service IDs: $SERVICE_IDS"

# Copy Dockerfile to git root if not already there
if [ ! -f "$GIT_ROOT/Dockerfile" ]; then
  echo "Copying Dockerfile to git root..."
  cp "$APP_PATH/Dockerfile" "$GIT_ROOT/Dockerfile"
fi

# Build Docker image
IMAGE_TAG="${APP_NAME}:latest"
docker build --build-arg APP_FOLDER="$APP_NAME" -t "$IMAGE_TAG" "$GIT_ROOT"

# Run the container
docker run --rm -it \
  --network polycode-dev \
  -v "$PWD/runtime:/tmp" \
  -e NATS_HOST=nats \
  -e polycode_ORG_ID="$polycode_ORG_ID" \
  -e polycode_ENV_ID="$polycode_ENV_ID" \
  -e AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID" \
  -e AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY" \
  -e AWS_REGION="$AWS_REGION" \
  -e DIRECT_ACCESS="true" \
  -e polycode_APP_NAME="$APP_NAME" \
  -e polycode_SERVICE_IDS="$SERVICE_IDS" \
  -e polycode_RUNTIME="dev" \
  "$IMAGE_TAG"
