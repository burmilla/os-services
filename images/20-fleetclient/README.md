# Fleet client

Optional system service which connects a BurmillaOS server to
[Fleet Manager](https://github.com/burmilla/os-fleet-manager).

On every check interval (from Fleet Manager, 5 minutes by default) it:

1. Reports the server state to Fleet Manager: hostname, Swarm, Environment,
   BurmillaOS version (`ros os version`), User Docker engine (`ros engine list`),
   kernel, boot time and the Docker Swarm status of the node (from the User
   Docker API). Managers also send node and manager counts and the state of
   every service: desired vs. running tasks and whether an update is in
   progress.
2. Gets back the target versions, upgrade policy, maintenance window and
   whether a scheduled restart is due.
3. Depending on `upgrade_policy`:
   * `none` – only reports.
   * `download` – pre-pulls the target BurmillaOS image.
   * `auto` – inside the maintenance window, asks Fleet Manager for a
     **maintenance lock**. The lock is only granted when the whole Swarm is
     healthy (all nodes ready and all services running their desired tasks
     with their target configuration) and the Swarm + Environment has a free
     slot. With the lock it runs
     `ros engine switch` (User Docker), then `ros os upgrade` (reboots) or a
     plain reboot for a scheduled restart. The lock is released by Fleet Manager
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

Swarm and Environment can also be assigned centrally in the `servers:`
section of the fleet configuration, which wins over the values above.

| Variable            | Default            | Description                                    |
|---------------------|--------------------|------------------------------------------------|
| `FLEET_MANAGER_URL` |                    | Fleet Manager address (required)               |
| `FLEET_SWARM`       |                    | Swarm this server belongs to                   |
| `FLEET_ENVIRONMENT` |                    | Environment this server belongs to             |
| `FLEET_USERNAME`    |                    | User of the Swarm + Environment when it is protected with `htpasswd` in the fleet configuration |
| `FLEET_PASSWORD`    |                    | Password for `FLEET_USERNAME`                  |
| `FLEET_CA_CERT`     |                    | Extra CA certificate (PEM) to trust for an HTTPS Fleet Manager |
| `FLEET_HOSTNAME`    | system hostname    | Identity of the server in the fleet            |
| `FLEET_INTERVAL`    | from Fleet Manager | Override the check interval (Go duration)      |
| `FLEET_DOCKER_SOCK` | `/run/docker.sock` | User Docker socket used for the Swarm status   |

Logs: `sudo system-docker logs fleet-client`.

Run fleet-client on **all** Swarm members, managers included: Fleet Manager
treats a Swarm without manager reports, or with a member that stopped
reporting, as unhealthy and does not hand out maintenance locks for it.
