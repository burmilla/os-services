# Rapid7 Scan Assistant

Optional system service which runs the Linux
[Rapid7 Scan Assistant](https://docs.rapid7.com/insightvm/scan-assistant-combined/)
for InsightVM / Nexpose credentialed scans. The Scan Engine connects to it on
TCP port **21047** (TLS) instead of logging in with SSH.

The image contains `/usr/sbin/ScanAssistant` from Rapid7's
`R7ScanAssistant_amd64.deb` (amd64 only, Rapid7 publishes no arm64 Linux
package). The package is extracted at build time; its install scripts, which
expect systemd or upstart, are not run.

## What it scans

On BurmillaOS the packages, users and configuration files that matter live
in the **console** container (Debian 13 by default), so the service runs the
Scan Assistant in the mount namespace of the console. It shares the host's
network, PID, IPC and UTS namespaces, so the scan sees:

* `/etc/os-release` of BurmillaOS, the running kernel (`uname`), all
  processes and listening ports of the host;
* the Debian packages of the console (`dpkg`), which is what you update with
  `apt-get` and what `ros os upgrade` replaces;
* the files of the console, including `/var/lib/rancher`, `/home` and
  `/opt`, which are shared with the host.

System containers other than the console (e.g. System Docker services) and
User Docker containers are not scanned as packages. Use container image
scanning for those.

Its configuration is written to `/var/lib/rancher/scan-assistant/etc` and bind
mounted over `/etc/rapid7/ScanAssistant` in a private mount namespace, so the
console filesystem stays unchanged apart from the empty
`/etc/rapid7/ScanAssistant` directory. When the console container is
restarted, the service restarts so it follows the new console.

The service is privileged and uses the host PID namespace because entering
the console's namespaces requires it.

## Certificate

The Scan Engine authenticates with a client certificate. Create it in the
Security Console: **Administration > Vulnerability Management > Console
Administration > Shared Credentials > New**, select service
**Scan Assistant**, check **Generate** and save. Edit the credential again
and copy the generated certificate (PEM). The private key stays in the
Security Console. Only the public certificate goes to the server.

Write it to `/var/lib/rancher/conf/scan-assistant.pem` with cloud-config
`write_files`, or put it in `SCAN_ASSISTANT_CERT`.

## Enable

```yaml
#cloud-config
write_files:
- path: /var/lib/rancher/conf/scan-assistant.pem
  permissions: "0600"
  owner: root
  content: |
    -----BEGIN CERTIFICATE-----
    MIIB...
    -----END CERTIFICATE-----
rancher:
  services_include:
    rapid7-scan-assistant: true
```

or on a running server:

```bash
sudo vi /var/lib/rancher/conf/scan-assistant.pem   # paste the certificate
sudo ros service enable rapid7-scan-assistant
sudo ros service up rapid7-scan-assistant
```

| Variable                          | Default                                  | Description |
|-----------------------------------|------------------------------------------|-------------|
| `SCAN_ASSISTANT_CERT_FILE`        | `/var/lib/rancher/conf/scan-assistant.pem` | Certificate (PEM) file |
| `SCAN_ASSISTANT_CERT`             |                                          | Certificate (PEM) as a value, wins over the file |
| `SCAN_ASSISTANT_RESPONSE_TIMEOUT` | `300`                                    | `ResponseTimeout` in `config.json` (seconds) |
| `SCAN_ASSISTANT_DEBUG`            | `false`                                  | `Debug` in `config.json` |
| `SCAN_ASSISTANT_PACKAGE_MANAGER`  | detected in the console (`deb` / `rpm`)  | `PackageManager` in `config.json` |
| `SCAN_ASSISTANT_CONSOLE`          | `console`                                | System container whose files are scanned |

Variables are set with `ros config set rancher.environment.<NAME> <value>`.
After changing the certificate or a variable, run
`sudo system-docker restart rapid7-scan-assistant`.

Logs: `sudo system-docker logs rapid7-scan-assistant`.

## Scan template

* Add TCP port 21047 to Service Discovery and Asset Discovery of the scan
  template and allow it from the Scan Engine to the server.
* Scan Engine 6.6.153 or later is required.
* Turn off Scan Assistant software updates in the scan template. The service
  cannot be updated in place. A new Scan Assistant version comes with a new
  image version of this service.

## Building

`images/20-scanassistant` downloads the latest package from
`download2.rapid7.com` and checks its `.sha512sum`. Rapid7 does not version
the download URL, so build the image with the build-service workflow
(`IMAGE_BUILD_NAME=20-scanassistant`) and a new `VERSION` whenever a new Scan
Assistant should be shipped, and update the tag in
`r/rapid7-scan-assistant.yml`. The build log prints the packaged version.
