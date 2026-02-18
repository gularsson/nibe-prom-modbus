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
		{Address: 1, Type: "s16", Unit: "°C", Factor: 10, Desc: "Outdoor temperature in °C", Name: "outdoor_temperature_celsius"},
		{Address: 5, Type: "s16", Unit: "°C", Factor: 10, Desc: "Supply temperature in °C", Name: "supply_temperature_celsius"},
		{Address: 7, Type: "s16", Unit: "°C", Factor: 10, Desc: "Return temperature in °C", Name: "return_temperature_celsius"},
		{Address: 8, Type: "u16", Unit: "°C", Factor: 10, Desc: "Hot water top in °C", Name: "hotwater_top_celsius"},
		{Address: 9, Type: "u16", Unit: "°C", Factor: 10, Desc: "Hot water charging temperature in °C", Name: "hotwater_charging_celsius"},
		{Address: 2166, Type: "u32", Unit: "W", Factor: 10, Desc: "Power usage in watts", Name: "power_usage_watts"},
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
