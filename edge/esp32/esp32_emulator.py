#!/usr/bin/env python3
"""
ECON ESP32 node emulator — a full software twin of the edge firmware.

Speaks the exact same wire contract as src/main.cpp, so the entire
hardware-in-the-loop path (zone auto-binding, /api/hardware, the dashboard's
LIVE HARDWARE badge, actuation commands, LWT status) can be exercised with no
board on the desk:

  * publishes telemetry  -> econ/telemetry/<zone-topic>   (every 2 s + on change)
  * retained liveness    -> econ/status/<zone-topic>      (online/offline + Last Will)
  * executes commands    <- econ/commands/<zone-topic>    (LIGHTS_x / SETPOINT= / HVAC_SET:)

Presence is interactive, mirroring the real firmware's touch pin: press ENTER in
this terminal to toggle occupied/vacant. Simulated temps are flagged
tempReal:false so they never pin the zone's physics (same as the firmware's
placeholder mode).

Run:  python3 esp32_emulator.py [--zone-topic zone_9] [--broker 127.0.0.1]
"""

import argparse
import json
import random
import threading
import time

import paho.mqtt.client as mqtt

parser = argparse.ArgumentParser(description="ECON ESP32 node emulator")
parser.add_argument("--zone-topic", default="zone_1", help="MQTT topic suffix (one per node)")
parser.add_argument("--zone-label", default="Level 4", help="human label sent as 'zone'")
parser.add_argument("--broker", default="127.0.0.1")
parser.add_argument("--port", type=int, default=1883)
parser.add_argument("--occupied", action="store_true", help="start in the occupied state")
args = parser.parse_args()

TELEMETRY_TOPIC = f"econ/telemetry/{args.zone_topic}"
COMMAND_TOPIC = f"econ/commands/{args.zone_topic}"
STATUS_TOPIC = f"econ/status/{args.zone_topic}"

state = {"occupied": args.occupied, "lights": True, "setpoint": 24.0}
state_lock = threading.Lock()


def handle_command(payload: str):
    # Same parser semantics as the firmware: ;-separated tokens, unknown ones ignored.
    for tok in payload.split(";"):
        tok = tok.strip()
        if tok == "LIGHTS_ON":
            with state_lock:
                state["lights"] = True
            print("   [HARDWARE] -> RELAY CLICK: Lights ON")
        elif tok == "LIGHTS_OFF":
            with state_lock:
                state["lights"] = False
            print("   [HARDWARE] -> RELAY CLICK: Lights OFF")
        elif tok.startswith("SETPOINT=") or tok.startswith("HVAC_SET:"):
            try:
                sp = float(tok.split("=" if "=" in tok else ":", 1)[1])
            except ValueError:
                continue
            with state_lock:
                state["setpoint"] = sp
            print(f"   [HARDWARE] -> HVAC IR BLAST: setpoint {sp:.1f} C")


def on_connect(client, *rest):
    print(f"[ESP32 EMULATOR] Connected to {args.broker}:{args.port} as '{args.zone_topic}'")
    client.publish(STATUS_TOPIC, "online", retain=True)
    client.subscribe(COMMAND_TOPIC)
    print(f"[ESP32 EMULATOR] Subscribed to {COMMAND_TOPIC}")
    print("[ESP32 EMULATOR] Press ENTER to toggle presence (like touching GPIO32)...")


def on_message(client, userdata, msg):
    payload = msg.payload.decode(errors="ignore")
    print(f"\n[ESP32 EMULATOR] Received on {msg.topic}: {payload}")
    handle_command(payload)


def telemetry() -> str:
    with state_lock:
        occ = 3 if state["occupied"] else 0
        lights = "ON" if state["lights"] else "OFF"
        sp = state["setpoint"]
    return json.dumps({
        "zone": args.zone_label,
        "occupancy": occ,
        "temperature": round(22.0 + random.random() * 4.0, 1),
        "humidity": round(40.0 + random.random() * 20.0, 1),
        "co2": 400 + occ * 120 + random.randint(0, 60),
        "source": "esp32",
        "tempReal": False,  # simulated placeholder — must never pin zone physics
        "lights": lights,
        "setpoint": sp,
    })


def presence_loop(client):
    # ENTER toggles presence, mirroring the firmware's instant publish-on-change.
    while True:
        try:
            input()
        except EOFError:
            return
        with state_lock:
            state["occupied"] = not state["occupied"]
            label = "OCCUPIED" if state["occupied"] else "VACANT"
        print(f"[ESP32 EMULATOR] Presence toggled -> {label}")
        client.publish(TELEMETRY_TOPIC, telemetry())


def main():
    cid = f"econ-esp32-emu-{args.zone_topic}"
    try:
        client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION1, client_id=cid)
    except AttributeError:
        client = mqtt.Client(client_id=cid)
    client.on_connect = on_connect
    client.on_message = on_message
    client.will_set(STATUS_TOPIC, "offline", retain=True)

    print("===========================================")
    print("   ECON ESP32 IoT Node Emulator")
    print("===========================================")
    while True:
        try:
            client.connect(args.broker, args.port, 60)
            break
        except Exception as e:
            print(f"Could not connect: {e} — is docker compose (mosquitto) up? Retrying in 3 s")
            time.sleep(3)

    threading.Thread(target=presence_loop, args=(client,), daemon=True).start()
    client.loop_start()
    try:
        while True:
            client.publish(TELEMETRY_TOPIC, telemetry())
            time.sleep(2)
    except KeyboardInterrupt:
        print("\n[ESP32 EMULATOR] Exiting...")
        try:
            client.publish(STATUS_TOPIC, "offline", retain=True).wait_for_publish(timeout=2)
        except Exception:
            pass
        client.loop_stop()
        client.disconnect()


if __name__ == "__main__":
    main()
