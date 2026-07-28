package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// [GEMINI IMPLEMENTATION START]
// Added by Gemini (Antigravity) on June 2026.
// Handles connection to TimescaleDB and inserts/queries for history.

var DB *sql.DB

// Quality classifies where a persisted number came from. The live telemetry stream has
// always drawn this line (tempReal, acReal, Co2Live) but history did not: every row went
// into sensor_readings identically, so a modelled zone temperature and one measured by an
// SHT30 were indistinguishable once written. That defeats the point of the distinction —
// an operator reading a week-old chart could not tell which curve was evidence.
const (
	QualityMeasured = "measured" // a physical sensor reported this value
	QualityModelled = "modelled" // the engine computed it (2R1C, occupancy schedule, ...)
	QualityDerived  = "derived"  // aggregated or arithmetic over other series (GLOBAL rollups)
)

// reading is one buffered metric sample awaiting a batched insert.
type reading struct {
	t       time.Time
	zone    string
	stype   string
	value   float64
	device  string // MQTT topic suffix of the node that reported it; "" = engine-computed
	quality string // one of the Quality* constants
}

// writeCh decouples the engine's broadcast goroutine (30 fps hot path) from the
// database. persistReading only enqueues; writeLoop batches and flushes. nil until
// initDB succeeds.
var writeCh chan reading

func initDB() {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = "postgres://econ:econ@localhost:5432/econ?sslmode=disable"
	}

	var err error
	DB, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("[db] Failed to open DB: %v", err)
		DB = nil
		return
	}

	if err = DB.Ping(); err != nil {
		log.Printf("[db] DB not reachable (is the container up?): %v", err)
		DB = nil
		return
	}
	DB.SetMaxOpenConns(8)
	DB.SetMaxIdleConns(4)

	migrateSchema()

	writeCh = make(chan reading, 8192)
	go writeLoop()
	log.Println("[db] Connected to TimescaleDB.")
}

// migrateSchema brings an existing database up to the current shape. Every statement is
// idempotent, because this runs on every boot against databases created by any earlier
// version — db/init.sql only ever runs on a *fresh* volume, so it cannot be the only
// place the schema is defined.
func migrateSchema() {
	stmts := []string{
		// Provenance for every row: which physical node reported it, and whether it was
		// measured or modelled. Nullable and unindexed-by-default so old rows stay valid;
		// they simply carry NULL, which reads as "written before provenance was tracked".
		`ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS device_id TEXT`,
		`ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS quality   TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_readings_device ON sensor_readings (device_id, time DESC)`,

		// Node lifecycle, separate from the sample stream. A dropout is an event, not a
		// reading: it has no value, and averaging it would be meaningless. Keeping it in
		// its own table is what lets "this board fell off the bus at 14:02" be answerable.
		`CREATE TABLE IF NOT EXISTS device_events (
		   time      TIMESTAMPTZ NOT NULL,
		   device_id TEXT NOT NULL,
		   event     TEXT NOT NULL,
		   detail    TEXT
		 )`,
		`CREATE INDEX IF NOT EXISTS idx_device_events ON device_events (device_id, time DESC)`,
	}
	for _, s := range stmts {
		if _, err := DB.Exec(s); err != nil {
			log.Printf("[db] migration step failed (continuing): %v", err)
		}
	}
}

// persistReading is called once per metric per zone from the engine. It must never
// block the broadcast goroutine, so it only enqueues; if the buffer is saturated the
// sample is dropped (history is best-effort, the live stream is the source of truth).
func persistReading(zoneId, sensorType string, value float64) {
	q := QualityModelled
	if zoneId == "GLOBAL" {
		q = QualityDerived // building rollups are arithmetic over zones, not observations
	}
	enqueue(reading{t: time.Now(), zone: zoneId, stype: sensorType, value: value, quality: q})
}

// persistMeasured records a value that came off a physical sensor, attributed to the node
// that reported it. This is the counterpart to persistReading: same table, same batching,
// but the row carries provenance so a later reader can separate evidence from simulation.
func persistMeasured(deviceId, zoneId, sensorType string, value float64) {
	enqueue(reading{t: time.Now(), zone: zoneId, stype: sensorType, value: value,
		device: deviceId, quality: QualityMeasured})
}

func enqueue(r reading) {
	if writeCh == nil {
		return
	}
	select {
	case writeCh <- r:
	default:
		// buffer full — drop rather than stall the engine.
	}
}

// persistDeviceEvent records a node lifecycle transition. Unlike a reading this is written
// directly rather than batched: events are rare, and one that is lost to a full buffer is
// exactly the one being investigated.
func persistDeviceEvent(deviceId, event, detail string) {
	if DB == nil {
		return
	}
	go func() {
		_, err := DB.Exec(
			`INSERT INTO device_events (time, device_id, event, detail) VALUES ($1,$2,$3,$4)`,
			time.Now(), deviceId, event, detail)
		if err != nil {
			log.Printf("[db] device event insert failed: %v", err)
		}
	}()
}

// writeLoop drains writeCh and flushes batched multi-row inserts, either when a batch
// fills or on a short timer, so a full building's worth of samples costs one round-trip
// instead of hundreds.
func writeLoop() {
	const maxBatch = 512
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	buf := make([]reading, 0, maxBatch)
	flush := func() {
		if len(buf) == 0 || DB == nil {
			buf = buf[:0]
			return
		}
		var sb strings.Builder
		sb.WriteString("INSERT INTO sensor_readings (time, zone_id, sensor_type, value, device_id, quality) VALUES ")
		args := make([]interface{}, 0, len(buf)*6)
		for i, r := range buf {
			if i > 0 {
				sb.WriteByte(',')
			}
			n := i * 6
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d)", n+1, n+2, n+3, n+4, n+5, n+6)
			var dev interface{} // NULL rather than "" so "no device" is unambiguous in SQL
			if r.device != "" {
				dev = r.device
			}
			args = append(args, r.t, r.zone, r.stype, r.value, dev, r.quality)
		}
		if _, err := DB.Exec(sb.String(), args...); err != nil {
			log.Printf("[db] batch insert failed (%d rows): %v", len(buf), err)
		}
		buf = buf[:0]
	}

	for {
		select {
		case r := <-writeCh:
			buf = append(buf, r)
			if len(buf) >= maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func historyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if DB == nil {
		w.Write([]byte("[]"))
		return
	}

	zone := r.URL.Query().Get("zone")
	if zone == "" {
		zone = "GLOBAL"
	}

	minutesStr := r.URL.Query().Get("minutes")
	limit := 60
	if minutesStr != "" {
		if m, err := strconv.Atoi(minutesStr); err == nil && m > 0 {
			limit = m * 60
		}
	}

	var rows *sql.Rows
	var err error

	if zone == "GLOBAL" {
		rows, err = DB.Query(`
			SELECT
				to_char(time_bucket('1 second', time), 'HH24:MI:SS') as time_str,
				MAX(CASE WHEN sensor_type = 'buildingLoadMw' THEN value ELSE 0 END) * 1000 AS pwr,
				MAX(CASE WHEN sensor_type = 'avgCo2' THEN value ELSE 0 END) AS co2
			FROM sensor_readings
			WHERE zone_id = 'GLOBAL'
			GROUP BY time_bucket('1 second', time)
			ORDER BY time_bucket('1 second', time) DESC
			LIMIT $1
		`, limit)
	} else {
		rows, err = DB.Query(`
			SELECT
				to_char(time_bucket('1 second', time), 'HH24:MI:SS') as time_str,
				MAX(CASE WHEN sensor_type = 'temp' THEN value ELSE 0 END) AS pwr,
				MAX(CASE WHEN sensor_type = 'occupancy' THEN value ELSE 0 END) AS co2
			FROM sensor_readings
			WHERE zone_id = $1
			GROUP BY time_bucket('1 second', time)
			ORDER BY time_bucket('1 second', time) DESC
			LIMIT $2
		`, zone, limit)
	}

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type histItem struct {
		Time string  `json:"time"`
		Pwr  float64 `json:"pwr"`
		Co2  float64 `json:"co2"`
	}
	res := []histItem{} // non-nil so an empty result marshals as [] rather than null
	for rows.Next() {
		var item histItem
		if err := rows.Scan(&item.Time, &item.Pwr, &item.Co2); err == nil {
			res = append([]histItem{item}, res...) // prepend to get chronological order
		}
	}

	importJson, _ := json.Marshal(res)
	w.Write(importJson)
}

// [GEMINI IMPLEMENTATION END]

// seriesAllowed is the set of metrics the generic series endpoint will serve. It is an
// allow-list, not because sensor_type is interpolated (it is parameterized), but because
// the endpoint is a public read surface: naming exactly what a client may pull keeps it a
// telemetry API instead of an open query into whatever the engine happens to persist.
var seriesAllowed = map[string]bool{
	"temp": true, "occupancy": true, "humidity": true, "co2": true,
	"afddResidual": true, "buildingLoadMw": true, "coolingOutputMw": true,
	"systemHealth": true, "avgCo2": true, "plugKw": true,
	// Plant efficiency: the modelled curve and, where an AC clamp is fitted, the COP the
	// plant is actually achieving. Charting them together is how a drifting chiller shows up.
	"plantCop": true, "measuredCop": true, "meteredAcKw": true,
	// Feature series persisted for offline LSTM retraining (see engine.go); also
	// chartable through this read path.
	"avgTemp": true, "avgAirflow": true, "outdoorTemp": true, "outdoorHum": true,
}

// seriesHandler serves one metric's time series for one zone as [{t, v}], newest-last.
// This is the read path for everything the engine persists that the two hardcoded
// history queries never exposed — most importantly the per-zone AFDD residual, which
// is the queryable maintenance evidence behind a "physics divergence" alert: an
// operator (or the dashboard) can pull how long a room has actually been drifting from
// its 2R1C model, not just see the live LED.
//
//	GET /api/series?zone=zone-office-3-lvl1&metric=afddResidual&minutes=120
//
// Short windows read the raw hypertable at 1-second buckets; windows over ~6 h read the
// 5-minute continuous aggregate so a day-long pull stays one cheap query.
func seriesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if DB == nil {
		w.Write([]byte("[]"))
		return
	}

	zone := r.URL.Query().Get("zone")
	metric := r.URL.Query().Get("metric")
	if zone == "" || !seriesAllowed[metric] {
		http.Error(w, `query must be ?zone=<id>&metric=<one of the telemetry series>`, http.StatusBadRequest)
		return
	}

	minutes := 60
	if m := r.URL.Query().Get("minutes"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v > 0 && v <= 60*24*7 {
			minutes = v
		}
	}

	// Keep the payload bounded with an ADAPTIVE bucket instead of switching data sources:
	// bucketSecs = ceil(windowSeconds / maxPoints), so a 2 h window returns ~1000 points
	// at ~7 s each and a 5 min window returns 1 s points. The raw hypertable (7-day
	// retention) is queried directly for any window it still holds — the continuous
	// aggregate is only for windows beyond that, where its 5-minute lag is irrelevant.
	// The earlier "switch to the aggregate past 1000 rows" was wrong: it routed a 30-min
	// AFDD pull into a materialized view that lags 5+ minutes, so a just-alerting zone's
	// freshly-persisted residual read as empty.
	const maxPoints = 1000
	const rawRetentionMin = 7 * 24 * 60
	bucketSecs := (minutes*60 + maxPoints - 1) / maxPoints
	if bucketSecs < 1 {
		bucketSecs = 1
	}
	var rows *sql.Rows
	var err error
	if minutes > rawRetentionMin {
		rows, err = DB.Query(`
			SELECT to_char(time_bucket(make_interval(secs => $4), bucket), 'YYYY-MM-DD"T"HH24:MI:SS'), AVG(avg_value)
			FROM sensor_readings_5m
			WHERE zone_id = $1 AND sensor_type = $2 AND bucket > now() - make_interval(mins => $3)
			GROUP BY 1 ORDER BY 1 ASC`, zone, metric, minutes, bucketSecs)
	} else {
		rows, err = DB.Query(`
			SELECT to_char(time_bucket(make_interval(secs => $4), time), 'YYYY-MM-DD"T"HH24:MI:SS'), AVG(value)
			FROM sensor_readings
			WHERE zone_id = $1 AND sensor_type = $2 AND time > now() - make_interval(mins => $3)
			GROUP BY 1 ORDER BY 1 ASC`, zone, metric, minutes, bucketSecs)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type point struct {
		T string  `json:"t"`
		V float64 `json:"v"`
	}
	out := []point{}
	for rows.Next() {
		var p point
		if err := rows.Scan(&p.T, &p.V); err == nil {
			out = append(out, p)
		}
	}
	b, _ := json.Marshal(out)
	w.Write(b)
}
