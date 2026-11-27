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

All metrics use the `nibe_` prefix (configurable via `--prefix`). They are
gauges, except the `*_total` metrics, which are counters.

- **32-bit values are sent low word first.** Before 2026-11 the exporter decoded
  them big-endian, so every u32 metric read exactly 65 536× too high.
- **Unconnected sensors are omitted.** Nibe reports them as -3276.8 °C (s16
  0x8000); no series is exported rather than a fake temperature.
- **Holding registers are only read, never written.**
- A register the pump refuses (Modbus exception) is skipped and logged; the
  rest of the scrape continues.

### Temperatures

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_outdoor_temperature_celsius` | 1 | Outdoor temperature (BT1) |
| `nibe_outdoor_average_celsius` | 37 | Average outdoor temperature (BT1) |
| `nibe_supply_temperature_celsius` | 5 | Supply line temperature (BT2) |
| `nibe_return_temperature_celsius` | 7 | Return line temperature (BT3) |
| `nibe_supply_calculated_celsius` | 1017 | Calculated supply temperature climate system 1 |
| `nibe_room_celsius` | 116 | Room temperature climate system 1 (BT50). Absent if no BT50 is fitted |
| `nibe_hotwater_top_celsius` | 8 | Hot water top (BT7) |
| `nibe_hotwater_charging_celsius` | 9 | Hot water charging temperature (BT6) |
| `nibe_hotwater_outgoing_celsius` | 87 | Outgoing hot water (BT70). Absent if not fitted |
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
| `nibe_operating_prioritisation` | 1028 | 10 off, 20 hot water, 30 heat, 40 pool, 60 cooling |

### Degree minutes

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_degree_minutes` | 11 (holding) | Degree minutes, the heat-balance integral the pump regulates on |

The start thresholds for compressor (holding 97) and additional heat (holding
679) are in the manual but an S1255 rejects both with *illegal function*; read
them from the pump's menu.

### Additional heat

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_additional_heat_steps` | 1029 | Additional heat steps currently active |
| `nibe_additional_heat_runtime_hours` | 1025 | Total run time additional heat |

### Pumps

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_heating_medium_pump_speed_pct` | 1102 | Heating medium pump speed GP1 (%) |
| `nibe_brine_pump_speed_pct` | 1104 | Brine pump speed GP2 (%) |

### Power

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_power_usage_watts` | 2166 | Instantaneous used power, compressor and additional heat |
| `nibe_current_power_watts` | 2176 | Current power. **Always 0 on an S1255** — use `nibe_power_usage_watts` |

The manual gives 2166 a factor of 10. On an S1255 the raw value is already
watts: with ÷10 the implied COP is an impossible ~40. With factor 1 it matches
the hourly energy log.

### Energy

Heat **produced**, measured via the flow sensor (the manual's *Flow
measurement* registers) — not electricity consumed. Cumulative counters.

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_energy_produced_heating_kwh_total` | 1577 | Heating, compressor incl. additional heat |
| `nibe_energy_produced_heating_compressor_kwh_total` | 1585 | Heating, compressor only |
| `nibe_energy_produced_hotwater_kwh_total` | 1575 | Hot water, compressor incl. additional heat |
| `nibe_energy_produced_hotwater_compressor_kwh_total` | 1583 | Hot water, compressor only |

The manual labels 1577 only "compressor incl. add."; on an S1255 it reads
just above 1585, so it is heating including additional heat.

Hourly energy log — **electricity used**, updated once per hour. These
registers are not in Nibe's Modbus manual, but on an S1255 they agree with
`nibe_power_usage_watts` integrated over a week to within ~2%. They are the only per-use
electricity split:

| Metric | Modbus address | Description |
|--------|---------------|-------------|
| `nibe_heating_energy_kwh` | 2291 | Electricity used for heating over the past hour |
| `nibe_hotwater_energy_kwh` | 2293 | Electricity used for hot water over the past hour |
| `nibe_current_hour_energy_kwh` | 2305 | Electricity used in the current hour so far |

## Grafana Dashboards

Pre-built dashboards are available in the [`dashboards/`](dashboards/) directory and can be imported directly into Grafana.

## Reference

[Nibe Modbus S-series documentation (PDF)](https://www.nibe.eu/download/18.3db69dc1795e0d992c5722/1622634529178/Modbus%20S-series%20EN%20M12676EN-1.pdf)
