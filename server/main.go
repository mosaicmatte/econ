package main

import (
	"econ/simulation"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for dev
	},
}

func main() {
	// 1. Serve static building data
	http.HandleFunc("/api/building-data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		data, err := os.ReadFile("./data/building-data.json")
		if err != nil {
			http.Error(w, "Failed to read building data", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// 2. Serve Brick Ontology Data
	http.HandleFunc("/api/ontology", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		data, err := os.ReadFile("./data/brick-ontology.json")
		if err != nil {
			http.Error(w, "Failed to read ontology data", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Initialize simulation engine
	engine := simulation.NewEngine()

	// [GEMINI IMPLEMENTATION START]
	initDB()
	engine.Persist = persistReading
	http.HandleFunc("/api/history", historyHandler)
	// [GEMINI IMPLEMENTATION END]

	// Generic per-zone, per-metric time series (TimescaleDB). The read path for
	// everything the engine persists beyond the two hardcoded history charts — most
	// importantly the AFDD residual, the queryable drift history behind a "dispatch
	// technician" alert. GET /api/series?zone=&metric=&minutes=
	http.HandleFunc("/api/series", seriesHandler)

	// 3. Peak-load forecast: proxy a live-telemetry window to the Python LSTM service.
	http.HandleFunc("/api/forecast", forecastHandler(engine))

	// 4. Physical edge nodes (ESP32 / Pico): which zones are hardware-bound right now.
	// The dashboard polls this to badge zones that mirror a real device.
	http.HandleFunc("/api/hardware", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(engine.HardwareStatus())
	})

	// 5. Forecast-driven pre-cooling: GET = window status, POST = open a window now.
	// The background poller (precool.go) opens windows automatically off the LSTM.
	http.HandleFunc("/api/precool", precoolHandler(engine))

	// 6. Live weather: what outdoor temperature the 2R1C envelope is using, and whether
	// it is live Open-Meteo data or the climatological fallback.
	http.HandleFunc("/api/weather", weatherHandler(engine))

	// 7. Blueprint import: digitize a real-world drawing for review, then deploy it as
	// the running building. Deploy/rollback are guarded by ECON_ADMIN_TOKEN when set,
	// back up the previous building automatically, and append to the audit log
	// (blueprint.go).
	http.HandleFunc("/api/digitize", digitizeHandler())
	http.HandleFunc("/api/building", deployBuildingHandler(engine))
	http.HandleFunc("/api/building/backups", backupsHandler())
	http.HandleFunc("/api/building/rollback", rollbackHandler(engine))

	// 8. Plug-load management (APLC): the end use a conventional BMS neither meters nor
	// controls — and the largest one in the Hanoi case study (26.4% of energy). GET =
	// snapshot + phantom leaderboard; POST = sweep policy (guarded, audited, durable).
	loadPlugState(engine)
	http.HandleFunc("/api/plugs", plugsHandler(engine))

	// Connect to the MQTT broker: ingest real occupancy from the CV/edge layer and
	// publish actuation commands to the ESP32. Non-blocking; the sim runs regardless.
	startMQTT(engine)

	go engine.Start()
	go precoolLoop(engine)
	go weatherLoop(engine)
	go plugPersistLoop(engine)

	// 2. WebSocket endpoint for telemetry streaming
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, engine)
	})

	// Start server
	port := "8080"
	log.Printf("ECON Enterprise Backend running on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, engine *simulation.Engine) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	engine.AddClient(conn)
	defer engine.RemoveClient(conn)

	log.Println("New telemetry client connected!")

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Client disconnected")
			break
		}
		log.Printf("Received command: %s", string(msg))
		// Isolate command handling: a panic here must never take down the whole
		// backend (and thus stop the telemetry stream for every client).
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("recovered from panic handling command %q: %v", string(msg), r)
				}
			}()
			// [GEMINI IMPLEMENTATION START]
			// Added by Gemini (Antigravity) on June 2026.
			// This block intercepts JSON payloads for manual override vetos
			// sent from the dashboard, parsing them to trigger PublishCommand
			// while leaving legacy string scenarios intact.
			strMsg := string(msg)
			if len(strMsg) > 0 && strMsg[0] == '{' {
				var override map[string]string
				if err := json.Unmarshal(msg, &override); err == nil {
					if action, ok := override["action"]; ok {
						if action == "precool" {
							// Building-wide strategy, not a per-zone veto: open a
							// pre-cool window ahead of the predicted demand peak.
							until := engine.StartPreCool(precoolWindow)
							log.Printf("[precool] dashboard opened window until %s", until.Format("15:04:05"))
							return
						}
						if zone, ok := override["zone"]; ok {
							engine.PublishCommand(action, zone)
							return
						}
					}
				}
			}
			// [GEMINI IMPLEMENTATION END]
			engine.SetScenario(strMsg)
		}()
	}
}
