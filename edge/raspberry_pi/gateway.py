#!/usr/bin/env python3
"""
ECON Raspberry Pi edge gateway — autonomous local failsafe.

Role (see econ/BACKEND_ARCHITECTURE.md): the Pi hosts the Mosquitto broker for the facility
and runs THIS resilient rules engine. The Go engine (the cloud "brain") does the optimization;
this gateway only steps in when the brain is unreachable, so vacant zones are never left lit or
cooling. It DEFERS to the engine whenever the engine is alive — no duplicate commands.

Behaviour:
  * Subscribes econ/telemetry/+ (occupancy from CV/ESP32) and econ/commands/+ (to sense the
    engine's liveness — every command the engine publishes is a heartbeat for that zone).
  * Per zone tracks: occupancy, vacant_since, last_engine_cmd_ts.
  * Every tick: if the engine looks OFFLINE (no engine command for ENGINE_TIMEOUT_S) AND a zone
    has been vacant longer than FAILSAFE_DELAY_S, it publishes a failsafe setback to
    econ/commands/<zone>, tagged ";SRC=FAILSAFE" so (a) it never mistakes its own command for
    the engine and (b) the ESP32 firmware ignores the tag while still acting on LIGHTS_OFF.

Config via env: MQTT_BROKER, MQTT_PORT, ENGINE_TIMEOUT_S, FAILSAFE_DELAY_S, SETBACK_C.
Run:  pip install -r requirements.txt  &&  python gateway.py
"""

import json
import logging
import os
import threading
import time

import paho.mqtt.client as mqtt

MQTT_BROKER      = os.getenv("MQTT_BROKER", "127.0.0.1")
MQTT_PORT        = int(os.getenv("MQTT_PORT", "1883"))
ENGINE_TIMEOUT_S = float(os.getenv("ENGINE_TIMEOUT_S", "10"))   # engine = offline after this gap
FAILSAFE_DELAY_S = float(os.getenv("FAILSAFE_DELAY_S", "30"))   # vacancy delay before a local cut
SETBACK_C        = float(os.getenv("SETBACK_C", "28"))
TICK_S           = 2.0

TELEMETRY_SUB = "econ/telemetry/+"
COMMAND_SUB   = "econ/commands/+"
FAILSAFE_TAG  = ";SRC=FAILSAFE"

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("econ-gateway")

zones = {}            # suffix -> state dict
lock = threading.Lock()


def _suffix(topic: str) -> str:
    return topic.rsplit("/", 1)[-1]


def _zone(z: str) -> dict:
    return zones.setdefault(z, {"occupancy": 1, "vacant_since": None,
                                "last_engine_cmd": 0.0, "failsafe_active": False})


def on_connect(client, userdata, flags, rc):
    log.info("connected to broker %s:%s (rc=%s)", MQTT_BROKER, MQTT_PORT, rc)
    client.subscribe([(TELEMETRY_SUB, 0), (COMMAND_SUB, 0)])
    client.publish("econ/status/gateway", "online", retain=True)


def on_message(client, userdata, msg):
    topic = msg.topic
    payload = msg.payload.decode(errors="ignore")
    now = time.time()

    if topic.startswith("econ/telemetry/"):
        try:
            occ = int(json.loads(payload).get("occupancy", 0))
        except Exception:
            return
        with lock:
            st = _zone(_suffix(topic))
            st["occupancy"] = occ
            if occ <= 0:
                if st["vacant_since"] is None:
                    st["vacant_since"] = now
            else:
                st["vacant_since"] = None
                st["failsafe_active"] = False  # reoccupied -> allow engine/normal control again

    elif topic.startswith("econ/commands/"):
        if FAILSAFE_TAG in payload:
            return  # our own failsafe command — not an engine heartbeat
        with lock:
            _zone(_suffix(topic))["last_engine_cmd"] = now  # engine is alive for this zone


def failsafe_loop(client):
    while True:
        time.sleep(TICK_S)
        now = time.time()
        with lock:
            for z, st in zones.items():
                engine_alive = (now - st["last_engine_cmd"]) < ENGINE_TIMEOUT_S
                vacant_for = (now - st["vacant_since"]) if st["vacant_since"] else 0
                if (not engine_alive) and vacant_for > FAILSAFE_DELAY_S and not st["failsafe_active"]:
                    cmd = f"LIGHTS_OFF;SETPOINT={SETBACK_C:.1f}{FAILSAFE_TAG}"
                    client.publish(f"econ/commands/{z}", cmd)
                    st["failsafe_active"] = True
                    log.warning("FAILSAFE engaged for %s (engine offline, vacant %.0fs) -> %s",
                                z, vacant_for, cmd)


def health_loop():
    while True:
        time.sleep(15)
        now = time.time()
        with lock:
            for z, st in zones.items():
                engine = "UP" if (now - st["last_engine_cmd"]) < ENGINE_TIMEOUT_S else "DOWN"
                log.info("zone=%s occ=%s engine=%s failsafe=%s",
                         z, st["occupancy"], engine, st["failsafe_active"])


def main():
    client = mqtt.Client(client_id="econ-rpi-gateway")
    client.on_connect = on_connect
    client.on_message = on_message
    client.will_set("econ/status/gateway", "offline", retain=True)

    while True:
        try:
            client.connect(MQTT_BROKER, MQTT_PORT, 60)
            break
        except Exception as e:
            log.error("broker connect failed (%s); retry in 5s", e)
            time.sleep(5)

    threading.Thread(target=failsafe_loop, args=(client,), daemon=True).start()
    threading.Thread(target=health_loop, daemon=True).start()
    log.info("ECON gateway running (failsafe delay=%ss, engine timeout=%ss)",
             FAILSAFE_DELAY_S, ENGINE_TIMEOUT_S)
    client.loop_forever()  # paho handles auto-reconnect


if __name__ == "__main__":
    main()
