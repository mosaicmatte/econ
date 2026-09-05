# Technical Analysis: Database Schema, Non-Destructive Migration & SQL Inserts (Milestone 2)

**Agent Identity**: `teamwork_preview_explorer_m2_2`  
**Working Directory**: `d:\ECON1\econ\.agents\teamwork_preview_explorer_m2_2`  
**Project Directory**: `d:\ECON1\econ`  
**Timestamp**: 2026-09-04T07:31:30Z  

---

## 1. Executive Summary

This investigation analyzed the database schema, persistence layer, and non-destructive migration requirements for the `stripW` (Power Strip Watts, ACS712 sensor) telemetry metric across `server/db.go`, `server/db/init.sql`, `server/devices.go`, `server/simulation/engine.go`, and the live containerized database `econ_wifi_ch_a-db-1`.

### Key Findings:
1. **Source Code Status (`server/db.go` & `server/db/init.sql`)**:
   - `migrateSchema()` in `server/db.go` (lines 90–91) already contains:
     ```sql
     ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION
     CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings
     ```
   - `reading` struct in `server/db.go` (line 42) already contains `stripW *float64`.
   - `writeLoop()` in `server/db.go` (lines 179–197) is already updated to a **7-column batch INSERT** including `strip_w`:
     ```sql
     INSERT INTO sensor_readings (time, zone_id, sensor_type, value, device_id, quality, strip_w) VALUES ($1,$2,$3,$4,$5,$6,$7)
     ```
   - `seriesAllowed` map in `server/db.go` (line 309) has `"stripW": true` registered.
   - `server/db/init.sql` (lines 14, 19) already includes `strip_w DOUBLE PRECISION` and `CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;`.
2. **Live Database Status (`econ_wifi_ch_a-db-1`)**:
   - The running TimescaleDB container `econ_wifi_ch_a-db-1` currently contains **6 columns** in `sensor_readings` (`time`, `zone_id`, `sensor_type`, `value`, `device_id`, `quality`).
   - Column `strip_w` is **NOT** present in `econ_wifi_ch_a-db-1`.
   - Compatibility view `telemetry` is **NOT** present in `econ_wifi_ch_a-db-1`.
   - Current row count in `sensor_readings` is **0**.
3. **Root Cause of Missing Column and 0 Rows in Database**:
   - When container `econ_wifi_ch_a-server-1` booted 2 hours ago (`2026/09/04 12:44:24`), it attempted to ping PostgreSQL before the database container had completed its internal initialization:
     ```
     2026/09/04 12:44:24 [db] DB not reachable (is the container up?): dial tcp 172.22.0.5:5432: connect: connection refused
     ```
   - In `server/db.go` `initDB()`, there is **no retry loop**. When `DB.Ping()` failed on that first attempt, `initDB()` logged the error and returned, leaving global `DB = nil` and `writeCh = nil`.
   - Consequently:
     1. `migrateSchema()` was never executed on `econ_wifi_ch_a-db-1`.
     2. `writeLoop()` was never started.
     3. Incoming telemetry in `devices.go` / `persistMeasured()` was dropped by `enqueue()` (`if writeCh == nil { return }`).
     4. `sensor_readings` accumulated 0 rows.
4. **Data Integrity & Non-Destructive Migration Proof**:
   - Executing `ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION;` on TimescaleDB is 100% non-destructive. In PostgreSQL 11+, adding a nullable column without a default value updates the catalog in $O(1)$ time without table rewrites, exclusive locks, or chunk corruption.
   - `CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;` ensures backward-compatibility for queries expecting the name `telemetry`.
   - 100% of historical records are retained (with `strip_w = NULL`).

---

## 2. Detailed Inspection of Source Code

### 2.1 `server/db.go`

#### A. Schema Migration (`migrateSchema`)
Located at `server/db.go:83-110`:
```go
func migrateSchema() {
	stmts := []string{
		// Provenance for every row: which physical node reported it, and whether it was
		// measured or modelled. Nullable and unindexed-by-default so old rows stay valid;
		// they simply carry NULL, which reads as "written before provenance was tracked".
		`ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS device_id TEXT`,
		`ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS quality   TEXT`,
		`ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION`,
		`CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings`,
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
```
- Line 90: `ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION` is present.
- Line 91: `CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings` is present.
- Both statements are idempotent (`IF NOT EXISTS`, `CREATE OR REPLACE`).

#### B. Reading Buffer Struct (`reading`)
Located at `server/db.go:34-43`:
```go
// reading is one buffered metric sample awaiting a batched insert.
type reading struct {
	t       time.Time
	zone    string
	stype   string
	value   float64
	device  string   // MQTT topic suffix of the node that reported it; "" = engine-computed
	quality string   // one of the Quality* constants
	stripW  *float64 // Power Strip Watts (ACS712 sensor); nil when not applicable
}
```
- Line 42: `stripW *float64` is present as a pointer to `float64`, properly allowing `nil` when the metric is not a power strip measurement.

#### C. Batch Insert Pipeline (`writeLoop`)
Located at `server/db.go:167-204`:
```go
		var sb strings.Builder
		sb.WriteString("INSERT INTO sensor_readings (time, zone_id, sensor_type, value, device_id, quality, strip_w) VALUES ")
		args := make([]interface{}, 0, len(buf)*7)
		for i, r := range buf {
			if i > 0 {
				sb.WriteByte(',')
			}
			n := i * 7
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d)", n+1, n+2, n+3, n+4, n+5, n+6, n+7)
			var dev interface{} // NULL rather than "" so "no device" is unambiguous in SQL
			if r.device != "" {
				dev = r.device
			}
			var stripW interface{}
			if r.stripW != nil {
				stripW = *r.stripW
			} else if r.stype == "stripW" {
				stripW = r.value
			}
			args = append(args, r.t, r.zone, r.stype, r.value, dev, r.quality, stripW)
		}
		if _, err := DB.Exec(sb.String(), args...); err != nil {
			log.Printf("[db] batch insert failed (%d rows): %v", len(buf), err)
		}
		buf = buf[:0]
```
- **Column count**: Exactly **7 columns** (`time, zone_id, sensor_type, value, device_id, quality, strip_w`).
- **Placeholder pattern**: Multi-row batch parameterization `($1,$2,$3,$4,$5,$6,$7), ($8,$9,$10,$11,$12,$13,$14), ...` with stride `n = i * 7`.
- **Value mapping for `strip_w`**:
  - If `r.stripW != nil`, takes `*r.stripW`.
  - Fallback: if `r.stype == "stripW"`, takes `r.value`.
  - Otherwise `nil`, translating to SQL `NULL`.
- **Robustness**: Any non-strip reading (temperature, occupancy, etc.) inserts `NULL` into `strip_w`, perfectly preserving relational normalization.

#### D. Metrics Allow-List (`seriesAllowed`)
Located at `server/db.go:305-316`:
```go
var seriesAllowed = map[string]bool{
	"temp": true, "occupancy": true, "humidity": true, "co2": true,
	"afddResidual": true, "buildingLoadMw": true, "coolingOutputMw": true,
	"systemHealth": true, "avgCo2": true, "plugKw": true, "totalOccupants": true,
	"stripW": true,
	// Plant efficiency: the modelled curve and, where an AC clamp is fitted, the COP the
	// plant is actually achieving. Charting them together is how a drifting chiller shows up.
	"plantCop": true, "measuredCop": true, "meteredAcKw": true,
	// Feature series persisted for offline LSTM retraining (see engine.go); also
	// chartable through this read path.
	"avgTemp": true, "avgAirflow": true, "outdoorTemp": true, "outdoorHum": true,
}
```
- Line 309: `"stripW": true` is registered.
- Allows clients to query `/api/series?zone=<id>&metric=stripW&minutes=<N>` without HTTP 400 rejection.

---

### 2.2 `server/db/init.sql`

Located at `server/db/init.sql:1-23`:
```sql
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE sensor_readings (
  time        TIMESTAMPTZ NOT NULL,
  zone_id     TEXT NOT NULL,
  sensor_type TEXT NOT NULL,
  value       DOUBLE PRECISION,
  -- Provenance. device_id is the MQTT topic suffix of the node that reported the value,
  -- NULL when the engine computed it. quality is 'measured' | 'modelled' | 'derived'.
  -- Without these two columns a modelled temperature and one off a real SHT30 are the
  -- same row, which makes any historical chart unable to say which curve is evidence.
  device_id   TEXT,
  quality     TEXT,
  strip_w     DOUBLE PRECISION
);

SELECT create_hypertable('sensor_readings', 'time');

CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;

CREATE INDEX ON sensor_readings (zone_id, time DESC);
CREATE INDEX ON sensor_readings (device_id, time DESC);
```
- Line 14: `strip_w DOUBLE PRECISION` is defined in the initial DDL.
- Line 19: `CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;` is created immediately after hypertable setup.
- Any freshly deployed volume running `init.sql` will automatically match the 7-column schema.

---

### 2.3 `server/devices.go` and Telemetry Ingestion Pipeline

Located at `server/devices.go:151-163`:
```go
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
```
- Line 163: `track("stripW", msg.StripW)` handles incoming `stripW` readings.
- Invokes `persistMeasured(topicSuffix, dev.Zone, "stripW", *msg.StripW)`.
- In `server/db.go`:
  ```go
  func persistMeasured(deviceId, zoneId, sensorType string, value float64) {
  	var sw *float64
  	if sensorType == "stripW" {
  		v := value
  		sw = &v
  	}
  	enqueue(reading{t: time.Now(), zone: zoneId, stype: sensorType, value: value,
  		device: deviceId, quality: QualityMeasured, stripW: sw})
  }
  ```
- This sets `reading.stripW = &value`, which reaches `writeLoop()` and inserts both `value = 185.4` and `strip_w = 185.4` with `quality = "measured"`.

---

## 3. Live Database Container Inspection (`econ_wifi_ch_a-db-1`)

### 3.1 Hypertable Column Verification
Command executed:
```powershell
docker exec econ_wifi_ch_a-db-1 psql -U econ -d econ -c "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_name = 'sensor_readings' ORDER BY ordinal_position;"
```
Verbatim Output:
```text
 column_name |        data_type         | is_nullable 
-------------+--------------------------+-------------
 time        | timestamp with time zone | NO
 zone_id     | text                     | NO
 sensor_type | text                     | NO
 value       | double precision         | YES
 device_id   | text                     | YES
 quality     | text                     | YES
(6 rows)
```
- **Existing Columns**: 6 columns.
- **`strip_w` Status**: Absent. Migration is required.

### 3.2 Compatibility View Verification
Command executed:
```powershell
docker exec econ_wifi_ch_a-db-1 psql -U econ -d econ -c "\d telemetry"
```
Verbatim Output:
```text
Did not find any relation named "telemetry".
```
- **`telemetry` View Status**: Absent. Creation is required.

### 3.3 Current Row Count
Command executed:
```powershell
docker exec econ_wifi_ch_a-db-1 psql -U econ -d econ -c "SELECT count(*) FROM sensor_readings;"
```
Verbatim Output:
```text
 count 
-------
     0
(1 row)
```
- Row count in `sensor_readings` is 0.

### 3.4 Forensic Root Cause of 0 Rows and Unmigrated DB
Inspection of `econ_wifi_ch_a-server-1` logs:
```text
2026/09/04 12:44:24 [db] DB not reachable (is the container up?): dial tcp 172.22.0.5:5432: connect: connection refused
2026/09/04 12:44:24 [devices] hardware inspector at /api/devices (TEMPORARY bring-up module)
2026/09/04 12:44:24 ECON Enterprise Backend running on port 8080...
```
- When `docker-compose` launched the stack 2 hours ago, `econ_wifi_ch_a-server-1` started while `econ_wifi_ch_a-db-1` was still executing its startup scripts.
- `initDB()` pinged the database once and immediately gave up without retrying.
- `DB` was set to `nil`.
- Because `DB == nil`, `migrateSchema()` was never called on boot.
- Because `writeCh == nil`, `enqueue()` discarded all incoming readings silently.
- Currently, `nc -zv db 5432` from inside `econ_wifi_ch_a-server-1` reports `db (172.22.0.5:5432) open`. The database is fully operational and awaiting connections.

---

## 4. Verification of Non-Destructive Migration

### 4.1 SQL Migration Statements
```sql
ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION;
CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;
```

### 4.2 Non-Destructive Integrity Analysis
1. **No Data Loss (`NO DROP TABLE`)**:
   - The query uses `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`.
   - It does not drop, alter, or recreate `sensor_readings`.
   - Existing rows will have `strip_w = NULL`.
2. **PostgreSQL / TimescaleDB Hypertable Safety**:
   - In PostgreSQL 11+, adding a nullable column with no default does not rewrite the table heap or hypertable chunks. It is an $O(1)$ metadata catalog operation.
   - TimescaleDB automatically propagates the new column to all existing and future chunks.
3. **Continuous Aggregates Compatibility**:
   - In `econ_wifi_ch_a-db-1`, `timescaledb_information.continuous_aggregates` shows `sensor_readings_5m` selects `time_bucket('5 minutes', time), zone_id, sensor_type, avg(value)`.
   - Adding `strip_w` to `sensor_readings` does NOT conflict with or invalidate `sensor_readings_5m`.
4. **Compatibility View**:
   - `CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;` ensures any component or external tool referencing `telemetry` works seamlessly.

---

## 5. Required Implementation & Code Refinements for Worker

### 5.1 Refinement 1: Database Connection Retry in `server/db.go`
**File**: `server/db.go`  
**Problem**: The current `initDB()` fails permanently if PostgreSQL takes more than a fraction of a second to initialize during container startup.  
**Proposed Change**:
```go
// Replace single DB.Ping() with bounded retry loop in initDB():
func initDB() {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = "postgres://econ:econ@localhost:5432/econ?sslmode=disable"
	}

	var err error
	for attempt := 1; attempt <= 15; attempt++ {
		DB, err = sql.Open("postgres", dbURL)
		if err == nil {
			if err = DB.Ping(); err == nil {
				break
			}
		}
		log.Printf("[db] Waiting for database (attempt %d/15): %v", attempt, err)
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		log.Printf("[db] DB not reachable after retries: %v", err)
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
```

### 5.2 Refinement 2: Zone-Level `stripW` Periodic Persistence in `simulation/engine.go`
**File**: `server/simulation/engine.go`  
**Lines**: ~2165 (in periodic 1-second zone loop)  
**Rationale**: `devices.go` persists `stripW` under the raw MQTT topic suffix / device ID (`dev.Zone`). Adding `stripW` to the periodic zone loop ensures canonical building zone IDs (e.g. `zone-north-west-office-lvl4`) also accumulate `stripW` series history for `/api/series?zone=<zone_id>&metric=stripW`.  
**Proposed Change**:
```go
		for id, z := range e.Zones {
			e.Persist(id, "temp", z.Temp)
			e.Persist(id, "occupancy", float64(z.Occupancy))
			if z.HwHum > 0 && z.humFresh() {
				e.Persist(id, "humidity", z.HwHum)
			}
			if z.HwCo2 > 0 && z.co2Fresh() {
				e.Persist(id, "co2", z.HwCo2)
			}
			if z.ShadowTemp != 0 && z.hwFresh() {
				e.Persist(id, "afddResidual", z.ResidualEma)
			}
			if z.HwStripW > 0 && z.stripFresh() {
				e.Persist(id, "stripW", z.HwStripW)
			}
		}
```

### 5.3 Execution on Running Database Container
The Worker should execute the migration directly or trigger it via server rebuild:
```powershell
docker exec -i econ_wifi_ch_a-db-1 psql -U econ -d econ -c "ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION; CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;"
```
Followed by rebuilding and restarting the server container:
```powershell
docker compose -f server/docker-compose.yml -p econ_wifi_ch_a up -d --build server
```
