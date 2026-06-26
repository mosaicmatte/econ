import React, { useMemo, useRef } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';

// Lightweight 3D value-noise (no external deps) used to drive a turbulent,
// aerodynamic-looking velocity field for the airflow particles.
function makeNoise() {
  const fract = (v) => v - Math.floor(v);
  const hash = (x, y, z) => fract(Math.sin(x * 127.1 + y * 311.7 + z * 74.7) * 43758.5453);
  const smooth = (t) => t * t * (3 - 2 * t);
  const lerp = (a, b, t) => a + (b - a) * t;
  return (x, y, z) => {
    const xi = Math.floor(x), yi = Math.floor(y), zi = Math.floor(z);
    const xf = x - xi, yf = y - yi, zf = z - zi;
    const u = smooth(xf), v = smooth(yf), w = smooth(zf);
    const c = (i, j, k) => hash(xi + i, yi + j, zi + k);
    const x00 = lerp(c(0, 0, 0), c(1, 0, 0), u), x10 = lerp(c(0, 1, 0), c(1, 1, 0), u);
    const x01 = lerp(c(0, 0, 1), c(1, 0, 1), u), x11 = lerp(c(0, 1, 1), c(1, 1, 1), u);
    return lerp(lerp(x00, x10, v), lerp(x01, x11, v), w) * 2 - 1; // [-1, 1]
  };
}

// Renders flowing air inside one floor's volume: supply air descends from the
// ceiling diffusers, fans out across the floor, recirculates up the walls and is
// recycled — giving an aerodynamic sense of airflow in 3D. Lives inside the
// floor's local group, so coordinates are centered the same way as the zones
// (x,z in [-20,20], y in [0, floorHeight]).
export default function AirflowField({ floor, intensity = 1.0 }) {
  const ref = useRef();
  const COUNT = 1500;
  const noise = useMemo(() => makeNoise(), []);

  const { bounds, positions, colors } = useMemo(() => {
    const xs = floor.geometry.exteriorPolygon.map((p) => p[0] - 20);
    const zs = floor.geometry.exteriorPolygon.map((p) => 20 - p[1]);
    const b = {
      minX: Math.min(...xs), maxX: Math.max(...xs),
      minZ: Math.min(...zs), maxZ: Math.max(...zs),
      maxY: floor.height || 4,
    };
    const positions = new Float32Array(COUNT * 3);
    const colors = new Float32Array(COUNT * 3);
    for (let i = 0; i < COUNT; i++) {
      positions[i * 3] = THREE.MathUtils.lerp(b.minX, b.maxX, Math.random());
      positions[i * 3 + 1] = Math.random() * b.maxY;
      positions[i * 3 + 2] = THREE.MathUtils.lerp(b.minZ, b.maxZ, Math.random());
    }
    return { bounds: b, positions, colors };
  }, [floor]);

  const geometry = useMemo(() => {
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.BufferAttribute(positions, 3));
    g.setAttribute('color', new THREE.BufferAttribute(colors, 3));
    return g;
  }, [positions, colors]);

  useFrame((state, delta) => {
    const dt = Math.min(delta, 0.05);
    const t = state.clock.elapsedTime * 0.25;
    const b = bounds;
    const pos = positions, col = colors;
    const s = 0.12;       // spatial noise scale
    const spread = 5.0;   // horizontal turbulence strength
    const supply = 2.2;   // downward supply velocity from ceiling

    for (let i = 0; i < COUNT; i++) {
      const ix = i * 3;
      let x = pos[ix], y = pos[ix + 1], z = pos[ix + 2];

      // Turbulent velocity field (offset seeds -> decorrelated components)
      let vx = noise(x * s, y * s + t, z * s) * spread;
      let vy = noise(x * s + 41.2, y * s + t, z * s + 9.7) * spread * 0.5 - supply;
      let vz = noise(x * s + 17.3, y * s + t, z * s + 53.1) * spread;

      // Recirculation: near the perimeter, air rises back toward the ceiling
      const edge = Math.max(Math.abs(x) / b.maxX, Math.abs(z) / b.maxZ);
      if (edge > 0.72) vy += supply * 1.8;

      vx *= intensity; vy *= intensity; vz *= intensity;
      x += vx * dt; y += vy * dt; z += vz * dt;

      // Recycle particles that leave the room back to the ceiling supply
      if (y < 0.1 || y > b.maxY || x < b.minX || x > b.maxX || z < b.minZ || z > b.maxZ) {
        x = THREE.MathUtils.lerp(b.minX * 0.6, b.maxX * 0.6, Math.random());
        y = b.maxY * 0.96;
        z = THREE.MathUtils.lerp(b.minZ * 0.6, b.maxZ * 0.6, Math.random());
      }
      pos[ix] = x; pos[ix + 1] = y; pos[ix + 2] = z;

      // Colour by speed: slow = deep blue supply, fast = bright cyan/white
      const speed = Math.min(1, Math.hypot(vx, vy, vz) / 6);
      col[ix] = THREE.MathUtils.lerp(0.0, 0.85, speed);
      col[ix + 1] = THREE.MathUtils.lerp(0.7, 1.0, speed);
      col[ix + 2] = 1.0;
    }

    if (ref.current) {
      ref.current.geometry.attributes.position.needsUpdate = true;
      ref.current.geometry.attributes.color.needsUpdate = true;
    }
  });

  return (
    <points ref={ref} geometry={geometry} frustumCulled={false}>
      <pointsMaterial
        size={1.6}
        vertexColors
        transparent
        opacity={1.0}
        sizeAttenuation
        depthWrite={false}
        blending={THREE.AdditiveBlending}
      />
    </points>
  );
}
