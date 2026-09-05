// -----------------------------------------------------------------------------
// dual_mode_comm.h — Dual-Mode Non-Blocking Communication Engine
//
// Features:
//   * Primary Transport: Wi-Fi UDP Broadcast on port 4210 (:4210) & MQTT hook.
//   * Fallback Transport: Automatic zero-delay USB Serial (UART0 115200) JSON.
//   * Guaranteed <0.2ms execution slice time per tick (never starves camera/ML).
//   * Full dependency injection for off-target host testing.
// -----------------------------------------------------------------------------
#pragma once

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>

#include "tracking_payload.h"

#if defined(ARDUINO) && !defined(HOST_TEST)
#include <Arduino.h>
#include <WiFi.h>
#include <WiFiUdp.h>
#include <PubSubClient.h>
#else
#include "arduino_shim.h"
#include "PubSubClient.h"
#endif

// Communication State Machine States
enum CommState {
  COMM_STATE_UNINITIALIZED = 0,
  COMM_STATE_SERIAL_ONLY   = 1,
  COMM_STATE_CONNECTING    = 2,
  COMM_STATE_CONNECTED     = 3,
  COMM_STATE_DISCONNECTED  = 4
};

// Active Transport Mode
enum CommTransportMode {
  COMM_TRANSPORT_NONE      = 0,
  COMM_TRANSPORT_SERIAL    = 1,
  COMM_TRANSPORT_WIFI_UDP  = 2,
  COMM_TRANSPORT_WIFI_MQTT = 3,
  COMM_TRANSPORT_WIFI_DUAL = 4
};

// Configuration Parameters
struct CommConfig {
  const char* wifi_ssid             = nullptr;
  const char* wifi_pass             = nullptr;
  const char* mqtt_host             = nullptr;
  uint16_t    mqtt_port             = 1883;
  const char* mqtt_topic            = "econ/telemetry/zone_1";
  const char* zone_topic            = "zone_1";
  const char* zone_label            = "Level 4";
  const char* sensor_id             = "esp32_cam_01";
  uint16_t    udp_port              = 4210;
  uint16_t    udp_broadcast_port    = 4210;
  IPAddress   broadcast_ip          = IPAddress(255, 255, 255, 255);
  uint32_t    connect_timeout_ms    = 8000;
  uint32_t    reconnect_interval_ms = 5000;
  bool        enable_udp_broadcast  = true;
  bool        enable_serial_fallback= true;
};

CommConfig defaultCommConfig();

class DualModeComm {
public:
  using Config = CommConfig;

  // Constructors
  DualModeComm();
  DualModeComm(WiFiUDP& udp, PubSubClient& mqtt, Stream& serial);
  DualModeComm(WiFiUDP& udp, Stream& serial);
  DualModeComm(WiFiUDP& udp, PubSubClient& mqtt);
  explicit DualModeComm(Stream& serial);
  virtual ~DualModeComm();

  // Initialization & Lifecycle
  bool begin(const CommConfig& config);
  bool init(const char* ssid = nullptr, const char* pass = nullptr, uint16_t port = 4210);
  void update(); // Non-blocking state machine tick (<0.2ms)
  void tick();   // Alias for update()
  void stop();

  // Transmission API
  virtual bool transmit(const PersonTrackingData& data);
  virtual bool transmit(const char* payload, size_t len);
  virtual bool transmitRaw(const char* json_buffer, size_t len);

  // Status & Telemetry Queries
  CommState getState() const { return _state; }
  CommTransportMode getActiveTransport() const;
  bool isWifiConnected() const;
  bool isWiFiConnected() const { return isWifiConnected(); }
  bool isPrimaryTransportActive() const;
  bool isSerialFallbackActive() const;

  uint32_t getSuccessfulTransmissions() const { return _tx_success_count; }
  uint32_t getPacketsSentWiFi() const { return _tx_success_count; }
  uint32_t getFallbackTransmissions() const { return _tx_fallback_count; }
  uint32_t getPacketsSentSerial() const { return _tx_fallback_count; }
  uint32_t getFailoverCount() const { return _failover_count; }
  uint32_t getLastStateChangeTime() const { return _state_enter_time; }

  // Socket & Endpoint Accessors
  WiFiUDP& getUDP() { return *_udp; }
  void setBroadcastIP(const IPAddress& ip) { _config.broadcast_ip = ip; }
  IPAddress getBroadcastIP() const { return _config.broadcast_ip; }

  // MQTT Hook Management
  void setMqttClient(PubSubClient* client, const char* telemetry_topic = nullptr);

  // Control & Testing Helpers
  void forceDisconnect();
  void reconnect();
  void setWifiCredentials(const char* ssid, const char* pass);

private:
  CommConfig        _config;
  CommState         _state;
  uint32_t          _state_enter_time;
  uint32_t          _last_reconnect_attempt;
  uint32_t          _tx_success_count;
  uint32_t          _tx_fallback_count;
  uint32_t          _failover_count;
  bool              _last_wifi_connected;

  WiFiUDP*          _udp;
  PubSubClient*     _mqtt_client;
  Stream*           _serial;

  bool              _owns_udp;
  bool              _udp_bound;
  char              _telemetry_topic[64];

  void transitionTo(CommState new_state);
  bool sendUdpBroadcast(const char* buf, size_t len);
  bool sendMqtt(const char* buf, size_t len);
  bool sendSerial(const char* buf, size_t len);
};
