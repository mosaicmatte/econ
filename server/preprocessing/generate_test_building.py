#!/usr/bin/env python3
"""
Comprehensive test-building generator for ECON.

Emits a diverse, realistic 14-floor building in the exact schema the Go engine + React twin
consume (building-data.json) PLUS the matching Brick ontology (brick-ontology.json) that drives
the React-Flow systems map. Designed to exercise the WHOLE system: every zone archetype, multiple
server rooms (fault targets) on different floors, perimeter solar gain, and a full HVAC/electrical/
sensor topology.

  python generate_test_building.py            # writes ../data/building-data.json + brick-ontology.json
"""

import json
import os
import uuid

FW, FD = 60.0, 40.0          # footprint metres (X x Y)
FLOOR_HEIGHT = 4.0
WALL_THICKNESS = 0.3
AIR_VOL_HEAT_CAP = 1210.0    # cAir = area * height * this

# 9-slot layout: 8 perimeter rooms ring a central core.
CORE = [[20, 15], [40, 15], [40, 25], [20, 25]]
SLOTS = {
    "NW":   [[0, 0], [20, 0], [20, 15], [0, 15]],
    "N":    [[20, 0], [40, 0], [40, 15], [20, 15]],
    "NE":   [[40, 0], [60, 0], [60, 15], [40, 15]],
    "W":    [[0, 15], [20, 15], [20, 25], [0, 25]],
    "E":    [[40, 15], [60, 15], [60, 25], [40, 25]],
    "SW":   [[0, 25], [20, 25], [20, 40], [0, 40]],
    "S":    [[20, 25], [40, 25], [40, 40], [20, 40]],
    "SE":   [[40, 25], [60, 25], [60, 40], [40, 40]],
    "CORE": CORE,
}

# Archetype thermal properties (cAir computed per room from area).
ARCH = {
    "office":      {"setpoint": 22.0, "deadband": 2.0, "baseHeatLoad": 8000,  "occ": 35},
    "conference":  {"setpoint": 22.0, "deadband": 2.0, "baseHeatLoad": 6000,  "occ": 16},
    "corridor":    {"setpoint": 24.0, "deadband": 3.0, "baseHeatLoad": 2000,  "occ": 4},
    "lobby":       {"setpoint": 24.0, "deadband": 2.0, "baseHeatLoad": 5000,  "occ": 40},
    "retail":      {"setpoint": 23.0, "deadband": 2.0, "baseHeatLoad": 7000,  "occ": 20},
    "server-room": {"setpoint": 18.0, "deadband": 1.0, "baseHeatLoad": 85000, "occ": 1},
    "mechanical":  {"setpoint": 26.0, "deadband": 3.0, "baseHeatLoad": 12000, "occ": 1},
}

# Floor archetypes: slot -> (zoneType, name)
FLOORS = {
    "lobby":      {"NW": ("retail", "Retail East"), "N": ("lobby", "Reception"), "NE": ("retail", "Retail West"),
                   "W": ("office", "Leasing Office"), "E": ("office", "Security Office"),
                   "SW": ("lobby", "Main Lobby"), "S": ("lobby", "Atrium"), "SE": ("conference", "Visitor Lounge"),
                   "CORE": ("corridor", "Core Elevator Lobby")},
    "office":     {"NW": ("office", "North-West Office"), "N": ("office", "North Perimeter"), "NE": ("office", "North-East Office"),
                   "W": ("conference", "West Conference"), "E": ("conference", "East Conference"),
                   "SW": ("office", "South-West Office"), "S": ("office", "South Perimeter"), "SE": ("office", "Open Office"),
                   "CORE": ("corridor", "Core Elevator Lobby")},
    "server":     {"NW": ("office", "NOC Office"), "N": ("server-room", "Server Room A"), "NE": ("mechanical", "UPS Room"),
                   "W": ("server-room", "Cold Aisle"), "E": ("server-room", "Server Room B"),
                   "SW": ("mechanical", "PDU Room"), "S": ("server-room", "Server Room C"), "SE": ("office", "Operations"),
                   "CORE": ("corridor", "Core Elevator Lobby")},
    "executive":  {"NW": ("office", "Executive Office 1"), "N": ("conference", "Boardroom"), "NE": ("office", "Executive Office 2"),
                   "W": ("office", "Executive Office 3"), "E": ("office", "Executive Office 4"),
                   "SW": ("conference", "Strategy Room"), "S": ("lobby", "Executive Lounge"), "SE": ("office", "Assistant Pool"),
                   "CORE": ("corridor", "Core Elevator Lobby")},
    "mechanical": {"NW": ("mechanical", "AHU Room"), "N": ("mechanical", "Chiller Plant"), "NE": ("mechanical", "Cooling Tower"),
                   "W": ("mechanical", "Pump Room"), "E": ("mechanical", "Electrical Switchgear"),
                   "SW": ("mechanical", "Fan Room"), "S": ("mechanical", "Heat Exchanger"), "SE": ("office", "BMS Office"),
                   "CORE": ("corridor", "Core Elevator Lobby")},
}

# Per-level floor-archetype assignment (diverse + multiple server floors for faults).
LEVEL_TYPE = {1: "lobby", 6: "server", 9: "server", 11: "executive", 14: "mechanical"}
N_FLOORS = 14


def slug(s):
    return s.lower().replace(" ", "-").replace("/", "")


def centroid(poly):
    xs = [p[0] for p in poly]; ys = [p[1] for p in poly]
    return {"x": round(sum(xs) / len(xs), 2), "y": round(sum(ys) / len(ys), 2)}


def touches_perimeter(poly):
    xs = [p[0] for p in poly]; ys = [p[1] for p in poly]
    return min(xs) <= 0.5 or min(ys) <= 0.5 or max(xs) >= FW - 0.5 or max(ys) >= FD - 0.5


def area(poly):
    xs = [p[0] for p in poly]; ys = [p[1] for p in poly]
    return (max(xs) - min(xs)) * (max(ys) - min(ys))


def build():
    building = {"buildingId": "bldg-econ-testbed", "floors": []}
    onto_nodes = [{"id": "ahu-main", "type": "brick:AHU", "label": "Main Rooftop AHU"}]
    onto_rels = []

    for lvl in range(1, N_FLOORS + 1):
        ftype = LEVEL_TYPE.get(lvl, "office")
        layout = FLOORS[ftype]
        floor = {
            "level": lvl, "elevation": round((lvl - 1) * FLOOR_HEIGHT, 2),
            "height": FLOOR_HEIGHT, "name": f"Level {lvl}",
            "geometry": {"exteriorPolygon": [[0, 0], [FW, 0], [FW, FD], [0, FD]],
                         "corePolygon": CORE, "wallThickness": WALL_THICKNESS},
            "zones": [],
        }
        onto_nodes.append({"id": f"panel-lvl{lvl}", "type": "brick:Electrical_Panel",
                           "label": f"Electrical Panel L{lvl}"})

        for slot, (ztype, base_name) in layout.items():
            poly = SLOTS[slot]
            a = max(1.0, area(poly))
            arch = ARCH[ztype]
            zid = f"zone-{slug(base_name)}-lvl{lvl}"
            vid = f"vav-{slug(base_name)}-lvl{lvl}"
            bim = str(uuid.uuid4())
            perim = touches_perimeter(poly) and ztype not in ("server-room", "corridor", "mechanical")
            zone = {
                "zoneId": zid, "name": f"{base_name} Level {lvl}", "zoneType": ztype,
                "bim_asset_id": bim, "polygon": poly, "centroid": centroid(poly),
                "thermalProperties": {
                    "setpoint": arch["setpoint"], "deadband": arch["deadband"],
                    "baseHeatLoad": arch["baseHeatLoad"],
                    "solarGainMultiplier": round(1.2 if perim else 0.0, 2),
                    "rWall": 0.2, "cAir": round(a * FLOOR_HEIGHT * AIR_VOL_HEAT_CAP),
                    "occupancy": arch["occ"],
                },
                "hvacMapping": {"vavId": vid},
            }
            floor["zones"].append(zone)

            # --- ontology: nodes + HVAC/electrical/sensor relationships ---
            onto_nodes.append({"id": zid, "type": "brick:HVAC_Zone", "label": zone["name"], "bim_asset_id": bim})
            onto_rels += [
                {"source": "ahu-main", "predicate": "brick:feeds", "target": vid},
                {"source": vid, "predicate": "brick:feeds", "target": zid},
                {"source": f"panel-lvl{lvl}", "predicate": "brick:feeds", "target": f"circuit_{zid}"},
                {"source": f"circuit_{zid}", "predicate": "brick:feeds", "target": zid},
                {"source": zid, "predicate": "brick:hasPoint", "target": f"sensor_temp_{zid}"},
                {"source": zid, "predicate": "brick:hasPoint", "target": f"camera_{zid}"},
            ]
        building["floors"].append(floor)

    ontology = {"@context": {"brick": "https://brickschema.org/schema/Brick#",
                              "bf": "https://brickschema.org/schema/BrickFrame#"},
                "nodes": onto_nodes, "relationships": onto_rels}
    return building, ontology


def main():
    import sys
    building, ontology = build()
    data_dir = os.path.join(os.path.dirname(__file__), "..", "data")
    # Write to *.testbed.json by default so we never clobber the live data; pass --deploy to
    # write straight to the live files (then update the ID refs + rebuild — see TEST_DATA_GUIDE.md).
    suffix = "" if "--deploy" in sys.argv else ".testbed"
    bpath = os.path.join(data_dir, f"building-data{suffix}.json")
    opath = os.path.join(data_dir, f"brick-ontology{suffix}.json")
    with open(bpath, "w") as f:
        json.dump(building, f, indent=2)
    with open(opath, "w") as f:
        json.dump(ontology, f, indent=2)

    nz = sum(len(fl["zones"]) for fl in building["floors"])
    from collections import Counter
    types = Counter(z["zoneType"] for fl in building["floors"] for z in fl["zones"])
    servers = [z["zoneId"] for fl in building["floors"] for z in fl["zones"] if z["zoneType"] == "server-room"]
    print(f"[gen] {N_FLOORS} floors, {nz} zones")
    print(f"[gen] zone types: {dict(types)}")
    print(f"[gen] server rooms (fault targets): {len(servers)} -> {servers[:5]}{'...' if len(servers)>5 else ''}")
    print(f"[gen] ontology: {len(ontology['nodes'])} nodes, {len(ontology['relationships'])} relationships")
    print(f"[gen] wrote {os.path.relpath(bpath)} + {os.path.relpath(opath)}")
    if suffix:
        print("[gen] (testbed files — pass --deploy to write live, then wire IDs + rebuild; see TEST_DATA_GUIDE.md)")


if __name__ == "__main__":
    main()
