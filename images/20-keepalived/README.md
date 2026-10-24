# keepalived

Optional system service which runs [keepalived](https://www.keepalived.org/)
on a BurmillaOS server, so that a floating IP (VRRP virtual IP) moves between
the nodes of a cluster, for example in front of Docker Swarm managers or
ingress nodes.

keepalived needs node specific settings (state, priority, own address). On
BurmillaOS every node has its own cloud-config, so the service is configured
with `KEEPALIVED_*` variables in `rancher.environment` and generates
`keepalived.conf` from them when it starts. A complete configuration file
can be used instead for anything the variables do not cover.

## Enable

```bash
sudo ros config set rancher.environment.KEEPALIVED_VIRTUAL_IPS 192.168.1.100/24
sudo ros config set rancher.environment.KEEPALIVED_PRIORITY 150
sudo ros service enable keepalived
sudo ros service up keepalived
```

or with cloud-init (the only difference between the nodes is the priority):

```yaml
#cloud-config
rancher:
  environment:
    KEEPALIVED_VIRTUAL_IPS: 192.168.1.100/24
    KEEPALIVED_PRIORITY: "150"
    KEEPALIVED_PASSWORD: s3cret
  services_include:
    keepalived: true
```

| Variable                     | Default                                 | Description |
|------------------------------|-----------------------------------------|-------------|
| `KEEPALIVED_VIRTUAL_IPS`     |                                         | Comma separated floating IPs in CIDR format, with optional keepalived options per address (`10.0.0.100/24 dev eth1`). Required unless a config file is used |
| `KEEPALIVED_INTERFACE`       | interface of the default route          | Interface VRRP advertisements are sent on and the floating IPs are added to |
| `KEEPALIVED_PRIORITY`        | `100`                                   | Node priority, the node with the highest priority owns the floating IPs |
| `KEEPALIVED_STATE`           | `BACKUP`                                | Initial state, `MASTER` or `BACKUP`. `BACKUP` on every node is fine, the priority decides |
| `KEEPALIVED_ROUTER_ID`       | `51`                                    | `virtual_router_id` (1-255), must be the same on every node of the cluster and unique in the network segment |
| `KEEPALIVED_PASSWORD`        |                                         | VRRP authentication password (max 8 characters) |
| `KEEPALIVED_UNICAST_PEERS`   |                                         | Comma or space separated addresses of the other nodes. Uses VRRP unicast instead of multicast, needed in most clouds |
| `KEEPALIVED_UNICAST_SRC_IP`  | first global IPv4 address of the interface | Own address for unicast VRRP |
| `KEEPALIVED_ADVERT_INT`      | `1`                                     | Advertisement interval in seconds |
| `KEEPALIVED_NOPREEMPT`       | `false`                                 | `true` to keep the floating IP on the current master when a higher priority node comes back. Needs `KEEPALIVED_STATE=BACKUP` on every node |
| `KEEPALIVED_CHECK_SCRIPT`    |                                         | Health check command, run inside the service container. While it fails the node gives up the floating IP, e.g. `nc -z 127.0.0.1 2377` for a Swarm manager |
| `KEEPALIVED_CHECK_INTERVAL`  | `5`                                     | Seconds between health checks |
| `KEEPALIVED_CHECK_TIMEOUT`   | `3`                                     | Health check timeout in seconds |
| `KEEPALIVED_CHECK_FALL`      | `2`                                     | Failed checks before the node is marked faulty |
| `KEEPALIVED_CHECK_RISE`      | `2`                                     | Successful checks before the node is healthy again |
| `KEEPALIVED_CHECK_WEIGHT`    |                                         | Instead of marking the node faulty, adjust its priority by this amount while the check fails (negative) or succeeds (positive) |
| `KEEPALIVED_INSTANCE`        | `VI_1`                                  | Name of the `vrrp_instance` |
| `KEEPALIVED_CONFIG`          | `/var/lib/rancher/conf/keepalived.conf` | Complete configuration file. When it exists the variables above are ignored |

The generated configuration is printed to the service log:
`sudo system-docker logs keepalived`. Restart the service after changing
variables: `sudo system-docker restart keepalived`.

## Own configuration file

For several VRRP instances, IPv6, virtual servers (IPVS) or notify scripts,
write a complete `keepalived.conf` with
[`write_files`](https://burmillaos.org/docs/configuration/advanced/write-files/):

```yaml
#cloud-config
rancher:
  services_include:
    keepalived: true
write_files:
  - path: /var/lib/rancher/conf/keepalived.conf
    permissions: "0600"
    owner: root
    content: |
      vrrp_instance VI_1 {
          state BACKUP
          interface eth0
          virtual_router_id 51
          priority 150
          virtual_ipaddress {
              192.168.1.100/24
          }
      }
```

## Notes

* The service runs privileged in the host network namespace and starts
  right after the `network` service, so the floating IP is available
  before User Docker is up. Use `KEEPALIVED_CHECK_SCRIPT` to hand the
  address over while the service behind it is down.
* Applications that must bind to the floating IP while the node is not the
  master need `net.ipv4.ip_nonlocal_bind=1`, set it with
  `rancher.sysctl`.
* VRRP uses multicast (`224.0.0.18`, IP protocol 112) unless
  `KEEPALIVED_UNICAST_PEERS` is set. Allow it in the firewall between the
  nodes.
* The image is Alpine based, so the health check has BusyBox tools (`nc`,
  `wget`) and `ip`.
