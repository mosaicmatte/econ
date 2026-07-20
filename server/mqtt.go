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

// telemetryMsg is the payload the edge nodes publish (yolo_tracker.py, the ESP32
// firmware, and the Pico node/bridge): econ/telemetry/<zone> ->
//
//	{"zone":"...","occupancy":N,"temperature":24.9,"humidity":51,"co2":520,
//	 "source":"esp32","tempReal":true}
//
// Pointer fields distinguish "not reported" from a real zero. tempReal marks a
// genuinely measured temperature (DHT22 / RP2040 sensor); firmware running with
// simulated placeholder sensors leaves it false so fake temps never pin the physics.
type telemetryMsg struct {
	Zone        string   `json:"zone"`
	Occupancy   *int     `json:"occupancy"`
	Temperature *float64 `json:"temperature"`
	Humidity    *float64 `json:"humidity"`
	Co2         *float64 `json:"co2"`
	PlugW       *float64 `json:"plugW"` // measured plug-circuit watts (SCT-013 clamp)
	Source      string   `json:"source"`
	TempReal    bool     `json:"tempReal"`
}

// startMQTT connects the Go engine to the MQTT broker so it (a) ingests real telemetry
// from the CV/edge layer (occupancy, and measured temperature/humidity/CO2 from
// physical ESP32/Pico nodes), (b) tracks node liveness via the econ/status LWT topic,
// and (c) publishes actuation commands back to the edge. If no broker is reachable the
// simulation keeps running normally (all zones stay simulated).
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
		if token := c.Subscribe("econ/status/+", 0, func(_ mqtt.Client, m mqtt.Message) {
			handleStatus(engine, m.Topic(), m.Payload())
		}); token.Wait() && token.Error() != nil {
			log.Printf("[mqtt] status subscribe error: %v", token.Error())
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
	suffix := topicSuffix(topic)
	// Prefer an explicit zone id/name in the payload; fall back to the topic suffix.
	ref := msg.Zone
	if ref == "" {
		ref = suffix
	}
	engine.IngestTelemetry(ref, suffix, simulation.Measurement{
		Occupancy: msg.Occupancy,
		Temp:      msg.Temperature,
		Humidity:  msg.Humidity,
		Co2:       msg.Co2,
		PlugW:     msg.PlugW,
		Source:    msg.Source,
		TempReal:  msg.TempReal,
	})
	occ := -1
	if msg.Occupancy != nil {
		occ = *msg.Occupancy
	}
	log.Printf("[mqtt] telemetry %s occ=%d src=%q real_temp=%v (zone=%q)",
		suffix, occ, msg.Source, msg.TempReal && msg.Temperature != nil, msg.Zone)
}

// handleStatus ingests the retained online/offline flags the nodes (and the broker's
// Last Will on their behalf) publish to econ/status/<topic>. Non-zone statuses (e.g.
// the failsafe gateway's econ/status/gateway) simply match no zone and are ignored.
func handleStatus(engine *simulation.Engine, topic string, payload []byte) {
	suffix := topicSuffix(topic)
	online := string(payload) == "online"
	engine.SetNodeStatus(suffix, online)
	log.Printf("[mqtt] status %s -> %s", suffix, string(payload))
}

func topicSuffix(topic string) string {
	if i := strings.LastIndex(topic, "/"); i >= 0 {
		return topic[i+1:]
	}
	return topic
}
