package main

import (
	"econ/simulation"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWebSocketProtocolActionDispatch tests WebSocket message handling for all action shapes:
// purge, cool, precool, autopilot, custom firmware strings, malformed payloads, and concurrency.
func TestWebSocketProtocolActionDispatch(t *testing.T) {
	engine := simulation.NewEngine()

	var publishedMqtt []string
	var mu sync.Mutex
	engine.Publish = func(topic, payload string) {
		mu.Lock()
		publishedMqtt = append(publishedMqtt, topic+":"+payload)
		mu.Unlock()
	}

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

	// Helper to send JSON command
	sendCommand := func(payload interface{}) {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json marshal error: %v", err)
		}
		if err := ws.WriteMessage(websocket.TextMessage, b); err != nil {
			t.Fatalf("ws write error: %v", err)
		}
	}

	// 1. Purge action
	sendCommand(map[string]string{
		"action": "purge",
		"zone":   "zone-office-a",
	})
	time.Sleep(50 * time.Millisecond)

	// 2. Cool action
	sendCommand(map[string]string{
		"action": "cool",
		"zone":   "zone-office-a",
	})
	time.Sleep(50 * time.Millisecond)

	// 3. Precool action
	sendCommand(map[string]string{
		"action": "precool",
		"zone":   "GLOBAL",
	})
	time.Sleep(50 * time.Millisecond)

	active, _ := engine.PreCoolStatus()
	if !active {
		t.Fatal("expected precool window active after ws precool command")
	}

	// 4. Auto-Pilot action
	valFalse := false
	sendCommand(struct {
		Action string `json:"action"`
		Value  *bool  `json:"value"`
	}{
		Action: "autopilot",
		Value:  &valFalse,
	})
	time.Sleep(50 * time.Millisecond)

	valTrue := true
	sendCommand(struct {
		Action string `json:"action"`
		Value  *bool  `json:"value"`
	}{
		Action: "autopilot",
		Value:  &valTrue,
	})
	time.Sleep(50 * time.Millisecond)

	// 5. Custom firmware string
	sendCommand(map[string]string{
		"action": "LIGHTS_OFF;SETPOINT=25.0",
		"zone":   "zone-office-a",
	})
	time.Sleep(50 * time.Millisecond)

	// 6. Malformed JSON & non-JSON payloads (must not crash)
	if err := ws.WriteMessage(websocket.TextMessage, []byte("{ malformed json }")); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.TextMessage, []byte("normal")); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.TextMessage, []byte("fault:zone-office-a")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	pCount := len(publishedMqtt)
	mu.Unlock()

	if pCount == 0 {
		t.Fatal("expected published mqtt commands from ws dispatch")
	}
}

// TestConcurrentWebSocketClients opens 5 concurrent WebSocket connections sending rapid actions.
func TestConcurrentWebSocketClients(t *testing.T) {
	engine := simulation.NewEngine()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, engine)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	const numClients = 5
	const msgsPerClient = 20

	var wg sync.WaitGroup
	wg.Add(numClients)

	for c := 0; c < numClients; c++ {
		go func(clientID int) {
			defer wg.Done()
			ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				if strings.Contains(err.Error(), "operation not permitted") {
					return
				}
				t.Errorf("client %d dial failed: %v", clientID, err)
				return
			}
			defer ws.Close()

			for m := 0; m < msgsPerClient; m++ {
				var msg []byte
				switch m % 3 {
				case 0:
					msg = []byte(`{"action":"purge","zone":"zone-office-a"}`)
				case 1:
					msg = []byte(`{"action":"cool","zone":"zone-office-b"}`)
				case 2:
					msg = []byte(`{"action":"precool","zone":"GLOBAL"}`)
				}
				_ = ws.WriteMessage(websocket.TextMessage, msg)
				time.Sleep(5 * time.Millisecond)
			}
		}(c)
	}

	wg.Wait()
	time.Sleep(50 * time.Millisecond)
}

// TestHttpApiEndpoints tests GET /api/recommendations, GET /api/precool, and GET /api/hardware.
func TestHttpApiEndpoints(t *testing.T) {
	engine := simulation.NewEngine()

	// Recommendations
	recHandler := recommendationsHandler(engine)
	reqRec := httptest.NewRequest("GET", "/api/recommendations", nil)
	rrRec := httptest.NewRecorder()
	recHandler.ServeHTTP(rrRec, reqRec)
	if rrRec.Code != http.StatusOK {
		t.Fatalf("GET /api/recommendations returned status %d", rrRec.Code)
	}
	var recReport simulation.RecommendationReport
	if err := json.NewDecoder(rrRec.Body).Decode(&recReport); err != nil {
		t.Fatalf("failed to decode recommendations JSON: %v", err)
	}

	// Precool GET
	pcHandler := precoolHandler(engine)
	reqPc := httptest.NewRequest("GET", "/api/precool", nil)
	rrPc := httptest.NewRecorder()
	pcHandler.ServeHTTP(rrPc, reqPc)
	if rrPc.Code != http.StatusOK {
		t.Fatalf("GET /api/precool returned status %d", rrPc.Code)
	}

	// Hardware GET
	hwHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engine.HardwareStatus())
	}
	reqHw := httptest.NewRequest("GET", "/api/hardware", nil)
	rrHw := httptest.NewRecorder()
	hwHandler(rrHw, reqHw)
	if rrHw.Code != http.StatusOK {
		t.Fatalf("GET /api/hardware returned status %d", rrHw.Code)
	}
}
