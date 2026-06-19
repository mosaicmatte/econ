package simulation

import (
	"econ/schema/Telemetry"
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"os"
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
	Deadband          float64
	LastBroadcastTemp float64
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
}

func NewEngine() *Engine {
	e := &Engine{
		Clients:  make(map[*websocket.Conn]bool),
		Zones:    make(map[string]*ZoneSim),
		Vavs:     make(map[string]*VavSim),
		PMax:     600.0,
		KFan:     0.01,
		Scenario: "peak",
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

			e.Zones[z.ZoneId] = &ZoneSim{
				Temp:         temp,
				WallTemp:     temp,
				Type:         z.ZoneType,
				BimAssetId:   z.BimAssetId,
				Occupancy:    rand.Intn(10),
				BaseHeatGain: z.ThermalProperties.BaseHeatLoad,
				SolarGainMult: z.ThermalProperties.SolarGainMultiplier,
				CAir:         z.ThermalProperties.CAir,
				CWall:        4000000.0,
				RIn:          z.ThermalProperties.RWall / 2,
				ROut:         z.ThermalProperties.RWall / 2 + 0.1,
				Setpoint:     z.ThermalProperties.Setpoint,
				Deadband:     z.ThermalProperties.Deadband,
				LastBroadcastTemp: 24.0,
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
		}
}

func (e *Engine) broadcast() {
		// ---- Live global metrics (computed from current zone state) ----
		totalHeatW := 0.0
		totalOccupants := 0
		inBand := 0
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
			if math.Abs(z.Temp-sp) <= z.Deadband {
				inBand++
			}
		}
		// Electrical draw: cooling (heat / COP) + fixed base (lighting + plug loads).
		coolingMW := (totalHeatW / 3.0) / 1e6
		buildingLoadMW := coolingMW + 2.0
		systemHealth := 100.0
		if len(e.Zones) > 0 {
			systemHealth = 100.0 * float64(inBand) / float64(len(e.Zones))
		}

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
