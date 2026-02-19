package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/goburrow/modbus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	host   string
	port   string
	path   string
	prefix string
)

type Register struct {
	Type    string
	Unit    string
	Name    string
	Desc    string
	Factor  float64
	Address uint16
}

type nibeCollector struct {
	handler   *modbus.TCPClientHandler
	registers []Register
	descs     []*prometheus.Desc
}

func newCollector(handler *modbus.TCPClientHandler, registers []Register) *nibeCollector {
	descs := make([]*prometheus.Desc, len(registers))
	for i, reg := range registers {
		descs[i] = prometheus.NewDesc(
			fmt.Sprintf("%s_%s", prefix, reg.Name),
			reg.Desc,
			nil, nil,
		)
	}
	return &nibeCollector{handler: handler, registers: registers, descs: descs}
}

func (c *nibeCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.descs {
		ch <- d
	}
}

func (c *nibeCollector) Collect(ch chan<- prometheus.Metric) {
	client := modbus.NewClient(c.handler)
	for i, reg := range c.registers {
		value, err := getData(client, reg)
		if err != nil {
			slog.Error("failed to read register", "register", reg.Name, "err", err)
			c.handler.Close()
			if err := c.handler.Connect(); err != nil {
				slog.Error("failed to reconnect", "err", err)
			}
			break
		}
		ch <- prometheus.MustNewConstMetric(c.descs[i], prometheus.GaugeValue, value)
	}
}

func getData(client modbus.Client, reg Register) (float64, error) {
	var result []byte
	var err error

	switch reg.Type {
	case "u16", "s16":
		result, err = client.ReadInputRegisters(reg.Address, 1)
	case "u32":
		result, err = client.ReadInputRegisters(reg.Address, 2)
	default:
		return 0, fmt.Errorf("unsupported register type: %s", reg.Type)
	}

	if err != nil {
		return 0, err
	}

	var value float64
	switch reg.Type {
	case "u16":
		value = float64(binary.BigEndian.Uint16(result))
	case "s16":
		value = float64(int16(binary.BigEndian.Uint16(result)))
	case "u32":
		value = float64(binary.BigEndian.Uint32(result))
	}

	return value / reg.Factor, nil
}

func init() {
	flag.StringVar(&host, "host", "0.0.0.0", "Heat pump host, assumes port 502")
	flag.StringVar(&port, "port", "2112", "Metrics port, default 2112")
	flag.StringVar(&path, "path", "/metrics", "Metrics path, default /metrics")
	flag.StringVar(&prefix, "prefix", "nibe", "Prefix for metrics, default nibe")

	flag.Parse()
}

func main() {
	resourceAddress := fmt.Sprintf("%s:502", host)
	servePort := fmt.Sprintf(":%s", port)

	registers := []Register{
		// Temperatures
		{Address: 1, Type: "s16", Unit: "°C", Factor: 10, Desc: "Outdoor temperature (BT1)", Name: "outdoor_temperature_celsius"},
		{Address: 5, Type: "s16", Unit: "°C", Factor: 10, Desc: "Supply line temperature (BT2)", Name: "supply_temperature_celsius"},
		{Address: 7, Type: "s16", Unit: "°C", Factor: 10, Desc: "Return line temperature (BT3)", Name: "return_temperature_celsius"},
		{Address: 8, Type: "s16", Unit: "°C", Factor: 10, Desc: "Hot water top (BT7)", Name: "hotwater_top_celsius"},
		{Address: 9, Type: "s16", Unit: "°C", Factor: 10, Desc: "Hot water charging temperature (BT6)", Name: "hotwater_charging_celsius"},
		{Address: 10, Type: "s16", Unit: "°C", Factor: 10, Desc: "Brine in (BT10)", Name: "brine_in_celsius"},
		{Address: 11, Type: "s16", Unit: "°C", Factor: 10, Desc: "Brine out (BT11)", Name: "brine_out_celsius"},
		{Address: 12, Type: "s16", Unit: "°C", Factor: 10, Desc: "Condenser (BT12)", Name: "condenser_celsius"},
		{Address: 13, Type: "s16", Unit: "°C", Factor: 10, Desc: "Discharge (BT14)", Name: "discharge_celsius"},
		{Address: 14, Type: "s16", Unit: "°C", Factor: 10, Desc: "Liquid line (BT15)", Name: "liquid_line_celsius"},
		{Address: 16, Type: "s16", Unit: "°C", Factor: 10, Desc: "Suction gas (BT17)", Name: "suction_gas_celsius"},
		{Address: 37, Type: "s16", Unit: "°C", Factor: 10, Desc: "Average outdoor temperature (BT1)", Name: "outdoor_average_celsius"},
		{Address: 87, Type: "s16", Unit: "°C", Factor: 10, Desc: "Outgoing hot water (BT70)", Name: "hotwater_outgoing_celsius"},
		{Address: 116, Type: "s16", Unit: "°C", Factor: 10, Desc: "Room temperature climate system 1 (BT50)", Name: "room_celsius"},
		{Address: 1017, Type: "s16", Unit: "°C", Factor: 10, Desc: "Calculated supply temperature climate system 1", Name: "supply_calculated_celsius"},

		// Flow
		{Address: 40, Type: "u16", Unit: "l/m", Factor: 10, Desc: "Flow sensor (BF1)", Name: "flow_litres_per_minute"},

		// Compressor operating data
		{Address: 1041, Type: "s16", Unit: "Hz", Factor: 1, Desc: "Compressor minimum allowed frequency", Name: "compressor_min_frequency_hz"},
		{Address: 1042, Type: "s16", Unit: "Hz", Factor: 1, Desc: "Compressor maximum allowed frequency", Name: "compressor_max_frequency_hz"},
		{Address: 1094, Type: "u16", Unit: "", Factor: 1, Desc: "Compressor operating mode", Name: "compressor_operating_mode"},
		{Address: 1100, Type: "u16", Unit: "", Factor: 1, Desc: "Compressor status", Name: "compressor_status"},
		{Address: 1102, Type: "u16", Unit: "%", Factor: 1, Desc: "Heating medium pump speed (GP1)", Name: "heating_medium_pump_speed_pct"},
		{Address: 1104, Type: "u16", Unit: "%", Factor: 1, Desc: "Brine pump speed (GP2)", Name: "brine_pump_speed_pct"},

		// Power
		{Address: 2166, Type: "u32", Unit: "W", Factor: 10, Desc: "Power usage", Name: "power_usage_watts"},
		{Address: 2176, Type: "u32", Unit: "W", Factor: 1, Desc: "Current power consumption", Name: "current_power_watts"},

		// Energy log (updated once per hour)
		{Address: 2291, Type: "u16", Unit: "kWh", Factor: 100, Desc: "Energy used for heating over the past hour", Name: "heating_energy_kwh"},
		{Address: 2293, Type: "u16", Unit: "kWh", Factor: 100, Desc: "Energy used for hot water over the past hour", Name: "hotwater_energy_kwh"},
		{Address: 2305, Type: "u16", Unit: "kWh", Factor: 100, Desc: "Energy consumed in the current hour so far", Name: "current_hour_energy_kwh"},

		// Compressor runtimes and starts (EB100-EP14)
		{Address: 1534, Type: "u32", Unit: "", Factor: 1, Desc: "Compressor total number of starts (EB100-EP14)", Name: "compressor_starts_total"},
		{Address: 1536, Type: "u32", Unit: "h", Factor: 1, Desc: "Compressor total operating time (EB100-EP14)", Name: "compressor_runtime_total_hours"},
		{Address: 1538, Type: "u32", Unit: "h", Factor: 1, Desc: "Compressor hot water operating time (EB100-EP14)", Name: "compressor_runtime_hotwater_hours"},
	}

	handler := modbus.NewTCPClientHandler(resourceAddress)
	handler.Timeout = 10 * time.Second
	handler.SlaveId = 1
	defer handler.Close()

	prometheus.MustRegister(newCollector(handler, registers))

	http.Handle(path, promhttp.Handler())

	if err := http.ListenAndServe(servePort, nil); err != nil {
		slog.Error("could not start server", "err", err)
	}
}
