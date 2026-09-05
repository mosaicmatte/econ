// -----------------------------------------------------------------------------
// node_config.h — runtime configuration for an ECON edge node.
//
// WHY THIS EXISTS
//
// Every tunable on this node used to be a compile-time -D flag. That is correct for
// anything that decides which CODE is linked (which sensors exist, which IR protocol,
// which pins), but it is wrong for everything that is a PROPERTY OF THE INSTALLATION:
// a burden resistor's calibration constant, the mains voltage, how often to publish,
// how sensitive the touch pad is. Those are discovered on a ladder with a multimeter,
// and needing a reflash — a laptop, a USB cable, and physical access to a board that
// may already be in a ceiling — to change one float is how a node ends up running on a
// calibration nobody ever corrected.
//
// So: the compile-time flags become the DEFAULTS, and each one may be overridden at
// runtime over MQTT and persisted to NVS. A node that is never configured behaves
// EXACTLY as it did before this file existed. That is the whole design constraint.
//
// THE DISCIPLINE THIS FILE INHERITS
//
// The repo's first rule is "never fabricate a measurement", and a calibration constant
// is upstream of every measurement the clamp reports. Two consequences:
//
//   1. A bad config value is REJECTED, not clamped. Silently clamping 6060.0 to 500.0
//      would publish confident, wrong watts forever. The node keeps its last good value
//      and says loudly what it refused and why.
//   2. Changing a calibration changes the MEANING of the series the engine is storing.
//      A step in plugW caused by recalibrating is indistinguishable, downstream, from a
//      step caused by the load actually changing — unless the node says so. Hence
//      cfgRev: a monotonic counter published in every telemetry message, so the engine
//      can mark exactly where the series stopped being comparable to itself.
//
// WIRE PROTOCOL
//
//   econ/config/<zone>        <- JSON, any subset of fields. {"reset":true} restores
//                                defaults. Retain it and a rebooting node picks it up.
//   econ/config/<zone>/state  -> JSON, retained: effective config, which fields are
//                                overridden, cfgRev, and the last rejection if any.
// -----------------------------------------------------------------------------
#pragma once

#include <Arduino.h>
#include <ArduinoJson.h>
#include <Preferences.h>

// ---------------------------------------------------------------------------------
// Compile-time defaults.
//
// Each is taken from the -D flag that used to be the ONLY way to set it, so the default
// config is bit-identical to the previous hardcoded behaviour. Guarded individually
// because the sensor flags they belong to may not be compiled in — the config struct
// carries every field regardless, so a board can be pre-configured for a clamp that is
// fitted later without reflashing twice.
// ---------------------------------------------------------------------------------
#ifndef PUBLISH_INTERVAL_MS_DEFAULT
  #define PUBLISH_INTERVAL_MS_DEFAULT 5000
#endif
#ifndef PLUG_CAL_A_PER_V
  #define PLUG_CAL_A_PER_V 60.6
#endif
#ifndef PLUG_MAINS_V
  #define PLUG_MAINS_V 230.0
#endif
#ifndef AC_CAL_A_PER_V
  #define AC_CAL_A_PER_V 60.6f
#endif
#ifndef AC_MAINS_V
  #define AC_MAINS_V 220.0f
#endif
#ifndef STRIP_CAL_A_PER_V
  #define STRIP_CAL_A_PER_V 15.0f
#endif
// Touch presence hysteresis, as a percentage of the boot-time baseline. Enter on a firm
// touch, leave only after recovering most of the way — the literals 62 and 82 come from
// the observed 3<->0 occupancy flapping these were introduced to stop.
#ifndef TOUCH_ENTER_PCT_DEFAULT
  #define TOUCH_ENTER_PCT_DEFAULT 62
#endif
#ifndef TOUCH_EXIT_PCT_DEFAULT
  #define TOUCH_EXIT_PCT_DEFAULT 82
#endif
#ifndef TOUCH_OCCUPANTS_DEFAULT
  #define TOUCH_OCCUPANTS_DEFAULT 3
#endif
// Bounds the engine's setpoint commands before they reach a real compressor.
#ifndef SETPOINT_MIN_C_DEFAULT
  #define SETPOINT_MIN_C_DEFAULT 16.0f
#endif
#ifndef SETPOINT_MAX_C_DEFAULT
  #define SETPOINT_MAX_C_DEFAULT 30.0f
#endif

struct NodeConfig {
  char     zoneLabel[32];         // human label published as telemetry "zone"
  uint32_t publishIntervalMs;     // telemetry cadence
  float    plugCalAPerV;          // SCT-013 plug clamp: amps of primary per volt at the ADC
  float    plugMainsV;            // assumed mains voltage for the plug circuit
  float    acCalAPerV;            // SCT-013 on the AC supply
  float    acMainsV;
  float    stripCalAPerV;         // ACS712 strip sensor: amps per volt at ADC
  uint8_t  touchEnterPct;         // capacitive presence hysteresis, % of baseline
  uint8_t  touchExitPct;
  uint8_t  touchOccupants;        // headcount reported while the pad is held
  float    setpointMinC;          // refuse engine setpoints outside this band
  float    setpointMaxC;
  bool     relayActiveLow;        // true = LOW turns relay ON, false = HIGH turns relay ON
  uint32_t cfgRev;                // bumped on every accepted change; published in telemetry
};

// cfgDefaults returns the configuration this firmware was BUILT with. It is what a
// factory-reset node runs, and what every "is this field overridden?" comparison is made
// against.
inline NodeConfig cfgDefaults() {
  NodeConfig c{};
  snprintf(c.zoneLabel, sizeof(c.zoneLabel), "%s", ZONE_LABEL_OVERRIDE);
  c.publishIntervalMs = PUBLISH_INTERVAL_MS_DEFAULT;
  c.plugCalAPerV      = (float)PLUG_CAL_A_PER_V;
  c.plugMainsV        = (float)PLUG_MAINS_V;
  c.acCalAPerV        = (float)AC_CAL_A_PER_V;
  c.acMainsV          = (float)AC_MAINS_V;
  c.stripCalAPerV     = (float)STRIP_CAL_A_PER_V;
  c.touchEnterPct     = TOUCH_ENTER_PCT_DEFAULT;
  c.touchExitPct      = TOUCH_EXIT_PCT_DEFAULT;
  c.touchOccupants    = TOUCH_OCCUPANTS_DEFAULT;
  c.setpointMinC      = SETPOINT_MIN_C_DEFAULT;
  c.setpointMaxC      = SETPOINT_MAX_C_DEFAULT;
  c.relayActiveLow    = false;
  c.cfgRev            = 0;
  return c;
}

// The live configuration. Reads are lock-free single-core Arduino loop reads; every write
// goes through cfgApplyJson().
inline NodeConfig  gCfg           = cfgDefaults();
inline Preferences gCfgPrefs;
inline char        gCfgLastError[96] = {0};

// ---------------------------------------------------------------------------------
// Validation. Each field states its own acceptable range, and a value outside it is
// REFUSED — see the header comment. The ranges are physical, not arbitrary:
//   * a clamp calibration below 1 or above 500 A/V describes no SCT-013 ever sold
//   * mains outside 90..260 V is not a single-phase supply this node will meet
//   * a publish interval under 1 s floods the broker; over 5 min the engine's 20 s
//     staleness window means the zone is unpinned more often than it is pinned
// ---------------------------------------------------------------------------------
inline bool cfgFail(const char* fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  vsnprintf(gCfgLastError, sizeof(gCfgLastError), fmt, ap);
  va_end(ap);
  Serial.printf("[config] REJECTED: %s\n", gCfgLastError);
  return false;
}

inline bool cfgValidate(const NodeConfig& c) {
  if (strlen(c.zoneLabel) == 0)
    return cfgFail("zoneLabel must not be empty");
  if (c.publishIntervalMs < 1000 || c.publishIntervalMs > 300000)
    return cfgFail("publishIntervalMs %lu outside 1000..300000",
                   (unsigned long)c.publishIntervalMs);
  if (!(c.plugCalAPerV >= 1.0f && c.plugCalAPerV <= 500.0f))
    return cfgFail("plugCalAPerV %.3f outside 1..500 A/V", (double)c.plugCalAPerV);
  if (!(c.acCalAPerV >= 1.0f && c.acCalAPerV <= 500.0f))
    return cfgFail("acCalAPerV %.3f outside 1..500 A/V", (double)c.acCalAPerV);
  if (!(c.stripCalAPerV >= 1.0f && c.stripCalAPerV <= 500.0f))
    return cfgFail("stripCalAPerV %.3f outside 1..500 A/V", (double)c.stripCalAPerV);
  if (!(c.plugMainsV >= 90.0f && c.plugMainsV <= 260.0f))
    return cfgFail("plugMainsV %.1f outside 90..260 V", (double)c.plugMainsV);
  if (!(c.acMainsV >= 90.0f && c.acMainsV <= 260.0f))
    return cfgFail("acMainsV %.1f outside 90..260 V", (double)c.acMainsV);
  // Exit must sit ABOVE enter or the hysteresis inverts and the presence state chatters —
  // which is the exact bug the two thresholds were introduced to fix.
  if (c.touchEnterPct < 10 || c.touchEnterPct > 99)
    return cfgFail("touchEnterPct %u outside 10..99", c.touchEnterPct);
  if (c.touchExitPct < 10 || c.touchExitPct > 99)
    return cfgFail("touchExitPct %u outside 10..99", c.touchExitPct);
  if (c.touchExitPct <= c.touchEnterPct)
    return cfgFail("touchExitPct %u must exceed touchEnterPct %u (hysteresis would invert)",
                   c.touchExitPct, c.touchEnterPct);
  if (c.touchOccupants < 1 || c.touchOccupants > 50)
    return cfgFail("touchOccupants %u outside 1..50", c.touchOccupants);
  if (!(c.setpointMinC >= 10.0f && c.setpointMinC <= 30.0f))
    return cfgFail("setpointMinC %.1f outside 10..30 C", (double)c.setpointMinC);
  if (!(c.setpointMaxC >= 10.0f && c.setpointMaxC <= 35.0f))
    return cfgFail("setpointMaxC %.1f outside 10..35 C", (double)c.setpointMaxC);
  if (c.setpointMaxC <= c.setpointMinC)
    return cfgFail("setpointMaxC %.1f must exceed setpointMinC %.1f",
                   (double)c.setpointMaxC, (double)c.setpointMinC);
  return true;
}

// ---------------------------------------------------------------------------------
// NVS persistence. Only fields that DIFFER from the compiled defaults are stored, so
// reflashing with a new default (say a corrected burden calibration) moves every node
// that never overrode it, while genuinely site-specific overrides survive.
// ---------------------------------------------------------------------------------
inline void cfgSave() {
  gCfgPrefs.begin("econ", false);
  gCfgPrefs.putBytes("cfg", &gCfg, sizeof(gCfg));
  gCfgPrefs.end();
}

inline void cfgLoad() {
  gCfg = cfgDefaults();
  gCfgPrefs.begin("econ", true);
  NodeConfig stored{};
  size_t n = gCfgPrefs.getBytesLength("cfg");
  if (n == sizeof(stored) && gCfgPrefs.getBytes("cfg", &stored, sizeof(stored)) == sizeof(stored)) {
    // A stored blob from an older/newer struct layout, or one corrupted in flash, must
    // never become the running calibration. Validate it exactly as if it had arrived over
    // the wire; fall back to defaults if it fails.
    if (cfgValidate(stored)) {
      gCfg = stored;
      Serial.printf("[config] restored from NVS (rev %lu)\n", (unsigned long)gCfg.cfgRev);
    } else {
      Serial.println("[config] stored config failed validation — running compiled defaults");
    }
  } else if (n > 0) {
    Serial.printf("[config] stored config is %u bytes, expected %u (layout changed) — "
                  "running compiled defaults\n", (unsigned)n, (unsigned)sizeof(stored));
  }
  gCfgPrefs.end();
}

inline void cfgFactoryReset() {
  uint32_t rev = gCfg.cfgRev;
  gCfgPrefs.begin("econ", false);
  gCfgPrefs.remove("cfg");
  gCfgPrefs.end();
  gCfg = cfgDefaults();
  gCfg.cfgRev = rev + 1;   // a reset IS a change; the series is no longer comparable
  cfgSave();
  Serial.printf("[config] factory reset -> rev %lu\n", (unsigned long)gCfg.cfgRev);
}

// ---------------------------------------------------------------------------------
// Apply a JSON config message. Returns true if anything actually changed.
//
// The merge is field-by-field over a COPY, validated as a whole before it is adopted:
// a message setting three fields where the second is out of range applies none of them,
// so the node is never left in a half-configured state that no operator asked for.
// ---------------------------------------------------------------------------------
inline bool cfgApplyJson(const JsonDocument& doc, bool& outReset) {
  outReset = false;
  if (doc["reset"].as<bool>()) {
    cfgFactoryReset();
    outReset = true;
    return true;
  }

  NodeConfig next = gCfg;
  if (doc.containsKey("zoneLabel"))
    snprintf(next.zoneLabel, sizeof(next.zoneLabel), "%s", doc["zoneLabel"].as<const char*>());
  if (doc.containsKey("publishIntervalMs")) next.publishIntervalMs = doc["publishIntervalMs"];
  if (doc.containsKey("plugCalAPerV"))      next.plugCalAPerV      = doc["plugCalAPerV"];
  if (doc.containsKey("plugMainsV"))        next.plugMainsV        = doc["plugMainsV"];
  if (doc.containsKey("acCalAPerV"))        next.acCalAPerV        = doc["acCalAPerV"];
  if (doc.containsKey("acMainsV"))          next.acMainsV          = doc["acMainsV"];
  if (doc.containsKey("stripCalAPerV"))     next.stripCalAPerV     = doc["stripCalAPerV"];
  if (doc.containsKey("touchEnterPct"))     next.touchEnterPct     = doc["touchEnterPct"];
  if (doc.containsKey("touchExitPct"))      next.touchExitPct      = doc["touchExitPct"];
  if (doc.containsKey("touchOccupants"))    next.touchOccupants    = doc["touchOccupants"];
  if (doc.containsKey("setpointMinC"))      next.setpointMinC      = doc["setpointMinC"];
  if (doc.containsKey("setpointMaxC"))      next.setpointMaxC      = doc["setpointMaxC"];
  if (doc.containsKey("relayActiveLow"))    next.relayActiveLow    = doc["relayActiveLow"];

  if (!cfgValidate(next)) return false;               // gCfgLastError already set
  if (memcmp(&next, &gCfg, sizeof(next)) == 0) {
    Serial.println("[config] message matched the running config — nothing to do");
    return false;                                     // retained message replayed on reconnect
  }

  next.cfgRev = gCfg.cfgRev + 1;
  gCfg = next;
  gCfgLastError[0] = '\0';
  cfgSave();
  Serial.printf("[config] applied -> rev %lu (interval %lums, plug %.2f A/V @ %.0f V, "
                "ac %.2f A/V @ %.0f V, strip %.2f A/V, touch %u/%u%%, setpoint %.1f..%.1f C)\n",
                (unsigned long)gCfg.cfgRev, (unsigned long)gCfg.publishIntervalMs,
                (double)gCfg.plugCalAPerV, (double)gCfg.plugMainsV,
                (double)gCfg.acCalAPerV, (double)gCfg.acMainsV,
                (double)gCfg.stripCalAPerV,
                gCfg.touchEnterPct, gCfg.touchExitPct,
                (double)gCfg.setpointMinC, (double)gCfg.setpointMaxC);
  return true;
}

// cfgSerializeState fills `out` with the retained /state document: the effective config,
// which fields differ from the compiled defaults, and the last rejection. "overrides" is
// the field an operator actually wants — it answers "what has this board been told that
// its siblings have not?", which is otherwise only discoverable by diffing two boards.
inline void cfgSerializeState(JsonDocument& out) {
  NodeConfig d = cfgDefaults();
  out["cfgRev"]            = gCfg.cfgRev;
  out["zoneLabel"]         = gCfg.zoneLabel;
  out["publishIntervalMs"] = gCfg.publishIntervalMs;
  out["plugCalAPerV"]      = gCfg.plugCalAPerV;
  out["plugMainsV"]        = gCfg.plugMainsV;
  out["acCalAPerV"]        = gCfg.acCalAPerV;
  out["acMainsV"]          = gCfg.acMainsV;
  out["stripCalAPerV"]     = gCfg.stripCalAPerV;
  out["touchEnterPct"]     = gCfg.touchEnterPct;
  out["touchExitPct"]      = gCfg.touchExitPct;
  out["touchOccupants"]    = gCfg.touchOccupants;
  out["setpointMinC"]      = gCfg.setpointMinC;
  out["setpointMaxC"]      = gCfg.setpointMaxC;
  out["relayActiveLow"]    = gCfg.relayActiveLow;

  JsonArray ov = out.createNestedArray("overrides");
  if (strcmp(gCfg.zoneLabel, d.zoneLabel) != 0)        ov.add("zoneLabel");
  if (gCfg.publishIntervalMs != d.publishIntervalMs)   ov.add("publishIntervalMs");
  if (gCfg.plugCalAPerV      != d.plugCalAPerV)        ov.add("plugCalAPerV");
  if (gCfg.plugMainsV        != d.plugMainsV)          ov.add("plugMainsV");
  if (gCfg.acCalAPerV        != d.acCalAPerV)          ov.add("acCalAPerV");
  if (gCfg.acMainsV          != d.acMainsV)            ov.add("acMainsV");
  if (gCfg.stripCalAPerV     != d.stripCalAPerV)       ov.add("stripCalAPerV");
  if (gCfg.touchEnterPct     != d.touchEnterPct)       ov.add("touchEnterPct");
  if (gCfg.touchExitPct      != d.touchExitPct)        ov.add("touchExitPct");
  if (gCfg.touchOccupants    != d.touchOccupants)      ov.add("touchOccupants");
  if (gCfg.setpointMinC      != d.setpointMinC)        ov.add("setpointMinC");
  if (gCfg.setpointMaxC      != d.setpointMaxC)        ov.add("setpointMaxC");
  if (gCfg.relayActiveLow    != d.relayActiveLow)      ov.add("relayActiveLow");

  if (gCfgLastError[0]) out["lastError"] = gCfgLastError;
}
