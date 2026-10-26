# Fleet client

Optional system service which connects a BurmillaOS server to
[BurFleet](https://github.com/burmilla/os-fleet-manager).

On every check interval (from BurFleet, 5 minutes by default) it:

1. Reports the server state to BurFleet: hostname, Swarm, Environment,
   BurmillaOS version (`ros os version`), User Docker engine (`ros engine list`),
   kernel, boot time and the Docker Swarm status of the node (from the User
   Docker API). Managers also send node and manager counts and the state of
   every service: desired vs. running tasks and whether an update is in
   progress. Every server also reports its local User Docker containers
   which Docker restarts on its own (restart policy `always`,
   `unless-stopped` or `on-failure`, Swarm tasks excluded): ready means
   running and, with a health check, healthy. Label a container
   `io.burmilla.fleet.ignore=true` to leave it out.
2. Gets back the target versions, upgrade policy, maintenance window and
   whether a scheduled restart is due.
3. Depending on `upgrade_policy`:
   * `none` – only reports.
   * `download` – pre-pulls the target BurmillaOS image.
   * `auto` – inside the maintenance window, asks BurFleet for a
     **maintenance lock**. The lock is only granted when the whole Swarm is
     healthy (all nodes ready, all services running their desired tasks
     with their target configuration and all local containers with a
     restart policy ready) and the Swarm + Environment has a free
     slot. With the lock it runs
     `ros engine switch` (User Docker), then `ros os upgrade` (reboots) or a
     plain reboot for a scheduled restart. The lock is released by BurFleet
     once the server reports the target state and the Swarm is healthy again.
4. Applies the `ssh` settings right away, in any upgrade policy and without a
   maintenance lock:
   * `authorized_keys` go to a block between `# BEGIN burmilla-fleet-manager`
     and `# END burmilla-fleet-manager` in `~/.ssh/authorized_keys` of the
     `rancher` and `docker` users. Other keys in the file (from cloud-config
     `ssh_authorized_keys` or added by hand) are not touched.
   * `password_authentication` is written to
     `/etc/ssh/sshd_config.d/50-burmilla-fleet.conf` in the console container
     (`system-docker exec console`) and sshd is reloaded; open sessions stay.
     The effective value (`sshd -T`) is reported back. Consoles whose
     `sshd_config` does not include `sshd_config.d` report an error.

## Enable

```bash
sudo ros config set rancher.environment.FLEET_MANAGER_URL http://fleet-manager.example.com:8080
sudo ros config set rancher.environment.FLEET_SWARM swarm-a
sudo ros config set rancher.environment.FLEET_ENVIRONMENT production
# only when the Swarm + Environment is protected with htpasswd:
sudo ros config set rancher.environment.FLEET_USERNAME swarm-a-production
sudo ros config set rancher.environment.FLEET_PASSWORD 'password'
sudo ros service enable fleet-client
sudo ros service up fleet-client
```

or with cloud-init:

```yaml
rancher:
  environment:
    FLEET_MANAGER_URL: http://fleet-manager.example.com:8080
    FLEET_SWARM: swarm-a
    FLEET_ENVIRONMENT: production
    FLEET_USERNAME: swarm-a-production   # optional, see above
    FLEET_PASSWORD: password
  services_include:
    fleet-client: true
```

## Older BurmillaOS and RancherOS

The client also runs on older BurmillaOS (v1.9, v2.0) and RancherOS 1.x
installations, so you can get visibility of them before planning upgrades.
Their service repositories do not have fleet-client, so enable it by URL:

```bash
# Old RancherOS may not define REGISTRY_DOMAIN, which the service file uses:
[ -n "$(sudo ros config get rancher.environment.REGISTRY_DOMAIN)" ] || \
  sudo ros config set rancher.environment.REGISTRY_DOMAIN docker.io
sudo ros config set rancher.environment.FLEET_MANAGER_URL https://fleet-manager.example.com
sudo ros config set rancher.environment.FLEET_SWARM swarm-a
sudo ros config set rancher.environment.FLEET_ENVIRONMENT production
sudo ros service enable https://raw.githubusercontent.com/burmilla/os-services/v3.0.x/f/fleet-client.yml
sudo ros service up fleet-client
```

What works there:

* Reports (versions, kernel, boot time, Swarm status, services, local
  containers, SSH state) work. With Docker before 20.10, service task counts
  come from the task list instead of the newer service status API.
* `authorized_keys` works. `password_authentication` needs a console whose
  `sshd_config` includes `sshd_config.d`, which older consoles usually do not.
* Keep `upgrade_policy: none` (the default) for them until you have tested
  the upgrade path. `auto` would run `ros os upgrade` to the target image
  directly.

`FLEET_SWARM` and `FLEET_ENVIRONMENT` decide which Swarm + Environment the
server belongs to: fleet-client calls
`/api/v1/swarms/<FLEET_SWARM>/<FLEET_ENVIRONMENT>/...` on BurFleet
(`/api/v1/...` when neither is set). They must be set together. Needs Fleet
Manager v1.0.2 or later.

| Variable            | Default            | Description                                    |
|---------------------|--------------------|------------------------------------------------|
| `FLEET_MANAGER_URL` |                    | BurFleet address (required)               |
| `FLEET_SWARM`       |                    | Swarm (or standalone group) this server belongs to |
| `FLEET_ENVIRONMENT` |                    | Environment this server belongs to             |
| `FLEET_USERNAME`    |                    | User of the Swarm + Environment when it is protected with `htpasswd` in the fleet configuration |
| `FLEET_PASSWORD`    |                    | Password for `FLEET_USERNAME`                  |
| `FLEET_CA_CERT`     |                    | Extra CA certificate (PEM) to trust for an HTTPS BurFleet |
| `FLEET_HOSTNAME`    | system hostname    | Identity of the server in the fleet            |
| `FLEET_INTERVAL`    | from BurFleet | Override the check interval (Go duration)      |
| `FLEET_DOCKER_SOCK` | `/run/docker.sock` | User Docker socket used for the Swarm status   |

Logs: `sudo system-docker logs fleet-client`.

Run fleet-client on **all** Swarm members, managers included: BurFleet
treats a Swarm without manager reports, or with a member that stopped
reporting, as unhealthy and does not hand out maintenance locks for it.
