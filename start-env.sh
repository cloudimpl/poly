#!/bin/bash
set -e

# Load .env file
if [ -f .env ]; then
  echo "Loading environment variables from .env"
  export $(grep -v '^#' .env | xargs)
else
  echo "No .env file found, continuing without loading env vars"
fi

# Download from S3 to ./.runtime
echo "Downloading S3://buildspecs.polycode.app/polycode/engine/${SIDECAR_VERSION} to ./.runtime/sidecar ..."
aws s3 cp "s3://buildspecs.polycode.app/polycode/engine/${SIDECAR_VERSION}" "./.runtime/sidecar"
echo "Download complete: ./runtime/sidecar"
chmod +x ./.runtime/sidecar

PASS=$(aws ecr get-login-password --region us-east-1)

echo "$PASS" | docker login --username AWS --password-stdin 537413656254.dkr.ecr.us-east-1.amazonaws.com
echo "$PASS" | docker login --username AWS --password-stdin 485496110001.dkr.ecr.us-east-1.amazonaws.com

echo "ECR login successful."
docker compose up
