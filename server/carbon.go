package main

import (
	"context"
	"econ/simulation"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Default configuration constants
const (
	defaultGridEmissionFactor     = 0.5   // kgCO2e / kWh
	defaultCarbonBudgetKg         = 50.0  // kgCO2e target budget
	defaultCarbonMarketURL        = "https://api.coingecko.com/api/v3/simple/price?ids=toucan-protocol-base-carbon-tonne&vs_currencies=usd"
	defaultMarketFallbackPrice    = 12.50 // USD per metric ton
	defaultMarketCacheTTL         = 10 * time.Minute
	defaultHTTPTimeout            = 8 * time.Second
	sustainabilityStatePath       = "./data/sustainability-state.json"

	// Predictive maintenance diagnostic thresholds
	powerStripRatedWatts          = 2000.0 // W
	acRatedWatts                  = 3500.0 // W
	powerSurgeDeltaWatts          = 1000.0 // W
	runtimeMaintenanceHours       = 2000.0 // hours
	activeEquipmentWattsThreshold = 5.0    // equipment draw to increment runtime
)

// SustainabilityPayload models the unified REST payload defined in PROJECT.md.
type SustainabilityPayload struct {
	Timestamp                   string                          `json:"timestamp"`
	CarbonAccounting            CarbonAccountingData            `json:"carbonAccounting"`
	SpaceUtilization            SpaceUtilizationData            `json:"spaceUtilization"`
	PredictiveMaintenance       PredictiveMaintenanceData       `json:"predictiveMaintenance"`
	CarbonCreditRecommendations CarbonCreditRecommendationsData `json:"carbonCreditRecommendations"`
}

type CarbonAccountingData struct {
	GridEmissionFactorKgPerKwh         float64            `json:"gridEmissionFactorKgPerKwh"`
	InstantaneousPowerW                float64            `json:"instantaneousPowerW"`
	InstantaneousEmissionRateKgPerHour float64            `json:"instantaneousEmissionRateKgPerHour"`
	CumulativeEmissionsKgCO2e          float64            `json:"cumulativeEmissionsKgCO2e"`
	Breakdown                          PowerBreakdownData `json:"breakdown"`
}

type PowerBreakdownData struct {
	PlugW  float64 `json:"plugW"`
	StripW float64 `json:"stripW"`
	AcW    float64 `json:"acW"`
}

type SpaceUtilizationData struct {
	TotalLiveOccupants       int                    `json:"totalLiveOccupants"`
	TotalBuildingCapacity    int                    `json:"totalBuildingCapacity"`
	OverallEfficiencyPercent float64                `json:"overallEfficiencyPercent"`
	Zones                    []ZoneSpaceUtilization `json:"zones"`
}

type ZoneSpaceUtilization struct {
	ZoneId            string  `json:"zoneId"`
	LiveOccupants     int     `json:"liveOccupants"`
	DesignCapacity    int     `json:"designCapacity"`
	EfficiencyPercent float64 `json:"efficiencyPercent"`
}

type PredictiveMaintenanceData struct {
	ActiveAlertsCount int                `json:"activeAlertsCount"`
	Warnings          []MaintenanceAlert `json:"warnings"`
}

type MaintenanceAlert struct {
	EquipmentId string  `json:"equipmentId"`
	ZoneId      string  `json:"zoneId"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	MetricValue float64 `json:"metricValue"`
	Threshold   float64 `json:"threshold"`
	Timestamp   string  `json:"timestamp"`
}

type CarbonCreditRecommendationsData struct {
	CarbonBudgetKgCO2e      float64     `json:"carbonBudgetKgCO2e"`
	CurrentEmissionsKgCO2e  float64     `json:"currentEmissionsKgCO2e"`
	OverBudget              bool        `json:"overBudget"`
	DeficitKgCO2e           float64     `json:"deficitKgCO2e"`
	CreditsNeededMetricTons float64     `json:"creditsNeededMetricTons"`
	WholeCertificatesNeeded int         `json:"wholeCertificatesNeeded"`
	MarketQuote             MarketQuote `json:"marketQuote"`
	EstimatedCostUSD        float64     `json:"estimatedCostUSD"`
	Recommendation          string      `json:"recommendation"`
}

type MarketQuote struct {
	Source                   string  `json:"source"`
	SpotPricePerMetricTonUSD float64 `json:"spotPricePerMetricTonUSD"`
	Currency                 string  `json:"currency"`
	IsLive                   bool    `json:"isLive"`
	FetchedAt                string  `json:"fetchedAt"`
}

// sustainabilityState represents the persisted state across server restarts.
type sustainabilityState struct {
	BuildingId             string             `json:"buildingId"`
	CumulativeEmissionsKg  float64            `json:"cumulativeEmissionsKg"`
	EquipmentRuntimeHours  map[string]float64 `json:"equipmentRuntimeHours"`
}

// CalculateScope2Emissions translates electrical energy into kgCO2e using the exact integration formula:
// E_kWh = (P_watts * dt_seconds) / 3.6e6
// kgCO2e = E_kWh * gridFactor
func CalculateScope2Emissions(powerW float64, dtSec float64, gridFactor float64) (energyKwh float64, kgCO2e float64) {
	energyKwh = (powerW * dtSec) / 3.6e6
	kgCO2e = energyKwh * gridFactor
	return energyKwh, kgCO2e
}

// CalculateOffsetRecommendation compares cumulative emissions against a carbon budget and produces
// recommendations including metric tons, whole certificates (ceiling), and estimated cost.
func CalculateOffsetRecommendation(cumulativeEmissionsKg float64, budgetKg float64, quote MarketQuote) CarbonCreditRecommendationsData {
	rec := CarbonCreditRecommendationsData{
		CarbonBudgetKgCO2e:     budgetKg,
		CurrentEmissionsKgCO2e: math.Round(cumulativeEmissionsKg*100.0) / 100.0,
		MarketQuote:            quote,
	}

	if cumulativeEmissionsKg > budgetKg {
		rec.OverBudget = true
		deficit := cumulativeEmissionsKg - budgetKg
		rec.DeficitKgCO2e = math.Round(deficit*100.0) / 100.0
		tons := deficit / 1000.0
		rec.CreditsNeededMetricTons = math.Round(tons*10000.0) / 10000.0
		whole := int(math.Ceil(tons))
		if whole < 1 {
			whole = 1
		}
		rec.WholeCertificatesNeeded = whole
		rec.EstimatedCostUSD = math.Round((tons*quote.SpotPricePerMetricTonUSD)*1000.0) / 1000.0
		certWord := "certificate"
		if whole > 1 {
			certWord = "certificates"
		}
		rec.Recommendation = fmt.Sprintf("Purchase %d carbon credit %s (~%.4f tCO2e deficit) at $%.2f/tCO2e to offset emissions.",
			whole, certWord, rec.CreditsNeededMetricTons, quote.SpotPricePerMetricTonUSD)
	} else {
		rec.OverBudget = false
		rec.DeficitKgCO2e = 0.0
		rec.CreditsNeededMetricTons = 0.0
		rec.WholeCertificatesNeeded = 0
		rec.EstimatedCostUSD = 0.0
		rec.Recommendation = fmt.Sprintf("Emissions (%.2f kgCO2e) are within target budget (%.2f kgCO2e). No carbon offset purchase required.",
			rec.CurrentEmissionsKgCO2e, budgetKg)
	}
	return rec
}

// CarbonMarketClient queries live carbon market spot prices with in-memory caching and graceful fallback.
type CarbonMarketClient struct {
	mu            sync.RWMutex
	apiURL        string
	httpClient    *http.Client
	cachedQuote   *MarketQuote
	cacheExpiry   time.Time
	cacheTTL      time.Duration
	fallbackPrice float64
}

// NewCarbonMarketClient constructs an outbound carbon market client.
func NewCarbonMarketClient(apiURL string, ttl time.Duration) *CarbonMarketClient {
	if apiURL == "" {
		apiURL = defaultCarbonMarketURL
	}
	if ttl <= 0 {
		ttl = defaultMarketCacheTTL
	}
	return &CarbonMarketClient{
		apiURL:        apiURL,
		httpClient:    &http.Client{Timeout: defaultHTTPTimeout},
		cacheTTL:      ttl,
		fallbackPrice: defaultMarketFallbackPrice,
	}
}

// ClearCache invalidates any currently cached market quote.
func (c *CarbonMarketClient) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedQuote = nil
	c.cacheExpiry = time.Time{}
}

// SetHTTPClient allows configuring a custom http.Client (e.g. mockServer.Client() in unit tests).
func (c *CarbonMarketClient) SetHTTPClient(client *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if client != nil {
		c.httpClient = client
	}
}

// GetQuote returns the current carbon market quote. It checks the in-memory cache first,
// and if expired or missing, performs an outbound HTTP GET. If the outbound call fails,
// it gracefully returns the default fallback benchmark without panicking.
func (c *CarbonMarketClient) GetQuote() (MarketQuote, error) {
	// In-memory sync.RWMutex read check
	c.mu.RLock()
	if c.cachedQuote != nil && time.Now().Before(c.cacheExpiry) {
		quote := *c.cachedQuote
		c.mu.RUnlock()
		return quote, nil
	}
	c.mu.RUnlock()

	// Cache expired or empty; acquire write lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double check after acquiring write lock
	if c.cachedQuote != nil && time.Now().Before(c.cacheExpiry) {
		return *c.cachedQuote, nil
	}

	targetURL := c.apiURL
	if envURL := os.Getenv("CARBON_MARKET_URL"); envURL != "" {
		targetURL = envURL
	}

	// Demonstrable outbound HTTP request log per requirement R3
	log.Printf("[carbon-market] outbound HTTP GET %s", targetURL)

	quote, err := c.fetchPrice(targetURL)
	if err != nil {
		log.Printf("[carbon-market] outbound fetch failed (%v); using fallback $%.2f/ton", err, c.fallbackPrice)
		fallback := MarketQuote{
			Source:                   "Toucan Protocol BCT (CoinGecko) [Fallback]",
			SpotPricePerMetricTonUSD: c.fallbackPrice,
			Currency:                 "USD",
			IsLive:                   false,
			FetchedAt:                time.Now().UTC().Format(time.RFC3339),
		}
		return fallback, nil
	}

	c.cachedQuote = &quote
	c.cacheExpiry = time.Now().Add(c.cacheTTL)
	return quote, nil
}

func (c *CarbonMarketClient) fetchPrice(targetURL string) (MarketQuote, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return MarketQuote{}, err
	}
	req.Header.Set("User-Agent", "econ-sustainability/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return MarketQuote{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return MarketQuote{}, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return MarketQuote{}, err
	}

	price, err := parseMarketPrice(body)
	if err != nil {
		return MarketQuote{}, err
	}

	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return MarketQuote{}, fmt.Errorf("invalid spot price: %v", price)
	}

	log.Printf("[carbon-market] live carbon spot price fetched: $%.2f/ton", price)
	return MarketQuote{
		Source:                   "Toucan Protocol BCT (CoinGecko)",
		SpotPricePerMetricTonUSD: price,
		Currency:                 "USD",
		IsLive:                   true,
		FetchedAt:                time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func parseMarketPrice(body []byte) (float64, error) {
	// 1. CoinGecko nested format: {"toucan-protocol-base-carbon-tonne": {"usd": 12.50}}
	var nested map[string]map[string]float64
	if err := json.Unmarshal(body, &nested); err == nil && len(nested) > 0 {
		for _, v := range nested {
			if usd, ok := v["usd"]; ok && usd > 0 {
				return usd, nil
			}
		}
	}

	// 2. Flat format: {"usd": 12.50} or {"price": 12.50} or {"spotPricePerMetricTonUSD": 12.50}
	var flat map[string]float64
	if err := json.Unmarshal(body, &flat); err == nil && len(flat) > 0 {
		for _, key := range []string{"usd", "price", "spotPricePerMetricTonUSD", "spotPrice"} {
			if p, ok := flat[key]; ok && p > 0 {
				return p, nil
			}
		}
	}

	// 3. Generic map format
	var anyMap map[string]interface{}
	if err := json.Unmarshal(body, &anyMap); err == nil {
		for _, v := range anyMap {
			if subMap, ok := v.(map[string]interface{}); ok {
				if usdVal, ok := subMap["usd"]; ok {
					switch num := usdVal.(type) {
					case float64:
						if num > 0 {
							return num, nil
						}
					case int:
						if num > 0 {
							return float64(num), nil
						}
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("could not parse spot price from body: %s", string(body))
}

// CarbonTracker manages operational Scope 2 carbon accounting, space utilization, and equipment diagnostics.
type CarbonTracker struct {
	mu                        sync.Mutex
	engine                    *simulation.Engine
	gridFactor                float64
	carbonBudgetKg            float64
	cumulativeEmissionsKgCO2e float64
	lastTickAt                time.Time
	marketClient              *CarbonMarketClient
	equipmentRuntimeHours     map[string]float64
	lastEquipmentPower        map[string]float64
}

// newCarbonTracker initializes a new CarbonTracker instance with environment configuration.
func newCarbonTracker(engine *simulation.Engine) *CarbonTracker {
	gridFactor := defaultGridEmissionFactor
	if gfStr := os.Getenv("GRID_EMISSION_FACTOR"); gfStr != "" {
		if gf, err := strconv.ParseFloat(gfStr, 64); err == nil && gf > 0 {
			gridFactor = gf
		}
	}

	carbonBudget := defaultCarbonBudgetKg
	if cbStr := os.Getenv("CARBON_BUDGET_KG"); cbStr != "" {
		if cb, err := strconv.ParseFloat(cbStr, 64); err == nil && cb > 0 {
			carbonBudget = cb
		}
	}

	return &CarbonTracker{
		engine:                engine,
		gridFactor:            gridFactor,
		carbonBudgetKg:        carbonBudget,
		marketClient:          NewCarbonMarketClient("", defaultMarketCacheTTL),
		equipmentRuntimeHours: make(map[string]float64),
		lastEquipmentPower:    make(map[string]float64),
		lastTickAt:            time.Now(),
	}
}

// SetCumulativeEmissions manually sets cumulative emissions (useful in tests).
func (t *CarbonTracker) SetCumulativeEmissions(kg float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cumulativeEmissionsKgCO2e = kg
}

// SetCarbonBudget manually sets the carbon budget (useful in tests).
func (t *CarbonTracker) SetCarbonBudget(kg float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.carbonBudgetKg = kg
}

// SetEquipmentRuntimeHours updates equipment runtime hours (useful in predictive maintenance tests).
func (t *CarbonTracker) SetEquipmentRuntimeHours(equipId string, hours float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.equipmentRuntimeHours[equipId] = hours
}

// RecordEnergy integrates energy and carbon emissions for a given load and duration.
func (t *CarbonTracker) RecordEnergy(powerW float64, dtSec float64) (energyKwh float64, kgCO2e float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	energyKwh, kgCO2e = CalculateScope2Emissions(powerW, dtSec, t.gridFactor)
	t.cumulativeEmissionsKgCO2e += kgCO2e
	return energyKwh, kgCO2e
}

type equipMeasurement struct {
	equipId   string
	zoneId    string
	power     float64
	threshold float64
	isAc      bool
}

// Snapshot evaluates live engine telemetry and generates the unified SustainabilityPayload.
func (t *CarbonTracker) Snapshot(engine *simulation.Engine, now time.Time) SustainabilityPayload {
	t.mu.Lock()
	defer t.mu.Unlock()

	if now.IsZero() {
		now = time.Now()
	}

	dtSec := 0.0
	if !t.lastTickAt.IsZero() {
		dtSec = now.Sub(t.lastTickAt).Seconds()
		if dtSec < 0 {
			dtSec = 0
		} else if dtSec > 3600 {
			dtSec = 1.0
		}
	}
	t.lastTickAt = now

	// 1. Gather electrical telemetry across zones
	var plugW, stripW, acW float64
	var equipList []equipMeasurement

	if engine != nil && len(engine.Zones) > 0 {
		var zoneIds []string
		for id := range engine.Zones {
			zoneIds = append(zoneIds, id)
		}
		sort.Strings(zoneIds)

		for _, id := range zoneIds {
			z := engine.Zones[id]

			// Plug draw
			pW := 0.0
			if z.HwPlugW > 0 {
				pW = z.HwPlugW
			} else if z.PlugStandbyW > 0 {
				pW = z.PlugStandbyW
			}
			plugW += pW

			// Strip draw
			sW := 0.0
			if z.HwStripW > 0 {
				sW = z.HwStripW
			}
			stripW += sW

			// AC draw
			aW := 0.0
			if z.HwAcW > 0 {
				aW = z.HwAcW
			}
			acW += aW

			if sW > 0 {
				equipList = append(equipList, equipMeasurement{
					equipId:   "strip-" + id,
					zoneId:    id,
					power:     sW,
					threshold: powerStripRatedWatts,
					isAc:      false,
				})
			}
			if aW > 0 {
				equipList = append(equipList, equipMeasurement{
					equipId:   "ac-" + id,
					zoneId:    id,
					power:     aW,
					threshold: acRatedWatts,
					isAc:      true,
				})
			}
		}
	}

	totalPowerW := plugW + stripW + acW
	if dtSec > 0 && totalPowerW > 0 {
		_, carbonKg := CalculateScope2Emissions(totalPowerW, dtSec, t.gridFactor)
		t.cumulativeEmissionsKgCO2e += carbonKg
	}

	instantaneousEmissionRate := (totalPowerW / 1000.0) * t.gridFactor

	// 2. Predictive maintenance diagnostics
	warnings := make([]MaintenanceAlert, 0)

	for _, eq := range equipList {
		// Track equipment runtime hours
		if eq.power > activeEquipmentWattsThreshold && dtSec > 0 {
			t.equipmentRuntimeHours[eq.equipId] += dtSec / 3600.0
		}

		// Over-capacity detection
		if eq.power > eq.threshold {
			name := "Power strip"
			if eq.isAc {
				name = "AC unit"
			}
			warnings = append(warnings, MaintenanceAlert{
				EquipmentId: eq.equipId,
				ZoneId:      eq.zoneId,
				Type:        "over_capacity",
				Severity:    "warning",
				Message:     fmt.Sprintf("%s load exceeded rated threshold (%.1fW > %.0fW)", name, eq.power, eq.threshold),
				MetricValue: eq.power,
				Threshold:   eq.threshold,
				Timestamp:   now.UTC().Format(time.RFC3339),
			})
		}

		// Transient surge detection
		lastP, exists := t.lastEquipmentPower[eq.equipId]
		if exists && (eq.power-lastP) > powerSurgeDeltaWatts {
			delta := eq.power - lastP
			warnings = append(warnings, MaintenanceAlert{
				EquipmentId: eq.equipId,
				ZoneId:      eq.zoneId,
				Type:        "power_surge",
				Severity:    "warning",
				Message:     fmt.Sprintf("Transient power surge detected on %s: delta +%.1fW exceeds %.0fW threshold (%.1fW -> %.1fW)", eq.equipId, delta, powerSurgeDeltaWatts, lastP, eq.power),
				MetricValue: delta,
				Threshold:   powerSurgeDeltaWatts,
				Timestamp:   now.UTC().Format(time.RFC3339),
			})
		}
		t.lastEquipmentPower[eq.equipId] = eq.power
	}

	// Runtime hours maintenance check (> 2000 hours)
	var runtimeEquipIds []string
	for eqId := range t.equipmentRuntimeHours {
		runtimeEquipIds = append(runtimeEquipIds, eqId)
	}
	sort.Strings(runtimeEquipIds)

	for _, eqId := range runtimeEquipIds {
		hours := t.equipmentRuntimeHours[eqId]
		if hours > runtimeMaintenanceHours {
			zoneId := strings.TrimPrefix(strings.TrimPrefix(eqId, "strip-"), "ac-")
			warnings = append(warnings, MaintenanceAlert{
				EquipmentId: eqId,
				ZoneId:      zoneId,
				Type:        "runtime_exceeded",
				Severity:    "warning",
				Message:     fmt.Sprintf("Equipment runtime exceeded maintenance threshold (%.1f hours > %.0f hours)", hours, runtimeMaintenanceHours),
				MetricValue: math.Round(hours*10.0) / 10.0,
				Threshold:   runtimeMaintenanceHours,
				Timestamp:   now.UTC().Format(time.RFC3339),
			})
		}
	}

	// 3. Space utilization efficiency
	spaceData := computeSpaceUtilization(engine)

	// 4. Carbon credit recommendations with live market quote
	quote, _ := t.marketClient.GetQuote()
	recData := CalculateOffsetRecommendation(t.cumulativeEmissionsKgCO2e, t.carbonBudgetKg, quote)

	return SustainabilityPayload{
		Timestamp: now.UTC().Format(time.RFC3339),
		CarbonAccounting: CarbonAccountingData{
			GridEmissionFactorKgPerKwh:         t.gridFactor,
			InstantaneousPowerW:                math.Round(totalPowerW*10.0) / 10.0,
			InstantaneousEmissionRateKgPerHour: math.Round(instantaneousEmissionRate*1000.0) / 1000.0,
			CumulativeEmissionsKgCO2e:          math.Round(t.cumulativeEmissionsKgCO2e*100.0) / 100.0,
			Breakdown: PowerBreakdownData{
				PlugW:  math.Round(plugW*10.0) / 10.0,
				StripW: math.Round(stripW*10.0) / 10.0,
				AcW:    math.Round(acW*10.0) / 10.0,
			},
		},
		SpaceUtilization: spaceData,
		PredictiveMaintenance: PredictiveMaintenanceData{
			ActiveAlertsCount: len(warnings),
			Warnings:          warnings,
		},
		CarbonCreditRecommendations: recData,
	}
}

// computeSpaceUtilization calculates space utilization efficiency for occupiable zones.
func computeSpaceUtilization(engine *simulation.Engine) SpaceUtilizationData {
	data := SpaceUtilizationData{
		Zones: make([]ZoneSpaceUtilization, 0),
	}
	if engine == nil || len(engine.Zones) == 0 {
		return data
	}

	var zoneIds []string
	for id := range engine.Zones {
		zoneIds = append(zoneIds, id)
	}
	sort.Strings(zoneIds)

	for _, id := range zoneIds {
		z := engine.Zones[id]
		areaPerPerson, occupiable := getDesignAreaPerOccupant(z.Type)
		if !occupiable || areaPerPerson <= 0 {
			// Exclude non-occupiable service zones (corridor, wet-core, plant-room, store, etc.)
			continue
		}

		capacity := int(math.Floor(z.AreaM2 / areaPerPerson))
		if capacity < 1 {
			capacity = 1
		}

		eff := 0.0
		if capacity > 0 {
			eff = math.Round((float64(z.Occupancy)/float64(capacity))*10000.0) / 100.0
		}

		data.TotalLiveOccupants += z.Occupancy
		data.TotalBuildingCapacity += capacity
		data.Zones = append(data.Zones, ZoneSpaceUtilization{
			ZoneId:            id,
			LiveOccupants:     z.Occupancy,
			DesignCapacity:    capacity,
			EfficiencyPercent: eff,
		})
	}

	if data.TotalBuildingCapacity > 0 {
		data.OverallEfficiencyPercent = math.Round((float64(data.TotalLiveOccupants)/float64(data.TotalBuildingCapacity))*10000.0) / 100.0
	}
	return data
}

// getDesignAreaPerOccupant returns design m² per occupant and whether space is occupiable.
func getDesignAreaPerOccupant(zoneType string) (float64, bool) {
	norm := strings.ToLower(strings.TrimSpace(zoneType))

	// Explicit non-occupiable service zones to exclude
	switch norm {
	case "corridor", "wet-core", "store", "plant-room", "comms-room", "mechanical", "server-room":
		return 0, false
	}

	// Programme library inspection
	lib := simulation.Library()
	if prog, ok := lib.Programmes[norm]; ok {
		if prog.AreaPerOccupantM2 != nil && *prog.AreaPerOccupantM2 > 0 {
			return *prog.AreaPerOccupantM2, true
		}
		return 0, false
	}

	// Heuristic rules for common zone designations
	if strings.Contains(norm, "meeting") || strings.Contains(norm, "conference") {
		return 2.5, true
	}
	if strings.Contains(norm, "office") {
		return 10.0, true
	}
	if strings.Contains(norm, "lobby") {
		return 20.0, true
	}

	return 0, false
}

// Persistence functions

func loadSustainabilityState(tracker *CarbonTracker, currentBuildingId string) {
	data, err := os.ReadFile(sustainabilityStatePath)
	if err != nil {
		return
	}
	var s sustainabilityState
	if err := json.Unmarshal(data, &s); err != nil {
		log.Printf("[carbon] state unreadable, starting fresh: %v", err)
		return
	}
	if s.BuildingId != "" && currentBuildingId != "" && s.BuildingId != currentBuildingId {
		log.Printf("[carbon] state belongs to %q but loaded building is %q; discarding cumulative emissions", s.BuildingId, currentBuildingId)
		return
	}
	tracker.mu.Lock()
	tracker.cumulativeEmissionsKgCO2e = s.CumulativeEmissionsKg
	if s.EquipmentRuntimeHours != nil {
		tracker.equipmentRuntimeHours = s.EquipmentRuntimeHours
	}
	tracker.mu.Unlock()
	log.Printf("[carbon] restored state: cumulative=%.2f kgCO2e, %d equipment runtime trackers",
		s.CumulativeEmissionsKg, len(s.EquipmentRuntimeHours))
}

func saveSustainabilityState(tracker *CarbonTracker, buildingId string) {
	tracker.mu.Lock()
	s := sustainabilityState{
		BuildingId:            buildingId,
		CumulativeEmissionsKg: tracker.cumulativeEmissionsKgCO2e,
		EquipmentRuntimeHours: tracker.equipmentRuntimeHours,
	}
	tracker.mu.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(sustainabilityStatePath, data, 0644); err != nil {
		log.Printf("[carbon] save state failed: %v", err)
	}
}

func carbonPersistLoop(tracker *CarbonTracker, engine *simulation.Engine) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		buildingId := ""
		if engine != nil {
			buildingId = engine.BuildingId()
		}
		saveSustainabilityState(tracker, buildingId)
	}
}

// sustainabilityHandler handles GET /api/sustainability and OPTIONS CORS preflight.
func sustainabilityHandler(engine *simulation.Engine, tracker *CarbonTracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		payload := tracker.Snapshot(engine, time.Now())
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
