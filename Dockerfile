FROM 537413656254.dkr.ecr.us-east-1.amazonaws.com/polycode/next-builder:latest AS base

ARG APP_FOLDER
WORKDIR /project/${APP_FOLDER}

RUN go install github.com/cloudimpl/poly-watcher@latest

COPY ./.runtime/sidecar /tmp/sidecar
COPY entrypoint.sh /tmp/entrypoint.sh
RUN chmod +x /tmp/sidecar && chmod +x /tmp/entrypoint.sh

CMD ["/tmp/entrypoint.sh"]
