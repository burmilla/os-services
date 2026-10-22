FROM debian:trixie-slim
# FROM arm=skip arm64=skip

# Debian 13 packages open-vm-tools (12.5.x) including vmhgfs-fuse, so there is
# no need to build it from source any more
RUN apt-get update \
    && apt-get install --no-install-recommends --no-install-suggests -y adduser open-vm-tools iproute2 sudo \
    && apt-get clean \
    && rm -rf /var/lib/apt/* \
    && mkdir -p /mnt/hgfs \
    && vmtoolsd --version

ENTRYPOINT ["/usr/bin/ros", "entrypoint"]

RUN addgroup --gid 1100 rancher && \
    addgroup --gid 1101 docker && \
    adduser -q -u 1100 --gid 1100 --disabled-password --gecos "" --shell /bin/bash rancher && \
    adduser -q -u 1101 --gid 1101 --disabled-password --gecos "" --shell /bin/bash docker && \
    adduser docker sudo && \
    sed -i 's/rancher:!/rancher:*/g' /etc/shadow && \
    sed -i 's/docker:!/docker:*/g' /etc/shadow && \
    echo '## allow password less for rancher user' >> /etc/sudoers && \
    echo 'rancher ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers && \
    echo '## allow password less for docker user' >> /etc/sudoers && \
    echo 'docker ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers && \
    echo "docker:tcuser" | chpasswd
