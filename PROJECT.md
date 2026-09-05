# Project: Sustainability & Decarbonization Backend Module

## Architecture
The Sustainability & Decarbonization backend module integrates directly into the `econ` Go server architecture, transforming physical and simulated telemetry (`plugW`, `stripW`, `acW`, and `occupancy`) into continuous Scope 2 operational carbon accounting, predictive maintenance diagnostics, space utilization efficiency, and live carbon credit market recommendations:

```
[Sensors & Telemetry Streams]
 (plugW, stripW, acW, occupancy)
                │
                ▼
[Simulation Engine: server/simulation/]
 (ZoneSim instances, electrical loads, runtime trackers)
                │
                ▼
[Sustainability Engine: server/carbon.go]
 ├── Scope 2 Operational Carbon (R1)
 │    - Instantaneous load & cumulative emissions (kgCO2e)
 │    - Configurable grid emission factor (0.50 kgCO2e/kWh default, EPA/regional options)
 │    - Submetered breakdown (stripW, plugW, acW)
 ├── Predictive Maintenance & Space Utilization (R2)
 │    - Equipment health diagnostics (runtime hours, power surge, over-capacity, baseline drift)
 │    - Space utilization efficiency (live occupancy / design capacity)
 ├── Live Carbon Market Client (R3)
 │    - Outbound HTTP client querying live carbon credit spot price (Toucan BCT / CoinGecko API)
 │    - In-memory 10-minute mutex cache, 8s timeout, graceful $12.50 fallback
 │    - Carbon budget comparison, offset certificate count & estimated purchase cost
 └── REST API Endpoint (R4)
      - GET /api/sustainability: Unified JSON payload
```

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Scope 2 Operational Carbon | Calculate instantaneous emissions rate and cumulative kgCO2e from `plugW`, `stripW`, and AC states | M1 | ORIGINAL_REQUEST §R1 |
| 2 | Configurable Grid Emission Factors | Configurable grid factor (default 0.5 kgCO2e/kWh, supports regional/EPA overrides) | M1 | ORIGINAL_REQUEST §R1 |
| 3 | Predictive Maintenance Diagnostics | Track equipment health, runtime hours (warning at 2000h), power surges, and over-capacity | M1 | ORIGINAL_REQUEST §R2 |
| 4 | Space Utilization Efficiency | Calculate building and zone space efficiency % from occupancy and design area/occupant | M1 | ORIGINAL_REQUEST §R2 |
| 5 | Outbound Carbon Market Client | Fetch live carbon credit pricing from public API with 10-min cache, timeout, and fallback | M1 | ORIGINAL_REQUEST §R3 |
| 6 | Dynamic Carbon Offset Recommendations | Compare emissions to carbon budget, recommend exact certificates and estimated USD cost | M1 | ORIGINAL_REQUEST §R3 |
| 7 | Sustainability REST API Endpoint | Expose `/api/sustainability` returning carbon totals, maintenance alerts, and offset advice | M1 | ORIGINAL_REQUEST §R4 |
| 8 | Comprehensive Unit & Verification Tests | Write `server/carbon_test.go` verifying 1000W/1h math, outbound HTTP, caching, and edge cases | M1 | ORIGINAL_REQUEST §Acceptance Criteria |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | Sustainability & Decarbonization Module | Features 1–8: `server/carbon.go`, `server/carbon_test.go`, route binding in `server/main.go` | none | DONE |
| 2 | Review, Verification & Forensic Integrity Gate | Full verification by 2 Reviewers, 2 Challengers, and 1 Forensic Auditor | M1 | DONE |

## Interface Contracts

### GET `/api/sustainability`
- **Method**: `GET`, `OPTIONS` (CORS preflight)
- **Response Format**: `application/json`
- **Payload Schema**:
```json
{
  "timestamp": "2026-09-05T04:30:00Z",
  "carbonAccounting": {
    "gridEmissionFactorKgPerKwh": 0.5,
    "instantaneousPowerW": 2450.0,
    "instantaneousEmissionRateKgPerHour": 1.225,
    "cumulativeEmissionsKgCO2e": 14.85,
    "breakdown": {
      "plugW": 1200.0,
      "stripW": 350.0,
      "acW": 900.0
    }
  },
  "spaceUtilization": {
    "totalLiveOccupants": 18,
    "totalBuildingCapacity": 42,
    "overallEfficiencyPercent": 42.86,
    "zones": [
      {
        "zoneId": "zone_1",
        "liveOccupants": 8,
        "designCapacity": 12,
        "efficiencyPercent": 66.67
      }
    ]
  },
  "predictiveMaintenance": {
    "activeAlertsCount": 1,
    "warnings": [
      {
        "equipmentId": "strip-zone-1",
        "zoneId": "zone_1",
        "type": "over_capacity",
        "severity": "warning",
        "message": "Power strip load exceeded rated threshold (2150W > 2000W)",
        "metricValue": 2150.0,
        "threshold": 2000.0,
        "timestamp": "2026-09-05T04:29:50Z"
      }
    ]
  },
  "carbonCreditRecommendations": {
    "carbonBudgetKgCO2e": 50.0,
    "currentEmissionsKgCO2e": 58.4,
    "overBudget": true,
    "deficitKgCO2e": 8.4,
    "creditsNeededMetricTons": 0.0084,
    "wholeCertificatesNeeded": 1,
    "marketQuote": {
      "source": "Toucan Protocol BCT (CoinGecko)",
      "spotPricePerMetricTonUSD": 12.50,
      "currency": "USD",
      "isLive": true,
      "fetchedAt": "2026-09-05T04:28:00Z"
    },
    "estimatedCostUSD": 0.105,
    "recommendation": "Purchase 1 carbon credit certificate (~0.0084 tCO2e deficit) at $12.50/tCO2e to offset emissions."
  }
}
```

## Code Layout
- `server/carbon.go`: Implementation of `CarbonTracker`, `CarbonMarketClient`, predictive maintenance logic, space efficiency calculator, and `sustainabilityHandler` (package `main`).
- `server/carbon_test.go`: Unit and integration tests (package `main`):
  - Exact mathematical assertion: $1000\text{ W} \times 1\text{ h} \times 0.5\text{ kgCO}_2\text{e/kWh} = 0.5\text{ kgCO}_2\text{e}$.
  - Resilient outbound HTTP client verification via `httptest.Server`.
  - Cache TTL expiration and hit assertion.
  - Fallback price assertion under simulated network failure.
  - Predictive maintenance and space utilization math tests.
- `server/main.go`: Route registration: `http.HandleFunc("/api/sustainability", sustainabilityHandler(engine, carbonTracker))`
