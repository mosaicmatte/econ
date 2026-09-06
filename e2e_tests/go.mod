module econ/e2e_tests

go 1.22.12

replace econ => ../server

require (
	econ v0.0.0
	github.com/google/flatbuffers v25.12.19+incompatible
	github.com/gorilla/websocket v1.5.3
	github.com/eclipse/paho.mqtt.golang v1.4.3
	github.com/lib/pq v1.12.3
)
