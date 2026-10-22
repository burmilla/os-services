FROM debian:trixie-slim AS download
# Rapid7 publishes only the latest Linux Scan Assistant, without a version
# in the URL. The image tag of the service follows the packaged version.
ARG SCAN_ASSISTANT_URL=https://download2.rapid7.com/download/InsightVM/R7ScanAssistant_amd64.deb
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && cd /tmp \
    && curl -fsSLO "${SCAN_ASSISTANT_URL}" \
    && curl -fsSLO "${SCAN_ASSISTANT_URL}.sha512sum" \
    && sha512sum -c "$(basename ${SCAN_ASSISTANT_URL}).sha512sum" \
    && dpkg-deb -f "$(basename ${SCAN_ASSISTANT_URL})" Package Version \
    && dpkg-deb -x "$(basename ${SCAN_ASSISTANT_URL})" /pkg \
    && test -x /pkg/usr/sbin/ScanAssistant

# system-docker comes from the host through command-volumes
FROM alpine:3.22
RUN apk add --no-cache util-linux-misc
COPY --from=download /pkg/usr/sbin/ScanAssistant /usr/share/scan-assistant/ScanAssistant
COPY entrypoint.sh /usr/bin/entrypoint.sh
ENTRYPOINT ["/usr/bin/entrypoint.sh"]
