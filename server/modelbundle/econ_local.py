#!/usr/bin/env python3
"""ECON local room analyst — run the twin's learned ROOM MODELS offline.

recommender.py answers "what is abnormal right now" from the learned baselines. This
answers the harder question the twin's room models make possible: what is each room about
to do, when, and which rooms cannot be held at all.

It is a faithful, dependency-free port of server/simulation/dynamics.go. For every room
the twin has identified, the export carries that room's own fitted physics:

    thermal:  dT/dt = θ0·(T_out − T_in) + θ1·flow·(T_in − T_supply) + θ2·people + θ3
    co2:      dC/dt = φ0·people + φ1·(C_out − C) + φ2

Because both are first-order and linear they integrate in closed form, so this script
computes — not guesses — where each room settles, what it reads at each horizon, and how
long until it crosses a limit. θ0 gives the room's thermal time constant, θ1 the cooling
its VAV actually delivers, φ1 its measured air-change rate.

    python3 econ_local.py analyze  sample_rooms.json     # full per-room prediction sweep
    python3 econ_local.py replay   history.json          # batch-replay a whole history
    python3 econ_local.py models                         # what has been identified so far

Files in this bundle it uses:
  dynamics.json    the per-room identified models (θ, φ, sample counts)
  baselines.json   the learned per-(zone, metric, hour) distributions
  model-spec.json  scoring parameters, so anomaly scoring matches the server exactly
  MANIFEST.json    tier + worker count chosen for THIS machine

Work is spread across the worker count the server picked for your hardware. Exit code
doubles as an alert signal: 0 = clear, 1 = warning, 2 = critical.
"""
import json
import math
import os
import sys
import datetime

# --- constants, mirrored from dynamics.go so predictions match the server exactly -------
SUPPLY_AIR_C = 12.0
OUTDOOR_CO2_PPM = 400.0
DYNAMICS_MATURE = 60
CAPACITY_MARGIN_C = 0.8
# The physical band the engine's own integration clamps to, and the point beyond which a
# predicted settling point is an extrapolation artefact rather than a forecast. A linear
# fit cannot tell it is being extrapolated: hold a busy room at 15% airflow forever and the
# arithmetic will promise 50 C. The crossing TIME stays trustworthy (it depends on the
# early part of the curve, inside the identified regime); the asymptote does not.
PHYS_MIN_C = 5.0
PHYS_MAX_C = 50.0
CREDIBLE_MAX_C = 45.0
CO2_CREDIBLE_MAX = 5000.0
CO2_PHYS_MAX = 40000.0
PREDICT_MIN_ETA_SEC = 120.0
HORIZONS_MIN = (15, 30, 60, 120)


def _clamp(v, lo, hi):
    return max(lo, min(hi, v))


def _load(path, default=None):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except (IOError, OSError, ValueError):
        if default is None:
            raise
        return default


# --- interpreting an identified room ----------------------------------------------------

def thermal_usable(room):
    """Mirrors zoneDynamics.thermalUsable: mature AND physically sane. A fit that says the
    outside cools the room, or that cooling heats it, has not identified anything."""
    th = (room or {}).get("thermal") or {}
    theta = th.get("theta") or []
    if len(theta) != 4 or th.get("n", 0) < DYNAMICS_MATURE:
        return False
    env, cool = theta[0], theta[1]
    return 0.01 < env < 20 and cool < 0


def co2_usable(room):
    ph = (room or {}).get("co2") or {}
    phi = ph.get("theta") or []
    if len(phi) != 3 or ph.get("n", 0) < DYNAMICS_MATURE:
        return False
    return 0.05 < phi[1] < 30


def thermal_outlook(room, temp, outdoor_c, flow, people, limit, horizon_sec):
    """Closed-form solution of this room's identified thermal balance.

    With drivers held constant, dT/dt = k·(T_eq − T) where k = θ0 − θ1·flow, so
    T(t) = T_eq + (T0 − T_eq)·e^(−k·t) exactly. Mirrors thermalOutlookLocked."""
    if not thermal_usable(room):
        return None
    env, cool, occ, base = room["thermal"]["theta"]
    k = env - cool * flow
    if k <= 1e-6:
        return None
    eq = (env * outdoor_c - cool * flow * SUPPLY_AIR_C + occ * people + base) / k
    h = horizon_sec / 3600.0
    out = {
        "equilibrium": _clamp(eq, PHYS_MIN_C, PHYS_MAX_C),
        "predicted": _clamp(eq + (temp - eq) * math.exp(-k * h), PHYS_MIN_C, PHYS_MAX_C),
        "tauMin": 60.0 / k,
        "etaSec": -1.0,
        "samples": room["thermal"]["n"],
        "extrapolated": eq > CREDIBLE_MAX_C or eq < PHYS_MIN_C,
    }
    if eq > limit and temp < limit:
        ratio = (limit - eq) / (temp - eq)
        if 0 < ratio < 1:
            out["etaSec"] = -math.log(ratio) / k * 3600.0
    return out


def co2_outlook(room, co2, people, limit, horizon_sec):
    """Closed-form solution of this room's identified mass balance:
    C_eq = C_out + (φ0·people + φ2)/φ1, approached at rate φ1 (its air-change rate)."""
    if not co2_usable(room):
        return None
    gen, ach, base = room["co2"]["theta"]
    eq = OUTDOOR_CO2_PPM + (gen * people + base) / ach
    h = horizon_sec / 3600.0
    out = {
        "equilibrium": _clamp(eq, OUTDOOR_CO2_PPM, CO2_PHYS_MAX),
        "predicted": _clamp(eq + (co2 - eq) * math.exp(-ach * h), OUTDOOR_CO2_PPM, CO2_PHYS_MAX),
        "achPerHour": ach,
        "etaSec": -1.0,
        "samples": room["co2"]["n"],
        "extrapolated": eq > CO2_CREDIBLE_MAX,
    }
    if eq > limit and co2 < limit:
        ratio = (limit - eq) / (co2 - eq)
        if 0 < ratio < 1:
            out["etaSec"] = -math.log(ratio) / ach * 3600.0
    return out


def _eta_label(sec):
    return "%.1f h" % (sec / 3600.0) if sec >= 5400 else "%.0f min" % (sec / 60.0)


# --- the per-room analysis (module level so multiprocessing can pickle it) ---------------

def analyze_room(job):
    """Full sweep for one room: a horizon curve for both balances, the capability check at
    full flow, and the findings that fall out of them. This is the unit of work that gets
    spread across cores."""
    room, state, co2_limit = job
    zone = state.get("zone", "?")
    label = state.get("label") or zone
    temp = float(state.get("temp", 0.0))
    setpoint = float(state.get("setpoint", 0.0) or 0.0)
    outdoor = float(state.get("outdoorC", 30.0))
    flow = float(state.get("flowRatio", 0.0))
    people = int(state.get("occupancy", 0) or 0)
    co2 = float(state.get("co2", 0.0) or 0.0)
    co2_live = bool(state.get("co2Live", co2 > 0))

    limit_c = setpoint + 1.0 if setpoint > 0 else 26.0

    result = {
        "zone": zone,
        "label": label,
        "identified": {
            "thermal": thermal_usable(room),
            "co2": co2_usable(room),
        },
        "findings": [],
        "risk": 0.0,
    }
    if room is None:
        result["identified"]["note"] = "this room has no identified model in the bundle"
        return result

    # --- thermal ------------------------------------------------------------------
    if thermal_usable(room):
        env, cool, occ_gain, _ = room["thermal"]["theta"]
        result["identified"].update({
            "tauMin": 60.0 / env,
            "coolingAuthority": -cool,
            "perOccupantC": occ_gain,
            "thermalSamples": room["thermal"]["n"],
        })

        # Horizon sweep — the extra processing a capable machine is being asked for.
        curve = {}
        for m in HORIZONS_MIN:
            look = thermal_outlook(room, temp, outdoor, flow, people, limit_c, m * 60.0)
            if look:
                curve[str(m)] = round(look["predicted"], 2)
        base_look = thermal_outlook(room, temp, outdoor, flow, people, limit_c, 1800.0)
        if base_look:
            result["thermal"] = {
                "equilibrium": round(base_look["equilibrium"], 2),
                "tauMin": round(base_look["tauMin"], 1),
                "etaSec": round(base_look["etaSec"], 1),
                "horizonC": curve,
            }

        # Capability: can this room be held AT ALL, with its VAV wide open?
        full = thermal_outlook(room, temp, outdoor, 1.0, people, limit_c, 1800.0)
        if full and setpoint > 0:
            can_hold = full["equilibrium"] <= setpoint + CAPACITY_MARGIN_C
            result["capability"] = {
                "fullFlowEquilibriumC": round(full["equilibrium"], 2),
                "canHoldSetpoint": can_hold,
            }
            if not can_hold:
                excess = full["equilibrium"] - setpoint
                result["risk"] = max(result["risk"], 5.0 + excess)
                result["findings"].append({
                    "kind": "capability", "metric": "coolingAuthority",
                    "severity": "critical" if excess >= 2.0 else "warning",
                    "action": "cool",
                    "message": ("%s settles at %.1f°C even with its VAV wide open — %.1f°C "
                                "above its %.1f°C setpoint. Identified from %d samples "
                                "(time constant %.0f min). Capacity or delivery fault, not "
                                "a control problem." % (label, full["equilibrium"], excess,
                                                        setpoint, room["thermal"]["n"],
                                                        full["tauMin"])),
                })
        if base_look and base_look["etaSec"] > PREDICT_MIN_ETA_SEC:
            eta = base_look["etaSec"]
            result["risk"] = max(result["risk"], 4.0 + 4.0 * (1.0 - min(eta, 1800.0) / 1800.0))
            result["findings"].append({
                "kind": "prediction", "metric": "temp",
                "severity": "critical" if eta < 600 else "warning",
                "action": "cool",
                "message": ("%s is %.1f°C and %s — crosses %.1f°C in about "
                            "%s at its current %.0f%% airflow and %d occupants "
                            "(time constant %.0f min)." % (
                                label, temp,
                                ("climbing well past %.0f°C (extrapolated beyond this "
                                 "room's identified range — act on the crossing time)"
                                 % CREDIBLE_MAX_C) if base_look["extrapolated"]
                                else "heading for %.1f°C" % base_look["equilibrium"],
                                limit_c, _eta_label(eta), flow * 100, people,
                                base_look["tauMin"])),
            })

    # --- ventilation --------------------------------------------------------------
    if co2_usable(room):
        gen, ach, _ = room["co2"]["theta"]
        result["identified"].update({
            "achPerHour": ach,
            "perOccupantPpm": gen,
            "co2Samples": room["co2"]["n"],
        })
        if co2_live and co2 > 0:
            curve = {}
            for m in HORIZONS_MIN:
                look = co2_outlook(room, co2, people, co2_limit, m * 60.0)
                if look:
                    curve[str(m)] = round(look["predicted"], 0)
            look = co2_outlook(room, co2, people, co2_limit, 1800.0)
            if look:
                result["co2"] = {
                    "equilibrium": round(look["equilibrium"], 0),
                    "achPerHour": round(ach, 2),
                    "etaSec": round(look["etaSec"], 1),
                    "horizonPpm": curve,
                }
                if look["equilibrium"] > co2_limit and look["etaSec"] > PREDICT_MIN_ETA_SEC:
                    eta = look["etaSec"]
                    result["risk"] = max(result["risk"],
                                         4.0 + 4.0 * (1.0 - min(eta, 1800.0) / 1800.0))
                    result["findings"].append({
                        "kind": "prediction", "metric": "co2",
                        "severity": "critical" if eta < 600 else "warning",
                        "action": "purge",
                        "message": ("%s reads %.0f ppm and is %s at %d "
                                    "occupants — past %.0f ppm in about %s. Measured "
                                    "air-change rate is %.1f ACH; that is not enough for "
                                    "this many people." % (
                                        label, co2,
                                        ("climbing well past %.0f ppm (extrapolated)"
                                         % CO2_CREDIBLE_MAX) if look["extrapolated"]
                                        else "heading for %.0f ppm" % look["equilibrium"],
                                        people, co2_limit, _eta_label(eta), ach)),
                    })
        # A room whose ventilation cannot cope even at its typical load is a standing
        # finding, independent of what it happens to read right now.
        elif people > 0:
            eq = OUTDOOR_CO2_PPM + (gen * people + room["co2"]["theta"][2]) / ach
            if eq > co2_limit:
                result["risk"] = max(result["risk"], 3.5)
                result["findings"].append({
                    "kind": "capability", "metric": "co2", "severity": "warning",
                    "action": "purge",
                    "message": ("%s would settle at %.0f ppm at %d occupants on its measured "
                                "%.1f ACH — above the %.0f ppm guideline. Under-ventilated "
                                "for the way this room is used." % (
                                    label, eq, people, ach, co2_limit)),
                })

    return result


# --- drivers ----------------------------------------------------------------------------

def _pool_map(fn, jobs, workers):
    """Spread the per-room work across cores when there is enough of it to be worth the
    process overhead; otherwise stay inline. Falls back to serial if the platform will not
    give us a pool."""
    if workers > 1 and len(jobs) >= 8:
        try:
            import multiprocessing
            with multiprocessing.Pool(processes=workers) as pool:
                return pool.map(fn, jobs)
        except (ImportError, OSError, ValueError):
            pass
    return [fn(j) for j in jobs]


def _co2_limit(spec_doc):
    try:
        return float(spec_doc["metrics"]["co2"]["standardHi"]) or 1000.0
    except (KeyError, TypeError, ValueError):
        return 1000.0


def cmd_analyze(dynamics, spec_doc, states, workers):
    limit = _co2_limit(spec_doc)
    jobs = [(dynamics.get(s.get("zone", "")), s, limit) for s in states]
    results = _pool_map(analyze_room, jobs, workers)
    results.sort(key=lambda r: -r["risk"])
    return results


def cmd_replay(dynamics, spec_doc, history, workers):
    """Batch-replay a whole history instead of a single snapshot: every room at every
    timestep, aggregated into which rooms spend the most time in a predicted-breach state.
    This is the heaviest thing the bundle does, and the reason the tier check cares about
    core count."""
    limit = _co2_limit(spec_doc)
    samples = history.get("samples") if isinstance(history, dict) else history
    if not samples:
        return {"samples": 0, "rooms": []}

    jobs = []
    for snap in samples:
        rooms = snap.get("rooms", snap) if isinstance(snap, dict) else snap
        for s in rooms:
            jobs.append((dynamics.get(s.get("zone", "")), s, limit))

    results = _pool_map(analyze_room, jobs, workers)

    agg = {}
    for r in results:
        a = agg.setdefault(r["zone"], {
            "zone": r["zone"], "label": r["label"], "steps": 0,
            "breachSteps": 0, "criticalSteps": 0, "peakRisk": 0.0,
            "cannotHoldSteps": 0,
        })
        a["steps"] += 1
        a["peakRisk"] = max(a["peakRisk"], r["risk"])
        if r["findings"]:
            a["breachSteps"] += 1
        for f in r["findings"]:
            if f["severity"] == "critical":
                a["criticalSteps"] += 1
                break
        cap = r.get("capability")
        if cap and not cap["canHoldSetpoint"]:
            a["cannotHoldSteps"] += 1

    rooms = sorted(agg.values(), key=lambda a: (-a["peakRisk"], -a["breachSteps"]))
    for a in rooms:
        a["breachFraction"] = round(a["breachSteps"] / float(a["steps"]), 3)
    return {"samples": len(samples), "roomSteps": len(results), "rooms": rooms}


def cmd_models(dynamics):
    out = []
    for zone, room in sorted(dynamics.items()):
        entry = {"zone": zone,
                 "thermalIdentified": thermal_usable(room),
                 "co2Identified": co2_usable(room)}
        th = (room or {}).get("thermal") or {}
        if thermal_usable(room):
            env, cool, occ, _ = th["theta"]
            entry.update({"timeConstantMin": round(60.0 / env, 1),
                          "coolingAuthority": round(-cool, 3),
                          "perOccupantC": round(occ, 4),
                          "samples": th.get("n", 0)})
        else:
            entry["samples"] = th.get("n", 0)
            entry["note"] = "still being identified (needs %d samples and a sane fit)" % DYNAMICS_MATURE
        if co2_usable(room):
            gen, ach, _ = room["co2"]["theta"]
            entry.update({"achPerHour": round(ach, 2), "perOccupantPpm": round(gen, 1)})
        out.append(entry)
    return out


def main(argv):
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 64
    cmd = argv[1]
    here = os.path.dirname(os.path.abspath(argv[0]))

    def rel(name):
        return os.path.join(here, name)

    dynamics = _load(rel("dynamics.json"), {}) or {}
    spec_doc = _load(rel("model-spec.json"), {}) or {}
    manifest = _load(rel("MANIFEST.json"), {}) or {}
    workers = int(manifest.get("workers", 0) or 0)
    if workers < 1:
        workers = max(1, (os.cpu_count() or 2) - 1)

    if not dynamics and cmd != "models":
        print("[!] dynamics.json is empty — the twin had not identified any room models "
              "when this bundle was exported. Let it run longer and re-export.",
              file=sys.stderr)

    if cmd == "models":
        report = cmd_models(dynamics)
        print(json.dumps(report, ensure_ascii=False, indent=2))
        ready = sum(1 for r in report if r["thermalIdentified"] or r["co2Identified"])
        print("[i] %d of %d rooms have an identified model" % (ready, len(report)),
              file=sys.stderr)
        return 0

    if len(argv) < 3:
        print("usage: python3 econ_local.py %s <input.json>" % cmd, file=sys.stderr)
        return 64
    payload = _load(argv[2])

    if cmd == "replay":
        report = cmd_replay(dynamics, spec_doc, payload, workers)
        print(json.dumps(report, ensure_ascii=False, indent=2))
        print("[i] replayed %d snapshots (%d room-steps) across %d worker(s)" % (
            report.get("samples", 0), report.get("roomSteps", 0), workers), file=sys.stderr)
        for a in report.get("rooms", [])[:10]:
            if a["breachSteps"]:
                print("[%s] %s predicted-breach in %d/%d steps (peak risk %.1f)" % (
                    "CRIT" if a["criticalSteps"] else "WARN", a["label"],
                    a["breachSteps"], a["steps"], a["peakRisk"]), file=sys.stderr)
        return 2 if any(a["criticalSteps"] for a in report.get("rooms", [])) else (
            1 if any(a["breachSteps"] for a in report.get("rooms", [])) else 0)

    if cmd == "analyze":
        results = cmd_analyze(dynamics, spec_doc, payload, workers)
        print(json.dumps(results, ensure_ascii=False, indent=2))
        worst = 0
        printed = 0
        for r in results:
            for f in r["findings"]:
                worst = max(worst, 2 if f["severity"] == "critical" else 1)
                print("[%s] %s" % (f["severity"].upper(), f["message"]), file=sys.stderr)
                printed += 1
        ident = sum(1 for r in results
                    if r["identified"]["thermal"] or r["identified"]["co2"])
        print("[i] analysed %d rooms (%d identified) across %d worker(s) at %s" % (
            len(results), ident, workers,
            datetime.datetime.now().strftime("%H:%M")), file=sys.stderr)
        if not printed:
            print("[OK] no room is predicted to breach within the horizon", file=sys.stderr)
        return worst

    print("unknown command %r (expected analyze, replay or models)" % cmd, file=sys.stderr)
    return 64


if __name__ == "__main__":
    sys.exit(main(sys.argv))
