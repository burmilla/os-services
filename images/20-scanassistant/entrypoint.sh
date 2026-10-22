#!/bin/sh
set -e

# The Scan Assistant runs commands for the Scan Engine (package lists,
# configuration files, ...), so it runs in the mount namespace of the console
# container, which holds the package database of the system. Its config
# directory is bind mounted over /etc/rapid7/ScanAssistant in a private mount
# namespace, so nothing is left behind in the console when the service stops.

if [ "$(uname -m)" != x86_64 ]; then
    echo "Rapid7 publishes the Linux Scan Assistant only for amd64" >&2
    exit 1
fi

STATE_DIR=/var/lib/rancher/scan-assistant
CACHE_DIR=${STATE_DIR}-cache
URL=${SCAN_ASSISTANT_URL:-https://download2.rapid7.com/download/InsightVM/R7ScanAssistant_amd64.deb}
CERT_FILE=${SCAN_ASSISTANT_CERT_FILE:-/var/lib/rancher/conf/scan-assistant.pem}
CONSOLE=${SCAN_ASSISTANT_CONSOLE:-console}

if [ -n "${SCAN_ASSISTANT_CERT}" ]; then
    CERT="${SCAN_ASSISTANT_CERT}"
elif [ -f "${CERT_FILE}" ]; then
    CERT="$(cat "${CERT_FILE}")"
else
    echo "Scan Assistant certificate missing: set SCAN_ASSISTANT_CERT or write it to ${CERT_FILE}" >&2
    exit 1
fi
case "${CERT}" in
*"PRIVATE KEY"*)
    echo "The certificate contains a private key, use only the public certificate (PEM) from the Security Console" >&2
    exit 1
    ;;
*"BEGIN CERTIFICATE"*) ;;
*)
    echo "The Scan Assistant certificate is not a PEM certificate" >&2
    exit 1
    ;;
esac

console_pid() {
    system-docker inspect -f '{{if .State.Running}}{{.State.Pid}}{{end}}' "${CONSOLE}" 2>/dev/null || true
}

PID=$(console_pid)
while [ -z "${PID}" ] || [ "${PID}" = "0" ]; do
    echo "Waiting for the ${CONSOLE} container"
    sleep 5
    PID=$(console_pid)
done

PACKAGE_MANAGER=${SCAN_ASSISTANT_PACKAGE_MANAGER}
if [ -z "${PACKAGE_MANAGER}" ]; then
    if [ -e /proc/${PID}/root/usr/bin/dpkg ]; then
        PACKAGE_MANAGER=deb
    elif [ -e /proc/${PID}/root/usr/bin/rpm ]; then
        PACKAGE_MANAGER=rpm
    else
        echo "No dpkg or rpm in the ${CONSOLE} container, set SCAN_ASSISTANT_PACKAGE_MANAGER" >&2
        exit 1
    fi
fi

case "${SCAN_ASSISTANT_DEBUG:-false}" in
true|false) ;;
*) echo "SCAN_ASSISTANT_DEBUG must be true or false" >&2; exit 1 ;;
esac
case "${SCAN_ASSISTANT_RESPONSE_TIMEOUT:-300}" in
''|*[!0-9]*) echo "SCAN_ASSISTANT_RESPONSE_TIMEOUT must be a number of seconds" >&2; exit 1 ;;
esac

# Rapid7 does not allow redistributing the Scan Assistant, so the latest
# package is downloaded from Rapid7 on every start. The last verified package
# is kept so the service also starts when the download fails.
deb_valid() {
    [ -f "$1" ] && [ -f "$2" ] && \
        [ "$(sha512sum "$1" | cut -d' ' -f1)" = "$(cut -d' ' -f1 "$2" | tr 'A-F' 'a-f')" ]
}

mkdir -p ${CACHE_DIR}
DEB=${CACHE_DIR}/R7ScanAssistant.deb
SUM=${CACHE_DIR}/R7ScanAssistant.deb.sha512sum
echo "Downloading ${URL}.sha512sum"
if curl -fsSL --retry 3 -o ${SUM}.new "${URL}.sha512sum"; then
    if deb_valid ${DEB} ${SUM}.new; then
        echo "Cached Scan Assistant package is up to date"
        mv ${SUM}.new ${SUM}
    else
        echo "Downloading ${URL}"
        curl -fsSL --retry 3 -o ${DEB}.new "${URL}"
        if ! deb_valid ${DEB}.new ${SUM}.new; then
            echo "Checksum of the downloaded Scan Assistant package does not match ${URL}.sha512sum" >&2
            rm -f ${DEB}.new ${SUM}.new
            exit 1
        fi
        mv ${DEB}.new ${DEB}
        mv ${SUM}.new ${SUM}
    fi
elif deb_valid ${DEB} ${SUM}; then
    echo "Download failed, using the cached Scan Assistant package" >&2
else
    echo "Cannot download the Scan Assistant from ${URL}" >&2
    exit 1
fi
echo "Scan Assistant package $(dpkg-deb -f ${DEB} Version)"

# ClientCertificate takes the PEM on one line, like "cat scan-assistant.pem | xargs"
ONE_LINE_CERT=$(echo "${CERT}" | tr -d '\r' | xargs)

rm -rf ${STATE_DIR}
mkdir -p ${STATE_DIR}/etc
# The package scripts expect systemd or upstart, so the package is only
# extracted and /usr/sbin/ScanAssistant is started from there
dpkg-deb -x ${DEB} ${STATE_DIR}/pkg
echo "${CERT}" > ${STATE_DIR}/etc/scan-assistant.pem
cat > ${STATE_DIR}/etc/config.json <<JSON
{
  "ClientCertificate": "${ONE_LINE_CERT}",
  "ResponseTimeout": ${SCAN_ASSISTANT_RESPONSE_TIMEOUT:-300},
  "Debug": ${SCAN_ASSISTANT_DEBUG:-false},
  "PackageManager": "${PACKAGE_MANAGER}"
}
JSON
chmod 700 ${STATE_DIR}/etc
chmod 600 ${STATE_DIR}/etc/*

echo "Starting Scan Assistant in the ${CONSOLE} container (pid ${PID}), package manager ${PACKAGE_MANAGER}"
nsenter -t "${PID}" -m -- unshare -m --propagation private /bin/sh -c "
    mkdir -p /etc/rapid7/ScanAssistant &&
    mount --bind ${STATE_DIR}/etc /etc/rapid7/ScanAssistant &&
    exec ${STATE_DIR}/pkg/usr/sbin/ScanAssistant" &
CHILD=$!

trap 'kill ${CHILD} 2>/dev/null; wait ${CHILD}; exit 0' TERM INT

# Restart (restart: always) when the console container is restarted, so the
# Scan Assistant does not keep running in the mount namespace of the old one.
while kill -0 ${CHILD} 2>/dev/null; do
    sleep 30 &
    wait $! || true
    if [ "$(console_pid)" != "${PID}" ]; then
        echo "The ${CONSOLE} container was restarted, restarting Scan Assistant"
        kill ${CHILD} 2>/dev/null || true
        wait ${CHILD} || true
        exit 1
    fi
done
wait ${CHILD}
