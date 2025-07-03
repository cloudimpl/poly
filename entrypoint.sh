#!/bin/sh
set -e

# Start sidecar in background
/tmp/sidecar &

# Start CompileDaemon
exec CompileDaemon \
  --build="next-gen && go mod tidy && go mod download && go build -o /main ." \
  --command=./main
