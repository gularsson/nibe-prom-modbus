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
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	host string
	port string
	path string
)

type Register struct {
	Mode    string
	Type    string
	Unit    string
	Name    string
	Desc    string
	Factor  float32
	Address uint16
}

const prefix = "nibe"

var registers = [6]Register{
	{Address: 1, Type: "u16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Outdoor temperature", Name: fmt.Sprintf("%s_outdoor_temperature", prefix)},
	{Address: 5, Type: "u16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Supply temperature", Name: fmt.Sprintf("%s_supply_temperature", prefix)},
	{Address: 7, Type: "u16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Return temperature", Name: fmt.Sprintf("%s_return_temperature", prefix)},
	{Address: 8, Type: "u16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Hot water top", Name: fmt.Sprintf("%s_hotwater_temperature", prefix)},
	{Address: 9, Type: "u16", Unit: "°C", Factor: 10, Mode: "r", Desc: "Hot water charging", Name: fmt.Sprintf("%s_hotwater_charging", prefix)},
	{Address: 2166, Type: "u32", Unit: "W", Factor: 10, Mode: "r", Desc: "Power usage", Name: fmt.Sprintf("%s_power_usage", prefix)},
}

var outdoorTemperature = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "nibe_outdoor_temperature",
	Help: "Outdoor temperature",
})

var supplyTemperature = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "nibe_supply_temperature",
	Help: "Supply temperature",
})

var returnTemperature = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "nibe_return_temperature",
	Help: "Return temperature",
})

var hotWaterTemperature = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "nibe_hotwater_temperature",
	Help: "Hot water temperature",
})

var hotWaterCharging = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "nibe_hotwater_charging",
	Help: "Hot water charging",
})

var powerUsage = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "nibe_power_usage",
	Help: "Power usage",
})

func getData(client modbus.Client, reg Register) float64 {
	result, err := client.ReadInputRegisters(reg.Address, 1)
	if err != nil {
		panic(err)
	}
	return float64(binary.BigEndian.Uint16(result)) / float64(reg.Factor)
}

func recordMetrics(client modbus.Client) {
	go func() {
		for {
			outdoorTemperature.Set(getData(client, registers[0]))
			supplyTemperature.Set(getData(client, registers[1]))
			returnTemperature.Set(getData(client, registers[2]))
			hotWaterTemperature.Set(getData(client, registers[3]))
			hotWaterCharging.Set(getData(client, registers[4]))
			time.Sleep(30 * time.Second)
		}
	}()
}

func init() {
	flag.StringVar(&host, "host", "0.0.0.0", "Heat pump host, assumes port 502")
	flag.StringVar(&port, "port", "2112", "Metrics port, default 2112")
	flag.StringVar(&path, "path", "/metrics", "Metrics path, default /metrics")
}

func main() {
	flag.Parse()

	resourceAddress := fmt.Sprintf("%s:502", host)
	servePort := fmt.Sprintf(":%s", port)

	client := modbus.TCPClient(resourceAddress)
	recordMetrics(client)

	http.Handle(path, promhttp.Handler())

	if err := http.ListenAndServe(servePort, nil); err != nil {
		slog.Error("Could not start server: %s", err)
	}
}
