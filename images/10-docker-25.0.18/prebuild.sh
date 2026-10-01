#!/bin/bash
set -ex

VERSION=$1
URL="https://github.com/burmilla/docker-ce-packaging/releases/download/v${VERSION}/docker-${VERSION}.tgz"
COMPLETION_URL="https://raw.githubusercontent.com/docker/cli/v25.0.7/contrib/completion/bash/docker"
DEST="./images/10-docker-${VERSION}${SUFFIX}"

mkdir -p $DEST/docker
curl -sL ${URL} | tar xzf - -C $DEST
curl -sL -o $DEST/docker/completion ${COMPLETION_URL}
mv $DEST/docker $DEST/engine
