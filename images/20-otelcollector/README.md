# OpenTelemetry Collector

Optional system service which runs
[otelcol-contrib](https://github.com/open-telemetry/opentelemetry-collector-releases)
on a BurmillaOS server and exports its metrics and logs with OTLP/gRPC.

The [default configuration](config.yaml) collects:

* **Metrics** – host metrics (CPU, memory, disk, filesystem, network, load,
  processes), User Docker container stats (`docker_stats`, Swarm service and
  task names as `job_name` and `task_name` labels) and the collector's own
  metrics.
* **Logs** – system logs from `/var/log/syslog` and the logs of every User
  Docker and System Docker container. BurmillaOS does not run journald, so
  container logs are read from the `json-file` log files instead, with
  `container.name`, `container.id`, `container.image.name` and
  `docker.engine` (`user` / `system`) resource attributes. `service.name` is
  the Swarm service name for Swarm tasks and the container name otherwise.
  stdout lines get severity `INFO` and stderr lines `ERROR`.
* **OTLP** – receivers on `127.0.0.1:4317` (gRPC) and `127.0.0.1:4318`
  (HTTP) for applications running on the host.

## Enable

```bash
sudo ros config set rancher.environment.OTELCOL_ENDPOINT otel-gateway.example.com:4317
sudo ros service enable otel-collector
sudo ros service up otel-collector
```

or with cloud-init:

```yaml
rancher:
  environment:
    OTELCOL_ENDPOINT: otel-gateway.example.com:4317
  services_include:
    otel-collector: true
```

| Variable                 | Default                                   | Description |
|--------------------------|-------------------------------------------|-------------|
| `OTELCOL_ENDPOINT`       |                                           | OTLP/gRPC endpoint (`host:port`) where everything is exported. Required with the default config |
| `OTELCOL_TLS_INSECURE`   | `true`                                    | `false` to use TLS towards `OTELCOL_ENDPOINT` |
| `OTELCOL_LOG_LEVEL`      | `warn`                                    | Log level of the collector itself (`debug`, `info`, `warn`, `error`). Used with the default config |
| `OTELCOL_CONFIG`         | `/var/lib/rancher/conf/otel-collector.yaml` | Additional config file, used when it exists |
| `OTELCOL_DEFAULT_CONFIG` | `true`                                    | `false` to skip the default config and use only `OTELCOL_CONFIG` |

Logs: `sudo system-docker logs otel-collector`. Only warnings and errors are
logged by default; set `OTELCOL_LOG_LEVEL` to `info` or `debug` for
troubleshooting.

## Additional configuration

Write an additional config file with `write_files`. It is passed to the
collector as a second `--config`, so it is merged on top of the default
config: maps are merged and lists (e.g. the receivers of a pipeline) are
replaced. Environment variables can be used with `${env:NAME}`.

For example, to also scrape a Prometheus endpoint:

```yaml
write_files:
  - path: /var/lib/rancher/conf/otel-collector.yaml
    permissions: "0600"
    owner: root
    content: |
      receivers:
        prometheus/node-app:
          config:
            scrape_configs:
              - job_name: node-app
                scrape_interval: 30s
                static_configs:
                  - targets: ["127.0.0.1:9100"]
      service:
        pipelines:
          metrics/node-app:
            receivers: [prometheus/node-app]
            processors: [resource_detection/system, batch]
            exporters: [otlp_grpc]
```

To use only your own config, set `OTELCOL_DEFAULT_CONFIG` to `false`.
`OTELCOL_ENDPOINT` is then not needed and the file must contain a complete
collector configuration:

```yaml
rancher:
  environment:
    OTELCOL_DEFAULT_CONFIG: "false"
  services_include:
    otel-collector: true
write_files:
  - path: /var/lib/rancher/conf/otel-collector.yaml
    permissions: "0600"
    owner: root
    content: |
      receivers:
        ...
```

Restart the service after changing the file:
`sudo system-docker restart otel-collector`.

## Notes

* The host root filesystem is mounted read-only to `/hostfs` and the
  collector runs privileged with the host PID, network and UTS namespaces.
* Log read positions are stored in `/var/lib/rancher/otel-collector`, so
  logs are not sent twice after a restart.
* The collector's own container logs are not collected.
* The User Docker must be running: the service starts after it and is
  restarted until the Docker API is reachable.
