package main

import (
	"econ/simulation"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
)

// checkOrigin lives in auth.go. Browsers do not apply the same-origin policy to
// WebSockets, so this is the only thing standing between a page the operator happened to
// open and a socket that can switch the building.
var upgrader = websocket.Upgrader{CheckOrigin: checkOrigin}

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

	// 9. Learned recommendations (recommendapi.go): the ranked, per-zone anomaly report
	// scored against the online baseline model that replaced the hardcoded threshold
	// cards. The model is restored at boot and checkpointed once a minute so its learned
	// "normal" survives a redeploy.
	loadBaselineState(engine)
	http.HandleFunc("/api/recommendations", recommendationsHandler(engine))

	// 9b. Learned room dynamics (simulation/dynamics.go): the online system identification
	// that gives every room its own physical model — thermal time constant, the cooling its
	// VAV actually delivers, its measured air-change rate. It is what makes the report
	// predictive ("this room breaches in 14 min") instead of only reactive, and it is
	// restored and checkpointed alongside the baselines.
	loadDynamicsState(engine)
	loadLoadHistoryState(engine)
	http.HandleFunc("/api/rooms/models", roomModelsHandler(engine))

	// 10. Downloadable local models (modelexport.go): package the learned baseline model,
	// the identified room models, the LSTM forecaster artifacts, and dependency-free
	// runtimes into one zip so the operator can run recommendations, alerts and room
	// predictions offline from the twin's own processed state. /api/model/recommend sizes
	// that bundle to the machine that will actually run it (modelcatalog.go).
	http.HandleFunc("/api/model", modelInfoHandler(engine))
	http.HandleFunc("/api/model/export", modelExportHandler(engine))
	http.HandleFunc("/api/model/recommend", modelRecommendHandler())

	// 11. Zero-shot load forecasting (forecast.go -> backend/forecasting): Google TimesFM
	// is a pretrained time-series foundation model, so it forecasts this building's load
	// with no training and no fitted scaler. That covers the supervised LSTM's cold start,
	// where it has nothing to offer until train.py has real accumulated history.
	http.HandleFunc("/api/forecast/load", loadForecastHandler(engine))
	http.HandleFunc("/api/forecast/engines", forecastEnginesHandler())

	// Connect to the MQTT broker: ingest real occupancy from the CV/edge layer and
	// publish actuation commands to the ESP32. Non-blocking; the sim runs regardless.
	startMQTT(engine)

	go engine.Start()
	go precoolLoop(engine)
	go weatherLoop(engine)
	go plugPersistLoop(engine)
	go baselinePersistLoop(engine)

	// 2. WebSocket endpoint for telemetry streaming
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, engine)
	})

	// Start server. PORT is overridable so a second engine can be run alongside a live
	// one — the identification soak takes hours, and having to kill it to test an
	// unrelated change is how a soak never finishes.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	logAuthPosture()
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

	// Telemetry is readable by anyone who got this far; control is not. A connection
	// starts unauthorized and stays that way until it presents the token, so the
	// read-only case (a wall display, a read-only viewer) needs no credential at all.
	// In demo mode (no ECON_ADMIN_TOKEN) every connection is authorized on arrival —
	// the engine warns about that at boot rather than silently pretending otherwise.
	authorized := !authEnforced()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Client disconnected")
			break
		}
		// The auth handshake is handled before the command log line: a token must never
		// be written to the log, and every other message is safe to record.
		if tok, isAuth := parseAuthMessage(msg); isAuth {
			authorized = tokenMatches(tok)
			if authorized {
				log.Println("[auth] websocket client authorized for control")
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"auth","ok":true}`))
			} else {
				log.Println("[auth] websocket client presented an invalid token")
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"auth","ok":false}`))
			}
			continue
		}
		if !authorized {
			log.Printf("[auth] refused command from unauthorized websocket client: %s", string(msg))
			_ = conn.WriteMessage(websocket.TextMessage,
				[]byte(`{"type":"error","error":"unauthorized: send {\"action\":\"auth\",\"token\":\"...\"} before issuing commands"}`))
			continue
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
				// Auto-Pilot toggle carries a bool value, so it is parsed first with its
				// own shape (the override map below is map[string]string and would reject
				// it). This is what makes the dashboard's AI switch a real engine control.
				var ap struct {
					Action string `json:"action"`
					Value  *bool  `json:"value"`
				}
				if json.Unmarshal(msg, &ap) == nil && ap.Action == "autopilot" && ap.Value != nil {
					engine.SetAutoPilot(*ap.Value)
					return
				}
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
