#!/usr/bin/env bash
set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <environment-id>"
  echo "Error: <environment-id> is required."
  exit 1
fi

export ENVIRONMENT_ID="$1"

# === Verify required tools ===
for tool in docker; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Error: $tool is not installed or not in PATH."
    exit 1
  fi
done

# === Set defaults ===
AWS_REGION=${AWS_REGION:-us-east-1}
ECR_ACCOUNTS=("537413656254" "485496110001")

# === ECR login ===
echo "Logging into ECR for region $AWS_REGION..."
PASS=$(aws ecr get-login-password --region "$AWS_REGION")

for account in "${ECR_ACCOUNTS[@]}"; do
  echo "$PASS" | docker login --username AWS --password-stdin "${account}.dkr.ecr.${AWS_REGION}.amazonaws.com"
done
echo "ECR login successful."

# === Start Docker Compose ===
echo "Starting Docker Compose..."
docker compose -f docker-compose-env.yml -p polycode-env-$ENVIRONMENT_ID up