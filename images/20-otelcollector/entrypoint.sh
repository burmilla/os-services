#!/bin/sh
set -e

DEFAULT_CONFIG=/etc/otelcol-contrib/config.yaml
USER_CONFIG=${OTELCOL_CONFIG:-/var/lib/rancher/conf/otel-collector.yaml}

set --
if [ "${OTELCOL_DEFAULT_CONFIG:-true}" != "false" ]; then
    if [ -z "${OTELCOL_ENDPOINT}" ]; then
        echo "OTELCOL_ENDPOINT is not set, it is required by the default config" >&2
        exit 1
    fi
    export OTELCOL_TLS_INSECURE=${OTELCOL_TLS_INSECURE:-true}
    export OTELCOL_LOG_LEVEL=${OTELCOL_LOG_LEVEL:-warn}
    set -- --config "${DEFAULT_CONFIG}"
fi

# The user config is merged on top of the default one: maps are merged,
# lists (e.g. the receivers of a pipeline) are replaced.
if [ -f "${USER_CONFIG}" ]; then
    set -- "$@" --config "${USER_CONFIG}"
elif [ $# -eq 0 ]; then
    echo "OTELCOL_DEFAULT_CONFIG=false but ${USER_CONFIG} does not exist" >&2
    exit 1
fi

echo "Starting otelcol-contrib $*"
exec /usr/bin/otelcol-contrib "$@"
