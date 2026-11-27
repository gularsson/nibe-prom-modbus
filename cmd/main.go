package main

import (
	"encoding/binary"
	"errors"
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

// Register is one Nibe Modbus register to export. Addresses, types and factors
// come from the "Common registers" table of nibe_manual_en.pdf.
type Register struct {
	Type    string // u8, s8, u16, s16, u32, s32
	Unit    string
	Name    string
	Desc    string
	Factor  float64
	Address uint16
	// Holding selects FC03 (holding register) instead of FC04 (input register).
	// Many holding registers are R/W on the pump; this exporter only ever READS
	// them. Never add a write call here.
	Holding bool
	// Counter exports the value as a Prometheus counter (monotonic total).
	Counter bool
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
		if errors.Is(err, errSensorAbsent) {
			continue
		}
		var exc *modbus.ModbusError
		if errors.As(err, &exc) {
			// The pump answered but refused this register (e.g. illegal
			// function/address). The connection is fine: skip just this one,
			// rather than dropping every register after it from the scrape.
			slog.Warn("pump rejected register", "register", reg.Name, "address", reg.Address, "err", err)
			continue
		}
		if err != nil {
			slog.Error("failed to read register", "register", reg.Name, "err", err)
			c.handler.Close()
			if err := c.handler.Connect(); err != nil {
				slog.Error("failed to reconnect", "err", err)
			}
			break
		}
		valueType := prometheus.GaugeValue
		if reg.Counter {
			valueType = prometheus.CounterValue
		}
		ch <- prometheus.MustNewConstMetric(c.descs[i], valueType, value)
	}
}

// registerCount returns how many 16-bit Modbus registers a type occupies.
func registerCount(typ string) (uint16, error) {
	switch typ {
	case "u8", "s8", "u16", "s16":
		return 1, nil
	case "u32", "s32":
		return 2, nil
	}
	return 0, fmt.Errorf("unsupported register type: %s", typ)
}

// decode converts raw register bytes to a number, before the scale factor.
//
// Each 16-bit register is big-endian, but Nibe sends 32-bit values LOW WORD
// FIRST. Decoding them as one big-endian uint32 -- which this exporter did
// until 2026-11 -- multiplies every value below 65536 by exactly 65536.
func decode(typ string, b []byte) (float64, error) {
	n, err := registerCount(typ)
	if err != nil {
		return 0, err
	}
	if len(b) < int(n)*2 {
		return 0, fmt.Errorf("%s needs %d bytes, got %d", typ, n*2, len(b))
	}
	switch typ {
	case "u8", "u16":
		return float64(binary.BigEndian.Uint16(b)), nil
	case "s16":
		return float64(int16(binary.BigEndian.Uint16(b))), nil
	case "s8":
		// An 8-bit value in a 16-bit register: the low byte, signed. Taking
		// only the low byte is right whether or not the pump sign-extends.
		return float64(int8(b[1])), nil
	}
	u := uint32(binary.BigEndian.Uint16(b[2:4]))<<16 | uint32(binary.BigEndian.Uint16(b[0:2]))
	if typ == "s32" {
		return float64(int32(u)), nil
	}
	return float64(u), nil
}

func getData(client modbus.Client, reg Register) (float64, error) {
	n, err := registerCount(reg.Type)
	if err != nil {
		return 0, err
	}

	var result []byte
	if reg.Holding {
		result, err = client.ReadHoldingRegisters(reg.Address, n)
	} else {
		result, err = client.ReadInputRegisters(reg.Address, n)
	}
	if err != nil {
		return 0, err
	}

	value, err := decode(reg.Type, result)
	if err != nil {
		return 0, err
	}
	if reg.Type == "s16" && value == sensorAbsent {
		return 0, errSensorAbsent
	}
	return value / reg.Factor, nil
}

// Nibe reports an unconnected sensor as s16 0x8000 (-32768, i.e. -3276.8 °C
// after the factor). Exporting it would plot as a real, absurd temperature, so
// the series is omitted instead.
const sensorAbsent = -32768

var errSensorAbsent = errors.New("sensor not connected")

func init() {
	flag.StringVar(&host, "host", "0.0.0.0", "Heat pump host, assumes port 502")
	flag.StringVar(&port, "port", "2112", "Metrics port, default 2112")
	flag.StringVar(&path, "path", "/metrics", "Metrics path, default /metrics")
	flag.StringVar(&prefix, "prefix", "nibe", "Prefix for metrics, default nibe")
}

func main() {
	// Parsed here, not in init(): parsing in init() also ran under `go test`
	// and rejected the test binary's own -test.* flags.
	flag.Parse()

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

		// Operating state
		{Address: 1028, Type: "u8", Unit: "", Factor: 1, Desc: "Operating prioritisation (10 off, 20 hot water, 30 heat, 40 pool, 60 cooling)", Name: "operating_prioritisation"},

		// Degree minutes (holding register, READ ONLY): the heat-balance
		// integral the pump regulates on. The start thresholds for compressor
		// (reg 97) and additional heat (reg 679) are listed in the manual but
		// an S1255 answers both with "illegal function" -- read them from the
		// pump's menu instead.
		{Address: 11, Holding: true, Type: "s32", Unit: "DM", Factor: 10, Desc: "Degree minutes", Name: "degree_minutes"},

		// Hot water demand (holding register, READ ONLY): menu 2.2.
		{Address: 56, Holding: true, Type: "s8", Unit: "", Factor: 1, Desc: "Hot water demand (0 small, 1 medium, 2 large, 3 not in use, 4 smart control)", Name: "hotwater_demand"},

		// Additional heat (internal immersion heater)
		{Address: 1029, Type: "u8", Unit: "", Factor: 1, Desc: "Additional heat steps currently active", Name: "additional_heat_steps"},
		{Address: 1025, Type: "s32", Unit: "h", Factor: 10, Desc: "Total run time additional heat", Name: "additional_heat_runtime_hours"},

		// Power
		// The manual gives 2166 a factor of 10, but on an S1255 the raw value
		// is already watts: with /10 the implied COP is over 40. With factor 1
		// it matches the hourly energy log.
		{Address: 2166, Type: "u32", Unit: "W", Factor: 1, Desc: "Instantaneous used power, compressor and additional heat", Name: "power_usage_watts"},
		// Reads 0 on an S1255 at all times. Kept so the series
		// does not silently vanish from existing dashboards; do not use it.
		{Address: 2176, Type: "u32", Unit: "W", Factor: 1, Desc: "Current power (always 0 on S1255; use power_usage_watts)", Name: "current_power_watts"},

		// Energy meters: the manual's "Flow measurement" registers, i.e. heat
		// energy measured via the flow sensor -- PRODUCED heat, not electricity
		// consumed. Cumulative, so robust to scrape gaps. The manual labels
		// 1577 only "compressor incl. add."; on an S1255 it reads slightly above
		// 1585 (heating, compressor only), so it is heating incl. add. heat.
		{Address: 1575, Type: "u32", Unit: "kWh", Factor: 10, Counter: true, Desc: "Heat produced for hot water, compressor incl. additional heat", Name: "energy_produced_hotwater_kwh_total"},
		{Address: 1577, Type: "u32", Unit: "kWh", Factor: 10, Counter: true, Desc: "Heat produced for heating, compressor incl. additional heat", Name: "energy_produced_heating_kwh_total"},
		{Address: 1583, Type: "u32", Unit: "kWh", Factor: 10, Counter: true, Desc: "Heat produced for hot water, compressor only", Name: "energy_produced_hotwater_compressor_kwh_total"},
		{Address: 1585, Type: "u32", Unit: "kWh", Factor: 10, Counter: true, Desc: "Heat produced for heating, compressor only", Name: "energy_produced_heating_compressor_kwh_total"},

		// Energy log: ELECTRICITY used, updated once per hour. Not in Nibe's
		// Modbus manual, but cross-checked on an S1255: over a week, heating + hot
		// water agree within ~2% with integrating power_usage_watts. The only
		// per-use electricity split.
		{Address: 2291, Type: "u16", Unit: "kWh", Factor: 100, Desc: "Electricity used for heating over the past hour", Name: "heating_energy_kwh"},
		{Address: 2293, Type: "u16", Unit: "kWh", Factor: 100, Desc: "Electricity used for hot water over the past hour", Name: "hotwater_energy_kwh"},
		{Address: 2305, Type: "u16", Unit: "kWh", Factor: 100, Desc: "Electricity used in the current hour so far", Name: "current_hour_energy_kwh"},

		// Compressor runtimes and starts (EB100-EP14)
		{Address: 1534, Type: "u32", Unit: "", Factor: 1, Counter: true, Desc: "Compressor total number of starts (EB100-EP14)", Name: "compressor_starts_total"},
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
