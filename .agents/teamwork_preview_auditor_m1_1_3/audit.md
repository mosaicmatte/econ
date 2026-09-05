## Forensic Audit Report

**Work Product**: `server/carbon.go`, `server/carbon_test.go`, and route registration in `server/main.go`
**Profile**: General Project
**Integrity Mode**: Development (per `ORIGINAL_REQUEST.md`)
**Verdict**: CLEAN

---

### Executive Summary

A comprehensive, zero-tolerance forensic integrity audit was conducted across the Sustainability & Decarbonization backend module for the `econ` building management system. Every claim, algorithm, mathematical formulation, network logic, and test harness was independently inspected and empirically validated.

No hardcoded test outputs, facade implementations, test evasions, or fabricated artifacts were detected. The mathematical formulation strictly computes $E_{\text{kWh}} = (P_{\text{watts}} \times \Delta t_{\text{seconds}}) / 3.6\times 10^6$ and $\text{kgCO}_2\text{e} = E_{\text{kWh}} \times \text{factor}$. The carbon market client executes authentic outbound HTTP GET calls using `http.Client` with timeout contexts, in-memory mutex caching with TTL expiration, and robust fallback handling. The test suite executes genuine assertions, builds cleanly (`go build .`), and passes all tests under the Go race detector (`go test -race ./...`).

---

### Phase Results

1. **Check 1: Hardcoded Test Results Detection — PASS**
   - Source code analysis of `server/carbon.go` confirmed zero instances of embedded expected test answers, fixed lookup tables, or shortcut returns matching test fixtures (e.g., $18.75, $14.50, 58.4 kg, 0.105 USD).
   - Functions `CalculateScope2Emissions`, `CalculateOffsetRecommendation`, `Snapshot`, and `computeSpaceUtilization` perform full dynamic mathematical operations.

2. **Check 2: Mathematical Formulation Integrity — PASS**
   - Inspected `CalculateScope2Emissions(powerW float64, dtSec float64, gridFactor float64)` at lines 121–125 of `server/carbon.go`:
     ```go
     energyKwh = (powerW * dtSec) / 3.6e6
     kgCO2e = energyKwh * gridFactor
     return energyKwh, kgCO2e
     ```
   - Confirmed genuine mathematical integration of electrical energy from Watts and seconds to kWh ($1\text{ kWh} = 3.6\times 10^6\text{ J}$), and multiplication by grid factor.
   - Tested $1000\text{ W} \times 3600\text{ s} / 3.6\times 10^6 \times 0.5\text{ kgCO}_2\text{e/kWh} = 0.50\text{ kgCO}_2\text{e}$ with exact precision.

3. **Check 3: Real Outbound HTTP Request Logic — PASS**
   - Verified that `CarbonMarketClient` in `server/carbon.go` (lines 167–346) implements actual outbound HTTP requests:
     - Configured with `&http.Client{Timeout: 8 * time.Second}` and `context.WithTimeout(context.Background(), defaultHTTPTimeout)`.
     - Sets request headers `User-Agent: econ-sustainability/1.0` and `Accept: application/json`.
     - Enforces memory bounds via `io.LimitReader(resp.Body, 1<<20)` (1 MB max payload).
     - Dynamically parses JSON responses (CoinGecko nested format, flat format, and generic float format) and rejects invalid, negative, NaN, or infinite prices.
     - Implements in-memory thread-safe caching (`sync.RWMutex`) with configurable TTL (default 10 minutes).
     - Implements graceful fallback to `$12.50/ton` on upstream network failures, HTTP non-200 responses, timeouts, or corrupt JSON payloads without panic.

4. **Check 4: Facade & Dummy Implementation Detection — PASS**
   - Verified that no functions return static constants or dummy placeholders.
   - `computeSpaceUtilization` calculates real capacity based on zone m² and programme library `prog.AreaPerOccupantM2` or architectural heuristics, correctly excluding non-occupiable zones (corridors, plant rooms, wet cores).
   - `Snapshot` computes submetered breakdowns (`plugW`, `stripW`, `acW`), detects over-capacity equipment (strip > 2000W, AC > 3500W), detects transient power surges (> 1000W delta), and tracks equipment runtime hours (> 2000 hours).

5. **Check 5: Test Evasion & Acceptance Bypass Analysis — PASS**
   - Evaluated `server/carbon_test.go` (494 lines, 8 test suites).
   - Tests verify:
     1. Exact 1000W / 1h / 0.5 factor math ($1.0\text{ kWh}, 0.5\text{ kgCO}_2\text{e}$).
     2. Outbound HTTP request execution via live mock HTTP server with hit counter verification.
     3. In-memory caching TTL and cache invalidation.
     4. Graceful fallback under 3 distinct upstream failure modes (HTTP 500, corrupt JSON, connection error).
     5. Predictive maintenance alert generation (over-capacity, transient surge, runtime exceeded).
     6. Space utilization calculation across multiple zone types.
     7. Offset recommendations math under budget deficit and surplus.
     8. Full REST API HTTP handler verification (CORS OPTIONS preflight & GET response).

6. **Check 6: Pre-Populated Artifact Detection — PASS**
   - Scanned workspace for pre-populated logs, result files, or state artifacts.
   - No pre-existing `sustainability-state.json` or mock result logs were present.

7. **Check 7: Clean Build & Test Verification — PASS**
   - `go build -v .` in `server/` succeeded with exit code 0.
   - `go test -v ./...` in `server/` succeeded with exit code 0.
   - `go test -race ./...` in `server/` succeeded with exit code 0 (zero data races).

---

### Empirical Evidence

#### Build Execution Output
```
$ cd server && go build -v .
(clean compilation, exit code 0)
```

#### Test Execution Output
```
$ cd server && go test -v -run "TestCarbon|TestOutbound|TestSpace|TestPredictive|TestSustainability" .
=== RUN   TestCarbonCalculationExact1000W1H
--- PASS: TestCarbonCalculationExact1000W1H (0.00s)
=== RUN   TestOutboundLiveCarbonMarketPricing
2026/09/05 11:32:23 [carbon-market] outbound HTTP GET http://127.0.0.1:59722
2026/09/05 11:32:23 [carbon-market] live carbon spot price fetched: $18.75/ton
--- PASS: TestOutboundLiveCarbonMarketPricing (0.00s)
=== RUN   TestCarbonMarketClientCachingBehavior
2026/09/05 11:32:23 [carbon-market] outbound HTTP GET http://127.0.0.1:59723
2026/09/05 11:32:23 [carbon-market] live carbon spot price fetched: $14.50/ton
2026/09/05 11:32:23 [carbon-market] outbound HTTP GET http://127.0.0.1:59723
2026/09/05 11:32:23 [carbon-market] live carbon spot price fetched: $14.50/ton
--- PASS: TestCarbonMarketClientCachingBehavior (0.00s)
=== RUN   TestCarbonMarketClientGracefulFallback
2026/09/05 11:32:23 [carbon-market] outbound HTTP GET http://127.0.0.1:59724
2026/09/05 11:32:23 [carbon-market] outbound fetch failed (unexpected HTTP status 500); using fallback $12.50/ton
2026/09/05 11:32:23 [carbon-market] outbound HTTP GET http://127.0.0.1:59725
2026/09/05 11:32:23 [carbon-market] outbound fetch failed (could not parse spot price from body: {not valid json); using fallback $12.50/ton
2026/09/05 11:32:23 [carbon-market] outbound HTTP GET http://127.0.0.1:59998/nonexistent
2026/09/05 11:32:23 [carbon-market] outbound fetch failed (Get "http://127.0.0.1:59998/nonexistent": dial tcp 127.0.0.1:59998: connect: operation not permitted); using fallback $12.50/ton
--- PASS: TestCarbonMarketClientGracefulFallback (0.00s)
=== RUN   TestPredictiveMaintenanceAlertGeneration
2026/09/05 11:32:23 [library] loaded data/programme-library.json v2: 15 programmes (2 critical), fresh-air 10 L/s/person
--- PASS: TestPredictiveMaintenanceAlertGeneration (0.01s)
=== RUN   TestSpaceUtilizationCalculation
--- PASS: TestSpaceUtilizationCalculation (0.00s)
=== RUN   TestCarbonCreditRecommendationsMath
--- PASS: TestCarbonCreditRecommendationsMath (0.00s)
=== RUN   TestSustainabilityAPIEndpoint
--- PASS: TestSustainabilityAPIEndpoint (0.00s)
PASS
ok  	econ	0.167s
```

#### Race Detector Execution Output
```
$ cd server && go test -race -v -run "TestCarbon|TestOutbound|TestSpace|TestPredictive|TestSustainability" .
PASS
ok  	econ	1.214s
```
Zero data races detected.

---

### Conclusion

The work product demonstrates genuine engineering integrity, fully satisfies all requirements of `ORIGINAL_REQUEST.md` and `PROJECT.md`, and contains zero integrity violations. Final verdict is **CLEAN**.
