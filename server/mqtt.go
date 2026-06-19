package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"econ/simulation"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// telemetryMsg is the payload the CV/edge nodes publish (matches yolo_tracker.py and
// the ESP32 firmware): econ/telemetry/<zone> -> {"zone":"...","occupancy":N,...}
type telemetryMsg struct {
	Zone      string `json:"zone"`
	Occupancy int    `json:"occupancy"`
}

// startMQTT connects the Go engine to the MQTT broker so it (a) ingests real occupancy
// from the CV/edge layer and (b) publishes actuation commands back to the ESP32. If no
// broker is reachable the simulation keeps running normally (occupancy stays simulated).
func startMQTT(engine *simulation.Engine) {
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		broker = "tcp://localhost:1883"
	}

	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID("econ-go-engine").
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second)

	opts.OnConnect = func(c mqtt.Client) {
		log.Printf("[mqtt] connected to %s", broker)
		if token := c.Subscribe("econ/telemetry/+", 0, func(_ mqtt.Client, m mqtt.Message) {
			handleTelemetry(engine, m.Topic(), m.Payload())
		}); token.Wait() && token.Error() != nil {
			log.Printf("[mqtt] subscribe error: %v", token.Error())
		} else {
			log.Printf("[mqtt] subscribed to econ/telemetry/+")
		}
	}
	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		log.Printf("[mqtt] connection lost: %v", err)
	}

	client := mqtt.NewClient(opts)

	// Wire the engine's actuation publisher to this MQTT client.
	engine.Publish = func(topic, payload string) {
		client.Publish(topic, 0, false, payload)
	}

	// Connect in the background so a missing broker never blocks the telemetry server.
	go func() {
		if token := client.Connect(); token.Wait() && token.Error() != nil {
			log.Printf("[mqtt] initial connect failed (will retry): %v", token.Error())
		}
	}()
}

func handleTelemetry(engine *simulation.Engine, topic string, payload []byte) {
	var msg telemetryMsg
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Printf("[mqtt] bad telemetry payload on %s: %v", topic, err)
		return
	}
	suffix := topic
	if i := strings.LastIndex(topic, "/"); i >= 0 {
		suffix = topic[i+1:]
	}
	// Prefer an explicit zone id/name in the payload; fall back to the topic suffix.
	ref := msg.Zone
	if ref == "" {
		ref = suffix
	}
	engine.SetZoneOccupancy(ref, suffix, msg.Occupancy)
	log.Printf("[mqtt] telemetry %s occupancy=%d (zone=%q)", suffix, msg.Occupancy, msg.Zone)
}
