# syntax=docker/dockerfile:1.6

FROM 537413656254.dkr.ecr.us-east-1.amazonaws.com/polycode/next-builder:latest AS base

ARG APP_FOLDER
WORKDIR /project/${APP_FOLDER}

RUN go install github.com/cloudimpl/poly-watcher@v0.3.0
RUN go install github.com/go-delve/delve/cmd/dlv@latest

# Copy from the extra build context "platform"
COPY --from=platform --chmod=0755 runtime/sidecar /tmp/sidecar
COPY --from=platform --chmod=0755 entrypoint.sh /tmp/entrypoint.sh

CMD ["/tmp/entrypoint.sh"]
