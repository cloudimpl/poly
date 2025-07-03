#!/bin/bash
set -e

PASS=$(aws ecr get-login-password --region us-east-1)

echo "$PASS" | docker login --username AWS --password-stdin 537413656254.dkr.ecr.us-east-1.amazonaws.com
echo "$PASS" | docker login --username AWS --password-stdin 485496110001.dkr.ecr.us-east-1.amazonaws.com

echo "ECR login successful."
docker-compose up
