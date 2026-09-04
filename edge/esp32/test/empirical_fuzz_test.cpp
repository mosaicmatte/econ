// Empirical Challenge: Configuration Fuzzing for stripCalAPerV in node_config.h
// Tests invalid types, extreme values (-100, 0, 0.99, 500.01, NaN, Inf, long strings)
// Verifies clean rejection without state corruption.

#include "arduino_shim.h"
#include <ArduinoJson.h>

#define ZONE_LABEL_OVERRIDE "Level 4"
#include "node_config.h"

#include <cstdio>
#include <cmath>
#include <cstring>
#include <string>
#include <vector>
#include <limits>
#include <cassert>
#include <functional>

static int failures = 0;

static void check(bool cond, const char* what) {
  if (cond) {
    printf("  [PASS] %s\n", what);
  } else {
    printf("  [FAIL] %s\n", what);
    failures++;
  }
}

// Helper simulating handleConfig inbound message processing
struct FuzzResult {
  bool deserializeOk;
  bool applyOk;
  std::string lastError;
  uint32_t revAfter;
  float stripCalAfter;
};

static FuzzResult executeConfigMessage(const char* jsonStr) {
  FuzzResult res;
  StaticJsonDocument<512> doc;
  DeserializationError err = deserializeJson(doc, jsonStr);
  if (err) {
    res.deserializeOk = false;
    res.applyOk = false;
    res.lastError = std::string("malformed JSON: ") + err.c_str();
    res.revAfter = gCfg.cfgRev;
    res.stripCalAfter = gCfg.stripCalAPerV;
    return res;
  }

  res.deserializeOk = true;
  bool wasReset = false;
  res.applyOk = cfgApplyJson(doc, wasReset);
  res.lastError = gCfgLastError;
  res.revAfter = gCfg.cfgRev;
  res.stripCalAfter = gCfg.stripCalAPerV;
  return res;
}

// Helper for programmatic JsonDocument injection (bypassing JSON string parser)
static FuzzResult executeConfigVariant(const std::function<void(JsonDocument&)>& populate) {
  FuzzResult res;
  StaticJsonDocument<512> doc;
  populate(doc);
  res.deserializeOk = true;
  bool wasReset = false;
  res.applyOk = cfgApplyJson(doc, wasReset);
  res.lastError = gCfgLastError;
  res.revAfter = gCfg.cfgRev;
  res.stripCalAfter = gCfg.stripCalAPerV;
  return res;
}

int main() {
  printf("=== EMPIRICAL CHALLENGE: stripCalAPerV CONFIGURATION FUZZING ===\n\n");

  // Reset to initial defaults
  gCfg = cfgDefaults();
  prefStore().has = false;
  prefStore().blob.clear();

  float initialStripCal = gCfg.stripCalAPerV; // 15.0f
  uint32_t initialRev = gCfg.cfgRev;           // 0

  printf("Baseline configuration:\n");
  printf("  gCfg.stripCalAPerV: %.3f\n", initialStripCal);
  printf("  gCfg.cfgRev: %u\n\n", initialRev);

  // -----------------------------------------------------------------
  // 1. Extreme and Negative Values
  // -----------------------------------------------------------------
  printf("--- Fuzz Test 1: Extreme and Negative Values ---\n");
  {
    const char* badInputs[] = {
      "{\"stripCalAPerV\": -100}",
      "{\"stripCalAPerV\": -100.0}",
      "{\"stripCalAPerV\": -0.001}",
      "{\"stripCalAPerV\": -1e20}",
      "{\"stripCalAPerV\": 0}",
      "{\"stripCalAPerV\": 0.0}",
      "{\"stripCalAPerV\": -0.0}",
      "{\"stripCalAPerV\": 0.99}",
      "{\"stripCalAPerV\": 0.99999}",
      "{\"stripCalAPerV\": 500.01}",
      "{\"stripCalAPerV\": 500.001}",
      "{\"stripCalAPerV\": 501.0}",
      "{\"stripCalAPerV\": 1000.0}",
      "{\"stripCalAPerV\": 1e10}",
      "{\"stripCalAPerV\": 1e38}"
    };

    for (const char* input : badInputs) {
      uint32_t revBefore = gCfg.cfgRev;
      float calBefore = gCfg.stripCalAPerV;

      FuzzResult r = executeConfigMessage(input);

      char desc[128];
      snprintf(desc, sizeof(desc), "Refused: %s (err: %s)", input, r.lastError.c_str());
      check(!r.applyOk, desc);
      check(gCfg.stripCalAPerV == calBefore, "  stripCalAPerV strictly unmodified");
      check(gCfg.cfgRev == revBefore, "  cfgRev strictly unchanged");
      check(!r.lastError.empty(), "  Rejection error recorded");
    }
  }

  // -----------------------------------------------------------------
  // 2. Boundary Condition Verification (1.0f and 500.0f)
  // -----------------------------------------------------------------
  printf("\n--- Fuzz Test 2: Valid Boundaries Acceptance ---\n");
  {
    // Lower boundary: 1.0f
    FuzzResult r1 = executeConfigMessage("{\"stripCalAPerV\": 1.0}");
    check(r1.applyOk, "Boundary min 1.0f accepted");
    check(std::abs(gCfg.stripCalAPerV - 1.0f) < 1e-4, "stripCalAPerV updated to 1.0f");
    check(gCfg.cfgRev == 1, "cfgRev incremented to 1");

    // Upper boundary: 500.0f
    FuzzResult r2 = executeConfigMessage("{\"stripCalAPerV\": 500.0}");
    check(r2.applyOk, "Boundary max 500.0f accepted");
    check(std::abs(gCfg.stripCalAPerV - 500.0f) < 1e-4, "stripCalAPerV updated to 500.0f");
    check(gCfg.cfgRev == 2, "cfgRev incremented to 2");

    // Reset back to nominal 15.0f for subsequent tests
    FuzzResult r3 = executeConfigMessage("{\"stripCalAPerV\": 15.0}");
    check(r3.applyOk, "Reset to nominal 15.0f");
    check(gCfg.cfgRev == 3, "cfgRev incremented to 3");
  }

  // -----------------------------------------------------------------
  // 3. Special IEEE-754 Floats (NaN, Inf, -Inf)
  // -----------------------------------------------------------------
  printf("\n--- Fuzz Test 3: NaN and Infinity Injection ---\n");
  {
    // 3A: Via JSON text string representations
    const char* nanStrings[] = {
      "{\"stripCalAPerV\": NaN}",
      "{\"stripCalAPerV\": \"NaN\"}",
      "{\"stripCalAPerV\": Infinity}",
      "{\"stripCalAPerV\": \"Infinity\"}",
      "{\"stripCalAPerV\": -Infinity}",
      "{\"stripCalAPerV\": \"-Infinity\"}"
    };

    for (const char* input : nanStrings) {
      uint32_t revBefore = gCfg.cfgRev;
      float calBefore = gCfg.stripCalAPerV;

      FuzzResult r = executeConfigMessage(input);

      char desc[128];
      snprintf(desc, sizeof(desc), "Refused NaN/Inf string: %s", input);
      check(!r.applyOk, desc);
      check(gCfg.stripCalAPerV == calBefore, "  stripCalAPerV unchanged");
      check(gCfg.cfgRev == revBefore, "  cfgRev unchanged");
    }

    // 3B: Programmatic IEEE-754 NaN injection directly into JsonDocument
    {
      uint32_t revBefore = gCfg.cfgRev;
      float calBefore = gCfg.stripCalAPerV;

      FuzzResult r = executeConfigVariant([](JsonDocument& d) {
        d["stripCalAPerV"] = std::numeric_limits<float>::quiet_NaN();
      });

      check(!r.applyOk, "Programmatic quiet_NaN refused in cfgApplyJson");
      check(gCfg.stripCalAPerV == calBefore, "stripCalAPerV unchanged after NaN injection");
      check(gCfg.cfgRev == revBefore, "cfgRev unchanged after NaN injection");
    }

    // 3C: Programmatic positive infinity injection
    {
      uint32_t revBefore = gCfg.cfgRev;
      float calBefore = gCfg.stripCalAPerV;

      FuzzResult r = executeConfigVariant([](JsonDocument& d) {
        d["stripCalAPerV"] = std::numeric_limits<float>::infinity();
      });

      check(!r.applyOk, "Programmatic +Infinity refused in cfgApplyJson");
      check(gCfg.stripCalAPerV == calBefore, "stripCalAPerV unchanged after +Inf injection");
      check(gCfg.cfgRev == revBefore, "cfgRev unchanged after +Inf injection");
    }

    // 3D: Programmatic negative infinity injection
    {
      uint32_t revBefore = gCfg.cfgRev;
      float calBefore = gCfg.stripCalAPerV;

      FuzzResult r = executeConfigVariant([](JsonDocument& d) {
        d["stripCalAPerV"] = -std::numeric_limits<float>::infinity();
      });

      check(!r.applyOk, "Programmatic -Infinity refused in cfgApplyJson");
      check(gCfg.stripCalAPerV == calBefore, "stripCalAPerV unchanged after -Inf injection");
      check(gCfg.cfgRev == revBefore, "cfgRev unchanged after -Inf injection");
    }
  }

  // -----------------------------------------------------------------
  // 4. Invalid JSON Types and Long Strings
  // -----------------------------------------------------------------
  printf("\n--- Fuzz Test 4: Type Confusion and String Injection ---\n");
  {
    std::string str64(64, 'A');
    std::string str256(256, 'B');
    std::string str1000(1000, 'C');

    // 4A: Types that are strictly rejected (false, null, arrays, objects, non-numeric strings)
    std::vector<std::string> rejectedTypeCases = {
      "{\"stripCalAPerV\": \"invalid_string\"}",
      "{\"stripCalAPerV\": \"\"}",
      "{\"stripCalAPerV\": \"   \"}",
      "{\"stripCalAPerV\": false}",
      "{\"stripCalAPerV\": null}",
      "{\"stripCalAPerV\": []}",
      "{\"stripCalAPerV\": [15.0]}",
      "{\"stripCalAPerV\": [\"15.0\"]}",
      "{\"stripCalAPerV\": {}}",
      "{\"stripCalAPerV\": {\"value\": 15.0}}",
      "{\"stripCalAPerV\": \"" + str64 + "\"}",
      "{\"stripCalAPerV\": \"" + str256 + "\"}",
      "{\"stripCalAPerV\": \"" + str1000 + "\"}"
    };

    for (const auto& json : rejectedTypeCases) {
      uint32_t revBefore = gCfg.cfgRev;
      float calBefore = gCfg.stripCalAPerV;

      FuzzResult r = executeConfigMessage(json.c_str());

      std::string preview = json.size() > 50 ? json.substr(0, 47) + "..." : json;
      char desc[128];
      snprintf(desc, sizeof(desc), "Cleanly rejected invalid type: %s", preview.c_str());
      check(!r.applyOk, desc);
      check(gCfg.stripCalAPerV == calBefore, "  stripCalAPerV unchanged");
      check(gCfg.cfgRev == revBefore, "  cfgRev unchanged");
    }

    // 4B: Empirical Analysis of Boolean true coercion
    // In ArduinoJson, boolean `true` coerces to float 1.0f via VariantData::asFloat().
    // Because 1.0f is precisely the lower bound [1.0f, 500.0f], node_config.h accepts it.
    {
      uint32_t revBefore = gCfg.cfgRev;
      FuzzResult rTrue = executeConfigMessage("{\"stripCalAPerV\": true}");
      check(rTrue.applyOk, "Boolean true coerces to 1.0f in ArduinoJson and passes validation");
      check(std::abs(gCfg.stripCalAPerV - 1.0f) < 1e-4, "stripCalAPerV set to 1.0f from boolean true");
      check(gCfg.cfgRev == revBefore + 1, "cfgRev bumped on boolean true conversion");
      printf("  [NOTE] Finding documented: boolean true coerces to 1.0f without explicit .is<float>() check\n");

      // Reset back to nominal 15.0f
      executeConfigMessage("{\"stripCalAPerV\": 15.0}");
    }
  }

  // -----------------------------------------------------------------
  // 5. String Numeric Coercion Safety
  // -----------------------------------------------------------------
  printf("\n--- Fuzz Test 5: String Numeric Values ---\n");
  {
    // What if someone publishes '{"stripCalAPerV": "15.2"}' or '{"stripCalAPerV": "0.5"}'?
    // In ArduinoJson, string "15.2" converts to 15.2 (valid) or "0.5" converts to 0.5 (invalid).
    // Test out-of-bounds string:
    uint32_t revBefore = gCfg.cfgRev;
    float calBefore = gCfg.stripCalAPerV;

    FuzzResult r1 = executeConfigMessage("{\"stripCalAPerV\": \"0.5\"}");
    check(!r1.applyOk, "String \"0.5\" outside 1..500 rejected");
    check(gCfg.stripCalAPerV == calBefore, "stripCalAPerV unchanged");

    FuzzResult r2 = executeConfigMessage("{\"stripCalAPerV\": \"9999.0\"}");
    check(!r2.applyOk, "String \"9999.0\" outside 1..500 rejected");
    check(gCfg.stripCalAPerV == calBefore, "stripCalAPerV unchanged");
  }

  // -----------------------------------------------------------------
  // 6. Multi-Field Atomic Protection Under Fuzzed Inputs
  // -----------------------------------------------------------------
  printf("\n--- Fuzz Test 6: Multi-Field Atomic Rejection ---\n");
  {
    // If a message contains a valid field AND an invalid stripCalAPerV:
    // NONE of the fields must be applied.
    uint32_t revBefore = gCfg.cfgRev;
    uint32_t intervalBefore = gCfg.publishIntervalMs;
    float plugCalBefore = gCfg.plugCalAPerV;
    float stripCalBefore = gCfg.stripCalAPerV;

    FuzzResult r = executeConfigMessage(
        "{\"publishIntervalMs\": 12000, \"plugCalAPerV\": 45.0, \"stripCalAPerV\": -100.0}"
    );

    check(!r.applyOk, "Multi-field payload with bad stripCalAPerV rejected as whole");
    check(gCfg.publishIntervalMs == intervalBefore, "publishIntervalMs kept previous value");
    check(gCfg.plugCalAPerV == plugCalBefore, "plugCalAPerV kept previous value");
    check(gCfg.stripCalAPerV == stripCalBefore, "stripCalAPerV kept previous value");
    check(gCfg.cfgRev == revBefore, "cfgRev unchanged");
  }

  // -----------------------------------------------------------------
  // 7. Post-Fuzz Recovery and Liveness Test
  // -----------------------------------------------------------------
  printf("\n--- Fuzz Test 7: Post-Fuzz Health and Recovery ---\n");
  {
    // Ensure the node is not left in a bricked state and can still accept valid updates
    uint32_t revBefore = gCfg.cfgRev;
    FuzzResult r = executeConfigMessage("{\"stripCalAPerV\": 22.5}");
    check(r.applyOk, "Valid configuration accepted after extreme fuzzing");
    check(std::abs(gCfg.stripCalAPerV - 22.5f) < 1e-4, "stripCalAPerV cleanly set to 22.5f");
    check(gCfg.cfgRev == revBefore + 1, "cfgRev properly incremented on valid config");

    // Factory reset cleanly restores stripCalAPerV
    cfgFactoryReset();
    check(std::abs(gCfg.stripCalAPerV - initialStripCal) < 1e-4, "Factory reset restores default 15.0f");
    check(gCfg.cfgRev == revBefore + 2, "cfgRev bumped on factory reset");
  }

  printf("\n=== Fuzz Test Summary: %s (%d failure%s) ===\n",
         failures ? "FAILED" : "PASSED", failures, failures == 1 ? "" : "s");
  return failures ? 1 : 0;
}
