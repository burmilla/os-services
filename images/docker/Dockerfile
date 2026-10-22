ARG DOCKER_VERSION
FROM docker:${DOCKER_VERSION}-dind AS dind
FROM scratch
COPY engine/* /engine/
# Since 29.9.0 the dind image no longer ships containerd because dockerd
# uses its embedded containerd, so copy it only when it is there.
COPY --from=dind /usr/local/bin/containerd* /engine/
COPY --from=dind /usr/local/bin/ctr /engine/
COPY --from=dind /usr/local/bin/docker /engine/
COPY --from=dind /usr/local/bin/docker-init /engine/
COPY --from=dind /usr/local/bin/docker-proxy /engine/
COPY --from=dind /usr/local/bin/dockerd /engine/
COPY --from=dind /usr/local/bin/runc /engine/
COPY --from=dind /usr/local/libexec/docker/cli-plugins/* /engine-plugins/
