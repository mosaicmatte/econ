#include <Arduino.h>

const int PIR1_PIN = 14; 
const int PIR2_PIN = 15; 

const unsigned long DEAD_TIME = 1500; // (ms) Thoi gian khoa dem sau khi dem
const unsigned long HEARTBEAT_INTERVAL = 30000;
const unsigned long TELEMETRY_INTERVAL = 1000; // 1 giay mot lan

enum State { IDLE, COOLDOWN };

State state1 = IDLE;
State state2 = IDLE;

unsigned long timer1 = 0;
unsigned long timer2 = 0;
unsigned long last_heartbeat = 0;
unsigned long last_telemetry = 0;
bool need_telemetry = false;

int occupancy = 0;
int occupancy_2 = 0;

void sendTelemetry() {
  Serial.print("{\"_topic\": \"econ/telemetry/pico_1\", \"zone\": \"Pico Lab\", \"occupancy\": ");
  Serial.print(occupancy);
  Serial.print(", \"occupancy_2\": ");
  Serial.print(occupancy_2);
  Serial.println("}");
}

void setup() {
  Serial.begin(115200);
  pinMode(PIR1_PIN, INPUT_PULLDOWN);
  pinMode(PIR2_PIN, INPUT_PULLDOWN);

  delay(3000);
  Serial.println("[LOG] He thong Pico 2 Cam Bien Doc Lap Bat Dau!");
  sendTelemetry();
}

void loop() {
  unsigned long now = millis();
    
  bool s1 = (digitalRead(PIR1_PIN) == HIGH);
  bool s2 = (digitalRead(PIR2_PIN) == HIGH);
    
  // --- XU LY SENSOR 1 ---
  if (state1 == IDLE) {
      if (s1) {
          occupancy++;
          Serial.print("[LOG] >>> SENSOR 1 PHAT HIEN (occupancy: ");
          Serial.print(occupancy);
          Serial.println(")");
          need_telemetry = true;
          timer1 = now;
          state1 = COOLDOWN;
      }
  } else if (state1 == COOLDOWN) {
      if (s1) {
          timer1 = now; // Nguoi van con trong vung, reset timer
      } else if (now - timer1 >= DEAD_TIME) {
          state1 = IDLE; // Nguoi da di qua han
      }
  }

  // --- XU LY SENSOR 2 ---
  if (state2 == IDLE) {
      if (s2) {
          occupancy_2++;
          Serial.print("[LOG] >>> SENSOR 2 PHAT HIEN (occupancy_2: ");
          Serial.print(occupancy_2);
          Serial.println(")");
          need_telemetry = true;
          timer2 = now;
          state2 = COOLDOWN;
      }
  } else if (state2 == COOLDOWN) {
      if (s2) {
          timer2 = now; // Nguoi van con trong vung, reset timer
      } else if (now - timer2 >= DEAD_TIME) {
          state2 = IDLE; // Nguoi da di qua han
      }
  }

  // Cap nhat Dashboard:
  // Neu co su thay doi, gioi han gui len toi da 1 giay 1 lan.
  // Neu khong co su thay doi, heartbeat gui 30 giay 1 lan.
  if ((need_telemetry && now - last_telemetry >= TELEMETRY_INTERVAL) || (now - last_heartbeat >= HEARTBEAT_INTERVAL)) {
      sendTelemetry();
      last_telemetry = now;
      last_heartbeat = now;
      need_telemetry = false;
  }
}
