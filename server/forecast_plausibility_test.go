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
		wantUnjudged    bool // there was not enough evidence to reach a judgement
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
			wantUnjudged: true,
		},
		{
			name: "no forecast to judge",
			peak: nil, lo: 0.0099, hi: 0.0252, n: 512,
			wantUnjudged: true,
		},
		{
			name: "no observed range yet",
			peak: mw(2.39), lo: 0, hi: 0, n: 512,
			wantUnjudged: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := engineResult{PeakMw: c.peak}
			checkPlausible(&res, c.lo, c.hi, c.n)
			if c.wantUnjudged {
				if res.Implausible {
					t.Fatalf("nothing was judged, so nothing may be flagged: %q", res.Plausibility)
				}
				if res.Judged {
					t.Fatal("reported a judgement it did not have the evidence to make")
				}
				// A result that was never checked must SAY it was never checked. Silence
				// here is what let a 2.41 MW forecast be drawn against a 5.2 kW house as a
				// peak-shaving target: Implausible was false, so it read as "checked, fine".
				if c.peak != nil && res.Plausibility == "" {
					t.Error("declined to judge without saying so; a consumer cannot tell this " +
						"apart from a forecast that was checked and passed")
				}
				return
			}
			if !res.Judged {
				t.Error("reached a verdict but did not mark it as judged")
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

// A forecast the engine has already flagged as belonging to a different building is not
// one half of a model comparison. The compare endpoint used to publish "the two engines
// differ by 2.37 MW (99%)" for a house whose highest observed load is 0.025 MW, and the AI
// panel rendered that percentage as a headline directly beneath its own "not this building"
// badge. The difference there measures the gap between two buildings, not two models.
func TestAgreementExcludesAnOutOfDistributionForecast(t *testing.T) {
	mw := func(v float64) *float64 { return &v }

	lstm := engineResult{Available: true, PeakMw: mw(2.40)}
	tfm := engineResult{Available: true, PeakMw: mw(0.025)}
	// The same range check the endpoint applies: a house, 894 samples, 12-25 kW.
	checkPlausible(&lstm, 0.0123, 0.0252, 894)
	checkPlausible(&tfm, 0.0123, 0.0252, 894)

	if !lstm.Implausible {
		t.Fatal("2.40 MW against a 0.025 MW building should have been flagged")
	}
	if tfm.Implausible {
		t.Fatal("0.025 MW against a 0.025 MW building should not have been flagged")
	}

	a := forecastAgreement(lstm, tfm)
	if a["comparable"] != false {
		t.Error("an out-of-distribution forecast must not be reported as comparable")
	}
	if _, ok := a["relativeDiff"]; ok {
		t.Error("no disagreement statistic should be published from a flagged forecast")
	}
	excluded, _ := a["excluded"].([]string)
	if len(excluded) != 1 || excluded[0] != "lstm" {
		t.Errorf("the payload should name which engine was excluded, got %v", a["excluded"])
	}
}

// Two forecasts that both sit inside the building's own observed range are exactly what
// the comparison is for, and it must still describe them.
func TestAgreementStillComparesTwoPlausibleForecasts(t *testing.T) {
	mw := func(v float64) *float64 { return &v }

	lstm := engineResult{Available: true, PeakMw: mw(0.030)}
	tfm := engineResult{Available: true, PeakMw: mw(0.025)}
	checkPlausible(&lstm, 0.0123, 0.0252, 894)
	checkPlausible(&tfm, 0.0123, 0.0252, 894)

	a := forecastAgreement(lstm, tfm)
	if a["comparable"] != true {
		t.Fatalf("two in-range forecasts should be comparable, got %v", a)
	}
	if a["higher"] != "lstm" {
		t.Errorf("the higher engine should be named, got %v", a["higher"])
	}
}
