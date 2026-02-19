# nibe-prom-modbus

Export metrics from Nibe S-series heat pumps in Prometheus format via Modbus TCP.

On each Prometheus scrape the service reads a set of Modbus input registers from the heat pump, converts them to typed gauge metrics, and exposes them on an HTTP endpoint. There is no internal polling interval — scrape frequency is configured entirely on the Prometheus side.

## Requirements

- Nibe S-series heat pump with Modbus TCP enabled (port 502)
- Go 1.25+ **or** Docker

## Usage

### Run directly

```bash
go run cmd/main.go --host=<heat-pump-ip>
```

### Build and run binary

```bash
CGO_ENABLED=0 go build -o main ./cmd/main.go
./main --host=<heat-pump-ip>
```

### Docker

A pre-built image is published to the GitHub Container Registry on every push to `main`:

```bash
docker run -p 2112:2112 ghcr.io/gularsson/nibe-prom-modbus:main --host=<heat-pump-ip>
```

Or build locally:

```bash
docker build -t nibe-prom-modbus .
docker run -p 2112:2112 nibe-prom-modbus --host=<heat-pump-ip>
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `0.0.0.0` | Heat pump IP address (Modbus TCP port 502) |
| `--port` | `2112` | HTTP metrics port |
| `--path` | `/metrics` | HTTP metrics path |
| `--prefix` | `nibe` | Prometheus metric name prefix |

## Prometheus Configuration

Add the exporter as a scrape target:

```yaml
scrape_configs:
  - job_name: "nibe"
    static_configs:
      - targets: ["<heat-pump-ip>:2112"]
```

## Metrics

All metrics are exposed as Prometheus gauges under the `nibe_` prefix (configurable via `--prefix`).

### Temperatures

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_outdoor_temperature_celsius` | 1 | Outdoor temperature (BT1) |
| `nibe_outdoor_average_celsius` | 37 | Average outdoor temperature (BT1) |
| `nibe_supply_temperature_celsius` | 5 | Supply line temperature (BT2) |
| `nibe_return_temperature_celsius` | 7 | Return line temperature (BT3) |
| `nibe_supply_calculated_celsius` | 1017 | Calculated supply temperature climate system 1 |
| `nibe_room_celsius` | 116 | Room temperature climate system 1 (BT50) |
| `nibe_hotwater_top_celsius` | 8 | Hot water top (BT7) |
| `nibe_hotwater_charging_celsius` | 9 | Hot water charging temperature (BT6) |
| `nibe_hotwater_outgoing_celsius` | 87 | Outgoing hot water (BT70) |
| `nibe_brine_in_celsius` | 10 | Brine in (BT10) |
| `nibe_brine_out_celsius` | 11 | Brine out (BT11) |
| `nibe_condenser_celsius` | 12 | Condenser temperature (BT12) |
| `nibe_discharge_celsius` | 13 | Discharge temperature (BT14) |
| `nibe_liquid_line_celsius` | 14 | Liquid line temperature (BT15) |
| `nibe_suction_gas_celsius` | 16 | Suction gas temperature (BT17) |

### Flow

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_flow_litres_per_minute` | 40 | Flow sensor (BF1) |

### Compressor

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_compressor_status` | 1100 | Compressor status |
| `nibe_compressor_operating_mode` | 1094 | Compressor operating mode |
| `nibe_compressor_min_frequency_hz` | 1041 | Compressor minimum allowed frequency |
| `nibe_compressor_max_frequency_hz` | 1042 | Compressor maximum allowed frequency |
| `nibe_compressor_starts_total` | 1534 | Total number of compressor starts (EB100-EP14) |
| `nibe_compressor_runtime_total_hours` | 1536 | Total compressor operating time (EB100-EP14) |
| `nibe_compressor_runtime_hotwater_hours` | 1538 | Compressor hot water operating time (EB100-EP14) |

### Pumps

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_heating_medium_pump_speed_pct` | 1102 | Heating medium pump speed GP1 (%) |
| `nibe_brine_pump_speed_pct` | 1104 | Brine pump speed GP2 (%) |

### Power

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_power_usage_watts` | 2166 | Power usage |
| `nibe_current_power_watts` | 2176 | Current power consumption |

## Grafana Dashboards

Pre-built dashboards are available in the [`dashboards/`](dashboards/) directory and can be imported directly into Grafana.

## Reference

[Nibe Modbus S-series documentation (PDF)](https://www.nibe.eu/download/18.3db69dc1795e0d992c5722/1622634529178/Modbus%20S-series%20EN%20M12676EN-1.pdf)
