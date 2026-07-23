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
	"strconv"
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
	Polygon           [][]float64  `json:"polygon"` // floor outline in metres; sizes the plug model
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
	Temp                float64
	WallTemp            float64
	Type                string
	BimAssetId          string
	AreaM2              float64 // real floor area (digitized polygon); sizes plug + lighting models
	Occupancy           int
	BaseHeatGain        float64
	SolarGainMult       float64
	CAir                float64
	CWall               float64
	RIn                 float64
	ROut                float64
	Setpoint            float64
	BaseSetpoint        float64 // occupied setpoint; we set back from this when vacant
	Deadband            float64
	LastBroadcastTemp   float64
	LastBroadcastLights bool // last lighting state sent to clients (change forces a re-send)
	// Occupancy-driven control (real data arrives over MQTT from the CV/edge layer)
	Live          bool      // true once real occupancy has been received for this zone
	VacantTicks   int       // consecutive ticks at 0 occupancy (safety delay before setback)
	LightsOn      bool      // last actuated lighting state
	MqttTopic     string    // telemetry suffix this zone was seen on (commands route back here)
	OverrideUntil time.Time // Latch manual overrides so optimizer doesn't overwrite
	// Hardware-in-the-loop (physical ESP32 / Pico nodes). While the bound node's
	// measured temperature is fresh, the zone's air temp is pulled to the measurement
	// instead of the 2R1C integration — the dashboard shows the physical room, not the
	// model. Simulated placeholder temps never set HwTempAt (see Measurement.TempReal).
	HwSource string    // node kind ("esp32", "pico", "cv", ...); empty = never bound
	HwSeenAt time.Time // last telemetry of any kind from the bound node
	HwTemp   float64   // last measured air temperature (valid only while HwTempAt is fresh)
	HwTempAt time.Time // when HwTemp arrived; zero = node never sent a real temperature
	HwHum    float64   // last measured relative humidity (%), valid while HwHumAt is fresh
	HwHumAt  time.Time // when HwHum arrived; zero = no humidity sensor has ever reported
	HwCo2    float64   // last measured CO2 (ppm), valid while HwCo2At is fresh
	// Measured replacements for values the model otherwise assumes.
	HwSupplyC  float64   // AC discharge temperature (DS18B20) — supersedes supplyAirDesignC
	HwSupplyAt time.Time
	HwAcW      float64   // air-conditioner power (SCT-013) — the real cooling drive term
	HwAcAt     time.Time
	HwLux      float64   // ambient illuminance (BH1750) — a real irradiance proxy
	HwLuxAt    time.Time
	HwCo2At  time.Time // when HwCo2 arrived; zero = no NDIR sensor has ever reported
	HwOnline bool      // broker LWT verdict from econ/status/<topic>
	// Automated Plug Load Control (see plugs.go). PlugStandbyW is sized from the zone's
	// real floor area at build time; PlugShed flips when the after-hours sweep cuts the
	// zone's switchable sockets. HwPlugW/HwPlugAt carry a live SCT-013 clamp reading,
	// under the same per-field freshness rules as every other sensor.
	PlugStandbyW          float64
	PlugShed              bool
	PlugVacantSince       time.Time // zero while occupied; set at the moment occupancy hits 0
	HwPlugW               float64
	HwPlugAt              time.Time
	LastBroadcastPlugShed bool
	// Physics-grounded AFDD (roadmap challenge 2): while a real sensor pins this zone,
	// ShadowTemp keeps integrating the pure 2R1C model with NO sensor pull. The smoothed
	// |measured − modeled| residual is the fault signal — a healthy room tracks its
	// physics, a faulty one (blocked coil, stuck damper, open window) diverges. Needs no
	// training data or fault labels.
	ShadowTemp  float64 // sensor-free model twin of Temp (0 = not yet seeded)
	ResidualEma float64 // smoothed |HwTemp − ShadowTemp| in °C
	// Does this zone's bound node actually drive an air conditioner? The optimizer will
	// happily compute a setback for a zone whose commands terminate in a serial log line;
	// this is what lets the twin distinguish a saving it caused from one it only imagined.
	HwAcReal     bool
	HwAcRealSeen bool // a node has positively reported either way
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
	// Forecast-driven pre-cooling: while now < PreCoolUntil the optimizer drives every
	// occupied zone below its base setpoint, charging the building's thermal mass ahead
	// of a predicted demand peak so the chillers can shed load when it lands.
	PreCoolUntil time.Time
	// Battery Energy Storage System: charges off-peak, discharges on peak to shave grid draw.
	Bess       Battery
	lastLoadMw float64   // latest computed building electrical load (MW), fed to BESS dispatch
	lastBessAt time.Time // wall-clock of the last BESS integration step
	// Automated Plug Load Control (plugs.go): sweep policy, cumulative avoided energy,
	// and the wall-clock anchor its integration runs on (sim time accelerates; savings
	// must not).
	Plug         PlugConfig
	plugSavedKwh float64
	lastPlugAt   time.Time
	// Autonomous optimizer. AutoPilot is the real control flag the dashboard toggles
	// (over the websocket, main.go): when false, actuate() is suspended and the operator
	// owns the setpoints. zonesInSetback is how many zones it is currently holding in
	// energy-saving setback — the genuine backing for the "autonomous action" UI.
	AutoPilot      bool
	zonesInSetback int
	// Live outdoor conditions from the weather poller (main.go). Same freshness
	// philosophy as the zone sensors: outdoorAt records when the value arrived, and a
	// value the poller has not refreshed within outdoorStaleAfter stops driving the
	// envelope — the physics falls back to climatology rather than integrating against
	// a reading from hours ago as if it were current. Humidity rides along for the
	// LSTM forecaster, which was trained on [.., outdoor_temp, outdoor_humidity].
	outdoorTemp float64
	outdoorHum  float64
	outdoorAt   time.Time
	// Rolling telemetry window for the LSTM forecaster: one [avg room temp, avg airflow
	// fraction] sample every histInterval, most recent last. This is what makes the
	// forecast input a REAL last-hour sequence — including hardware-pinned temperatures
	// — instead of the current instant photocopied twelve times.
	histBuf    []histSample
	lastHistAt time.Time
	// Rolling building-load history (MW) at the same histInterval cadence. The LSTM is fed
	// a short [temp, airflow] window because that is what it was trained on; a time-series
	// FOUNDATION model (Google TimesFM, backend/forecasting/timesfm_forecaster.py) instead
	// forecasts the load series directly and zero-shot, so it wants as much of the real
	// series as we can give it. loadHistKeep spans ~42 h, which covers the daily cycle the
	// forecast is really about.
	loadHist []float64
	// Learned operating baselines (baselines.go): the online per-(zone,metric,hour) model
	// that turns the telemetry stream into "normal for this room at this hour", replacing
	// hardcoded thresholds. Feeds both /api/recommendations and the data-driven pre-cool
	// trigger. Self-locked, so it is folded into on the tick and read on the HTTP path
	// without coupling to e.mu.
	baselines      *Baselines
	lastBaselineAt time.Time
	// Learned per-room dynamics (dynamics.go): the online system identification that gives
	// each room a physical identity — its own thermal time constant, the cooling authority
	// its VAV actually delivers, and its measured air-change rate. This is what lets the
	// twin predict where a room is heading instead of only reporting where it is. Self-
	// locked like the baselines (e.mu → dynamics.mu).
	dynamics *Dynamics
	// simClock is the accumulated SIMULATION time in seconds. The tick's dt is deliberately
	// accelerated during faults and recovery, so wall-clock is the wrong time base for
	// anything learning the physics: identifying on this clock keeps every learned time
	// constant in one consistent timescale.
	simClock float64
}

// histSample is one forecaster timestep: building-average room temperature and
// building-average airflow fraction (0..1 of nominal), captured together.
type histSample struct {
	temp float64
	flow float64
}

const (
	// histInterval MUST match the cadence the LSTM was trained at (SEQ_LEN=12 steps of
	// 5 minutes — the last hour). Changing one without the other feeds the model a
	// sequence at a timescale it has never seen.
	histInterval = 5 * time.Minute
	histKeep     = 12
	// loadHistKeep is how many load samples to retain for the foundation-model forecaster:
	// 512 × 5 min ≈ 42 hours, enough context for it to see the building's daily shape.
	loadHistKeep = 512
)

func NewEngine() *Engine {
	e := &Engine{
		Clients:    make(map[*websocket.Conn]bool),
		Zones:      make(map[string]*ZoneSim),
		Vavs:       make(map[string]*VavSim),
		PMax:       600.0,
		KFan:       0.01,
		Scenario:   "peak",
		lastCmd:    make(map[string]string),
		demoAssign: make(map[string]string),
		Bess:       NewBattery(),
		lastBessAt: time.Now(),
		Plug:       defaultPlugConfig(),
		lastPlugAt: time.Now(),
		AutoPilot:  true,
		baselines:  NewBaselines(),
		dynamics:   NewDynamics(),
	}

	data, err := os.ReadFile("./data/building-data.json")
	if err != nil {
		log.Printf("Failed to load building data: %v", err)
		return e
	}
	if err := e.buildFromJSON(data); err != nil {
		log.Printf("Failed to parse building data: %v", err)
	}
	return e
}

// buildFromJSON populates Zones/Vavs from a building-data.json payload. It is the single
// construction path: NewEngine uses it at boot and ReloadBuilding uses it when a freshly
// digitized blueprint is deployed. Not locked — callers own the locking discipline.
func (e *Engine) buildFromJSON(data []byte) error {
	var bd BuildingData
	if err := json.Unmarshal(data, &bd); err != nil {
		return err
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

			// A zone with no setpoint in the fixture takes its programme's setpoint from
			// the library, not a guess keyed off one hardcoded zone type.
			temp := z.ThermalProperties.Setpoint
			if temp == 0 {
				temp = 24.0
				if prog, ok := ProgrammeFor(z.ZoneType); ok && prog.SetpointC > 0 {
					temp = prog.SetpointC
				}
			}

			baseSp := z.ThermalProperties.Setpoint
			if baseSp == 0 {
				baseSp = temp
			}
			// Plug standby scales with real floor area (shoelace over the digitized
			// polygon; volume/3m where only a volume exists; a small default otherwise).
			areaM2 := polygonAreaM2(z.Polygon)
			if areaM2 <= 0 && z.Volume > 0 {
				areaM2 = z.Volume / 3.0
			}
			if areaM2 <= 0 {
				areaM2 = plugDefaultAreaM2
			}
			e.Zones[z.ZoneId] = &ZoneSim{
				Temp:          temp,
				WallTemp:      temp,
				Type:          z.ZoneType,
				BimAssetId:    z.BimAssetId,
				Occupancy:     rand.Intn(10),
				BaseHeatGain:  z.ThermalProperties.BaseHeatLoad,
				SolarGainMult: z.ThermalProperties.SolarGainMultiplier,
				// Floor CAir: some digitized zones (e.g. tiny "server rooms") carry an
				// unrealistically small air capacitance that makes the explicit-Euler thermal
				// integration unstable (runaway temps). A modest floor keeps it stable; steady
				// state is unaffected since it depends on the heat balance, not CAir.
				CAir:                math.Max(z.ThermalProperties.CAir, Phys().MinZoneCapacitanceJPerK),
				CWall:               4000000.0,
				RIn:                 z.ThermalProperties.RWall / 2,
				ROut:                z.ThermalProperties.RWall/2 + 0.1,
				Setpoint:            z.ThermalProperties.Setpoint,
				BaseSetpoint:        baseSp,
				Deadband:            z.ThermalProperties.Deadband,
				LastBroadcastTemp:   24.0,
				LightsOn:            true,
				LastBroadcastLights: true,
				AreaM2:              areaM2,
				PlugStandbyW:        areaM2 * plugStandbyWPerM2,
			}
		}
	}

	e.doHardyCross()
	// Capture each VAV's nominal flow (at default resistance) so the cooling
	// model can be normalized to it regardless of how many VAVs share the AHU.
	for _, v := range e.Vavs {
		v.NominalFlow = v.Flow
	}
	return nil
}

// ReloadBuilding swaps the running twin onto a new building — the deploy step of the
// blueprint import flow. The physics loop, MQTT ingestion and HTTP snapshots all touch
// zone state under e.mu, so the swap happens in one critical section: clients simply see
// one tick of the old building followed by one tick of the new one. Everything keyed to
// the old zones (edge-node demo bindings, actuation dedupe, fault target, pre-cool
// window) is dropped rather than remapped — a binding onto a zone that no longer exists
// is a lie, and the node will re-bind on its next telemetry.
func (e *Engine) ReloadBuilding(data []byte) error {
	// Validate on scratch state first so a malformed upload cannot leave a half-built twin.
	scratch := &Engine{Zones: map[string]*ZoneSim{}, Vavs: map[string]*VavSim{}, PMax: 600.0, KFan: 0.01}
	if err := scratch.buildFromJSON(data); err != nil {
		return err
	}
	if len(scratch.Zones) == 0 {
		return fmt.Errorf("blueprint produced zero zones")
	}
	// Structural sanity a professional deployment needs stated, not assumed:
	// duplicate zoneIds would silently last-write-win in the map (two rooms, one
	// simulated), and an absurd zone count is a corrupt or hostile payload, not a
	// building — the physics loop is O(zones) at 30 Hz.
	var declared struct {
		Floors []struct {
			Zones []struct {
				ZoneId string `json:"zoneId"`
			} `json:"zones"`
		} `json:"floors"`
	}
	if err := json.Unmarshal(data, &declared); err == nil {
		n := 0
		for _, f := range declared.Floors {
			n += len(f.Zones)
		}
		if n > 50000 {
			return fmt.Errorf("%d zones exceeds the 50,000-zone limit", n)
		}
		if n != len(scratch.Zones) {
			return fmt.Errorf("%d zones declared but only %d unique zoneIds — duplicate ids in the payload", n, len(scratch.Zones))
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.Zones = map[string]*ZoneSim{}
	e.Vavs = map[string]*VavSim{}
	e.lastCmd = map[string]string{}
	e.demoAssign = map[string]string{}
	e.FaultTarget = ""
	e.Scenario = "peak"
	e.PreCoolUntil = time.Time{}
	if err := e.buildFromJSON(data); err != nil {
		return err // unreachable in practice: scratch already parsed this payload
	}
	log.Printf("[building] reloaded: %d zones, %d VAVs", len(e.Zones), len(e.Vavs))
	return nil
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
	SupplyC   *float64
	AcW       *float64
	Lux       *float64
	PlugW     *float64 // measured plug-circuit draw (SCT-013 clamp), watts
	Source    string
	TempReal  bool
	// AcReal reports whether the node's setpoint commands actually reach an air
	// conditioner, or are merely parsed and logged. A pointer because "not reported"
	// (firmware older than the field) must stay distinct from a node that positively
	// says its IR control is absent.
	AcReal *bool
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
	if m.AcReal != nil {
		// Log the transition only, not every 5 s telemetry cycle.
		if !z.HwAcRealSeen || z.HwAcReal != *m.AcReal {
			if *m.AcReal {
				log.Printf("[actuate] zone=%s has REAL AC control (setpoints reach the unit)", z.BimAssetId)
			} else {
				log.Printf("[actuate] zone=%s reports NO AC control: setpoint commands are parsed but reach no machine, so any saving attributed to its setback is not real", z.BimAssetId)
			}
		}
		z.HwAcReal, z.HwAcRealSeen = *m.AcReal, true
	}
	if m.Temp != nil && m.TempReal {
		z.HwTemp = *m.Temp
		z.HwTempAt = time.Now()
		if z.ShadowTemp == 0 {
			// First real measurement: seed the shadow model at reality so the AFDD
			// residual starts near zero and only grows on genuine divergence.
			z.ShadowTemp = *m.Temp
		}
	}
	// Each environmental field carries its own arrival time. A node reports per-sensor:
	// the SHT30 can keep answering while the NDIR fails its CRC or is unplugged, and the
	// firmware then omits only that field. Timestamping the node as a whole would let the
	// last CO2 reading keep streaming as "measured" for as long as the board still sent
	// temperature.
	if m.Humidity != nil {
		z.HwHum = *m.Humidity
		z.HwHumAt = time.Now()
	}
	if m.Co2 != nil {
		z.HwCo2 = *m.Co2
		z.HwCo2At = time.Now()
	}
	if m.PlugW != nil {
		z.HwPlugW = *m.PlugW
		z.HwPlugAt = time.Now()
	}
	// Measurements that displace an assumption in the model. Each carries its own
	// arrival time for the same reason every other field does: a probe that falls out of
	// the louvre must stop being believed without taking the rest of the node with it.
	if m.SupplyC != nil {
		z.HwSupplyC = *m.SupplyC
		z.HwSupplyAt = time.Now()
	}
	if m.AcW != nil {
		z.HwAcW = *m.AcW
		z.HwAcAt = time.Now()
	}
	if m.Lux != nil {
		z.HwLux = *m.Lux
		z.HwLuxAt = time.Now()
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

const (
	// preCoolDelta is how far below the occupied setpoint zones run during a
	// forecast-triggered pre-cool window.
	preCoolDelta = 1.5 // °C
	// fixedSetbackC is the conventional vacancy setback, used only until a room has been
	// identified. It used to be applied unconditionally to every zone; the depth is now
	// asked of each room's own thermal model (Dynamics.SetbackCeiling), because the right
	// answer depends on how fast that specific room recovers.
	fixedSetbackC = 4.0 // °C
	// setbackRecoverySecs is the budget a setback room is given to return to setpoint once
	// occupancy comes back — the constraint the learned depth is solved against.
	setbackRecoverySecs = 1800.0 // 30 min
	// afddThreshold flags a zone whose measured temperature has drifted this far
	// (smoothed) from its sensor-free 2R1C shadow model.
	afddThreshold = 2.0 // °C
)

// actuate runs the occupancy-driven optimizer: a zone empty past the safety delay is set
// back (warmer setpoint, which lowers cooling load and shows up as a drop on the
// dashboard) and its lights are commanded off; a reoccupied zone is restored.
//
// Two deliberate design points:
//   - AutoPilot gates the whole thing. When it is off the optimizer is genuinely
//     suspended — setpoints hold wherever they are (manual or last-commanded) and
//     nothing autonomous is claimed. This is what the dashboard's AI toggle now
//     actually controls, end to end, instead of only re-labelling cards.
//   - It acts on EVERY zone, not just hardware-bound ones, so the twin's autonomy is
//     real in the model (physics + streamed savings reflect it). MQTT commands are
//     published ONLY to zones with a real device listening (MqttTopic set) — a pure-sim
//     zone changes state in the engine but we don't spray commands onto topics no board
//     subscribes to.
func (e *Engine) actuate() {
	if !e.AutoPilot {
		// Suspended: release the optimizer's setbacks so the operator is handed a
		// normally-conditioned building at its occupied baseline, not zones stranded in
		// stale setback. Manual overrides (the human-in-the-loop veto latch) are left
		// untouched — turning off automation must not stomp a human command.
		for _, z := range e.Zones {
			if time.Now().Before(z.OverrideUntil) {
				continue
			}
			z.Setpoint = z.BaseSetpoint
			z.LightsOn = true
		}
		e.zonesInSetback = 0
		return
	}
	preCool := time.Now().Before(e.PreCoolUntil)
	// One ambient for the recovery calculation, hoisted out of the zone loop.
	tOutForSetback, _ := e.outdoorNow()
	setback := 0
	for id, z := range e.Zones {
		if time.Now().Before(z.OverrideUntil) {
			if z.Setpoint > z.BaseSetpoint+0.01 {
				setback++ // a manual veto can itself be a setback; still count it
			}
			continue // Respect the human-in-the-loop manual override latch
		}
		if z.Occupancy <= 0 {
			z.VacantTicks++
		} else {
			z.VacantTicks = 0
		}
		// A critical space is never set back, however empty it looks. A comms room has
		// no occupants by design, so vacancy is not evidence that it may drift — and
		// raising its setpoint does not reduce the heat its equipment makes. The plug
		// sweep has always known this (PlugConfig.CriticalTypes); the setback did not,
		// and on the digitized fixture that disagreement is where the overwhelming
		// majority of credited savings came from. Both now read the same library flag.
		if IsCritical(z.Type) {
			z.Setpoint = z.BaseSetpoint
			z.LightsOn = true
			continue
		}
		vacant := z.Occupancy <= 0 && z.VacantTicks >= vacancyDelayTicks

		desiredLights := !vacant
		desiredSp := z.BaseSetpoint
		if vacant {
			// Setback depth is asked of the room's own identified physics rather than
			// being the same number everywhere: how far can THIS room drift and still be
			// back at setpoint within the recovery budget once someone returns? A light,
			// responsive room earns a deeper setback (more energy saved); a heavy one that
			// cannot catch up gets a shallower one. Falls back to the conventional fixed
			// figure until the room is identified.
			delta := fixedSetbackC
			if e.dynamics != nil {
				if learned, ok := e.dynamics.SetbackCeiling(id, z.BaseSetpoint, tOutForSetback, setbackRecoverySecs); ok {
					delta = learned
				}
			}
			desiredSp = z.BaseSetpoint + delta
			setback++
		} else if preCool {
			// Forecast says a demand peak is coming: run occupied zones slightly cold
			// now (cheap thermal-mass charge) so chillers can shed load at the peak.
			desiredSp = z.BaseSetpoint - preCoolDelta
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
			// Only command zones a real device is bound to; sim zones just changed state.
			if z.MqttTopic != "" && e.Publish != nil {
				log.Printf("[actuate] zone=%s occ=%d -> %s", id, z.Occupancy, cmd)
				e.Publish("econ/commands/"+z.MqttTopic, cmd)
			}
		}
	}
	e.zonesInSetback = setback
}

// SetAutoPilot toggles the autonomous optimizer. Off suspends actuate() (the operator
// holds control); on resumes occupancy-driven setback on the next tick.
func (e *Engine) SetAutoPilot(on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.AutoPilot != on {
		log.Printf("[autopilot] %v", on)
	}
	e.AutoPilot = on
}

// hwStaleAfter bounds how long a measured temperature keeps pinning a zone: past it the
// node is presumed unplugged and the 2R1C model takes back over. Nodes publish every
// 2–5 s, so 20 s tolerates a few dropped messages without flapping.
const hwStaleAfter = 20 * time.Second

// hwFresh reports whether this zone is currently pinned to a live measured temperature.
func (z *ZoneSim) hwFresh() bool {
	return !z.HwTempAt.IsZero() && time.Since(z.HwTempAt) < hwStaleAfter
}

// outdoorFallbackC is the Ho Chi Minh City climatological mean the envelope ran on before
// live weather was wired in. It is the value of last resort: used until the first fetch
// succeeds and again whenever the feed goes stale.
const outdoorFallbackC = 30.0

// outdoorStaleAfter bounds how long one weather reading may keep driving the envelope.
// Open-Meteo refreshes its current conditions on a sub-hourly cadence and the poller asks
// every 10 minutes, so three missed hours means the feed is genuinely down, not jittery.
const outdoorStaleAfter = 3 * time.Hour

// SetOutdoorTemp ingests one outdoor reading from the weather poller. Humidity may be
// zero when the fetch didn't include it; the forecaster path checks for that.
func (e *Engine) SetOutdoorTemp(c float64) {
	e.SetOutdoor(c, 0)
}

func (e *Engine) SetOutdoor(c, humPct float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.outdoorTemp = c
	e.outdoorHum = humPct
	e.outdoorAt = time.Now()
}

// OutdoorForForecast reports the outdoor conditions the forecaster should use, and
// whether they are live enough to hand over. Both readings arrive together from
// Open-Meteo, so one freshness verdict covers them.
func (e *Engine) OutdoorForForecast() (tempC, humPct float64, live bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.outdoorNow()
	return t, e.outdoorHum, ok && e.outdoorHum > 0
}

// outdoorNow returns the temperature the envelope should integrate against and whether it
// is live weather. Callers must hold e.mu.
func (e *Engine) outdoorNow() (float64, bool) {
	if !e.outdoorAt.IsZero() && time.Since(e.outdoorAt) < outdoorStaleAfter {
		return e.outdoorTemp, true
	}
	return outdoorFallbackC, false
}

// OutdoorStatus is the /api/weather snapshot: what the physics is using right now.
func (e *Engine) OutdoorStatus() (tempC float64, live bool, ageSec float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.outdoorNow()
	age := -1.0
	if !e.outdoorAt.IsZero() {
		age = time.Since(e.outdoorAt).Seconds()
	}
	return t, ok, age
}

// humFresh / co2Fresh report whether a physical sensor is measuring that quantity right
// now. Both are per-field rather than per-node: one sensor on a shared I2C bus can fail
// while its neighbour keeps reporting, and a stale value presented as measured is exactly
// the fabrication the edge firmware goes out of its way to avoid.
func (z *ZoneSim) humFresh() bool {
	return !z.HwHumAt.IsZero() && time.Since(z.HwHumAt) < hwStaleAfter
}

func (z *ZoneSim) co2Fresh() bool {
	return !z.HwCo2At.IsZero() && time.Since(z.HwCo2At) < hwStaleAfter
}

// avgCo2 is the building CO2 figure, and it prefers reality: the average of whatever
// fresh NDIR sensors are actually reporting, falling back to a modelled estimate only
// when nothing is measuring. One function feeds both the TimescaleDB history and the
// live stream, so a real sensor sitting at 900 ppm can never coexist with a chart
// serenely plotting a modelled 450.
//
// The estimate is the mean of per-zone steady states (400 ppm outdoor + 15 ppm per
// occupant in the zone, the same model the dashboard uses for a single zone) — NOT
// 400 + total_occupants*k, which treats every person in the building as if they shared
// one room and, at 6,000 occupants, "estimated" 5,500 ppm: an occupational exposure
// limit, not a ventilated building's average. Callers hold e.mu.
func (e *Engine) avgCo2(totalOccupants int) float64 {
	var sum float64
	var n int
	for _, z := range e.Zones {
		if z.HwCo2 > 0 && z.co2Fresh() {
			sum += z.HwCo2
			n++
		}
	}
	if n > 0 {
		return sum / float64(n)
	}
	if len(e.Zones) == 0 {
		return 400.0
	}
	return 400.0 + 15.0*float64(totalOccupants)/float64(len(e.Zones))
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
		// AFDD residual: how far the measured room has drifted from the sensor-free
		// shadow model. Slow EMA (~2 s time constant at 30 FPS) rejects sensor noise
		// while still catching real faults in well under a minute.
		if z.ShadowTemp != 0 {
			r := math.Abs(z.HwTemp - z.ShadowTemp)
			z.ResidualEma += (r - z.ResidualEma) * 0.02
		}
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
			// The node is gone, so every sensor hanging off it is gone with it.
			z.HwTempAt = time.Time{}
			z.HwHumAt = time.Time{}
			z.HwCo2At = time.Time{}
			z.HwPlugAt = time.Time{}
		}
	}
}

// StartPreCool opens (or extends) a pre-cooling window: for its duration the optimizer
// drives every occupied zone preCoolDelta below its base setpoint, charging the
// building's thermal mass ahead of a forecast demand peak. Called by the LSTM poller
// (precool.go) and by the dashboard's "pre-cool" action. Returns when the window ends.
func (e *Engine) StartPreCool(d time.Duration) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	if until := time.Now().Add(d); until.After(e.PreCoolUntil) {
		e.PreCoolUntil = until
	}
	return e.PreCoolUntil
}

// PreCoolStatus reports whether a pre-cool window is active and when it ends.
func (e *Engine) PreCoolStatus() (bool, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return time.Now().Before(e.PreCoolUntil), e.PreCoolUntil
}

// Recommendations scores the building's current state against the learned baseline model
// (baselines.go / recommend.go) and returns the ranked report the dashboard renders. It
// gathers each zone's live values under e.mu, then releases e.mu before scoring, so the
// engine lock and the baseline lock are never held together (one-way order e.mu →
// baselines.mu everywhere else; this path takes neither pair simultaneously).
func (e *Engine) Recommendations(topN int) RecommendationReport {
	e.mu.Lock()
	readings := make([]ZoneReading, 0, len(e.Zones))
	for id, z := range e.Zones {
		label := strings.TrimPrefix(id, "zone-")
		zr := ZoneReading{
			Zone: id, Label: label, Type: z.Type,
			Temp: z.Temp, Setpoint: z.Setpoint, Occupancy: z.Occupancy,
		}
		if z.HwCo2 > 0 && z.co2Fresh() {
			zr.Co2 = z.HwCo2
			zr.Co2Live = true
		}
		readings = append(readings, zr)
	}
	conds := e.roomConditions()
	loadMw := e.lastLoadMw
	e.mu.Unlock()

	if e.baselines == nil {
		return RecommendationReport{}
	}
	now := time.Now()
	// Two passes over two different models, merged and ranked together: the baselines say
	// what is abnormal RIGHT NOW, the identified room dynamics say what is about to go
	// wrong and how long there is to act. topN is applied after the merge so a genuinely
	// urgent prediction can outrank a mild present anomaly.
	report := e.baselines.Recommend(readings, loadMw, now, 0)
	var predictive []Recommendation
	if e.dynamics != nil {
		predictive = e.dynamics.PredictiveRecommendations(conds, now)
		report.Model.RoomsIdentified, report.Model.RoomsLearning = e.dynamics.Coverage()
	}
	return mergeRecommendations(report, predictive, topN)
}

// LoadForecastThreshold returns the learned "high load" line the pre-cool automation
// triggers against — mean + k·σ of the building-load baseline for the coming hour — and
// whether the model has matured enough to trust it. When ok is false the caller keeps its
// fixed fallback, so pre-cooling is data-driven once the twin knows the building and
// safely conventional before that. lead looks slightly ahead so the trigger anticipates
// the peak the forecast is warning about.
func (e *Engine) LoadForecastThreshold(k float64, lead time.Duration) (threshold float64, ok bool) {
	if e.baselines == nil {
		return 0, false
	}
	return e.baselines.LoadThreshold(time.Now().Add(lead), k)
}

// MarshalBaselines / LoadBaselines let main.go persist the learned model across restarts
// (recommendapi.go), exactly like the plug savings counter — a model that forgets
// everything it learned on every redeploy would be re-learning "normal" forever. Bytes,
// not the internal map type, so the persistence lives cleanly in package main.
func (e *Engine) MarshalBaselines() ([]byte, error) {
	if e.baselines == nil {
		return []byte("{}"), nil
	}
	return e.baselines.MarshalState()
}

func (e *Engine) LoadBaselines(data []byte) error {
	if e.baselines == nil {
		return nil
	}
	return e.baselines.LoadState(data)
}

// BaselineCoverage reports the model's maturity (established vs still-learning buckets) —
// the honest readout the recommendations panel shows.
func (e *Engine) BaselineCoverage() (established, learning int) {
	if e.baselines == nil {
		return 0, 0
	}
	return e.baselines.Coverage()
}

// roomConditions gathers every room's current physical drivers for the learned dynamics
// model: the terms that actually appear in its energy and mass balances. Lock held.
func (e *Engine) roomConditions() []RoomCondition {
	// Cooling reaches a zone through the VAV that targets it. Normalizing to that VAV's
	// own nominal flow keeps the regressor the physical flow fraction, matching the
	// cooling law the engine integrates (tick).
	flow := make(map[string]float64, len(e.Vavs))
	for _, v := range e.Vavs {
		f := 0.0
		if v.NominalFlow > 1e-6 {
			f = v.Flow / v.NominalFlow
		}
		flow[v.TargetZone] = f
	}
	tOut, _ := e.outdoorNow()

	out := make([]RoomCondition, 0, len(e.Zones))
	for id, z := range e.Zones {
		c := RoomCondition{
			Zone: id, Label: strings.TrimPrefix(id, "zone-"),
			Temp: z.Temp, Setpoint: z.Setpoint, OutdoorC: tOut,
			FlowRatio: flow[id], Occupancy: z.Occupancy,
		}
		// Only a live NDIR reading may teach or be scored by the CO2 balance — a modelled
		// estimate would train the model on the twin's own guess.
		if z.HwCo2 > 0 && z.co2Fresh() {
			c.Co2, c.Co2Live = z.HwCo2, true
		}
		out = append(out, c)
	}
	return out
}

// RoomConditions is the exported snapshot of every room's drivers, used by the model API
// and the export bundle. Takes and releases e.mu.
func (e *Engine) RoomConditions() []RoomCondition {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.roomConditions()
}

// RoomModels returns each room's learned physical identity (time constant, cooling
// authority, air-change rate). Gathers labels under e.mu, then reads the dynamics model
// after releasing it — preserving the one-way lock order.
func (e *Engine) RoomModels() []RoomModel {
	if e.dynamics == nil {
		return nil
	}
	e.mu.Lock()
	labels := make(map[string]string, len(e.Zones))
	for id := range e.Zones {
		labels[id] = strings.TrimPrefix(id, "zone-")
	}
	e.mu.Unlock()
	return e.dynamics.RoomModels(labels)
}

// DynamicsCoverage reports how many rooms the twin has actually identified vs. how many
// are still being learned.
func (e *Engine) DynamicsCoverage() (identified, learning int) {
	if e.dynamics == nil {
		return 0, 0
	}
	return e.dynamics.Coverage()
}

// MarshalDynamics / LoadDynamics persist the identified room models across restarts, the
// same way the baselines are persisted — re-identifying every room from scratch on each
// redeploy would throw away days of learning.
func (e *Engine) MarshalDynamics() ([]byte, error) {
	if e.dynamics == nil {
		return []byte("{}"), nil
	}
	return e.dynamics.MarshalState()
}

func (e *Engine) LoadDynamics(data []byte) error {
	if e.dynamics == nil {
		return nil
	}
	return e.dynamics.LoadState(data)
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
	// Physics-grounded AFDD outputs (zero until the zone's first real temperature).
	ShadowTemp float64 `json:"shadowTemp"`
	Residual   float64 `json:"residual"`
	AfddAlert  bool    `json:"afddAlert"`
	// APLC: live clamp watts (0 = no meter reporting) and current sweep state.
	PlugW    float64 `json:"plugW"`
	PlugShed bool    `json:"plugShed"`
	// Closed-loop AC control: whether this node's setpoint commands reach a real machine.
	// AcControlKnown is false for firmware predating the acReal field.
	AcReal         bool `json:"acReal"`
	AcControlKnown bool `json:"acControlKnown"`
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
		// Report an environmental only while its own sensor is still reporting, so this
		// endpoint agrees with the telemetry stream rather than showing a last-known value
		// the dashboard has already dropped.
		hum, co2, plugW := 0.0, 0.0, 0.0
		if z.humFresh() {
			hum = z.HwHum
		}
		if z.co2Fresh() {
			co2 = z.HwCo2
		}
		if z.plugFresh() {
			plugW = z.HwPlugW
		}
		out = append(out, HardwareNode{
			ZoneId:     id,
			Topic:      z.MqttTopic,
			Source:     z.HwSource,
			Online:     z.HwOnline && age < 60,
			TempPinned: z.hwFresh(),
			Occupancy:  z.Occupancy,
			ZoneTemp:   z.Temp,
			HwTemp:     z.HwTemp,
			Humidity:   hum,
			Co2:        co2,
			LightsOn:   z.LightsOn,
			Setpoint:   z.Setpoint,
			AgeSec:     age,
			ShadowTemp: z.ShadowTemp,
			Residual:   z.ResidualEma,
			AfddAlert:  z.ShadowTemp != 0 && z.ResidualEma > afddThreshold,
			PlugW:      plugW,
			PlugShed:   z.PlugShed,
			// Whether this node's setpoint commands actually reach an air conditioner.
			AcReal:         z.HwAcReal,
			AcControlKnown: z.HwAcRealSeen,
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

		if v.Resistance < 0.01 {
			v.Resistance = 0.01
		}
		if v.Resistance > 100.0 {
			v.Resistance = 100.0
		}
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
	for u == 0 {
		u = rand.Float64()
	}
	for v == 0 {
		v = rand.Float64()
	}
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
				// Compare against the zone's OWN target. Guessing it from the zone type
				// meant a room whose setpoint differed from the guess read as
				// permanently "recovering" and held the whole building in accelerated
				// time — which also detached the learned models' hour-of-day buckets
				// from real hours.
				sp := z.BaseSetpoint
				if sp == 0 {
					sp = 24.0
				}
				if dev := math.Abs(z.Temp - sp); dev > maxDev {
					maxDev = dev
				}
			}
			if maxDev > 1.0 {
				dt = 2.0 // 60x speed recovery!
			}
		}
		// Physics + optimizer + hardware pinning run as ONE critical section: the MQTT
		// ingestion and HTTP snapshot goroutines mutate/read the same zone state under
		// e.mu, so integrating outside the lock (as before) was a data race.
		e.tick(dt)
		// Advance the simulation clock in step with the physics — the time base the
		// learned room dynamics are identified on (dynamics.go).
		e.simClock += dt

		// Occupancy-driven optimizer + edge actuation (publishes only on state change).
		e.actuate()
		e.applyHardware()

		// After-hours plug sweep (APLC): shed/restore switchable sockets on verified
		// vacancy, accumulate avoided energy on wall-clock time.
		e.plugTick(time.Now())

		// One forecaster timestep every histInterval: the LSTM's input window is a
		// real sampled hour, not a photocopied instant.
		e.sampleHistory(time.Now())

		// BESS dispatch: TOU-driven charge/discharge against the last computed building load,
		// integrated on real wall-clock time so the state of charge trends realistically.
		now := time.Now()
		e.Bess.Dispatch(now.Sub(e.lastBessAt).Seconds(), e.lastLoadMw, touBand(now))
		e.lastBessAt = now
		e.mu.Unlock()

		e.broadcast()
	}
}

// tick integrates one thermal step. Called only from Start's loop with e.mu held.
func (e *Engine) tick(dt float64) {
	// One ambient for the whole building per step: live weather when the poller has a
	// fresh reading, the HCMC climatological constant otherwise. Hoisted out of the
	// VAV loop — 891 zones share one sky.
	tOutside, _ := e.outdoorNow()

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
		if qCooling < 0 {
			qCooling = 0
		} // Cannot heat with cold air

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

		// Physics-grounded AFDD: integrate the sensor-free shadow twin with the
		// same 2R1C dynamics and cooling law, but never pulled toward the hardware
		// measurement (applyHardware skips it). Divergence between the measured
		// room and this twin is the fault signal.
		if z.ShadowTemp != 0 {
			qCoolShadow := flowRatio * qNominalTotal * ((z.ShadowTemp - 12.0) / (sp - 12.0))
			if qCoolShadow < 0 {
				qCoolShadow = 0
			}
			dShadowDt := ((z.WallTemp-z.ShadowTemp)/(z.RIn*z.CAir) + (qInternal-qCoolShadow)/z.CAir)
			z.ShadowTemp += dShadowDt * dt
			z.ShadowTemp = math.Max(5.0, math.Min(50.0, z.ShadowTemp))
		}
	}
}

// currentAvg computes the building-average room temperature and airflow fraction —
// one forecaster timestep. Airflow is normalized to each VAV's nominal flow so it
// matches the model's training scale (raw m³/s would be far out of distribution).
// Hardware-pinned zone temperatures are naturally included: z.Temp IS the measured
// value while a sensor is fresh. Lock held.
func (e *Engine) currentAvg() histSample {
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
	s := histSample{temp: 24.0, flow: 0.5}
	if len(e.Zones) > 0 {
		s.temp = tempSum / float64(len(e.Zones))
	}
	if len(e.Vavs) > 0 {
		s.flow = flowSum / float64(len(e.Vavs))
	}
	return s
}

// sampleHistory appends one timestep to the forecaster's rolling window every
// histInterval. Called from Start's loop with e.mu held; the first tick seeds the
// buffer immediately so a fresh boot has at least one real sample.
func (e *Engine) sampleHistory(now time.Time) {
	if !e.lastHistAt.IsZero() && now.Sub(e.lastHistAt) < histInterval {
		return
	}
	e.lastHistAt = now
	e.histBuf = append(e.histBuf, e.currentAvg())
	if len(e.histBuf) > histKeep {
		e.histBuf = e.histBuf[len(e.histBuf)-histKeep:]
	}
	// The same timestep for the load series the foundation-model forecaster consumes. Only
	// a real computed load is recorded — a zero here would teach the forecaster that the
	// building went dark, so pre-boot ticks are skipped rather than logged as 0 MW.
	if e.lastLoadMw > 0 {
		e.loadHist = append(e.loadHist, e.lastLoadMw)
		if len(e.loadHist) > loadHistKeep {
			e.loadHist = e.loadHist[len(e.loadHist)-loadHistKeep:]
		}
	}
}

// MarshalLoadHistory / LoadLoadHistory persist the load series across restarts.
//
// This matters more than it looks: a foundation model's forecast quality is a function of
// how much real context it gets, and the series only accumulates at one sample per five
// minutes. Discarding it on every redeploy would permanently cap the forecaster at however
// long the process happened to have been up.
func (e *Engine) MarshalLoadHistory() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return json.Marshal(e.loadHist)
}

func (e *Engine) LoadLoadHistory(data []byte) error {
	var hist []float64
	if err := json.Unmarshal(data, &hist); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Only finite, positive samples: a corrupt file must not be able to teach the
	// forecaster that the building drew NaN megawatts.
	e.loadHist = e.loadHist[:0]
	for _, v := range hist {
		if v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			e.loadHist = append(e.loadHist, v)
		}
	}
	if len(e.loadHist) > loadHistKeep {
		e.loadHist = e.loadHist[len(e.loadHist)-loadHistKeep:]
	}
	return nil
}

// LoadHistory returns the retained building-load series (MW, oldest first) for the
// zero-shot forecaster. Unlike the LSTM window it is NEVER padded: a foundation model
// handles a short series natively, so the honest thing is to hand over exactly what has
// really been measured and let the caller report how much that is.
func (e *Engine) LoadHistory() []float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]float64(nil), e.loadHist...)
}

// ForecastWindow returns the [room_temp(°C), airflow_fraction(0..1)] sequence the
// Python forecaster expects — the REAL sampled last hour, most recent last — plus how
// many of the timesteps are genuine samples. While the buffer is still warming up
// after a boot, the window is left-padded with the oldest real sample (or the current
// state when the buffer is empty), and the caller can report that honestly instead of
// presenting a photocopied instant as an hour of history.
func (e *Engine) ForecastWindow(seqLen int) ([][]float64, int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	real := e.histBuf
	if len(real) > seqLen {
		real = real[len(real)-seqLen:]
	}
	pad := e.currentAvg()
	if len(real) > 0 {
		pad = real[0]
	}

	seq := make([][]float64, seqLen)
	for i := range seq {
		idx := i - (seqLen - len(real))
		s := pad
		if idx >= 0 {
			s = real[idx]
		}
		seq[i] = []float64{s.temp, s.flow}
	}
	return seq, len(real)
}

func (e *Engine) broadcast() {
	// Metrics + serialization read (and update LastBroadcast*) zone state, so they
	// run under the lock; the websocket writes below happen outside it.
	e.mu.Lock()
	// ---- Live global metrics (all derived from current zone state) ----
	totalHeatW := 0.0 // total thermal load the plant must remove (W)
	totalOccupants := 0
	comfortSum := 0.0     // Σ per-zone thermal-comfort score (report §4.5 discomfort model)
	strainSum := 0.0      // sum of how far zones sit above setpoint (drives plant COP)
	savedLightingW := 0.0 // lighting cut on vacant (set-back) zones
	savedThermalW := 0.0  // cooling demand avoided on vacant (set-back) zones
	alarmCount := 0       // zones far enough past the band to be a genuine alarm
	plugTotalW := 0.0     // live plug draw (measured where clamped, modelled otherwise)
	// Outdoor air is what a setback actually saves against — no lift, no saving.
	tOutside, _ := e.outdoorNow()
	ventW := 0.0     // fresh-air load, the dominant cooling term in a tropical office
	condFloorM2 := 0.0 // conditioned floor area, for the area-scaled electrical baseline
	plugStandbyW := 0.0   // the always-on phantom portion of plugTotalW
	plugShedW := 0.0      // switchable watts currently swept off by the APLC
	// Half-comfort point: a zone this many °C *beyond* its deadband scores 0.5 comfort.
	const sigmaComfort2 = 2.5 * 2.5
	// °C past the deadband before a zone is "critical". Matches the dashboard's own
	// CRITICAL_MARGIN so the health number and the red banner never disagree.
	const criticalMargin = 5.0
	for id, z := range e.Zones {
		qSolar := z.SolarGainMult * 10000.0
		qi := z.BaseHeatGain + float64(z.Occupancy)*100.0 + qSolar
		if e.Scenario == "fault" && id == e.FaultTarget {
			qi *= 5.0
		}
		totalHeatW += qi
		totalOccupants += z.Occupancy
		condFloorM2 += z.AreaM2
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
		// A zone this far past its deadband is an alarm, not a drift — counted so health
		// below can charge for it (see the averaging problem there).
		if z.Temp > sp+z.Deadband+criticalMargin {
			alarmCount++
		}
		// Occupancy-driven savings. Two rules, both of which used to be wrong:
		//
		// Lighting: credited only for the zone's OWN installed lighting density, read
		// from the programme library rather than assumed to be office lighting. A plant
		// room and an open-plan floor do not have the same luminaires, and crediting
		// them alike is how a store cupboard came to look like a saving.
		//
		// Cooling: the avoided cooling of a setback is NOT a fixed fraction of the
		// zone's internal gain — that old `BaseHeatGain * 0.25` credited a room for
		// equipment that goes on running whether or not the thermostat moved. What a
		// setback actually avoids is the envelope and ventilation load that the zone no
		// longer has to hold down: raising the setpoint by ΔT cuts conduction against
		// outdoor air in proportion to ΔT over the original lift. That is a smaller,
		// defensible number, and it goes to zero when it should — when there is no
		// temperature difference to exploit.
		if z.Setpoint > z.BaseSetpoint+0.01 {
			if prog, ok := ProgrammeFor(z.Type); ok && !z.LightsOn {
				savedLightingW += z.AreaM2 * prog.LightingWPerM2
			}
			lift := tOutside - z.BaseSetpoint
			if lift > 0 {
				dT := z.Setpoint - z.BaseSetpoint
				uaW := 1.0 / math.Max(z.RIn+z.ROut, 0.05)
				savedThermalW += uaW * math.Min(dT, lift)
			}
		}
		// Plug loads (APLC, plugs.go): the end use the case-study BMS could not see.
		pTot, pStandby := z.plugNowW()
		plugTotalW += pTot
		plugStandbyW += pStandby
		if z.PlugShed {
			plugShedW += z.PlugStandbyW * plugSwitchableFrac
		}
	}

	// Plant coefficient of performance degrades as the building is strained (chillers
	// run harder at higher lift), so efficiency, cooling, and load are all coupled.
	avgStrain := 0.0
	if len(e.Zones) > 0 {
		avgStrain = strainSum / float64(len(e.Zones))
	}
	plantCop := math.Max(2.2, math.Min(3.8, 3.6-0.35*avgStrain))

	// Fresh air. In Ho Chi Minh City, dehumidifying outdoor air to a supply condition is
	// the LARGEST single cooling term in an office and it is mostly latent — omitting it
	// is why a twin calibrated on internal gains alone under-reports a Vietnamese
	// building by roughly a third. Scales with the people actually present, so it falls
	// away out of hours exactly as a real demand-controlled AHU would let it.
	ph := Phys()
	ventW = float64(totalOccupants) * (ph.OutdoorAirLPerSPerPerson / 1000.0) *
		ph.AirDensityKgPerM3 * ph.VentilationEnthalpyKjPerKg * 1000.0
	totalHeatW += ventW

	coolingOutputMW := totalHeatW / 1e6 // thermal cooling delivered (MW)
	coolingElectricalMW := coolingOutputMW / plantCop
	// Non-HVAC electrical: lighting, fans, lifts and pumps, scaled by the building's own
	// conditioned floor area rather than a fixed 1.15 MW that was sized for the
	// mis-digitized fixture and did not move when the building did. Plus the LIVE plug
	// figure, so the sweep's effect shows up the moment sockets shed. (Plug loads were
	// 26.4% of the Hanoi case-study tower's energy; hiding them in a constant is how
	// that happens.)
	baseElectricalMW := (condFloorM2*ph.NonHvacBaseWPerM2 + plugTotalW) / 1e6
	buildingLoadMW := coolingElectricalMW + baseElectricalMW
	energySavedMW := (savedLightingW + savedThermalW/plantCop) / 1e6
	// Feed the load to the BESS dispatcher (read next tick) and snapshot battery state.
	e.lastLoadMw = buildingLoadMW
	bessDischargeMW := e.Bess.DischargeMw
	bessSocPct := e.Bess.Soc * 100.0

	// System health = mean per-zone comfort (severity-weighted), per the report's discomfort
	// model, minus a charge for zones actually in alarm. The mean alone is misleading at
	// this scale: one server room cooking at 50 C across 1350 zones averages to 99.93%, so
	// the dashboard cheerfully reported "HEALTH 100%" directly beside its own CRITICAL FAULT
	// banner. Alarms are rare and serious, so each one moves the number an operator watches.
	systemHealth := 100.0
	if len(e.Zones) > 0 {
		systemHealth = 100.0 * comfortSum / float64(len(e.Zones))
	}
	if alarmCount > 0 {
		systemHealth = math.Max(0, systemHealth-math.Min(45.0, 12.0*float64(alarmCount)))
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
		e.Persist("GLOBAL", "avgCo2", e.avgCo2(totalOccupants))
		e.Persist("GLOBAL", "plugKw", plugTotalW/1000)
		// Feature series for OFFLINE LSTM retraining (backend/forecasting/train.py):
		// the SAME building-average [temp, airflow] the live forecaster consumes, plus
		// the outdoor conditions the envelope integrates against. Persisting them is
		// what lets the model be retrained on the building's OWN accumulated history
		// instead of only synthetic data — the target (buildingLoadMw) is already here.
		avg := e.currentAvg()
		e.Persist("GLOBAL", "avgTemp", avg.temp)
		e.Persist("GLOBAL", "avgAirflow", avg.flow)
		if tOut, live := e.outdoorNow(); live {
			e.Persist("GLOBAL", "outdoorTemp", tOut)
			if e.outdoorHum > 0 {
				e.Persist("GLOBAL", "outdoorHum", e.outdoorHum)
			}
		}
		for id, z := range e.Zones {
			e.Persist(id, "temp", z.Temp)
			e.Persist(id, "occupancy", float64(z.Occupancy))
			// Environmentals from a live physical sensor (humidity %, CO2 ppm) get
			// their own history series, each gated on its own sensor still reporting.
			if z.HwHum > 0 && z.humFresh() {
				e.Persist(id, "humidity", z.HwHum)
			}
			if z.HwCo2 > 0 && z.co2Fresh() {
				e.Persist(id, "co2", z.HwCo2)
			}
			// AFDD residual history for sensor-bound zones: the maintenance
			// evidence behind a "dispatch technician" card — how long the room
			// has been drifting from its physics, queryable, not just a live LED.
			if z.ShadowTemp != 0 && z.hwFresh() {
				e.Persist(id, "afddResidual", z.ResidualEma)
			}
		}
	}
	// [GEMINI IMPLEMENTATION END]

	// Fold this tick into the learned operating baselines (baselines.go). Deliberately
	// slower than the 1 Hz DB persist: at one sample per baselineSampleSecs, each
	// hour-of-day bucket's EWMA memory spans days, so it learns a real diurnal normal
	// instead of chasing the last few minutes. The model learns with or without a DB —
	// it is the twin's own memory of what "normal" is, independent of history storage.
	if e.baselines != nil && now.Sub(e.lastBaselineAt) >= baselineSampleSecs*time.Second {
		e.lastBaselineAt = now
		e.baselines.Observe("GLOBAL", "buildingLoadMw", buildingLoadMW, now)
		e.baselines.Observe("GLOBAL", "plugKw", plugTotalW/1000, now)
		for id, z := range e.Zones {
			e.baselines.Observe(id, "temp", z.Temp, now)
			// Only a live NDIR reading teaches the CO2 baseline — a modelled estimate
			// would train the model on its own guess and then flag reality against it.
			if z.HwCo2 > 0 && z.co2Fresh() {
				e.baselines.Observe(id, "co2", z.HwCo2, now)
			}
			// How busy a room is, is itself a learned normal — it is what makes an
			// "unusually crowded for this hour" judgement possible without a rule.
			e.baselines.Observe(id, "occupancy", float64(z.Occupancy), now)
		}
	}

	// Fold this tick into the learned per-room dynamics (dynamics.go). Sampled on the
	// SIMULATION clock, because that is the clock the physics advances on; the model
	// enforces its own spacing, so calling it every broadcast is safe and cheap.
	if e.dynamics != nil {
		e.dynamics.Observe(e.roomConditions(), e.simClock)
	}

	// FlatBuffers Serialization
	builder := flatbuffers.NewBuilder(1024)

	// Create Zones
	zoneOffsets := make([]flatbuffers.UOffsetT, 0)
	for id, z := range e.Zones {
		noiseTemp := z.Temp + getNoise(0.08)
		// A lighting or plug-shed flip must stream even when the temperature hasn't
		// moved past the dedupe threshold, or the UI would show it a frame too late.
		if math.Abs(noiseTemp-z.LastBroadcastTemp) > 0.05 || z.LightsOn != z.LastBroadcastLights || z.PlugShed != z.LastBroadcastPlugShed {
			z.LastBroadcastTemp = noiseTemp
			z.LastBroadcastLights = z.LightsOn
			z.LastBroadcastPlugShed = z.PlugShed
			idStr := builder.CreateString(id)
			Telemetry.ZoneDataStart(builder)
			Telemetry.ZoneDataAddId(builder, idStr)
			Telemetry.ZoneDataAddTemp(builder, float32(noiseTemp))
			Telemetry.ZoneDataAddOccupants(builder, int32(z.Occupancy))
			Telemetry.ZoneDataAddLoad(builder, float32(z.BaseHeatGain/1000.0))
			Telemetry.ZoneDataAddLightsOn(builder, z.LightsOn)
			// Measured air quality rides the main stream so the dashboard reads a bound
			// sensor's real humidity/CO2 straight from the telemetry it already consumes,
			// instead of a side poll. Gated on the node still being fresh: a board that
			// dropped off must stop reporting rather than pin its last reading there
			// forever, so zero always means "nothing is measuring this right now".
			var hwHum, hwCo2 float32
			if z.humFresh() {
				hwHum = float32(z.HwHum)
			}
			if z.co2Fresh() {
				hwCo2 = float32(z.HwCo2)
			}
			Telemetry.ZoneDataAddHumidity(builder, hwHum)
			Telemetry.ZoneDataAddCo2(builder, hwCo2)
			zPlugW, _ := z.plugNowW()
			Telemetry.ZoneDataAddPlugW(builder, float32(zPlugW))
			Telemetry.ZoneDataAddPlugShed(builder, z.PlugShed)
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
	Telemetry.GlobalDataAddBessDischargeMw(builder, float32(bessDischargeMW))
	Telemetry.GlobalDataAddBessSocPct(builder, float32(bessSocPct))
	Telemetry.GlobalDataAddAvgCo2(builder, float32(e.avgCo2(totalOccupants)))
	Telemetry.GlobalDataAddPlugKw(builder, float32(plugTotalW/1000))
	Telemetry.GlobalDataAddPlugStandbyKw(builder, float32(plugStandbyW/1000))
	Telemetry.GlobalDataAddPlugShedKw(builder, float32(plugShedW/1000))
	Telemetry.GlobalDataAddPlugSavedKwh(builder, float32(e.plugSavedKwh))
	Telemetry.GlobalDataAddAutoPilot(builder, e.AutoPilot)
	Telemetry.GlobalDataAddZonesInSetback(builder, int32(e.zonesInSetback))
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

	conns := make([]*websocket.Conn, 0, len(e.Clients))
	for c := range e.Clients {
		conns = append(conns, c)
	}
	e.mu.Unlock()

	// Network writes happen OUTSIDE the lock: a slow websocket client must never
	// stall the simulation loop or MQTT ingestion.
	var dead []*websocket.Conn
	for _, client := range conns {
		if err := client.WriteMessage(websocket.BinaryMessage, buf); err != nil {
			client.Close()
			dead = append(dead, client)
		}
	}
	if len(dead) > 0 {
		e.mu.Lock()
		for _, c := range dead {
			delete(e.Clients, c)
		}
		e.mu.Unlock()
	}
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
	if z != nil {
		// Mirror the veto onto the twin's own state immediately — the 3D lighting,
		// /api/hardware and the optimizer's view must reflect the human command during
		// the latch, not only after the optimizer reasserts control.
		applyCommandToZone(z, cmd)
	}
	log.Printf("[override] manual command %q (from %q) to %s (latched 15m)", cmd, action, topic)
	if e.Publish != nil {
		e.Publish("econ/commands/"+topic, cmd)
	}
}

// applyCommandToZone applies a firmware-format command string to the engine's zone
// state. Mirrors the edge firmware's parser: ;-separated LIGHTS_x / SETPOINT= /
// HVAC_SET: tokens, unknown tokens ignored. Lock held by the caller.
func applyCommandToZone(z *ZoneSim, cmd string) {
	for _, tok := range strings.Split(cmd, ";") {
		tok = strings.TrimSpace(tok)
		switch {
		case tok == "LIGHTS_ON":
			z.LightsOn = true
		case tok == "LIGHTS_OFF":
			z.LightsOn = false
		case strings.HasPrefix(tok, "SETPOINT="):
			if v, err := strconv.ParseFloat(tok[len("SETPOINT="):], 64); err == nil {
				z.Setpoint = v
			}
		case strings.HasPrefix(tok, "HVAC_SET:"):
			if v, err := strconv.ParseFloat(tok[len("HVAC_SET:"):], 64); err == nil {
				z.Setpoint = v
			}
		}
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
