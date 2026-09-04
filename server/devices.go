package main

// TEMPORARY MODULE — hardware bring-up and troubleshooting.
//
// Scope note: this exists so someone wiring a node can answer "is my board talking, what
// is it actually sending, and what did it send an hour ago" without reading MQTT by hand
// or writing SQL. It is deliberately self-contained — one file, its own endpoints, no
// other package imports it — so it can be deleted in one commit once the nodes are stable
// and the main dashboard covers what is worth keeping.
//
// It is NOT a second source of truth. The engine's /api/hardware remains the authority on
// which zones are hardware-bound; this tracks the raw MQTT stream one level below that,
// including messages the engine discards (unmatched topics, malformed payloads), which is
// exactly where bring-up problems live.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// staleAfter is how long a node may go quiet before the inspector calls it offline. Nodes
// publish every 5 s (ESP32) or 2 s (Pico), so three missed cycles is a real gap rather
// than jitter — short enough to notice a pulled wire while you are still holding it.
const staleAfter = 20 * time.Second

// fieldStat is the per-metric health of one device: not just the latest value but whether
// it is still arriving. A field that stopped updating is the signature of a single failed
// sensor on an otherwise healthy board, which a whole-node online flag cannot show.
type fieldStat struct {
	Last    float64   `json:"last"`
	At      time.Time `json:"at"`
	Count   int64     `json:"count"`
	Min     float64   `json:"min"`
	Max     float64   `json:"max"`
	Omitted int64     `json:"omitted"` // messages where this field was absent
}

// device is one physical node as seen from the MQTT stream.
type device struct {
	ID        string    `json:"id"`     // topic suffix — the node's identity on the bus
	Source    string    `json:"source"` // "esp32" | "pico" | CV node
	Zone      string    `json:"zone"`   // human label the node reports
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Messages  int64     `json:"messages"`
	Malformed int64     `json:"malformed"`

	// What the firmware claims about its own honesty. These mirror the payload flags and
	// are the fastest way to see that a node is publishing modelled numbers: a board with
	// tempReal=false is working correctly and still must not be trusted as evidence.
	TempReal bool `json:"tempReal"`
	AcReal   bool `json:"acReal"`
	AcKnown  bool `json:"acKnown"`
	// Node runtime-configuration revision, and how many times it has changed while we have
	// been watching. A recalibration rewrites the meaning of this device's power series, so
	// "when did it last change" is the first question to ask of a step in plugW that has no
	// matching step in occupancy.
	CfgRev     uint32 `json:"cfgRev"`
	CfgKnown   bool   `json:"cfgKnown"`
	CfgChanges int64  `json:"cfgChanges"`

	Fields map[string]*fieldStat `json:"fields"`

	online   bool
	lastJSON string
}

type deviceRegistry struct {
	mu sync.RWMutex
	d  map[string]*device
}

var registry = &deviceRegistry{d: map[string]*device{}}

// observe records one telemetry message. Called from handleTelemetry on every payload,
// including ones the engine cannot bind to a zone — an unbound node is still a node, and
// "it publishes but nothing matches it" is a bring-up failure worth seeing.
func (r *deviceRegistry) observe(topicSuffix string, msg telemetryMsg, raw []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	dev, ok := r.d[topicSuffix]
	if !ok {
		dev = &device{ID: topicSuffix, FirstSeen: time.Now(), Fields: map[string]*fieldStat{}}
		r.d[topicSuffix] = dev
		log.Printf("[devices] first sight of %q", topicSuffix)
		persistDeviceEvent(topicSuffix, "first-seen", msg.Source)
	}
	if !dev.online {
		if !dev.LastSeen.IsZero() {
			gap := time.Since(dev.LastSeen).Round(time.Second)
			log.Printf("[devices] %q back after %s", topicSuffix, gap)
			persistDeviceEvent(topicSuffix, "reconnect", "gap="+gap.String())
		}
		dev.online = true
	}

	dev.LastSeen = time.Now()
	dev.Messages++
	dev.Source = msg.Source
	if msg.Zone != "" {
		dev.Zone = msg.Zone
	}
	dev.TempReal = msg.TempReal
	if msg.AcReal != nil {
		dev.AcReal, dev.AcKnown = *msg.AcReal, true
	}
	if msg.CfgRev != nil {
		// A revision bump means the node's calibration was changed underneath the series we
		// are storing. Recorded as a device event, in the same table as dropouts, because
		// that is where someone investigating a discontinuity will already be looking.
		if dev.CfgKnown && *msg.CfgRev != dev.CfgRev {
			dev.CfgChanges++
			log.Printf("[devices] %q config revision %d -> %d: its calibrated series are no "+
				"longer comparable across this point", topicSuffix, dev.CfgRev, *msg.CfgRev)
			persistDeviceEvent(topicSuffix, "config-change",
				fmt.Sprintf("cfgRev %d -> %d", dev.CfgRev, *msg.CfgRev))
		}
		dev.CfgRev, dev.CfgKnown = *msg.CfgRev, true
	}
	dev.lastJSON = string(raw)

	// Every numeric field the contract defines, recorded present-or-absent. Absence is
	// tracked because the firmware *omits* a field when its sensor fails rather than
	// sending a placeholder — so a rising Omitted count is the sensor failing, and it is
	// invisible if you only look at the values that did arrive.
	track := func(name string, v *float64) {
		fs, ok := dev.Fields[name]
		if !ok {
			fs = &fieldStat{Min: 1e18, Max: -1e18}
			dev.Fields[name] = fs
		}
		if v == nil {
			fs.Omitted++
			return
		}
		fs.Last, fs.At, fs.Count = *v, time.Now(), fs.Count+1
		if *v < fs.Min {
			fs.Min = *v
		}
		if *v > fs.Max {
			fs.Max = *v
		}
		// Persist with provenance. Only fields that actually arrived are written, and
		// they are written as measured against this device id.
		persistMeasured(topicSuffix, dev.Zone, name, *v)
	}

	track("temperature", msg.Temperature)
	track("humidity", msg.Humidity)
	track("co2", msg.Co2)
	track("plugW", msg.PlugW)
	track("supplyC", msg.SupplyC)
	track("acW", msg.AcW)
	track("lux", msg.Lux)
	track("stripW", msg.StripW)
	if msg.Occupancy != nil {
		occ := float64(*msg.Occupancy)
		track("occupancy", &occ)
	} else {
		track("occupancy", nil)
	}
}

func (r *deviceRegistry) observeMalformed(topicSuffix string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dev, ok := r.d[topicSuffix]
	if !ok {
		dev = &device{ID: topicSuffix, FirstSeen: time.Now(), Fields: map[string]*fieldStat{}}
		r.d[topicSuffix] = dev
	}
	dev.Malformed++
	dev.LastSeen = time.Now()
}

// sweep marks nodes offline once they go quiet, so the inspector reports a dropout even
// when nothing is arriving to trigger an update.
func (r *deviceRegistry) sweep() {
	for range time.Tick(5 * time.Second) {
		r.mu.Lock()
		for id, dev := range r.d {
			if dev.online && time.Since(dev.LastSeen) > staleAfter {
				dev.online = false
				log.Printf("[devices] %q went quiet (last seen %s ago)", id,
					time.Since(dev.LastSeen).Round(time.Second))
				persistDeviceEvent(id, "offline", "silent > "+staleAfter.String())
			}
		}
		r.mu.Unlock()
	}
}

// deviceView is the JSON shape returned to the inspector UI.
type deviceView struct {
	*device
	Online    bool    `json:"online"`
	AgeSec    float64 `json:"ageSec"`
	RateHz    float64 `json:"rateHz"`
	LastJSON  string  `json:"lastJson"`
	Bound     bool    `json:"bound"`     // does the engine have this node bound to a zone?
	BoundZone string  `json:"boundZone"` // which zone id, if so
}

func devicesHandler(engineBound func() map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bound := engineBound()
		registry.mu.RLock()
		defer registry.mu.RUnlock()

		out := make([]deviceView, 0, len(registry.d))
		for id, dev := range registry.d {
			age := time.Since(dev.LastSeen).Seconds()
			rate := 0.0
			if span := dev.LastSeen.Sub(dev.FirstSeen).Seconds(); span > 0 {
				rate = float64(dev.Messages) / span
			}
			zoneId, isBound := bound[id]
			out = append(out, deviceView{
				device: dev, Online: dev.online, AgeSec: age, RateHz: rate,
				LastJSON: dev.lastJSON, Bound: isBound, BoundZone: zoneId,
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

		writeJSON(w, map[string]interface{}{
			"devices":    out,
			"staleAfter": staleAfter.Seconds(),
			"now":        time.Now(),
		})
	}
}

// deviceSeriesHandler serves one device's history for one metric, straight from the rows
// that carry its device_id. GET /api/devices/series?device=zone_1&metric=temperature&minutes=60
func deviceSeriesHandler(w http.ResponseWriter, r *http.Request) {
	if DB == nil {
		http.Error(w, `{"error":"no database"}`, http.StatusServiceUnavailable)
		return
	}
	dev := r.URL.Query().Get("device")
	metric := r.URL.Query().Get("metric")
	if dev == "" || metric == "" {
		http.Error(w, `{"error":"device and metric are required"}`, http.StatusBadRequest)
		return
	}
	minutes := 60
	if m := r.URL.Query().Get("minutes"); m != "" {
		if n, err := strconv.Atoi(m); err == nil && n > 0 && n <= 10080 {
			minutes = n
		}
	}
	rows, err := DB.Query(
		`SELECT time, value, quality FROM sensor_readings
		  WHERE device_id = $1 AND sensor_type = $2 AND time > NOW() - ($3 || ' minutes')::INTERVAL
		  ORDER BY time`, dev, metric, strconv.Itoa(minutes))
	if err != nil {
		log.Printf("[devices] series query failed: %v", err)
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type pt struct {
		T time.Time `json:"t"`
		V float64   `json:"v"`
		Q string    `json:"q"`
	}
	out := []pt{}
	for rows.Next() {
		var p pt
		var q *string
		if err := rows.Scan(&p.T, &p.V, &q); err != nil {
			continue
		}
		if q != nil {
			p.Q = *q
		}
		out = append(out, p)
	}
	writeJSON(w, map[string]interface{}{"device": dev, "metric": metric, "points": out})
}

// deviceEventsHandler serves the lifecycle log. Omit ?device= for every node.
func deviceEventsHandler(w http.ResponseWriter, r *http.Request) {
	if DB == nil {
		http.Error(w, `{"error":"no database"}`, http.StatusServiceUnavailable)
		return
	}
	dev := r.URL.Query().Get("device")
	q := `SELECT time, device_id, event, COALESCE(detail,'') FROM device_events`
	args := []interface{}{}
	if dev != "" {
		q += ` WHERE device_id = $1`
		args = append(args, dev)
	}
	q += ` ORDER BY time DESC LIMIT 200`

	rows, err := DB.Query(q, args...)
	if err != nil {
		log.Printf("[devices] events query failed: %v", err)
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type ev struct {
		T      time.Time `json:"t"`
		Device string    `json:"device"`
		Event  string    `json:"event"`
		Detail string    `json:"detail"`
	}
	out := []ev{}
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.T, &e.Device, &e.Event, &e.Detail); err == nil {
			out = append(out, e)
		}
	}
	writeJSON(w, map[string]interface{}{"events": out})
}

// dataQualityHandler answers "how much of my stored history is actually evidence?" — a
// breakdown of rows by quality and device. This is the classification made visible: if a
// week of history is 95% modelled, any conclusion drawn from it is a claim about the
// simulation, not about the building.
func dataQualityHandler(w http.ResponseWriter, r *http.Request) {
	if DB == nil {
		http.Error(w, `{"error":"no database"}`, http.StatusServiceUnavailable)
		return
	}
	// Window this hard. The engine persists every zone every second, so a full building
	// is ~1500 rows/s and a 7-day scan is tens of millions of rows — measured at 40 s,
	// which reads as a hung page rather than a slow one. An hour is enough to answer
	// "what is arriving now"; widen deliberately with ?minutes= when you want more.
	minutes := 60
	if m := r.URL.Query().Get("minutes"); m != "" {
		if n, err := strconv.Atoi(m); err == nil && n > 0 && n <= 10080 {
			minutes = n
		}
	}
	rows, err := DB.Query(
		`SELECT COALESCE(quality,'unclassified') AS q,
		        COALESCE(device_id,'(engine)')   AS d,
		        sensor_type, COUNT(*), MIN(time), MAX(time)
		   FROM sensor_readings
		  WHERE time > NOW() - ($1 || ' minutes')::INTERVAL
		  GROUP BY q, d, sensor_type
		  ORDER BY COUNT(*) DESC LIMIT 200`, strconv.Itoa(minutes))
	if err != nil {
		log.Printf("[devices] quality query failed: %v", err)
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type row struct {
		Quality string    `json:"quality"`
		Device  string    `json:"device"`
		Metric  string    `json:"metric"`
		Rows    int64     `json:"rows"`
		From    time.Time `json:"from"`
		To      time.Time `json:"to"`
	}
	out := []row{}
	totals := map[string]int64{}
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.Quality, &x.Device, &x.Metric, &x.Rows, &x.From, &x.To); err == nil {
			out = append(out, x)
			totals[x.Quality] += x.Rows
		}
	}
	writeJSON(w, map[string]interface{}{"breakdown": out, "totals": totals, "minutes": minutes})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

// registerDeviceRoutes wires the inspector. Read-only: nothing here actuates, so it is
// safe to leave reachable during bring-up without widening the control surface auth.go
// closed. Deleting this call and devices.go removes the module entirely.
func registerDeviceRoutes(engineBound func() map[string]string) {
	http.HandleFunc("/api/devices", devicesHandler(engineBound))
	http.HandleFunc("/api/devices/series", deviceSeriesHandler)
	http.HandleFunc("/api/devices/events", deviceEventsHandler)
	http.HandleFunc("/api/devices/quality", dataQualityHandler)
	go registry.sweep()
	log.Println("[devices] hardware inspector at /api/devices (TEMPORARY bring-up module)")
}

// topicSuffixOf is a local helper so this file does not depend on mqtt.go's unexported one.
func topicSuffixOf(topic string) string {
	if i := strings.LastIndex(topic, "/"); i >= 0 {
		return topic[i+1:]
	}
	return topic
}
