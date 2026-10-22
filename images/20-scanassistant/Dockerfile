# The Scan Assistant may not be redistributed, so the image contains only the
# tooling and the entrypoint downloads the package from Rapid7 on start.
# system-docker comes from the host through command-volumes
FROM alpine:3.22
RUN apk add --no-cache ca-certificates curl dpkg util-linux-misc
COPY entrypoint.sh /usr/bin/entrypoint.sh
ENTRYPOINT ["/usr/bin/entrypoint.sh"]
