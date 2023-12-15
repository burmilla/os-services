#!/bin/bash

VERSION=$(jq -r 'map(select(.prerelease == false)) | first | .tag_name' <<< $(wget -q -O - https://api.github.com/repos/docker/compose/releases))
DOCKERARCH=$(uname -m)
COMPOSE_URL="https://github.com/docker/compose/releases/download/${VERSION}/docker-compose-linux-${DOCKERARCH}"
mkdir -p /home/rancher/.docker/cli-plugins
wget -q -O /home/rancher/.docker/cli-plugins/docker-compose $COMPOSE_URL
chmod 0755 /home/rancher/.docker/cli-plugins/docker-compose
ln -s /home/rancher/.docker/cli-plugins/docker-compose /var/lib/rancher/compose/docker-compose
