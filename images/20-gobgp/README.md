# GoBGP
Packages version of [GoBGP](https://github.com/osrg/gobgp/).

# Deployment
Use environment variable like these to generate needed configuration for GoBGP
```yaml
#cloud-config
rancher:
  environment:
    GOBGP_GLOBAL_as: 64512
    GOBGP_GLOBAL_router-id: 192.168.255.1
    GOBGP_NEIGHBOR1_neighbor_address: 10.0.255.1
    GOBGP_NEIGHBOR1_peer-as: 65001
    GOBGP_NEIGHBOR1_auth-password: P@ssw0rd!
    GOBGP_NEIGHBOR1_ROUTE-SERVER_client: true
    GOBGP_NEIGHBOR2_neighbor_address: 10.0.255.2
    GOBGP_NEIGHBOR2_peer-as: 65002
    GOBGP_NEIGHBOR2_auth-password: P@ssw0rd1
    GOBGP_NEIGHBOR2_ROUTE-SERVER_client: true
  services_include:
    gobgp: true
```
