package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/goburrow/modbus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	host     string
	port     string
	path     string
	interval string
	prefix   string
)

type Register struct {
	Mode    string
	Type    string
	Unit    string
	Name    string
	Desc    string
	Factor  float64
	Address uint16
}

func setName(suffix string) string {
	return fmt.Sprintf("%s_%s", prefix, suffix)
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

func recordMetrics(ctx context.Context, client modbus.Client, registers []Register, metrics map[string]prometheus.Gauge) {
	duration, err := strconv.Atoi(interval)
	if err != nil {
		slog.Error("could not convert interval to int", "err", err)
	}
	ticker := time.NewTicker(time.Duration(duration) * time.Second)
	defer ticker.Stop()

	for {
		for _, reg := range registers {
			if gauge, ok := metrics[reg.Name]; ok {
				value, err := getData(client, reg)
				if err != nil {
					slog.Error("failed to read register", "register", reg.Name, "err", err)
					continue
				}
				gauge.Set(value)
			} else {
				slog.Error("metric not found for register", "register", reg.Name)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func init() {
	flag.StringVar(&host, "host", "0.0.0.0", "Heat pump host, assumes port 502")
	flag.StringVar(&port, "port", "2112", "Metrics port, default 2112")
	flag.StringVar(&path, "path", "/metrics", "Metrics path, default /metrics")
	flag.StringVar(&interval, "interval", "30", "Fetch interval, default 30s")
	flag.StringVar(&prefix, "prefix", "nibe", "Prefix for metrics, default nibe")

	flag.Parse()
}

func main() {
	resourceAddress := fmt.Sprintf("%s:502", host)
	servePort := fmt.Sprintf(":%s", port)

	registers := []Register{
		{Address: 1, Type: "s16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Outdoor temperature in °C", Name: "outdoor_temperature"},
		{Address: 5, Type: "s16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Supply temperature in °C", Name: "supply_temperature"},
		{Address: 7, Type: "s16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Return temperature in °C", Name: "return_temperature"},
		{Address: 8, Type: "u16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Hot water top in °C", Name: "hotwater_temperature"},
		{Address: 9, Type: "u16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Hot water charging in °C", Name: "hotwater_charging"},
		{Address: 2166, Type: "u32", Unit: "W", Factor: 10, Mode: "r", Desc: "Power usage in Watt", Name: "power_usage"},
	}

	metrics := make(map[string]prometheus.Gauge)
	for _, reg := range registers {
		metrics[reg.Name] = promauto.NewGauge(prometheus.GaugeOpts{
			Name: setName(reg.Name),
			Help: reg.Desc,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := modbus.TCPClient(resourceAddress)
	go recordMetrics(ctx, client, registers, metrics)

	http.Handle(path, promhttp.Handler())

	if err := http.ListenAndServe(servePort, nil); err != nil {
		slog.Error("could not start server", "err", err)
	}
}
