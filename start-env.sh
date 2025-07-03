#!/bin/bash
set -e

# Load .env file safely
if [ -f .env ]; then
  echo "Loading environment variables from .env"
  set -o allexport
  source .env
  set +o allexport
else
  echo "No .env file found, continuing without loading env vars"
fi

# Verify required tools
for tool in aws docker; do
  if ! command -v $tool >/dev/null 2>&1; then
    echo "Error: $tool is not installed or not in PATH."
    exit 1
  fi
done

# Verify SIDECAR_VERSION is set
if [ -z "$SIDECAR_VERSION" ]; then
  echo "Error: SIDECAR_VERSION is not set. Please set it in your .env file or environment."
  exit 1
fi

# Verify ENVIRONMENT_ID is provided as argument
if [ -z "$1" ]; then
  echo "Error: ENVIRONMENT_ID argument is required. Usage: $0 <environment-id>"
  exit 1
fi

ENVIRONMENT_ID=$1

# Verify docker-compose.yml exists
if [ ! -f docker-compose.yml ]; then
  echo "Error: docker-compose.yml not found in current directory."
  exit 1
fi

# Function to calculate MD5 in a portable way
calc_md5() {
  if command -v md5sum >/dev/null 2>&1; then
    md5sum "$1" | awk '{print $1}'
  elif command -v md5 >/dev/null 2>&1; then
    md5 -q "$1"
  else
    echo "Error: no MD5 tool found (md5sum or md5 required)." >&2
    exit 1
  fi
}

# Set defaults
AWS_REGION=${AWS_REGION:-us-east-1}

mkdir -p ./.runtime
LOCAL_FILE="./.runtime/sidecar"
CHECKSUM_FILE="./.runtime/sidecar.checksum"
S3_BINARY_URI="s3://buildspecs.polycode.app/polycode/engine/${SIDECAR_VERSION}"
S3_CHECKSUM_URI="s3://buildspecs.polycode.app/polycode/engine/${SIDECAR_VERSION}.checksum"

# Download checksum file
echo "Downloading checksum file..."
aws s3 cp "$S3_CHECKSUM_URI" "$CHECKSUM_FILE"

LATEST_CHECKSUM=$(cat "$CHECKSUM_FILE")
echo "Latest checksum: $LATEST_CHECKSUM"

# Compute local file MD5 if it exists
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
echo "Logging into ECR for region $AWS_REGION..."
PASS=$(aws ecr get-login-password --region "$AWS_REGION")

echo "$PASS" | docker login --username AWS --password-stdin 537413656254.dkr.ecr.us-east-1.amazonaws.com
echo "$PASS" | docker login --username AWS --password-stdin 485496110001.dkr.ecr.us-east-1.amazonaws.com

echo "ECR login successful."

# Start Docker Compose
echo "Starting Docker Compose..."
docker compose up --build --remove-orphans
