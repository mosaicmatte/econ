package simulation

import (
	"econ/schema/Telemetry"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/gorilla/websocket"
)

// Building Data structs
type ThermalProps struct {
	BaseHeatLoad        float64 `json:"baseHeatLoad"`
	Setpoint            float64 `json:"setpoint"`
	Deadband            float64 `json:"deadband"`
	SolarGainMultiplier float64 `json:"solarGainMultiplier"`
	RWall               float64 `json:"rWall"`
	CAir                float64 `json:"cAir"`
}

type HvacMap struct {
	VavId string `json:"vavId"`
}

type ZoneData struct {
	ZoneId            string       `json:"zoneId"`
	ZoneType          string       `json:"zoneType"`
	BimAssetId        string       `json:"bim_asset_id"`
	Volume            float64      `json:"volume"`
	WallArea          float64      `json:"wallArea"`
	ThermalProperties ThermalProps `json:"thermalProperties"`
	HvacMapping       HvacMap      `json:"hvacMapping"`
}

type FloorData struct {
	Zones []ZoneData `json:"zones"`
}

type BuildingData struct {
	Floors []FloorData `json:"floors"`
}

// Sim Structs
type ZoneSim struct {
	Temp              float64
	WallTemp          float64
	Type              string
	BimAssetId        string
	Occupancy         int
	BaseHeatGain      float64
	SolarGainMult     float64
	CAir              float64
	CWall             float64
	RIn               float64
	ROut              float64
	Setpoint          float64
	BaseSetpoint      float64 // occupied setpoint; we set back from this when vacant
	Deadband          float64
	LastBroadcastTemp float64
	// Occupancy-driven control (real data arrives over MQTT from the CV/edge layer)
	Live        bool   // true once real occupancy has been received for this zone
	VacantTicks int    // consecutive ticks at 0 occupancy (safety delay before setback)
	LightsOn    bool   // last actuated lighting state
	MqttTopic   string // telemetry suffix this zone was seen on (commands route back here)
	OverrideUntil time.Time // Latch manual overrides so optimizer doesn't overwrite
	// Hardware-in-the-loop (physical ESP32 / Pico nodes). While the bound node's
	// measured temperature is fresh, the zone's air temp is pulled to the measurement
	// instead of the 2R1C integration — the dashboard shows the physical room, not the
	// model. Simulated placeholder temps never set HwTempAt (see Measurement.TempReal).
	HwSource string    // node kind ("esp32", "pico", "cv", ...); empty = never bound
	HwSeenAt time.Time // last telemetry of any kind from the bound node
	HwTemp   float64   // last measured air temperature (valid only while HwTempAt is fresh)
	HwTempAt time.Time // when HwTemp arrived; zero = node never sent a real temperature
	HwHum    float64   // last measured relative humidity (%), surfaced via /api/hardware
	HwCo2    float64   // last measured CO2 (ppm)
	HwOnline bool      // broker LWT verdict from econ/status/<topic>
}

type VavSim struct {
	TargetZone        string
	Resistance        float64
	Flow              float64
	NominalFlow       float64 // flow at default resistance; cooling is sized against this
	LastBroadcastFlow float64
}

type Engine struct {
	Clients     map[*websocket.Conn]bool
	mu          sync.Mutex
	Zones       map[string]*ZoneSim
	Vavs        map[string]*VavSim
	AhuPressure float64
	PMax        float64
	KFan        float64
	Scenario    string
	FaultTarget string
	// Actuation: set by main.go to the MQTT publisher; nil when no broker is up.
	Publish func(topic, payload string)
	// Persist: set by main.go to the TimescaleDB writer; nil when no DB is up.
	Persist    func(zoneId, sensorType string, value float64)
	lastDbSave time.Time
	lastCmd    map[string]string // zoneId -> last command published (dedupe)
	demoAssign map[string]string // edge-node identifier -> zoneId (sticky demo binding)
}

func NewEngine() *Engine {
	e := &Engine{
		Clients:  make(map[*websocket.Conn]bool),
		Zones:    make(map[string]*ZoneSim),
		Vavs:     make(map[string]*VavSim),
		PMax:     600.0,
		KFan:     0.01,
		Scenario: "peak",
		lastCmd:  make(map[string]string),
		demoAssign: make(map[string]string),
	}

	data, err := os.ReadFile("./data/building-data.json")
	if err != nil {
		log.Printf("Failed to load building data: %v", err)
		return e
	}

	var bd BuildingData
	if err := json.Unmarshal(data, &bd); err != nil {
		log.Printf("Failed to parse building data: %v", err)
		return e
	}

	for _, f := range bd.Floors {
		for _, z := range f.Zones {
			if z.HvacMapping.VavId != "" {
				e.Vavs[z.HvacMapping.VavId] = &VavSim{
					TargetZone: z.ZoneId,
					Resistance: 1.0,
					Flow:       0,
				}
			}

			temp := z.ThermalProperties.Setpoint
			if temp == 0 {
				temp = 24.0
				if z.ZoneType == "server-room" {
					temp = 22.0
				}
			}

			baseSp := z.ThermalProperties.Setpoint
			if baseSp == 0 {
				baseSp = temp
			}
			e.Zones[z.ZoneId] = &ZoneSim{
				Temp:         temp,
				WallTemp:     temp,
				Type:         z.ZoneType,
				BimAssetId:   z.BimAssetId,
				Occupancy:    rand.Intn(10),
				BaseHeatGain: z.ThermalProperties.BaseHeatLoad,
				SolarGainMult: z.ThermalProperties.SolarGainMultiplier,
				// Floor CAir: some digitized zones (e.g. tiny "server rooms") carry an
				// unrealistically small air capacitance that makes the explicit-Euler thermal
				// integration unstable (runaway temps). A modest floor keeps it stable; steady
				// state is unaffected since it depends on the heat balance, not CAir.
				CAir:         math.Max(z.ThermalProperties.CAir, 5e5),
				CWall:        4000000.0,
				RIn:          z.ThermalProperties.RWall / 2,
				ROut:         z.ThermalProperties.RWall / 2 + 0.1,
				Setpoint:     z.ThermalProperties.Setpoint,
				BaseSetpoint: baseSp,
				Deadband:     z.ThermalProperties.Deadband,
				LastBroadcastTemp: 24.0,
				LightsOn:     true,
			}
		}
	}

	e.doHardyCross()
	// Capture each VAV's nominal flow (at default resistance) so the cooling
	// model can be normalized to it regardless of how many VAVs share the AHU.
	for _, v := range e.Vavs {
		v.NominalFlow = v.Flow
	}
	return e
}

func (e *Engine) doHardyCross() {
	sumInvSqrtR := 0.0
	for _, v := range e.Vavs {
		sumInvSqrtR += 1.0 / math.Sqrt(v.Resistance)
	}
	R_system := 1.0 / (sumInvSqrtR * sumInvSqrtR)

	Q_total_sq := e.PMax / (e.KFan + R_system)
	e.AhuPressure = R_system * Q_total_sq

	for _, v := range e.Vavs {
		v.Flow = math.Sqrt(math.Max(0, e.AhuPressure) / v.Resistance)
	}
}

// demoZoneAlias maps inbound MQTT identifiers (demo node names / aliases) to a real
// building zone. In a full deployment the payload would carry the actual zoneId.
var demoZoneAlias = map[string]string{
	"zone_1":  "zone-north-west-office-lvl4",
	"Level 4": "zone-north-west-office-lvl4",
}

// Measurement is one telemetry sample from a physical edge node (ESP32, Pico, CV
// tracker). Pointer fields are nil when the node didn't report that quantity.
// TempReal marks a genuinely measured temperature (DHT22, RP2040 die sensor, ...) as
// opposed to a firmware's simulated placeholder — only real temperatures may pin the
// zone's physics to the sensor.
type Measurement struct {
	Occupancy *int
	Temp      *float64
	Humidity  *float64
	Co2       *float64
	Source    string
	TempReal  bool
}

// IngestTelemetry ingests one sample from the CV/edge layer (MQTT) and marks the zone
// "live" so the physics + optimizer use real data instead of the random seed. This is
// what makes the twin genuinely sensor-driven.
func (e *Engine) IngestTelemetry(zoneRef, topicSuffix string, m Measurement) {
	e.mu.Lock()
	defer e.mu.Unlock()
	z := e.resolveZone(zoneRef)
	if z == nil {
		log.Printf("[telemetry] no zone matches %q; ignoring", zoneRef)
		return
	}
	if m.Occupancy != nil {
		z.Occupancy = *m.Occupancy
		z.Live = true
	}
	if topicSuffix != "" {
		z.MqttTopic = topicSuffix
	}
	if m.Source != "" {
		z.HwSource = m.Source
	}
	z.HwSeenAt = time.Now()
	z.HwOnline = true
	if m.Temp != nil && m.TempReal {
		z.HwTemp = *m.Temp
		z.HwTempAt = time.Now()
	}
	if m.Humidity != nil {
		z.HwHum = *m.Humidity
	}
	if m.Co2 != nil {
		z.HwCo2 = *m.Co2
	}
}

// SetZoneOccupancy keeps the original CV-layer entry point (yolo_tracker.py publishes
// occupancy-only messages): plain occupancy ingestion, attributed to the CV node.
func (e *Engine) SetZoneOccupancy(zoneRef, topicSuffix string, count int) {
	occ := count
	e.IngestTelemetry(zoneRef, topicSuffix, Measurement{Occupancy: &occ, Source: "cv"})
}

// resolveZone maps an inbound identifier (real zoneId or demo alias) to a zone. Lock held.
func (e *Engine) resolveZone(ref string) *ZoneSim {
	if z, ok := e.Zones[ref]; ok {
		return z
	}
	if id, ok := demoZoneAlias[ref]; ok {
		if z, ok := e.Zones[id]; ok {
			return z
		}
	}
	// Fallback: a regenerated building changes zoneIds, so an aliased id may not exist.
	// Assign each unknown identifier its own office zone so two physical boards (say an
	// ESP32 and a Pico) demo side by side instead of both landing on the same fallback.
	return e.assignDemoZone(ref)
}

// assignDemoZone gives an unrecognized edge-node identifier a stable, distinct office
// zone: the first unknown node gets the lexicographically-smallest office, the second
// the next one, and so on (deterministic despite Go's randomized map iteration; wraps
// around if there are somehow more nodes than offices). Lock held.
func (e *Engine) assignDemoZone(ref string) *ZoneSim {
	if id, ok := e.demoAssign[ref]; ok {
		return e.Zones[id]
	}
	offices := make([]string, 0, 16)
	for id, z := range e.Zones {
		if z.Type == "office" {
			offices = append(offices, id)
		}
	}
	if len(offices) == 0 {
		return nil // building without offices: nothing sensible to bind a demo node to
	}
	sort.Strings(offices)
	id := offices[len(e.demoAssign)%len(offices)]
	e.demoAssign[ref] = id
	log.Printf("[edge] node %q bound to zone %s", ref, id)
	return e.Zones[id]
}

const vacancyDelayTicks = 90 // ~3s at 30 FPS — stand-in for the real safety time-delay

// actuate runs the occupancy-driven optimizer for every live (instrumented) zone: a zone
// that has been empty past the safety delay is set back (warmer setpoint, which lowers
// cooling load and shows up as a drop on the dashboard) and its lights are commanded off;
// a reoccupied zone is restored. Commands publish to the edge (ESP32) only on change.
func (e *Engine) actuate() {
	for id, z := range e.Zones {
		if !z.Live {
			continue
		}
		if time.Now().Before(z.OverrideUntil) {
			continue // Respect the human-in-the-loop manual override latch
		}
		if z.Occupancy <= 0 {
			z.VacantTicks++
		} else {
			z.VacantTicks = 0
		}
		vacant := z.Occupancy <= 0 && z.VacantTicks >= vacancyDelayTicks

		desiredLights := !vacant
		desiredSp := z.BaseSetpoint
		if vacant {
			desiredSp = z.BaseSetpoint + 4.0 // energy-saving setback
		}
		z.Setpoint = desiredSp
		z.LightsOn = desiredLights

		lightStr := "OFF"
		if desiredLights {
			lightStr = "ON"
		}
		cmd := fmt.Sprintf("LIGHTS_%s;SETPOINT=%.1f", lightStr, desiredSp)
		if e.lastCmd[id] != cmd {
			e.lastCmd[id] = cmd
			topic := z.MqttTopic
			if topic == "" {
				topic = id
			}
			log.Printf("[actuate] zone=%s occ=%d -> %s", id, z.Occupancy, cmd)
			if e.Publish != nil {
				e.Publish("econ/commands/"+topic, cmd)
			}
		}
	}
}

// hwStaleAfter bounds how long a measured temperature keeps pinning a zone: past it the
// node is presumed unplugged and the 2R1C model takes back over. Nodes publish every
// 2–5 s, so 20 s tolerates a few dropped messages without flapping.
const hwStaleAfter = 20 * time.Second

// hwFresh reports whether this zone is currently pinned to a live measured temperature.
func (z *ZoneSim) hwFresh() bool {
	return !z.HwTempAt.IsZero() && time.Since(z.HwTempAt) < hwStaleAfter
}

// applyHardware pulls every hardware-bound zone's air temperature toward the physical
// sensor reading — a fast exponential blend (~1 s at 30 FPS) rather than a hard jump,
// so the dashboard never teleports. The thermal model keeps integrating underneath and
// resumes control the moment telemetry goes stale, so unplugging a node degrades
// gracefully back to simulation. Lock held.
func (e *Engine) applyHardware() {
	for _, z := range e.Zones {
		if !z.hwFresh() {
			continue
		}
		z.Temp += (z.HwTemp - z.Temp) * 0.1
	}
}

// SetNodeStatus records the broker's Last-Will verdict for an edge node
// (econ/status/<topic> -> "online"/"offline"). An offline node stops pinning its zone
// immediately instead of waiting out the staleness window. Any transition also clears
// the zone's command-dedupe entry: a node that reboots comes back in its firmware
// default state, so the optimizer must re-send the current command even if it is
// unchanged from the engine's point of view.
func (e *Engine) SetNodeStatus(topicSuffix string, online bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, z := range e.Zones {
		if z.MqttTopic != topicSuffix {
			continue
		}
		z.HwOnline = online
		delete(e.lastCmd, id)
		if !online {
			z.HwTempAt = time.Time{}
		}
	}
}

// HardwareNode is one physical edge-node binding as reported by GET /api/hardware.
type HardwareNode struct {
	ZoneId     string  `json:"zoneId"`
	Topic      string  `json:"topic"`
	Source     string  `json:"source"`
	Online     bool    `json:"online"`
	TempPinned bool    `json:"tempPinned"`
	Occupancy  int     `json:"occupancy"`
	ZoneTemp   float64 `json:"zoneTemp"`
	HwTemp     float64 `json:"hwTemp"`
	Humidity   float64 `json:"humidity"`
	Co2        float64 `json:"co2"`
	LightsOn   bool    `json:"lightsOn"`
	Setpoint   float64 `json:"setpoint"`
	AgeSec     float64 `json:"ageSec"`
}

// HardwareStatus snapshots every zone currently bound to a physical edge node, for the
// dashboard's live-hardware indicators.
func (e *Engine) HardwareStatus() []HardwareNode {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []HardwareNode{}
	for id, z := range e.Zones {
		if z.HwSeenAt.IsZero() {
			continue
		}
		age := time.Since(z.HwSeenAt).Seconds()
		out = append(out, HardwareNode{
			ZoneId:     id,
			Topic:      z.MqttTopic,
			Source:     z.HwSource,
			Online:     z.HwOnline && age < 60,
			TempPinned: z.hwFresh(),
			Occupancy:  z.Occupancy,
			ZoneTemp:   z.Temp,
			HwTemp:     z.HwTemp,
			Humidity:   z.HwHum,
			Co2:        z.HwCo2,
			LightsOn:   z.LightsOn,
			Setpoint:   z.Setpoint,
			AgeSec:     age,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ZoneId < out[j].ZoneId })
	return out
}

func (e *Engine) AddClient(conn *websocket.Conn) {
	e.mu.Lock()
	e.Clients[conn] = true
	e.mu.Unlock()
}

func (e *Engine) SetScenario(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(s) > 6 && s[:6] == "fault:" {
		e.Scenario = "fault"
		e.FaultTarget = s[6:]
	} else {
		e.Scenario = s
	}

	for _, v := range e.Vavs {
		z := e.Zones[v.TargetZone]
		// Modulate VAV
		errorSignal := z.Temp - z.Setpoint
		if errorSignal > z.Deadband/2 {
			v.Resistance -= 0.05
		} else if errorSignal < -z.Deadband/2 {
			v.Resistance += 0.05
		}

		if e.Scenario == "fault" && v.TargetZone == e.FaultTarget {
			v.Resistance = 50.0 // Damper stuck closed
		} else if e.Scenario == "remediating" && (v.TargetZone == e.FaultTarget || z.Type == "core") {
			v.Resistance = 0.01 // Maximum flow to faulty zone and core
		}
		
		if v.Resistance < 0.01 { v.Resistance = 0.01 }
		if v.Resistance > 100.0 { v.Resistance = 100.0 }
	}
	e.doHardyCross()
}

func (e *Engine) RemoveClient(conn *websocket.Conn) {
	e.mu.Lock()
	delete(e.Clients, conn)
	e.mu.Unlock()
}

func getNoise(std float64) float64 {
	u, v := 0.0, 0.0
	for u == 0 { u = rand.Float64() }
	for v == 0 { v = rand.Float64() }
	return math.Sqrt(-2.0*math.Log(u)) * math.Cos(2.0*math.Pi*v) * std
}

func (e *Engine) Start() {
	ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS

	for range ticker.C {
		dt := 0.033
		e.mu.Lock()
		if e.Scenario == "fault" {
			dt = 0.3 // Accelerate heating
		} else if e.Scenario == "remediating" {
			dt = 0.6 // Super-accelerate cooling
		} else {
			// Peak Load Scenario: If the building is out of equilibrium (e.g. after a fault),
			// dynamically accelerate time so the user can watch it physically recover back
			// to stable green states quickly, without getting stuck in a thermal limbo!
			maxDev := 0.0
			for _, z := range e.Zones {
				if z.hwFresh() {
					continue // pinned to a live sensor: deviation is reality, not "recovering"
				}
				sp := 24.0
				if z.Type == "server-room" { sp = 22.0 }
				if dev := math.Abs(z.Temp - sp); dev > maxDev {
					maxDev = dev
				}
			}
			if maxDev > 1.0 {
				dt = 2.0 // 60x speed recovery!
			}
		}
		e.mu.Unlock()

		e.tick(dt)

		// Occupancy-driven optimizer + edge actuation (publishes only on state change).
		e.mu.Lock()
		e.actuate()
		e.applyHardware()
		e.mu.Unlock()

		e.broadcast()
	}
}
func (e *Engine) tick(dt float64) {
		// Thermodynamics
		for _, v := range e.Vavs {
			z, ok := e.Zones[v.TargetZone]
			if !ok {
				continue
			}

			// Nominal (non-fault) internal load: base equipment + people + solar.
			qSolar := z.SolarGainMult * 10000.0
			qInternalNominal := z.BaseHeatGain + (float64(z.Occupancy) * 100.0) + qSolar

			qInternal := qInternalNominal
			if e.Scenario == "fault" && v.TargetZone == e.FaultTarget {
				qInternal *= 5.0 // Thermal runaway strictly on selected fault target
			}

			sp := z.Setpoint
			if sp == 0 {
				sp = 24.0
			}

			tOutside := 30.0

			// Size cooling so that at the VAV's NOMINAL flow the room holds setpoint:
			// qCooling(Temp=sp, flow=nominal) must offset the full nominal internal
			// load plus steady-state wall conduction. Normalizing by the VAV's own
			// nominal flow (not a hard-coded 5.4 m3/s) keeps this correct no matter
			// how many VAVs share the AHU.
			qSteadyStateWall := (tOutside - sp) / (z.RIn + z.ROut)
			qNominalTotal := qInternalNominal + qSteadyStateWall

			nominalFlow := v.NominalFlow
			if nominalFlow < 1e-6 {
				nominalFlow = v.Flow
			}
			if nominalFlow < 1e-6 {
				nominalFlow = 1.0
			}
			flowRatio := v.Flow / nominalFlow

			qCooling := flowRatio * qNominalTotal * ((z.Temp - 12.0) / (sp - 12.0))
			if qCooling < 0 { qCooling = 0 } // Cannot heat with cold air

			dTAirDt := ((z.WallTemp-z.Temp)/(z.RIn*z.CAir) + (qInternal-qCooling)/z.CAir)
			dTWallDt := ((tOutside-z.WallTemp)/(z.ROut*z.CWall) - (z.WallTemp-z.Temp)/(z.RIn*z.CWall))

			z.Temp += dTAirDt * dt
			z.WallTemp += dTWallDt * dt

			// Clamp to physically plausible bounds. Guards against numerical runaway when a
			// (possibly mis-digitized) zone pairs a tiny CAir with a large heat load — without
			// this, such a zone integrates to absurd temperatures (e.g. 200+°C) instead of
			// just reading "hot / cooling-starved" like a real failing room.
			z.Temp = math.Max(5.0, math.Min(50.0, z.Temp))
			z.WallTemp = math.Max(5.0, math.Min(50.0, z.WallTemp))
		}
}

// ForecastWindow builds the [room_temp(°C), airflow_fraction(0..1)] sequence the Python
// forecaster expects, from the current zone/VAV state. Airflow is normalized to a fraction of
// each VAV's nominal flow so it matches the model's training scale (the engine's raw m³/s would
// be far out of distribution). The engine keeps no telemetry history yet, so the current
// building-average conditions are replicated across `seqLen` steps.
func (e *Engine) ForecastWindow(seqLen int) [][]float64 {
	e.mu.Lock()
	defer e.mu.Unlock()

	tempSum := 0.0
	for _, z := range e.Zones {
		tempSum += z.Temp
	}
	flowSum := 0.0
	for _, v := range e.Vavs {
		frac := 0.0
		if v.NominalFlow > 1e-6 {
			frac = v.Flow / v.NominalFlow
		}
		flowSum += math.Max(0, math.Min(1, frac))
	}

	avgTemp := 24.0
	if len(e.Zones) > 0 {
		avgTemp = tempSum / float64(len(e.Zones))
	}
	avgFlow := 0.5
	if len(e.Vavs) > 0 {
		avgFlow = flowSum / float64(len(e.Vavs))
	}

	seq := make([][]float64, seqLen)
	for i := range seq {
		seq[i] = []float64{avgTemp, avgFlow}
	}
	return seq
}

func (e *Engine) broadcast() {
		// ---- Live global metrics (all derived from current zone state) ----
		totalHeatW := 0.0    // total thermal load the plant must remove (W)
		totalOccupants := 0
		comfortSum := 0.0      // Σ per-zone thermal-comfort score (report §4.5 discomfort model)
		strainSum := 0.0       // sum of how far zones sit above setpoint (drives plant COP)
		savedLightingW := 0.0  // lighting cut on vacant (set-back) zones
		savedThermalW := 0.0   // cooling demand avoided on vacant (set-back) zones
		// Half-comfort point: a zone this many °C *beyond* its deadband scores 0.5 comfort.
		const sigmaComfort2 = 2.5 * 2.5
		for id, z := range e.Zones {
			qSolar := z.SolarGainMult * 10000.0
			qi := z.BaseHeatGain + float64(z.Occupancy)*100.0 + qSolar
			if e.Scenario == "fault" && id == e.FaultTarget {
				qi *= 5.0
			}
			totalHeatW += qi
			totalOccupants += z.Occupancy
			sp := z.Setpoint
			if sp == 0 {
				sp = 24.0
			}
			strainSum += math.Max(0, z.Temp-sp)
			// Report §4.5 thermal-discomfort term — excess beyond the deadband penalized
			// quadratically (max(0,|T-Tset|-δ))² — mapped to a bounded [0,1] comfort score.
			// This grades health by *severity* (a 0.1°C overshoot ≈ healthy; a runaway ≈ 0)
			// instead of the old binary in-band / out-of-band flag.
			excess := math.Max(0, math.Abs(z.Temp-sp)-z.Deadband)
			comfortSum += 1.0 / (1.0 + (excess*excess)/sigmaComfort2)
			// Occupancy-driven savings: a live zone in setback (lights off) avoids its
			// lighting load and a chunk of its internal-gain cooling.
			if z.Live && z.Setpoint > z.BaseSetpoint+0.01 {
				savedLightingW += 2000.0
				savedThermalW += z.BaseHeatGain * 0.25
			}
		}

		// Plant coefficient of performance degrades as the building is strained (chillers
		// run harder at higher lift), so efficiency, cooling, and load are all coupled.
		avgStrain := 0.0
		if len(e.Zones) > 0 {
			avgStrain = strainSum / float64(len(e.Zones))
		}
		plantCop := math.Max(2.2, math.Min(3.8, 3.6-0.35*avgStrain))

		coolingOutputMW := totalHeatW / 1e6      // thermal cooling delivered (MW)
		coolingElectricalMW := coolingOutputMW / plantCop
		const baseElectricalMW = 2.0             // lighting + plug + fans baseline
		buildingLoadMW := coolingElectricalMW + baseElectricalMW
		energySavedMW := (savedLightingW + savedThermalW/plantCop) / 1e6

		// System health = mean per-zone comfort (severity-weighted), per the report's discomfort
		// model. Bounded [0,100]; the discrete "active critical faults" count handles alarms.
		systemHealth := 100.0
		if len(e.Zones) > 0 {
			systemHealth = 100.0 * comfortSum / float64(len(e.Zones))
		}

		// [GEMINI IMPLEMENTATION START]
		// Persist metrics to TimescaleDB at most once per second. persistReading
		// (db.go) only enqueues, so this never blocks the broadcast goroutine.
		now := time.Now()
		if e.Persist != nil && now.Sub(e.lastDbSave) > time.Second {
			e.lastDbSave = now
			e.Persist("GLOBAL", "buildingLoadMw", buildingLoadMW)
			e.Persist("GLOBAL", "coolingOutputMw", coolingOutputMW)
			e.Persist("GLOBAL", "systemHealth", systemHealth)
			avgCo2 := 400.0 + float64(totalOccupants)*0.85
			e.Persist("GLOBAL", "avgCo2", avgCo2)
			for id, z := range e.Zones {
				e.Persist(id, "temp", z.Temp)
				e.Persist(id, "occupancy", float64(z.Occupancy))
			}
		}
		// [GEMINI IMPLEMENTATION END]

		// FlatBuffers Serialization
		builder := flatbuffers.NewBuilder(1024)

		// Create Zones
		zoneOffsets := make([]flatbuffers.UOffsetT, 0)
		for id, z := range e.Zones {
			noiseTemp := z.Temp + getNoise(0.08)
			if math.Abs(noiseTemp-z.LastBroadcastTemp) > 0.05 {
				z.LastBroadcastTemp = noiseTemp
				idStr := builder.CreateString(id)
				Telemetry.ZoneDataStart(builder)
				Telemetry.ZoneDataAddId(builder, idStr)
				Telemetry.ZoneDataAddTemp(builder, float32(noiseTemp))
				Telemetry.ZoneDataAddOccupants(builder, int32(z.Occupancy))
				Telemetry.ZoneDataAddLoad(builder, float32(z.BaseHeatGain/1000.0))
				zoneOffsets = append(zoneOffsets, Telemetry.ZoneDataEnd(builder))
			}
		}
		Telemetry.SimStateStartZonesVector(builder, len(zoneOffsets))
		for i := len(zoneOffsets) - 1; i >= 0; i-- {
			builder.PrependUOffsetT(zoneOffsets[i])
		}
		zonesVec := builder.EndVector(len(zoneOffsets))

		// Create VAVs
		vavOffsets := make([]flatbuffers.UOffsetT, 0)
		for id, v := range e.Vavs {
			noiseFlow := math.Max(0, v.Flow+getNoise(0.2))
			if math.Abs(noiseFlow-v.LastBroadcastFlow) > 0.1 {
				v.LastBroadcastFlow = noiseFlow
				idStr := builder.CreateString(id)
				Telemetry.VavDataStart(builder)
				Telemetry.VavDataAddId(builder, idStr)
				Telemetry.VavDataAddAirflow(builder, float32(noiseFlow))
				vavOffsets = append(vavOffsets, Telemetry.VavDataEnd(builder))
			}
		}
		Telemetry.SimStateStartVavsVector(builder, len(vavOffsets))
		for i := len(vavOffsets) - 1; i >= 0; i-- {
			builder.PrependUOffsetT(vavOffsets[i])
		}
		vavsVec := builder.EndVector(len(vavOffsets))

		// Create Global
		Telemetry.GlobalDataStart(builder)
		Telemetry.GlobalDataAddBuildingLoadMw(builder, float32(buildingLoadMW))
		Telemetry.GlobalDataAddSystemHealth(builder, float32(systemHealth))
		Telemetry.GlobalDataAddTotalOccupants(builder, int32(totalOccupants))
		Telemetry.GlobalDataAddCoolingOutputMw(builder, float32(coolingOutputMW))
		Telemetry.GlobalDataAddPlantCop(builder, float32(plantCop))
		Telemetry.GlobalDataAddEnergySavedMw(builder, float32(energySavedMW))
		globalPos := Telemetry.GlobalDataEnd(builder)

		// Build SimState
		Telemetry.SimStateStart(builder)
		Telemetry.SimStateAddTimestamp(builder, time.Now().UnixMilli())
		Telemetry.SimStateAddZones(builder, zonesVec)
		Telemetry.SimStateAddVavs(builder, vavsVec)
		Telemetry.SimStateAddGlobal(builder, globalPos)
		simStatePos := Telemetry.SimStateEnd(builder)

		builder.Finish(simStatePos)
		buf := builder.FinishedBytes()

		e.mu.Lock()
		for client := range e.Clients {
			err := client.WriteMessage(websocket.BinaryMessage, buf)
			if err != nil {
				client.Close()
				delete(e.Clients, client)
			}
		}
		e.mu.Unlock()
	}

// [GEMINI IMPLEMENTATION START]
// PublishCommand dispatches a manual override directly to the edge IoT device,
// bypassing the autonomous optimizer (the "human-in-the-loop" veto). The action is
// normalized to a firmware-valid payload before publishing so the ESP32 (which only
// parses LIGHTS_ON|OFF / SETPOINT= / HVAC_SET:) always gets something it can actuate,
// regardless of which UI panel issued it. The override is transient: the occupancy
// optimizer reasserts control on the next tick.
func (e *Engine) PublishCommand(action, zoneRef string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	z := e.resolveZone(zoneRef)
	topic := zoneRef
	if z != nil {
		if z.MqttTopic != "" {
			topic = z.MqttTopic
		}
		// Set a 15-minute latch so the optimizer respects the human veto
		z.OverrideUntil = time.Now().Add(15 * time.Minute)
	}

	cmd := normalizeOverride(action, z)
	log.Printf("[override] manual command %q (from %q) to %s (latched 15m)", cmd, action, topic)
	if e.Publish != nil {
		e.Publish("econ/commands/"+topic, cmd)
	}
}

// normalizeOverride maps the dashboard's high-level override verbs to the
// LIGHTS_x;SETPOINT=y wire format the firmware and optimizer share. Payloads already
// in that format (e.g. "LIGHTS_OFF;SETPOINT=26.0") pass through unchanged.
func normalizeOverride(action string, z *ZoneSim) string {
	a := strings.TrimSpace(action)
	upper := strings.ToUpper(a)
	if strings.HasPrefix(upper, "LIGHTS_") || strings.HasPrefix(upper, "SETPOINT=") || strings.HasPrefix(upper, "HVAC_SET:") {
		return a // already a firmware command
	}

	switch strings.ToLower(a) {
	case "purge": // emergency air flush: lights off, drive cooling hard
		return "LIGHTS_OFF;SETPOINT=18.0"
	case "cool": // max cool while occupied
		return "LIGHTS_ON;SETPOINT=20.0"
	case "reset": // hand back to the zone's nominal occupied setpoint
		sp := 24.0
		if z != nil {
			sp = z.BaseSetpoint
		}
		return fmt.Sprintf("LIGHTS_ON;SETPOINT=%.1f", sp)
	default:
		return a // unknown verb: forward verbatim; firmware ignores tokens it can't parse
	}
}
// [GEMINI IMPLEMENTATION END]
