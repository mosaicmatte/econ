const fs = require('fs');

// Read the massive 15-story BIM geometry file
const rawData = fs.readFileSync('../data/building-data.json');
const buildingData = JSON.parse(rawData);

const ontology = {
  "@context": {
    "brick": "https://brickschema.org/schema/Brick#",
    "bf": "https://brickschema.org/schema/BrickFrame#"
  },
  "nodes": [],
  "relationships": []
};

// 1. Root AHU
ontology.nodes.push({
  id: "ahu-main",
  type: "brick:AHU",
  label: "Main Rooftop AHU"
});

// Parse all floors
buildingData.floors.forEach(floor => {
  // Floor Electrical Panel
  const panelId = `panel-lvl${floor.level}`;
  ontology.nodes.push({
    id: panelId,
    type: "brick:Electrical_Panel",
    label: `Electrical Panel L${floor.level}`
  });

  // Add Zones and their VAVs
  floor.zones.forEach(zone => {
    ontology.nodes.push({
      id: zone.zoneId,
      type: "brick:HVAC_Zone",
      label: zone.name,
      bim_asset_id: zone.bim_asset_id
    });
    
    // Virtual Sensor Point
    const sensorId = `sensor_temp_${zone.zoneId}`;
    ontology.nodes.push({
      id: sensorId,
      type: "brick:Temperature_Sensor"
    });
    
    // Relationship: Zone has a Temperature Sensor
    ontology.relationships.push({
      source: zone.zoneId,
      predicate: "brick:hasPoint",
      target: sensorId
    });

    // Camera
    const cameraId = `camera_${zone.zoneId}`;
    ontology.nodes.push({
      id: cameraId,
      type: "brick:Camera"
    });

    // Relationship: Zone has a Camera
    ontology.relationships.push({
      source: zone.zoneId,
      predicate: "brick:hasPoint",
      target: cameraId
    });

    // Electrical Circuit
    const circuitId = `circuit_${zone.zoneId}`;
    ontology.nodes.push({
      id: circuitId,
      type: "brick:Electrical_Circuit",
      label: `Circuit ${zone.zoneId}`
    });

    // Relationship: Panel feeds Circuit
    ontology.relationships.push({
      source: panelId,
      predicate: "brick:feeds",
      target: circuitId
    });

    // Relationship: Circuit feeds Zone
    ontology.relationships.push({
      source: circuitId,
      predicate: "brick:feeds",
      target: zone.zoneId
    });

    // Add VAV mapping if exists
    if (zone.hvacMapping && zone.hvacMapping.vavId) {
      const vavId = zone.hvacMapping.vavId;
      ontology.nodes.push({
        id: vavId,
        type: "brick:VAV",
        label: `VAV Box ${vavId}`
      });

      // Relationship: AHU feeds VAV
      ontology.relationships.push({
        source: "ahu-main",
        predicate: "brick:feeds",
        target: vavId
      });

      // Relationship: VAV feeds Zone
      ontology.relationships.push({
        source: vavId,
        predicate: "brick:feeds",
        target: zone.zoneId
      });
    }
  });
});

// Deduplicate nodes (since multiple zones might mistakenly share a VAV, though in our data it's 1:1)
const uniqueNodesMap = {};
ontology.nodes.forEach(n => uniqueNodesMap[n.id] = n);
ontology.nodes = Object.values(uniqueNodesMap);

// Deduplicate relationships
const uniqueRelsMap = {};
ontology.relationships.forEach(r => uniqueRelsMap[`${r.source}-${r.predicate}-${r.target}`] = r);
ontology.relationships = Object.values(uniqueRelsMap);

// Save to disk
fs.writeFileSync('../data/brick-ontology.json', JSON.stringify(ontology, null, 2));
console.log(`✅ Generated Brick Schema Ontology with ${ontology.nodes.length} entities and ${ontology.relationships.length} relationships.`);
