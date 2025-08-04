ARG DOCKER_VERSION
FROM docker:26.1.4-dind AS dind
FROM scratch
COPY engine/* /engine/
COPY --from=dind /usr/local/bin/containerd /engine/
COPY --from=dind /usr/local/bin/containerd-shim-runc-v2 /engine/
COPY --from=dind /usr/local/bin/ctr /engine/
COPY --from=dind /usr/local/bin/docker /engine/
COPY --from=dind /usr/local/bin/docker-init /engine/
COPY --from=dind /usr/local/bin/docker-proxy /engine/
# COPY --from=dind /usr/local/bin/dockerd /engine/
COPY --from=dind /usr/local/bin/runc /engine/
COPY --from=dind /usr/local/libexec/docker/cli-plugins/* /engine-plugins/
