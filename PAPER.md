## Abstract

A Building Management System manages chillers, air handlers, pumps and lighting on a fixed
schedule, and stops at the wall socket. Three Vietnamese and Australian field studies show
what that boundary costs: in a 117,000 m² Hanoi office tower already running a full BMS, the
**largest single end use was plug load at 26.4%** — an end use the BMS neither meters nor
controls; a Darwin case study found the schedule that saved energy in one season *increased*
it in the next; and comfort complaints were resolved by manual intervention because nothing
in the loop could tell a hot room from a hot sensor. The common failure is not bad control
but **absent measurement**: a fixed rule cannot know what it does not sense, and cannot know
whether the room it is setting back can recover in time.

**ECON** is a building digital twin built against that gap. It couples (i) an autonomous
floorplan-digitization pipeline that turns a DXF, PDF or phone photo of a plan into metric
zone polygons, a Brick-schema ontology and a full thermal fixture; (ii) privacy-preserving
edge occupancy sensing — YOLO + ByteTrack over a local video stream that leaves only a scalar
count, alongside ESP32/Pico nodes carrying temperature, CO₂, presence, illuminance and
current-clamp sensors; (iii) a Go physics engine that resolves every zone as an
energy-conserving **2R1C** circuit against live outdoor weather, with per-zone envelope
resistance derived from façade, roof and partition geometry, a closed-form reduction of the
Hardy Cross duct network, load-dependent plant COP, and a fresh-air term that in a tropical
office is the **largest single cooling load** (2,155 kW of 3,283 kW at design occupancy —
66%, and mostly latent); and (iv) an online identification layer in which **recursive least
squares with conditional forgetting** fits each room's own thermal and CO₂ balance from its
own history, recovering physical coefficients — time constant, cooling authority,
per-occupant gain, air-change rate — that supersede the configured prior.

Identification is what converts the twin from a simulator into a controller that can refuse:
setback depth is *solved* from the room's identified dynamics against the time remaining
until reoccupancy, so a room that cannot recover is never set back in the first place. The
same learned layer drives per-(zone, metric, hour) baselines scored in σ, an automated
plug-load sweep that closes the loop the BMS could not, and a model bundle that can be
exported and scored offline.

We report what is verified separately from what is not. Fifty Go tests pass, including
recovery of a synthetic room's time constant and cooling authority to within 15% of ground
truth and a regression test for a live failure in which closed-loop collinearity destroyed
the fit. Deriving envelope resistance from geometry moved **93% of zones into the 1–40 h**
time-constant band a real office exhibits, and correcting a mis-digitized fixture moved
reported grid power from **15.2 MW to 0.7 MW**. Against this, no room has yet passed the
36-sample maturity gate, so **no identified coefficient is in use by the controller**; the
savings projection of ≈11.9% of building electricity applies assumed reduction fractions to
measured end-use shares, and only its air-conditioning component has independent support.
Demand-controlled ventilation, on-site generation and reinforcement-learning supervisory
control are specified here but **not implemented**, and no figure in this paper depends on
them. Sections below cite the implementing source; **Appendix A is the authoritative
inventory of which module is live, which is built but unwired, and which is superseded.**

---

> **Provenance.** This paper merges the original ECON systems paper — edge CV (§3),
> autonomous digitization (§4), the thermodynamic engine and forecasting (§5), the software
> architecture (§6), and the physics of §7.1–7.6 — with the per-room identification work:
> the BMS-failure evidence (§2), the identification mathematics (§7.7–7.11), measured
> results (§8), and the implementation inventory (Appendix A). Where the original text made
> a claim the code no longer supports, the claim has been corrected in place and the change
> noted, rather than quietly dropped.

## 1. Introduction
Commercial office buildings are massive consumers of electricity, with HVAC systems accounting for over 40% of their total energy footprint. Currently, these systems rely on coarse scheduling (e.g., ON at 8:00 AM, OFF at 6:00 PM). This results in "ghost cooling"—the conditioning of empty conference rooms and under-utilized sectors. 

ECON addresses this through five capabilities, each of which is implemented and running:

1. **Real-Time Occupancy Tracking:** Privacy-preserving edge AI counts inhabitants per zone and publishes only a scalar (§3).
2. **Automated Topology Digitization:** A segmentation pipeline parses 2D plans into metric zone polygons, a Brick ontology and a thermal fixture (§4).
3. **First-Principles Thermodynamic Simulation:** A Go engine integrates a 2R1C heat balance per zone against live weather, with a closed-form duct solve and a fresh-air term (§5, §7.1–7.7).
4. **Online Per-Room Identification:** Recursive least squares fits each room's own thermal and CO₂ dynamics from its own history, replacing configured priors with measured coefficients (§7.8).
5. **Plug-Load Sensing and Control:** The end use the BMS cannot see is modelled per zone, measured where a clamp is fitted, and swept on verified vacancy (§2.1, §7.6).

A sixth capability — reinforcement-learning supervisory control (§5.5) — is specified but
**not implemented**; it is described here as design intent, and no result in this paper
depends on it. The full live/unwired split is Appendix A.

---

## 2. The measurable gap in conventional BMS

### 2.1 The largest end use is invisible to the BMS

Luong et al. [1] metered a 45-storey, 117,000 m² Hanoi office tower from September 2024 to
May 2025. The building is not unmanaged: it runs water-cooled chillers, high-performance LED
lighting with daylight sensors, and a BMS controlling *"chillers and cooling towers of air
conditioning, lighting, pumps, ventilation fans, elevators."*

| End use | Share of energy |
|---|---|
| **Plug loads** | **26.4%** |
| Air-conditioning | 25.1% |
| Outdoor lighting | 16.1% |
| Ventilation | 15.3% |
| Indoor lighting | 9.1% |
| Elevators | 7.8% |
| Domestic water pumps | 0.2% |

The largest end use in a fully BMS-managed tower is the one category the BMS has neither a
meter nor an actuator on. A BMS manages *plant*; plug load sits downstream of the socket
outlet. The paper's own recommendation — *"automated plug load controls, awareness programs,
and shutdown protocols"* — describes a capability a plant-level BMS does not have.

> **A correction to the source.** Luong et al. report one set of end-use shares for energy
> (their §3.2) and a different set for CO₂ (their §3.3): indoor lighting 9.1% of energy but 16% of
> emissions, outdoor lighting 16.1% of energy but 0.4% of emissions. Applying a *single*
> grid emission factor, as the paper does, carbon share must equal energy share. The two
> cannot both be correct, and the pattern suggests indoor/outdoor labels were transposed in
> one figure. We use the energy shares throughout as the internally consistent set.

### 2.2 Strategies do not transfer across seasons

Meng et al. [3] field-tested three BMS control strategies on a mixed-use office in Darwin, a
hot-humid tropical climate comparable to southern Vietnam, reporting each by season:

| Strategy | Wet season | Dry season |
|---|---|---|
| Space temperature setpoint reset | +8% | +11% |
| Chilled water delivery temperature optimisation | +5% | +7% |
| **Condenser water delivery temperature optimisation** | **−7%** | +3% |

The same unchanged strategy lost 7% in one season and gained 3% in the other. A rule
commissioned once is not merely suboptimal for part of the year; it can be actively
negative, and nothing in the BMS reports it. The same authors note that a Canberra retrofit
saving 60% of annual energy *"has a drastically different climate to Darwin and no similar
current studies have been conducted in a hot humid tropical climate"* — strategies validated
in temperate markets arrive in Vietnam unvalidated.

### 2.3 Comfort fails silently

From the same study: *"the thermal comfort of the occupants was compromised during the wet
season when both these strategies were implemented."* A plant-optimising BMS measures the
plant. Without a per-room model of whether a given room can still recover to setpoint, it
cannot distinguish a saving from a comfort failure until an occupant complains. The
literature the same paper cites is blunter still: *"the BMS is not used to its full potential
in many buildings, performing little more than time clock functions."*

Hoang et al. [2] surveyed 57 commercial and government offices in Hanoi and Ho Chi Minh
City, finding mean energy use intensity of 105.9, 116.4 and 109.6 kWh/m²·yr for Hanoi, HCMC
and both, with monthly intensity tracking ambient air temperature — precisely the
sensitivity a fixed setpoint cannot express.

---

## 3. Edge AI & Occupancy Tracking (Branch A)
To achieve fine-grained control and capture the proven 10-20% energy savings of occupant-centric management, the system must know exactly how many people are in a specific thermal zone.

### 3.1 Privacy-Preserving Object Detection
We deploy an **Ultralytics YOLO** person detector natively on edge hardware (Apple Silicon MPS, CUDA, or CPU) to run inference on localized camera feeds. To strictly preserve occupant privacy, the architecture adheres to a zero-cloud-video policy: frames are captured via local CSI/USB, processed entirely in volatile RAM during inference, and immediately discarded. The system extracts only scalar telemetry (`zone`, `occupancy`, `source`), which is published over MQTT on the *same wire contract as a hardware node* — `econ/telemetry/<topic>` for readings and a retained `econ/status/<topic>` with a Last Will for liveness — so a webcam pointed at a doorway is indistinguishable, to the engine, from an ESP32.

> **Implementation note.** The running node (`ai_modules/branch_a_occupancy/yolo_bytetrack/yolo_tracker.py`) defaults to **`yolov8n.pt`**, selectable with `--model`; YOLOv11 is used in Branch B for symbol detection (§4.2). An earlier revision of this paper stated YOLOv11 here. The engine attributes CV counts to source `"cv"` and, by design, **never lets them pin zone physics** — that is reserved for genuinely measured temperatures.

### 3.2 Tracking-by-Detection (ByteTrack)
To ensure individuals are accurately tracked without double-counting, we employ **ByteTrack** for Multi-Object Tracking ([GitHub Repository](https://github.com/ifzhang/ByteTrack)). 

In dense indoor office environments (e.g., lobbies), simple centroid tracking fails rapidly when people overlap or change scale, leading to identity switches. Conversely, algorithms like DeepSORT ([GitHub Repository](https://github.com/nwojke/deep_sort)) rely heavily on appearance embeddings, which degrade under indoor lighting variations, partial occlusion, or when workers wear visually similar clothing. 

ByteTrack outperforms both by associating *every* detection box — including low-confidence ones — using motion and box-overlap cues. Let the high- and low-confidence detection sets be $\mathcal{D}\_{\text{high}}$ and $\mathcal{D}\_{\text{low}}$. A Kalman filter (Kalman, 1960) first predicts each tracklet's location $\mathcal{T}$ in the current frame; the engine then forms the Intersection-over-Union (IoU) cost matrix between tracklets and high-confidence detections,

$$
C(i, j) = 1 - \text{IoU}\!\left(\mathcal{T}_i,\, \mathcal{D}_j\right),
$$

and solves the optimal assignment with the Hungarian algorithm (Kuhn, 1955). Tracklets left unmatched after this first pass are then associated against $\mathcal{D}\_{\text{low}}$ in a secondary matching pass. This recovers an occupant's identity tracklet when their confidence momentarily drops under partial occlusion, instead of discarding it and double-counting on re-entry.

---

## 4. Autonomous Digital Twin Digitization (Branch B)
Manually mapping the electrical and HVAC topology of a building into a Digital Twin is cost-prohibitive. ECON automates this via a three-step Computer Vision pipeline applied directly to architectural PDFs.

### 4.1 Semantic Room Segmentation
ECON adopts DeepFloorplan's two-head structure — a wall/boundary head and a room-type head ([GitHub Repository](https://github.com/zlzeng/DeepFloorplan)) — behind a single interface (`ai_modules/branch_b_digitization/deepfloorplan_infer.py`) with **two interchangeable engines**, because a digitization pipeline that only runs when a model downloads is a pipeline that does not run at a customer site:

- **Classical (default).** Wall mask from adaptive thresholding and morphology, then watershed on the non-wall interior seeded by distance-transform peaks, so adjacent rooms separate even across open doorways. No model, no network, works anywhere.
- **Neural (opt-in, `ECON_DF_ENGINE=neural`).** A SegFormer-B0 fine-tuned on floorplans (`Patnev71/segformer-b0-finetuned-floorplan`, 3.7 M parameters, MPS/CPU) replaces the fragile dark-pixel wall threshold with a learned Room/Background mask; the *same* watershed, polygon-extraction and typing stages then run on top. Weights are cached under `models/` and the path falls back to classical if `torch`/`transformers` or the download is unavailable.

Either engine emits binary raster room regions ($R\_i$) that are contour-traced to polygons. **Room type is geometric in both cases** — no off-the-shelf segmenter emits commercial archetypes (office / server-room / conference), so type is inferred from area, aspect ratio, topological position and any OCR'd label text.

> **Implementation note.** An earlier revision described this stage as a PyTorch **CubiCasa5K** segmenter. CubiCasa5K is the *training set for the symbol detector* of §4.2, not the room segmenter; the correction is recorded rather than silently applied. A `deepfloorplan_weights.py` drop-in hook remains for a true CubiCasa5K or TF2-DeepFloorplan port.

### 4.2 Symbol Detection via Real-World Datasets (CubiCasa5K)
Electrical symbols (lights, VAV boxes, thermostats) and structural boundaries (doors, windows) are isolated using a **YOLOv11** object detector, inspired by approaches like SkeySpot ([GitHub Repository](https://github.com/HAIx-Lab/Skeyspot)). To ensure the model generalizes across diverse architectural drafting styles, the lightweight `yolo11n` (nano) network was trained directly on the **CubiCasa5K** dataset (5,000 real-world, high-quality floorplans) rather than purely synthetic data — 100 epochs at 1024×1024 input resolution on Apple Metal Performance Shaders (MPS), roughly four hours of wall-clock training on an M4. The final validation telemetry (`results.csv`, epoch 100):

| Metric | Value |
|---|---|
| mAP@50 | **69.5%** |
| mAP@50–95 | 48.1% |
| Precision | 73.0% |
| Recall | 69.7% |
| Final validation box / cls loss | 0.98 / 0.82 |
| Final learning rate (decayed) | 1.8 × 10⁻⁵ |

Source artifact: `ai_modules/branch_b_digitization/skeyspot/runs/detect/models/cubicasa_run-5/results.csv`, final row (epoch 100); weights at `weights/best.pt` in the same directory.

For a nano-scale detector resolving small, densely packed symbols on full-sheet architectural drawings, this is a strong operating point. A precision of 73.0% keeps the false-positive rate low — when the model asserts a symbol, it is usually right — while a recall of 69.7% means the pipeline autonomously digitizes roughly 70% of the physical assets present on a flat PDF. The loss curves converged smoothly with no evidence of overfitting. Crucially, the residual ~30% (missed or slightly misplaced symbols) is inexpensive to repair: facility engineers simply drag-and-drop the remaining assets in the 3D dashboard editor. The detector therefore does not need perfect recall to be transformative — it eliminates the bulk of the manual data-entry hours previously required per floorplan.

> **Not wired.** The detector is trained and its weights are in the repository, but it is **not on the deployed digitization path**. The `digitizer` FastAPI service imports only `floorplan_to_buildingdata`, which performs room segmentation (§4.1) and geometric zone/VAV assembly; it never calls `skeyspot.detector.infer_symbols`. Symbol detection currently runs only through the standalone `skeyspot_pipeline.py`, and its output is not merged into `building-data.json`. The reconciliation mathematics of §4.3 is therefore **specified and implemented in the standalone pipeline, but not exercised by the running twin**, where VAVs and lights are instead assigned one-per-zone from the segmented geometry. See Appendix A.

### 4.3 Geometry Reconciliation & Graph Output
To automatically connect the detected HVAC/lighting symbols (Section 3.2) to their respective thermal zones (Section 3.1), we implement a geometric overlap algorithm. 

Let each room mask $R\_i$ be a binary region from segmentation, and each detection box $B\_j$ be the symbol bounding box from YOLO. We compute the bounding box center $c\_j$ and calculate the assignment score $s(i,j)$:

$$
s(i,j) = \alpha \cdot \frac{|B_j \cap R_i|}{|B_j|} + (1-\alpha)\cdot \mathbf{1}\!\left[c_j \in R_i\right]
$$

where $0 \le \alpha \le 1$ trades off fractional-overlap against a hard centroid-containment test $\mathbf{1}[\cdot]$. The symbol is assigned to the room

$$
i^{*} = \arg\max_{i}\, s(i,j),
$$

resolving ambiguities when symbols are drafted near doorways or bounding walls.

### 4.4 Space Syntax Topological Analysis
To understand the spatial logic of the digitized rooms without requiring expensive 3D BIM models, ECON calculates the **Closeness Centrality** (Integration Score) of the generated topological graph. The Mean Depth ($MD$) and Integration ($I$) for a room $x$ are calculated as:

$$
MD(x) = \frac{\sum_{y \neq x} d(x, y)}{N - 1}
$$

$$
I(x) = \frac{1}{MD(x)}
$$

Where $d(x, y)$ is the shortest-path topological distance between room $x$ and room $y$, and $N$ is the total number of rooms. This allows the autonomous engine to mathematically determine the "core" zones of the facility. This reconciled geometry is output as a directed JSON graph, seamlessly mapping the physical 2D layout directly into the 3D React Three Fiber frontend.

---

## 5. Thermodynamic Simulation & Forecasting
The core brain of ECON is a continuous physics simulation engine written in Go.

### 5.1 Thermodynamic RC-Network Modeling
The building is modeled as a lumped Resistor–Capacitor (RC) network. The rate of change of the indoor air temperature $T\_z$ in a given zone obeys a first-order energy balance:

$$
C_z \frac{dT_z}{dt} = Q_{\text{HVAC}} + Q_{\text{internal}} + Q_{\text{envelope}} + Q_{\text{solar}}
$$

where

- $C\_z$ — thermal capacitance of the zone air and thermal mass $(\mathrm{J/K})$;
- $Q\_{\text{HVAC}}$ — net sensible cooling/heating power delivered by the VAV terminal $(\mathrm{W}$, negative for cooling$)$;
- $Q\_{\text{internal}}$ — metabolic heat from occupants $(\approx 100\,\mathrm{W}$ per person$)$ plus equipment loads;
- $Q\_{\text{envelope}}$ — conductive/convective transfer through the boundary, $U A\,(T\_{\text{ext}} - T\_z)$;
- $Q\_{\text{solar}}$ — radiative solar gain through fenestration.

To obtain the electrical power drawn by the chiller plant, the sensible cooling duty is divided by the plant Coefficient of Performance (COP):

$$
P_{\text{electrical}} = \frac{|Q_{\text{HVAC}}|}{\text{COP}}
$$

The implementation refines this single-node model into a two-state **2R1C** circuit that separates the fast air node from the slow wall node (§7.1), and the full building electrical load is assembled in §7.6. The engine integrates the resulting ODE system server-side in Go at a fixed $\approx 30\,\mathrm{Hz}$ tick ($\Delta t = 33\,\mathrm{ms}$) and streams the state to the browser as packed FlatBuffers over WebSockets, so the numerical integration never blocks the rendering thread.

### 5.2 Airflow Balancing (Hardy Cross Method)
As Variable Air Volume (VAV) dampers modulate to satisfy local $Q\_{\text{HVAC}}$ demands, the pressure across the building's ductwork shifts. ECON balances the network with the **Hardy Cross method**, whose general loop-flow correction for an arbitrary looped topology is

$$
\Delta Q = -\,\frac{\sum r\,Q\,|Q|^{\,n-1}}{\sum n\,r\,|Q|^{\,n-1}},
$$

with the turbulent-duct exponent $n = 2$. This tunes the Air Handling Unit (AHU) fan to the minimum required static pressure. Because ECON's AHU→VAV layout is a purely parallel star, the network admits a closed-form equivalent-resistance solution rather than iteration; that reduction — the form actually implemented in `engine.go` — is derived in detail in §7.3.

### 5.3 Time-Series Load Forecasting (LSTM)
To transition from reactive cooling to proactive pre-cooling, ECON incorporates a **Long Short-Term Memory (LSTM)** network (Hochreiter & Schmidhuber, 1997). The gated cell state lets the model retain long-range dependencies in weather and occupancy without the vanishing-gradient problem that afflicts vanilla RNNs. At each timestep $t$, with input $x\_t$ and previous hidden state $h\_{t-1}$, the cell computes the forget, input, and output gates and updates its memory $c\_t$:

$$
\begin{aligned}
f_t &= \sigma\!\left(W_f\,[h_{t-1}, x_t] + b_f\right) & \text{(forget gate)}\\
i_t &= \sigma\!\left(W_i\,[h_{t-1}, x_t] + b_i\right) & \text{(input gate)}\\
\tilde{c}_t &= \tanh\!\left(W_c\,[h_{t-1}, x_t] + b_c\right) & \text{(candidate)}\\
c_t &= f_t \odot c_{t-1} + i_t \odot \tilde{c}_t & \text{(cell update)}\\
o_t &= \sigma\!\left(W_o\,[h_{t-1}, x_t] + b_o\right) & \text{(output gate)}\\
h_t &= o_t \odot \tanh(c_t) & \text{(hidden state)}
\end{aligned}
$$

where $\sigma$ is the logistic sigmoid and $\odot$ the Hadamard product. As implemented (`backend/forecasting/model.py`), the network is a 2-layer LSTM with hidden width $64$ over a sequence of $L=12$ timesteps; each timestep is the standardized feature vector

$$
x_t = \big[\,T_{\text{room},t},\; \phi_{\text{flow},t},\; T_{\text{out},t},\; \mathrm{RH}_{\text{out},t}\,\big]
$$

(zone temperature, airflow fraction, outdoor temperature, outdoor humidity). A linear head maps the final hidden state $h\_L$ to the scalar prediction — the building **peak cooling load** in MW:

$$
\hat{P}_{\text{peak}} = W_{\text{out}}\, h_L + b_{\text{out}}.
$$

The model is trained on data synthesized from the engine's own load physics ($P\_{\text{build}} = Q\_{\text{cool}}/\text{COP} + P\_{\text{base}}$, §7.6), reaching a validation MAE of $\approx 0.045\,\mathrm{MW}$. At runtime the Go engine assembles the live $[\,T\_{\text{room}}, \phi\_{\text{flow}}\,]$ window via `Engine.ForecastWindow`, the FastAPI service appends the cached weather features, and the LSTM returns the forecast through `GET /api/forecast`.

### 5.4 Layout-Constrained Airflow (Masked Potential Flow)
Rather than scatter cosmetic particles, ECON solves a **layout-constrained potential-flow field** so the visualized air actually respects walls, doors, diffusers, and returns (`dashboard/src/flowfield3d.js`). Treating the conditioned air as an incompressible, irrotational flow, the velocity is the gradient of a scalar potential $\phi$, and mass conservation with distributed sources reduces to a Poisson equation:

$$
\mathbf{v} = \nabla \phi, \qquad \nabla^2 \phi = S(\mathbf{x}),
$$

where the source term $S$ injects air at ceiling supply diffusers (strength proportional to the live VAV flow of §7.3, amplified on a zone alarm) and withdraws it at the low returns and window relief vents. Solid walls impose a **no-flux Neumann boundary** so air never crosses a partition:

$$
\frac{\partial \phi}{\partial n}\Big|_{\partial\Omega_{\text{wall}}} = 0,
\qquad
\sum_{\mathbf{x}} S(\mathbf{x}) = 0,
$$

the second (net-zero source) condition being the compatibility requirement that makes the pure-Neumann problem solvable. The discretized system is solved over a masked voxel grid (≈ $60\times40\times8$, ceiling supply / floor return / mid-height window relief) with **Gauss–Seidel** relaxation, excluding wall cells from each stencil. Streamlines are then traced for visualization by advecting massless tracers through the trilinearly-sampled field with explicit Euler:

$$
\mathbf{P}_{t+\Delta t} = \mathbf{P}_t + \mathbf{v}(\mathbf{P}_t)\,\Delta t.
$$

The solve is memoized on a coarse key (rounded VAV flow, bucketed occupancy, alarm flips), so it only re-runs when the field meaningfully changes, keeping the rendering interactive. When a digitized blueprint supplies an `airflowDomain` (real door and window positions from Branch B), the solver consumes it directly; otherwise the domain is derived geometrically from shared-edge adjacency and the envelope.

### 5.5 Reinforcement Learning Operations — *design intent, not implemented*

> **Status: NOT IMPLEMENTED.** There is no RL agent, no policy network, no training loop and
> no replay buffer anywhere in the repository. The supervisory controller that actually runs
> is `Engine.actuate()` — a deterministic, physics-gated optimizer whose setback depth is
> *solved* from each room's identified dynamics (§7.9), not learned. This section states the
> MDP formulation the project would adopt if RL were pursued, and is retained because §7.5's
> comfort score is deliberately built from the same discomfort kernel, so the two would agree
> if a policy were ever trained. **No result in this paper comes from an RL policy.**

To frame building operation as a Markov Decision Process (MDP) $(\mathcal{S}, \mathcal{A}, P, R, \gamma)$, following the occupant-centric building-control RL literature (Wei et al., 2017; Vázquez-Canteli & Nagy, 2019):

- **State $S\_t$** — current zone temperatures, occupancy, dynamic grid prices, and weather forecasts.
- **Action $A\_t$** — HVAC setpoints, pre-cooling activation, and battery dispatch.
- **Reward $R\_t$** — a multi-objective signal penalizing both energy expenditure and thermal discomfort, where the discomfort term grows quadratically with any excursion beyond the comfort deadband $\delta$:

$$
R_t = -\left( \alpha\,\text{EnergyCost}_t + \beta \sum_{z} \big(\max(0,\,|T_z - T_{\text{set}}| - \delta)\big)^2 \right).
$$

An agent would maximize the discounted return $\mathbb{E}\big[\sum\_t \gamma^t R\_t\big]$. The quadratic-excess discomfort kernel above **is** implemented — as the bounded system-health score of §7.5 — so the dashboard's live "health" metric is already expressed in the objective any future policy would be trained against. That consistency is the implemented part of this section; the policy is not.

---

## 6. Software Architecture & Digital Twin UI
To bridge the gap between static architecture and real-time IoT data, ECON implements a highly optimized web architecture.

### 6.1 Semantic Ontologies (Brick Schema)
Rather than parsing raw IFC (Industry Foundation Classes) files—which contain gigabytes of useless geometric data—ECON leverages the **Brick Schema**. Brick is an open-source RDF ontology designed specifically for smart buildings, mapping logical relationships (e.g., `VAV_01 -> brick:feeds -> Zone_A`). By exposing a `/api/ontology` endpoint, the React frontend dynamically renders equipment P&ID diagrams based purely on semantic graph traversal, entirely decoupling the UI from hardcoded topological JSON assumptions.

### 6.2 High-Performance Telemetry Serialization
Traditional REST/JSON pipelines crash browser garbage collectors when attempting to stream 30 FPS telemetry for 135+ zones. ECON solves this by serializing the simulation state into tightly packed binary structs using **Google FlatBuffers** over WebSockets. The React frontend accesses the data directly via byte-offsets (Zero-copy deserialization), achieving flawless 30 FPS rendering on the 3D WebGL heatmaps with negligible memory overhead.

### 6.3 WebGL Rendering Strategy & Optimization
Built using **React Three Fiber**, the platform binds declarative React state (e.g., `selectedZone`) directly to 3D scene updates. A major challenge in mobile WebGL rendering is VRAM exhaustion caused by converting complex geometries to non-indexed formats to generate wireframe `<Edges>`. To prevent `webglcontextlost` crashes on mobile GPUs, ECON implements strict conditional rendering: edges are only computed for the *active* floor and dynamically injected, while the base building relies on cached, indexed Constructive Solid Geometry (CSG). Furthermore, a `CanvasErrorBoundary` intercepts context losses to reload the engine gracefully.

### 6.4 Mobile UX & Spatial Data Binding
Taking design cues from premium interfaces like the Tesla Energy app, the mobile UX prioritizes a top-down isometric 3D view anchored by floating WebGL-to-DOM labels. Data overlays utilize absolute positioning projected from 3D world coordinates to 2D screen space, connected by vertical "drop lines." Using `100dvh` combined with bottom-drawer navigation paradigms ensures the 3D context remains permanently visible without conflicting with iOS Safari's dynamic address bar.

### 6.5 Logical Topology Mapping
While the physical layout is rendered in Three.js, the underlying mechanical lineage (e.g., Chiller $\rightarrow$ AHU $\rightarrow$ VAV box $\rightarrow$ Zone) is rendered using a 2D node-based graph via **ReactFlow**. This duality allows facility managers to debug both spatial problems ("The south perimeter is hot") and mechanical dependencies ("Which VAV serves the south perimeter?"). Thermodynamic characteristic charts (powered by **Recharts**) simultaneously plot CO₂ vs Power to identify mechanical anomalies.

### 6.6 Time-Series Telemetry & Data Lifecycle
To persist the massive influx of simulated and physical telemetry, ECON integrates **TimescaleDB** (a time-series extension for PostgreSQL). The Go engine utilizes non-blocking buffered channels and background batch writers to flush sub-second telemetry without stalling the 30 FPS broadcast loop. To manage long-term storage and prevent database bloat, the schema employs continuous aggregates: raw high-fidelity data is automatically bucketed into 5-minute averages, with raw data strictly pruned after 7 days and downsampled historical aggregates retained for 90 days. Query routing follows the window: requests under ~6 h read the raw hypertable at 1-second buckets, longer windows read the continuous aggregate.

Persistence is **optional and degrades cleanly**. If `DB_URL` is unset or the database is unreachable, `initDB` logs the reason, leaves the handle nil, and the engine runs in full — physics, control, identification and the live websocket are unaffected; only `/api/history` and `/api/series` return empty. This matters for the identification layer of §7.8, which learns from an in-process ring buffer rather than from the database, and therefore converges identically with or without TimescaleDB running.

### 6.7 Supervisory Human-in-the-Loop Override (Veto Latching)
While ECON operates as an autonomous system, facility managers must retain supervisory control. Operators can issue direct commands (e.g., `FORCE OFF`, `PURGE`) via the React frontend. These WebSocket payloads are normalized by the Go engine into edge-compatible strings (e.g., `LIGHTS_OFF;SETPOINT=26.0`) and broadcasted via MQTT. Crucially, the engine implements a temporal veto latch (`OverrideUntil`); when a human issues a command, the autonomous occupancy optimizer respects the manual override and suspends its own control loop for 15 minutes, preventing the AI from instantly reversing the operator's decision. A second, coarser gate sits above it: `AutoPilot` is a real engine flag, not a dashboard decoration — with it off, `actuate()` returns immediately and the twin becomes a pure observer.

### 6.8 The Learned-Model Surface

The architecture of §6.1–6.7 describes the twin as it streams *physics*. A second surface
exposes what it has **learned**, and it is separated deliberately: physics is available the
instant the engine boots, whereas every endpoint below is meaningless until enough real data
has passed through it, and each one therefore reports its own maturity rather than answering
confidently from a cold start.

- **`/api/rooms/models`** — the per-room identified coefficients of §7.8: time constant,
  cooling authority, per-occupant gain, air-change rate, sample count, and whether the room
  has passed the maturity gate. A room below the gate reports its coefficients *and* its
  immaturity, so a caller can never mistake a prior for a measurement.
- **`/api/recommendations`** — σ-scored deviations from the learned per-(zone, metric, hour)
  baselines of §7.10, ranked and turned into actions.
- **`/api/model`, `/api/model/export`** — the twin's learned intelligence packaged for
  offline use: the baseline model, its threshold spec, a **dependency-free Python 3 standard-library
  recommender** that reproduces the engine's σ-scoring exactly, and the trained LSTM weights
  when the forecasting service was reachable at export time. The bundle's exit code is the
  alert signal (0 / 1 / 2), so a cron job can page on it without parsing anything.
- **`/api/model/recommend`** — hardware-matched tier selection. Which bundle an operator
  should run is a hardware question (a standard-library scorer runs on a decade-old laptop;
  TimesFM on an accelerator does not) and the browser is the only party that knows what
  machine it is on. The client measures — `deviceMemory`, `hardwareConcurrency`, the WebGL
  unmasked-renderer string — and the server decides. Each of those probes is **absent on some
  browser**, and every gap is reported as absent rather than guessed, with the server
  labelling its estimate accordingly.

This surface is where the paper's central claim is operationalized: a twin that cannot say
how confident it is in a learned coefficient is not usable for control, and §7.9 refuses to
act on an immature one.

---

## 7. Mathematical Foundations & Physics Engine Rationale

To ensure ECON operates as a deterministic, physical Digital Twin rather than a superficial dashboard, the Go backend (`econ/server/simulation/engine.go`) implements a strict state-space thermodynamic and fluid dynamics model. 

### 7.1 The 2R1C Lumped-Capacitance Thermodynamic Model
While purely data-driven models often fail out-of-distribution during critical HVAC faults or thermal-runaway events, a physical 2-Resistor / 1-Capacitor (2R1C) equivalent-circuit model guarantees first-law (energy-conservation) consistency by construction. Following the simplified RC building-model formulation reviewed by Kramer et al. (2012) and the ASHRAE heat-balance method, each zone resolves transient heat transfer between the outdoor environment, the wall thermal mass ($C\_{\text{wall}}$), and the indoor air volume ($C\_{\text{air}}$) through an inner resistance $R\_{\text{in}}$ (air ↔ wall) and an outer resistance $R\_{\text{out}}$ (wall ↔ outdoors):

$$
\frac{dT_{\text{air}}}{dt} = \frac{1}{C_{\text{air}}} \left[ \frac{T_{\text{wall}} - T_{\text{air}}}{R_{\text{in}}} + \dot{q}_{\text{int}} - \dot{q}_{\text{cool}} \right]
$$

$$
\frac{dT_{\text{wall}}}{dt} = \frac{1}{C_{\text{wall}}} \left[ \frac{T_{\text{out}} - T_{\text{wall}}}{R_{\text{out}}} - \frac{T_{\text{wall}} - T_{\text{air}}}{R_{\text{in}}} \right]
$$

where $\dot{q}\_{\text{int}}$ is the aggregate internal heat load (occupants + equipment + solar gain) and $\dot{q}\_{\text{cool}}$ is the active sensible cooling delivered by the VAV terminal unit (§7.2).

> **Implementation note — the thermal boundary is live; the solar term is not.**
> $T\_{\text{out}}$ is fed from the same Open-Meteo service the LSTM of §5.3 consumes: a poller
> refreshes it every 10 minutes and the envelope integrates against the measured value, so the
> physics and the forecaster now share one boundary condition. The freshness contract matches the
> zone sensors': a reading older than three hours stops driving $R\_{\text{out}}$ and the model
> falls back to the $30\,^{\circ}\mathrm{C}$ design-day constant — degraded, and *reported* as
> degraded via `/api/weather` (`live: false`) rather than silently. The remaining asymmetry is
> solar: $\dot{q}\_{\text{solar}}$ is still a static per-zone multiplier, not an irradiance
> signal, so "weather-driven" here means the conduction path, not the radiation path.

Collecting the state $\mathbf{T} = [\,T\_{\text{air}},\, T\_{\text{wall}}\,]^{\top}$, this is a linear state-space system $\dot{\mathbf{T}} = \mathbf{A}\mathbf{T} + \mathbf{b}$ with

$$
\mathbf{A} =
\begin{bmatrix}
-\dfrac{1}{R_{\text{in}}C_{\text{air}}} & \dfrac{1}{R_{\text{in}}C_{\text{air}}} \\
\dfrac{1}{R_{\text{in}}C_{\text{wall}}} & -\dfrac{1}{C_{\text{wall}}}\left(\dfrac{1}{R_{\text{in}}}+\dfrac{1}{R_{\text{out}}}\right)
\end{bmatrix},
\qquad
\mathbf{b} =
\begin{bmatrix}
\dfrac{\dot{q}_{\text{int}} - \dot{q}_{\text{cool}}}{C_{\text{air}}} \\
\dfrac{T_{\text{out}}}{R_{\text{out}}\,C_{\text{wall}}}
\end{bmatrix}.
$$

**Numerical integration & stability.** The engine advances the state with explicit (forward) Euler at each tick (`engine.go`, `z.Temp += dTAirDt * dt`):

$$
\mathbf{T}_{k+1} = (\mathbf{I} + \Delta t\,\mathbf{A})\,\mathbf{T}_k + \Delta t\,\mathbf{b}.
$$

The scheme is numerically stable while the amplification matrix has spectral radius $\rho(\mathbf{I} + \Delta t\,\mathbf{A}) \le 1$. Because the fast air node dominates the eigenvalues, this gives the practical step bound

$$
\Delta t < \frac{2}{|\lambda_{\max}(\mathbf{A})|} \approx 2\,R_{\text{in}} C_{\text{air}} = 2\,\tau_{\text{air}},
$$

i.e. the timestep must stay below twice the smallest zone time constant $\tau\_{\text{air}} = R\_{\text{in}}C\_{\text{air}}$. The base tick $\Delta t = 33\,\mathrm{ms}$ sits far inside this limit; the scenario engine only inflates $\Delta t$ (up to $2.0$) for *visual* fast-forward of recovery, which remains stable given the large zone capacitances ($C\_{\text{air}}, C\_{\text{wall}} \sim 10^{6}\,\mathrm{J/K}$).

**Capacitance** is derived from the room's own volume rather than assigned per type:

$$
C_a \;=\; \kappa\,\rho c_p\,A\,h,\qquad \rho c_p = 1206~\mathrm{J\,m^{-3}K^{-1}},\quad \kappa = 5
$$

The multiplier $\kappa$ lumps furniture, partitions and slab surface that couple to the air;
room air alone gives a time constant of minutes, where real rooms respond over hours.

**Resistance** is derived from the zone's own exposed area:

$$
(UA)_{\text{zone}} \;=\; U_w A_w \;+\; U_r A_r \;+\; U_p A_p, \qquad R \;=\; \frac{1}{(UA)_{\text{zone}}}
$$

$$
R_i = f_R\,R,\qquad R_o = (1-f_R)\,R,\qquad f_R = 0.4
$$

with $A\_w$ the façade area (from polygon edges lying on the building outline), $A\_r$ roof
area on the top floor, and $A\_p$ partition area. $U\_p$ is an *effective* coupling well below
the assembly U-value, because the 2R1C has a single outdoor node: conductance to a neighbour
at nearly the same temperature must enter as a smaller coupling to outdoors, or a core zone
is modelled as though the room next door were the weather.

### 7.2 HVAC Cooling Capacity & Nominal Flow Normalization
To prevent thermal drift across unequally-sized zones, the engine sizes each zone's cooling capacity against its VAV's *nominal* design flow, so that at nominal flow and setpoint the zone is in exact steady-state balance:

$$
\dot{q}_{\text{cool}} = \underbrace{\frac{\dot{m}}{\dot{m}_{\text{nom}}}}_{\text{flow ratio}} \cdot\; \dot{q}_{\text{total,nom}} \cdot \underbrace{\frac{T_{\text{air}} - T_{\text{supply}}}{T_{\text{sp}} - T_{\text{supply}}}}_{\text{thermal driving ratio}},
\qquad T_{\text{supply}} = 12\,^{\circ}\mathrm{C},
$$

where $\dot{m}$ is the live VAV mass-airflow, $\dot{m}\_{\text{nom}}$ its nominal value (captured once from the Hardy-Cross solve at default damper resistance, §7.3), and $T\_{\text{supply}} = 12\,^{\circ}\mathrm{C}$ is the conditioned supply-air temperature. The nominal total load that must be removed to hold setpoint is the sum of the nominal internal gains and the steady-state envelope conduction:

$$
\dot{q}_{\text{total,nom}} = \dot{q}_{\text{int,nom}} + \frac{T_{\text{out}} - T_{\text{sp}}}{R_{\text{in}} + R_{\text{out}}}.
$$

Normalizing by each VAV's own nominal flow (rather than a hard-coded reference) keeps the model correct regardless of how many VAVs share an AHU. Consequently any airflow reduction ($\dot{m} < \dot{m}\_{\text{nom}}$) — whether from an occupancy setback or a stuck damper — produces an immediate, physically-grounded drop in cooling capacity, driving the $T\_{\text{air}}$ equation of §7.1 into a warming state.

### 7.3 Hardy Cross Fluid Network Solver
When a VAV damper closes (from a fault or an occupancy-driven setback), the static pressure in the shared ductwork shifts, inherently forcing more airflow into the parallel zones. Each duct branch obeys the turbulent head-loss law

$$
h = K\,Q^{2} \quad (\text{exponent } n = 2),
$$

so the loop residual is non-linear in flow. The classical **Hardy Cross method** (Cross, 1936) solves a looped network by iteratively applying the flow correction that drives each loop's net head loss to zero — a Newton step on the loop residual:

$$
\Delta Q = -\,\frac{\sum K\,Q\,|Q|}{\sum 2K\,|Q|}.
$$

For ECON's topology — $V$ VAV branches in **parallel** between a common AHU plenum and the return — the network reduces to a single node, so the engine solves the equivalent system in closed form rather than looping (`doHardyCross`). At a common plenum-to-return pressure drop $\Delta P$, each branch carries $Q\_v = \sqrt{\Delta P / K\_v}$; summing and inverting gives the equivalent system resistance

$$
K_{\text{sys}} = \left( \sum_{v=1}^{V} K_v^{-1/2} \right)^{-2}.
$$

The AHU fan supplies a fixed pressure budget $P\_{\max}$ split between the fan curve coefficient $K\_{\text{fan}}$ and the network, fixing the total flow and the operating pressure:

$$
Q_{\text{tot}}^{2} = \frac{P_{\max}}{K_{\text{fan}} + K_{\text{sys}}},
\qquad
\Delta P = K_{\text{sys}}\,Q_{\text{tot}}^{2},
\qquad
Q_v = \sqrt{\frac{\Delta P}{K_v}}.
$$

This equivalent-resistance reduction is exact for the parallel star (no iteration needed), while the iterative form above remains available for genuinely looped duct topologies. Solving the network dynamically — rather than assuming static per-VAV flows — lets the AI Auto-Pilot train against the realistic, cascading aerodynamic consequences (a closing damper raising $K\_{\text{sys}}$ and redistributing flow to its neighbours) that occur during peak load or mechanical failure.

### 7.4 Dynamic Coefficient of Performance (COP) Degradation
The chiller plant's efficiency is not static: as zones drift above setpoint the chillers run at higher lift, so the plant COP degrades with thermal **strain**. We define the strain as the mean per-zone over-setpoint excess (clamped at zero, so well-conditioned zones contribute nothing):

$$
\text{Strain} = \frac{1}{N}\sum_{z=1}^{N} \max\!\left(0,\; T_z - T_{\text{sp},z}\right),
$$

and the plant COP follows an empirical, bounded degradation curve:

$$
\text{COP} = \max\!\Big(2.2,\; \min\!\big(3.8,\; 3.6 - 0.35\cdot\text{Strain}\big)\Big).
$$

This couples the thermodynamic state to the electrical state, so a thermal fault both degrades overall system health and spikes the `buildingLoadMw` metric broadcast to the dashboard.

### 7.5 Bounded Thermal-Comfort & System-Health Score
The dashboard's single "system health" figure is not an ad-hoc heuristic but a direct, bounded mapping of the same discomfort kernel that the RL reward of §5.5 penalizes. For each zone we measure the excess excursion *beyond* its comfort deadband $\delta$,

$$
e_z = \max\!\left(0,\; |T_z - T_{\text{sp},z}| - \delta\right),
$$

and convert it into a per-zone comfort score with a Lorentzian (Cauchy) kernel of half-width $\sigma = 2.5\,^{\circ}\mathrm{C}$, which scores $1$ in-band and decays smoothly toward $0$ as a zone runs away:

$$
c_z = \frac{1}{1 + e_z^{2}/\sigma^{2}} \in (0, 1].
$$

The broadcast system health is the mean comfort across all zones, expressed as a percentage:

$$
H = \frac{100}{N}\sum_{z=1}^{N} c_z.
$$

This grades the building by *severity* — a $0.1\,^{\circ}\mathrm{C}$ overshoot reads as essentially healthy while a runaway collapses toward $0$ — replacing the earlier binary in-band/out-of-band flag, while a separate discrete count of alarmed zones drives the hard fault indicators.

### 7.6 Building Electrical-Load Decomposition & Occupancy Savings

The total electrical demand streamed to the dashboard is assembled from the thermodynamic
state and the load-dependent COP of §7.4. Aggregating the sensible cooling delivered to every
zone gives the plant's thermal duty $Q\_{\text{cool}} = \sum\_z \dot q\_{\text{int},z}$
(MW-thermal); dividing by the COP yields the chiller draw, to which the non-cooling
electrical load is added:

$$
P_{\text{build}} \;=\; \frac{Q_{\text{cool}}}{\mathrm{COP}} \;+\; \underbrace{A_{\text{cond}}\,p_{\text{nonHVAC}}}_{\text{lighting, fans, lifts, pumps}} \;+\; \underbrace{\textstyle\sum_z P_{\text{plug},z}}_{\text{live plug draw}}
$$

Two things about this expression are deliberate, and both were corrections to an earlier
formulation that carried a **fixed $P\_{\text{base}} = 2.0\,\mathrm{MW}$**:

1. **The non-HVAC term scales with the building.** $A\_{\text{cond}}$ is the twin's own
   conditioned floor area (shoelace-summed from the zone polygons) and
   $p\_{\text{nonHVAC}} = 9.0\,\mathrm{W/m^2}$ comes from `programme-library.json` with its
   source. A constant sized for one fixture does not move when the building does, which is
   how a mis-digitized floorplan survived undetected (§9.1).
2. **Plug load enters as a live sum, not a constant.** $P\_{\text{plug},z}$ is the measured
   clamp reading where an SCT-013 is fitted and the model otherwise (§2.1), so the
   automated sweep's effect appears in building load *the moment sockets shed*. Burying
   plug load inside a baseline constant is precisely the blind spot that made it the
   largest end use in the case-study tower.

**Attributed savings.** For every zone currently in setback the engine credits two terms:

$$
P_{\text{saved}} \;=\; \frac{1}{10^{6}}\left[\sum_{z \in \mathcal{Z}_{\text{setback}}} A_z\,p_{\text{light},z}\;+\;\frac{1}{\mathrm{COP}}\sum_{z \in \mathcal{Z}_{\text{setback}}} U\!A_z \min\!\big(\Delta T_z,\; T_{\text{out}} - T_{\text{set},z}\big)^{+}\right]
$$

where $A\_z p\_{\text{light},z}$ is the zone's *own* lighting density from the programme
library (an open-plan floor and a store cupboard no longer save the same 2 kW),
$U\!A\_z = 1/(R\_{\text{in},z} + R\_{\text{out},z})$ is its geometry-derived envelope
conductance, and $\Delta T\_z$ is the setback depth actually applied.

The $\min(\cdot)$ and the positive part $(\cdot)^{+}$ are the physics that a percentage-based
credit gets wrong. Letting a room float saves conduction only against the driving temperature
difference that exists: if outdoor air is **below** the occupied setpoint there is no cooling
to avoid and the term is zero; if the setback is deeper than the available lift
$T\_{\text{out}} - T\_{\text{set}}$, only the lift is credited, because the room cannot drift
past ambient. The earlier formulation — a flat $0.25\,\dot q\_{\text{base}}$ of internal gain
per setback zone — credited a saving on a mild night that the building never made.

This is the `energySavedMw` figure on the dashboard: an attributable, occupancy-driven
avoided load bounded by the envelope, not a modelled percentage.

> **Correction.** An earlier revision of this section also described broadcast telemetry as
> perturbed by additive Box–Muller Gaussian noise. That synthetic jitter has been **removed**
> from the engine. Streamed values are now either genuinely measured or the physics state
> itself, on the rule that a modelled value must never travel on a channel implying it was
> measured; reference [20] is retained for the historical method only.

---

### 7.7 Fresh-air load

In a tropical climate the outdoor-air load is the largest single cooling term and is
predominantly *latent* — dehumidification, not sensible cooling:

$$
\dot{Q}_{\text{oa}} \;=\; N\,\dot{v}_{\text{oa}}\,\rho_{\text{air}}\,\Delta h
$$

with $N$ the building occupant count, $\dot{v}\_{\text{oa}} = 10$ L·s⁻¹ per person (QCVN
09:2017/BXD; ASHRAE 62.1), $\rho\_{\text{air}} = 1.2$ kg/m³, and $\Delta h \approx 55$ kJ/kg
the total enthalpy drop from Ho Chi Minh design outdoor air (33 °C, ~75% RH, ≈88 kJ/kg) to
the supply condition (≈12 °C saturated, ≈33 kJ/kg). At design occupancy this is 2,155 kW of a
3,283 kW total — **66%**. Because it scales with occupants present, it falls away out of
hours exactly as a demand-controlled air handler would allow.

### 7.8 Online system identification

Both balances are linear in their parameters, so each is written $y\_k = \mathbf{x}\_k^\top\boldsymbol{\theta} + \varepsilon\_k$:

$$
\underbrace{\frac{dT}{dt}}_{y} = \underbrace{\begin{bmatrix} T_o - \bar T & u(\bar T - T_s) & n & 1\end{bmatrix}}_{\mathbf{x}^\top}
\begin{bmatrix}\theta_0\\\theta_1\\\theta_2\\\theta_3\end{bmatrix},
\qquad
\underbrace{\frac{dC}{dt}}_{y} = \begin{bmatrix} n & C_{\text{out}} - \bar C & 1\end{bmatrix}\begin{bmatrix}\varphi_0\\\varphi_1\\\varphi_2\end{bmatrix}
$$

The recovered coefficients are physical: $\tau = 1/\theta\_0$ is the thermal time constant,
$\theta\_1$ the cooling authority, $\theta\_2$ the per-occupant gain as *this* room experiences
it, and $\varphi\_1$ the room's air-change rate in h⁻¹.

**Estimator.** Recursive least squares with forgetting factor $\lambda$:

$$
\mathbf{P}_{k-1}\mathbf{x}_k \;=\; \mathbf{g},\qquad
\iota_k = \mathbf{x}_k^\top \mathbf{g},\qquad
\mathbf{K}_k = \frac{\mathbf{g}}{\lambda + \iota_k}
$$

$$
\boldsymbol{\theta}_k = \boldsymbol{\theta}_{k-1} + \mathbf{K}_k\big(y_k - \mathbf{x}_k^\top\boldsymbol{\theta}_{k-1}\big),
\qquad
\mathbf{P}_k = \frac{1}{\lambda}\Big(\mathbf{P}_{k-1} - \mathbf{K}_k\mathbf{x}_k^\top\mathbf{P}_{k-1}\Big)
$$

with $\lambda = 0.999$, $\mathbf{P}\_0 = 50\,\mathbf{I}$. The scalar $\iota\_k = \mathbf{x}\_k^\top\mathbf{P}\_{k-1}\mathbf{x}\_k$ is the *information* the sample carries — how far it moves the
regressors into a direction the estimator is still uncertain about.

**Forgetting is conditional.** When $\iota\_k < \iota\_{\min}$ the update sets $\lambda = 1$.
This is the standard covariance-windup guard: dividing by $\lambda<1$ on an uninformative
sample inflates $\mathbf{P}$ without adding knowledge, and repeated often enough the
estimator becomes arbitrarily sensitive to the next sample — a well-documented failure of
exponential forgetting under poor excitation.

**Midpoint regressors.** The derivative is a finite difference over the sample interval, and
the regressor uses the *midpoint* state $\bar T = \tfrac{1}{2}(T\_k + T\_{k-1})$ rather than an
endpoint. With endpoint regressors the same measurement noise $\nu\_k$ appears in both $y\_k$
and $\mathbf{x}\_k$, giving $\mathbb{E}[\mathbf{x}\_k\varepsilon\_k]\neq\mathbf{0}$ — the
errors-in-variables condition — and the least-squares estimate is asymptotically biased
toward zero. The midpoint form makes the shared-noise contribution first-order cancelling.

**Excitation gating.** A sample is admitted only if a *driver* moved or the state is in
genuine transient:

$$
\text{accept} \iff \bigvee_j \frac{|d_{j,k} - d_{j,k-1}|}{s_j} > 1 \quad\lor\quad \Big|\frac{dT}{dt}\Big| \ge \eta
$$

over drivers $d = (T\_o, u, n)$ with per-driver scales $s\_j$. Gating on the *regressors*
instead admits sensor noise as though it were excitation — the free response of a room after
a step is genuinely informative about $\tau$, whereas a stationary room being jittered by
±0.2 °C of sensor noise is not.

**Uncertainty-weighted ridge.** Under closed-loop control the regressors become collinear —
flow is itself a function of temperature error — so the estimate can drift in the null space
without changing the residual. A leak toward the physical prior $\boldsymbol{\theta}^\ast$ is
applied per coefficient, weighted by that coefficient's *own* remaining uncertainty:

$$
\theta_i \;\leftarrow\; \theta_i + \gamma\,w_i\,(\theta^\ast_i - \theta_i),
\qquad w_i = \mathrm{clip}\!\left(\frac{P_{ii}}{P_0},0,1\right),\qquad \gamma = 0.002
$$

A well-determined coefficient has $P\_{ii}\ll P\_0$, so $w\_i\to0$ and it is left alone; a
poorly excited one is pulled gently back toward physics. An *unweighted* ridge drags
well-identified rooms off their measured values — observed moving a correctly identified
air-change rate from 0.5 to 0.89 h⁻¹.

**Sample spacing.** Observations are taken every $\Delta t\_s = 300$ s. This is a
signal-to-noise choice, not a responsiveness one. For sensor noise $\sigma\_\nu$, the
finite-difference derivative has standard error

$$
\sigma_{\dot T} \;=\; \frac{\sqrt{2}\,\sigma_\nu}{\Delta t_s}
$$

An SHT30 at $\sigma\_\nu \approx 0.2$ °C differenced over 30 s gives $\pm17$ °C/h against a
true rate of a few °C/h; over 300 s it falls to $\pm1.7$ °C/h. Maturity requires $k \ge 36$
accepted samples — three simulated hours, on the principle that a system whose time constant
is measured in hours cannot be characterised in minutes.

### 7.9 Setback depth as a solved quantity

This is where the identified model does work a schedule cannot. With the room empty ($n=0$)
and cooling at full flow ($u=1$), the thermal balance is first-order with pole

$$
k \;=\; \theta_0 - \theta_1
$$

and equilibrium

$$
T_\infty \;=\; \frac{\theta_0 T_o - \theta_1 T_s + \theta_3}{k}
$$

If $T\_\infty \ge T\_{sp}$ the room cannot reach setpoint even at full cooling; it has no
recovery margin and **the setback is refused**. Otherwise, integrating the free response
backwards over the recovery budget $t\_r$ gives the temperature from which the room can just
return in time:

$$
T_{sb} \;=\; T_\infty + (T_{sp} - T_\infty)\,e^{\,k\,t_r}
$$

$$
\boxed{\;\Delta_{\text{setback}} \;=\; \mathrm{clip}\Big(T_{sb} - T_{sp} - \epsilon,\; \Delta_{\min},\; \Delta_{\max}\Big)\;}
$$

with safety margin $\epsilon$ and $t\_r = 1800$ s. A light, responsive room earns a deep
setback; a heavy one earns a shallow one; a room that cannot recover earns none. **Comfort
cannot fail silently**, because the bound is derived from the room's own measured recovery
capability rather than assumed — which is precisely the failure reported in §2.3.

### 7.10 Baseline anomaly scoring

Independently of the physical models, each $(\text{zone},\text{metric},\text{hour})$ triple
carries an online mean and variance updated by Welford's algorithm, and observations are
scored in standard deviations:

$$
\mu_k = \mu_{k-1} + \frac{x_k-\mu_{k-1}}{k},\qquad
M_{2,k} = M_{2,k-1} + (x_k-\mu_{k-1})(x_k-\mu_k),\qquad
\sigma_k = \sqrt{\frac{M_{2,k}}{k-1}}
$$

$$
z \;=\; \frac{x - \mu}{\max(\sigma,\sigma_{\min})}
$$

The floor $\sigma\_{\min}$ prevents a series that has been quiet from generating enormous
scores on its first small deviation. This answers "abnormal *for this room, at this hour*",
which a fixed threshold such as $\mathrm{CO\_2} > 1000$ cannot express.

### 7.11 Energy, intensity and carbon

Avoided cooling from a setback is the envelope load the zone no longer holds down, bounded by
the available outdoor lift — it correctly vanishes when there is no lift to exploit:

$$
\dot{Q}_{\text{saved}} \;=\; \frac{1}{R_i + R_o}\,\min\big(\Delta_{\text{setback}},\; T_o - T_{sp}\big),\qquad \text{for } T_o > T_{sp}
$$

Annual energy intensity is defined on the **mean** load, since $\int\_0^{8760} P\,dt = \bar P \cdot 8760$ exactly:

$$
\mathrm{EUI} \;=\; \frac{\bar P \cdot 8760}{A_{\text{floor}}},\qquad
\bar P = \frac{\int P\,dt}{\int dt} \;\approx\; \frac{\sum_k \tfrac{1}{2}(P_k + P_{k-1})\,\Delta t_k}{\sum_k \Delta t_k}
$$

with intervals longer than 5 minutes excluded as unobserved rather than assumed constant.
Substituting an instantaneous $P$ for $\bar P$ is not an approximation but a different
quantity: an office at 09:00 with 3,000 people in it sits far above its own annual mean, and
the resulting figure over-reads by roughly 3×.

Operational carbon follows the national grid factor $\epsilon\_g = 0.6766$ tCO₂/MWh:

$$
E_{\mathrm{CO_2}} \;=\; \frac{\bar P\,[\mathrm{kW}] \cdot 8760 \cdot \epsilon_g}{1000}~\mathrm{tCO_2/yr}
$$

---

## 8. Results

We separate what is verified from what is not, because the distinction is the point.

### 8.1 Verified

**The estimator recovers known dynamics.** A suite of **50 Go tests passes** — 42 in the
simulation package and 8 in the server package (`go test ./...`).
`TestDynamicsIdentifiesKnownRoom` recovers a synthetic room's thermal time constant
and cooling authority to **within 15%** of ground truth;
`TestDynamicsIdentifiesAirChangeRate` recovers air-change rate to the same tolerance;
`TestDynamicsPredictsActualCrossing` predicts a threshold-crossing time within 20%;
`TestSetbackRefusedWhenRoomCannotRecover` confirms the capability refusal;
`TestIdentificationSurvivesClosedLoopOperation` is a regression test for a live failure in
which closed-loop collinearity destroyed the fit.

**Fixture calibration.** Design-day analysis gives 3,266 occupants at 12.2 m²/person, 3,283
kW thermal load (lighting 409, occupants 327, solar 9, plug 384, fresh air 2,155) and a
1,654 kW electrical peak, equivalent to **≈125 kWh/m²·yr** at 3,000 equivalent full-load
hours against a cohort of 109.6 [2]. Correcting the mis-digitized fixture moved reported
grid power from **15.2 MW to 0.7 MW** for the same building.

**Envelope realism.** Deriving resistance from geometry places **93% of zones in the 1–40 h**
time-constant band a real office exhibits, with genuine spread (0.7–10.8 h), against a prior
fixture whose flat resistance gave every zone the same value and a simulated time constant
of 1,300 h.

**Fresh-air load dominates.** At design occupancy the outdoor-air load is 2,155 kW of a
3,283 kW total — 66%, and mostly latent. A twin calibrated on internal gains alone
under-reports a tropical office by roughly a third.

**Hardware.** Two physical edge nodes publish live telemetry into the twin, and vendor IR
frames reach a real air conditioner.

### 8.2 Not demonstrated

**Identification is converging but has not matured.** Maturity requires 36 samples at 300
simulated seconds; with simulation time at 1× real this is ~3 hours of continuous uptime.
Convergence against sample count on the live instance, across all 735 zones:

| Samples per room (median) | Implied τ median | In 1–40 h band | Negative per-occupant | Cooling sign correct |
|---|---|---|---|---|
| 1 | 375 h | 7% | 88% | 90% |
| 3 (max 20) | **1.1 h** | **70%** | **4%** | **96%** |

This is the expected behaviour of a recursive estimator — at one sample it is the prior plus
noise — and it is also evidence that the earlier pathology was in the *model*, not the
estimator. On the previous fixture, whose flat envelope resistance gave every zone a
simulated time constant near 1,300 h, 36% of rooms reported that occupants *cool* the room.
With envelope resistance derived from geometry, that falls to 4% and the cooling-authority
sign is correct in 96% of rooms. The identifier had been faithfully recovering the physics of
a building modelled as a thermos.

**No room has yet passed the maturity gate** (median 3 of 36 samples at time of writing), so
no identified coefficient is in use by the controller. We report convergence, not
convergence *achieved*.

**Projected rather than measured savings.** Applying reduction fractions to the measured
end-use shares of [1] gives ≈11.9% of building electricity: plug loads 6.6% (26.4% × 25%),
air-conditioning 3.0% (25.1% × 12%), indoor lighting 2.3% (9.1% × 25%). The end-use shares
are measured; **the reduction fractions are assumptions.** Only the air-conditioning figure
has independent support: [3] measured 8–11% from setpoint reset alone on central plant. At
the Hanoi tower's peak month (219.4 t CO₂) an 11.9% cut is ≈26 t CO₂ avoided in one month.

---

## 9. Conclusion & Novel Contributions
ECON merges physical 3D space, logical 2D topology, and a first-principles thermodynamic engine into a single occupancy-aware Digital Twin. Its specific contributions are:

1. **An energy-conserving 2R1C engine with normalized cooling and a closed-form duct solver** (§7.1–7.3) that stays physically valid out-of-distribution — during faults and thermal runaway — where data-driven surrogates fail.
2. **A severity-graded comfort/health model** (§7.5) that derives the dashboard's live health metric from a quadratic-excess discomfort kernel — the same objective any future supervisory policy would be trained against (§5.5), keeping monitoring and control objectives consistent by construction rather than by coincidence.
3. **A layout-constrained, volumetric potential-flow airflow field** (§5.4) — masked Poisson solve with exact no-flux walls and balanced supply/return sources — that visualizes HVAC circulation which respects the real floor geometry, driven directly by the Branch B digitization output.
4. **An end-to-end occupancy loop**: edge CV (YOLO + ByteTrack) → MQTT → Go optimizer → ESP32 actuation, with an LSTM peak-load forecaster wired in over `GET /api/forecast` and a zero-shot TimesFM path over `GET /api/forecast/load`.
5. **Online per-room identification as a control precondition** (§7.8–7.9): recursive least squares recovers each room's own thermal and CO₂ dynamics from its own history, and setback depth is *solved* from those dynamics against the time remaining until reoccupancy — so the twin can **refuse** to set back a room it has measured to be incapable of recovering. This is the contribution that does not exist in a schedule-driven BMS, and the one that distinguishes ECON from a well-instrumented dashboard.
6. **Plug-load closure** (§2.1, §7.6): the largest end use in the case-study building is modelled per zone, measured where a clamp is fitted, and swept on verified vacancy — entering building load as a live sum rather than hiding inside a baseline constant.

By proving that a complex WebGL twin can run on mobile GPUs through aggressive geometry culling, and by deriving its dashboard figures from streamed engine state — measured wherever a sensor is bound, and labelled *modelled* wherever one is not — ECON extends the Digital Twin beyond the stationary control room. Real-world field testing of occupancy-presence sensing has validated up to **17.6%** HVAC energy savings (Bai et al., 2023; Chen et al., 2020). ECON's own projection is more modest and more explicit: ≈11.9% of building electricity, from measured end-use shares and assumed reduction fractions, with the assumption stated at every step (§8.2). The pathway to those targets here is measurement-driven rather than reinforcement-learning-driven — the identification layer is built and running, the RL layer is not.

### 9.1 Limitations & Roadmap

Stating the boundary of the claim is part of the claim. ECON's envelope, plant, airflow and
tariff models are first-principles and its measured quantities are genuinely measured, but the
following are **not implemented**, and the system should not be described as if they were:

1. **~~The envelope boundary condition is synthetic~~ CLOSED.** The 2R1C integrates against live Open-Meteo outdoor temperature, with `/api/weather` reporting what is in use and a climatological fallback only while the feed is down. Envelope *resistance* is now derived per zone from façade, roof and partition area (§7.1). Originally: (§7.1). $T\_{\text{out}}$ is a fixed
   $30\,^{\circ}\mathrm{C}$ constant and solar gain a static multiplier, so the 2R1C is exercised at a
   representative rather than a measured boundary. The weather feed already exists for §5.3;
   wiring it into $R\_{\text{out}}$ is the highest-value next step and would make a
   weather-driven-envelope claim true.
2. **Load shifting is forecast-triggered, not tariff-scheduled.** Pre-cooling opens when the
   predicted peak crosses a threshold; there is no scheduled charge ahead of the 17:30 EVN peak
   and no partial-hibernation setback across it. Setback remains vacancy-driven.
3. **No generation or storage-ageing model.** There is no on-site PV, and the BESS integrates SoC
   against real capacity and inverter limits without cycle degradation or round-trip loss — so
   arbitrage returns are an upper bound.
4. **~~Plug loads are invisible.~~ CLOSED.** `simulation/plugs.go` models per-zone plug draw and sweeps switchable sockets on verified vacancy; an SCT-013 clamp replaces the model with measured watts where fitted. Originally: The Hanoi case study attributes the largest single share of
   consumption (26.4%) to plug loads; ECON senses and actuates the *lighting and cooling* that
   surround an unoccupied desk, but never the desk's own draw. The saving is on the coupled
   HVAC/lighting load, not on the plug load itself, and should be quantified as such.
5. **Measured CO₂ drives no control loop.** *(Still open — and now quantified: ventilation is 15.3% of the case-study building's energy, and §8.2 deliberately excludes any saving from it.)* NDIR readings are ingested, streamed and persisted,
   but demand-controlled ventilation is not implemented — the sensing precedes the actuation.
6. **~~The demo fixture is not an office.~~ CLOSED.** The digitizer had labelled 555 closets of ~4 m² as server rooms at 85 kW each — 86% of connected load. `tools/officeize_fixture.py` re-derives programme and physics from geometry against a cited programme library; reported grid power moved 15.2 MW → 0.7 MW. Originally: ECON now reports both headline metrics of the cited
   literature — energy use intensity over the building's own digitized floor area (42,037 m²,
   summed by shoelace from the zone polygons) and Scope 2 operational carbon at Vietnam's grid
   emission factor. Computing them immediately exposed something the tariff and physics models
   could not: the bundled building is **86% server-room by connected load** (555 zones at 85 kW),
   giving a run-rate EUI of $\approx 3{,}700\,\mathrm{kWh/m^2{\cdot}yr}$ — roughly $32\times$ the
   $116.4$ office cohort, and squarely in data-centre territory. The engine, tariff and comfort
   models are unaffected, but every đồng figure the twin quotes is scaled to a 17.6 MW IT load
   rather than a Vietnamese office. The dashboard therefore suppresses the office benchmark
   whenever load is IT-dominated rather than printing a meaningless $32\times$ ratio, and the
   savings figures in this paper should not be read as office numbers until the fixture is
   recalibrated to a representative $\approx 40$–$120\,\mathrm{W/m^2}$ commercial load.
7. **No reinforcement-learning controller exists.** §5.5 specifies the MDP; nothing implements
   it. The supervisory controller that runs is deterministic and physics-gated. Any
   description of ECON as "RL-driven" is unsupported, and the abstract and conclusion have
   been corrected accordingly.
8. **The symbol detector is trained but not deployed.** The YOLOv11 electrical-symbol model of
   §4.2 reaches mAP@50 = 69.5% and its weights are in the repository, but the digitization
   service never calls it: VAVs and lights are assigned one-per-zone from segmented geometry,
   and the overlap-reconciliation mathematics of §4.3 runs only in a standalone script.
   Wiring it is the cheapest remaining improvement to fixture fidelity.
9. **Solar gain does not respond to time of day.** $\dot q\_{\text{solar}}$ scales with a
   façade-distance multiplier that is now derived from real geometry rather than assumed, but
   it carries no diurnal or cloud response — a west façade behaves identically at 08:00 and
   16:00. A BH1750 sensor (`-DUSE_LUX=1`) publishes measured illuminance and the engine
   ingests it, but **nothing drives solar gain from that measurement yet**; this is the same
   sensing-precedes-actuation pattern as limitation 5.
10. **The firmware is not deployable at building scale.** No OTA update path, MQTT anonymous
    on port 1883, per-board identity baked into build flags, and no local fail-safe when the
    broker is unreachable beyond the Raspberry Pi gateway. Forty ceiling boxes cannot be
    USB-flashed one at a time. This blocks a real installation harder than any missing
    sensor, and it is a deployment problem rather than a research one — which is precisely
    why it is easy to leave unstated.

---

## 10. References

### Occupancy-Centric HVAC & Energy Savings
1. Bai, Z., et al. (2023). *Long-term field testing of the accuracy and HVAC energy savings potential of occupancy presence sensors in a single-family home*. U.S. Department of Energy, OSTI.
2. Chen, Y., Hong, T., & Luo, X. (2020). *Nationwide HVAC energy-saving potential quantification for office buildings with occupant-centric controls in various climates*. Applied Energy, 269, 115103.
3. Akhtar, T., Mahmood, A., & Khatoon, S. (2024). *Occupancy detection for HVAC systems using IoT edge computing and vision-based image processing*. University of East London.
4. Abade, A., et al. (2021). *Quantifying the nationwide HVAC energy savings in large hotels: the role of occupant-centric controls*. Energy and Buildings.
5. Louisiana State University Repository. (2023). *Field testing of the energy-saving potential of an occupancy presence sensing system in an apartment unit*.

### Computer Vision: Detection, Tracking & Floorplan Analysis
6. Redmon, J., Divvala, S., Girshick, R., & Farhadi, A. (2016). *You Only Look Once: Unified, real-time object detection*. CVPR 2016. (YOLO family; YOLOv11 via Ultralytics, 2024.)
7. Zhang, Y., et al. (2022). *ByteTrack: Multi-Object Tracking by Associating Every Detection Box*. ECCV 2022. [GitHub: ifzhang/ByteTrack](https://github.com/ifzhang/ByteTrack)
8. Wojke, N., Bewley, A., & Paulus, D. (2017). *Simple online and realtime tracking with a deep association metric (DeepSORT)*. ICIP 2017. [GitHub: nwojke/deep_sort](https://github.com/nwojke/deep_sort)
9. Kalman, R. E. (1960). *A new approach to linear filtering and prediction problems*. Journal of Basic Engineering, 82(1), 35–45.
10. Kuhn, H. W. (1955). *The Hungarian method for the assignment problem*. Naval Research Logistics Quarterly, 2(1–2), 83–97.
11. Kalervo, A., et al. (2019). *CubiCasa5K: A dataset and an improved multi-task model for floorplan image analysis*. SCIA 2019. [GitHub: cubicasa/cubicasa5k](https://github.com/cubicasa/cubicasa5k)
12. Zeng, Z., et al. (2019). *DeepFloorplan: Deep multi-task floorplan recognition*. ICCV 2019. [GitHub: zlzeng/DeepFloorplan](https://github.com/zlzeng/DeepFloorplan)
13. HAIx Lab. (2025). *SkeySpot: Automating service-key detection for digital electrical layout plans in the construction industry*. IEEE SMC 2025. [GitHub: HAIx-Lab/Skeyspot](https://github.com/HAIx-Lab/Skeyspot)

### Thermodynamics, Fluid Networks & Forecasting
14. Kramer, R., van Schijndel, A., & Schellen, H. (2012). *Simplified thermal and hygric building models: A literature review*. Frontiers of Architectural Research, 1(4), 318–325.
15. ASHRAE. (2021). *ASHRAE Handbook — Fundamentals* (Ch. 18, Nonresidential Cooling and Heating Load Calculations; heat-balance method). ASHRAE, Atlanta.
16. Braun, J. E. (1990). *Reducing energy costs and peak electrical demand through building thermal mass*. ASHRAE Transactions, 96(2), 870–888.
17. Cross, H. (1936). *Analysis of flow in networks of conduits or conductors*. University of Illinois Engineering Experiment Station, Bulletin 286.
18. Hochreiter, S., & Schmidhuber, J. (1997). *Long short-term memory*. Neural Computation, 9(8), 1735–1780.
19. Stam, J. (1999). *Stable fluids*. SIGGRAPH 1999, 121–128. (Reference for the planned advection–diffusion upgrade of §5.4.)
20. Box, G. E. P., & Muller, M. E. (1958). *A note on the generation of random normal deviates*. Annals of Mathematical Statistics, 29(2), 610–611.

### Reinforcement Learning & Semantic Building Models
21. Wei, T., Wang, Y., & Zhu, Q. (2017). *Deep reinforcement learning for building HVAC control*. DAC 2017.
22. Vázquez-Canteli, J. R., & Nagy, Z. (2019). *Reinforcement learning for demand response: A review of algorithms and modeling techniques*. Applied Energy, 235, 1072–1089. (CityLearn.)
23. Balaji, B., et al. (2016). *Brick: Towards a unified metadata schema for buildings*. BuildSys 2016, 41–50.
24. Hillier, B., & Hanson, J. (1984). *The Social Logic of Space*. Cambridge University Press. (Space-syntax integration / closeness centrality.)

### Vietnamese building-energy case studies and policy
[1] Nguyen Duc Luong, Nguyen Thi Hue, Nguyen Huy Tien, Nguyen Van Duy, Hoang Xuan Hoa (2025).
*Assessing energy consumption and operational carbon emission: A case study of office
building in Hanoi.* Journal of Materials and Construction 15(02).
doi:10.54772/jomc.v15i02.1190

[2] Hoang Tuan Viet, Nguyen Duy Dong, Nguyen My Anh, Nguyen Duc Luong, Tran Ngoc Quang, Mac
Van Dat, Joseph J. Deringer (2021). *A study of energy consumption for office buildings in
Vietnam for sustainable energy and climate change mitigation.* Proceedings of the
International Conference on Evolving Cities, 63–70. doi:10.55066/proc-icec.2021.19

[3] Li Meng, Rita Yi Man Li, William Finocchiaro, Alemu Alemu (2023). *Integrating Building
Management Systems (BMS) and BIM to improve HVAC efficiency: a case study in Darwin.* EBIMCS
2023, ACM. doi:10.1145/3644479.3644494

[4] Socialist Republic of Vietnam (2022). *Decision 896/QĐ-TTg approving the National
Strategy on Climate Change to 2050.*

[5] QCVN 09:2017/BXD — National Technical Regulation on Energy Efficiency Buildings.

---

## Appendix A. Implementation Inventory

This appendix is the authoritative answer to a question the body of the paper can only answer
section by section: **for every module in the repository, is it running, is it built but
unwired, or is it superseded?**

It exists because the failure mode of a systems paper is describing the union of everything
ever built as though it were one running system. Three of this paper's own sections claimed
capabilities the code does not have (§3.1 model version, §4.1 segmenter, §5.5 the RL
controller), and one described a load decomposition the engine had replaced (§7.6). Each is
corrected in place above; this table is what makes the next such drift visible.

**Status vocabulary**

| Status | Meaning |
|---|---|
| **LIVE** | On the running path. `docker compose up` exercises it, or the dashboard calls it. |
| **LIVE (opt-in)** | Runs when a flag, env var or device is present; the system degrades cleanly without it. |
| **BUILT, UNWIRED** | Working code with no caller in the running system. |
| **INGESTED ONLY** | The measurement arrives and is stored; no downstream consumer acts on it. |
| **SPECIFIED** | Described in this paper; no implementation exists. |
| **SUPERSEDED** | Replaced by something else; retained for history or audit. |

---

### A.1 The runtime spine

What actually runs, in the order data moves:

```
blueprint ──▶ digitizer (FastAPI) ──▶ building-data.json ──▶ ┐
                                                             │
ESP32 / Pico / CV node ──▶ MQTT ──▶ mqtt.go ──▶ Engine.tick() │
                                                             ▼
                                   ┌──────────────────────────────────────┐
                                   │  engine.go   2R1C + Hardy Cross + COP│
                                   │  library.go  programme-library.json  │
                                   │  dynamics.go RLS identification      │
                                   │  baselines.go per-(zone,metric,hour) │
                                   │  recommend.go ranked actions         │
                                   │  plugs.go    vacancy sweep           │
                                   │  bess.go     TOU dispatch            │
                                   └──────────────────────────────────────┘
                                        │              │            │
                        FlatBuffers /ws │      REST /api/*          │ MQTT econ/commands/*
                                        ▼              ▼            ▼
                                   dashboard      dashboard     ESP32 relays / IR
```

Everything outside this diagram is in A.6 or A.7.

---

### A.2 Go engine — `server/`

| Module | Role | Consumed by | Status |
|---|---|---|---|
| `simulation/engine.go` | 2R1C integration, `actuate()`, `broadcast()`, hardware ingest | everything | **LIVE** |
| `simulation/library.go` | Loads `programme-library.json`; `Phys()`, `ProgrammeFor()`, `CriticalTypes()` | engine, plugs, fixture generator | **LIVE** |
| `simulation/dynamics.go` | RLS per-room thermal + CO₂ identification (§7.8) | `/api/rooms/models`, setback gate | **LIVE** |
| `simulation/baselines.go` | Learned per-(zone, metric, hour) normals, σ-scored (§7.10) | `recommend.go`, model export | **LIVE** |
| `simulation/recommend.go` | Ranked recommendations from both learned models | `/api/recommendations` | **LIVE** |
| `simulation/plugs.go` | Plug-load model, clamp override, vacancy sweep (§7.6) | `/api/plugs`, building load | **LIVE** |
| `simulation/bess.go` | Battery SoC integration and TOU dispatch | `broadcast()`, dashboard | **LIVE** — no ageing model (§9.1.3) |
| `mqtt.go` | Broker client; telemetry in, commands out | engine | **LIVE** |
| `weather.go` | Open-Meteo poller feeding $T\_{\text{out}}$ | 2R1C envelope, `/api/weather` | **LIVE** — falls back to design-day constant |
| `blueprint.go` | Digitizer proxy, deploy, backup, rollback | `/api/digitize`, `/api/building*` | **LIVE (opt-in)** — needs `DIGITIZER_URL` |
| `forecast.go` | LSTM proxy + TimesFM zero-shot path | `/api/forecast`, `/api/forecast/load` | **LIVE (opt-in)** — needs the Python service |
| `precool.go` | Forecast-triggered pre-cool window | `/api/precool` | **LIVE** — forecast-triggered, not tariff-scheduled (§9.1.2) |
| `db.go`, `db/init.sql` | TimescaleDB hypertable + continuous aggregates | `/api/history`, `/api/series` | **LIVE (opt-in)** — needs `DB_URL`; engine unaffected if absent |
| `modelcatalog.go` | Hardware-tier matching from browser probes | `/api/model/recommend` | **LIVE** |
| `modelexport.go` | Packages the learned models into a bundle | `/api/model/export` | **LIVE** |
| `schema/telemetry.fbs` | FlatBuffers wire schema (§6.2) | `/ws`, `dashboard/src/telemetry.ts` | **LIVE** |
| `cli/dashboard.go` | Separate terminal-dashboard binary | operator, manually | **BUILT, UNWIRED** — not part of the server |
| `test_thermo.go` | Standalone thermal scratch harness | — | **SUPERSEDED** — `//go:build ignore`; replaced by the `_test.go` suite |
| `preprocessing/*` | One-off IFC/Brick/fixture generators | — | **SUPERSEDED** by `tools/officeize_fixture.py` and the digitizer |

---

### A.3 API surface

| Endpoint | Served by | Dashboard caller | Status |
|---|---|---|---|
| `/ws` | `engine.broadcast()` | `useDigitalTwin.js` | **LIVE** |
| `/api/building-data` | `main.go` | `buildingStore.js` | **LIVE** |
| `/api/ontology` | `main.go` | ReactFlow topology (§6.5) | **LIVE** |
| `/api/history`, `/api/series` | `db.go` | `TelemetryPanel`, `MaintenanceDrawer` | **LIVE (opt-in)** |
| `/api/weather` | `weather.go` | `LiveWeatherBackground` | **LIVE** |
| `/api/hardware` | `main.go` | `TelemetryPanel` | **LIVE** |
| `/api/plugs` | `plugs.go` | `usePlugs.js` → 5 panels | **LIVE** |
| `/api/recommendations` | `recommend.go` | `useRecommendations.js` → `AiInsightsPanel`, `MobileAIScreen` | **LIVE** |
| `/api/precool` | `precool.go` | `useOpsStatus.js` | **LIVE** |
| `/api/forecast` | `forecast.go` (LSTM) | `AiInsightsPanel` | **LIVE (opt-in)** |
| `/api/model`, `/api/model/export`, `/api/model/recommend` | `modelexport.go`, `modelcatalog.go` | `useLocalModel.js` | **LIVE** |
| `/api/digitize`, `/api/building`, `/api/building/backups`, `/api/building/rollback` | `blueprint.go` | `BlueprintImportPanel` | **LIVE (opt-in)** |
| `/api/rooms/models` | `dynamics.go` | **none** | **BUILT, UNWIRED** — the identification results have no UI |
| `/api/forecast/load`, `/api/forecast/engines` | `forecast.go` (TimesFM) | **none** | **BUILT, UNWIRED** — zero-shot forecasting is reachable only by direct HTTP |

> The two unwired endpoints are the paper's clearest instance of its own thesis: the
> identification layer of §7.8 is the system's differentiator and **has no dashboard**, so the
> convergence table of §8.2 had to be assembled by querying the engine directly. Surfacing
> `/api/rooms/models` is the highest-value UI work outstanding.

---

### A.4 Dashboard — `dashboard/src/`

**Live:** `main.jsx` → `Root.jsx` (viewport split + `ErrorBoundary`) → `App.jsx` (desktop) or
`MobileApp.jsx` (< 768 px). Panels: `BuildingModel`, `TelemetryPanel`, `TelemetryLogs`,
`GlobalMetricsPanel`, `MaintenanceDrawer`, `BlueprintImportPanel`, `AiInsightsPanel`,
`PlugLoadPanel`, `AirflowWindow` → `ConstrainedAirflow3D` → `flowfield3d.js` (§5.4),
`FloorInfrastructure`, `LiveWeatherBackground`, `MobileEnergyScreen`, `MobileImpactScreen`,
`MobileAIScreen`. Hooks: `useDigitalTwin`, `usePlugs`, `useRecommendations`, `useOpsStatus`,
`useLocalModel`, `useMeanLoad`. Libraries: `api.js`, `tariff.js`, `sustainability.js`,
`flowfield.js`, `buildingStore.js`, `telemetry.ts`. Boundaries: `ErrorBoundary`,
`UIErrorBoundary`, `CanvasErrorBoundary`.

**Orphaned — no importer:**

| File | Superseded by |
|---|---|
| `ConstrainedAirflow.jsx` | `ConstrainedAirflow3D.jsx` (2D → volumetric) |
| `AirflowField.jsx`, `AirflowVectorField.jsx`, `VectorFieldFlow.jsx` | `flowfield3d.js` masked Poisson solve |
| `WindSimulation.jsx` | `LiveWeatherBackground.jsx` |

These are earlier iterations of the airflow visualization, retained because they document the
progression from cosmetic particles to a physically-constrained field. They are dead code in
the shipped bundle.

---

### A.5 Edge firmware — `edge/`

`esp32/src/main.cpp` compiles one binary whose sensor set is chosen entirely by build flags.
The governing rule is **omission over fabrication**: a sensor that fails to read causes the
field to be *absent from the JSON*, never sent as zero, because a fabricated zero on a current
clamp tells the twin the compressor is off.

| Flag | Sensor | Publishes | Engine consumer | Status |
|---|---|---|---|---|
| *(default)* | ESP32 capacitive touch | `occupancy` | occupancy, setback | **LIVE** |
| `USE_SHT30` | SHT30 (I²C) | `temperature`, `humidity`, `tempReal` | pins zone physics | **LIVE (opt-in)** |
| `USE_DHT` | DHT22 | `temperature`, `humidity` | as above | **LIVE (opt-in)** |
| `USE_PIR` | PIR | `occupancy` | occupancy | **LIVE (opt-in)** |
| `USE_MMWAVE` | HLK-LD2410C | `occupancy` incl. stationary | occupancy | **LIVE (opt-in)** |
| `USE_CO2` | ASAIR ACD1200 NDIR | `co2` | CO₂ identification (§7.8), σ-scoring | **LIVE (opt-in)** |
| `USE_PLUG` | SCT-013 clamp | `plugW` | replaces the plug model (§7.6) | **LIVE (opt-in)** |
| `USE_SUPPLY_TEMP` | DS18B20 | `supplyC` | supersedes design $T\_s$ in the RLS regressor | **LIVE (opt-in)** |
| `USE_AC_CLAMP` | 2nd SCT-013 | `acW` | stored as `HwAcW`; **no consumer** | **INGESTED ONLY** |
| `USE_LUX` | BH1750 | `lux` | stored as `HwLux`; **no consumer** | **INGESTED ONLY** |

Two sensors are honest dead ends today. `acW` is the real cooling-drive term and would let the
identifier regress against measured compressor power instead of commanded damper position;
`lux` is a real irradiance proxy that would close §9.1.9's static-solar-gain gap. Both arrive,
both are stored, neither changes a single output — and saying so is the point of this table.

| Other node | Role | Status |
|---|---|---|
| `pico/main.py` | RP2040 internal temp + BOOTSEL presence + LED; USB-serial or Pico W WiFi | **LIVE** |
| `pico/bridge.py` | USB-serial ↔ MQTT bridge for the non-W Pico | **LIVE** |
| `raspberry_pi/gateway.py` | Hosts Mosquitto; autonomous failsafe setback when the engine goes silent, tagged `;SRC=FAILSAFE` so it never mistakes its own command for the engine's | **LIVE** |
| `esp32/esp32_emulator.py` | Software node speaking the same wire contract, for testing without hardware | **LIVE** |

---

### A.6 Python and AI modules

| Module | Role | Status |
|---|---|---|
| `digitizer/app.py` | FastAPI: DXF / PDF / image → `building-data.json` | **LIVE (opt-in)** |
| `branch_b/floorplan_to_buildingdata.py` | Segmentation → metric polygons → zones, VAVs, windows | **LIVE** (via digitizer) |
| `branch_b/deepfloorplan_infer.py` | Classical watershed (default) or SegFormer-B0 (§4.1) | **LIVE**, neural path **LIVE (opt-in)** |
| `branch_a/yolo_bytetrack/yolo_tracker.py` | YOLO + ByteTrack occupancy node on the MQTT wire contract | **LIVE** |
| `backend/forecasting/` | FastAPI: LSTM `/predict` + TimesFM `/forecast/load` | **LIVE (opt-in)** |
| `tools/officeize_fixture.py` | Regenerates the fixture from geometry against the programme library | **LIVE** (run on demand) |
| `server/modelbundle/recommender.py` | Standard-library offline scorer shipped in the export | **LIVE** |
| `branch_b/skeyspot/detector.py`, `skeyspot_pipeline.py` | YOLOv11 symbol detection (§4.2), mAP@50 = 69.5% | **BUILT, UNWIRED** — digitizer never calls it (§9.1.8) |
| `branch_b/ocr_graph_search/netlist_builder.py` | Symbol → netlist graph | **BUILT, UNWIRED** — only via `skeyspot_pipeline.py` |
| `branch_b/geometry_merge.py` | Geometry reconciliation helper | **BUILT, UNWIRED** — no importer |
| `branch_a/count_occupancy.py` | Early tracker draft; inference calls commented out, count hardcoded | **SUPERSEDED** by `yolo_tracker.py` |
| `branch_a/density_map/`, `branch_a/rgbt_fusion/` | Crowd-density and RGB-thermal fusion | **SPECIFIED** — README only, no implementation |
| `backend/core_engine/` | FastAPI optimizer + SQLAlchemy models, an earlier architecture | **SUPERSEDED** — **zero references anywhere in the repository**; the Go engine replaced it entirely |

---

### A.7 Specified but not implemented

Consolidated from §9.1 so that one list answers "what does ECON not do":

| Capability | Where described | Why it is not built |
|---|---|---|
| Reinforcement-learning supervisory control | §5.5 | Deterministic physics-gated control is what runs; RL was never started |
| CO₂ demand-controlled ventilation | §9.1.5 | Sensing precedes actuation — CO₂ is measured, scored and identified against, but no ventilation loop acts on it |
| Irradiance-driven solar gain | §9.1.9 | `lux` is ingested but drives nothing |
| On-site PV / generation | §9.1.3 | No model of any kind |
| BESS cycle ageing and round-trip loss | §9.1.3 | SoC integrates against capacity and inverter limits only |
| Tariff-scheduled pre-cool and peak setback | §9.1.2 | Pre-cool is forecast-triggered; setback is vacancy-driven |
| Two-part tariff / capacity charge | `docs/ROADMAP.md` | Energy-only pricing is correct for commercial sites in Vietnam today |
| OTA firmware update, MQTT auth, per-board identity | §9.1.10 | Blocks a real installation; a deployment problem, not a research one |
| BACnet / Modbus / OPC-UA ingestion | `docs/ROADMAP.md` | ECON speaks MQTT to its own nodes; no path from an existing BAS |

**No result, figure or saving reported in this paper depends on any row of this table.**

---

### A.8 Keeping this appendix honest

The inventory above is mechanically checkable, and should be re-derived rather than trusted:

```bash
cd econ/server && go build ./... && go test ./...      # 50 tests
grep -rn "HandleFunc" server/main.go                    # API surface vs A.3
grep -rn "api/" dashboard/src/                          # which endpoints have callers
```

For orphaned dashboard modules, the check is a reverse-import sweep over `dashboard/src`: any
file with no importer other than itself is dead in the shipped bundle. For Python, the
equivalent question is whether `digitizer/app.py` or `backend/forecasting/main.py` transitively
imports the module — if not, it is A.6's "BUILT, UNWIRED" column regardless of how complete it
looks in isolation.

The rule this repository enforces, recorded in `CLAUDE.md` and applied throughout this paper:
**a capability leaves the "not implemented" list when something acts on it — not when a sensor
is bought, and not when a field is ingested.**
