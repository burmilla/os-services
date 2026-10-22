FROM docker:29.0.1-cli AS docker-cli
FROM scratch
COPY engine/* /engine/
COPY --from=docker-cli /usr/local/libexec/docker/cli-plugins/* /engine-plugins/
