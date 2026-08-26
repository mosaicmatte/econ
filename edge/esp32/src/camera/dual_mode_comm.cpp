// -----------------------------------------------------------------------------
// dual_mode_comm.cpp — Dual-Mode Non-Blocking Communication Engine
// -----------------------------------------------------------------------------
#include "dual_mode_comm.h"

CommConfig defaultCommConfig() {
  CommConfig cfg;
  return cfg;
}

DualModeComm::DualModeComm()
  : _state(COMM_STATE_UNINITIALIZED),
    _state_enter_time(0),
    _last_reconnect_attempt(0),
    _tx_success_count(0),
    _tx_fallback_count(0),
    _failover_count(0),
    _last_wifi_connected(false),
    _udp(nullptr),
    _mqtt_client(nullptr),
    _serial(&Serial),
    _owns_udp(true),
    _udp_bound(false)
{
  _udp = new WiFiUDP();
  _telemetry_topic[0] = '\0';
}

DualModeComm::DualModeComm(WiFiUDP& udp, PubSubClient& mqtt, Stream& serial)
  : _state(COMM_STATE_UNINITIALIZED),
    _state_enter_time(0),
    _last_reconnect_attempt(0),
    _tx_success_count(0),
    _tx_fallback_count(0),
    _failover_count(0),
    _last_wifi_connected(false),
    _udp(&udp),
    _mqtt_client(&mqtt),
    _serial(&serial),
    _owns_udp(false),
    _udp_bound(false)
{
  _telemetry_topic[0] = '\0';
}

DualModeComm::DualModeComm(WiFiUDP& udp, Stream& serial)
  : _state(COMM_STATE_UNINITIALIZED),
    _state_enter_time(0),
    _last_reconnect_attempt(0),
    _tx_success_count(0),
    _tx_fallback_count(0),
    _failover_count(0),
    _last_wifi_connected(false),
    _udp(&udp),
    _mqtt_client(nullptr),
    _serial(&serial),
    _owns_udp(false),
    _udp_bound(false)
{
  _telemetry_topic[0] = '\0';
}

DualModeComm::DualModeComm(WiFiUDP& udp, PubSubClient& mqtt)
  : _state(COMM_STATE_UNINITIALIZED),
    _state_enter_time(0),
    _last_reconnect_attempt(0),
    _tx_success_count(0),
    _tx_fallback_count(0),
    _failover_count(0),
    _last_wifi_connected(false),
    _udp(&udp),
    _mqtt_client(&mqtt),
    _serial(&Serial),
    _owns_udp(false),
    _udp_bound(false)
{
  _telemetry_topic[0] = '\0';
}

DualModeComm::DualModeComm(Stream& serial)
  : _state(COMM_STATE_UNINITIALIZED),
    _state_enter_time(0),
    _last_reconnect_attempt(0),
    _tx_success_count(0),
    _tx_fallback_count(0),
    _failover_count(0),
    _last_wifi_connected(false),
    _udp(nullptr),
    _mqtt_client(nullptr),
    _serial(&serial),
    _owns_udp(true),
    _udp_bound(false)
{
  _udp = new WiFiUDP();
  _telemetry_topic[0] = '\0';
}

DualModeComm::~DualModeComm() {
  stop();
  if (_owns_udp && _udp) {
    delete _udp;
    _udp = nullptr;
  }
}

bool DualModeComm::begin(const CommConfig& config) {
  _config = config;

  // Configure telemetry topic
  if (_config.mqtt_topic && strlen(_config.mqtt_topic) > 0) {
    strncpy(_telemetry_topic, _config.mqtt_topic, sizeof(_telemetry_topic) - 1);
    _telemetry_topic[sizeof(_telemetry_topic) - 1] = '\0';
  } else if (_config.zone_topic && strlen(_config.zone_topic) > 0) {
    snprintf(_telemetry_topic, sizeof(_telemetry_topic), "econ/telemetry/%s", _config.zone_topic);
  } else {
    strncpy(_telemetry_topic, "econ/telemetry/zone_1", sizeof(_telemetry_topic) - 1);
  }

  // Initialize UDP socket
  uint16_t port = _config.udp_port ? _config.udp_port : _config.udp_broadcast_port;
  if (_udp && !_udp_bound) {
    _udp->begin(port);
    _udp_bound = true;
  }

  // Check Wi-Fi configuration
  if (_config.wifi_ssid && strlen(_config.wifi_ssid) > 0) {
    WiFi.mode(WIFI_STA);
    WiFi.begin(_config.wifi_ssid, _config.wifi_pass);
    
    if (isWifiConnected()) {
      transitionTo(COMM_STATE_CONNECTED);
      _last_wifi_connected = true;
    } else {
      transitionTo(COMM_STATE_CONNECTING);
      _last_wifi_connected = false;
    }
  } else {
    transitionTo(COMM_STATE_SERIAL_ONLY);
    _last_wifi_connected = false;
  }

  _last_reconnect_attempt = millis();
  return true;
}

bool DualModeComm::init(const char* ssid, const char* pass, uint16_t port) {
  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = ssid;
  cfg.wifi_pass = pass;
  cfg.udp_port = port;
  cfg.udp_broadcast_port = port;
  return begin(cfg);
}

void DualModeComm::stop() {
  if (_udp && _udp_bound) {
    _udp->stop();
    _udp_bound = false;
  }
  transitionTo(COMM_STATE_UNINITIALIZED);
}

void DualModeComm::transitionTo(CommState new_state) {
  _state = new_state;
  _state_enter_time = millis();
}

void DualModeComm::update() {
  uint32_t now = millis();
  bool wifi_conn = isWifiConnected();

  if (_state == COMM_STATE_SERIAL_ONLY) {
    _last_wifi_connected = false;
    return;
  }

  if (_state == COMM_STATE_CONNECTED) {
    if (!wifi_conn) {
      transitionTo(COMM_STATE_DISCONNECTED);
      _failover_count++;
    }
  } else if (_state == COMM_STATE_CONNECTING) {
    if (wifi_conn) {
      transitionTo(COMM_STATE_CONNECTED);
    } else if (now - _state_enter_time >= _config.connect_timeout_ms) {
      transitionTo(COMM_STATE_DISCONNECTED);
      _failover_count++;
    }
  } else if (_state == COMM_STATE_DISCONNECTED) {
    if (wifi_conn) {
      transitionTo(COMM_STATE_CONNECTED);
    } else if (now - _last_reconnect_attempt >= _config.reconnect_interval_ms) {
      _last_reconnect_attempt = now;
      if (_config.wifi_ssid && strlen(_config.wifi_ssid) > 0) {
        WiFi.begin(_config.wifi_ssid, _config.wifi_pass);
      }
    }
  }

  _last_wifi_connected = wifi_conn;
}

void DualModeComm::tick() {
  update();
}

bool DualModeComm::isWifiConnected() const {
  return (WiFi.status() == WL_CONNECTED || WiFi.isConnected());
}

bool DualModeComm::isPrimaryTransportActive() const {
  if (_state == COMM_STATE_SERIAL_ONLY) return false;
  return isWifiConnected();
}

bool DualModeComm::isSerialFallbackActive() const {
  return !isPrimaryTransportActive();
}

CommTransportMode DualModeComm::getActiveTransport() const {
  if (isPrimaryTransportActive()) {
    if (_mqtt_client && _mqtt_client->connected()) {
      return COMM_TRANSPORT_WIFI_DUAL;
    }
    return COMM_TRANSPORT_WIFI_UDP;
  }
  if (_state == COMM_STATE_SERIAL_ONLY || !isWifiConnected()) {
    return COMM_TRANSPORT_SERIAL;
  }
  return COMM_TRANSPORT_NONE;
}

void DualModeComm::setMqttClient(PubSubClient* client, const char* telemetry_topic) {
  _mqtt_client = client;
  if (telemetry_topic && strlen(telemetry_topic) > 0) {
    strncpy(_telemetry_topic, telemetry_topic, sizeof(_telemetry_topic) - 1);
    _telemetry_topic[sizeof(_telemetry_topic) - 1] = '\0';
  }
}

bool DualModeComm::transmit(const PersonTrackingData& data) {
  char buf[TRACKING_PAYLOAD_BUFFER_SIZE];

  // Try Primary Transport (Wi-Fi UDP Broadcast + MQTT)
  if (isPrimaryTransportActive()) {
    size_t len = serializeTrackingPayload(data, buf, sizeof(buf));
    if (len > 0) {
      bool udp_ok = sendUdpBroadcast(buf, len);
      if (_mqtt_client && _mqtt_client->connected()) {
        sendMqtt(buf, len);
      }
      if (udp_ok) {
        _tx_success_count++;
        return true;
      }
      // Immediate failover if UDP socket transmit failed
      _failover_count++;
    }
  }

  // Automatic Fallback Transport (USB Serial UART0 115200)
  size_t len = serializeTrackingPayloadForSerial(data, _telemetry_topic, buf, sizeof(buf));
  if (len == 0) {
    len = serializeTrackingPayload(data, buf, sizeof(buf));
  }

  if (len > 0) {
    bool serial_ok = sendSerial(buf, len);
    if (serial_ok) {
      _tx_fallback_count++;
      return true;
    }
  }

  return false;
}

bool DualModeComm::transmit(const char* payload, size_t len) {
  if (!payload || len == 0) return false;

  if (isPrimaryTransportActive()) {
    bool udp_ok = sendUdpBroadcast(payload, len);
    if (_mqtt_client && _mqtt_client->connected()) {
      sendMqtt(payload, len);
    }
    if (udp_ok) {
      _tx_success_count++;
      return true;
    }
    _failover_count++;
  }

  bool serial_ok = sendSerial(payload, len);
  if (serial_ok) {
    _tx_fallback_count++;
    return true;
  }

  return false;
}

bool DualModeComm::transmitRaw(const char* json_buffer, size_t len) {
  return transmit(json_buffer, len);
}

bool DualModeComm::sendUdpBroadcast(const char* buf, size_t len) {
  if (!_udp || !_config.enable_udp_broadcast) return false;

  if (!_udp_bound) {
    uint16_t port = _config.udp_port ? _config.udp_port : _config.udp_broadcast_port;
    _udp->begin(port);
    _udp_bound = true;
  }

  uint16_t port = _config.udp_port ? _config.udp_port : _config.udp_broadcast_port;
  if (_udp->beginPacket(_config.broadcast_ip, port) == 0) {
    return false;
  }

  size_t written = _udp->write((const uint8_t*)buf, len);
  if (written != len) {
    return false;
  }

  return (_udp->endPacket() == 1);
}

bool DualModeComm::sendMqtt(const char* buf, size_t len) {
  if (!_mqtt_client || !_mqtt_client->connected()) return false;
  const char* topic = _telemetry_topic[0] ? _telemetry_topic : _config.mqtt_topic;
  return _mqtt_client->publish(topic, (const uint8_t*)buf, (unsigned int)len, false);
}

bool DualModeComm::sendSerial(const char* buf, size_t len) {
  if (!_serial || !_config.enable_serial_fallback) return false;
  _serial->write((const uint8_t*)buf, len);
  _serial->write((uint8_t)'\n');
  return true;
}

void DualModeComm::forceDisconnect() {
  WiFi.disconnect();
  transitionTo(COMM_STATE_DISCONNECTED);
  _failover_count++;
  _last_wifi_connected = false;
  _last_reconnect_attempt = 0;
}

void DualModeComm::reconnect() {
  WiFi.reconnect();
  transitionTo(COMM_STATE_CONNECTED);
  _last_wifi_connected = true;
}
