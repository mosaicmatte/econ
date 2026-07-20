package simulation

import (
	"testing"
	"time"
)

// A fresh boot has one seeded sample; the window must left-pad with it and say so.
func TestForecastWindowWarmup(t *testing.T) {
	e := newTestEngine()
	for _, z := range e.Zones {
		z.Temp = 26.0
	}

	e.mu.Lock()
	e.sampleHistory(time.Now())
	e.mu.Unlock()

	window, real := e.ForecastWindow(12)
	if real != 1 {
		t.Fatalf("one sample taken, got real=%d", real)
	}
	if len(window) != 12 {
		t.Fatalf("window must be padded to full length, got %d", len(window))
	}
	for i, row := range window {
		if len(row) != 2 {
			t.Fatalf("row %d must be [temp, flow], got %v", i, row)
		}
		if row[0] != window[0][0] {
			t.Fatalf("warm-up padding must replicate the oldest real sample, row %d differs", i)
		}
	}
}

// Samples respect the cadence: two calls inside one interval yield one sample; the
// buffer orders oldest→newest and caps at histKeep.
func TestForecastWindowCadenceAndOrder(t *testing.T) {
	e := newTestEngine()
	now := time.Now()

	e.mu.Lock()
	for i := 0; i < 20; i++ {
		for _, z := range e.Zones {
			z.Temp = 20.0 + float64(i)
		}
		e.sampleHistory(now.Add(time.Duration(i) * histInterval))
		// A second call inside the same interval must be a no-op.
		e.sampleHistory(now.Add(time.Duration(i)*histInterval + time.Second))
	}
	e.mu.Unlock()

	window, real := e.ForecastWindow(12)
	if real != 12 {
		t.Fatalf("buffer must cap at %d real samples, got %d", histKeep, real)
	}
	// 20 samples taken at temps 20..39; the last 12 are 28..39, oldest first.
	if window[0][0] != 28.0 || window[11][0] != 39.0 {
		t.Fatalf("window must be the most recent samples oldest-first, got first=%v last=%v",
			window[0][0], window[11][0])
	}
}

// The engine's live outdoor conditions hand over to the forecaster only while fresh
// AND complete — a temperature without humidity is not a usable training-shaped pair.
func TestOutdoorForForecast(t *testing.T) {
	e := newTestEngine()

	if _, _, live := e.OutdoorForForecast(); live {
		t.Fatal("no weather ever fetched: must not be live")
	}

	e.SetOutdoor(31.5, 74.0)
	tc, h, live := e.OutdoorForForecast()
	if !live || tc != 31.5 || h != 74.0 {
		t.Fatalf("fresh full reading must hand over, got %v %v live=%v", tc, h, live)
	}

	e.SetOutdoor(31.5, 0) // humidity missing from the fetch
	if _, _, live := e.OutdoorForForecast(); live {
		t.Fatal("temperature without humidity must not be handed to the forecaster")
	}
}
