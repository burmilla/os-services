#!/bin/bash

VERSION=$(jq -r 'map(select(.prerelease == false)) | first | .tag_name' <<< $(wget -q -O - https://api.github.com/repos/docker/compose/releases))
DOCKERARCH=$(uname -m)
COMPOSE_URL="https://github.com/docker/compose/releases/download/${VERSION}/docker-compose-linux-${DOCKERARCH}"
wget -q -O /var/lib/rancher/compose/docker-compose $COMPOSE_URL
chmod 0755 /var/lib/rancher/compose/docker-compose
