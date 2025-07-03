#!/bin/bash
set -e

# Prompt for version
read -p "Enter VERSION: " VERSION

# Ensure ./.runtime exists
mkdir -p ./.runtime

# Download from S3 to ./.runtime
echo "Downloading S3://buildspecs.polycode.app/polycode/engine/${VERSION} to ./.runtime ..."
aws s3 cp "s3://buildspecs.polycode.app/polycode/engine/${VERSION}" "./.runtime/sidecar"

echo "Download complete: ./runtime/${VERSION}"
