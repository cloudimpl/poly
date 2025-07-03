#!/bin/bash
set -e

# Load .env file
if [ -f .env ]; then
  echo "Loading environment variables from .env"
  export $(grep -v '^#' .env | xargs)
else
  echo "No .env file found, continuing without loading env vars"
fi

# Function to calculate MD5 in a portable way
calc_md5() {
  if command -v md5sum >/dev/null 2>&1; then
    md5sum "$1" | awk '{print $1}'
  elif command -v md5 >/dev/null 2>&1; then
    md5 -q "$1"
  else
    echo "Error: no MD5 tool found" >&2
    exit 1
  fi
}

mkdir -p ./.runtime
LOCAL_FILE="./.runtime/sidecar"
CHECKSUM_FILE="./.runtime/sidecar.checksum"
S3_BINARY_URI="s3://buildspecs.polycode.app/polycode/engine/${SIDECAR_VERSION}"
S3_CHECKSUM_URI="s3://buildspecs.polycode.app/polycode/engine/${SIDECAR_VERSION}.checksum"

# Download the latest checksum file
echo "Downloading checksum file..."
aws s3 cp "$S3_CHECKSUM_URI" "$CHECKSUM_FILE"

LATEST_CHECKSUM=$(cat "$CHECKSUM_FILE")
echo "Latest checksum: $LATEST_CHECKSUM"

# Compute local file MD5 if exists
if [ -f "$LOCAL_FILE" ]; then
  LOCAL_MD5=$(calc_md5 "$LOCAL_FILE")
else
  LOCAL_MD5="none"
fi

echo "Local checksum: $LOCAL_MD5"

# Compare and download if needed
if [ "$LATEST_CHECKSUM" != "$LOCAL_MD5" ]; then
  echo "Checksum differs or file missing. Downloading sidecar..."
  aws s3 cp "$S3_BINARY_URI" "$LOCAL_FILE"
  chmod +x "$LOCAL_FILE"
  echo "Download complete: $LOCAL_FILE"
else
  echo "Checksum matches. Skipping download."
fi

# ECR login
PASS=$(aws ecr get-login-password --region us-east-1)

echo "$PASS" | docker login --username AWS --password-stdin 537413656254.dkr.ecr.us-east-1.amazonaws.com
echo "$PASS" | docker login --username AWS --password-stdin 485496110001.dkr.ecr.us-east-1.amazonaws.com

echo "ECR login successful."

# Start services
docker compose up
