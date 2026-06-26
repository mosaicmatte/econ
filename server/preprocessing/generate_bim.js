const fs = require('fs');
const crypto = require('crypto');

function uuidv4() {
    return crypto.randomUUID();
}

const TOTAL_FLOORS = 15;
const BUILDING_WIDTH = 60; // X axis
const BUILDING_DEPTH = 40; // Y axis

// Generate the 15-story building layout
const building = {
    buildingId: "bldg-econ-hq",
    floors: []
};

for (let i = 1; i <= TOTAL_FLOORS; i++) {
    const level = i;
    const floor = {
        level: level,
        elevation: (level - 1) * 4.0, // 4 meters per floor
        height: 4.0,
        name: `Level ${level}`,
        geometry: {
            exteriorPolygon: [[0, 0], [60, 0], [60, 40], [0, 40]],
            corePolygon: [[20, 15], [40, 15], [40, 25], [20, 25]],
            wallThickness: 0.3
        },
        zones: []
    };

    // --- ZONE: CORE (Stairs/Elevators) ---
    // Central core 20m x 10m
    floor.zones.push({
        zoneId: `zone-core-lvl${level}`,
        name: `Core Elevator Lobby Level ${level}`,
        zoneType: "corridor",
        bim_asset_id: uuidv4(),
        polygon: [
            [20, 15], [40, 15], [40, 25], [20, 25]
        ],
        centroid: { x: 30, y: 20 },
        thermalProperties: { setpoint: 24.0, deadband: 2.0, baseHeatLoad: 5000, solarGainMultiplier: 0.0, rWall: 0.2, cAir: 1000000 },
        hvacMapping: { vavId: `vav-core-lvl${level}` }
    });

    // --- ZONE: NORTH PERIMETER ---
    floor.zones.push({
        zoneId: `zone-north-lvl${level}`,
        name: `North Perimeter Level ${level}`,
        zoneType: "office",
        bim_asset_id: uuidv4(),
        polygon: [
            [0, 0], [60, 0], [60, 5], [0, 5]
        ],
        centroid: { x: 30, y: 2.5 },
        thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 15000, solarGainMultiplier: 0.5, rWall: 0.15, cAir: 1500000 },
        hvacMapping: { vavId: `vav-north-lvl${level}` }
    });

    // --- ZONE: SOUTH PERIMETER ---
    floor.zones.push({
        zoneId: `zone-south-lvl${level}`,
        name: `South Perimeter Level ${level}`,
        zoneType: "office",
        bim_asset_id: uuidv4(),
        polygon: [
            [0, 35], [60, 35], [60, 40], [0, 40]
        ],
        centroid: { x: 30, y: 37.5 },
        thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 15000, solarGainMultiplier: 1.5, rWall: 0.15, cAir: 1500000 },
        hvacMapping: { vavId: `vav-south-lvl${level}` }
    });

    // --- ZONE: EAST PERIMETER ---
    floor.zones.push({
        zoneId: `zone-east-lvl${level}`,
        name: `East Perimeter Level ${level}`,
        zoneType: "office",
        bim_asset_id: uuidv4(),
        polygon: [
            [55, 5], [60, 5], [60, 35], [55, 35]
        ],
        centroid: { x: 57.5, y: 20 },
        thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 10000, solarGainMultiplier: 1.2, rWall: 0.15, cAir: 1000000 },
        hvacMapping: { vavId: `vav-east-lvl${level}` }
    });

    // --- ZONE: WEST PERIMETER ---
    floor.zones.push({
        zoneId: `zone-west-lvl${level}`,
        name: `West Perimeter Level ${level}`,
        zoneType: "office",
        bim_asset_id: uuidv4(),
        polygon: [
            [0, 5], [5, 5], [5, 35], [0, 35]
        ],
        centroid: { x: 2.5, y: 20 },
        thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 10000, solarGainMultiplier: 1.2, rWall: 0.15, cAir: 1000000 },
        hvacMapping: { vavId: `vav-west-lvl${level}` }
    });

    // --- ZONE: OPEN OFFICE A (North of Core) ---
    floor.zones.push({
        zoneId: `zone-open-a-lvl${level}`,
        name: `Open Office A Level ${level}`,
        zoneType: "office",
        bim_asset_id: uuidv4(),
        polygon: [
            [5, 5], [55, 5], [55, 15], [5, 15]
        ],
        centroid: { x: 30, y: 10 },
        thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 45000, solarGainMultiplier: 0.2, rWall: 0.2, cAir: 3000000 },
        hvacMapping: { vavId: `vav-open-a-lvl${level}` }
    });

    // --- ZONE: OPEN OFFICE B (South of Core) ---
    floor.zones.push({
        zoneId: `zone-open-b-lvl${level}`,
        name: `Open Office B Level ${level}`,
        zoneType: "office",
        bim_asset_id: uuidv4(),
        polygon: [
            [5, 25], [55, 25], [55, 35], [5, 35]
        ],
        centroid: { x: 30, y: 30 },
        thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 45000, solarGainMultiplier: 0.2, rWall: 0.2, cAir: 3000000 },
        hvacMapping: { vavId: `vav-open-b-lvl${level}` }
    });

    // --- ZONE: SERVER ROOM (Only on specific floors) ---
    if (level === 4 || level === 8 || level === 12) {
        // Place server room on the west side
        floor.zones.push({
            zoneId: `zone-server-lvl${level}`,
            name: `Server Room Level ${level}`,
            zoneType: "server-room",
            bim_asset_id: uuidv4(),
            polygon: [
                [5, 15], [20, 15], [20, 25], [5, 25]
            ],
            centroid: { x: 12.5, y: 20 },
            thermalProperties: { setpoint: 20.0, deadband: 1.0, baseHeatLoad: 85000, solarGainMultiplier: 0.0, rWall: 0.05, cAir: 2500000 },
            hvacMapping: { vavId: `vav-server-lvl${level}` }
        });
        
        // East side is a conference room
        floor.zones.push({
            zoneId: `zone-conf-lvl${level}`,
            name: `Conference Room Level ${level}`,
            zoneType: "conference",
            bim_asset_id: uuidv4(),
            polygon: [
                [40, 15], [55, 15], [55, 25], [40, 25]
            ],
            centroid: { x: 47.5, y: 20 },
            thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 30000, solarGainMultiplier: 0.1, rWall: 0.2, cAir: 1500000 },
            hvacMapping: { vavId: `vav-conf-lvl${level}` }
        });
    } else {
        // Normal floors have extra open offices bridging the sides
        floor.zones.push({
            zoneId: `zone-open-west-lvl${level}`,
            name: `Open Office West Level ${level}`,
            zoneType: "office",
            bim_asset_id: uuidv4(),
            polygon: [
                [5, 15], [20, 15], [20, 25], [5, 25]
            ],
            centroid: { x: 12.5, y: 20 },
            thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 25000, solarGainMultiplier: 0.2, rWall: 0.2, cAir: 2000000 },
            hvacMapping: { vavId: `vav-open-west-lvl${level}` }
        });
        
        floor.zones.push({
            zoneId: `zone-open-east-lvl${level}`,
            name: `Open Office East Level ${level}`,
            zoneType: "office",
            bim_asset_id: uuidv4(),
            polygon: [
                [40, 15], [55, 15], [55, 25], [40, 25]
            ],
            centroid: { x: 47.5, y: 20 },
            thermalProperties: { setpoint: 22.0, deadband: 1.5, baseHeatLoad: 25000, solarGainMultiplier: 0.2, rWall: 0.2, cAir: 2000000 },
            hvacMapping: { vavId: `vav-open-east-lvl${level}` }
        });
    }

    building.floors.push(floor);
}

// Ensure the data directory exists
const targetDir = '/Users/nguyenhoangkhoi/Downloads/bki_project/econ/server/data';
if (!fs.existsSync(targetDir)){
    fs.mkdirSync(targetDir, { recursive: true });
}

fs.writeFileSync(`${targetDir}/building-data.json`, JSON.stringify(building, null, 2));

console.log(`Successfully generated massive 15-story BIM model with ${TOTAL_FLOORS * 9} thermal zones and UUIDs.`);
