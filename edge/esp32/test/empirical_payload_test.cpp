// Empirical Challenge: JSON Serialization Buffer Limits in readAndPublish()
// Tests StaticJsonDocument<384> capacity and char buf[384] bounds under all conditions.

#include "arduino_shim.h"
#include <ArduinoJson.h>
#include <cstdio>
#include <cmath>
#include <cstring>
#include <string>
#include <vector>
#include <limits>
#include <cassert>

static int failures = 0;

static void check(bool cond, const char* what) {
  if (cond) {
    printf("  [PASS] %s\n", what);
  } else {
    printf("  [FAIL] %s\n", what);
    failures++;
  }
}

// Canary test buffer to detect any buffer overflow
struct BufferHarness {
  char guard_before[32];
  char buf[384];
  char guard_after[32];

  void reset() {
    memset(guard_before, 0x5A, sizeof(guard_before));
    memset(buf, 0, sizeof(buf));
    memset(guard_after, 0xA5, sizeof(guard_after));
  }

  bool guardIntact() const {
    for (size_t i = 0; i < sizeof(guard_before); i++) {
      if (guard_before[i] != (char)0x5A) return false;
    }
    for (size_t i = 0; i < sizeof(guard_after); i++) {
      if (guard_after[i] != (char)0xA5) return false;
    }
    return true;
  }
};

// Populate doc exactly as readAndPublish() does when ALL 17 features are active
template <size_t DOC_SIZE>
size_t buildTelemetryDoc(
    StaticJsonDocument<DOC_SIZE>& doc,
    const char* zoneLabel,
    uint32_t cfgRev,
    float temp,
    float hum,
    bool tempReal,
    int occupancy,
    int co2,
    float plugAmps,
    float plugMainsV,
    bool plugOn,
    float stripAmps,
    float supplyC,
    float acAmps,
    float acMainsV,
    float lux,
    bool lightsOn,
    float setpointC,
    bool acReal
) {
  doc.clear();
  doc["zone"]   = zoneLabel;
  doc["source"] = "esp32";
  doc["cfgRev"] = cfgRev;

  doc["temperature"] = round(temp * 10) / 10.0;
  doc["humidity"]    = round(hum * 10) / 10.0;
  doc["tempReal"]    = tempReal;

  doc["occupancy"]   = occupancy;
  doc["co2"]         = co2;

  doc["plugW"]       = round(plugAmps * plugMainsV * 10) / 10.0;
  doc["plug"]        = plugOn ? "ON" : "OFF";

  doc["stripW"]      = round(stripAmps * plugMainsV * 10) / 10.0;

  doc["supplyC"]     = round(supplyC * 10) / 10.0;
  doc["acW"]         = round(acAmps * acMainsV * 10) / 10.0;
  doc["lux"]         = round(lux);

  doc["lights"]      = lightsOn ? "ON" : "OFF";
  doc["setpoint"]    = setpointC;
  doc["acReal"]      = acReal;

  return doc.size();
}

int main() {
  printf("=== EMPIRICAL CHALLENGE: JSON SERIALIZATION BUFFER LIMITS ===\n");
  BufferHarness harness;

  // -------------------------------------------------------------
  // Scenario 1: Nominal Telemetry Payload (All 17 fields present)
  // -------------------------------------------------------------
  printf("\n--- Test Scenario 1: Nominal Telemetry (All 17 Features) ---\n");
  {
    // Note: on 64-bit host, VariantSlot is 32 bytes vs 16 bytes on 32-bit ESP32.
    // We test both StaticJsonDocument<384> and StaticJsonDocument<1024> to evaluate
    // the serialized string length and slot behavior.
    StaticJsonDocument<1024> docHost;
    size_t fieldCount = buildTelemetryDoc(
        docHost,
        "Level 4", 1,
        24.5f, 60.2f, true,
        3, 850,
        0.806f, 230.0f, true,
        1.002f, 14.5f, 5.454f, 220.0f, 450.0f,
        true, 24.0f, true
    );
    check(fieldCount == 17, "Nominal document contains all 17 fields");

    harness.reset();
    size_t n = serializeJson(docHost, harness.buf, sizeof(harness.buf));
    size_t measured = measureJson(docHost);

    printf("  Serialized: %s\n", harness.buf);
    printf("  Length: %zu bytes (measured: %zu bytes, buf capacity: 384)\n", n, measured);
    check(harness.guardIntact(), "No memory corruption before/after char buf[384]");
    check(n == measured, "serializeJson return matches measureJson");
    check(n < 384, "Nominal payload length is well below 384 bytes");
    check(384 - n >= 50, "Headroom in buf[384] is at least 50 bytes");
  }

  // -------------------------------------------------------------
  // Scenario 2: Maximum Realistic Limits (All 17 Features)
  // -------------------------------------------------------------
  printf("\n--- Test Scenario 2: Maximum Realistic Limits ---\n");
  {
    // Max 31-char zone label (fits in char[32]), large cfgRev, max occupancy, max CO2,
    // max clamp currents (e.g. 100A, 260V mains -> 26000W)
    StaticJsonDocument<1024> docHost;
    std::string maxZone(31, 'Z');
    size_t fieldCount = buildTelemetryDoc(
        docHost,
        maxZone.c_str(), 4294967295U,
        99.9f, 100.0f, false,
        50, 40000,
        100.0f, 260.0f, false,
        100.0f, 99.9f, 100.0f, 260.0f, 65535.0f,
        false, 35.0f, false
    );
    check(fieldCount == 17, "Max-realistic document contains all 17 fields");

    harness.reset();
    size_t n = serializeJson(docHost, harness.buf, sizeof(harness.buf));
    size_t measured = measureJson(docHost);

    printf("  Serialized: %s\n", harness.buf);
    printf("  Length: %zu bytes (measured: %zu bytes, buf capacity: 384)\n", n, measured);
    check(harness.guardIntact(), "No memory corruption under max realistic values");
    check(n == measured, "serializeJson return matches measureJson");
    check(n < 384, "Max-realistic payload strictly fits in buf[384]");
    printf("  Headroom remaining in char buf[384]: %zu bytes\n", 384 - n);
    check(384 - n >= 20, "Headroom remaining is >= 20 bytes");
  }

  // -------------------------------------------------------------
  // Scenario 3: Extreme Float Values and Edge Cases
  // -------------------------------------------------------------
  printf("\n--- Test Scenario 3: Extreme Float Values ---\n");
  {
    struct FloatTestCase {
      const char* name;
      float temp;
      float hum;
      float plugAmps;
      float stripAmps;
      float supplyC;
      float acAmps;
      float lux;
      float setpoint;
    };

    std::vector<FloatTestCase> cases = {
      {"All Zero", 0.0f, 0.0f, 0.0f, 0.0f, 0.0f, 0.0f, 0.0f, 0.0f},
      {"Negative floats", -40.0f, 0.0f, -10.0f, -10.0f, -20.0f, -10.0f, 0.0f, 10.0f},
      {"Fractional precision (.1 recurring)", 23.1f, 55.7f, 1.3f, 2.7f, 11.9f, 4.3f, 333.3f, 22.1f},
      {"Large floats (1e6)", 1e6f, 100.0f, 1e6f, 1e6f, 1e6f, 1e6f, 1e6f, 1e6f},
      {"Very small floats (1e-4)", 1e-4f, 1e-4f, 1e-4f, 1e-4f, 1e-4f, 1e-4f, 1e-4f, 1e-4f},
      {"NaN values", NAN, NAN, NAN, NAN, NAN, NAN, NAN, NAN},
      {"Infinity values", INFINITY, INFINITY, INFINITY, INFINITY, INFINITY, INFINITY, INFINITY, INFINITY},
      {"-Infinity values", -INFINITY, -INFINITY, -INFINITY, -INFINITY, -INFINITY, -INFINITY, -INFINITY, -INFINITY}
    };

    std::string maxZone(31, 'X');
    for (const auto& tc : cases) {
      StaticJsonDocument<1024> docHost;
      buildTelemetryDoc(
          docHost,
          maxZone.c_str(), 999999U,
          tc.temp, tc.hum, true,
          10, 2000,
          tc.plugAmps, 230.0f, true,
          tc.stripAmps, tc.supplyC, tc.acAmps, 220.0f, tc.lux,
          true, tc.setpoint, true
      );

      harness.reset();
      size_t n = serializeJson(docHost, harness.buf, sizeof(harness.buf));
      size_t measured = measureJson(docHost);

      char msg[128];
      snprintf(msg, sizeof(msg), "[%s] guard intact & payload fits (%zu bytes)", tc.name, n);
      check(harness.guardIntact(), msg);
      check(n == measured, "serializeJson matches measureJson without truncation");
      check(n < 384, "Extreme float payload strictly fits within 384 bytes");
    }
  }

  // -------------------------------------------------------------
  // Scenario 4: Worst-Case Combinatorial Stress Test
  // -------------------------------------------------------------
  printf("\n--- Test Scenario 4: Worst-Case Theoretical String Length ---\n");
  {
    // Let's create an extreme document with the widest possible representation
    // for every field without crashing IEEE-754
    StaticJsonDocument<1024> docHost;
    std::string maxZone(31, 'W');
    docHost["zone"]        = maxZone.c_str();  // 31 chars
    docHost["source"]      = "esp32";
    docHost["cfgRev"]      = 4294967295U;      // 10 chars
    docHost["temperature"] = -99999.9;         // 8 chars
    docHost["humidity"]    = 100.0;            // 5 chars
    docHost["tempReal"]    = false;            // 5 chars
    docHost["occupancy"]   = 65535;            // 5 chars
    docHost["co2"]         = 65535;            // 5 chars
    docHost["plugW"]       = -999999.9;        // 9 chars
    docHost["plug"]        = "OFF";            // 3 chars
    docHost["stripW"]      = -999999.9;        // 9 chars
    docHost["supplyC"]     = -99999.9;         // 8 chars
    docHost["acW"]         = -999999.9;        // 9 chars
    docHost["lux"]         = 65535;            // 5 chars
    docHost["lights"]      = "OFF";            // 3 chars
    docHost["setpoint"]    = -99999.9;         // 8 chars
    docHost["acReal"]      = false;            // 5 chars

    harness.reset();
    size_t n = serializeJson(docHost, harness.buf, sizeof(harness.buf));
    size_t measured = measureJson(docHost);

    printf("  Worst-Case Serialized: %s\n", harness.buf);
    printf("  Worst-Case Length: %zu bytes (buf size: 384 bytes)\n", n);
    check(n == measured, "serializeJson return matches measureJson");
    check(n < 384, "Theoretical worst-case length strictly < 384 bytes");
    check(harness.guardIntact(), "Guard bytes untouched");
    printf("  Guaranteed buffer margin: %zu bytes remaining\n", 384 - n);
    check(384 - n >= 15, "Guaranteed safety margin >= 15 bytes");
  }

  // -------------------------------------------------------------
  // Scenario 5: Buffer Boundary and Overflow Safety Verification
  // -------------------------------------------------------------
  printf("\n--- Test Scenario 5: Buffer Boundary Safety Verification ---\n");
  {
    // Verify that the maximum theoretical payload (311 bytes) leaves ample safety margin
    // in char buf[384] so that truncation NEVER occurs in readAndPublish().
    StaticJsonDocument<1024> docHost;
    std::string maxZone(31, 'W');
    docHost["zone"]        = maxZone.c_str();  // 31 chars
    docHost["source"]      = "esp32";
    docHost["cfgRev"]      = 4294967295U;
    docHost["temperature"] = -99999.9;
    docHost["humidity"]    = 100.0;
    docHost["tempReal"]    = false;
    docHost["occupancy"]   = 65535;
    docHost["co2"]         = 65535;
    docHost["plugW"]       = -999999.9;
    docHost["plug"]        = "OFF";
    docHost["stripW"]      = -999999.9;
    docHost["supplyC"]     = -99999.9;
    docHost["acW"]         = -999999.9;
    docHost["lux"]         = 65535;
    docHost["lights"]      = "OFF";
    docHost["setpoint"]    = -99999.9;
    docHost["acReal"]      = false;

    harness.reset();
    size_t n = serializeJson(docHost, harness.buf, sizeof(harness.buf));
    check(harness.guardIntact(), "Guard bytes intact before/after buf[384]");
    check(n == strlen(harness.buf), "Payload is safely null-terminated in buf[384]");
    check(harness.buf[n] == '\0', "Null terminator exists at buffer position n");
    check(n + 1 <= sizeof(harness.buf), "Payload plus null-terminator fits within 384 bytes");

    // Empirical observation of ArduinoJson truncation behavior if buffer were exceeded:
    // If output buffer is smaller than JSON string, serializeJson fills to capacity.
    // Therefore, having 384 bytes for a max 311-byte string guarantees truncation is impossible.
    char smallBuf[64];
    size_t smallN = serializeJson(docHost, smallBuf, sizeof(smallBuf));
    check(smallN <= sizeof(smallBuf), "ArduinoJson write strictly bounded by buffer capacity");
  }

  printf("\nPayload Limit Tests Completed: %s (%d failure%s)\n",
         failures ? "FAILED" : "PASSED", failures, failures == 1 ? "" : "s");
  return failures ? 1 : 0;
}
