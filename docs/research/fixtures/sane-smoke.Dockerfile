FROM debian:12-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends sane-utils \
 && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["scanimage"]
CMD ["-L"]
