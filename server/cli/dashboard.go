package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"econ/schema/Telemetry"
	"github.com/gorilla/websocket"
)

// ANSI Color Codes
const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Cyan   = "\033[36m"
	Clear  = "\033[H\033[2J"
)

type ZoneInfo struct {
	ID        string
	Temp      float32
	Occupants int32
	Load      float32
}

type Centroid struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type StaticZone struct {
	ZoneID   string   `json:"zoneId"`
	Centroid Centroid `json:"centroid"`
}

type FloorData struct {
	Level int          `json:"level"`
	Zones []StaticZone `json:"zones"`
}

type BuildingData struct {
	Floors []FloorData `json:"floors"`
}

var spatialMap map[string]Centroid

func fetchBuildingData() {
	spatialMap = make(map[string]Centroid)
	resp, err := http.Get("http://localhost:8080/api/building-data")
	if err != nil {
		log.Println("Failed to fetch building data:", err)
		return
	}
	defer resp.Body.Close()

	var data BuildingData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Println("JSON decode error:", err)
		return
	}

	for _, f := range data.Floors {
		if f.Level == 6 {
			for _, z := range f.Zones {
				spatialMap[z.ZoneID] = z.Centroid
			}
		}
	}
}

func main() {
	log.SetFlags(0)
	
	// Fetch spatial mapping first
	fmt.Printf("%sFetching Building Geometry...%s\n", Cyan, Reset)
	fetchBuildingData()
	
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := "ws://localhost:8080/ws"
	
	fmt.Printf("%sConnecting to ECON Backend at %s...%s\n", Cyan, u, Reset)

	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		log.Fatal("Dial error:", err)
	}
	defer c.Close()

	done := make(chan struct{})

	// Render loop variables
	var lastGlobal Telemetry.GlobalData
	var zones []ZoneInfo
	var vCount, zCount int

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("Read error:", err)
				return
			}

			// Decode FlatBuffers
			simState := Telemetry.GetRootAsSimState(message, 0)
			
			// Extract Global Data
			globalData := new(Telemetry.GlobalData)
			if simState.Global(globalData) != nil {
				lastGlobal = *globalData
			}

			// Extract Zones
			zLen := simState.ZonesLength()
			zCount = zLen
			zones = make([]ZoneInfo, zLen)
			zObj := new(Telemetry.ZoneData)
			for i := 0; i < zLen; i++ {
				if simState.Zones(zObj, i) {
					zones[i] = ZoneInfo{
						ID:        string(zObj.Id()),
						Temp:      zObj.Temp(),
						Occupants: zObj.Occupants(),
						Load:      zObj.Load(),
					}
				}
			}

			vCount = simState.VavsLength()
		}
	}()

	// 10 FPS Render Loop
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			renderDashboard(lastGlobal, zones, zCount, vCount)
		case <-interrupt:
			fmt.Println("Closing connection...")
			c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}

func renderDashboard(global Telemetry.GlobalData, zones []ZoneInfo, zCount, vCount int) {
	fmt.Print(Clear)
	fmt.Printf("%s=== ECON TERMINAL DASHBOARD ===%s\n\n", Blue, Reset)
	
	// 1. ASCII Floorplan Radar (Level 6)
	// Create a 40x10 grid (X scaled by 0.66, Y scaled by 0.25) to fit a 24-row terminal
	gridWidth := 40
	gridHeight := 10
	grid := make([][]string, gridHeight)
	for i := range grid {
		grid[i] = make([]string, gridWidth)
		for j := range grid[i] {
			grid[i][j] = " "
		}
	}

	// Map live zones onto the grid
	for _, z := range zones {
		if c, exists := spatialMap[z.ID]; exists {
			x := int(c.X * 0.66)
			y := int(c.Y * 0.25)
			if x >= 0 && x < gridWidth && y >= 0 && y < gridHeight {
				color := Green
				if z.Temp > 26.0 {
					color = Red
				} else if z.Temp > 24.5 {
					color = Yellow
				}
				grid[y][x] = fmt.Sprintf("%s█%s", color, Reset)
			}
		}
	}

	// Print Grid with borders
	border := "+" + strings.Repeat("-", gridWidth) + "+"
	fmt.Printf("%sLEVEL 6 SPATIAL THERMAL RADAR%s\n", Cyan, Reset)
	fmt.Println(border)
	for _, row := range grid {
		fmt.Printf("|%s|\n", strings.Join(row, ""))
	}
	fmt.Println(border)

	// 2. Global Stats (Inline to save space)
	healthColor := Green
	if global.SystemHealth() < 90 {
		healthColor = Red
	}
	fmt.Printf("%s[LOAD: %s%.1f MW%s] | [HEALTH: %s%.1f%%%s] | [PAX: %s%d%s] | [STREAMS: %s%d%s]%s\n", 
		Cyan, Yellow, global.BuildingLoadMw(), Cyan, healthColor, global.SystemHealth(), Cyan, Yellow, global.TotalOccupants(), Cyan, Yellow, zCount, Cyan, Reset)

	// Sort zones by hottest first to show faults at the top
	sort.Slice(zones, func(i, j int) bool {
		return zones[i].Temp > zones[j].Temp
	})

	fmt.Printf("\n%sTOP 5 HOTTEST ZONES%s\n", Cyan, Reset)
	fmt.Printf("%-25s | %-10s | %-10s | %-10s\n", "ZONE ID", "TEMP (°C)", "LOAD (W)", "OCCUPANTS")
	fmt.Println(strings.Repeat("-", 65))

	limit := 5
	if len(zones) < limit {
		limit = len(zones)
	}

	for i := 0; i < limit; i++ {
		z := zones[i]
		
		color := Green
		if z.Temp > 26.0 {
			color = Red // Thermal Runaway Alert
		} else if z.Temp > 24.5 {
			color = Yellow // Warning
		}

		fmt.Printf("%s%-25s | %-10.2f | %-10.0f | %-10d%s\n", color, z.ID, z.Temp, z.Load, z.Occupants, Reset)
	}
	
	fmt.Printf("\n%sPress Ctrl+C to exit.%s\n", Cyan, Reset)
}
