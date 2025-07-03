#!/bin/bash
set -e

# Load .env file if it exists
if [ -f .env ]; then
  echo "Loading environment variables from .env"
  export $(grep -v '^#' .env | xargs)
else
  echo "No .env file found, continuing without loading env vars"
fi

# Start only DynamoDB and S3
docker compose up -d dynamodb s3

# Wait briefly for services to be ready
sleep 3

# Create polycode-workflows table if not exists
if ! aws dynamodb describe-table --table-name polycode-workflows --endpoint-url http://localhost:8000 >/dev/null 2>&1; then
  echo "Creating table polycode-workflows..."
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
      {
        "IndexName": "AppId-Timestamp-index",
        "KeySchema": [{"AttributeName": "AppId", "KeyType": "HASH"}, {"AttributeName": "Timestamp", "KeyType": "RANGE"}],
        "Projection": {"ProjectionType": "ALL"}
      },
      {
        "IndexName": "AppId-EndTime-index",
        "KeySchema": [{"AttributeName": "AppId", "KeyType": "HASH"}, {"AttributeName": "EndTime", "KeyType": "RANGE"}],
        "Projection": {"ProjectionType": "ALL"}
      },
      {
        "IndexName": "InstanceId-EndTime-index",
        "KeySchema": [{"AttributeName": "InstanceId", "KeyType": "HASH"}, {"AttributeName": "EndTime", "KeyType": "RANGE"}],
        "Projection": {"ProjectionType": "ALL"}
      },
      {
        "IndexName": "InstanceId-Timestamp-index",
        "KeySchema": [{"AttributeName": "InstanceId", "KeyType": "HASH"}, {"AttributeName": "Timestamp", "KeyType": "RANGE"}],
        "Projection": {"ProjectionType": "ALL"}
      },
      {
        "IndexName": "TraceId-Timestamp-index",
        "KeySchema": [{"AttributeName": "TraceId", "KeyType": "HASH"}, {"AttributeName": "Timestamp", "KeyType": "RANGE"}],
        "Projection": {"ProjectionType": "ALL"}
      }
    ]' \
    --endpoint-url http://localhost:8000

  aws dynamodb update-time-to-live \
    --table-name polycode-workflows \
    --time-to-live-specification "Enabled=true, AttributeName=TTL" \
    --endpoint-url http://localhost:8000

  echo "Created table polycode-workflows with TTL enabled"
else
  echo "Table polycode-workflows already exists"
fi

# Create polycode-logs
if ! aws dynamodb describe-table --table-name polycode-logs --endpoint-url http://localhost:8000 >/dev/null 2>&1; then
  echo "Creating table polycode-logs..."
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
      {
        "IndexName": "AppId-RKEY-index",
        "KeySchema": [{"AttributeName": "AppId", "KeyType": "HASH"}, {"AttributeName": "RKEY", "KeyType": "RANGE"}],
        "Projection": {"ProjectionType": "ALL"}
      }
    ]' \
    --endpoint-url http://localhost:8000

  aws dynamodb update-time-to-live \
    --table-name polycode-logs \
    --time-to-live-specification "Enabled=true, AttributeName=TTL" \
    --endpoint-url http://localhost:8000

  echo "Created table polycode-logs with TTL enabled"
else
  echo "Table polycode-logs already exists"
fi

# Create polycode-data
if ! aws dynamodb describe-table --table-name polycode-data --endpoint-url http://localhost:8000 >/dev/null 2>&1; then
  echo "Creating table polycode-data..."
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

  aws dynamodb update-time-to-live \
    --table-name polycode-data \
    --time-to-live-specification "Enabled=true, AttributeName=TTL" \
    --endpoint-url http://localhost:8000

  echo "Created table polycode-data with TTL enabled"
else
  echo "Table polycode-data already exists"
fi

# Create polycode-meta
if ! aws dynamodb describe-table --table-name polycode-meta --endpoint-url http://localhost:8000 >/dev/null 2>&1; then
  echo "Creating table polycode-meta..."
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

  aws dynamodb update-time-to-live \
    --table-name polycode-meta \
    --time-to-live-specification "Enabled=true, AttributeName=TTL" \
    --endpoint-url http://localhost:8000

  echo "Created table polycode-meta with TTL enabled"
else
  echo "Table polycode-meta already exists"
fi

# S3 bucket polycode-files
if ! aws --endpoint-url http://localhost:9000 s3api head-bucket --bucket polycode-files >/dev/null 2>&1; then
  echo "Creating bucket polycode-files..."
  aws --endpoint-url http://localhost:9000 s3api create-bucket --bucket polycode-files

  echo "Setting CORS configuration for polycode-files..."
  aws --endpoint-url http://localhost:9000 s3api put-bucket-cors --bucket polycode-files --cors-configuration '{
    "CORSRules": [
      {
        "AllowedMethods": ["GET", "PUT", "HEAD"],
        "AllowedOrigins": ["*"],
        "AllowedHeaders": ["*"],
        "MaxAgeSeconds": 3000
      }
    ]
  }'

  echo "Bucket polycode-files created and CORS configured"
else
  echo "Bucket polycode-files already exists"
fi

echo "Cleaning up: stopping dynamodb and s3 containers..."
docker compose down

echo "Setup complete."