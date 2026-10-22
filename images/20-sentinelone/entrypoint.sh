#!/bin/sh
set -e

# SentinelOne does not allow redistributing the agent, so the customer's own
# Linux agent package (.deb) is installed on the first start, from a file on
# the host or from a URL. /opt/sentinelone is kept on the host, so the agent
# keeps its identity and console-pushed updates over restarts and upgrades.

UNIT=${SENTINELONE_UNIT:-sentinelone}
PACKAGE_DIR=/var/lib/rancher/conf/sentinelone
CACHE_DIR=/var/lib/rancher/sentinelone-cache
STATE_DIR=/var/lib/rancher/sentinelone
TOKEN_FILE=${SENTINELONE_SITE_TOKEN_FILE:-/var/lib/rancher/conf/sentinelone-token}

installed() {
    [ -x /opt/sentinelone/bin/sentinelctl ]
}

find_package() {
    if [ -n "${SENTINELONE_PACKAGE}" ]; then
        echo "${SENTINELONE_PACKAGE}"
        return
    fi
    if [ -n "${SENTINELONE_PACKAGE_URL}" ]; then
        # The install carries the site token, so a downloaded package must be verified
        if [ -z "${SENTINELONE_PACKAGE_SHA256}" ]; then
            echo "SENTINELONE_PACKAGE_SHA256 is required with SENTINELONE_PACKAGE_URL" >&2
            exit 1
        fi
        mkdir -p ${CACHE_DIR}
        echo "Downloading ${SENTINELONE_PACKAGE_URL}" >&2
        curl -fsSL --retry 3 -o ${CACHE_DIR}/SentinelAgent.deb.new "${SENTINELONE_PACKAGE_URL}"
        mv ${CACHE_DIR}/SentinelAgent.deb.new ${CACHE_DIR}/SentinelAgent.deb
        echo ${CACHE_DIR}/SentinelAgent.deb
        return
    fi
    ls ${PACKAGE_DIR}/*.deb 2>/dev/null | sort -V | tail -1
}

install_agent() {
    DEB=${1:-$(find_package)}
    if [ -z "${DEB}" ] || [ ! -f "${DEB}" ]; then
        echo "SentinelOne package missing: copy the Linux agent .deb to ${PACKAGE_DIR}/ or set SENTINELONE_PACKAGE_URL" >&2
        exit 1
    fi
    if [ -n "${SENTINELONE_PACKAGE_SHA256}" ] && \
        [ "$(sha256sum "${DEB}" | cut -d' ' -f1)" != "${SENTINELONE_PACKAGE_SHA256}" ]; then
        echo "Checksum of ${DEB} does not match SENTINELONE_PACKAGE_SHA256" >&2
        exit 1
    fi
    ARCH=$(dpkg --print-architecture)
    PKG_ARCH=$(dpkg-deb -f "${DEB}" Architecture)
    if [ "${PKG_ARCH}" != "${ARCH}" ] && [ "${PKG_ARCH}" != "all" ]; then
        echo "${DEB} is for ${PKG_ARCH}, this system is ${ARCH}" >&2
        exit 1
    fi

    if [ -n "${SENTINELONE_SITE_TOKEN}" ]; then
        TOKEN="${SENTINELONE_SITE_TOKEN}"
    elif [ -f "${TOKEN_FILE}" ]; then
        TOKEN="$(tr -d ' \r\n' < "${TOKEN_FILE}")"
    else
        echo "SentinelOne site token missing: set SENTINELONE_SITE_TOKEN or write it to ${TOKEN_FILE}" >&2
        exit 1
    fi

    # Unattended install, see "Installing the Linux Agent with a configuration file"
    CONFIG=$(mktemp)
    chmod 600 "${CONFIG}"
    {
        echo "S1_AGENT_MANAGEMENT_TOKEN=${TOKEN}"
        echo "S1_AGENT_DEVICE_TYPE=${SENTINELONE_DEVICE_TYPE:-server}"
        echo "S1_AGENT_AUTO_START=false"
        if [ -n "${SENTINELONE_MANAGEMENT_PROXY}" ]; then
            echo "S1_AGENT_MANAGEMENT_PROXY=${SENTINELONE_MANAGEMENT_PROXY}"
        fi
        if [ -n "${SENTINELONE_DV_PROXY}" ]; then
            echo "S1_AGENT_DV_PROXY=${SENTINELONE_DV_PROXY}"
        fi
    } > "${CONFIG}"

    echo "Installing SentinelOne agent $(dpkg-deb -f "${DEB}" Version)"
    if ! S1_AGENT_INSTALL_CONFIG_PATH="${CONFIG}" dpkg -i "${DEB}"; then
        rm -f "${CONFIG}"
        exit 1
    fi
    rm -f "${CONFIG}"

    # Keep the installed package, so a recreated container can restore what
    # the package installs outside /opt/sentinelone (users, units, dpkg status)
    mkdir -p ${CACHE_DIR} ${STATE_DIR}
    if [ "${DEB}" != "${CACHE_DIR}/SentinelAgent.deb" ]; then
        cp "${DEB}" ${CACHE_DIR}/SentinelAgent.deb
    fi
    dpkg-deb -f "${DEB}" Package > ${STATE_DIR}/package
    dpkg-deb -f "${DEB}" Version > ${STATE_DIR}/version
}

# A recreated container (service update, BurmillaOS upgrade) has
# /opt/sentinelone but not the rest of the package. Install the same package
# again; a different version would replace an agent updated from the console.
restore_agent() {
    PACKAGE=$(cat ${STATE_DIR}/package 2>/dev/null || true)
    if [ -z "${PACKAGE}" ] || dpkg -s "${PACKAGE}" >/dev/null 2>&1; then
        return
    fi
    DEB=${CACHE_DIR}/SentinelAgent.deb
    if [ -f "${DEB}" ] && [ "$(dpkg-deb -f "${DEB}" Version)" = "$(cat ${STATE_DIR}/version)" ]; then
        echo "Restoring the SentinelOne package in the new container"
        install_agent "${DEB}"
    else
        echo "Cannot restore the SentinelOne package $(cat ${STATE_DIR}/version) in the new container, starting the agent from /opt/sentinelone" >&2
    fi
}

agent_running() {
    pgrep -f /opt/sentinelone/bin/ >/dev/null
}

start_agent() {
    if systemctl start "${UNIT}" 2>/dev/null; then
        return
    fi
    if [ -x "/etc/init.d/${UNIT}" ]; then
        "/etc/init.d/${UNIT}" start
        return
    fi
    /opt/sentinelone/bin/sentinelctl control start
}

# "sentinelctl control stop" needs the agent passphrase (anti-tamper), so the
# processes are only signalled; System Docker kills the rest with the container
stop_agent() {
    systemctl stop "${UNIT}" || true
}

if ! installed; then
    install_agent
else
    restore_agent
fi

# Keep the unit of the installed package for the next container
mkdir -p ${STATE_DIR}
for d in /etc/systemd/system /usr/lib/systemd/system /lib/systemd/system; do
    if [ -f "${d}/${UNIT}.service" ]; then
        cp "${d}/${UNIT}.service" ${STATE_DIR}/
        break
    fi
done

if ! agent_running; then
    echo "Starting SentinelOne agent"
    start_agent
fi

trap 'stop_agent; exit 0' TERM INT

/opt/sentinelone/bin/sentinelctl version 2>/dev/null || true

# Exit (restart: always) when the agent has stopped for three checks in a row,
# which leaves time for the agent to restart itself during an update.
FAILED=0
while true; do
    sleep 30 &
    wait $! || true
    if agent_running; then
        FAILED=0
        # Follow updates installed from the console, so restore_agent does not
        # install an older package over them
        if [ -f ${STATE_DIR}/package ]; then
            dpkg-query -W -f '${Version}\n' "$(cat ${STATE_DIR}/package)" > ${STATE_DIR}/version.new 2>/dev/null \
                && mv ${STATE_DIR}/version.new ${STATE_DIR}/version || rm -f ${STATE_DIR}/version.new
        fi
    else
        FAILED=$((FAILED + 1))
        echo "SentinelOne agent is not running (${FAILED}/3)" >&2
        if [ ${FAILED} -ge 3 ]; then
            exit 1
        fi
    fi
done
