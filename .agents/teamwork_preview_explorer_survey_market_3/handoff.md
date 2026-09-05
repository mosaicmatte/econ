# Handoff Report: Carbon Credit Recommendations & Live Market Data (R3)

**Agent ID**: `explorer_survey_market_3`  
**Working Directory**: `/Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_explorer_survey_market_3`  
**Handoff Type**: Hard (Task Complete)  
**Milestone**: `survey_market_R3`  
**Recipient**: Orchestrator / Worker

---

## 1. Observation

1. **Authoritative Requirements**:
   - File `/Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md`:
     - Lines 56–58: *"R3. Carbon Credit Recommendations (Live Data): Implement logic that compares the calculated emissions against a target 'carbon budget'. If the infrastructure fails to meet the requirement, the system must fetch live carbon credit pricing data from the internet (via public APIs or web scraping) and recommend purchasing the exact amount of carbon certificates needed to offset the difference, including the estimated cost."*
     - Lines 71–74: *"A new Go test (e.g., carbon_test.go) is written to programmatically verify the carbon calculation logic (e.g., asserting that 1000W drawn for 1 hour with a 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon). The backend demonstrably makes an outbound HTTP request to pull live carbon market pricing. go test ./... passes successfully."*
     - Lines 76–78: *"A curl request to the new endpoint (/api/sustainability) returns a valid JSON payload containing carbon totals, maintenance alerts, and (if over budget) the recommended carbon credit offset amount and live cost."*

2. **Existing Telemetry & Engine Structures**:
   - File `/Users/nguyenhoangkhoi/Documents/econ/server/simulation/engine.go`:
     - Lines 111–118: `HwPlugW` (SCT-013 clamp), `HwStripW` (ACS712 sensor), `HwAcW` (SCT-013 clamp on cooling).
     - Lines 2007–2008 & 2096: `meteredAcW` tracks cooling electrical draw in Watts. Total building electrical load is the sum of these metered components.

3. **Existing Outbound HTTP & Resilient Polling Patterns**:
   - File `/Users/nguyenhoangkhoi/Documents/econ/server/weather.go`:
     - Line 51: `client := &http.Client{Timeout: 8 * time.Second}`
     - Lines 55–60: Graceful network error handling: `log.Printf("[weather] fetch failed (%v); envelope stays on last/fallback value", err)`
     - Lines 74–77: Plausibility gating (`if t < -40 || t > 55 ... ignored`).
     - Lines 85–90: Initial fetch on boot followed by a periodic ticker loop.

4. **Sandbox Network & Proxy Behavior**:
   - Execution of `curl -s -m 10 "https://api.carbonmark.com/prices"` returned:
     `Request to GET /prices on api.carbonmark.com not allowed by policy`
   - Execution of `env | grep -i proxy` revealed:
     `http_proxy=http://127.0.0.1:59410`, `https_proxy=http://127.0.0.1:59410`
   - Direct observation: In sandboxed, firewalled, or offline environments, external outbound HTTP requests may fail due to proxy policy or network isolation. The system architecture must guarantee graceful degradation to an automatic fallback price ($12.50/ton) and support configurable URL injection (`CARBON_MARKET_URL`) so that tests run deterministically with `httptest.Server`.

5. **Build & Test Baseline**:
   - Execution of `go version` in `server/`: `go version go1.26.4 darwin/arm64`.
   - Execution of `go test ./...` in `server/`:
     ```
     ok   econ                (cached)
     ?    econ/cli            [no test files]
     ?    econ/schema/Telemetry [no test files]
     ok   econ/simulation     (cached)
     ```
     All tests pass cleanly without failures.

---

## 2. Logic Chain

1. **Step 1: Carbon Accounting Linkage (from Obs 1 & 2)**
   - Energy consumption $E$ (kWh) is computed from total power ($P = P_{\text{plug}} + P_{\text{strip}} + P_{\text{ac}}$) integrated over time $\Delta t$.
   - $\text{Scope 2 emissions} = E_{\text{kWh}} \times f_{\text{grid}}$.
   - For $1000\text{ W}$ over $1\text{ hour}$ ($1.0\text{ kWh}$) with $f_{\text{grid}} = 0.5\text{ kgCO}_2\text{e/kWh}$, emissions are $(1.0 \times 0.5) = 0.5\text{ kgCO}_2\text{e}$. This confirms the baseline mathematical integrity required by the prompt.

2. **Step 2: Carbon Budget Deficit / Excess Calculation (from Obs 1)**
   - A target carbon budget $B_{\text{kg}}$ is configured (default: $50.0\text{ kgCO}_2\text{e}$ via `CARBON_BUDGET_KG`).
   - $\Delta E_{\text{excess}} = \max(0, E_{\text{actual}} - B_{\text{kg}})$.
   - If $E_{\text{actual}} \le B_{\text{kg}}$, `OverBudget = false`, `Required = false`, and offset recommendations are marked `within_budget` with 0 cost.
   - If $E_{\text{actual}} > B_{\text{kg}}$, `OverBudget = true`, `Required = true`, and offset recommendations are marked `offset_required`.

3. **Step 3: Market Feed Evaluation & Tokenized Offsets (from Obs 1 & 4)**
   - 1 standard Carbon Certificate represents 1 Metric Ton ($1,000\text{ kg}$) of $\text{CO}_2\text{e}$.
   - Base Carbon Tonne (BCT) bridges Verra Verified Carbon Units (VCUs) onto Polygon. CoinGecko and DexScreener provide public, unauthenticated, machine-readable JSON endpoints for BCT spot prices:
     - CoinGecko: `https://api.coingecko.com/api/v3/simple/price?ids=toucan-protocol-base-carbon-tonne&vs_currencies=usd`
     - DexScreener: `https://api.dexscreener.com/latest/dex/tokens/0x2f800db0fdb5223b3c3f354886d907a671414a7f`
   - Both return standard JSON containing the spot price per metric ton.

4. **Step 4: Resilient Outbound HTTP Client Architecture (from Obs 3 & 4)**
   - Following `weather.go`, `CarbonMarketClient` uses:
     - Outbound HTTP GET via `http.NewRequestWithContext(ctx, "GET", apiURL, nil)` with an 8-second timeout.
     - In-memory `sync.RWMutex` caching with a 10-minute TTL. This completely prevents rate-limit exhaustion (e.g., CoinGecko's 10-30 req/min limit) while providing fresh data.
     - Plausibility gate ($0.10 to $500.00/ton) preventing malformed or zero prices from corrupting advice.
     - Graceful fallback: If an outbound request fails or is blocked by network policy (Obs 4), the client logs `[carbon-market] outbound fetch failed: ...; using fallback $12.50/ton`, sets `isLive = false`, and continues without crashing.

5. **Step 5: Certificate Recommendation & Cost Math (from Step 2 & 3)**
   - Metric tons needed: $M_{\text{tons}} = \Delta E_{\text{excess}} / 1000.0$.
   - Whole units needed: $N_{\text{whole}} = \lceil M_{\text{tons}} \rceil$.
   - Live price per ton: $p_{\text{ton}}$.
   - Fractional estimated cost: $\text{Cost}_{\text{frac}} = M_{\text{tons}} \times p_{\text{ton}}$.
   - Whole certificate cost: $\text{Cost}_{\text{whole}} = N_{\text{whole}} \times p_{\text{ton}}$.
   - Exposing both allows granular micro-offsets and traditional brokerage compliance.

6. **Step 6: Demonstrable Verification Architecture (from Obs 1 & 4)**
   - Production demonstration: Outbound HTTP requests log `[carbon-market] outbound HTTP GET <URL>` and populate `source`, `isLive`, and `fetchedAt` fields in the JSON response.
   - Test suite demonstration: `carbon_test.go` spins up `httptest.NewServer`, overrides `CARBON_MARKET_URL`, and asserts that the outbound HTTP request was made, the User-Agent was passed, unmarshaling succeeded, and caching prevented duplicate requests.

---

## 3. Caveats

1. **Network Sandbox & Proxy Policies**:
   - In offline test environments or corporate firewalls (like the current test runner sandbox), live outbound calls to public internet endpoints may fail or be rejected by local proxies.
   - **Mitigation**: The client must always feature the automatic fallback price ($12.50/ton) with `isLive: false`, and unit tests in `carbon_test.go` must use `httptest.NewServer` to ensure tests are 100% reliable and offline-capable without depending on internet reachability.
2. **Third-Party API Rate Limits**:
   - CoinGecko's public tier allows ~10–30 requests/minute. Calling it on every `/api/sustainability` request would cause HTTP 429.
   - **Mitigation**: The 10-minute in-memory mutex cache reduces external API calls to at most 6 per hour, safely within public allowances.
3. **Regional Grid Factor Variation**:
   - Default is set to $0.5\text{ kgCO}_2\text{e/kWh}$ (matching the acceptance criteria specification). Operators in different regions can override this via `GRID_EMISSION_FACTOR`.

---

## 4. Conclusion

The architecture for R3 is fully defined:
1. **Module Location**: Create `server/carbon.go` implementing `CarbonMarketClient`, `CalculateScope2Emissions`, and `CalculateOffsetRecommendation`.
2. **Live Data Endpoint**: Primary default is CoinGecko Toucan Base Carbon Tonne (`https://api.coingecko.com/api/v3/simple/price?ids=toucan-protocol-base-carbon-tonne&vs_currencies=usd`), configurable via `CARBON_MARKET_URL`.
3. **Resilience**: 8s timeout, 10m mutex cache, $12.50/ton fallback benchmark, and structured logging `[carbon-market] outbound HTTP GET ...`.
4. **Budget & Offset Math**:
   - Compares cumulative Scope 2 emissions to `CARBON_BUDGET_KG` (default: $50.0\text{ kg}$).
   - Emits `within_budget` ($0 cost, 0 credits) when emissions $\le$ budget.
   - Emits `offset_required`, metric tons ($M = \Delta E / 1000$), whole credits ($\lceil M \rceil$), and estimated costs when over budget.
5. **Endpoint Integration**: Expose dynamic recommendations under `carbonOffsetRecommendation` in `/api/sustainability`.
6. **Testing**: Add `server/carbon_test.go` testing the $1000\text{ W} \times 1\text{ h} \times 0.5 = 0.5\text{ kg}$ formula, outbound HTTP verification via `httptest.Server`, caching TTL, and fallback logic.

---

## 5. Verification Method

To independently verify the implementation once coded by the Worker:

1. **Run Programmatic Unit Tests**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v -run "TestCarbon" ./...
   ```
   **Pass Condition**:
   - Test asserts that 1000W drawn for 1 hour with a 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon.
   - Test asserts demonstrable outbound HTTP request using `httptest.NewServer`.
   - Test asserts that in-memory caching suppresses outbound requests during TTL.
   - Test asserts that an unreachable upstream API falls back to $12.50/ton with `isLive == false`.

2. **Run Full Test Suite**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test ./...
   ```
   **Pass Condition**: All tests pass cleanly (`ok econ`).

3. **Verify API Payload via `curl`**:
   Start the server (`go run .`) and execute:
   ```bash
   curl -s http://localhost:8080/api/sustainability
   ```
   **Pass Condition**:
   - Response status 200 OK.
   - Valid JSON payload containing `carbonAccounting` and `carbonOffsetRecommendation`.
   - If over budget, contains `creditsNeededMetricTons`, `wholeCreditsNeeded`, `marketQuote`, and `estimatedCostUSD`.

4. **Invalidation Conditions**:
   - If `server/` fails to compile (`go build .`).
   - If `go test ./...` fails.
   - If no outbound HTTP request is made to query carbon market pricing.
   - If network failure crashes the server instead of falling back to default pricing.
