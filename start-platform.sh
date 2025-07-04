#!/usr/bin/env bash
set -e

# === Verify required tools ===
for tool in aws docker; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Error: $tool is not installed or not in PATH."
    exit 1
  fi
done

# === Determine MD5 tool ===
if command -v md5sum >/dev/null 2>&1; then
  MD5_TOOL="md5sum"
elif command -v md5 >/dev/null 2>&1; then
  MD5_TOOL="md5 -q"
else
  echo "Error: no MD5 tool found (md5sum or md5 required)." >&2
  exit 1
fi

calc_md5() {
  $MD5_TOOL "$1" | awk '{print $1}'
}

mkdir -p ./.runtime
LOCAL_FILE="./.runtime/sidecar"
CHECKSUM_FILE="./.runtime/sidecar.checksum"
S3_BINARY_URI="s3://buildspecs.polycode.app/polycode/engine/latest"
S3_CHECKSUM_URI="s3://buildspecs.polycode.app/polycode/engine/latest.checksum"

# === Download checksum file ===
echo "Downloading checksum file..."
if ! aws s3 cp "$S3_CHECKSUM_URI" "$CHECKSUM_FILE"; then
  echo "Error: failed to download checksum file."
  exit 1
fi

LATEST_CHECKSUM=$(cat "$CHECKSUM_FILE")
echo "Latest checksum: $LATEST_CHECKSUM"

# === Compare local file checksum ===
if [ -f "$LOCAL_FILE" ]; then
  LOCAL_MD5=$(calc_md5 "$LOCAL_FILE")
else
  LOCAL_MD5="none"
fi
echo "Local checksum: $LOCAL_MD5"

if [ "$LATEST_CHECKSUM" != "$LOCAL_MD5" ]; then
  echo "Checksum differs or file missing. Downloading sidecar..."
  aws s3 cp "$S3_BINARY_URI" "$LOCAL_FILE"
  chmod +x "$LOCAL_FILE"
  echo "Download complete: $LOCAL_FILE"
else
  echo "Checksum matches. Skipping download."
fi

# === Start Docker Compose ===
echo "Starting Docker Compose..."
docker compose -f docker-compose-platform.yml -p polycode-platform up &

# === Wait briefly for services to be ready ===
sleep 3

# === DynamoDB local setup ===
export AWS_ACCESS_KEY_ID=local
export AWS_SECRET_ACCESS_KEY=local

create_table_if_missing() {
  local table_name="$1"
  local create_cmd="$2"

  if ! aws dynamodb describe-table --table-name "$table_name" --endpoint-url http://localhost:8000 >/dev/null 2>&1; then
    echo "Creating table $table_name..."
    eval "$create_cmd"
    echo "Created table $table_name"
  else
    echo "Table $table_name already exists"
  fi
}

create_table_if_missing "polycode-workflows" "
aws dynamodb create-table \
  --table-name polycode-workflows \
  --billing-mode PAY_PER_REQUEST \
  --attribute-definitions \
    AttributeName=PKEY,AttributeType=S \
    AttributeName=RKEY,AttributeType=S \
    AttributeName=AppId,AttributeType=S \
    AttributeName=EndTime,AttributeType=N \
    AttributeName=InstanceId,AttributeType=S \
    AttributeName=Timestamp,AttributeType=N \
    AttributeName=TraceId,AttributeType=S \
  --key-schema \
    AttributeName=PKEY,KeyType=HASH \
    AttributeName=RKEY,KeyType=RANGE \
  --global-secondary-indexes '[
    {\"IndexName\":\"AppId-Timestamp-index\",\"KeySchema\":[{\"AttributeName\":\"AppId\",\"KeyType\":\"HASH\"},{\"AttributeName\":\"Timestamp\",\"KeyType\":\"RANGE\"}],\"Projection\":{\"ProjectionType\":\"ALL\"}},
    {\"IndexName\":\"AppId-EndTime-index\",\"KeySchema\":[{\"AttributeName\":\"AppId\",\"KeyType\":\"HASH\"},{\"AttributeName\":\"EndTime\",\"KeyType\":\"RANGE\"}],\"Projection\":{\"ProjectionType\":\"ALL\"}},
    {\"IndexName\":\"InstanceId-EndTime-index\",\"KeySchema\":[{\"AttributeName\":\"InstanceId\",\"KeyType\":\"HASH\"},{\"AttributeName\":\"EndTime\",\"KeyType\":\"RANGE\"}],\"Projection\":{\"ProjectionType\":\"ALL\"}},
    {\"IndexName\":\"InstanceId-Timestamp-index\",\"KeySchema\":[{\"AttributeName\":\"InstanceId\",\"KeyType\":\"HASH\"},{\"AttributeName\":\"Timestamp\",\"KeyType\":\"RANGE\"}],\"Projection\":{\"ProjectionType\":\"ALL\"}},
    {\"IndexName\":\"TraceId-Timestamp-index\",\"KeySchema\":[{\"AttributeName\":\"TraceId\",\"KeyType\":\"HASH\"},{\"AttributeName\":\"Timestamp\",\"KeyType\":\"RANGE\"}],\"Projection\":{\"ProjectionType\":\"ALL\"}}
  ]' \
  --endpoint-url http://localhost:8000
"

create_table_if_missing "polycode-logs" "
aws dynamodb create-table \
  --table-name polycode-logs \
  --billing-mode PAY_PER_REQUEST \
  --attribute-definitions \
    AttributeName=PKEY,AttributeType=S \
    AttributeName=RKEY,AttributeType=N \
    AttributeName=AppId,AttributeType=S \
  --key-schema \
    AttributeName=PKEY,KeyType=HASH \
    AttributeName=RKEY,KeyType=RANGE \
  --global-secondary-indexes '[
    {\"IndexName\":\"AppId-RKEY-index\",\"KeySchema\":[{\"AttributeName\":\"AppId\",\"KeyType\":\"HASH\"},{\"AttributeName\":\"RKEY\",\"KeyType\":\"RANGE\"}],\"Projection\":{\"ProjectionType\":\"ALL\"}}
  ]' \
  --endpoint-url http://localhost:8000
"

create_table_if_missing "polycode-data" "
aws dynamodb create-table \
  --table-name polycode-data \
  --billing-mode PAY_PER_REQUEST \
  --attribute-definitions \
    AttributeName=PKEY,AttributeType=S \
    AttributeName=RKEY,AttributeType=S \
  --key-schema \
    AttributeName=PKEY,KeyType=HASH \
    AttributeName=RKEY,KeyType=RANGE \
  --endpoint-url http://localhost:8000
"

create_table_if_missing "polycode-meta" "
aws dynamodb create-table \
  --table-name polycode-meta \
  --billing-mode PAY_PER_REQUEST \
  --attribute-definitions \
    AttributeName=PKEY,AttributeType=S \
    AttributeName=RKEY,AttributeType=S \
  --key-schema \
    AttributeName=PKEY,KeyType=HASH \
    AttributeName=RKEY,KeyType=RANGE \
  --endpoint-url http://localhost:8000
"

# === MinIO S3 setup ===
export AWS_ACCESS_KEY_ID=minioadmin
export AWS_SECRET_ACCESS_KEY=minioadmin

if ! aws --endpoint-url http://localhost:9000 s3api head-bucket --bucket polycode-files >/dev/null 2>&1; then
  echo "Creating bucket polycode-files..."
  aws --endpoint-url http://localhost:9000 s3api create-bucket --bucket polycode-files
  echo "Bucket polycode-files created"
else
  echo "Bucket polycode-files already exists"
fi

wait
