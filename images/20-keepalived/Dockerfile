FROM alpine:3.22
RUN apk add --no-cache keepalived iproute2
COPY entrypoint.sh /usr/bin/entrypoint.sh
ENTRYPOINT ["/usr/bin/entrypoint.sh"]
