package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"

	"econ/simulation"
)

func TestHandleTelemetryFullJSONLogging(t *testing.T) {
	// Backup original logger output and flags
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	}()

	engine := simulation.NewEngine()

	tests := []struct {
		name         string
		topic        string
		payload      string
		wantSuffix   string
		wantOcc      string
		wantSrc      string
		wantRealTemp string
		wantZone     string
	}{
		{
			name:         "full esp32 telemetry",
			topic:        "econ/telemetry/zone_1",
			payload:      `{"zone":"Level 4","occupancy":3,"temperature":24.5,"humidity":55,"co2":600,"source":"esp32","tempReal":true}`,
			wantSuffix:   "zone_1",
			wantOcc:      "occ=3",
			wantSrc:      `src="esp32"`,
			wantRealTemp: "real_temp=true",
			wantZone:     `zone="Level 4"`,
		},
		{
			name:         "cv occupancy only",
			topic:        "econ/telemetry/zone_2",
			payload:      `{"zone":"Zone 2","occupancy":5,"source":"cv"}`,
			wantSuffix:   "zone_2",
			wantOcc:      "occ=5",
			wantSrc:      `src="cv"`,
			wantRealTemp: "real_temp=false",
			wantZone:     `zone="Zone 2"`,
		},
		{
			name:         "vacant zone without explicit zone name",
			topic:        "econ/telemetry/zone_office_a",
			payload:      `{"occupancy":0,"temperature":22.0,"source":"pico","tempReal":true}`,
			wantSuffix:   "zone_office_a",
			wantOcc:      "occ=0",
			wantSrc:      `src="pico"`,
			wantRealTemp: "real_temp=true",
			wantZone:     `zone=""`,
		},
		{
			name:         "rich sensor payload with power measurements",
			topic:        "econ/telemetry/zone_server",
			payload:      `{"zone":"Server Room","occupancy":1,"temperature":19.5,"humidity":42.0,"co2":480,"plugW":1250.5,"supplyC":14.2,"acW":3400.0,"lux":350.0,"source":"esp32","tempReal":true,"acReal":true,"cfgRev":2}`,
			wantSuffix:   "zone_server",
			wantOcc:      "occ=1",
			wantSrc:      `src="esp32"`,
			wantRealTemp: "real_temp=true",
			wantZone:     `zone="Server Room"`,
		},
		{
			name:         "simulated placeholder tempReal false",
			topic:        "econ/telemetry/zone_placeholder",
			payload:      `{"zone":"Demo Zone","occupancy":2,"temperature":23.0,"source":"esp32","tempReal":false}`,
			wantSuffix:   "zone_placeholder",
			wantOcc:      "occ=2",
			wantSrc:      `src="esp32"`,
			wantRealTemp: "real_temp=false",
			wantZone:     `zone="Demo Zone"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logBuf.Reset()
			handleTelemetry(engine, tc.topic, []byte(tc.payload))
			logOutput := strings.TrimSpace(logBuf.String())

			// 1. Verify log begins with [mqtt] telemetry <suffix>
			expectedPrefix := "[mqtt] telemetry " + tc.wantSuffix
			if !strings.Contains(logOutput, expectedPrefix) {
				t.Errorf("log output missing prefix %q\ngot: %s", expectedPrefix, logOutput)
			}

			// 2. Verify log contains the exact full JSON payload: payload=<payload>
			expectedPayloadStr := "payload=" + tc.payload
			if !strings.Contains(logOutput, expectedPayloadStr) {
				t.Errorf("log output missing full JSON payload %q\ngot: %s", expectedPayloadStr, logOutput)
			}

			// 3. Verify other expected fields
			if !strings.Contains(logOutput, tc.wantOcc) {
				t.Errorf("log output missing occupancy %q\ngot: %s", tc.wantOcc, logOutput)
			}
			if !strings.Contains(logOutput, tc.wantSrc) {
				t.Errorf("log output missing source %q\ngot: %s", tc.wantSrc, logOutput)
			}
			if !strings.Contains(logOutput, tc.wantRealTemp) {
				t.Errorf("log output missing real_temp %q\ngot: %s", tc.wantRealTemp, logOutput)
			}
			if !strings.Contains(logOutput, tc.wantZone) {
				t.Errorf("log output missing zone %q\ngot: %s", tc.wantZone, logOutput)
			}

			// 4. Verify valid JSON parseability of payload substring in log
			payloadStart := strings.Index(logOutput, "payload=")
			if payloadStart == -1 {
				t.Fatalf("payload= not found in log: %s", logOutput)
			}
			payloadSub := logOutput[payloadStart+len("payload="):]
			// The payload string ends before " occ="
			occIndex := strings.Index(payloadSub, " occ=")
			if occIndex == -1 {
				t.Fatalf("occ= not found after payload in log: %s", logOutput)
			}
			rawExtractedPayload := payloadSub[:occIndex]
			if rawExtractedPayload != tc.payload {
				t.Errorf("extracted payload in log does not match raw payload:\ngot:  %s\nwant: %s",
					rawExtractedPayload, tc.payload)
			}
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(rawExtractedPayload), &parsed); err != nil {
				t.Errorf("extracted payload in log is not valid JSON: %v", err)
			}
		})
	}
}

func TestHandleTelemetryMalformed(t *testing.T) {
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	}()

	engine := simulation.NewEngine()
	malformed := "not a json string"
	handleTelemetry(engine, "econ/telemetry/zone_broken", []byte(malformed))
	logOutput := logBuf.String()

	if !strings.Contains(logOutput, "[mqtt] bad telemetry payload on econ/telemetry/zone_broken") {
		t.Errorf("expected malformed log error, got: %s", logOutput)
	}
}

func TestHandleStatus(t *testing.T) {
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	}()

	engine := simulation.NewEngine()

	handleStatus(engine, "econ/status/zone_1", []byte("online"))
	if !strings.Contains(logBuf.String(), "[mqtt] status zone_1 -> online") {
		t.Errorf("expected status online log, got: %s", logBuf.String())
	}

	logBuf.Reset()
	handleStatus(engine, "econ/status/zone_1", []byte("offline"))
	if !strings.Contains(logBuf.String(), "[mqtt] status zone_1 -> offline") {
		t.Errorf("expected status offline log, got: %s", logBuf.String())
	}
}

func TestTopicSuffix(t *testing.T) {
	cases := []struct {
		topic string
		want  string
	}{
		{"econ/telemetry/zone_1", "zone_1"},
		{"econ/status/gateway", "gateway"},
		{"bare_topic", "bare_topic"},
		{"a/b/c/d", "d"},
	}
	for _, c := range cases {
		got := topicSuffix(c.topic)
		if got != c.want {
			t.Errorf("topicSuffix(%q) = %q, want %q", c.topic, got, c.want)
		}
	}
}

func TestDebugLogger(t *testing.T) {
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("DEBUG")
	}()

	os.Setenv("LOG_LEVEL", "DEBUG")
	logBuf.Reset()
	debugLog("test debug message with param %d", 42)
	if !strings.Contains(logBuf.String(), "[debug] test debug message with param 42") {
		t.Errorf("expected debug log output when LOG_LEVEL=DEBUG, got: %s", logBuf.String())
	}

	os.Setenv("LOG_LEVEL", "INFO")
	os.Setenv("DEBUG", "0")
	logBuf.Reset()
	debugLog("this should not be logged")
	if strings.Contains(logBuf.String(), "this should not be logged") {
		t.Errorf("expected no debug log output when LOG_LEVEL=INFO, got: %s", logBuf.String())
	}
}
