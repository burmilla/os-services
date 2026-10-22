# The SentinelOne agent may not be redistributed, so the image contains only
# the tooling and the entrypoint installs the customer's own package on start.
# Debian 13 is a distribution SentinelOne supports for the Linux agent.
FROM debian:trixie-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl procps \
    && rm -rf /var/lib/apt/lists/*
COPY systemctl /usr/bin/systemctl
COPY entrypoint.sh /usr/bin/entrypoint.sh
ENTRYPOINT ["/usr/bin/entrypoint.sh"]
