#include <Arduino.h>

const int RADAR_OUT_PIN = 14; 
const int RADAR_IN_PIN  = 15; 

volatile unsigned long time_out_triggered = 0;
volatile unsigned long time_in_triggered = 0;
unsigned long last_count_time = 0;

const unsigned long COOLDOWN_MS = 2500;   
const unsigned long MAX_PASS_TIME = 3000; 

int occupancy = 0;

void isr_out() {
  if (time_out_triggered == 0) {
    time_out_triggered = millis();
  }
}

void isr_in() {
  if (time_in_triggered == 0) {
    time_in_triggered = millis();
  }
}

void sendTelemetry() {
  Serial.print("{\"occupancy\": ");
  Serial.print(occupancy);
  Serial.println("}");
}

void setup() {
  Serial.begin(115200);
  pinMode(RADAR_OUT_PIN, INPUT);
  pinMode(RADAR_IN_PIN, INPUT);

  attachInterrupt(digitalPinToInterrupt(RADAR_OUT_PIN), isr_out, RISING);
  attachInterrupt(digitalPinToInterrupt(RADAR_IN_PIN), isr_in, RISING);

  delay(3000);
  Serial.println("[LOG] He thong Pico Radar Dem Nguoi Bat Dau!");
  sendTelemetry();
}

void loop() {
  unsigned long now = millis();

  if (now - last_count_time < COOLDOWN_MS) {
    time_out_triggered = 0;
    time_in_triggered = 0;
    return;
  }

  if (time_out_triggered > 0 && time_in_triggered > 0) {
    long diff = time_in_triggered - time_out_triggered;

    if (abs(diff) < MAX_PASS_TIME) {
      if (diff > 0) {
        occupancy++;
        Serial.println("[LOG] >>> CO NGUOI DI VAO");
      } else {
        occupancy--;
        if (occupancy < 0) occupancy = 0; 
        Serial.println("[LOG] <<< CO NGUOI DI RA");
      }
      sendTelemetry(); 
      last_count_time = now; 
    }
    time_out_triggered = 0;
    time_in_triggered = 0;
  }

  if (time_out_triggered > 0 && (now - time_out_triggered > MAX_PASS_TIME)) {
    time_out_triggered = 0;
  }
  if (time_in_triggered > 0 && (now - time_in_triggered > MAX_PASS_TIME)) {
    time_in_triggered = 0;
  }
}
