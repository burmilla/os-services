#!/bin/bash
set -ex

VERSION=27.4.0
ARCH=$2
if [ "$ARCH" == "amd64" ]; then
    DOCKERARCH="x86_64"
    URL="https://download.docker.com/linux/static/test/${DOCKERARCH}/docker-27.4.0-rc.2.tgz"
    COMPLETION_URL="https://raw.githubusercontent.com/docker/cli/v27.4.0-rc.2/contrib/completion/bash/docker"
fi

DEST="./images/10-docker-${VERSION}${SUFFIX}"

mkdir -p $DEST
curl -sL ${URL} | tar xzf - -C $DEST
curl -sL -o $DEST/docker/completion ${COMPLETION_URL}
mv $DEST/docker $DEST/engine
