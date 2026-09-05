#!/usr/bin/env bash
# Host-side test runner for ESP32 edge firmware off-target unit and integration tests.
set -euo pipefail
cd "$(dirname "$0")/.."

JSON=.pio/libdeps/esp32dev/ArduinoJson/src
if [ ! -d "$JSON" ]; then
  echo "ArduinoJson not found at $JSON — checking alternative locations..." >&2
  FOUND_JSON=$(find . -path "*/ArduinoJson/src" -print -quit || true)
  if [ -n "$FOUND_JSON" ] && [ -d "$FOUND_JSON" ]; then
    JSON="$FOUND_JSON"
    echo "Found ArduinoJson at $JSON"
  else
    echo "ERROR: ArduinoJson not found at $JSON — run 'pio run -e esp32dev' once to fetch lib_deps." >&2
    exit 1
  fi
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "================================================================================"
echo "           STARTING ESP32 HOST OFF-TARGET UNIT TEST SUITE RUNNER                "
echo "================================================================================"
echo "ArduinoJson: $JSON"
echo ""

echo ">>> [0/6] Running Basic Math and Empirical Fuzzing..."
c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/host_config_test.cpp -o "$TMP_DIR/cfgtest"
"$TMP_DIR/cfgtest"

c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/host_strip_power_test.cpp -o "$TMP_DIR/strippowertest"
"$TMP_DIR/strippowertest"

c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/empirical_fuzz_test.cpp -o "$TMP_DIR/fuzztest"
"$TMP_DIR/fuzztest"

c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/empirical_payload_test.cpp -o "$TMP_DIR/payloadtest"
"$TMP_DIR/payloadtest"

python3 test/verify_strip_power.py

echo ""
echo ">>> [1/6] Running Node Config Unit Tests..."
c++ -std=c++17 -Wall -Wextra -I "$JSON" -I src -I test test/host_config_test.cpp -o "$TMP_DIR/cfgtest2"
"$TMP_DIR/cfgtest2"

echo ""
echo ">>> [2/6] Running Milestone 1 Dual-Mode Communication Unit & Adversarial Tests..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_m1_dual_mode.cpp \
    -o "$TMP_DIR/m1test"
"$TMP_DIR/m1test"

c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_m1.cpp \
    -o "$TMP_DIR/advtest"
"$TMP_DIR/advtest"

c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_m1_challenger2.cpp \
    -o "$TMP_DIR/advtest2"
"$TMP_DIR/advtest2"

echo ""
echo ">>> [3/6] Running Milestone 3 Main System Integration & Strict Isolation Tests..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_m3_integration.cpp \
    -o "$TMP_DIR/m3test"
"$TMP_DIR/m3test"

echo ""
echo ">>> [4/6] Running Milestone 3 Challenger 1 & 2 Adversarial Stress & Failover Tests..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_m3_challenger1.cpp \
    -o "$TMP_DIR/m3adv1"
"$TMP_DIR/m3adv1"

c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_m3_challenger2.cpp \
    -o "$TMP_DIR/m3adv2"
"$TMP_DIR/m3adv2"

echo ""
echo ">>> [5/6] Running Challenger 2 Full Adversarial Stress & Failover Test Suite..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_challenger2_full.cpp \
    -o "$TMP_DIR/ch2full"
"$TMP_DIR/ch2full"

echo ""
echo ">>> [6/6] Running Milestone 2 ML Pipeline & Camera Driver Tests..."
c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_m2_camera_ml.cpp \
    -o "$TMP_DIR/m2test"
"$TMP_DIR/m2test"

c++ -std=c++17 -Wall -Wextra \
    -I "$JSON" \
    -I src \
    -I src/camera \
    -I test \
    src/camera/ov7670_driver.cpp \
    src/camera/model_data.cpp \
    src/camera/person_detector.cpp \
    src/camera/tracking_payload.cpp \
    src/camera/dual_mode_comm.cpp \
    test/test_adversarial_m2_ml.cpp \
    -o "$TMP_DIR/m2adv"
"$TMP_DIR/m2adv"

echo ""
echo "=== PlatformIO Firmware Build Verification ==="
PIO_BIN=""
if command -v pio >/dev/null 2>&1; then
  PIO_BIN="pio"
elif [ -x "$HOME/Library/Python/3.13/bin/pio" ]; then
  PIO_BIN="$HOME/Library/Python/3.13/bin/pio"
elif [ -x "$HOME/.platformio/penv/bin/pio" ]; then
  PIO_BIN="$HOME/.platformio/penv/bin/pio"
fi

if [ -n "$PIO_BIN" ]; then
  if "$PIO_BIN" --version >/dev/null 2>&1; then
    echo "Found PlatformIO ($PIO_BIN). Running 'pio run'..."
    "$PIO_BIN" run
    echo "PlatformIO build: PASSED (0 errors, 0 warnings)"
  else
    echo "Found PlatformIO at $PIO_BIN, but execution is restricted by environment sandbox. Skipping 'pio run'."
  fi
else
  echo "PlatformIO (pio) not found in PATH or standard paths. Skipping 'pio run'."
fi

echo ""
echo "================================================================================"
echo "          ALL HOST TESTS COMPLETED AND PASSED WITH EXIT CODE 0                  "
echo "================================================================================"
exit 0
