# Stage 1: Build the application
FROM 537413656254.dkr.ecr.us-east-1.amazonaws.com/polycode/next-builder:latest AS base

# Define build argument for the dynamic folder
ARG APP_FOLDER

COPY . /
WORKDIR /${APP_FOLDER}

RUN next-gen

RUN go mod download
RUN GOOS=linux GOARCH=amd64 go build -o /main .

RUN chmod +x /main

RUN if [ -n "${BUILD_ID}" ] && [ -n "${META_CALLBACK_URL}" ]; then \
      /main info log.txt && cat log.txt && \
      chmod u+x /extract-meta.sh && \
      /extract-meta.sh "${BUILD_ID}" "${META_CALLBACK_URL}"; \
    else \
      echo "Skipping metadata extraction: BUILD_ID or META_CALLBACK_URL not set"; \
    fi

RUN if [ -f "./install.sh" ]; then \
      cp ./install.sh /tmp/install.sh; \
    else \
      echo -e '#!/bin/sh\necho "No install.sh provided."' > /tmp/install.sh; \
    fi

# Stage 2: Create the minimal final image using Alpine
FROM 537413656254.dkr.ecr.us-east-1.amazonaws.com/polycode/lambda-sidecar:latest

COPY --from=base /main /var/task/main
COPY --from=base /tmp/install.sh /install.sh

RUN chmod +x /install.sh && /install.sh

