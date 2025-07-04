#!/bin/sh
set -e

# Install user defined additional libraries
if [ -f "./install.sh" ]; then \
  chmod +x ./install.sh
  ./install.sh
else \
  echo "No install.sh provided."
fi && \

/tmp/sidecar &

poly-watcher --depfile=go.mod --depcommand="go mod tidy && go mod download" --build="next-gen && GOOS=linux GOARCH=amd64 go build -o /main ." --run="/main" --include=.go,go.mod --exclude=.git,.polycode
