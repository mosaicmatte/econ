# Analysis: Carbon Credit Recommendations & Live Market Data Integration (R3)

**Author**: `explorer_survey_market_3`  
**Date**: 2026-09-05  
**Scope**: Requirement R3 (Carbon Budget Logic, Live Carbon Pricing Data Sources, Outbound HTTP Architecture, and Carbon Certificate Recommendations)  
**Target Module**: `server/carbon.go` (or `server/carbon_market.go`), `server/carbon_test.go`, and `/api/sustainability` integration in `server/main.go`

---

## 1. Executive Summary

Requirement R3 mandates:
1. Comparing calculated Scope 2 emissions against a configurable target "carbon budget" (e.g. daily, monthly, or cumulative).
2. Calculating the carbon deficit/excess requiring offset.
3. Demonstrably making an outbound HTTP request to pull live carbon market pricing from the internet (via public APIs or web scraping).
4. Recommending the exact amount of carbon certificates needed to offset the difference, including the estimated purchase cost.
5. Providing timeout handling, in-memory caching (5–15 minutes) to prevent upstream rate limiting, and an automatic fallback price if the external market feed is unreachable.

This analysis provides the complete architectural, mathematical, and implementation blueprint for R3.

---

## 2. Carbon Budget Logic & Mathematical Modeling

### 2.1 Scope 2 Emissions Formulation
Scope 2 emissions represent indirect greenhouse gas emissions resulting from the consumption of purchased electricity. In the `econ` building twin, electrical power is consumed by three primary hardware telemetry sources:
- $P_{\text{plug}}$: Measured plug-circuit draw (SCT-013 clamp) in Watts.
- $P_{\text{strip}}$: Measured power-strip draw (ACS712 sensor) in Watts.
- $P_{\text{ac}}$: Measured air conditioner / HVAC cooling electrical draw in Watts.

Total instantaneous power:
$$P_{\text{total}}(t) = P_{\text{plug}}(t) + P_{\text{strip}}(t) + P_{\text{ac}}(t) \quad [\text{W}]$$

Energy consumed over interval $\Delta t$ (in hours):
$$\Delta E = \frac{P_{\text{total}} \times \Delta t}{1000.0} \quad [\text{kWh}]$$

Scope 2 emissions generated over interval $\Delta t$:
$$\text{Emissions}_{\text{kgCO2e}} = \Delta E \times f_{\text{grid}} \quad [\text{kgCO}_2\text{e}]$$
where $f_{\text{grid}}$ is the grid emission factor in $\text{kgCO}_2\text{e/kWh}$ (default: $0.5\text{ kgCO}_2\text{e/kWh}$, overridable via `GRID_EMISSION_FACTOR`).

#### Acceptance Criteria Mathematical Proof:
- Power draw: $1000\text{ W} = 1.0\text{ kW}$.
- Duration: $1\text{ hour} = 1.0\text{ h}$.
- Energy consumed: $1.0\text{ kW} \times 1.0\text{ h} = 1.0\text{ kWh}$.
- Grid emission factor: $0.5\text{ kgCO}_2\text{e/kWh}$.
- Calculated emissions: $1.0\text{ kWh} \times 0.5\text{ kgCO}_2\text{e/kWh} = 0.5\text{ kgCO}_2\text{e}$.
*(Exact match required by Acceptance Criteria).*

### 2.2 Carbon Budget Comparison & Deficit Calculation
The system maintains a configurable target carbon budget:
- $B_{\text{kg}}$: Target carbon budget in $\text{kgCO}_2\text{e}$ (default: $50.0\text{ kg}$, configured via `CARBON_BUDGET_KG` environment variable).
- $E_{\text{actual}}$: Cumulative Scope 2 emissions in $\text{kgCO}_2\text{e}$.

#### Carbon Deficit / Excess Formula:
$$\Delta E_{\text{excess}} = \begin{cases} E_{\text{actual}} - B_{\text{kg}} & \text{if } E_{\text{actual}} > B_{\text{kg}} \\ 0.0 & \text{if } E_{\text{actual}} \le B_{\text{kg}} \end{cases}$$

#### Budget Status Flags:
- $\text{OverBudget} = (E_{\text{actual}} > B_{\text{kg}})$
- $\text{BudgetUtilizationPct} = \begin{cases} \left(\frac{E_{\text{actual}}}{B_{\text{kg}}}\right) \times 100\% & \text{if } B_{\text{kg}} > 0 \\ 0\% & \text{if } B_{\text{kg}} \le 0 \end{cases}$

### 2.3 Carbon Certificate Recommendation & Cost Mathematics
Under international carbon accounting standards (GHG Protocol, Verra VCS, Gold Standard):
- **1 Carbon Credit / Certificate** corresponds to **1 Metric Ton** of $\text{CO}_2\text{e} = 1,000\text{ kgCO}_2\text{e}$.

When $\text{OverBudget} == \text{true}$, the system computes:
1. **Fractional Carbon Credits Needed (Metric Tons)**:
   $$M_{\text{tons}} = \frac{\Delta E_{\text{excess}}}{1000.0} \quad [\text{metric tons CO}_2\text{e}]$$
2. **Whole Certificates Needed (Integer)**:
   $$N_{\text{whole}} = \lceil M_{\text{tons}} \rceil = \lceil \Delta E_{\text{excess}} / 1000.0 \rceil$$
3. **Estimated Purchase Cost (Fractional / Micro-Retirement)**:
   Modern digital carbon registries (Toucan, KlimaDAO, Carbonmark) permit fractional token retirement down to 18 decimal places.
   $$\text{Cost}_{\text{fractional}} = M_{\text{tons}} \times p_{\text{ton}} = \frac{\Delta E_{\text{excess}} \times p_{\text{ton}}}{1000.0} \quad [\text{USD}]$$
4. **Estimated Purchase Cost (Whole Certificates)**:
   For traditional brokerages or exchanges requiring integer certificate purchases:
   $$\text{Cost}_{\text{whole}} = N_{\text{whole}} \times p_{\text{ton}} \quad [\text{USD}]$$
   where $p_{\text{ton}}$ is the live market price per metric ton (USD).

---

## 3. Survey of Live Carbon Market Pricing Data Sources

We evaluated five distinct categories of carbon market data sources against four key production criteria:
1. **Public Availability**: Free tier without requiring proprietary paid API keys.
2. **Standardization**: Machine-readable JSON with consistent schema.
3. **Asset Relevance**: Represents real verified carbon reductions (Verra VCS / Gold Standard credits).
4. **Rate Limits & Reliability**: Feasibility of routine polling without risk of service bans.

| Data Source | Endpoint URL | Asset Class | Auth Req | Rate Limits | Verdict |
|---|---|---|---|---|---|
| **CoinGecko Simple Price (BCT)** | `https://api.coingecko.com/api/v3/simple/price?ids=toucan-protocol-base-carbon-tonne&vs_currencies=usd` | Voluntary Carbon Market (VCM) Verra VCUs | None | 10–30 req/min (Safe with 10-min cache) | **Recommended Primary** |
| **DexScreener BCT Token** | `https://api.dexscreener.com/latest/dex/tokens/0x2f800db0fdb5223b3c3f354886d907a671414a7f` | On-chain BCT/USDC liquidity pool | None | 300 req/min | **Recommended Alternate** |
| **Carbonmark API** | `https://api.carbonmark.com/prices` | VCM (BCT, NCT, UBO, NBO) | None | Standard public | Supported secondary (may hit corporate proxy rules) |
| **Ember Climate / Sandbag** | `https://ember-climate.org/...` | Compliance Market (EU ETS EUA) | None / Key | Varied | Less granular spot pricing |
| **HTML Web Scraping** (e.g. Terrapass/Wren) | `https://terrapass.com/...` | Retail Offsets | None | Fragile DOM | Discouraged due to DOM volatility |

### Detailed Evaluation of Recommended Endpoint: CoinGecko BCT

#### What is Base Carbon Tonne (BCT)?
Toucan Protocol's Base Carbon Tonne (BCT) bridges verified credits directly from the **Verra Verified Carbon Standard (VCS)** registry onto Polygon. 1 BCT token represents exactly 1 metric ton of verified carbon reduction/avoidance.

#### Request Specification:
- **URL**: `https://api.coingecko.com/api/v3/simple/price?ids=toucan-protocol-base-carbon-tonne&vs_currencies=usd`
- **Method**: `GET`
- **Headers**:
  ```http
  User-Agent: econ-sustainability-service/1.0
  Accept: application/json
  ```
- **Response Schema**:
  ```json
  {
    "toucan-protocol-base-carbon-tonne": {
      "usd": 0.85
    }
  }
  ```

#### Alternate Endpoint: DexScreener BCT
- **URL**: `https://api.dexscreener.com/latest/dex/tokens/0x2f800db0fdb5223b3c3f354886d907a671414a7f`
- **Response Schema**:
  ```json
  {
    "schemaVersion": "1.0.0",
    "pairs": [
      {
        "priceUsd": "0.85",
        "baseToken": { "name": "Base Carbon Tonne", "symbol": "BCT" }
      }
    ]
  }
  ```

---

## 4. Resilient Go HTTP Client Architecture

To meet production resilience standards and satisfy all acceptance criteria, the client is designed with:
1. **Explicit Timeout**: 8 seconds (`&http.Client{Timeout: 8 * time.Second}`).
2. **In-Memory Mutex Caching**: Default TTL of 10 minutes (`10 * time.Minute`), preventing rate-limit exhaustion while ensuring fresh pricing.
3. **Graceful Fallback Pricing**: Baseline benchmark of **$12.50 / metric ton** (standard voluntary carbon credit baseline) if the remote API fails, times out, or is blocked by network sandbox policies.
4. **Plausibility Filtering**: Rejects quotes outside the reasonable market bounds ($0.10 to $500.00/ton).
5. **Demonstrable Logging**: Logs outbound attempts (`log.Printf("[carbon-market] outbound HTTP GET %s", url)`).
6. **Environment Variable Configuration**:
   - `CARBON_MARKET_URL`: Custom or mock API URL.
   - `CARBON_BUDGET_KG`: Building target carbon budget (default: 50.0 kg).
   - `CARBON_FALLBACK_PRICE_USD`: Default fallback price (default: 12.50 USD/ton).
   - `CARBON_CACHE_TTL_MIN`: Cache expiration duration (default: 10 minutes).

### Client Data Structures

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// CarbonMarketQuote contains the pricing quote returned to consumers.
type CarbonMarketQuote struct {
	PricePerTonUSD  float64   `json:"pricePerTonUSD"`
	Source          string    `json:"source"`
	FetchedAt       time.Time `json:"fetchedAt"`
	IsLive          bool      `json:"isLive"`
	PriceAgeSeconds int       `json:"priceAgeSeconds"`
}

// CarbonMarketClient manages outbound requests, concurrency, and caching.
type CarbonMarketClient struct {
	mu            sync.RWMutex
	httpClient    *http.Client
	apiURL        string
	fallbackPrice float64
	cacheTTL      time.Duration
	cachedQuote   *CarbonMarketQuote
	requestCount  int // tracks outbound HTTP requests made (for testing and metrics)
}
```

### Constructor & Cache Accessors

```go
func NewCarbonMarketClient() *CarbonMarketClient {
	url := os.Getenv("CARBON_MARKET_URL")
	if url == "" {
		url = "https://api.coingecko.com/api/v3/simple/price?ids=toucan-protocol-base-carbon-tonne&vs_currencies=usd"
	}

	fallbackPrice := 12.50
	if s := os.Getenv("CARBON_FALLBACK_PRICE_USD"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			fallbackPrice = v
		}
	}

	ttl := 10 * time.Minute
	if s := os.Getenv("CARBON_CACHE_TTL_MIN"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			ttl = time.Duration(v) * time.Minute
		}
	}

	return &CarbonMarketClient{
		httpClient:    &http.Client{Timeout: 8 * time.Second},
		apiURL:        url,
		fallbackPrice: fallbackPrice,
		cacheTTL:      ttl,
	}
}

// GetQuote returns the latest quote, fetching via outbound HTTP if the cache has expired.
func (c *CarbonMarketClient) GetQuote(ctx context.Context) CarbonMarketQuote {
	// 1. Check read cache
	c.mu.RLock()
	if c.cachedQuote != nil && time.Since(c.cachedQuote.FetchedAt) < c.cacheTTL {
		q := *c.cachedQuote
		c.mu.RUnlock()
		q.PriceAgeSeconds = int(time.Since(q.FetchedAt).Seconds())
		return q
	}
	c.mu.RUnlock()

	// 2. Fetch under write lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check under write lock
	if c.cachedQuote != nil && time.Since(c.cachedQuote.FetchedAt) < c.cacheTTL {
		q := *c.cachedQuote
		q.PriceAgeSeconds = int(time.Since(q.FetchedAt).Seconds())
		return q
	}

	c.requestCount++
	quote, err := c.fetchLivePrice(ctx)
	if err != nil {
		log.Printf("[carbon-market] outbound fetch failed (%v); using fallback $%.2f/ton", err, c.fallbackPrice)
		fallback := CarbonMarketQuote{
			PricePerTonUSD:  c.fallbackPrice,
			Source:          "fallback-vcm-benchmark",
			FetchedAt:       time.Now(),
			IsLive:          false,
			PriceAgeSeconds: 0,
		}
		c.cachedQuote = &fallback
		return fallback
	}

	c.cachedQuote = &quote
	return quote
}
```

### Outbound Fetch and Multi-Schema Parser

```go
func (c *CarbonMarketClient) fetchLivePrice(ctx context.Context) (CarbonMarketQuote, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL, nil)
	if err != nil {
		return CarbonMarketQuote{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "econ-sustainability-service/1.0")
	req.Header.Set("Accept", "application/json")

	log.Printf("[carbon-market] outbound HTTP GET %s", c.apiURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return CarbonMarketQuote{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CarbonMarketQuote{}, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return CarbonMarketQuote{}, fmt.Errorf("decode json: %w", err)
	}

	price, source, err := parseCarbonPrice(raw)
	if err != nil {
		return CarbonMarketQuote{}, err
	}

	// Plausibility check: $0.10 to $500/ton
	if price < 0.10 || price > 500.00 {
		return CarbonMarketQuote{}, fmt.Errorf("implausible carbon price: $%.2f/ton", price)
	}

	log.Printf("[carbon-market] live price fetched: $%.2f/ton from %s", price, source)

	return CarbonMarketQuote{
		PricePerTonUSD:  price,
		Source:          source,
		FetchedAt:       time.Now(),
		IsLive:          true,
		PriceAgeSeconds: 0,
	}, nil
}

// parseCarbonPrice handles CoinGecko, DexScreener, and direct schemas.
func parseCarbonPrice(raw map[string]interface{}) (float64, string, error) {
	// Direct keys: "price", "pricePerTonUSD", "priceUsd"
	for _, key := range []string{"price", "pricePerTonUSD", "priceUsd"} {
		if val, ok := raw[key]; ok {
			switch v := val.(type) {
			case float64:
				return v, "direct-api", nil
			case string:
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					return f, "direct-api", nil
				}
			}
		}
	}

	// CoinGecko format: {"toucan-protocol-base-carbon-tonne": {"usd": 0.85}}
	for tokenKey, val := range raw {
		if subMap, ok := val.(map[string]interface{}); ok {
			if usdVal, ok := subMap["usd"]; ok {
				if f, ok := usdVal.(float64); ok {
					return f, fmt.Sprintf("coingecko-%s", tokenKey), nil
				}
			}
		}
	}

	// DexScreener format: {"pairs": [{"priceUsd": "0.85"}]}
	if pairs, ok := raw["pairs"].([]interface{}); ok && len(pairs) > 0 {
		if firstPair, ok := pairs[0].(map[string]interface{}); ok {
			if pStr, ok := firstPair["priceUsd"].(string); ok {
				if f, err := strconv.ParseFloat(pStr, 64); err == nil {
					return f, "dexscreener-bct", nil
				}
			}
		}
	}

	return 0, "", fmt.Errorf("could not extract carbon price from payload: %v", raw)
}
```

---

## 5. Offset Recommendation Engine

```go
// CarbonOffsetRecommendation represents the recommendation payload returned in /api/sustainability.
type CarbonOffsetRecommendation struct {
	Required                bool              `json:"required"`
	Status                  string            `json:"status"` // "within_budget" or "offset_required"
	CarbonBudgetKg          float64           `json:"carbonBudgetKg"`
	TotalEmissionsKg        float64           `json:"totalEmissionsKg"`
	ExcessEmissionsKg       float64           `json:"excessEmissionsKg"`
	BudgetUtilizationPct    float64           `json:"budgetUtilizationPct"`
	CreditsNeededMetricTons float64           `json:"creditsNeededMetricTons"`
	WholeCreditsNeeded      int               `json:"wholeCreditsNeeded"`
	MarketQuote             CarbonMarketQuote `json:"marketQuote"`
	EstimatedCostUSD        float64           `json:"estimatedCostUSD"`
	WholeCreditsCostUSD     float64           `json:"wholeCreditsCostUSD"`
}

func CalculateOffsetRecommendation(totalEmissionsKg, budgetKg float64, quote CarbonMarketQuote) CarbonOffsetRecommendation {
	utilization := 0.0
	if budgetKg > 0 {
		utilization = (totalEmissionsKg / budgetKg) * 100.0
	}

	excessKg := totalEmissionsKg - budgetKg
	if excessKg <= 0 {
		return CarbonOffsetRecommendation{
			Required:                false,
			Status:                  "within_budget",
			CarbonBudgetKg:          budgetKg,
			TotalEmissionsKg:        totalEmissionsKg,
			ExcessEmissionsKg:       0.0,
			BudgetUtilizationPct:    math.Round(utilization*100) / 100,
			CreditsNeededMetricTons: 0.0,
			WholeCreditsNeeded:      0,
			MarketQuote:             quote,
			EstimatedCostUSD:        0.0,
			WholeCreditsCostUSD:     0.0,
		}
	}

	creditsNeededTons := excessKg / 1000.0
	wholeCredits := int(math.Ceil(creditsNeededTons))
	estCost := creditsNeededTons * quote.PricePerTonUSD
	wholeCost := float64(wholeCredits) * quote.PricePerTonUSD

	return CarbonOffsetRecommendation{
		Required:                true,
		Status:                  "offset_required",
		CarbonBudgetKg:          budgetKg,
		TotalEmissionsKg:        totalEmissionsKg,
		ExcessEmissionsKg:       math.Round(excessKg*1000) / 1000,
		BudgetUtilizationPct:    math.Round(utilization*100) / 100,
		CreditsNeededMetricTons: math.Round(creditsNeededTons*1000000) / 1000000,
		WholeCreditsNeeded:      wholeCredits,
		MarketQuote:             quote,
		EstimatedCostUSD:        math.Round(estCost*100) / 100,
		WholeCreditsCostUSD:     math.Round(wholeCost*100) / 100,
	}
}
```

---

## 6. Acceptance Criteria Compliance & Demonstration

### 6.1 Requirement: "The backend demonstrably makes an outbound HTTP request to pull live carbon market pricing."
Demonstrated via two distinct verifiable mechanisms:
1. **Live Production / Staging Logs**:
   ```
   [carbon-market] outbound HTTP GET https://api.coingecko.com/api/v3/simple/price?ids=toucan-protocol-base-carbon-tonne&vs_currencies=usd
   [carbon-market] live price fetched: $0.85/ton from coingecko-toucan-protocol-base-carbon-tonne
   ```
2. **Automated Integration Test with Mock HTTP Server**:
   In `carbon_test.go`, the test spins up an `httptest.NewServer`:
   ```go
   func TestDemonstrableOutboundHTTPRequest(t *testing.T) {
       outboundCalled := false
       server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           outboundCalled = true
           if r.Header.Get("User-Agent") == "" {
               t.Error("expected User-Agent header in outbound request")
           }
           w.WriteHeader(http.StatusOK)
           w.Write([]byte(`{"toucan-protocol-base-carbon-tonne":{"usd":14.50}}`))
       }))
       defer server.Close()

       client := &CarbonMarketClient{
           httpClient:    server.Client(),
           apiURL:        server.URL,
           fallbackPrice: 12.50,
           cacheTTL:      5 * time.Minute,
       }

       quote := client.GetQuote(context.Background())
       if !outboundCalled {
           t.Fatal("backend failed to make demonstrable outbound HTTP request")
       }
       if !quote.IsLive || quote.PricePerTonUSD != 14.50 {
           t.Fatalf("unexpected quote: %+v", quote)
       }
   }
   ```

### 6.2 Requirement: "A new Go test (e.g., carbon_test.go) is written to programmatically verify the carbon calculation logic."
Verifies:
- 1000W drawn for 1 hour at 0.5 kgCO2e/kWh equals exactly 0.5 kgCO2e.
- Deficit excess calculation ($75\text{ kg} - 50\text{ kg} = 25\text{ kg}$).
- Metric ton conversion ($25\text{ kg} / 1000 = 0.025\text{ tons}$).
- Whole units ceiling ($\lceil 0.025 \rceil = 1$).
- Cost math ($0.025 \times \$14.50 = \$0.3625$).

### 6.3 Requirement: "A curl request to the new endpoint (/api/sustainability) returns a valid JSON payload containing carbon totals, maintenance alerts, and (if over budget) the recommended carbon credit offset amount and live cost."

#### Sample JSON Response When Over Budget:
```json
{
  "carbonAccounting": {
    "totalPowerW": 2450.0,
    "cumulativeEnergyKWh": 150.0,
    "scope2EmissionsKg": 75.0,
    "gridEmissionFactor": 0.5,
    "carbonBudgetKg": 50.0,
    "budgetUtilizationPct": 150.0,
    "overBudget": true
  },
  "carbonOffsetRecommendation": {
    "required": true,
    "status": "offset_required",
    "carbonBudgetKg": 50.0,
    "totalEmissionsKg": 75.0,
    "excessEmissionsKg": 25.0,
    "budgetUtilizationPct": 150.0,
    "creditsNeededMetricTons": 0.025,
    "wholeCreditsNeeded": 1,
    "marketQuote": {
      "pricePerTonUSD": 14.50,
      "source": "coingecko-toucan-protocol-base-carbon-tonne",
      "fetchedAt": "2026-09-05T04:20:00Z",
      "isLive": true,
      "priceAgeSeconds": 15
    },
    "estimatedCostUSD": 0.36,
    "wholeCreditsCostUSD": 14.50
  },
  "predictiveMaintenance": {
    "activeAlertsCount": 0,
    "alerts": []
  },
  "spaceUtilization": {
    "currentOccupancy": 12,
    "capacity": 20,
    "spaceUtilizationPct": 60.0
  }
}
```

#### Sample JSON Response When Within Budget:
```json
{
  "carbonAccounting": {
    "totalPowerW": 950.0,
    "cumulativeEnergyKWh": 70.0,
    "scope2EmissionsKg": 35.0,
    "gridEmissionFactor": 0.5,
    "carbonBudgetKg": 50.0,
    "budgetUtilizationPct": 70.0,
    "overBudget": false
  },
  "carbonOffsetRecommendation": {
    "required": false,
    "status": "within_budget",
    "carbonBudgetKg": 50.0,
    "totalEmissionsKg": 35.0,
    "excessEmissionsKg": 0.0,
    "budgetUtilizationPct": 70.0,
    "creditsNeededMetricTons": 0.0,
    "wholeCreditsNeeded": 0,
    "marketQuote": {
      "pricePerTonUSD": 14.50,
      "source": "coingecko-toucan-protocol-base-carbon-tonne",
      "fetchedAt": "2026-09-05T04:20:00Z",
      "isLive": true,
      "priceAgeSeconds": 45
    },
    "estimatedCostUSD": 0.0,
    "wholeCreditsCostUSD": 0.0
  },
  "predictiveMaintenance": {
    "activeAlertsCount": 0,
    "alerts": []
  },
  "spaceUtilization": {
    "currentOccupancy": 8,
    "capacity": 20,
    "spaceUtilizationPct": 40.0
  }
}
```

---

## 7. Implementation Recommendations for Worker

1. **File Placement**:
   Implement the carbon accounting, live market client, and recommendation logic in `server/carbon.go`.
2. **Test Placement**:
   Implement all mathematical assertions and outbound HTTP tests in `server/carbon_test.go`.
3. **Endpoint Registration**:
   In `server/main.go`:
   - Initialize `initCarbon(engine)`
   - Register `http.HandleFunc("/api/sustainability", sustainabilityHandler(engine))`
4. **Environment Defaults**:
   Provide sensible, robust defaults for all environment variables so the server runs out-of-the-box in development, CI, and test environments without manual setup.
