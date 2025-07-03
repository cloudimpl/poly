#!/bin/bash
set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <app-folder> [host-port]"
  exit 1
fi

APP_PATH="$1"
APP_NAME=$(basename "$APP_PATH")
HOST_PORT="$2"

echo "App folder: $APP_PATH"
echo "App name: $APP_NAME"

if [ -n "$HOST_PORT" ]; then
  echo "Host port: $HOST_PORT"
fi

DEV_TOOLS_ROOT=${PWD}

(
  cd "$APP_PATH"

  # Find git root
  GIT_ROOT=$(git rev-parse --show-toplevel)
  echo "Git root: $GIT_ROOT"

  echo "Change working directory to $GIT_ROOT"
  cd "$GIT_ROOT"

  # Build SERVICE_IDS from services folder
  SERVICE_IDS=$(find "$APP_PATH/services" -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | paste -sd "," -)
  echo "Service IDs: $SERVICE_IDS"

  # Build Docker image
  IMAGE_TAG="${APP_NAME}:latest"
  echo "Image tag: $IMAGE_TAG"

  # Determine APP_FOLDER arg
  if [ "$APP_PATH" = "$GIT_ROOT" ]; then
    APP_FOLDER="."
  else
    APP_FOLDER="$APP_NAME"
  fi
  echo "APP_FOLDER build arg: $APP_FOLDER"

  docker build -f "$DEV_TOOLS_ROOT/Dockerfile" --build-arg APP_FOLDER="$APP_FOLDER" -t "$IMAGE_TAG" .

  # Build docker run command
  DOCKER_RUN_CMD=(
    docker run --rm -it
    --network polycode-dev
    -v "$DEV_TOOLS_ROOT/runtime:/tmp"
    -e NATS_HOST=nats
    -e polycode_ORG_ID="$polycode_ORG_ID"
    -e polycode_ENV_ID="$polycode_ENV_ID"
    -e AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID"
    -e AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY"
    -e AWS_REGION="$AWS_REGION"
    -e DIRECT_ACCESS="true"
    -e polycode_APP_NAME="$APP_NAME"
    -e polycode_SERVICE_IDS="$SERVICE_IDS"
    -e polycode_RUNTIME="dev"
  )

  # Add port mapping if HOST_PORT is given
  if [ -n "$HOST_PORT" ]; then
    DOCKER_RUN_CMD+=(-p "$HOST_PORT:8080")
  fi

  # Add image
  DOCKER_RUN_CMD+=("$IMAGE_TAG")

  # Run the container
  "${DOCKER_RUN_CMD[@]}"
)
