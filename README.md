# ECON Enterprise Digital Twin


[![GitHub Repository](https://img.shields.io/badge/GitHub-Repository-blue?logo=github)](https://github.com/nguyenhoangkhoi2312/econ)

ECON is a high-performance Digital Twin platform designed to bridge Building Information Modeling (BIM) data with real-time SCADA/HVAC telemetry. It features a lightweight React/Three.js frontend and a heavy-duty Go backend that runs physical thermodynamic simulations and streams state via WebSockets.

> **🆕 Latest Updates**
>
> ### 2026-07-22 — The control loop now reaches real machines, and the setback depth is the room's own answer
>
> A scan for hardcoded and stubbed features found the control loop terminating in a no-op:
> `applyHvacSetpoint()` pulsed a GPIO three times and logged the setpoint. It looked right on
> a scope and in the serial monitor while **the air conditioner did nothing** — so every
> setback the twin reported saving energy on was, at that node, fictional.
> - **Real AC control** (`-DUSE_IR_AC=1`) — genuine per-brand IR frames via IRremoteESP8266.
>   `COOLIX`, `DAIKIN`, `PANASONIC_AC`, `MITSUBISHI_AC`, `LG2`, `SAMSUNG_AC`, `TOSHIBA_AC` and
>   `GREE` are all verified to build. The node now reports **`acReal`** in telemetry, the
>   engine ingests it, and `/api/hardware` exposes it — so a zone whose commands reach no
>   machine is visible instead of being quietly counted as a saving.
> - **Per-board identity actually works** — `ZONE_TOPIC_OVERRIDE` / `ZONE_LABEL_OVERRIDE`
>   were documented in the README *and* in `platformio.ini`, but nothing in the firmware read
>   them: every board flashed by following the instructions came up as `zone_1` and collided
>   with its neighbours on the bus.
> - **Learned setback depth** — the vacancy setback was a flat `+4 °C` for every zone, which
>   is simultaneously too timid for a light, responsive room and too aggressive for a heavy
>   one that cannot recover before the floor fills up. It is now solved from the room's own
>   identified thermal response: how far can *this* room drift and still be back at setpoint
>   within the recovery budget? Falls back to the fixed figure until the room is identified,
>   and refuses outright for a room that cannot reach setpoint even at full cooling.
> - **[edge/SHOPPING_LIST.md](edge/SHOPPING_LIST.md)** and **[edge/WIRING.md](edge/WIRING.md)**
>   — four build tiers from a zero-wiring demo to a full plug-metering node, with real
>   Vietnam sourcing (SHT30 over DHT22, ASAIR ACD1200 over MH-Z19 — neither of the latter is
>   stocked here), the IR driver circuit, the SCT-013 analog front end, the mandatory 5 V↔3.3 V
>   level shifter, a power budget, a commissioning checklist and a troubleshooting table.

> ### 2026-07-22 — Every room now has its own identified physics, and the forecaster no longer needs training
>
> The learned baselines answer *"is this reading abnormal?"*. They cannot answer *"what is
> this room about to do?"* — so the twin now identifies each room's actual physical
> response, online, from its own telemetry.
> - **Per-room system identification** (`server/simulation/dynamics.go`) — recursive least
>   squares fits two first-order balances per zone:
>   `dT/dt = θ₀(T_out−T) + θ₁·flow·(T−T_supply) + θ₂·occupancy + θ₃` and
>   `dC/dt = φ₀·occupancy + φ₁(C_out−C) + φ₂`. These are the real lumped physics, so the
>   fitted coefficients *are* physical properties of that specific room: θ₀ gives its thermal
>   time constant, θ₁ the cooling its VAV actually delivers, φ₁ its **measured air-change
>   rate**. Two rooms at the same temperature produce different predictions, because they are
>   different rooms and the model knows it.
> - **Predictive recommendations** — because the balances are first-order and linear they
>   integrate in closed form, so `/api/recommendations` now also reports *"this zone crosses
>   25 °C in about 14 minutes at its current 15% airflow and 18 occupants"*, and the strongest
>   finding a threshold can never produce: **a room that cannot hold its setpoint even with
>   its VAV wide open** is a capacity fault, not a control problem. Cards are badged
>   `LEARNED` / `PREDICTED` / `CAPABILITY` so a forecast never reads as a measurement.
> - **`/api/rooms/models`** — every identified constant, exposed, so the dashboard can show
>   the reasoning rather than only the conclusion. Persisted to `data/room-dynamics.json`.
> - **Google TimesFM, zero-shot** (`backend/forecasting/timesfm_forecaster.py`,
>   `GET /api/forecast/load`) — a pretrained time-series foundation model that forecasts the
>   building's load with **no training and no fitted scaler**, which is exactly the gap the
>   supervised LSTM cannot cover on day one. Selects MPS/CUDA/CPU automatically, returns
>   quantile bands so a pre-cool decision can be made on peak *risk* rather than a bare mean,
>   and degrades cleanly to the LSTM when it is not installed. `/api/forecast/engines` reports
>   which engine actually served a forecast.
> - **Local models matched to your machine** (`server/modelcatalog.go`,
>   `POST /api/model/recommend`) — the dashboard measures the client (cores, memory, GPU) and
>   the server picks the tier that machine can actually run, explains why, and lists what
>   blocks the others. The export is then tailored: `/api/model/export?tier=…&workers=N`
>   ships the identified room models plus `econ_local.py`, a stdlib-only runtime that
>   reproduces the server's predictions offline — multi-horizon sweeps across every room,
>   whole-history batch replay, parallelised across the cores that were measured.
>
> _Identification is honest about what it costs: it samples every 5 minutes (differencing a
> noisy sensor over 30 s measures the sensor, not the room) and needs 36 samples, so a room
> takes ~3 hours of building time before the twin will predict from it. Until then it reports
> "still learning" rather than predicting from a fit it has not earned._

> ### 2026-07-22 — The recommendations, automations, and forecaster now learn from the building instead of firing on hardcoded thresholds
>
> **The "AI Operations" panel was honest that it was rule-based — `co2 > 1000`, `temp >
> setpoint + deadband`, pre-cool at a fixed `2.0 MW` — and the LSTM only ever trained on
> synthetic data.** A fixed threshold is wrong for every building but the one it was tuned
> on, and it has no idea what is *normal* for a given room at a given hour. That whole layer
> is now learned:
> - **Learned-baseline model** (`server/simulation/baselines.go`) — an online, per-`(zone,
>   metric, hour-of-day)` EWMA mean/variance built from the live telemetry stream. It needs
>   no stored history, adapts as the building changes, survives restarts
>   (`data/baseline-model.json`), and is honest about maturity: a bucket that hasn't seen
>   enough samples is "still learning" and never raises an alarm.
> - **`/api/recommendations`** — ranked anomalies scored in σ against each zone's *own*
>   normal for the hour ("CO₂ 6.2σ above this room's 14:00 normal of 620±90 ppm"), each
>   mapped to the real remediation the engine already actuates (purge, flood cooling,
>   pre-cool). A recognized standard (the ASHRAE ≤ 1000 ppm CO₂ guideline) is the clearly
>   labelled cold-start floor. The desktop panel and the mobile AI screen both render these,
>   replacing the old threshold cards.
> - **Data-driven pre-cooling** — the poller now triggers when the LSTM's forecast peak runs
>   above what the building itself normally draws in the coming hour (learned mean + 1.5σ),
>   not a magic constant. It stays on the fixed fallback only until the load baseline matures.
> - **Real-data LSTM training** — the engine persists the forecaster's exact feature set
>   (building-average temp/airflow, live outdoor conditions, target load) to TimescaleDB, and
>   `train.py` now trains on that accumulated real history via
>   `data_loader.load_training_sequences()`, falling back to the physics-grounded synthetic
>   generator (labelled as such) only when there isn't enough. The serving path is unchanged.
> - **Downloadable local models** (`/api/model/export`) — a new *Local Models* card (desktop
>   + mobile) packages the learned baseline model, the LSTM artifacts, and a dependency-free
>   `recommender.py` into one zip, so an operator can run the *same* σ-scored recommendations
>   and alerts offline, on their own machine, from the twin's processed state. `recommender.py`
>   is a faithful stdlib-only port of the engine's scoring; its exit code doubles as an alert
>   signal (0 nominal / 1 warning / 2 critical) for cron and monitoring.
>
> New Go tests cover the learning, hour-bucket fallback, maturity gating, learned-vs-standard
> basis, the load-threshold, and persistence round-trip; the export bundle + offline
> recommender were verified end-to-end.
>
> ### 2026-07-21 — Auto-Pilot is now a real control, and the "autonomous action" cards tell the truth
>
> **The AI toggle was cosmetic and the "AUTONOMOUS ACTION — Setback engaged, saving
> X/day" cards were fabricated.** `autoPilot` lived only in the browser and was never
> sent to the engine; `actuate()` ran only on hardware-bound (`Live`) zones, so in the
> demo — which has none — the optimizer did *nothing*, `energySavedMw` was always 0, and
> the cards displayed per-card estimates as if setback were happening. Now it's real end
> to end. `Engine.AutoPilot` is a genuine flag: toggling the dashboard switch sends
> `{action:"autopilot"}` over the websocket, `main.go` calls `SetAutoPilot`, and the
> optimizer actually suspends or resumes. `actuate()` now acts on **every** zone (so the
> twin's autonomy is real in the physics and the streamed savings), publishing MQTT
> commands only to zones a real device is bound to — a pure-sim zone changes state in the
> engine without spraying commands onto topics no board subscribes to. Turning Auto-Pilot
> **off releases its setbacks to the occupied baseline** (a normally-conditioned building
> handed back to the operator), while leaving manual-override vetoes untouched. The engine
> streams the real `autoPilot` state and `zonesInSetback` count, and every card on both
> dashboards now reports those: "holding 117 zones in setback — 353 kW avoided
> (24.4M VND/day), streamed from the engine, not estimated." Two fabricated claims went
> with it — the "Auto-Adjusted VAV flow +15%" comfort card (the engine does no such
> per-zone action; it now describes the real VAV modulation) and the flat lighting-saving
> constant. Proven on the live stack: toggling moved the engine between 117 zones /
> 0.35 MW saved and 0 / 0, and clicking DISENGAGE in the real UI dropped ENERGY SAVED to
> zero, flipped every card, collapsed the performance scatter back to baseline, and set
> the command bar to AI: OFF — all from one flag, on desktop and mobile.
>
> ### 2026-07-21 — The PROFILER plots real cooling, drills into real history, works on a phone
>
> **The Zone Performance scatter's Y-axis said "Cooling kW" but plotted each zone's
> internal heat-gain constant.** It now plots the *actual* cooling power delivered —
> `Q = ρ·V̇·cp·ΔT` computed from each zone's live VAV airflow against the 12 °C supply
> temperature (verified: 2–11 kW per zone, server rooms highest). The same real figure
> replaces the last fabricated constant in the panel: the "unoccupied but wasting" card
> used a flat `2 kW + …` guess; it now reports the zone's genuine cooling electrical
> (thermal cooling / live plant COP), and skips 24/7-critical server/mechanical rooms
> the way the plug sweep already does. The peak-shaving insight no longer fires on a
> hardcoded `> 3 MW` regardless of the clock — it's driven by the EVN TOU clock (in cao
> điểm, or within 90 minutes of it), matching the AI Insights panel. Biggest add:
> **tap any point to drill into that zone's real persisted history** — temperature (with
> its setpoint line) and, for sensor-backed zones, measured CO₂ — pulled from the new
> `/api/series` endpoint, so the scatter is a door into the last hour, not just a
> snapshot. And it's finally **mobile-correct**: the analytics screen now passes
> `setAutoPilot` (its action buttons were dead on mobile) and `setSelectedZone`, the
> chart and insights scale up for touch, and scatter symbols are enlarged on phones so
> a fingertip can actually land on one — the tap-target fix that made the drill-down
> usable on mobile at all. Proven end-to-end on both viewports against live TimescaleDB
> history.
>
> ### 2026-07-21 — AFDD alerts carry queryable evidence
>
> **The AFDD residual was write-only — persisted to TimescaleDB but readable by
> nothing.** A "physics divergence" card showed a live number with no way to answer the
> question a technician actually asks: *how long has this been happening?* A new generic
> read path, `GET /api/series?zone=&metric=&minutes=`, serves any persisted zone metric
> (AFDD residual, measured CO₂/humidity, plug kW, temperature, load…) as a bounded time
> series — an allow-listed public surface with an **adaptive time bucket** so a 5-minute
> pull and a 7-day pull both come back at ≤1000 points. The AFDD card is now expandable
> on desktop and charts the zone's real residual history with the engine's own 2 °C
> fault threshold marked — a developing fault (a residual climbing for an hour) reads
> differently from a transient spike, and now you can see which. Proven end-to-end on
> the live stack: a real ESP32 temperature (34 °C) diverging from its 2R1C shadow model
> grew the residual 0.18 → 12.0 °C, TimescaleDB logged it, `/api/series` returned the
> 68-point drift curve, and the card rendered it. **A real bug this shook out:** the
> first cut of the endpoint switched to the 5-minute continuous aggregate for any window
> over ~17 minutes — but that materialized view lags 5+ minutes, so a just-alerting
> zone's freshly-persisted residual came back empty. It now reads the raw hypertable
> directly for any window inside its 7-day retention, using the aggregate only beyond
> that where its lag is irrelevant.
>
> ### 2026-07-21 — The forecast pipeline runs on sampled reality
>
> **The LSTM's input window is now a real sampled hour, not a photocopied instant.**
> `ForecastWindow` used to admit in its own comment that the engine "keeps no telemetry
> history yet" and replicated the current building-average twelve times — an hour-shaped
> input the model had to treat as history. The engine now keeps a rolling window: one
> `[avg room temp, avg airflow fraction]` timestep every 5 minutes (the exact cadence
> the model was trained at, `SEQ_LEN=12`), hardware-pinned temperatures included since
> a bound zone's temp *is* the measured value. While the buffer warms up after a boot
> the window is left-padded and `/api/forecast` says so — `window_real_samples`/`window_len`
> ride the response and both AI panels show "warming up: n/12 real samples" instead of
> presenting padding as history (verified live: 1/12 at boot, 2/12 after the first
> 5-minute interval). Second fix: **one weather truth.** The weather poller now fetches
> humidity alongside temperature, and the Go engine hands both to the Python forecaster
> in the `/predict` request — plausibility-gated on the Python side — so the forecast
> and the envelope physics can never disagree about the sky; `weather_source` reports
> `"engine"` (confirmed live: 26.0 °C / 87 %RH flowing end-to-end where the forecaster
> previously flagged its own keyless fallback). The auto-pre-cool poller builds its
> request through the same path, so automated decisions and the dashboard card always
> see the same prediction. Third: AFDD residuals for sensor-bound zones now persist to
> TimescaleDB (`afddResidual` series) — a "dispatch technician" card is backed by a
> queryable drift history, not just a live LED. Three new engine tests cover warm-up
> padding, sampling cadence/order, and the weather handover's freshness-and-completeness
> gate.
>
> ### 2026-07-21 — AI Insights now reasons over the building, not the demo
>
> **Every card in the AI panels (desktop + mobile) is now generated from a real signal,
> and every button performs a real action.** The "High Grid Demand" card previously fired
> off the *demo scenario toggle*; it now runs off the EVN TOU clock itself — warning
> while cao điểm is charging and up to 90 minutes ahead of it — and reflects the
> engine's actual pre-cool window (`/api/precool`), showing *open until HH:MM* instead
> of re-offering a window that is already running. New data-driven cards: **measured
> CO₂ over 1000 ppm** (only a live NDIR can raise it; its PURGE button sends a real
> override — verified on the broker as `LIGHTS_OFF;SETPOINT=18.0` on the node's command
> topic), **edge node offline** (broker LWT), **weather feed stale** (envelope running
> on the climatological fallback), and the **plug sweep's** live state with cumulative
> savings. The hardware card grew a per-sensor coverage checklist — T/H/CO₂/W badges
> per node, lit only while that sensor is actually delivering — so a half-wired ESP32
> reads as exactly that. Dishonest math fixed along the way: the "wasting zones" card
> was calling internal heat load "cooling power" and counting empty *server rooms* as
> waste (cooling an empty server room is correct operation) — it now prices genuinely
> wasteful zones through the live plant COP at the live tariff; the engine's flat
> 2,000 W lighting-setback credit became area-based (9 W/m² LED LPD over each zone's
> real digitized floor area); and the fault card's dead "override" button now floods
> the zone with cooling via the real override path. One deployment bug this exposed:
> the dashboard's WebSocket never reconnected, so restarting the engine silently froze
> every open dashboard on its last frame while the polls kept it looking alive — the
> stream now reconnects with a 3-second backoff, verified live mid-session.
>
> ### 2026-07-21 — Plug loads: the end use the BMS can't touch
>
> **Both Vietnamese office case studies this project benchmarks against say the same
> thing: the biggest energy consumer is the one the BMS doesn't manage.** In the 2025
> Hanoi tower study (117,000 m², full BMS on chillers/lighting/pumps/fans/elevators),
> plug loads were the *largest* end use — 26.4% of energy, 35.3% of CO₂ — precisely
> because a BMS stops at the wall socket. The 57-building survey (Hoang et al. 2022)
> puts appliance intensity at 17.7–20 kWh/m²·yr, second only to air conditioning. ECON
> now closes that loop with **Automated Plug Load Control (APLC)**: every zone carries
> a plug model sized from its real digitized floor area (1.2 W/m² always-on standby +
> 65 W per present occupant, coincidence-weighted per the survey's own k·factor
> methodology), and an **after-hours sweep** sheds the switchable 70% of standby in
> zones that are *verifiably empty* — the occupancy the twin already senses via
> CV/mmWave/PIR — then restores them the instant presence returns (`PLUG_OFF`/`PLUG_ON`
> over MQTT, proven live end-to-end). Server rooms and mechanical spaces are never
> swept. Where an ESP32 carries the new `USE_PLUG` build (SCT-013 clamp on GPIO34,
> plug relay on GPIO25, fail-energized on boot), measured watts replace the model under
> the same per-field freshness rules as every other sensor. Savings integrate on
> wall-clock time and survive rebuilds on the data volume (verified: 0.109 kWh crossed
> a container recreate), priced in VND at the EVN tariff and in CO₂ at the same 0.6766
> kg/kWh grid factor the case study used. Policy changes (work hours, grace, enable)
> are POST `/api/plugs` — admin-token-guarded when set, audited to the same
> `deploy-log.jsonl` as building deploys. Desktop gained a **PLUGS** tab (live draw,
> phantom-load leaderboard, sweep policy editor); mobile's Energy screen gained a Plug
> Loads card with the sweep toggle and a split Energy Flow (plug vs lighting+fans).
> Found and fixed in the process: the container ran on UTC, which would have armed the
> sweep at lunch and disarmed it at midnight — the image now ships tzdata with
> `TZ=Asia/Ho_Chi_Minh` compose default, because a schedule that switches a building's
> sockets must run on the building's clock.
>
> ### 2026-07-20 — Blueprints in, buildings out
>
> **A real-world drawing can now become the running twin without touching a terminal.**
> Desktop grew a *+ Blueprint* button and mobile an *Import Blueprint* menu entry (with
> camera capture — photograph the paper drawing); both feed a new dockerized digitizer
> service that wraps Branch B end to end. AutoCAD DXF is parsed as vectors — closed
> polylines become rooms, text labels become zone types, and `$INSUNITS` gives real
> dimensions (a millimetre test floor came back 29×19 m with the footprint override
> correctly ignored); DXFs without room polylines are rasterized and fed to the CV
> pipeline, as are PDFs (poppler) and image scans/photos. The flow is two-step by
> design — digitize returns a review (zone count, types, method, units) and nothing
> changes until *Deploy*, which hot-swaps the running engine in one critical section
> (`[building] reloaded: 12 zones, 12 VAVs` mid-simulation, verified live, original
> 1,350-zone building restored the same way).
>
> **The dashboard stopped baking geometry into the bundle.** Seven modules imported
> building-data.json at build time, so a deployed blueprint would run in the engine while
> every panel kept rendering the compiled-in building. The app now boots in two stages:
> fetch the engine's geometry first, then import the module graph, so every derived
> constant (floor area, EUI, fault targets, design peak) computes from the live building.
> The bundled copy remains only as the offline fallback.
>
> **Deploying a building is treated as the operational change it is.** The previous
> building and ontology are backed up automatically before every deploy (last 20 kept,
> restorable from the import panel or `POST /api/building/rollback`), every deploy and
> rollback lands in an append-only audit log (timestamp, source file, zone/floor counts,
> payload hash), and setting `ECON_ADMIN_TOKEN` in the compose environment gates both
> destructive endpoints behind an `X-Admin-Token` header — the UI asks for the token only
> when the server demands it, so the local demo stays frictionless. Payloads with
> duplicate zoneIds or absurd zone counts are rejected before the swap, and the data
> directory moved to a named volume so a deployed building, its backups and its audit
> trail survive `docker compose up --build`. All verified live: 401 without and with a
> wrong token, deploy + automatic 1,350-zone backup, duplicate-id rejection, container
> recreate with the deployed building intact, rollback, and a two-entry audit log.
>
> ### 2026-07-16 — The envelope gets real weather, and the sensor path stops lying
>
> **The 2R1C physics now integrates against live outdoor temperature.** A poller feeds
> Open-Meteo's current conditions (the same keyless service the sky and the LSTM already
> use) into the envelope every 10 minutes; `/api/weather` reports what the physics is using
> and whether it is live or the 30 °C design-day fallback, which now only appears while the
> feed is genuinely down — same freshness contract as the zone sensors. Verified end-to-end:
> the running engine picked up 27.4 °C against Open-Meteo's own 27.3 (one refresh apart).
> The §6.1 implementation note and the Not-Yet-Implemented table were corrected to match;
> the solar term remains a static multiplier and stays on that list.
>
> **A run of honesty bugs in the measured-data path are fixed.** Humidity and CO₂ now retire
> per *sensor*, not per node — previously an unplugged NDIR kept streaming its last reading
> as "measured" for as long as the board still sent temperature (regression-tested both
> ways). The mobile zone sheet showed an occupancy-derived CO₂ estimate even when a real
> sensor was bound, labelled identically to a measurement — it now prefers the sensor and
> says which is on screen. Worst of the lot: a no-sensor demo build published `random()`
> humidity and CO₂ over the same channel real sensors use, and both dashboards labelled
> them measured; those fields are now omitted, exactly as a failed read already was. The
> HVAC IR emitter also moved off GPIO22 — the I²C clock — where every setpoint command
> was corrupting the bus; the collision is now a compile error.
>
> **The edge node grew office-grade presence and 24/7 CO₂ discipline.** `-DUSE_MMWAVE=1`
> reads an HLK-LD2410C radar on GPIO18 (detects a *stationary* person, which a PIR cannot;
> OR-ed with the PIR when both are fitted), and `-DCO2_ABC_OFF=1` switches the ACD1200 to
> manual calibration at boot with a read-back confirm — in a continuously occupied space
> the factory auto-calibration re-zeroes weekly against a baseline the room never gives it,
> silently under-reporting forever after. All datasheet CRC vectors validate; six build
> permutations compile.
>
> **ECON now reports the two metrics the Vietnamese literature it cites is actually about.**
> Energy use intensity is computed over the building's own geometry — 42,037 m², summed by
> shoelace straight from the digitized zone polygons, so a regenerated building recomputes
> itself — and Scope 2 operational carbon at Vietnam's grid emission factor (0.6766 kgCO₂e/kWh,
> overridable per reporting year), surfaced on the desktop Overview and the mobile Impact screen
> along with the carbon the optimizer is actively avoiding. Building them was worth it for what
> they exposed on the first run: the bundled demo building is **86% server-room by connected
> load** (555 zones at 85 kW each), giving a run-rate EUI of ≈3,700 kWh/m²·yr — about 32× the
> 116.4 office cohort, and squarely data-centre territory. The physics, tariff and comfort models
> are unaffected, but the fixture is not an office, so the dashboard suppresses the office
> benchmark whenever load is IT-dominated instead of printing a meaningless 32× ratio, and says
> why. A metric that only ever flatters you isn't a metric.
>
> ### 2026-07-15 — Measured data end to end, and a profiler that says something
>
> **The twin now reports what it measures, and admits what it doesn't.** The edge firmware
> stopped inventing readings: sensors are enabled independently (`USE_DHT` / `USE_PIR` /
> `USE_CO2`), and a sensor that is absent or fails its read omits its field entirely instead of
> substituting a plausible constant — a failed DHT22 read used to publish 24.0 °C flagged
> `tempReal:true`, pinning a zone's physics *and* its AFDD residual to a number no sensor ever
> saw. Real CO₂ arrives over MH-Z19B NDIR (checksum + range validated), the Pico gained an
> optional DHT22 that outranks its own die sensor, and measured humidity/CO₂ now ride the main
> FlatBuffers stream — freshness-gated, so a board that drops off stops reporting rather than
> pinning its last reading there forever. Building CO₂ prefers real sensors over the occupancy
> estimate, which had been plotting 5477 ppm — an occupational exposure limit, not an office.
> System health now charges for zones in alarm: averaging comfort across 1350 zones reduced one
> server room at 50 °C to 99.93%, so the dashboard cheerfully reported *HEALTH 100%* beside its
> own critical-fault banner. And the profiler's "OFFICE DCV" scatter — 300 `Math.random()` points
> labelled *Historical Baseline*, plotted against invented CO₂ — is gone, replaced by zone cooling
> load against drift from setpoint, where overcooled, starved and struggling zones are each a
> quadrant with a count and a cost in đồng. Zone cost rates are priced at the live EVN band
> instead of a leftover `$0.12/kWh`, and System Logs folded into Diagnostics.
>
> ### 2026-07-15 — Live time-of-day sky, shared by mobile and desktop
>
> **The twin now sits under the same sky as the building it models.** The static night
> gradient behind the 3D view is gone: `LiveWeatherBackground` picks one of five hand-tuned
> scenes — golden hour, morning, afternoon, sunset, evening — from the site's real local
> wall-clock, anchored to today's actual sunrise/sunset for the deployment coordinates
> (Open-Meteo, no key, falling back to Ho Chi Minh City's near-constant tropical times when
> offline). Each scene keeps the original painterly art: a coloured, misty sky fading to
> near-black at the base, with a soft-glowing sun — or a cream crescent moon over a drifting
> starfield after dark — fixed high in frame, easing from one scene to the next as a boundary
> is crossed rather than snapping. The desktop view now mounts the same component behind its
> transparent WebGL canvas, so the phone and the workstation finally share one sky instead of
> the desktop's flat black.
>
> ### 2026-07-14 — Vietnamese EVN tariff, TOU reconciliation, and a real BESS
>
> **The economics are now Vietnamese, and current.** The inherited US demand-charge model is
> gone — Vietnam prices commercial power almost entirely through a three-tier energy tariff,
> so every figure is đồng at the EVN "Kinh doanh" ≥22 kV rates from Decision 1279/QĐ-BCT
> (peak 5,025 / normal 2,887 / off-peak 1,609 VND/kWh), and the TOU clock was reconciled to
> Decision 963/QĐ-BCT (effective 22 Apr 2026), which retired the old Thông tư 16/2014
> split-peak schedule in favour of a single 17:30–22:30 evening peak (Mon–Sat) and a
> 00:00–06:00 off-peak. Savings are quoted as load-shift arbitrage — the real play for a
> Vietnamese facility manager — not a demand charge that does not exist here. Riding on that
> spread: a genuine Battery Energy Storage System. `simulation/bess.go` charges the pack
> off-peak, discharges it through the peak and trickles through normal daytime hours,
> integrating true state of charge against capacity and inverter limits, and the engine
> subtracts its discharge from grid draw. The mobile Energy screen and the desktop Overview
> both show live SoC, dispatch state, and the megawatts being shaved off the grid; the Go
> dispatcher pins its own TOU classification to ICT so the engine and the dashboard can never
> disagree about which band it is.
>
> ### 2026-07-13 — Physics-grounded AFDD + forecast-driven pre-cooling + webcam CV node
>
> **The AI layer now closes the loop.** Every hardware-bound zone runs a sensor-free
> *shadow twin* — the same 2R1C physics, never pulled toward the measurement — and the
> smoothed |measured − modeled| residual is a fault signal that needs zero training
> data: a healthy room tracks its physics, a blocked coil or open window diverges and
> trips a red AFDD card (threshold 2 °C, surfaced per node in `/api/hardware` and the
> AI Insights panel). The LSTM forecaster now *actuates*: a background poller feeds it
> the live telemetry window every 5 minutes and, when the predicted peak crosses
> `PRECOOL_TRIGGER_MW`, opens a 20-minute pre-cool window that drives every occupied
> zone 1.5 °C below setpoint — charging thermal mass ahead of the peak — with the same
> window one click away on the High-Grid-Demand card (`{"action":"precool"}` over
> WebSocket, or `POST /api/precool`). The YOLO/ByteTrack tracker joined the edge
> contract too: `--source 0` points it at a live webcam and it publishes occupancy-only
> `source:"cv"` telemetry with retained online/offline status, making a doorway camera
> just another node on the twin.
>
> ### 2026-07-12 — Operator UX pass: pro topology, live panels, real mobile view
>
> **The control surfaces caught up with the engine.** The Level topology is now a
> professional riser schematic — one sorted terminal-unit card per zone (status LED,
> temp vs setpoint, occupancy, VAV airflow bar) beneath the AHU, replacing 181
> floorplan-scattered overlapping boxes — and clicking a card flies the 3D camera to
> the room (the zoom target transform was mirrored; fixed). The airflow window now
> renders in ~6 instanced draw calls with density-capped occupant markers, and
> diffuser cones show supply rate as a static heat gradient instead of pulsing.
> AI Insights gained a hardware-in-the-loop card (inspect + fly-to physical nodes),
> an inline LSTM forecast chart, and a real model-metrics grid; the Enterprise
> Overview gained live ENERGY SAVED, a Plant COP gauge, and an ⚡ EDGE HARDWARE list.
> And phones finally get a real experience: viewports ≤ 820 px automatically serve a
> live mobile Impact screen (savings donut, load vs predicted peak, occupancy by
> level, edge nodes) instead of the WebGL-heavy desktop stack.
>
> ### 2026-07-11 — Live lighting state streams into the 3D twin
>
> **Zones now go dark when the engine cuts their lights.** The FlatBuffers wire schema
> gained a backward-compatible `lightsOn` field (Go + TS bindings regenerated); the
> engine re-broadcasts a zone the instant its lighting flips, the zone shader dims
> lights-off zones to 30% brightness, and the zone micro-HUD shows a live LIGHTS row
> (`ON` / `OFF · SETBACK`). Combined with the physical edge nodes this makes the
> automation visible end to end: touch the ESP32's presence pin and the room lights up
> in the twin; release it and the energy-saving setback visibly darkens it seconds later.
>
> ### 2026-07-11 — Hardware-in-the-loop: physical ESP32 + Raspberry Pi Pico nodes
>
> **The twin now mirrors real hardware, not just the model.** Telemetry carrying a
> genuinely measured temperature (new `tempReal` wire flag) pins the bound zone's
> physics to the physical sensor — warm the chip with a fingertip and the dashboard
> (and TimescaleDB history) follows in seconds, while the 2R1C model resumes seamlessly
> the moment the node goes quiet. Each unknown board auto-binds to its own office zone,
> so an ESP32 and a Pico demo side by side; bindings are inspectable at
> `GET /api/hardware` and flagged with a ⚡ LIVE HARDWARE badge in the zone micro-HUD.
> New `edge/pico` package: MicroPython node (RP2040 internal temp sensor, BOOTSEL
> presence toggle, onboard-LED lights actuation) plus a USB-serial↔MQTT bridge that
> gives the radio-less Pico full network presence — a Pico W connects over WiFi
> directly. The ESP32 firmware gains zero-wiring touch presence (GPIO32) with instant
> publish on change. Flash-and-demo guides: `edge/esp32/README.md`, `edge/pico/README.md`.
>
> ### 2026-07-11 — SkeySpot symbol detector: training complete, accuracy validated
>
> **The blueprint symbol detector is production-ready.** The SkeySpot `yolo11n` detector
> completed its full 100-epoch training run on CubiCasa5K (1024×1024 floorplan sheets,
> ≈4 h wall-clock on an Apple M4 via MPS) and validated at **69.5% mAP@50**
> (48.1% mAP@50–95) with **73.0% precision / 69.7% recall**. Convergence was smooth —
> final validation box/cls losses of 0.98/0.82 with the learning rate decayed to 1.8e-5 —
> and showed no sign of overfitting. In practice this automates roughly 70% of the
> electrical-symbol digitization workload per floorplan; the remaining misses are cheap to
> repair by drag-and-drop in the 3D dashboard editor. Full metric profile and analysis now
> documented in research paper §3.2 below.
>
> ### 2026-06-25 — Digitized building deployed to the live twin
>
> **The real digitized floorplan now drives the twin.** The latest floorplan-digitization output
> (15 floors / 1350 zones) is deployed to both the engine (`server/data/`) and the dashboard
> (`dashboard/src/building-data.json`), so the 3D model and 2D topology render exactly the building
> the Go engine simulates. The engine boots it cleanly and streams live building load (~17.6 MW)
> with per-zone telemetry persisted to TimescaleDB — selecting a zone now shows its real
> temperature/occupancy history sparklines (previously empty whenever the dashboard and engine were
> built from different `building-data.json` copies).
>
> **Topology builder hardened for digitized ontologies.** `buildTopologyFromSim` now normalizes the
> Brick ontology to `{source, target, predicate}`, tolerating both the legacy
> `{ relationships: [...] }` object and the digitization pipeline's flat
> `{subject, predicate, object}` triple array. An `undefined` relationships array from the new
> format had been crashing the desktop dashboard to a black screen.
>
> ### 2026-06-25 — YOLO Integration & Backend Data Lifecycle
>
> **Computer Vision Floorplan Parsing.** The SkeySpot YOLOv11 model weights (`best.pt`, trained on CubiCasa5K) are now integrated into `detector.py`. This allows the platform to natively ingest raw 2D blueprints, detect physical boundaries, and classify bounding boxes for electrical appliances and structural components directly into the semantic ontology.
>
> **Human-in-the-loop veto latching.** Engineered a 15-minute latch (`OverrideUntil`) into the Go Physics Engine. When an operator manually overrides an actuator (e.g. "Lights Off"), the autonomous optimizer respects the veto and suspends automation for 15 minutes instead of instantly re-evaluating the zone state.
>
> **TimescaleDB Continuous Aggregates & Retention.** Upgraded the SQL schema to automatically bucket raw sub-second sensor telemetry into 5-minute averages. Added stringent lifecycle data management: raw high-fidelity telemetry is pruned after 7 days, while 5-minute downsampled historical aggregates are retained for 90 days.
>
> ### 2026-06-24 — TimescaleDB history persistence & manual-override veto
>
> **Self-recording history.** The Go engine now persists its own telemetry to the TimescaleDB
> container once per second — global load/CO₂ and per-zone temperature/occupancy (`server/db.go`).
> Writes go through a non-blocking buffered channel + background batch writer (one multi-row insert
> per flush), so the 30 FPS broadcast loop is never stalled. A new `GET /api/history?zone=&minutes=`
> endpoint serves second-bucketed history (`time_bucket`), and the dashboard seeds its delta-card
> sparklines from it on mount before continuing with the live stream — falling back to the live
> stream alone when the DB container is down.
>
> **Human-in-the-loop override.** Operators can now veto the autonomous optimizer from the dashboard:
> the micro-telemetry and zone panels expose FORCE OFF / MAX COOL / PURGE / RESET buttons that send a
> `{action, zone}` payload over the WebSocket. `main.go` routes it to `Engine.PublishCommand`, which
> normalizes the action (high-level verbs *or* raw firmware strings) to the `LIGHTS_x;SETPOINT=y`
> format the ESP32 and optimizer share, then publishes `econ/commands/<zone>`. The override is
> transient — the occupancy optimizer reasserts control on the next tick.
>
> ### 2026-06-24 — Attention camera, volumetric airflow & forecast wiring
>
> **Attention-floor camera.** Opening the dashboard now frames the *whole tower top-to-bottom*
> centred on the floor that "needs attention" (data-driven `ATTENTION_FLOOR` = the default
> critical asset's floor). Injecting a fault flies the camera straight to the faulting floor.
> Implemented as `towerFraming()` + `DynamicControls` in `dashboard/src/BuildingModel.jsx`.
>
> **Volumetric, layout-constrained airflow (real physics, not particles).** The airflow window
> was a cosmetic particle field; it is now a genuine **masked 3D potential-flow solve** over a
> voxel grid (`flowfield.js` → `flowfield3d.js` → `ConstrainedAirflow3D.jsx`). Air is injected at
> ceiling supply diffusers (strength ∝ live VAV flow), drawn down to low returns near the core,
> relieved at windows, and bent through doorways — **never crossing a wall** (exact no-flux
> Neumann boundary). The solver is documented mathematically in the paper below (§4.4) and in
> [`dashboard/AIRFLOW_AND_CAMERA.md`](dashboard/AIRFLOW_AND_CAMERA.md).
>
> **In-model infrastructure.** The active floor of the main 3D model now renders its real
> physical services — HVAC (AHU → supply ducts → ceiling diffusers → low returns), an electrical
> grid (panel → cable trays → junction boxes), per-zone sensors (camera / thermostat / CO₂), and
> live occupant capsules sized to per-zone occupancy (`dashboard/src/FloorInfrastructure.jsx`),
> all driven by the same `flowfield.js` domain so the model and the airflow window agree.
>
> **DeepFloorplan → airflow bridge.** `floorplan_to_buildingdata.py` now emits a per-floor
> `airflowDomain = { doors, windows }` from detected room adjacency + the building envelope, so a
> digitized blueprint drives the airflow directly; the frontend derives the same domain
> geometrically when it is absent (procedural testbed).
>
> **Forecast ↔ engine wired.** The Go engine exposes `GET /api/forecast` (`server/forecast.go` +
> `Engine.ForecastWindow`), which proxies a live telemetry window `[room_temp, airflow_fraction]`
> to the Python LSTM forecaster and returns its predicted peak cooling load (verified end-to-end:
> `200 {predicted_peak_load: ~2.07, …}`; `503` when the forecaster is down). `FORECAST_URL` wires
> it in `docker-compose.yml`.
>
> ### Earlier
>
> **Occupancy-Driven MQTT Loop.** The Go engine is wired directly to the Mosquitto broker as the
> single brain for both physics and IoT actuation. It subscribes to real telemetry
> (`econ/telemetry/+`), feeds occupancy into the thermal model, and publishes actuation commands
> (`LIGHTS_OFF;SETPOINT=…` to `econ/commands/<zone>`) when zones go vacant past a safety delay. An
> `eclipse-mosquitto` broker was added to `docker-compose.yml`.
>
> **Real metrics — nothing hard-coded.** `GlobalData` now also streams `coolingOutputMw`,
> `plantCop` (a *dynamic* coefficient of performance that degrades with plant strain), and
> `energySavedMw` (from occupancy setback); the engine reads real per-zone occupancy from the
> stream. The dashboard (desktop overview + Tesla-style mobile corners) shows only these
> engine-computed values — the old fabricated solar/grid/COP ratios and random seeds are gone.
> *Verified live:* injecting a fault drops COP and system health; a vacancy makes `energySavedMw` > 0.
>
> **Edge devices, real contract.** The ESP32 firmware now parses the engine's *combined* command
> `LIGHTS_ON|OFF;SETPOINT=<c>` (earlier sketches only matched a literal `"LIGHTS_ON"` and never
> fired) and publishes the telemetry JSON the engine expects. The Raspberry Pi runs an **autonomous
> failsafe gateway** (`edge/raspberry_pi/gateway.py`) — it hosts the broker and only cuts lights
> *when the engine is unreachable* and a zone stays vacant (defers to the engine otherwise),
> replacing the old MQTT↔WebSocket bridge.
>
> **Branch B digitization bridge.** `ai_modules/branch_b_digitization/floorplan_to_buildingdata.py`
> turns a 2D floorplan into the exact `building-data.json` schema the engine + twin consume
> (DeepFloorplan adapter as the upgrade segmenter, OpenCV working today). *Verified:* a real
> floorplan → 15 floors / 210 zones with full thermal properties + HVAC mapping. Note: The classical 
> computer vision pipeline (`cv2.watershed`) was re-engineered to fix severe expansion bugs, but polygon 
> extraction remains highly inaccurate for dense commercial floorplans.
>
> **Hardware-in-the-loop tracking.** Replaced raw static IoT logic with dynamic physical tracking.
> Integrated PyTorch YOLOv11/ByteTrack running on Apple MPS (Metal Performance Shaders) to count
> occupants crossing virtual doorways. Telemetry publishes live to the Go Engine, which routes 
> actuation commands back to a Python ESP32 simulator listening on wildcard MQTT topics.
>
> **WebGL blackout fix.** Every `<Canvas>` is wrapped in an auto-recovering `CanvasErrorBoundary`,
> so a transient render error self-heals instead of permanently blanking the 3D view.

> **📚 Deep specs:** [`docs/BACKEND_ARCHITECTURE.md`](docs/BACKEND_ARCHITECTURE.md) (engine internals, the
> FlatBuffers + MQTT wire contracts, how to add a streamed metric, build/run with no local
> `go`/`flatc`) · [`ai_modules/branch_b_digitization/LAYOUT_SCHEMA.md`](ai_modules/branch_b_digitization/LAYOUT_SCHEMA.md)
> (the building-data schema + DeepFloorplan ingestion) · [`edge/raspberry_pi/README.md`](edge/raspberry_pi/README.md)
> (broker + failsafe setup).

## What is not built yet

The honest ledger of gaps now lives in **[docs/ROADMAP.md](docs/ROADMAP.md)**, so this file
describes the system that exists and that one describes the system that is planned.

## 🚀 Development Process & Architecture

During **Sprint 1 (Core Architecture & Simulation)**, we built a robust and highly scalable foundation:

1. **High-Performance Telemetry Streaming (Go & FlatBuffers)**
   - Replaced basic JSON polling with a persistent WebSocket connection.
   - Integrated Google FlatBuffers to serialize the simulation state into a compact binary format, allowing the backend to stream building data at a flawless 30 FPS without choking the network or browser memory.

2. **Thermodynamic Physics Engine (Go)**
   - Built a custom simulation engine (`engine.go`) that models thermal mass (`CWall`, `CAir`), thermal resistance (`RIn`, `ROut`), and internal heat loads (people/servers).
   - Implemented a Hardy Cross network solver to calculate dynamic airflow based on VAV damper resistances and AHU pressure.
   - The engine correctly balances external weather, internal heat, and HVAC cooling capacity to reach realistic thermal equilibriums.

3. **3D Visualization Engine (React Three Fiber)**
   - Created a 3D rendering pipeline that dynamically generates multi-story floor plates and thermal zones based on geometric polygons defined in `building-data.json`.
   - Built a custom GLSL Shader using a `smoothstep` heatmap to smoothly and dynamically color the 3D zones based on real-time temperature deviation from the setpoint.

4. **2D P&ID Topology Mapping (React Flow)**
   - Mapped the HVAC systems (AHUs, VAVs, Zones) into an interactive 2D node graph using React Flow.
   - Applied smooth CSS transitions matching the 3D GLSL shader to provide consistent, jitter-free visual feedback across the entire application.

5. **AI Auto-Pilot & Fault Scenarios**
   - Implemented interactive Scenarios (Peak Load, Critical Fault).
   - The "Critical Fault" triggers a 5x thermal runaway in the server room by dynamically throttling VAV airflow.
   - The "AI Auto-Pilot" detects the thermal anomaly and issues a SCADA override script to aggressively route cooling to the core, successfully stabilizing the building.

6. **Advanced Telemetry Analytics & AI Insights**
   - Built a sleek Left Dock Navigation supporting toggleable tabs for **AI Insights** and **Telemetry Logs**.
   - **Dynamic Root Cause Analysis (RCA):** AI Insights accurately diagnoses the fault target, predicting the blast radius and cause (e.g., "CRAC unit compressor failure" for server rooms, "VAV damper stuck" for offices).
   - **Thermodynamic Characteristic Chart:** A real-time auto-scaling Recharts scatter plot showing CO₂ vs Power. The live telemetry dots dynamically follow the ideal thermodynamic slope under normal conditions, but violently break away from the baseline during injected faults to visibly demonstrate anomalies.
   - **Live Terminal Logging:** The Telemetry Logs tab renders a hacker-style real-time terminal of up to 30 active zones, showcasing live temperatures, loads, and occupancy streaming directly from the WASM engine.

7. **Mobile UI Adaptation (Live Impact Screen)**
   - Viewports ≤ 820 px are automatically served `MobileImpactScreen.jsx` — a lightweight, phone-first face of the twin that skips the WebGL stack entirely (three canvases + React Flow would crush a phone on a 1350-zone building).
   - Every card is fed by the same live FlatBuffers stream as the desktop: an autonomous-savings donut (engine `energySavedMw`), current load against the LSTM-predicted peak, and a live occupancy-by-level chart.
   - Physical edge nodes (ESP32 / Pico) appear with online status, pinned temperature, and occupancy — the hardware demo works from a phone.
   - Headline stat strip (load MW, occupants, system health) updates in real time at the top of the screen.

8. **Edge Hardware Integration (ESP32, Raspberry Pi Pico & Pi Gateway)**
   - **ESP32 Edge Node (`C++`):** Production firmware with zero-wiring capacitive touch presence (GPIO32, hysteresis + debounce), optional DHT22/PIR real sensors, `LIGHTS_x;SETPOINT=y` actuation, MQTT Last-Will liveness, and per-site credentials in a gitignored header.
   - **Raspberry Pi Pico Node (MicroPython):** RP2040 internal temperature sensor (real `tempReal` data), BOOTSEL presence toggle, onboard-LED lights actuation, an 8 s hardware watchdog for self-recovery, and a USB-serial↔MQTT bridge that gives the radio-less Pico full network presence (a Pico W connects over WiFi directly).
   - **Hardware-in-the-loop engine:** telemetry flagged `tempReal:true` pins the bound zone's physics to the physical sensor (graceful release on staleness or LWT offline); unknown boards auto-bind to distinct office zones; reconnecting boards get the current command re-sent; live bindings at `GET /api/hardware`.
   - **Raspberry Pi Gateway (`Python`):** Mosquitto broker host plus an autonomous failsafe rules engine — if the Go brain goes offline, vacant zones are still set back locally (`;SRC=FAILSAFE` tagged commands).
## Running it

Full setup — engine, dashboard, broker, forecaster, digitizer — is in
**[docs/RUNNING.md](docs/RUNNING.md)**.
