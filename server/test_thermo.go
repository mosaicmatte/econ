package main

import "fmt"

func main() {
	zTemp := 22.0
	zWallTemp := 22.0
	RIn := 0.05
	ROut := 0.25
	CAir := 2538624.0
	CWall := 4000000.0
	BaseHeatGain := 85000.0
	tOutside := 30.0
	sp := 22.0
	flowRatio := 0.258

	qSteadyStateWall := (tOutside - sp) / (RIn + ROut)
	qNominalTotal := BaseHeatGain + qSteadyStateWall

	for i := 0; i < 90; i++ { // 3 seconds at 30 fps
		qInternal := BaseHeatGain * 5.0
		qCooling := flowRatio * qNominalTotal * ((zTemp - 12.0) / (sp - 12.0))
		if qCooling < 0 { qCooling = 0 }

		dTAirDt := ((zWallTemp-zTemp)/(RIn) + (qInternal-qCooling)) / CAir
		dTWallDt := ((tOutside-zWallTemp)/(ROut) - (zWallTemp-zTemp)/(RIn)) / CWall

		zTemp += dTAirDt * 0.3
		zWallTemp += dTWallDt * 0.3
	}
	fmt.Printf("Temp after 3 seconds (sim 90 ticks): %f\n", zTemp)
}
