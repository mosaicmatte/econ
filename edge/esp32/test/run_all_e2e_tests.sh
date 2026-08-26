#!/usr/bin/env bash
# ==============================================================================
# run_all_e2e_tests.sh — Unified Host E2E Test Suite Runner
#
# Compiles and executes:
#   1. Node Runtime Config Unit & Safety Tests (host_config_test.cpp)
#   2. Full 4-Tier Opaque-Box E2E Test Suite (test_e2e_opaque_box.cpp)
#
# Returns exit code 0 if all tests pass, non-zero otherwise.
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

JSON=".pio/libdeps/esp32dev/ArduinoJson/src"
if [ ! -d "$JSON" ]; then
  echo "ArduinoJson not found at $JSON — checking alternative locations..." >&2
  FOUND_JSON=$(find "$ROOT_DIR/.." -path "*/ArduinoJson/src" -print -quit || true)
  if [ -n "$FOUND_JSON" ] && [ -d "$FOUND_JSON" ]; then
    JSON="$FOUND_JSON"
    echo "Found ArduinoJson at $JSON"
  else
    echo "ERROR: ArduinoJson not found. Please run 'pio run -e esp32dev' once to fetch dependencies." >&2
    exit 1
  fi
fi

TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

echo "================================================================================"
echo "           STARTING ESP32 HOST OFF-TARGET E2E TEST SUITE RUNNER                 "
echo "================================================================================"
echo "Base Directory: $ROOT_DIR"
echo "ArduinoJson:    $JSON"
echo ""

# ------------------------------------------------------------------------------
# Test 1: Node Runtime Config Unit & Safety Test
# ------------------------------------------------------------------------------
echo ">>> [1/2] Compiling and running host_config_test.cpp..."
CFG_BIN="$TEMP_DIR/host_config_test"
c++ -std=c++17 -Wall -Wextra -I "$JSON" -I src -I test test/host_config_test.cpp -o "$CFG_BIN"
"$CFG_BIN"
echo ">>> [1/2] host_config_test: SUCCESS"
echo ""

# ------------------------------------------------------------------------------
# Test 2: Full 4-Tier Opaque-Box E2E Test Suite
# ------------------------------------------------------------------------------
echo ">>> [2/2] Compiling and running test_e2e_opaque_box.cpp..."
E2E_BIN="$TEMP_DIR/test_e2e_opaque_box"
c++ -std=c++17 -Wall -Wextra -I "$JSON" -I src -I test test/test_e2e_opaque_box.cpp -o "$E2E_BIN"
"$E2E_BIN"
echo ">>> [2/2] test_e2e_opaque_box: SUCCESS"
echo ""

echo "================================================================================"
echo "          ALL E2E & HOST TESTS COMPLETED AND PASSED WITH EXIT CODE 0            "
echo "================================================================================"
exit 0
