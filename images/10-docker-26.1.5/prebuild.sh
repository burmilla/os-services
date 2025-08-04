#!/bin/bash
set -ex

VERSION=$1
COMPLETION_URL="https://raw.githubusercontent.com/docker/cli/v${VERSION}/contrib/completion/bash/docker"
DEST="./images/10-docker-${VERSION}${SUFFIX}"

mkdir -p $DEST/docker
curl -sL -o $DEST/docker/completion ${COMPLETION_URL}
mv $DEST/docker $DEST/engine

curl -sL -o $DEST/engine/dockerd https://github.com/olljanat/moby/releases/download/26.1.5-olljanat1/dockerd
chmod 0755 $DEST/engine/dockerd
