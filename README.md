# nibe-prom-modbus

## What
Export metrics from Nibe heatpumps in prometheus format

## How
Run service (as of now):
```bash
go run cmd/main.go
```

Add as a job to your prometheus config:
```yaml
- job_name: "nibe"
    static_configs:
      - targets: ["192.168.1.225:2112"]
```

Metrics should then be exposed for use in Grafana under the prefix `nibe`

## TODO
* Learn Go
* Add more Metrics
* Dockerize
