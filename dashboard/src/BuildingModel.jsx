import React, { useRef, useMemo, useState, useEffect } from 'react';
import { Canvas, useFrame, useThree } from '@react-three/fiber';
import { OrbitControls, Html, Edges } from '@react-three/drei';
import * as THREE from 'three';
import { Evaluator, Brush, SUBTRACTION } from 'three-bvh-csg';
import { getBuilding } from './buildingStore';
const buildingData = getBuilding(); // live geometry — fetched before this module evaluates (see main.jsx)
import * as BufferGeometryUtils from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import FloorInfrastructure from './FloorInfrastructure';
import { exteriorPolygon, corePolygon, wallThickness, toWorld, ORIGIN, FOOTPRINT } from './floorGeometry';

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
function WallWithWindows({ position: [x, y, z], width, height, depth, rotation, windows = [], isActive, viewMode = 'hybrid' }) {
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
  
  const isLogical = viewMode === 'logical';
  const opacity = isLogical ? 0.05 : (isActive ? 0.2 : 0.05);

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
        opacity={opacity}
        wireframe={isLogical && isActive}
      />
    </mesh>
  );
}

// ========== STEP 2: Exterior Walls Generator ==========
function ExteriorWalls({ floor, isActive, viewMode = 'hybrid' }) {
  const walls = useMemo(() => {
    // Via the shared accessor: a fixture whose generator spelled the envelope differently
    // used to throw here, and the canvas boundary swallowed it — so the whole building
    // silently failed to render rather than reporting anything.
    const polygon = exteriorPolygon(floor);
    if (!polygon) return [];
    const wallSegments = [];
    
    for (let i = 0; i < polygon.length; i++) {
      const start = polygon[i];
      const end = polygon[(i + 1) % polygon.length];
      
      // Convert from 2D coordinates [x, y] to 3D [x, 0, -z] centered around (20, 20)
      const [sx, sz] = toWorld(start);
      const [ex, ez] = toWorld(end);

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
        depth: wallThickness(floor),
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
          viewMode={viewMode}
        />
      ))}
    </group>
  );
}

// ========== STEP 3: Floor Plate with Core Cutout ==========
function FloorPlate({ floor, isActive, onClick, simState, viewMode = 'hybrid' }) {
  const [hovered, setHovered] = useState(false);

  const hasAlert = useMemo(() => {
    if (!simState || !simState.zones) return false;
    return floor.zones.some(z => {
        const alertState = simState.zones[z.zoneId]?.alert;
        return alertState === true || alertState === 'REMEDIATING';
    });
  }, [floor.zones, simState]);

  const geometry = useMemo(() => {
    const ext = exteriorPolygon(floor);
    if (!ext) return null; // no envelope for this floor — draw nothing rather than throw
    const core = corePolygon(floor);
    const thick = wallThickness(floor);
    const signature = `plate_native|${JSON.stringify(ext)}|${JSON.stringify(core)}|${thick}`;
    return getCachedGeometry(signature, () => {
      const exteriorShape = new THREE.Shape();
      ext.forEach((p, idx) => {
        if (idx === 0) exteriorShape.moveTo(p[0] - ORIGIN.x, p[1] - ORIGIN.y);
        else exteriorShape.lineTo(p[0] - ORIGIN.x, p[1] - ORIGIN.y);
      });
      exteriorShape.lineTo(ext[0][0] - ORIGIN.x, ext[0][1] - ORIGIN.y);

      // Natively subtract the core hole (No CSG needed, solves triangulation artifacts!)
      // A building with no service core — a house — simply has no hole to cut.
      if (core.length > 0) {
        const corePath = new THREE.Path();
        core.forEach((p, idx) => {
          if (idx === 0) corePath.moveTo(p[0] - ORIGIN.x, p[1] - ORIGIN.y);
          else corePath.lineTo(p[0] - ORIGIN.x, p[1] - ORIGIN.y);
        });
        corePath.lineTo(core[0][0] - ORIGIN.x, core[0][1] - ORIGIN.y);
        exteriorShape.holes.push(corePath);
      }

      const exteriorGeom = new THREE.ExtrudeGeometry(exteriorShape, {
        depth: thick,
        bevelEnabled: false,
      });
      
      // Bake rotation and Y shift into the geometry
      exteriorGeom.rotateX(-Math.PI / 2);
      exteriorGeom.translate(0, -thick, 0);

      // Return perfectly indexed geometry to prevent EdgesGeometry from drawing internal diagonals
      return exteriorGeom;
    });
  }, [floor]);
  
  const isLogical = viewMode === 'logical';
  const baseOpacity = isLogical ? 0.05 : (isActive ? 0.4 : 0.3);
  const opacity = hasAlert ? 0.6 : (hovered ? 0.6 : baseOpacity);

  // No envelope for this floor: the slab is what this component draws, so there is
  // nothing to draw. Zones render from their own polygons in a sibling component and are
  // unaffected. Returning null beats handing <mesh> a null geometry.
  if (!geometry) return null;

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
          opacity={opacity}
          polygonOffset={true}
          polygonOffsetFactor={2}
          wireframe={isLogical && isActive}
        />
        <Edges color={hasAlert ? "#ff0000" : (isActive ? "#ffffff" : hovered ? "#00ffff" : "#444444")} threshold={15} />
      </mesh>

      {/* Tesla-Style Vertical Drop Label */}
      {/* Floor label, just off the building's left edge and stepped per level so a
          stacked tower's labels do not overlap. The literal -30 this replaces was half the
          office plate's width; on a 13.6 m house it placed the label a building and a half
          away from the building. Anchored at mid-height rather than at the roofline, where
          on a portrait phone it collided with the HUD readouts across the top. */}
      {(isActive || hovered || hasAlert) && (
        <Html position={[-(FOOTPRINT.width / 2) - 2 + (floor.level * 1.8), floor.height / 2, 0]} style={{ pointerEvents: 'none' }}>
          <div style={{ 
            position: 'absolute', 
            bottom: '0px', 
            left: '-60px', 
            width: '120px', 
            display: 'flex', 
            flexDirection: 'column', 
            alignItems: 'center',
            zIndex: 10
          }}>
            {/* The Text Tag (No Background) */}
            <div style={{ 
              color: hasAlert ? '#ff453a' : '#ffffff', 
              fontSize: '13px', 
              fontFamily: 'system-ui, -apple-system, sans-serif', 
              fontWeight: '700',
              letterSpacing: '0.05em',
              textShadow: '0 2px 8px rgba(0,0,0,0.8)'
            }}>
              {hasAlert ? '⚠️ ' : ''}LEVEL {floor.level}
            </div>
            <div style={{ 
              color: hasAlert ? '#ff453a' : 'rgba(255,255,255,0.7)', 
              fontSize: '10px', 
              fontWeight: '600',
              textTransform: 'uppercase',
              textShadow: '0 2px 4px rgba(0,0,0,0.8)'
            }}>
              {hasAlert ? 'CRITICAL FAULT' : `${floor.zones.length} ZONES`}
            </div>

            {/* Vertical Drop Line */}
            <div style={{ 
              width: '1px', 
              height: '40px', 
              backgroundColor: hasAlert ? 'rgba(255,69,58,0.8)' : 'rgba(255,255,255,0.4)', 
              margin: '6px 0' 
            }} />
            
            {/* Anchor Dot */}
            <div style={{ 
              width: '5px', 
              height: '5px', 
              borderRadius: '50%', 
              backgroundColor: hasAlert ? '#ff453a' : '#ffffff', 
              marginBottom: '-2px',
              boxShadow: hasAlert ? '0 0 8px #ff453a' : '0 0 8px rgba(255,255,255,0.8)'
            }} />
          </div>
        </Html>
      )}
    </group>
  );
}

// ========== STEP 4: Zone Renderer with Thermal Heatmap ==========
function ZoneRenderer({ zone, isActive, simState, isHovered, onHover, isSelected, onSelect, viewMode = 'hybrid' }) {
  const meshRef = useRef();
  const zoneSim = simState.zones[zone.zoneId];
  const alertState = zoneSim?.alert;
  const temperature = zoneSim ? zoneSim.temp : zone.thermalProperties.setpoint;
  const setpoint = zone.thermalProperties.setpoint;
  const deadband = zone.thermalProperties.deadband;

  const thickness = 3.8;
  const cx = zone.centroid.x - ORIGIN.x;
  const cy = -(zone.centroid.y - ORIGIN.y);

  const geometry = useMemo(() => {
    const shape = new THREE.Shape();
    zone.polygon.forEach((p, idx) => {
      if (idx === 0) shape.moveTo(p[0] - ORIGIN.x, p[1] - ORIGIN.y);
      else shape.lineTo(p[0] - ORIGIN.x, p[1] - ORIGIN.y);
    });
    shape.lineTo(zone.polygon[0][0] - ORIGIN.x, zone.polygon[0][1] - ORIGIN.y);
    
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

  const isPhysical = viewMode === 'physical';

  const material = useMemo(() => {
    return new THREE.ShaderMaterial({
      uniforms: {
        temperature: { value: temperature },
        setpoint: { value: setpoint },
        deadband: { value: deadband },
        opacity: { value: isActive ? (isHovered ? 0.9 : 0.65) : 0.15 },
        isPhysical: { value: isPhysical ? 1.0 : 0.0 },
        lightsOn: { value: 1.0 } // 1 lit, 0 dark — actuated lighting streamed from the engine
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
        uniform float isPhysical;
        uniform float lightsOn;
        varying vec2 vUv;
        
        vec3 heatmap(float deviation) {
          float amount = clamp(deviation, -1.0, 3.0);
          vec3 cool = vec3(0.0, 0.5, 1.0);  // Blue
          vec3 good = vec3(0.0, 1.0, 0.0);  // Green
          vec3 warn = vec3(1.0, 1.0, 0.0);  // Yellow
          vec3 hot = vec3(1.0, 0.0, 0.0);   // Red
          
          if (amount < 0.0) return mix(good, cool, -amount);
          if (amount < 1.0) return mix(good, warn, amount);
          return mix(warn, hot, min(1.0, amount - 1.0));
        }
        
        void main() {
          float deviation = (temperature - setpoint) / deadband;
          vec3 heatColor = heatmap(deviation);
          vec3 physColor = vec3(0.2, 0.2, 0.2);
          vec3 finalColor = mix(heatColor, physColor, isPhysical);
          // Lights-off zones go visibly dark (engine setback / manual veto).
          finalColor *= mix(0.3, 1.0, lightsOn);
          float finalOpacity = mix(opacity, opacity * 0.1, isPhysical);
          gl_FragColor = vec4(finalColor, finalOpacity);
        }
      `,
      transparent: true,
      side: THREE.DoubleSide,
      depthWrite: false,
    });
  }, [isActive, isHovered, isPhysical]);

  // Per-frame work is the dominant CPU cost (126 zones × 60 fps). Only the active floor and
  // alarmed zones need the smooth lerp + pulse every frame; the ~110 faded inactive zones
  // (opacity 0.15) just snap their uniform when the live temp actually moves, so the bulk of
  // the loop early-returns. No element is removed — only the redundant updates are.
  const lastLive = useRef(null);
  const lastLights = useRef(null);
  useFrame((state) => {
    if (!material || !material.uniforms.temperature) return;
    const liveTemp = simState.zones[zone.zoneId]?.temp || setpoint;
    const liveLights = simState.zones[zone.zoneId]?.lightsOn === false ? 0.0 : 1.0;
    if (isActive || alertState === true) {
      material.uniforms.temperature.value = THREE.MathUtils.lerp(material.uniforms.temperature.value, liveTemp, 0.05);
      material.uniforms.lightsOn.value = THREE.MathUtils.lerp(material.uniforms.lightsOn.value, liveLights, 0.1);
      material.uniforms.opacity.value = alertState === true
        ? 0.65 + 0.3 * Math.sin(state.clock.elapsedTime * 8)
        : (isHovered ? 0.9 : 0.65);
    } else if (lastLive.current === null || Math.abs(lastLive.current - liveTemp) > 0.05
               || lastLights.current !== liveLights) {
      material.uniforms.temperature.value = liveTemp;
      material.uniforms.lightsOn.value = liveLights;
      material.uniforms.opacity.value = 0.15;
      lastLive.current = liveTemp;
      lastLights.current = liveLights;
    }
  });

  return (
    <group position={[0, 0.01, 0]}>
      <mesh 
        ref={meshRef} 
        geometry={geometry}
        onClick={(e) => {
          if (isActive) {
            e.stopPropagation();
            onSelect(zone.zoneId);
          }
        }}
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
        <Edges color={(isHovered || isSelected) && isActive ? "#ffffff" : (alertState === true ? "#ff0000" : "#222222")} threshold={15} />
      </mesh>

      {/* VIRTUAL IOT SENSORS (Only visible when drilled down into the room) */}
      {isActive && isSelected && (
        <group position={[cx, 1.5, cy]}>
          {/* Smart Thermostat */}
          <mesh position={[-1.5, 0, 0]}>
            <boxGeometry args={[0.3, 0.5, 0.1]} />
            <meshBasicMaterial color="#00e5ff" />
            <pointLight distance={3} intensity={0.5} color="#00e5ff" />
          </mesh>
          {/* Air Quality / CO2 Monitor */}
          <mesh position={[1.5, 0, 0]}>
            <boxGeometry args={[0.4, 0.3, 0.1]} />
            <meshBasicMaterial color="#00ff00" />
            <pointLight distance={3} intensity={0.5} color="#00ff00" />
          </mesh>
          {/* Ceiling Occupancy Camera */}
          <mesh position={[0, 2.0, 0]}>
            <sphereGeometry args={[0.2, 16, 16]} />
            <meshBasicMaterial color="#ff00ff" />
          </mesh>
        </group>
      )}

      {isActive && alertState && (
        <mesh position={[cx, 15, cy]}>
          <cylinderGeometry args={[1, 1, 30, 16]} />
          <meshBasicMaterial 
            color={alertState === 'REMEDIATING' ? "#ffff00" : "#ff0000"} 
            transparent 
            opacity={0.3} 
            blending={THREE.AdditiveBlending} 
            depthWrite={false} 
          />
        </mesh>
      )}
      
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

// The FIXED hero angle for the building: a front-left 3/4 view, moderately elevated.
// Azimuth 45° puts the camera in the +x/+z octant; elevation 26° looks gently down so the
// exploded active floor's top reads. Distance is aspect-aware so the WHOLE exploded tower
// fits the viewport at any shape (wide desktop or tall mobile/portrait) without cropping.
const VIEW_AZ = THREE.MathUtils.degToRad(45);
const VIEW_EL = THREE.MathUtils.degToRad(26);
const VIEW_DIR = new THREE.Vector3(
  Math.cos(VIEW_EL) * Math.sin(VIEW_AZ),
  Math.sin(VIEW_EL),
  Math.cos(VIEW_EL) * Math.cos(VIEW_AZ),
);

// safeArea describes the part of the canvas the operator can actually SEE — the console
// paints the AI panel, the metrics dock, the topology window and the airflow window over
// the 3D view, and framing the building to the full canvas centres it behind them. It is
// {left, right, top, bottom} in pixels, plus the viewport size. Absent (mobile, or any
// caller with no overlays), framing falls back to the whole canvas exactly as before.
export function towerFraming(activeFloor, aspect = 1.6, safeArea = null) {
  const floors = buildingData.floors;
  const dispElev = (f) => f.elevation + (f.level > activeFloor ? 30 : (f.level === activeFloor ? 5 : 0));
  let topY = -Infinity, botY = Infinity, activeY = 0;
  floors.forEach((f) => {
    const e = dispElev(f);
    topY = Math.max(topY, e + (f.height || 4));
    botY = Math.min(botY, e);
    if (f.level === activeFloor) activeY = e + (f.height || 4) / 2;
  });
  // The building is drawn about the world origin (see floorGeometry.ORIGIN), so that is
  // what the camera aims at. This used to read `{x: -20, z: -20}` with a comment noting
  // that "the 60×40 plate therefore centres on x=-20, z=-20" — correct for the office
  // fixture it was written against, and for no other building. A 13.6 × 5.5 m house was
  // framed twenty metres off-axis inside a sphere sized for a tower, which on screen is
  // indistinguishable from the model failing to render.
  const center = { x: 0, z: 0 };

  // span is the building's TRUE vertical extent, floored only so the arithmetic stays
  // well-conditioned for a single low floor. It is deliberately NOT used to place the aim
  // point: doing that (`botY + span * 0.46` against a span floored at 8) pointed the camera
  // at 8.68 m on a house whose roof is at 7.8 m — above the building, at nothing.
  const span = Math.max(topY - botY, 0.5);

  // Bounding sphere of the exploded tower, from the building's REAL footprint and real
  // height rather than the 60×40×span half-extents assumed here before.
  const R = Math.hypot(FOOTPRINT.width / 2, span / 2, FOOTPRINT.depth / 2);
  const vFov = (45 * Math.PI) / 180;
  // Fit to the FREE band, not the whole canvas. On the desktop console the visible strip
  // between the AI panel and the metrics dock is barely half the canvas width, so a
  // building fitted to the canvas is half-hidden the moment it is framed "correctly".
  const vw = safeArea?.viewportW || 0;
  const vh = safeArea?.viewportH || 0;
  const freeW = safeArea ? Math.max(200, vw - safeArea.left - safeArea.right) : 0;
  const freeH = safeArea ? Math.max(200, vh - safeArea.top - safeArea.bottom) : 0;
  const fitAspect = safeArea && freeH > 0 ? freeW / freeH : aspect;

  const hFov = 2 * Math.atan(Math.tan(vFov / 2) * Math.max(0.3, fitAspect));
  const fitFov = Math.min(vFov, hFov);
  // Portrait phones stack the exploded tower tall; at the desktop framing its top covers the
  // sky graphic (sun/moon) and the header. On portrait, pull the camera back a little for
  // margin and raise the aim point so the whole building sits in the lower ~75% of the frame,
  // leaving the top strip clear. Landscape/desktop keep the original tight framing.
  const portrait = aspect < 1;
  // Margin. Fitting the bounding sphere exactly to the canvas puts the building edge-to-
  // edge, and on the desktop console the canvas is not what the operator can see: the AI
  // panel and the metrics dock permanently overlay roughly a quarter of the width each,
  // so an edge-to-edge building is half hidden behind them. Pulling back leaves it inside
  // the unobscured centre band. Zoom stays enabled for a closer look.
  // With a real safe area the fit already accounts for the overlays, so only a small
  // breathing margin is wanted. Without one, keep the generous fallback.
  // The sphere fit guarantees a SPHERE of radius R inside the narrower fov; the building
  // is a box, whose projected corners reach further than that sphere in the wider
  // dimension. 1.15 covers the worst case measured across panel arrangements (the box
  // overran the free band by 12 px at 1.08 with the lower windows closed) and still leaves
  // the building filling most of the visible strip.
  const margin = portrait ? 1.2 : (safeArea ? 1.15 : 1.45);
  // A floor under the DISTANCE, not under the extents: it keeps a small building out of
  // the near plane and stops a single zone filling the frame, without moving the aim.
  const dist = Math.max((R / Math.sin(fitFov / 2)) * margin, 14);
  const aimBias = portrait ? span * 0.16 : 0; // raise look-at -> building drops in frame

  // Aim at the vertical middle of what is actually there.
  const target = new THREE.Vector3(center.x, (topY + botY) / 2 + aimBias, center.z);
  const position = target.clone().add(VIEW_DIR.clone().multiplyScalar(dist));

  // Slide the whole camera so the building lands in the middle of the FREE band rather
  // than the middle of the canvas. Panning camera and target together preserves the fixed
  // hero angle — only the framing moves. The shift is computed in world units from the
  // frustum size at this distance, so it stays correct at any viewport or panel width.
  if (safeArea && vw > 0 && vh > 0) {
    const halfH = dist * Math.tan(vFov / 2);
    const worldPerPxY = (2 * halfH) / vh;
    const worldPerPxX = (2 * halfH * aspect) / vw;
    // Centre of the free band minus centre of the canvas, in pixels.
    const dxPx = (safeArea.left + (vw - safeArea.right)) / 2 - vw / 2;
    const dyPx = (safeArea.top + (vh - safeArea.bottom)) / 2 - vh / 2;
    // VIEW_DIR points from the target TO the camera, so the camera's forward is its
    // negation. Building the right-vector from VIEW_DIR directly yields the camera's LEFT,
    // which slid the framing the wrong way.
    const forward = VIEW_DIR.clone().negate().normalize();
    const camRight = new THREE.Vector3().crossVectors(forward, new THREE.Vector3(0, 1, 0)).normalize();
    const camUp = new THREE.Vector3().crossVectors(camRight, forward).normalize();
    // The image moves opposite to the camera: to place the building left of the canvas
    // centre (dxPx < 0), the camera moves right.
    const shift = camRight.multiplyScalar(-dxPx * worldPerPxX).add(camUp.multiplyScalar(dyPx * worldPerPxY));
    target.add(shift);
    position.add(shift);
  }

  return { position, target, span, topY, botY, activeY };
}

function DynamicControls({ targetX, targetY, targetZ, isZoomed, activeFloor, safeArea }) {
  const controlsRef = useRef();
  const { camera, size } = useThree();
  const aspect = size.width / Math.max(1, size.height);

  // The fixed hero overview, recomputed when the attention floor OR the viewport shape
  // changes (so it stays correctly framed across desktop/mobile and on rotate/resize).
  const overview = useMemo(
    () => towerFraming(activeFloor, aspect, safeArea),
    [activeFloor, aspect, safeArea?.left, safeArea?.right, safeArea?.top, safeArea?.bottom, safeArea?.viewportW, safeArea?.viewportH],
  ); // eslint-disable-line

  const [animating, setAnimating] = useState(false);
  const [targetCameraPos, setTargetCameraPos] = useState(overview.position);
  const [targetLookAt, setTargetLookAt] = useState(overview.target);

  useEffect(() => {
    if (isZoomed) {
      // Drill-down: rise above and look down at the selected zone on the active floor.
      //
      // The offsets scale with the building. They were the literals (15, 28, 15) — a
      // sensible vantage over a 60×40 m office plate and roughly four storeys above a
      // 13.6×5.5 m house, which put the selected room in the far distance and looked
      // exactly like the model failing to frame. Derived from the footprint, the same
      // gesture reads the same way at either scale.
      const reach = Math.max(FOOTPRINT.width, FOOTPRINT.depth) * 0.35;
      const lateral = Math.max(reach, 4);
      const rise = Math.max(reach * 1.9, 8);
      setTargetCameraPos(new THREE.Vector3(targetX + lateral, targetY + rise, targetZ + lateral));
      setTargetLookAt(new THREE.Vector3(targetX, targetY, targetZ));
      setAnimating(true);
    } else {
      setTargetCameraPos(overview.position);
      setTargetLookAt(overview.target);
      setAnimating(true);
    }
  }, [isZoomed, targetX, targetY, targetZ, overview]);

  useFrame(() => {
    if (controlsRef.current && animating) {
      controlsRef.current.target.lerp(targetLookAt, 0.08);
      camera.position.lerp(targetCameraPos, 0.08);

      if (camera.position.distanceTo(targetCameraPos) < 1.5) {
        setAnimating(false);
      }
      controlsRef.current.update();
    }
  });

  // Rotation is LOCKED so the building stays at the fixed hero angle (per request); zoom
  // stays enabled for inspection, pan disabled to keep it centred. Drill-down still works
  // because we drive the camera position/target directly.
  return (
    <OrbitControls
      ref={controlsRef}
      target={[overview.target.x, overview.target.y, overview.target.z]}
      makeDefault
      enableRotate={false}
      enablePan={false}
      enableZoom
    />
  );
}

export function SingleFloorLayout({ floor, isActive, simState, activeScenario, faultTarget, onFloorClick, selectedZone, setSelectedZone, hoveredZone, setHoveredZone, viewMode = 'hybrid' }) {
  return (
    <>
      <FloorPlate floor={floor} isActive={isActive} onClick={onFloorClick} simState={simState} viewMode={viewMode} />
      {isActive && <ExteriorWalls floor={floor} isActive={isActive} viewMode={viewMode} />}
      <group>
        {floor.zones.map((zone) => (
          <ZoneRenderer
            key={zone.zoneId}
            zone={zone}
            isActive={isActive}
            simState={simState}
            isHovered={hoveredZone === zone.zoneId}
            onHover={setHoveredZone}
            isSelected={selectedZone === zone.zoneId}
            onSelect={setSelectedZone}
            viewMode={viewMode}
          />
        ))}
      </group>
      {isActive && <FloorInfrastructure floor={floor} simState={simState} viewMode={viewMode} />}
    </>
  );
}

// ========== STEP 5: Complete Production Building Component ==========
export default function BuildingModel({ simState, activeFloor, onFloorClick, showAirflow, selectedZone, setSelectedZone, viewMode = 'hybrid', safeArea = null }) {
  const [hoveredZone, setHoveredZone] = useState(null);
  const floors = buildingData.floors;

  const targetCoords = useMemo(() => {
    if (!selectedZone) return { x: 0, y: 0, z: 0 };
    for (const f of floors) {
      const z = f.zones.find(zone => zone.zoneId === selectedZone);
      if (z) {
        let yOffset = f.level > activeFloor ? 30.0 : (f.level === activeFloor ? 5.0 : 0.0);
        // Must mirror the render transform exactly — and now it does so by CALLING it,
        // rather than restating it as (px−50, ·, −py) and drifting the moment either
        // recentring changed. toWorld is the one definition of that mapping.
        const [wx, wz] = toWorld([z.centroid.x, z.centroid.y]);
        return {
          x: wx,
          y: f.elevation + yOffset + 1.5,
          z: wz
        };
      }
    }
    return { x: 0, y: 0, z: 0 };
  }, [selectedZone, activeFloor, floors]);

  // Frame the whole tower at the fixed hero angle for the very first paint, using the live
  // viewport aspect so it's correctly framed on both desktop and mobile from the start.
  const initialOverview = useMemo(() => {
    const aspect = (typeof window !== 'undefined' ? window.innerWidth / Math.max(1, window.innerHeight) : 1.6);
    return towerFraming(activeFloor, aspect, safeArea);
  }, []); // eslint-disable-line

  return (
    <div style={{ width: '100%', height: '100%', position: 'absolute', top: 0, left: 0, zIndex: 1 }}>
      <Canvas
        camera={{ position: [initialOverview.position.x, initialOverview.position.y, initialOverview.position.z], fov: 45 }}
        frameloop="always"
        dpr={[1, 1.5]}
        gl={{ antialias: true, powerPreference: 'high-performance' }}
        onCreated={({ gl, invalidate }) => {
          // Recover from WebGL context loss instead of leaving the building permanently black.
          // A heavy scene (14 floors, ~290 meshes + CSG) can trip GPU memory pressure / tab
          // suspend, which drops the context; without these handlers the canvas never repaints.
          const canvas = gl.domElement;
          canvas.addEventListener('webglcontextlost', (e) => { e.preventDefault(); }, false);
          canvas.addEventListener('webglcontextrestored', () => { invalidate(); }, false);
        }}
      >
        {/* Transparent background for weather overlay */}
        <ambientLight intensity={0.4} />
        <directionalLight position={[10, 20, 10]} intensity={1.2} />
        
        <DynamicControls
          targetX={targetCoords.x}
          targetY={targetCoords.y}
          targetZ={targetCoords.z}
          isZoomed={!!selectedZone}
          activeFloor={activeFloor}
          safeArea={safeArea}
        />

        {/* No recentring here any more. The tower used to be recentred TWICE for the
            60×40 office plate: once per-point as (px−20, py−20) inside every shape, and
            again by this group at (−30, 0, −20) — together putting world x at px−50 and
            world z at −py. Shapes now place themselves about the building's own footprint
            centre (floorGeometry.ORIGIN), so a second offset would move the model back off
            the origin the camera aims at, which is exactly what it did. */}
        <group>
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
                <SingleFloorLayout
                  floor={floor}
                  isActive={isActive}
                  simState={simState}
                  selectedZone={selectedZone}
                  setSelectedZone={setSelectedZone}
                  hoveredZone={hoveredZone}
                  setHoveredZone={setHoveredZone}
                  onFloorClick={onFloorClick}
                  viewMode={viewMode}
                />
              </group>
            );
          })}
        </group>

        {/* No ground grid. It was a 100 x 100 m gridHelper — another extent sized for the
            office plate — and at the fixed hero camera angle it filled the frame with a
            diagonal crosshatch behind the building, competing with the model for attention
            and reading as part of the scene rather than as a reference. The building now
            sits against the live sky background alone. */}
      </Canvas>
    </div>
  );
}
