# SentinelOne agent

Optional system service which runs the
[SentinelOne](https://www.sentinelone.com/) Linux agent (EDR) on
BurmillaOS.

SentinelOne does not allow redistributing the agent and its packages are only
available to customers, so the image does not contain it. You supply the
Linux agent package and site token from your own SentinelOne console, and the
service installs the package on its first start.

**Status: not tested with a real agent package.** BurmillaOS is not on the
SentinelOne list of supported distributions. The service runs the agent in a
Debian 13 container, so the console shows the endpoint as Debian 13.

## How it works

* The image is Debian 13 slim with an entrypoint and a small `systemctl`
  replacement. There is no systemd in the container, so the replacement runs
  the `ExecStart` of the unit installed by the package directly.
* On the first start the package is installed with `dpkg -i` using an
  [installation configuration file](https://support.guardz.com/en/articles/10088053-sentinelone-installation-linux)
  (`S1_AGENT_MANAGEMENT_TOKEN`, `S1_AGENT_DEVICE_TYPE=server`, proxies).
* `/opt/sentinelone` is stored on the host in `/var/lib/sentinelone`, so the
  agent keeps its identity, configuration and updates pushed from the console
  over service restarts and BurmillaOS upgrades.
* The installed package is kept in `/var/lib/rancher/sentinelone-cache`. When
  the container is recreated (service image update, BurmillaOS upgrade), the
  same package is installed again to restore what it puts outside
  `/opt/sentinelone` (users, units, dpkg status). If the agent was updated
  from the console in the meantime, the older package is not installed over
  it and the agent is started from `/opt/sentinelone` as it is.
* The agent uses eBPF, no kernel modules. The container is privileged and
  shares the host PID, network, IPC and UTS namespaces, so the agent sees all
  processes and connections of the host, System Docker and User Docker.
  `/var/lib/docker` and `/var/run/docker.sock` (via `system-volumes`) of User
  Docker, `/home` and
  `/opt` are mounted for file and container visibility.
* The service restarts when the agent processes are gone for 90 seconds.

## Supply the package and token

1. In the SentinelOne console, download the Linux agent `.deb` package for
   your architecture (`SentinelAgent_linux_x86_64_v*.deb` or the aarch64 one)
   and copy the site or group token.
2. Make the package available to the server in one of these ways:
   * copy it to `/var/lib/rancher/conf/sentinelone/` (the newest `.deb` there
     is used),
   * set `SENTINELONE_PACKAGE_URL` to an internal URL that serves it,
     together with its `SENTINELONE_PACKAGE_SHA256`, or
   * set `SENTINELONE_PACKAGE` to the path of the file.
3. Set the token in `SENTINELONE_SITE_TOKEN`, or write it to
   `/var/lib/rancher/conf/sentinelone-token`.

## Enable

```yaml
#cloud-config
rancher:
  environment:
    SENTINELONE_SITE_TOKEN: eyJ1cmwiOi...
    SENTINELONE_PACKAGE_URL: https://repo.example.com/s1/SentinelAgent_linux_x86_64_v26_2_1.deb
    SENTINELONE_PACKAGE_SHA256: 3f2a...
  services_include:
    sentinelone: true
```

or on a running system:

```sh
sudo mkdir -p /var/lib/rancher/conf/sentinelone
sudo cp SentinelAgent_linux_x86_64_v*.deb /var/lib/rancher/conf/sentinelone/
sudo ros config set rancher.environment.SENTINELONE_SITE_TOKEN eyJ1cmwiOi...
sudo ros service enable sentinelone
sudo ros service up sentinelone
```

Check it with:

```sh
sudo system-docker logs sentinelone
sudo system-docker exec sentinelone /opt/sentinelone/bin/sentinelctl management status
```

## Settings

| Variable | Default | Description |
| --- | --- | --- |
| `SENTINELONE_SITE_TOKEN` | | Site or group token from the console |
| `SENTINELONE_SITE_TOKEN_FILE` | `/var/lib/rancher/conf/sentinelone-token` | File with the token |
| `SENTINELONE_PACKAGE` | newest `/var/lib/rancher/conf/sentinelone/*.deb` | Package file |
| `SENTINELONE_PACKAGE_URL` | | URL of the package, downloaded to `/var/lib/rancher/sentinelone-cache` |
| `SENTINELONE_PACKAGE_SHA256` | | Expected SHA-256 of the package, required with `SENTINELONE_PACKAGE_URL` |
| `SENTINELONE_DEVICE_TYPE` | `server` | `server` or `desktop` |
| `SENTINELONE_MANAGEMENT_PROXY` | | Proxy for the management connection |
| `SENTINELONE_DV_PROXY` | | Proxy for Deep Visibility |
| `SENTINELONE_UNIT` | `sentinelone` | Name of the systemd unit in the package |

The token and package are only used for the first installation. To register
the agent again, or to install it from scratch, disable the service and
remove `/var/lib/sentinelone` (the agent anti-tamper protection may require
a passphrase from the console to uninstall).

## Kernel

The agent needs eBPF, which the BurmillaOS kernel has. Kernels with BTF
(`/sys/kernel/btf/vmlinux`) are recommended for eBPF agents.

## Limitations

* The Kubernetes agent (`s1agent` / `s1helper` images) is not supported: its
  helper needs the Kubernetes API.
* Container visibility for Docker needs the SentinelOne Cloud Workload
  Security for Containers license.
* Uninstalling and stopping the agent from inside the container follow the
  agent anti-tamper protection; stopping the service only signals the agent
  processes.
