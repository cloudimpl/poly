FROM 537413656254.dkr.ecr.us-east-1.amazonaws.com/polycode/next-builder:latest AS base

ARG APP_FOLDER
WORKDIR /project/${APP_FOLDER}

# Install CompileDaemon
RUN go install github.com/githubnemo/CompileDaemon@latest

# Install user defined additional libraries
RUN if [ -f "./install.sh" ]; then \
      cp ./install.sh /tmp/install.sh; \
    else \
      echo -e '#!/bin/sh\necho "No install.sh provided."' > /tmp/install.sh; \
    fi && \
    chmod +x /tmp/install.sh && \
    /tmp/install.sh

COPY ./.runtime/sidecar /tmp/sidecar
COPY entrypoint.sh /tmp/entrypoint.sh
RUN chmod +x /tmp/sidecar && chmod +x /tmp/entrypoint.sh

CMD ["/tmp/entrypoint.sh"]
