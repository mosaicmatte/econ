// Host-side test for node_config.h.
//
// The firmware compiling proves nothing about whether the validation rules actually
// reject what they claim to — and those rules are the safety layer over a calibration
// constant that multiplies every watt the node reports. So the config logic is written to
// be free of hardware dependencies (Preferences and Serial are the only touch points, both
// shimmed below) and is exercised here on the build machine.
//
// Build and run:
//   c++ -std=c++17 -I <ArduinoJson>/src -I ../src -I . host_config_test.cpp -o /tmp/cfgtest && /tmp/cfgtest
// or just: ./test/run_host_tests.sh

#include "arduino_shim.h"
#include <ArduinoJson.h>

#define ZONE_LABEL_OVERRIDE "Level 4"
#include "node_config.h"

#include <cassert>
#include <cstdio>
#include <string>

static int failures = 0;

static void check(bool cond, const char* what) {
  if (cond) {
    printf("  ok   %s\n", what);
  } else {
    printf("  FAIL %s\n", what);
    failures++;
  }
}

// apply parses a JSON string exactly as handleConfig would and returns whether it changed
// the running config.
static bool apply(const char* json) {
  StaticJsonDocument<512> doc;
  if (deserializeJson(doc, json)) return false;
  bool reset = false;
  return cfgApplyJson(doc, reset);
}

int main() {
  printf("node_config: defaults\n");
  gCfg = cfgDefaults();
  NodeConfig d = cfgDefaults();
  // The whole compatibility promise: an unconfigured node runs the compiled flags.
  check(d.publishIntervalMs == 5000,           "publish interval defaults to the old 5000 ms");
  check(std::abs(d.plugCalAPerV - 60.6f) < 1e-3, "plug calibration defaults to the -000 + 33ohm figure");
  check(std::abs(d.plugMainsV - 230.0f) < 1e-3, "plug mains defaults to Vietnam 230 V");
  check(std::abs(d.stripCalAPerV - 15.0f) < 1e-3, "strip calibration defaults to 15.0 A/V");
  check(d.touchEnterPct == 62 && d.touchExitPct == 82, "touch hysteresis defaults to the observed 62/82");
  check(d.cfgRev == 0,                          "a node that was never configured is at rev 0");
  check(cfgValidate(d),                         "the compiled defaults are themselves valid");

  printf("node_config: accepts a real recalibration\n");
  // The actual next step in the project: a 47 ohm burden gives 42.6 A/V.
  check(apply("{\"plugCalAPerV\":42.6}"),       "47ohm burden calibration accepted");
  check(std::abs(gCfg.plugCalAPerV - 42.6f) < 1e-3, "value took effect");
  check(gCfg.cfgRev == 1,                       "cfgRev bumped to 1");
  check(gCfg.plugMainsV == d.plugMainsV,        "untouched fields kept their defaults");
  check(apply("{\"stripCalAPerV\":15.2}"),      "strip calibration accepted");
  check(std::abs(gCfg.stripCalAPerV - 15.2f) < 1e-3, "stripCalAPerV took effect");
  check(gCfg.cfgRev == 2,                       "cfgRev bumped to 2");

  printf("node_config: rejects what it cannot physically be\n");
  uint32_t revBefore = gCfg.cfgRev;
  check(!apply("{\"plugCalAPerV\":6060}"),      "6060 A/V (decimal slip) refused");
  check(std::abs(gCfg.plugCalAPerV - 42.6f) < 1e-3, "running calibration UNCHANGED after refusal");
  check(gCfg.cfgRev == revBefore,               "a refused message does not bump cfgRev");
  check(!apply("{\"plugMainsV\":12}"),          "12 V mains refused");
  check(!apply("{\"stripCalAPerV\":0.5}"),      "0.5 A/V strip calibration refused (< 1.0)");
  check(!apply("{\"stripCalAPerV\":501.0}"),    "501.0 A/V strip calibration refused (> 500.0)");
  check(apply("{\"stripCalAPerV\":1.0}"),       "boundary min 1.0 A/V strip calibration accepted");
  check(apply("{\"stripCalAPerV\":500.0}"),     "boundary max 500.0 A/V strip calibration accepted");
  check(apply("{\"stripCalAPerV\":15.5}"),      "set stripCalAPerV override for state test");
  check(!apply("{\"publishIntervalMs\":50}"),   "50 ms publish interval refused (would flood the broker)");
  check(!apply("{\"publishIntervalMs\":900000}"), "15 min interval refused (zone would sit unpinned)");
  check(!apply("{\"zoneLabel\":\"\"}"),         "empty zone label refused");

  printf("node_config: rejects an inverted hysteresis\n");
  // Exit must sit above enter; swapping them reintroduces the 3<->0 occupancy flapping the
  // two thresholds exist to prevent.
  check(!apply("{\"touchEnterPct\":85,\"touchExitPct\":60}"), "inverted touch hysteresis refused");
  check(gCfg.touchEnterPct == 62,               "touch thresholds unchanged after refusal");
  check(!apply("{\"setpointMinC\":28,\"setpointMaxC\":20}"),  "inverted setpoint band refused");

  printf("node_config: a partly-invalid message applies NOTHING\n");
  float calBefore = gCfg.plugCalAPerV;
  uint32_t iBefore = gCfg.publishIntervalMs;
  check(!apply("{\"publishIntervalMs\":10000,\"plugCalAPerV\":9999}"),
        "message with one bad field refused as a whole");
  check(gCfg.publishIntervalMs == iBefore,      "the VALID field in that message was not applied either");
  check(std::abs(gCfg.plugCalAPerV - calBefore) < 1e-3, "the invalid field was not applied");

  printf("node_config: retained-message replay is a no-op\n");
  check(apply("{\"publishIntervalMs\":10000}"), "first application changes config");
  uint32_t rev = gCfg.cfgRev;
  check(!apply("{\"publishIntervalMs\":10000}"), "identical replay reports no change");
  check(gCfg.cfgRev == rev,                     "replay does not bump cfgRev (broker redelivers on every reconnect)");

  printf("node_config: state document\n");
  StaticJsonDocument<640> st;
  cfgSerializeState(st);
  std::string overrides;
  for (JsonVariant v : st["overrides"].as<JsonArray>()) overrides += std::string(v.as<const char*>()) + " ";
  check(st["cfgRev"].as<uint32_t>() == gCfg.cfgRev, "state reports the current revision");
  check(overrides.find("plugCalAPerV") != std::string::npos,
        "state lists plugCalAPerV as overridden");
  check(overrides.find("publishIntervalMs") != std::string::npos,
        "state lists publishIntervalMs as overridden");
  check(overrides.find("stripCalAPerV") != std::string::npos,
        "state lists stripCalAPerV as overridden");
  check(overrides.find("acMainsV") == std::string::npos,
        "state does NOT list a field still at its default");

  printf("node_config: factory reset\n");
  uint32_t preReset = gCfg.cfgRev;
  cfgFactoryReset();
  check(std::abs(gCfg.plugCalAPerV - d.plugCalAPerV) < 1e-3, "reset restores the compiled calibration");
  check(std::abs(gCfg.stripCalAPerV - d.stripCalAPerV) < 1e-3, "reset restores the compiled strip calibration");
  check(gCfg.publishIntervalMs == d.publishIntervalMs, "reset restores the compiled interval");
  check(gCfg.cfgRev == preReset + 1,
        "reset BUMPS cfgRev — it is a change, and the series is not comparable across it");

  printf("\n%s (%d failure%s)\n", failures ? "FAILED" : "PASSED", failures, failures == 1 ? "" : "s");
  return failures ? 1 : 0;
}
