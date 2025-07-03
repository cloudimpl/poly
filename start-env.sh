#!/bin/bash
set -e

read -p "Enter polycode_ORG_ID: " POLYCODE_ORG_ID
read -p "Enter polycode_ENV_ID: " POLYCODE_ENV_ID

export POLYCODE_ORG_ID
export POLYCODE_ENV_ID

PASS=$(aws ecr get-login-password --region us-east-1)

echo "$PASS" | docker login --username AWS --password-stdin 537413656254.dkr.ecr.us-east-1.amazonaws.com
echo "$PASS" | docker login --username AWS --password-stdin 485496110001.dkr.ecr.us-east-1.amazonaws.com

echo "ECR login successful."
docker-compose up
