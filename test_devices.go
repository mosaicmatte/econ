package main

import (
	"encoding/json"
	"fmt"
	"log"
)

type telemetryMsg struct {
	Zone        string   `json:"zone"`
	Occupancy   *int     `json:"occupancy"`
	Occupancy2  *int     `json:"occupancy_2"`
}

func main() {
	payload := []byte(`{"zone": "Pico Lab", "occupancy": 1, "occupancy_2": 2, "source": "pico"}`)
	var msg telemetryMsg
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("msg.Occupancy2 is %v\n", msg.Occupancy2)
	if msg.Occupancy2 != nil {
		fmt.Printf("Value is %d\n", *msg.Occupancy2)
	}
}
