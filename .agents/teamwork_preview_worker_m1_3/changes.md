# Summary of Changes

## 1. Files Created and Modified

### `server/carbon.go` (Created)
- Implemented **Scope 2 Operational Carbon Accounting**:
  - `CalculateScope2Emissions(powerW float64, dtSec float64, gridFactor float64) (energyKwh float64, kgCO2e float64)` implementing exact integration formula:
    $$E_{\text{kWh}} = \frac{P_{\text{watts}} \times \Delta t_{\text{seconds}}}{3.6 \times 10^6}$$
    $$\text{kgCO}_2\text{e} = E_{\text{kWh}} \times \text{gridFactor}$$
  - Default grid emission factor of `0.5 kgCO2e/kWh`, configurable via `GRID_EMISSION_FACTOR`.
  - Continuous integration of cumulative emissions and instantaneous rate calculation: $(P_{\text{watts}} / 1000.0) \times \text{gridFactor}$.
  - Detailed power and emission breakdown for `plugW`, `stripW`, and `acW`.
- Implemented **Predictive Maintenance Diagnostics**:
  - Over-capacity wattage detection: power strip $> 2000\text{ W}$, AC $> 3500\text{ W}$.
  - Transient power surge detection: $\Delta P > 1000\text{ W}$ between successive readings.
  - Cumulative equipment runtime tracking with predictive maintenance warning when runtime $> 2000\text{ hours}$.
- Implemented **Space Utilization Efficiency**:
  - Calculates building-wide and per-zone utilization percentage using live `Occupancy` from `engine` and design capacity calculated from zone area (e.g., office: 10 m²/person, meeting room: 2.5 m²/person).
  - Explicitly excludes non-occupiable service zones (`corridor`, `wet-core`, `plant-room`, `store`, etc.).
- Implemented **Live Carbon Market Client & Carbon Credit Recommendations**:
  - Queries public CoinGecko Toucan Base Carbon Tonne spot price feed with 8-second HTTP timeout and User-Agent headers.
  - Demonstrable outbound logging: `log.Printf("[carbon-market] outbound HTTP GET %s", url)`.
  - In-memory `sync.RWMutex` cache with 10-minute TTL to prevent rate limit exhaustion.
  - Graceful fallback benchmark of `$12.50/ton` with `isLive: false` if network request fails.
  - Target carbon budget comparison (default `50.0 kgCO2e`, configurable via `CARBON_BUDGET_KG`).
  - Precise deficit calculation in kgCO2e, metric tons ($M = \Delta E / 1000$), whole certificates ($\lceil M \rceil$), and estimated purchase cost in USD.
- Implemented **REST Handler & State Persistence**:
  - `sustainabilityHandler(engine *simulation.Engine, tracker *CarbonTracker) http.HandlerFunc`: exposes `GET /api/sustainability` with standard CORS preflight handling (`corsPreflight`).
  - `loadSustainabilityState`, `saveSustainabilityState`, and `carbonPersistLoop`: checkpoints cumulative emissions and equipment runtime to `./data/sustainability-state.json`.

### `server/carbon_test.go` (Created)
- Comprehensive test suite verifying all 6 core requirements and acceptance criteria:
  1. `TestCarbonCalculationExact1000W1H`: Exact programmatic assertion that 1000W drawn for 1 hour (3600s) with 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon.
  2. `TestOutboundLiveCarbonMarketPricing`: Demonstrable outbound HTTP request to live carbon market pricing using `httptest.NewServer`.
  3. `TestCarbonMarketClientCachingBehavior`: Verifies that subsequent calls within TTL hit in-memory mutex cache without making outbound network requests, and that cache invalidation triggers a fresh request.
  4. `TestCarbonMarketClientGracefulFallback`: Verifies graceful fallback to $12.50/ton with `isLive == false` across HTTP 500 error, malformed JSON body, and unreachable network host.
  5. `TestPredictiveMaintenanceAlertGeneration`: Verifies alert generation for power strip over-capacity (> 2000W), AC over-capacity (> 3500W), transient power surges (delta > 1000W), and cumulative runtime exceedance (> 2000 hours).
  6. `TestSpaceUtilizationCalculation`: Verifies capacity calculation, per-zone efficiency %, building-wide efficiency %, and exclusion of non-occupiable service zones.
  7. `TestCarbonCreditRecommendationsMath`: Verifies deficit, metric tons, whole certificates (ceiling), and estimated cost calculation for both within-budget and over-budget states.
  8. `TestSustainabilityAPIEndpoint`: Verifies `GET /api/sustainability` JSON structure against `PROJECT.md` schema, 200 OK status, CORS headers, and `OPTIONS` preflight handling.

### `server/main.go` (Modified)
- Initialized `carbonTracker := newCarbonTracker(engine)`.
- Restored saved state via `loadSustainabilityState(carbonTracker, engine.BuildingId())`.
- Bound HTTP endpoint: `http.HandleFunc("/api/sustainability", sustainabilityHandler(engine, carbonTracker))`.
- Started background persistence loop: `go carbonPersistLoop(carbonTracker, engine)`.
