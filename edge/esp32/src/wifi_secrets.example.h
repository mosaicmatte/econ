#pragma once
// Copy this file to wifi_secrets.h (gitignored) and fill in your site's values.
// The ESP32 only joins 2.4 GHz networks; MQTT_HOST is the LAN IP of the machine
// running `docker compose up` in econ/server (macOS: `ipconfig getifaddr en0`).
const char* WIFI_SSID = "YOUR_WIFI_SSID";
const char* WIFI_PASS = "YOUR_WIFI_PASSWORD";
const char* MQTT_HOST = "192.168.1.100";
