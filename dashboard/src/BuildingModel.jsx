import React, { useRef, useMemo, useState } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { OrbitControls, Html, Edges } from '@react-three/drei';
import * as THREE from 'three';
import { Evaluator, Brush, SUBTRACTION } from 'three-bvh-csg';
import buildingData from './building-data.json';
import * as BufferGeometryUtils from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import AirflowField from './AirflowField';

// ========== CSG Helper (three-bvh-csg Evaluator/Brush API) ==========
// three-bvh-csg has no static `CSG` helper; it exposes an Evaluator that
// operates on Brush meshes whose world matrices define their placement.
const csgEvaluator = new Evaluator();
csgEvaluator.attributes = ['position', 'normal'];

function meshToBrush(mesh) {
  mesh.updateMatrix();
  const brush = new Brush(mesh.geometry);
  brush.position.copy(mesh.position);
  brush.quaternion.copy(mesh.quaternion);
  brush.scale.copy(mesh.scale);
  brush.updateMatrixWorld(true);
  return brush;
}

// Subtract one or more tool meshes from a base mesh; returns baked geometry
// in the base mesh's transformed (world) space, matching the old CSG.toMesh.
function csgSubtract(baseMesh, toolMeshes) {
  if (!toolMeshes || toolMeshes.length === 0) {
    baseMesh.updateMatrix();
    return baseMesh.geometry.clone().applyMatrix4(baseMesh.matrix);
  }
  let result = meshToBrush(baseMesh);
  toolMeshes.forEach((tool) => {
    result = csgEvaluator.evaluate(result, meshToBrush(tool), SUBTRACTION);
  });
  let resultGeom = result.geometry;
  resultGeom = BufferGeometryUtils.mergeVertices(resultGeom, 1e-4);
  resultGeom.computeVertexNormals();
  return resultGeom;
}

// ========== Module-level CSG geometry cache ==========
// CSG is expensive and the tower has only ~5 distinct floor shapes across its
// 14 levels (the 8 typical-office floors are identical). Keying the result on a
// structural signature means each unique wall/plate runs CSG exactly once and
// the geometry is shared by every floor that matches — cutting initial CSG cost
// by ~3x. Geometries live for the app lifetime (bounded set), so no disposal is
// needed; the cache itself prevents the unbounded-leak case.
const _geometryCache = new Map();
function getCachedGeometry(signature, build) {
  let geom = _geometryCache.get(signature);
  if (!geom) {
    geom = build();
    _geometryCache.set(signature, geom);
  }
  return geom;
}

// ========== STEP 1: CSG-Based Wall with Window Cutouts ==========
function WallWithWindows({ position: [x, y, z], width, height, depth, rotation, windows = [], isActive }) {
  const meshRef = useRef();
  
  const wallGeometry = useMemo(() => {
    const signature = `wall|${width}|${height}|${depth}|${JSON.stringify(windows)}`;
    return getCachedGeometry(signature, () => {
      const wallBox = new THREE.BoxGeometry(width, height, depth);
      wallBox.translate(0, height / 2, 0); // Bake the Y shift into the geometry directly!
      const wallMesh = new THREE.Mesh(wallBox);

      const windowMeshes = windows.map((window) => {
        const windowBox = new THREE.BoxGeometry(window.width, window.height, depth + 0.5);
        windowBox.translate(window.x, window.y, 0); // Bake local window pos into geometry!
        const windowMesh = new THREE.Mesh(windowBox);
        return windowMesh;
      });

      return csgSubtract(wallMesh, windowMeshes);
    });
  }, [width, height, depth, windows]);
  
  return (
    <mesh
      ref={meshRef}
      position={[x, y, z]}
      rotation={[0, -rotation, 0]}
      geometry={wallGeometry}
      dispose={null}
    >
      <meshStandardMaterial 
        color={isActive ? "#888888" : "#222222"}
        roughness={0.8}
        metalness={0.2}
        transparent={true}
        opacity={isActive ? 0.2 : 0.05}
      />
    </mesh>
  );
}

// ========== STEP 2: Exterior Walls Generator ==========
function ExteriorWalls({ floor, isActive }) {
  const walls = useMemo(() => {
    const polygon = floor.geometry.exteriorPolygon;
    const wallSegments = [];
    
    for (let i = 0; i < polygon.length; i++) {
      const start = polygon[i];
      const end = polygon[(i + 1) % polygon.length];
      
      // Convert from 2D coordinates [x, y] to 3D [x, 0, -z] centered around (20, 20)
      const sx = start[0] - 20;
      const sz = -start[1] + 20;
      const ex = end[0] - 20;
      const ez = -end[1] + 20;

      const width = Math.sqrt((ex - sx) ** 2 + (ez - sz) ** 2);
      const angle = Math.atan2(ez - sz, ex - sx);
      
      const windowSpacing = floor.floorType === 'typical-office' ? 4.0 : 6.0;
      const windows = [];
      let currentX = windowSpacing / 2;
      while (currentX < width - windowSpacing / 2) {
        windows.push({
          x: currentX - width / 2,
          y: 1.0 + (floor.height - 1.5) / 2, // Bottom sill at 1m from floor
          width: 2.0,
          height: floor.height - 1.5,
        });
        currentX += windowSpacing;
      }

      wallSegments.push({
        position: [(sx + ex) / 2, 0, (sz + ez) / 2],
        width,
        height: floor.height,
        depth: floor.geometry.wallThickness,
        rotation: angle,
        windows: windows,
      });
    }
    
    return wallSegments;
  }, [floor]);
  
  return (
    <group>
      {walls.map((wall, idx) => (
        <WallWithWindows
          key={`wall-${idx}`}
          position={wall.position}
          width={wall.width}
          height={wall.height}
          depth={wall.depth}
          rotation={wall.rotation}
          windows={wall.windows}
          isActive={isActive}
        />
      ))}
    </group>
  );
}

// ========== STEP 3: Floor Plate with Core Cutout ==========
function FloorPlate({ floor, isActive, onClick, simState }) {
  const [hovered, setHovered] = useState(false);

  const hasAlert = useMemo(() => {
    if (!simState || !simState.zones) return false;
    return floor.zones.some(z => {
        const alertState = simState.zones[z.zoneId]?.alert;
        return alertState === true || alertState === 'REMEDIATING';
    });
  }, [floor.zones, simState]);

  const geometry = useMemo(() => {
    const g = floor.geometry;
    const signature = `plate_native|${JSON.stringify(g.exteriorPolygon)}|${JSON.stringify(g.corePolygon)}|${g.wallThickness}`;
    return getCachedGeometry(signature, () => {
      const exteriorShape = new THREE.Shape();
      g.exteriorPolygon.forEach((p, idx) => {
        if (idx === 0) exteriorShape.moveTo(p[0] - 20, p[1] - 20);
        else exteriorShape.lineTo(p[0] - 20, p[1] - 20);
      });
      exteriorShape.lineTo(g.exteriorPolygon[0][0] - 20, g.exteriorPolygon[0][1] - 20);

      // Natively subtract the core hole (No CSG needed, solves triangulation artifacts!)
      if (g.corePolygon && g.corePolygon.length > 0) {
        const corePath = new THREE.Path();
        g.corePolygon.forEach((p, idx) => {
          if (idx === 0) corePath.moveTo(p[0] - 20, p[1] - 20);
          else corePath.lineTo(p[0] - 20, p[1] - 20);
        });
        corePath.lineTo(g.corePolygon[0][0] - 20, g.corePolygon[0][1] - 20);
        exteriorShape.holes.push(corePath);
      }

      const exteriorGeom = new THREE.ExtrudeGeometry(exteriorShape, {
        depth: g.wallThickness,
        bevelEnabled: false,
      });
      
      // Bake rotation and Y shift into the geometry
      exteriorGeom.rotateX(-Math.PI / 2);
      exteriorGeom.translate(0, -g.wallThickness, 0);

      // Return perfectly indexed geometry to prevent EdgesGeometry from drawing internal diagonals
      return exteriorGeom;
    });
  }, [floor]);
  
  return (
    <group>
      <mesh
        geometry={geometry}
        dispose={null}
        onClick={(e) => { e.stopPropagation(); onClick(floor.level); }}
        onPointerOver={(e) => { e.stopPropagation(); setHovered(true); document.body.style.cursor = 'pointer'; }}
        onPointerOut={() => { setHovered(false); document.body.style.cursor = 'auto'; }}
      >
        <meshStandardMaterial 
          color={hasAlert ? "#aa0000" : (isActive ? "#dddddd" : hovered ? "#555555" : "#333333")}
          roughness={0.9}
          transparent={true}
          opacity={hasAlert ? 0.6 : (isActive ? 0.4 : hovered ? 0.6 : 0.3)}
          polygonOffset={true}
          polygonOffsetFactor={2}
        />
        <Edges color={hasAlert ? "#ff0000" : (isActive ? "#ffffff" : hovered ? "#00ffff" : "#444444")} threshold={15} />
      </mesh>

      {/* Floating Live Data Label */}
      {(isActive || hovered || hasAlert) && (
        <Html position={[-25, floor.height / 2, 20]} center zIndexRange={[100, 0]} style={{ transition: 'all 0.2s', pointerEvents: 'none' }}>
          <div style={{ 
            color: hasAlert ? '#ff3333' : (isActive ? '#fff' : '#00ffff'), 
            background: hasAlert ? 'rgba(50,0,0,0.85)' : 'rgba(0,0,0,0.85)', 
            border: `1px solid ${hasAlert ? '#ff0000' : (isActive ? '#fff' : '#00ffff')}`, 
            padding: '6px 12px', fontSize: '11px', fontFamily: 'monospace', whiteSpace: 'nowrap', 
            textShadow: hasAlert ? '0 0 10px rgba(255,0,0,0.8)' : '0 0 5px rgba(0,229,255,0.5)',
            transform: hasAlert ? 'scale(1.1)' : 'scale(1)',
            transition: 'all 0.3s'
          }}>
            <strong style={{ fontSize: '12px' }}>{hasAlert ? '⚠️ ' : ''}[ L{floor.level} ]</strong><br/>
            {floor.name}<br/>
            <span style={{ color: hasAlert ? '#ff5555' : '#00e5ff' }}>
              {hasAlert ? 'CRITICAL FAULT' : `ZONES: ${floor.zones.length}`}
            </span>
          </div>
        </Html>
      )}
    </group>
  );
}

// ========== STEP 4: Zone Renderer with Thermal Heatmap ==========
function ZoneRenderer({ zone, isActive, simState, isHovered, onHover }) {
  const meshRef = useRef();
  const zoneSim = simState.zones[zone.zoneId];
  const temperature = zoneSim ? zoneSim.temp : zone.thermalProperties.setpoint;
  const setpoint = zone.thermalProperties.setpoint;
  const deadband = zone.thermalProperties.deadband;

  const thickness = 3.8;
  const cx = zone.centroid.x - 20;
  const cy = -(zone.centroid.y - 20);

  const geometry = useMemo(() => {
    const shape = new THREE.Shape();
    zone.polygon.forEach((p, idx) => {
      if (idx === 0) shape.moveTo(p[0] - 20, p[1] - 20);
      else shape.lineTo(p[0] - 20, p[1] - 20);
    });
    shape.lineTo(zone.polygon[0][0] - 20, zone.polygon[0][1] - 20);
    
    const geom = new THREE.ExtrudeGeometry(shape, {
      depth: thickness,
      bevelThickness: 0.05,
      bevelSize: 0.05,
      bevelSegments: 3,
      curveSegments: 12,
    });

    geom.rotateX(-Math.PI / 2);
    geom.computeVertexNormals();
    return geom.toNonIndexed();
  }, [zone, thickness]);

  const material = useMemo(() => {
    return new THREE.ShaderMaterial({
      uniforms: {
        temperature: { value: temperature },
        setpoint: { value: setpoint },
        deadband: { value: deadband },
        opacity: { value: isActive ? (isHovered ? 0.9 : 0.65) : 0.15 }
      },
      vertexShader: `
        varying vec2 vUv;
        void main() {
          vUv = uv;
          gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
        }
      `,
      fragmentShader: `
        uniform float temperature;
        uniform float setpoint;
        uniform float deadband;
        uniform float opacity;
        varying vec2 vUv;
        
        vec3 heatmap(float deviation) {
          float r = smoothstep(0.5, 1.5, deviation);
          float b = smoothstep(0.5, 1.5, -deviation);
          float g = 1.0 - max(r, b);
          return vec3(r, g, b);
        }
        
        void main() {
          float deviation = (temperature - setpoint) / deadband;
          gl_FragColor = vec4(heatmap(deviation), opacity);
        }
      `,
      transparent: true,
      side: THREE.DoubleSide,
      depthWrite: false,
    });
  }, [isActive, isHovered]);

  useFrame(() => {
    if (material && material.uniforms.temperature) {
      const liveTemp = simState.zones[zone.zoneId]?.temp || setpoint;
      material.uniforms.temperature.value = THREE.MathUtils.lerp(
        material.uniforms.temperature.value,
        liveTemp,
        0.05
      );
    }
  });

  return (
    <group position={[0, 0.01, 0]}>
      <mesh 
        ref={meshRef} 
        geometry={geometry}
        onPointerOver={(e) => {
          if (isActive) {
            e.stopPropagation();
            onHover(zone.zoneId);
            document.body.style.cursor = 'pointer';
          }
        }}
        onPointerOut={(e) => {
          if (isActive) {
            onHover(null);
            document.body.style.cursor = 'auto';
          }
        }}
      >
        <primitive object={material} attach="material" />
        <Edges color={isHovered && isActive ? "#ffffff" : "#222222"} threshold={15} />
      </mesh>
      
      {isActive && isHovered && (
        <Html position={[cx, 2.5, cy]} center zIndexRange={[100, 0]}>
          <div style={{
            background: 'rgba(0,0,0,0.8)',
            border: '1px solid #00e5ff',
            padding: '4px 8px',
            borderRadius: '4px',
            color: '#00e5ff',
            fontFamily: 'monospace',
            fontSize: '10px',
            pointerEvents: 'none',
            whiteSpace: 'nowrap'
          }}>
            Asset: {zone.bim_asset_id}
          </div>
        </Html>
      )}
    </group>
  );
}

// ========== STEP 5: Complete Production Building Component ==========
export default function BuildingModel({ simState, activeFloor, onFloorClick, showAirflow }) {
  const [hoveredZone, setHoveredZone] = useState(null);
  const floors = buildingData.floors;

  return (
    <div style={{ width: '100%', height: '100%', position: 'absolute', top: 0, left: 0, zIndex: 1 }}>
      <Canvas camera={{ position: [50, 40, 50], fov: 45 }}>
        <color attach="background" args={['#000000']} />
        <ambientLight intensity={0.4} />
        <directionalLight position={[10, 20, 10]} intensity={1.2} />
        
        <OrbitControls target={[0, 15, 0]} />

        <group position={[-30, 0, -20]}>
          {floors.map((floor) => {
            const isActive = floor.level === activeFloor;
            
            let yOffset = 0;
            if (floor.level > activeFloor) {
                yOffset = 30.0;
            } else if (floor.level === activeFloor) {
                yOffset = 5.0;
            }

            const displayElevation = floor.elevation + yOffset;

            return (
              <group 
                key={`floor-${floor.level}`} 
                position={[0, displayElevation, 0]}
                onClick={(e) => {
                  e.stopPropagation();
                  onFloorClick(floor.level);
                }}
              >
                <FloorPlate floor={floor} isActive={isActive} onClick={onFloorClick} simState={simState} />

                {isActive && <ExteriorWalls floor={floor} isActive={isActive} />}
                {isActive && showAirflow && <AirflowField floor={floor} />}
                <group>
                  {floor.zones.map((zone) => (
                    <ZoneRenderer
                      key={zone.zoneId}
                      zone={zone}
                      isActive={isActive}
                      simState={simState}
                      isHovered={hoveredZone === zone.zoneId}
                      onHover={setHoveredZone}
                    />
                  ))}
                </group>
              </group>
            );
          })}
        </group>
        
        <gridHelper args={[100, 100, '#333333', '#111111']} position={[0, -0.1, 0]} />
      </Canvas>
    </div>
  );
}
