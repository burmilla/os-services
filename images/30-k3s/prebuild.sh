#!/bin/bash
set -ex

VERSION=$1
DEST="./images/30-k3s"
mkdir -p $DEST/engine

touch $DEST/engine/cni
ln -s cni $DEST/engine/bandwidth
ln -s cni $DEST/engine/bridge
ln -s cni $DEST/engine/firewall
ln -s cni $DEST/engine/flannel
ln -s cni $DEST/engine/host-local
ln -s cni $DEST/engine/loopback
ln -s cni $DEST/engine/portmap

cat > $DEST/engine/dockerd <<EOL
#!/bin/sh
/etc/k3s/node-script
EOL
chmod a+x $DEST/engine/dockerd
