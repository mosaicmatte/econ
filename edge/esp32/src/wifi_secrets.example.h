#pragma once
// Copy this file to wifi_secrets.h (gitignored) and fill in your site's values.
// The ESP32 only joins 2.4 GHz networks; MQTT_HOST is the LAN IP of the machine
// running `docker compose up` in econ/server (macOS: `ipconfig getifaddr en0`).
const char* WIFI_SSID = "YOUR_WIFI_SSID";
const char* WIFI_PASS = "YOUR_WIFI_PASSWORD";
const char* MQTT_HOST = "192.168.1.100";

// Broker credentials, printed by econ/server/setup-mqtt-auth.sh. Every node shares the
// econ-node identity, which may publish telemetry and subscribe to its own commands but
// may NOT publish commands — so one compromised ceiling box cannot switch the floor.
//
// Comment these out only for a bench test against the anonymous dev broker
// (mosquitto.dev.conf). Against the shipped broker, a node without them fails with rc=5.
#define MQTT_USER "econ-node"
#define MQTT_PASS "PASTE_FROM_setup-mqtt-auth.sh"
