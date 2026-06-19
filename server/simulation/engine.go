package simulation

import (
	"ecosync/schema/Telemetry"
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
	InternalHeatLoad float64 `json:"internalHeatLoad"`
	Occupancy        int     `json:"occupancy"`
	Setpoint         float64 `json:"setpoint"`
	WallThickness    float64 `json:"wallThickness"`
}

type HvacMap struct {
	VavId string `json:"vavId"`
}

type ZoneData struct {
	ZoneId            string       `json:"zoneId"`
	ZoneType          string       `json:"zoneType"`
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
	Occupancy         int
	BaseHeatGain      float64
	CAir              float64
	CWall             float64
	RIn               float64
	ROut              float64
	Setpoint          float64
	LastBroadcastTemp float64
}

type VavSim struct {
	TargetZone        string
	Resistance        float64
	Flow              float64
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
				Occupancy:    z.ThermalProperties.Occupancy,
				BaseHeatGain: z.ThermalProperties.InternalHeatLoad,
				CAir:         1.202 * 1006 * z.Volume,
				CWall:        2400 * 880 * z.WallArea * z.ThermalProperties.WallThickness,
				RIn:          1.0 / (8.3 * z.WallArea),
				ROut:         1.0 / (34.0 * z.WallArea),
				Setpoint:     temp,
			}
		}
	}

	e.doHardyCross()
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
	e.Scenario = s
	
	if s == "fault" {
		if v, ok := e.Vavs["vav-server-6a"]; ok {
			v.Resistance = 15.0
		}
	} else if s == "remediating" {
		if v, ok := e.Vavs["vav-server-6a"]; ok {
			v.Resistance = 0.01 // Massive airflow for rapid accurate cooling
		}
		for k, v := range e.Vavs {
			if k != "vav-server-6a" {
				v.Resistance = 10.0
			}
		}
	} else {
		for _, v := range e.Vavs {
			v.Resistance = 1.0
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

			qInternal := z.BaseHeatGain
			if e.Scenario == "fault" && z.Type == "server-room" {
				qInternal *= 5.0 // Server thermal runaway!
			}

			sp := z.Setpoint
			if sp == 0 {
				sp = 24.0
			}

			// Nominal flow at Resistance = 1.0 is ~5.4 m3/s.
			flowRatio := v.Flow / 5.4 

			// Heat transfer formula:
			tOutside := 30.0

			// To guarantee the room stabilizes exactly at the Setpoint during nominal flow (flowRatio ~= 1.0),
			// qCooling must equal the sum of internal heat and steady-state wall conduction.
			qSteadyStateWall := (tOutside - sp) / (z.RIn + z.ROut)
			qNominalTotal := z.BaseHeatGain + qSteadyStateWall
			
			qCooling := flowRatio * qNominalTotal * ((z.Temp - 12.0) / (sp - 12.0))
			if qCooling < 0 { qCooling = 0 } // Cannot heat with cold air

			dTAirDt := ((z.WallTemp-z.Temp)/(z.RIn*z.CAir) + (qInternal-qCooling)/z.CAir)
			dTWallDt := ((tOutside-z.WallTemp)/(z.ROut*z.CWall) - (z.WallTemp-z.Temp)/(z.RIn*z.CWall))

			z.Temp += dTAirDt * dt
			z.WallTemp += dTWallDt * dt
		}
}

func (e *Engine) broadcast() {
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
		Telemetry.GlobalDataAddBuildingLoadMw(builder, 4.15)
		Telemetry.GlobalDataAddSystemHealth(builder, 98.0)
		Telemetry.GlobalDataAddTotalOccupants(builder, 412)
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
