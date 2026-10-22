# Build environment for OpenZFS kernel modules and userland tools.
# Debian 13 (trixie) ships GCC 14.2, the same compiler the BurmillaOS 6.12
# kernel is built with (CONFIG_CC_VERSION_TEXT="gcc (Debian 14.2.0-8) 14.2.0"),
# and a glibc new enough to run the host tools (fixdep, modpost, objtool)
# that are shipped prebuilt inside the os-headers tarball.
FROM debian:trixie-slim
# FROM arm64=skip arm=skip

ENV ZFS_VERSION=2.4.4

RUN apt-get update \
    && apt-get install -yq --no-install-recommends \
        build-essential autoconf automake libtool pkg-config gawk bc \
        zlib1g-dev uuid-dev libattr1-dev libblkid-dev libudev-dev libssl-dev \
        libelf-dev libtirpc-dev libaio-dev libcurl4-openssl-dev \
        kmod python3 \
        parted lsscsi ksh curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /dist

# Fetch the source at image build time so `ros service up zfs` does not
# depend on GitHub being reachable from the host.
RUN curl -fsSL https://github.com/openzfs/zfs/releases/download/zfs-${ZFS_VERSION}/zfs-${ZFS_VERSION}.tar.gz -o /tmp/zfs.tar.gz \
    && mkdir -p /dist/zfs \
    && tar xzf /tmp/zfs.tar.gz --strip-components=1 -C /dist/zfs \
    && rm /tmp/zfs.tar.gz

COPY entry.sh /dist/
COPY build.sh /dist/
COPY wonka.sh /dist/
COPY Dockerfile.zfs-tools /dist/

CMD ["/dist/build.sh"]

ENTRYPOINT ["/usr/bin/ros", "entrypoint"]
