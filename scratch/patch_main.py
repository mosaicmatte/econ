import re

with open('edge/esp32/src/main.cpp', 'r') as f:
    content = f.read()

# Restore setupWifi to be non-blocking and use global string for SSID/PASS
content = re.sub(
    r'void setupWifi\(\) \{[\s\S]*?\}',
    r'''String current_ssid = WIFI_SSID;
String current_pass = WIFI_PASS;

void setupWifi() {
  if (current_ssid.length() == 0) return;
  WiFi.mode(WIFI_STA);
  WiFi.begin(current_ssid.c_str(), current_pass.c_str());
  Serial.printf("[wifi] connecting to %s\n", current_ssid.c_str());
  int attempts = 0;
  while (WiFi.status() != WL_CONNECTED && attempts < 15) { 
    delay(400); 
    Serial.print("."); 
    attempts++;
  }
  if (WiFi.status() == WL_CONNECTED) {
    Serial.printf("\n[wifi] connected, ip=%s\n", WiFi.localIP().toString().c_str());
  } else {
    Serial.println("\n[wifi] failed to connect, falling back to local connection.");
  }
}''',
    content
)

if 'void mqttConnect_custom()' not in content:
    mqtt_func = r'''
unsigned long lastReconnectAttempt = 0;
void mqttConnect_custom() {
  if (WiFi.status() != WL_CONNECTED) return;
  Serial.print("[mqtt] connecting...");
  if (client.connect(CLIENT_ID, "econ-node", "<node password>", STATUS_TOPIC, 1, true, "offline")) {
    Serial.println(" connected");
    client.subscribe(COMMAND_TOPIC);
    client.subscribe(CONFIG_TOPIC);
    client.publish(STATUS_TOPIC, "online", true);
    digitalWrite(STATUS_LED, HIGH);
  } else {
    Serial.printf(" failed rc=%d\n", client.state());
    digitalWrite(STATUS_LED, LOW);
  }
}
'''
    content = content.replace('void setup() {', mqtt_func + '\nvoid setup() {')

# In setup():
setup_repl = r'''  setupWifi();
  client.setServer(MQTT_HOST, MQTT_PORT);
  client.setCallback(onMessage);'''
content = re.sub(r'// setupWifi\(\); // Vô hiệu hóa Wi-Fi theo yêu cầu dùng USB\s*// client\.setServer.*?;\s*// client\.setCallback.*?;', setup_repl, content)

# In loop():
loop_serial = r'''  while (Serial.available()) {
    String line = Serial.readStringUntil('\n');
    line.trim();
    if (line.startsWith("[wifi] connect ")) {
      int space = line.indexOf(' ', 15);
      if (space > 15) {
        current_ssid = line.substring(15, space);
        current_pass = line.substring(space + 1);
        Serial.printf("[wifi] received new credentials: %s\n", current_ssid.c_str());
        WiFi.disconnect();
        setupWifi();
      }
    } else if (line.startsWith("[mqtt] sub ")) {
      int arrowIdx = line.indexOf(" -> ");
      if (arrowIdx > 0) {
        String topicStr = line.substring(11, arrowIdx);
        String payloadStr = line.substring(arrowIdx + 4);
        onMessage((char*)topicStr.c_str(), (byte*)payloadStr.c_str(), payloadStr.length());
      }
    }
  }

  if (WiFi.status() == WL_CONNECTED) {
    if (!client.connected()) {
      digitalWrite(STATUS_LED, LOW);
      unsigned long now = millis();
      if (now - lastReconnectAttempt > 5000) {
        lastReconnectAttempt = now;
        mqttConnect_custom();
      }
    } else {
      client.loop();
    }
  }'''

content = re.sub(
    r'while \(Serial\.available\(\)\) \{[\s\S]*?// Bỏ qua MQTT loop nếu không dùng Wi-Fi\s*// if \(\!client\.loop\(\)\) \{',
    loop_serial,
    content
)

with open('edge/esp32/src/main.cpp', 'w') as f:
    f.write(content)
