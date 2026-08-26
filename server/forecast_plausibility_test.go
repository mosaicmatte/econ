package main

import "testing"

// The LSTM is supervised: its weights encode the building it was trained on. Pointed at a
// different building it still answers, confidently, with that other building's numbers —
// 2.4 MW for a house that has never drawn more than 0.03 MW. Nothing in the arithmetic is
// wrong, so nothing downstream can catch it except a comparison against what this building
// has actually been observed doing.
func TestForecastPlausibility(t *testing.T) {
	mw := func(v float64) *float64 { return &v }

	cases := []struct {
		name            string
		peak            *float64
		lo, hi          float64
		n               int
		wantImplausible bool
		wantSilent      bool // no judgement offered at all
	}{
		{
			name: "office-trained model pointed at a house",
			peak: mw(2.39), lo: 0.0099, hi: 0.0252, n: 512,
			wantImplausible: true,
		},
		{
			name: "zero-shot model on the same house",
			peak: mw(0.127), lo: 0.0099, hi: 0.0252, n: 512,
			// 0.127 is 5x the observed max: above the 4x factor, so it IS flagged. That is
			// the intended sensitivity — a 5x jump deserves a caveat even from a good model.
			wantImplausible: true,
		},
		{
			name: "forecast of a modest new high",
			peak: mw(0.030), lo: 0.0099, hi: 0.0252, n: 512,
			wantImplausible: false,
		},
		{
			name: "forecast well inside the observed range",
			peak: mw(1.2), lo: 0.5, hi: 1.6, n: 512,
			wantImplausible: false,
		},
		{
			name: "absurdly low forecast",
			peak: mw(0.001), lo: 0.5, hi: 1.6, n: 512,
			wantImplausible: true,
		},
		{
			name: "too little history to judge",
			peak: mw(2.39), lo: 0.0099, hi: 0.0252, n: 4,
			wantSilent: true,
		},
		{
			name: "no forecast to judge",
			peak: nil, lo: 0.0099, hi: 0.0252, n: 512,
			wantSilent: true,
		},
		{
			name: "no observed range yet",
			peak: mw(2.39), lo: 0, hi: 0, n: 512,
			wantSilent: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := engineResult{PeakMw: c.peak}
			checkPlausible(&res, c.lo, c.hi, c.n)
			if c.wantSilent {
				if res.Implausible || res.Plausibility != "" {
					t.Fatalf("expected no judgement, got implausible=%v %q", res.Implausible, res.Plausibility)
				}
				return
			}
			if res.Implausible != c.wantImplausible {
				t.Errorf("implausible = %v, want %v (%q)", res.Implausible, c.wantImplausible, res.Plausibility)
			}
			if res.Plausibility == "" {
				t.Error("a judgement was reached but nothing explains it to the operator")
			}
		})
	}
}
