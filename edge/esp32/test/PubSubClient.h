#pragma once
#include "arduino_shim.h"
#include <string>
#include <vector>
#include <functional>
#include <cstdint>
#include <cstring>

struct MqttMessageRecord {
  std::string topic;
  std::vector<uint8_t> payload;
  bool retained = false;
};

class PubSubClient {
public:
  bool isMqttConnected = false;
  int stateCode = 0; // 0 = MQTT_CONNECTED
  std::string serverHost;
  uint16_t serverPort = 1883;
  std::vector<MqttMessageRecord> publishHistory;
  std::vector<std::string> subscriptions;
  std::function<void(char*, uint8_t*, unsigned int)> callback;
  bool failNextPublish = false;

  PubSubClient() {}
  PubSubClient(Stream&) {}

  PubSubClient& setServer(const char* host, uint16_t port) {
    serverHost = host ? host : "";
    serverPort = port;
    return *this;
  }
  PubSubClient& setServer(const IPAddress& ip, uint16_t port) {
    serverHost = ip.toString();
    serverPort = port;
    return *this;
  }
  PubSubClient& setCallback(std::function<void(char*, uint8_t*, unsigned int)> cb) {
    callback = cb;
    return *this;
  }
  PubSubClient& setClient(Stream&) { return *this; }
  PubSubClient& setKeepAlive(uint16_t) { return *this; }
  PubSubClient& setSocketTimeout(uint16_t) { return *this; }
  bool setBufferSize(uint16_t) { return true; }
  uint16_t getBufferSize() { return 512; }

  bool connect(const char* id, const char* user = nullptr, const char* pass = nullptr) {
    (void)id; (void)user; (void)pass;
    isMqttConnected = true;
    stateCode = 0;
    return true;
  }
  bool connect(const char* id, const char* user, const char* pass, const char* willTopic, uint8_t willQos, bool willRetain, const char* willMessage) {
    (void)id; (void)user; (void)pass; (void)willTopic; (void)willQos; (void)willRetain; (void)willMessage;
    isMqttConnected = true;
    stateCode = 0;
    return true;
  }
  void disconnect() {
    isMqttConnected = false;
    stateCode = -1;
  }

  bool publish(const char* topic, const char* payload) {
    return publish(topic, (const uint8_t*)payload, payload ? (unsigned int)strlen(payload) : 0, false);
  }
  bool publish(const char* topic, const char* payload, bool retained) {
    return publish(topic, (const uint8_t*)payload, payload ? (unsigned int)strlen(payload) : 0, retained);
  }
  bool publish(const char* topic, const uint8_t* payload, unsigned int len) {
    return publish(topic, payload, len, false);
  }
  bool publish(const char* topic, const uint8_t* payload, unsigned int len, bool retained) {
    if (!isMqttConnected || failNextPublish) {
      if (failNextPublish) failNextPublish = false;
      return false;
    }
    MqttMessageRecord msg;
    msg.topic = topic ? topic : "";
    if (payload && len > 0) {
      msg.payload.assign(payload, payload + len);
    }
    msg.retained = retained;
    publishHistory.push_back(msg);
    return true;
  }

  bool subscribe(const char* topic, uint8_t = 0) {
    if (!isMqttConnected) return false;
    subscriptions.push_back(topic ? topic : "");
    return true;
  }

  bool loop() { return isMqttConnected; }
  bool connected() const { return isMqttConnected; }
  int state() const { return stateCode; }

  // Mock control & introspection API
  void clearHistory() {
    publishHistory.clear();
    subscriptions.clear();
  }
  size_t getPublishCount() const { return publishHistory.size(); }
  const MqttMessageRecord& getLastPublished() const {
    static MqttMessageRecord empty;
    if (publishHistory.empty()) return empty;
    return publishHistory.back();
  }
  std::string getLastPublishedPayload() const {
    if (publishHistory.empty()) return "";
    return std::string((const char*)publishHistory.back().payload.data(), publishHistory.back().payload.size());
  }
  void setMockConnected(bool conn) {
    isMqttConnected = conn;
    stateCode = conn ? 0 : -1;
  }
};
