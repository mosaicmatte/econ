# Roadmap — what ECON does not do yet

Everything on this page is **deliberately not implemented**. It lives here rather than in
the README so the README describes the system that exists, and this file describes the one
that is planned. Nothing here is wired up, however plausible it sounds in a pitch.

Two rules keep this file honest:

1. **A gap leaves this page only when the code lands.** Not when a sensor is bought, not
   when a field is ingested — when something acts on it.
2. **Anything the twin claims a saving from must not be on this page.** If a number in
   [EVIDENCE.md](EVIDENCE.md) depends on a mechanism, that mechanism is built or the number
   is withdrawn.

---

## Current gap ledger

Ordered roughly by what blocks a real deployment first.

| Gap | Reality today | Why it matters |
| --- | --- | --- |
| **Irradiance-driven solar gain** | `qSolar = SolarGainMult × 10 kW`, a static per-zone constant. | No time-of-day or cloud response; a west façade behaves identically at 08:00 and 16:00. (Outdoor *temperature* is no longer on this list: the envelope integrates against live Open-Meteo data, `/api/weather` shows what it is using, and it falls back to the 30 °C design-day constant only while the feed is down.) |
| **On-site solar PV / generation** | No model of any kind. | Any "uses excess daytime solar" claim is unsupported. |
| **BESS degradation / state-of-health** | SoC integrates against real capacity and inverter limits, bounded 5–98%. No cycle ageing, no round-trip loss. | Payback maths that assumes a battery never degrades is optimistic. |
| **Tariff-clock pre-cooling & peak setback** | Pre-cooling fires when the LSTM's predicted peak crosses `PRECOOL_TRIGGER_MW`. Setback is vacancy-driven only. | There is no scheduled 15:00–17:00 charge, and no partial-hibernation setback across the 17:30–22:30 peak. |
| ~~Plug-load visibility or control~~ | **Implemented.** `simulation/plugs.go` models per-zone plug draw and sweeps switchable sockets on verified vacancy; an SCT-013 clamp replaces the model with measured watts where fitted (`plugW`). | Was the largest gap on this list. |
| **CO₂-based demand-controlled ventilation** | Real CO₂ is ingested, scored and used to identify each room's air-change rate, but **no ventilation loop acts on it**. Fresh-air *load* is now modelled (it is the largest cooling term in a tropical office); fresh-air *control* is not. | The measurement exists; the control does not. Any saving attributed to DCV would be unearned — see EVIDENCE.md, which no longer claims one. |
| **Two-part tariff / capacity charge** | Energy-only pricing. | Correct for commercial *kinh doanh* sites today; the manufacturing pilot began 1 Jul 2026 and commercial follows ~2028–2030. |
| ~~A demo building that resembles an office~~ | **Fixed.** The digitizer mislabelled 555 closets of ~4 m² as server rooms at 85 kW each — 86% of connected load, and 96% of all credited savings. `tools/officeize_fixture.py` re-derives programme and physics from geometry against `data/programme-library.json`, calibrated to the 105.9–116.4 kWh/m²·yr cohort. Grid power went 15.2 MW → 0.7 MW. Raw digitizer output is kept at `building-data.json.digitizer-raw`. | Every đồng figure was scaled to a 17 MW data centre. |
| **Solar gain still static** | `qSolar = solarGainMultiplier × solarPeakWPerM2 × area`, with the multiplier now derived from real façade distance rather than assumed — but no time-of-day or cloud response. | A BH1750 lux sensor (`-DUSE_LUX=1`) publishes measured illuminance; the engine ingests it but does not yet drive solar gain from it. |
| **Firmware not deployable at building scale** | No OTA update and no per-board identity — every node shares the `econ-node` broker credential baked into `wifi_secrets.h`. *(Partially closed: the broker no longer accepts anonymous clients, and an ACL stops a sensor node publishing commands at all, so one compromised box can no longer switch the floor.)* | You cannot USB-flash forty ceiling boxes, and you cannot revoke one board without rotating every board. This blocks a real install harder than any missing sensor. |
| **No transport encryption** | MQTT and HTTP are authenticated but plaintext on the LAN. Credentials and telemetry are readable by anything on the same segment. | Authentication without TLS stops the casual attacker, not the one already on the network. mTLS between nodes and broker is the next step, and is what IEC 62443 segmentation assumes. |
| **No user identity** | One shared admin token authorizes every write. The audit log records what changed and when, never who. | Sufficient for a pilot with one operator; insufficient the moment two people can command the building, and a blocker for any enterprise contract. |

---

## Longer-horizon direction

# ECON: From Prototype to Enterprise — Architecture Review & Roadmap

***

## Competitor Landscape: How Enterprise Platforms Work

The major enterprise Digital Twin platforms fundamentally differ from ECON in their architectural philosophy: they prioritize **data federation and ontology mapping** over live physics computation.

**Azure Digital Twins (ADT)** operates as a graph-based PaaS where building entities (rooms, AHUs, VAV boxes) are modeled as DTDL (Digital Twin Definition Language) nodes in a property graph. Real-time telemetry is routed through Azure IoT Hub into the twin graph, but ADT itself does not run a physics solver — it stores and queries live *state*, not *simulation*. The 3D visualization is handled by a separate Azure Maps or Cesium layer, loosely coupled via REST. **AWS IoT TwinMaker** follows the same pattern: it is a *knowledge graph* that stitches together disparate data sources (S3 for historical data, Kinesis for streams, Grafana for rendering), with 3D scene rendering delegated to Babylon.js or Three.js components. Neither platform ships a live thermodynamic solver.

**Bentley iTwin** comes closest to ECON's approach. It ingests IFC/BIM geometry natively, visualizes it via iModel.js (a WebGL framework similar in spirit to React Three Fiber), and supports IoT telemetry overlay. Critically, iTwin's differentiator is its **change-tracking database** (`iModelHub`) that versions the physical asset model over time — something no pure simulation prototype does. **Siemens Building X** (successor to MindSphere for buildings) and **Willow** both use **Brick Schema or Haystack ontologies** as their semantic backbone, mapping every BACnet/Modbus data point to a standardized entity vocabulary before it ever reaches the visualization layer.

The most important architectural insight: **no major enterprise platform runs a live first-principles physics simulation continuously.** They run physics models *on demand* (for "what-if" scenario planning) or use ML surrogates trained on historical physics outputs. Your ECON's continuous Hardy Cross + RC-network solver running at 30 FPS is genuinely differentiated — but it is a capability that currently lacks the enterprise scaffolding to be trusted, secured, or deployed.

***

## Gap Analysis: Brutal Critique of ECON

### Semantic Ontology Layer (Critical Missing)
ECON has no semantic model. Your Go backend knows about `zone_temp_f` and `vav_airflow_cfm` as raw float streams — but it has no concept that a VAV box *serves* a thermal zone, which *is part of* a floor, which *contains* an AHU, which *has* a supply fan. Without a **Brick Schema** or **RealEstateCore** ontology graph, your system is a bespoke visualization that cannot interoperate with any FM software, cannot be queried semantically ("which zones have a setpoint deviation > 3°F AND are served by AHU-3?"), and cannot be sold to a second building without manual re-wiring. This is the single largest architectural gap. Brick Schema 1.4 (maintained by Lawrence Berkeley National Laboratory) provides a free, open RDF/OWL vocabulary for exactly this purpose.

### Real-World SCADA Ingestion (Critical Missing)
You have virtual IoT sensors. A real skyscraper has a **BAS (Building Automation System)** — typically a Siemens Desigo CC, Johnson Controls Metasys, or Honeywell EBI — communicating over **BACnet/IP** (ASHRAE Standard 135) at the field level, often with **Modbus RTU** for legacy chillers and **OPC-UA** for newer PLCs. ECON has no ingestion path for any of these. You need a **protocol translation edge gateway** — either a commercial device (like a Siemens IoT2050 or Bivocom TR341) or an open-source stack like **ThingsBoard IoT Gateway** (which natively bridges BACnet/IP, Modbus TCP, and OPC-UA into MQTT). Without this, ECON is permanently dependent on simulated data and cannot be deployed in any real building.

### Edge Computing Architecture (Major Gap)
Your current architecture assumes a stable, high-bandwidth connection from field devices to the cloud render loop. In a real 1.2M sq ft skyscraper, the BAS network is air-gapped from the internet by IT security policy. You need a **3-tier edge architecture**: (1) field-level edge nodes aggregating BACnet/Modbus polling locally, (2) a building-level edge server running your Go physics engine *on-premise* (reducing latency for real-time control decisions), and (3) a cloud tier for ML training, dashboarding, and multi-site aggregation. Running a 30 FPS physics loop that depends on sub-100ms round-trip to a cloud backend will fail the moment a building's WAN link degrades.

### Time-Series Database Scalability (Major Gap)
FlatBuffers at 30 FPS over WebSockets is excellent for *display* but you have no durable time-series store. A 15-story building with 2,000 BACnet points at 1 Hz ingestion generates ~172M data points/day. You need a purpose-built TSDB. **InfluxDB** (IOx architecture) benchmarks 8–20× faster than TimescaleDB on time-sensitive aggregate queries and has a native predictive maintenance integration story. **TimescaleDB** is a strong alternative if you want PostgreSQL compatibility for relational joins against your ontology graph. Without this layer, you cannot do fault trend analysis, energy benchmarking, or ML feature engineering — everything is ephemeral.

### Predictive Maintenance / ML (Significant Gap)
Your "AI Auto-Pilot" is a rule-based stabilization script, not a predictive model. Enterprise platforms detect *incipient* faults — a VAV actuator showing micro-oscillations 72 hours before failure — using **AFDD (Automated Fault Detection and Diagnosis)** algorithms trained on historical TSDB data. The benchmark standard here is **ASHRAE Guideline 36** for high-performance sequences and **APAR/FDD rule sets** used by Siemens and Johnson Controls. A production roadmap should include anomaly detection models (Isolation Forest or LSTM autoencoders) trained on your physics engine's *predicted* vs *actual* telemetry delta — a powerful approach since your physics baseline eliminates the need for large historical datasets.

### Security & Authentication (Critical Gap for Enterprise Sales)
There is no mention of AuthN/AuthZ in your stack. Enterprise deployment in a skyscraper requires: **mTLS** between edge gateways and cloud ingestion, **OAuth 2.0 / OIDC** for user authentication, **RBAC** (building operators vs. tenants vs. energy managers have different data scopes), **network segmentation** between OT (BACnet) and IT networks per **IEC 62443** industrial cybersecurity standards, and **audit logging** for every control command sent via the AI Auto-Pilot. Sending an unverified WebSocket command that opens a VAV damper in a real building is a safety and liability issue, not just a security one.

***

## Strategic Roadmap: Top 3 Hardest Engineering Challenges

### Challenge 1 — The Ontology-Ingestion Bridge (Hardest, Highest ROI)
The single most transformative — and most grueling — engineering problem is building a **semantic ingestion pipeline** that simultaneously (a) discovers and polls real BACnet/Modbus points from a real BAS, (b) maps those points to Brick Schema entities via a combination of rule-based heuristics and NLP (BACnet object names like `"AHU-3.SF.Speed"` must be auto-classified), and (c) binds those entities to your 3D BIM geometry. This is hard because BACnet naming conventions are not standardized across vendors, auto-classification accuracy is ~80% at best, and every building will require a human commissioning pass. The architecture should be a **Go-based BACnet/IP polling daemon → MQTT broker (Mosquitto/EMQX) → stream processor (Apache Kafka or NATS JetStream) → InfluxDB + RDF triple store (Apache Jena or Oxigraph for Brick)**.

### Challenge 2 — Physics-Grounded AFDD with a Feedback Control Loop (Most Technically Novel)
Your Hardy Cross solver and RC thermal network are currently one-way simulation tools. The hardest and most defensible engineering challenge is closing the loop: ingesting real sensor data, running your physics model in *parallel* to compute the *expected* state, and using the residual (actual − predicted) as your fault signal. This is far more powerful than pure ML-based AFDD because it does not require labeled failure data. The engineering difficulty is **model calibration** — your RC thermal parameters (R, C values per zone) must be continuously auto-identified from real data using a Kalman filter or recursive least squares estimator. This is graduate-level control theory work, but it is your core moat. No off-the-shelf platform does this for building HVAC.

### Challenge 3 — Multi-Tenant Security & Verifiable Control Authority (Most Critical for Deployment)
Before a single enterprise customer lets ECON send a control command to their BAS, you must implement a **verifiable command authorization pipeline**: every AI Auto-Pilot output must pass through a signed, auditable approval chain before it reaches the BACnet write object. The architecture requires an **OPC-UA command proxy** with mTLS, RBAC enforced at the API gateway layer (Kong or Envoy with OPA policies), an immutable audit log (append-only PostgreSQL or WORM S3 bucket), and a **soft/hard override hierarchy** — operators must be able to instantly kill AI commands with physical priority. This is not just engineering; it requires interfacing with building commissioning agents and potentially local fire/safety code compliance review. It is the longest-lead-time item on the roadmap and must be started in parallel with Challenge 1.

***

## Prioritized Build Order

| Priority | Challenge | Estimated Complexity | Strategic Value |
|---|---|---|---|
| 1 | Ontology-Ingestion Bridge (BACnet → Brick → InfluxDB) | 6–9 months, 3–4 engineers | Unlocks real deployments |
| 2 | Physics-Grounded AFDD Feedback Loop (Kalman + residual fault model) | 4–6 months, 2 engineers | Defensible technical moat |
| 3 | Multi-Tenant Security + Verifiable Control Authority | 3–5 months ongoing | Required for enterprise contracts |
