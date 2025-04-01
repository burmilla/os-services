ARG VERSION
FROM rancher/k3s:v${VERSION}-k3s1 AS k3s
FROM scratch
COPY engine/* /engine/
COPY --from=k3s /bin/k3* /engine/
COPY --from=k3s /bin/cni /engine/
