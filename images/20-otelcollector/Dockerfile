FROM otel/opentelemetry-collector-contrib:0.163.2 AS otelcol

FROM alpine:3.22
COPY --from=otelcol /otelcol-contrib /usr/bin/otelcol-contrib
COPY config.yaml /etc/otelcol-contrib/config.yaml
COPY entrypoint.sh /usr/bin/entrypoint.sh
ENTRYPOINT ["/usr/bin/entrypoint.sh"]
