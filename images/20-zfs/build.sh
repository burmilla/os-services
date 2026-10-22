#!/bin/sh
set -ex


KERNEL_VERSION=$(uname -r)
echo "zfs ${ZFS_VERSION} for ${KERNEL_VERSION}"

DIR=/lib/modules/${KERNEL_VERSION}/build
STAMP=/lib/modules/${KERNEL_VERSION}/.zfs-done

if [ -e $STAMP ]; then
    modprobe zfs

    system-docker run --rm zfs-tools cat /setup_wonka.sh > /setup_wonka.sh
    chmod 755 /setup_wonka.sh
    # setup wonka in the console container
    /setup_wonka.sh console
    # setup wonka in this container so the import works
    /setup_wonka.sh zfs

    # re-init the zpool from disk
    zpool import -a
    echo ZFS for ${KERNEL_VERSION} already installed. Delete $STAMP to reinstall
    exit 0
fi

# kernel headers are installed into /lib/modules/${KERNEL_VERSION}/build
ros service enable kernel-headers-system-docker
ros service up kernel-headers-system-docker
if [ ! -e ${DIR}/Makefile ] || [ ! -e ${DIR}/include/generated/autoconf.h ]; then
    echo "Kernel headers for ${KERNEL_VERSION} not found in ${DIR}"
    exit 1
fi

# OpenZFS source is unpacked into /dist/zfs when the image is built
# (see Dockerfile, ZFS_VERSION). Fall back to downloading it.
if [ ! -e /dist/zfs/configure ]; then
    curl -fsSL https://github.com/openzfs/zfs/releases/download/zfs-${ZFS_VERSION}/zfs-${ZFS_VERSION}.tar.gz > zfs-${ZFS_VERSION}.tar.gz
    mkdir -p /dist/zfs
    tar zxf zfs-${ZFS_VERSION}.tar.gz --strip-components=1 -C /dist/zfs
fi

cd /dist/zfs
./configure \
          --with-linux=${DIR} \
          --with-linux-obj=${DIR} \
          --with-udevdir=/usr/lib/udev \
          --with-mounthelperdir=/usr/sbin \
          --disable-systemd \
          --disable-sysvinit \
          --disable-pam \
          --disable-pyzfs
make -s -j$(nproc)

# last layer - we could use stratos :)
rm -rf /dist/arch
make DESTDIR=/dist/arch install
cd /dist

mkdir -p /lib/modules/${KERNEL_VERSION}/extra
cp -r /dist/arch/lib/modules/${KERNEL_VERSION}/extra/. /lib/modules/${KERNEL_VERSION}/extra/
depmod ${KERNEL_VERSION}

cd /dist/zfs
LIB_DIR=/dist/arch/usr/local/lib scripts/zfs-helpers.sh -i
cd /dist

# zfs-tools image context: everything that was installed under /usr and /etc.
# The kernel modules (already copied to the host above) and any other top-level
# lib/sbin/bin entries are excluded: on the usrmerged Debian 13 base those are
# symlinks into /usr and must not be replaced by directories.
cp /dist/Dockerfile.zfs-tools /dist/arch/Dockerfile
printf 'Dockerfile\n.dockerignore\nlib\nsbin\nbin\n' > /dist/arch/.dockerignore
mkdir -p /dist/arch/usr/local/bin
cp /dist/entry.sh /dist/arch/usr/local/bin/entry.sh

# how do I export the commands to the console?
# in the future, it'd be nice to have some share /usr/local/bin, but that might interfere with other things.
# we do seem to have /opt mapped
echo "#!/bin/sh" > /dist/arch/setup_wonka.sh
echo "echo installing wonka bin links in \${1}" >> /dist/arch/setup_wonka.sh
chmod 755 /dist/arch/setup_wonka.sh
for i in $(ls arch/usr/local/bin); do
    [ "$i" = "entry.sh" ] && continue
    echo "system-docker cp wonka.sh \${1}:/usr/bin/$i" >> /dist/arch/setup_wonka.sh
done
for i in $(ls arch/usr/local/sbin); do
    echo "system-docker cp wonka.sh \${1}:/usr/sbin/$i" >> /dist/arch/setup_wonka.sh
done
# mount.zfs / fsck.zfs (mount helpers; /sbin is /usr/sbin on Debian 13)
for i in $(ls arch/usr/sbin); do
    echo "system-docker cp wonka.sh \${1}:/usr/sbin/$i" >> /dist/arch/setup_wonka.sh
done

system-docker cp arch/usr/lib/udev/zvol_id udev:/lib/udev/zvol_id
system-docker cp arch/usr/lib/udev/vdev_id udev:/lib/udev/vdev_id
system-docker cp arch/usr/lib/udev/rules.d/60-zvol.rules udev:/etc/udev/rules.d/60-zvol.rules
system-docker cp arch/usr/lib/udev/rules.d/69-vdev.rules udev:/etc/udev/rules.d/69-vdev.rules
system-docker cp arch/usr/lib/udev/rules.d/90-zfs.rules udev:/etc/udev/rules.d/90-zfs.rules

if [ "XX$HTTP_PROXY" != "XX" ]; then
    BUILD_ARGS="--build-arg HTTP_PROXY --build-arg http_proxy="$HTTP_PROXY
fi

if [ "XX$HTTPS_PROXY" != "XX" ]; then
    BUILD_ARGS=$BUILD_ARGS" --build-arg HTTPS_PROXY --build-arg https_proxy="$HTTPS_PROXY
fi

if [ "XX$NO_PROXY" != "XX" ]; then
    BUILD_ARGS=$BUILD_ARGS" --build-arg NO_PROXY --build-arg no_proxy="$NO_PROXY
fi

# Base zfs-tools on the image this container runs (already pulled), so the
# tools run against the glibc they were built with and nothing has to be pulled.
BASE_IMAGE=$(system-docker inspect -f '{{.Config.Image}}' zfs 2>/dev/null || true)
if [ -n "$BASE_IMAGE" ]; then
    BUILD_ARGS=$BUILD_ARGS" --build-arg BASE_IMAGE=$BASE_IMAGE"
fi

system-docker build --network=host $BUILD_ARGS -t zfs-tools arch/

modprobe zfs

/dist/arch/setup_wonka.sh console
/dist/arch/setup_wonka.sh zfs
zpool import -a

touch $STAMP
echo ZFS for ${KERNEL_VERSION} installed. Delete $STAMP to reinstall
