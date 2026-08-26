#!/usr/bin/env python3
"""Send control commands to ECON edge devices over MQTT.

This follows the same wire contract used by the Go engine and ESP32 firmware:

  LIGHTS_ON;SETPOINT=22.0
  LIGHTS_OFF;SETPOINT=26.0
  LIGHTS_OFF;SETPOINT=26.0;PLUG_OFF

The firmware listens on:
  econ/commands/<zone>

Examples:
  python3 tools/device_controller.py --zone zone_1 --lights on --setpoint 22.0
  python3 tools/device_controller.py --zone zone_1 --lights off --setpoint 26.0 --plug off
  python3 tools/device_controller.py --zone zone_1 --raw "LIGHTS_ON;SETPOINT=21.5;PLUG_ON"
"""

from __future__ import annotations

import argparse
import sys
from typing import List


def build_payload(args: argparse.Namespace) -> str:
    tokens: List[str] = []

    if args.raw:
        return args.raw.strip()

    if args.lights is not None:
        tokens.append("LIGHTS_ON" if args.lights.lower() == "on" else "LIGHTS_OFF")

    if args.plug is not None:
        tokens.append("PLUG_ON" if args.plug.lower() == "on" else "PLUG_OFF")

    if args.setpoint is not None:
        tokens.append(f"SETPOINT={args.setpoint:.1f}")

    if not tokens:
        raise ValueError(
            "No command provided. Use --lights, --plug, --setpoint, or --raw."
        )

    return ";".join(tokens)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Publish device commands to the ECON MQTT command topic."
    )
    parser.add_argument("--broker", default="127.0.0.1", help="MQTT broker host")
    parser.add_argument("--port", type=int, default=1883, help="MQTT broker port")
    parser.add_argument("--zone", default="zone_1", help="Zone topic suffix, e.g. zone_1")
    parser.add_argument(
        "--lights",
        choices=["on", "off"],
        help="Switch lighting relay on or off",
    )
    parser.add_argument(
        "--plug",
        choices=["on", "off"],
        help="Switch non-critical plug circuit on or off",
    )
    parser.add_argument(
        "--setpoint",
        type=float,
        help="AC setpoint in Celsius, e.g. 22.0",
    )
    parser.add_argument(
        "--raw",
        help="Send a raw command string exactly as the firmware expects",
    )
    parser.add_argument(
        "--repeat",
        type=int,
        default=1,
        help="Repeat the same command N times",
    )
    parser.add_argument(
        "--sleep",
        type=float,
        default=0.5,
        help="Delay between repeated sends in seconds",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    try:
        import paho.mqtt.client as mqtt
    except ImportError as exc:  # pragma: no cover - runtime dependency error
        print(
            "Missing dependency: paho-mqtt. Install it with:\n"
            "  python3 -m pip install paho-mqtt\n",
            file=sys.stderr,
        )
        raise SystemExit(2) from exc

    try:
        payload = build_payload(args)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    topic = f"econ/commands/{args.zone}"
    client = mqtt.Client(client_id=f"econ-control-{args.zone}")

    try:
        client.connect(args.broker, args.port, 10)
    except Exception as exc:  # pragma: no cover - network dependent
        print(f"Failed to connect to MQTT broker at {args.broker}:{args.port}: {exc}", file=sys.stderr)
        return 1

    print(f"Publishing to {topic}: {payload}")
    for i in range(max(1, args.repeat)):
        result = client.publish(topic, payload, qos=0, retain=False)
        if result.rc != mqtt.MQTT_ERR_SUCCESS:
            print(f"MQTT publish failed with rc={result.rc}", file=sys.stderr)
            return 1
        if i + 1 < args.repeat:
            import time
            time.sleep(args.sleep)

    client.disconnect()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
