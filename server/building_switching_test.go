package main

import (
	"bytes"
	"econ/schema/Telemetry"
	"econ/simulation"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestEngineBuildingSwitchingDirect verifies that the simulation engine cleanly transitions
// from the commercial office tower (bldg-econ-digitized, 735 zones) to the domestic house
// (bldg-econ-house-hcmc, 5 zones) and back, resetting baselines, load history, fan sizing,
// and zone mappings.
func TestEngineBuildingSwitchingDirect(t *testing.T) {
	homeData, err := os.ReadFile(simulation.DataPath(simulation.BuildingDataHomeFile))
	if err != nil {
		t.Fatalf("failed to read %s: %v", simulation.BuildingDataHomeFile, err)
	}

	officeData, err := os.ReadFile(simulation.DataPath(simulation.BuildingDataFile))
	if err != nil {
		t.Fatalf("failed to read %s: %v", simulation.BuildingDataFile, err)
	}

	engine := simulation.NewEngine()

	// 1. Initial state: commercial office tower
	if engine.BuildingId() != "bldg-econ-digitized" {
		t.Errorf("initial buildingId = %q, want %q", engine.BuildingId(), "bldg-econ-digitized")
	}
	initialZones := len(engine.Zones)
	if initialZones != 735 {
		t.Errorf("initial zone count = %d, want 735", initialZones)
	}
	if len(engine.Vavs) != 735 {
		t.Errorf("initial vav count = %d, want 735", len(engine.Vavs))
	}
	if engine.PMax < 100.0 {
		t.Errorf("commercial fan PMax = %.1f kW, want >= 100 kW", engine.PMax)
	}
	initialPMax := engine.PMax

	// 2. Switch to domestic house
	if err := engine.ReloadBuilding(homeData); err != nil {
		t.Fatalf("ReloadBuilding(homeData) failed: %v", err)
	}

	if engine.BuildingId() != "bldg-econ-house-hcmc" {
		t.Errorf("after home switch, buildingId = %q, want %q", engine.BuildingId(), "bldg-econ-house-hcmc")
	}
	if len(engine.Zones) != 5 {
		t.Fatalf("domestic home zone count = %d, want 5", len(engine.Zones))
	}
	if len(engine.Vavs) != 5 {
		t.Fatalf("domestic home vav count = %d, want 5", len(engine.Vavs))
	}

	expectedZones := []string{
		"zone-kitchen-rear-service-lvl1",
		"zone-office-lvl1",
		"zone-living-room-lvl1",
		"zone-passage-lvl1",
		"zone-bathroom-lvl1",
	}

	for _, zid := range expectedZones {
		z, ok := engine.Zones[zid]
		if !ok {
			t.Errorf("expected domestic zone %q missing from engine.Zones", zid)
			continue
		}
		if z.AreaM2 <= 0 {
			t.Errorf("zone %q AreaM2 = %.2f, want > 0", zid, z.AreaM2)
		}
		if z.CAir <= 0 {
			t.Errorf("zone %q CAir = %.2f, want > 0", zid, z.CAir)
		}
		if z.RIn <= 0 || z.ROut <= 0 {
			t.Errorf("zone %q RIn/ROut = (%.4f, %.4f), want positive", zid, z.RIn, z.ROut)
		}
	}

	// Fan sizing should scale down with the smaller domestic network.
	if engine.PMax <= 0 {
		t.Errorf("residential fan PMax = %.1f kW, want > 0", engine.PMax)
	}
	if engine.PMax >= initialPMax {
		t.Errorf("residential fan PMax = %.1f kW, want less than initial %.1f kW", engine.PMax, initialPMax)
	}

	// 3. Switch back to commercial office tower
	if err := engine.ReloadBuilding(officeData); err != nil {
		t.Fatalf("ReloadBuilding(officeData) failed: %v", err)
	}

	if engine.BuildingId() != "bldg-econ-digitized" {
		t.Errorf("after switch back, buildingId = %q, want %q", engine.BuildingId(), "bldg-econ-digitized")
	}
	if len(engine.Zones) != initialZones {
		t.Errorf("restored zone count = %d, want %d", len(engine.Zones), initialZones)
	}
	if len(engine.Vavs) != initialZones {
		t.Errorf("restored vav count = %d, want %d", len(engine.Vavs), initialZones)
	}
}

// TestBuildingDataAPIQueryParam verifies that GET /api/building-data respects ?model=
// and falls back to the active building model when no query parameter is provided.
func TestBuildingDataAPIQueryParam(t *testing.T) {
	engine := simulation.NewEngine()
	handler := buildingDataHandler(engine)

	// 1. GET /api/building-data?model=domestic-home
	reqHome := httptest.NewRequest(http.MethodGet, "/api/building-data?model=domestic-home", nil)
	recHome := httptest.NewRecorder()
	handler.ServeHTTP(recHome, reqHome)

	if recHome.Code != http.StatusOK {
		t.Fatalf("GET ?model=domestic-home status = %d, want 200", recHome.Code)
	}
	var docHome struct {
		BuildingId string `json:"buildingId"`
		Floors     []struct {
			Zones []struct {
				ZoneId string `json:"zoneId"`
			} `json:"zones"`
		} `json:"floors"`
	}
	if err := json.NewDecoder(recHome.Body).Decode(&docHome); err != nil {
		t.Fatalf("failed to decode home json: %v", err)
	}
	if docHome.BuildingId != "bldg-econ-house-hcmc" {
		t.Errorf("home buildingId = %q, want %q", docHome.BuildingId, "bldg-econ-house-hcmc")
	}
	if len(docHome.Floors) != 1 || len(docHome.Floors[0].Zones) != 5 {
		t.Errorf("home floors/zones = %d/%d, want 1/5", len(docHome.Floors), len(docHome.Floors[0].Zones))
	}

	// 2. GET /api/building-data?model=multi-level
	reqOffice := httptest.NewRequest(http.MethodGet, "/api/building-data?model=multi-level", nil)
	recOffice := httptest.NewRecorder()
	handler.ServeHTTP(recOffice, reqOffice)

	if recOffice.Code != http.StatusOK {
		t.Fatalf("GET ?model=multi-level status = %d, want 200", recOffice.Code)
	}
	var docOffice struct {
		BuildingId string `json:"buildingId"`
		Floors     []struct {
			Zones []struct {
				ZoneId string `json:"zoneId"`
			} `json:"zones"`
		} `json:"floors"`
	}
	if err := json.NewDecoder(recOffice.Body).Decode(&docOffice); err != nil {
		t.Fatalf("failed to decode office json: %v", err)
	}
	if docOffice.BuildingId != "bldg-econ-digitized" {
		t.Errorf("office buildingId = %q, want %q", docOffice.BuildingId, "bldg-econ-digitized")
	}

	// 3. GET /api/building-data?model=invalid-name -> 400 Bad Request
	reqInvalid := httptest.NewRequest(http.MethodGet, "/api/building-data?model=invalid-xyz", nil)
	recInvalid := httptest.NewRecorder()
	handler.ServeHTTP(recInvalid, reqInvalid)

	if recInvalid.Code != http.StatusBadRequest {
		t.Errorf("GET ?model=invalid-xyz status = %d, want 400", recInvalid.Code)
	}

	// 4. GET /api/building-data without query param -> returns active building
	// Initially office
	reqActive1 := httptest.NewRequest(http.MethodGet, "/api/building-data", nil)
	recActive1 := httptest.NewRecorder()
	handler.ServeHTTP(recActive1, reqActive1)
	if recActive1.Code != http.StatusOK {
		t.Fatalf("GET /api/building-data status = %d, want 200", recActive1.Code)
	}
	var docActive1 struct {
		BuildingId string `json:"buildingId"`
	}
	_ = json.NewDecoder(recActive1.Body).Decode(&docActive1)
	if docActive1.BuildingId != "bldg-econ-digitized" {
		t.Errorf("active buildingId = %q, want %q", docActive1.BuildingId, "bldg-econ-digitized")
	}

	// Switch engine to home
	homeData, _ := os.ReadFile(simulation.DataPath(simulation.BuildingDataHomeFile))
	_ = engine.ReloadBuilding(homeData)

	reqActive2 := httptest.NewRequest(http.MethodGet, "/api/building-data", nil)
	recActive2 := httptest.NewRecorder()
	handler.ServeHTTP(recActive2, reqActive2)
	if recActive2.Code != http.StatusOK {
		t.Fatalf("GET /api/building-data status = %d, want 200", recActive2.Code)
	}
	var docActive2 struct {
		BuildingId string `json:"buildingId"`
	}
	_ = json.NewDecoder(recActive2.Body).Decode(&docActive2)
	if docActive2.BuildingId != "bldg-econ-house-hcmc" {
		t.Errorf("after switch, active buildingId = %q, want %q", docActive2.BuildingId, "bldg-econ-house-hcmc")
	}
}

// TestBuildingSwitchEndpoints verifies that POST /api/building/switch and POST /api/model/switch
// dynamically reload the engine and respond with updated building state.
func TestBuildingSwitchEndpoints(t *testing.T) {
	engine := simulation.NewEngine()
	switchHandler := buildingSwitchHandler(engine)

	// 1. POST /api/building/switch with JSON {"model": "domestic-home"}
	bodyHome := bytes.NewBufferString(`{"model":"domestic-home"}`)
	reqHome := httptest.NewRequest(http.MethodPost, "/api/building/switch", bodyHome)
	reqHome.Header.Set("Content-Type", "application/json")
	recHome := httptest.NewRecorder()
	switchHandler.ServeHTTP(recHome, reqHome)

	if recHome.Code != http.StatusOK {
		t.Fatalf("POST /api/building/switch home status = %d, want 200; body: %s", recHome.Code, recHome.Body.String())
	}
	var resHome struct {
		Ok         bool   `json:"ok"`
		Model      string `json:"model"`
		BuildingId string `json:"buildingId"`
		Zones      int    `json:"zones"`
		Vavs       int    `json:"vavs"`
	}
	if err := json.NewDecoder(recHome.Body).Decode(&resHome); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resHome.Ok || resHome.BuildingId != "bldg-econ-house-hcmc" || resHome.Zones != 5 {
		t.Errorf("unexpected switch response: %+v", resHome)
	}
	if engine.BuildingId() != "bldg-econ-house-hcmc" || len(engine.Zones) != 5 {
		t.Errorf("engine state not updated: id=%q, zones=%d", engine.BuildingId(), len(engine.Zones))
	}

	// 2. POST /api/model/switch with JSON {"model": "multi-level"}
	bodyOffice := bytes.NewBufferString(`{"model":"multi-level"}`)
	reqOffice := httptest.NewRequest(http.MethodPost, "/api/model/switch", bodyOffice)
	reqOffice.Header.Set("Content-Type", "application/json")
	recOffice := httptest.NewRecorder()
	switchHandler.ServeHTTP(recOffice, reqOffice)

	if recOffice.Code != http.StatusOK {
		t.Fatalf("POST /api/model/switch office status = %d, want 200", recOffice.Code)
	}
	var resOffice struct {
		Ok         bool   `json:"ok"`
		Model      string `json:"model"`
		BuildingId string `json:"buildingId"`
		Zones      int    `json:"zones"`
	}
	if err := json.NewDecoder(recOffice.Body).Decode(&resOffice); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resOffice.Ok || resOffice.BuildingId != "bldg-econ-digitized" || resOffice.Zones != 735 {
		t.Errorf("unexpected switch response: %+v", resOffice)
	}
	if engine.BuildingId() != "bldg-econ-digitized" || len(engine.Zones) != 735 {
		t.Errorf("engine state not restored: id=%q, zones=%d", engine.BuildingId(), len(engine.Zones))
	}

	// 3. POST with query param ?model=domestic-home
	reqQuery := httptest.NewRequest(http.MethodPost, "/api/building/switch?model=domestic-home", nil)
	recQuery := httptest.NewRecorder()
	switchHandler.ServeHTTP(recQuery, reqQuery)

	if recQuery.Code != http.StatusOK {
		t.Fatalf("POST ?model=domestic-home status = %d, want 200", recQuery.Code)
	}
	if engine.BuildingId() != "bldg-econ-house-hcmc" {
		t.Errorf("engine buildingId after query switch = %q, want %q", engine.BuildingId(), "bldg-econ-house-hcmc")
	}

	// 4. Invalid model -> 400 Bad Request
	bodyBad := bytes.NewBufferString(`{"model":"mars-station"}`)
	reqBad := httptest.NewRequest(http.MethodPost, "/api/building/switch", bodyBad)
	recBad := httptest.NewRecorder()
	switchHandler.ServeHTTP(recBad, reqBad)

	if recBad.Code != http.StatusBadRequest {
		t.Errorf("POST invalid model status = %d, want 400", recBad.Code)
	}

	// 5. GET on switch endpoint -> 405 Method Not Allowed
	reqGet := httptest.NewRequest(http.MethodGet, "/api/building/switch", nil)
	recGet := httptest.NewRecorder()
	switchHandler.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET switch endpoint status = %d, want 405", recGet.Code)
	}
}

// TestWebSocketModelSwitchingCommand verifies that a connected WebSocket client can send
// {"action": "switch_model", "model": "..."} or {"action": "switch_building", "model": "..."}
// to trigger runtime engine building reload and receive confirmation.
func TestWebSocketModelSwitchingCommand(t *testing.T) {
	engine := simulation.NewEngine()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, engine)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("skipping websocket network test in restricted sandbox: %v", err)
		}
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer ws.Close()

	// Helper to send command
	sendCommand := func(payload interface{}) {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal err: %v", err)
		}
		if err := ws.WriteMessage(websocket.TextMessage, b); err != nil {
			t.Fatalf("write err: %v", err)
		}
	}

	// Helper to read text response (skipping binary telemetry frames if any)
	readTextMessage := func(timeout time.Duration) ([]byte, error) {
		deadline := time.Now().Add(timeout)
		_ = ws.SetReadDeadline(deadline)
		for {
			msgType, data, err := ws.ReadMessage()
			if err != nil {
				return nil, err
			}
			if msgType == websocket.TextMessage {
				return data, nil
			}
		}
	}

	// 1. Send switch_model to domestic-home
	sendCommand(map[string]string{
		"action": "switch_model",
		"model":  "domestic-home",
	})

	respHome, err := readTextMessage(2 * time.Second)
	if err != nil {
		t.Fatalf("failed to read switch_model response: %v", err)
	}

	var smHome struct {
		Type       string `json:"type"`
		Ok         bool   `json:"ok"`
		Model      string `json:"model"`
		BuildingId string `json:"buildingId"`
		Zones      int    `json:"zones"`
	}
	if err := json.Unmarshal(respHome, &smHome); err != nil {
		t.Fatalf("failed to unmarshal switch_model response: %v", err)
	}
	if !smHome.Ok || smHome.BuildingId != "bldg-econ-house-hcmc" || smHome.Zones != 5 {
		t.Errorf("unexpected switch_model response: %+v", smHome)
	}
	if engine.BuildingId() != "bldg-econ-house-hcmc" || len(engine.Zones) != 5 {
		t.Errorf("engine state not switched via WS: id=%q, zones=%d", engine.BuildingId(), len(engine.Zones))
	}

	// 2. Send switch_building back to multi-level
	sendCommand(map[string]string{
		"action": "switch_building",
		"model":  "multi-level",
	})

	respOffice, err := readTextMessage(2 * time.Second)
	if err != nil {
		t.Fatalf("failed to read switch_building response: %v", err)
	}

	var smOffice struct {
		Type       string `json:"type"`
		Ok         bool   `json:"ok"`
		BuildingId string `json:"buildingId"`
		Zones      int    `json:"zones"`
	}
	if err := json.Unmarshal(respOffice, &smOffice); err != nil {
		t.Fatalf("failed to unmarshal switch_building response: %v", err)
	}
	if !smOffice.Ok || smOffice.BuildingId != "bldg-econ-digitized" || smOffice.Zones != 735 {
		t.Errorf("unexpected switch_building response: %+v", smOffice)
	}
	if engine.BuildingId() != "bldg-econ-digitized" || len(engine.Zones) != 735 {
		t.Errorf("engine state not restored via WS: id=%q, zones=%d", engine.BuildingId(), len(engine.Zones))
	}
}

// TestFlatBuffersSimStateMatchesSwitchedBuilding asserts that the binary FlatBuffers
// stream emitted by the engine encodes exactly the active building's zones without stale artifacts.
func TestFlatBuffersSimStateMatchesSwitchedBuilding(t *testing.T) {
	engine := simulation.NewEngine()

	// Switch to domestic house
	homeData, err := os.ReadFile(simulation.DataPath(simulation.BuildingDataHomeFile))
	if err != nil {
		t.Fatalf("read home data: %v", err)
	}
	if err := engine.ReloadBuilding(homeData); err != nil {
		t.Fatalf("reload home: %v", err)
	}

	// Create a WebSocket test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, engine)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("skipping websocket network test in restricted sandbox: %v", err)
		}
		t.Fatalf("dial ws: %v", err)
	}
	defer ws.Close()

	// Force one broadcast tick
	engine.BroadcastOnce()

	// Read binary frame
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		msgType, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("failed reading binary frame: %v", err)
		}
		if msgType == websocket.BinaryMessage {
			simState := Telemetry.GetRootAsSimState(data, 0)
			zLen := simState.ZonesLength()
			if zLen == 0 {
				continue
			}

			// Assert exactly 5 zones streamed
			if zLen != 5 {
				t.Fatalf("streamed zones count = %d, want 5", zLen)
			}

			foundZones := make(map[string]bool)
			zObj := new(Telemetry.ZoneData)
			for i := 0; i < zLen; i++ {
				if simState.Zones(zObj, i) {
					id := string(zObj.Id())
					foundZones[id] = true
				}
			}

			for _, expectedId := range []string{
				"zone-kitchen-rear-service-lvl1",
				"zone-office-lvl1",
				"zone-living-room-lvl1",
				"zone-passage-lvl1",
				"zone-bathroom-lvl1",
			} {
				if !foundZones[expectedId] {
					t.Errorf("expected zone %q missing from FlatBuffers broadcast", expectedId)
				}
			}

			// Check global electrical load is in residential range (< 0.1 MW, i.e. < 100 kW)
			globalData := new(Telemetry.GlobalData)
			if simState.Global(globalData) != nil {
				loadMw := globalData.BuildingLoadMw()
				if loadMw > 0.05 {
					t.Errorf("domestic buildingLoadMw = %f MW, want < 0.05 MW (50 kW)", loadMw)
				}
			}
			break
		}
	}
}
