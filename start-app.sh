#!/bin/bash
set -e

if [ -z "$1" ] || [ -z "$2" ]; then
  echo "Usage: $0 <app-folder> <environment-id> [host-port]"
  echo "Error: <app-folder> and <environment-id> are required."
  exit 1
fi

# Load .env file
if [ -f .env ]; then
  echo "Loading environment variables from .env"
  export $(grep -v '^#' .env | xargs)
else
  echo "No .env file found, continuing without loading env vars"
fi

APP_PATH="$1"
ENVIRONMENT_ID="$2"
HOST_PORT="$3"

if [ ! -d "$APP_PATH" ]; then
  echo "Error: App folder '$APP_PATH' does not exist."
  exit 1
fi

echo "App folder: $APP_PATH"

APP_NAME=$(basename "$APP_PATH")
echo "App name: $APP_NAME"

if [ -n "$HOST_PORT" ]; then
  echo "Host port: $HOST_PORT"
fi

DEV_TOOLS_ROOT=${PWD}

# Find project root
cd "$APP_PATH"
PROJECT_ROOT=$(git rev-parse --show-toplevel)
echo "Project root: $PROJECT_ROOT"

# Build SERVICE_IDS from services folder
SERVICE_IDS=$(find "$APP_PATH/services" -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | paste -sd "," -)
echo "Service IDs: $SERVICE_IDS"

# Determine APP_FOLDER arg
if [ "$APP_PATH" = "$PROJECT_ROOT" ]; then
  APP_FOLDER="."
else
  APP_FOLDER="$APP_NAME"
fi
echo "APP_FOLDER build arg: $APP_FOLDER"

# Build Docker image
cd "$DEV_TOOLS_ROOT"
IMAGE_TAG="${APP_NAME}:latest"
echo "Building Docker image $IMAGE_TAG with APP_FOLDER=$APP_FOLDER"
docker build --load --build-arg APP_FOLDER="$APP_FOLDER" -t "$IMAGE_TAG" .

# Verify Docker network exists
docker network inspect polycode-dev-tools_polycode-dev >/dev/null 2>&1 || {
  echo "Docker network polycode-dev-tools_polycode-dev not found. Please create it."
  exit 1
}

# Build docker run command
DOCKER_RUN_CMD=(
  docker run --rm -it
  --network polycode-dev-tools_polycode-dev
  -v "$PROJECT_ROOT:/project"
  -e AWS_REGION="$AWS_REGION"
  -e AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID"
  -e AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY"
  -e NATS_HOST="nats:4222"
  -e DIRECT_ACCESS="true"
  -e polycode_DEV_MODE=true
  -e polycode_ORG_ID="$ORGANIZATION_ID"
  -e polycode_ENV_ID="$ENVIRONMENT_ID"
  -e polycode_APP_NAME="$APP_NAME"
  -e polycode_SERVICE_IDS="$SERVICE_IDS"
  -e polycode_RUNTIME="dev"
)

if [ -n "$HOST_PORT" ]; then
  DOCKER_RUN_CMD+=(-p "$HOST_PORT:8080")
fi

DOCKER_RUN_CMD+=("$IMAGE_TAG")

# Run the container
echo "Running container..."
"${DOCKER_RUN_CMD[@]}"
