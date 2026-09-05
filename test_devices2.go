package main

import (
	"encoding/json"
	"fmt"
)

type fieldStat struct {
	Last    float64   `json:"last"`
	Count   int64     `json:"count"`
	Min     float64   `json:"min"`
	Max     float64   `json:"max"`
	Omitted int64     `json:"omitted"`
}

type device struct {
	ID        string    `json:"id"`
	Fields map[string]*fieldStat `json:"fields"`
}

func main() {
	d := &device{ID: "test", Fields: map[string]*fieldStat{}}
	
	track := func(name string, v *float64) {
		fs, ok := d.Fields[name]
		if !ok {
			fs = &fieldStat{Min: 1e18, Max: -1e18}
			d.Fields[name] = fs
		}
		if v == nil {
			fs.Omitted++
			return
		}
		fs.Last = *v
		fs.Count++
	}
	
	track("occupancy_2", nil)
	out, _ := json.MarshalIndent(d, "", "  ")
	fmt.Println(string(out))
}
