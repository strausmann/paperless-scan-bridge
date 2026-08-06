FROM debian:12-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      sane-utils \
      scanbd \
 && rm -rf /var/lib/apt/lists/*
