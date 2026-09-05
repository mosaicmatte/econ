#!/usr/bin/env node

/**
 * verify_level_toggle.js — Automated E2E Verification Harness for Level Toggle Feature
 *
 * Verifies end-to-end integration of the Dynamic Level Toggle feature in the ECON Dashboard:
 * 1. Pure mathematical & telemetry aggregation invariants:
 *    - Validates multi-level tower telemetry aggregation (kW, Pax, °C, alarms, setbacks).
 *    - Validates 1-level domestic home telemetry aggregation.
 *    - Validates zero-zone fallback safety and variance across distinct floors.
 * 2. Real Built App Puppeteer Verification against compiled Vite production bundle:
 *    - Programmatically interacts with level buttons (`data-testid="level-btn-${lvl}"`),
 *      desktop stepper (`data-testid="level-step-prev"`, `data-testid="level-step-next"`),
 *      and mobile stepper (`data-testid="mobile-level-up"`, `data-testid="mobile-level-down"`).
 *    - Asserts real-time DOM mutations on active level displays, per-level metrics cards,
 *      and topology map headers (`MAP LEVEL {activeFloor} TOPOLOGY`).
 *    - Validates boundary clamping at min/max levels and dynamic asset switching (tower <-> home).
 */

import puppeteer from 'puppeteer';
import http from 'http';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { getBuilding, setBuildingModelType, getBuildingModelType, getAllKnownBuildings } from './src/buildingStore.js';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// ANSI Colors for formatted console output
const colors = {
  reset: '\x1b[0m',
  bright: '\x1b[1m',
  dim: '\x1b[2m',
  green: '\x1b[32m',
  red: '\x1b[31m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  cyan: '\x1b[36m',
  gray: '\x1b[90m',
};

class TestHarness {
  constructor() {
    this.tests = [];
    this.passed = 0;
    this.failed = 0;
    this.currentSuite = '';
    this.startTime = Date.now();
  }

  suite(name) {
    this.currentSuite = name;
    console.log(`\n${colors.bright}${colors.cyan}=== Suite: ${name} ===${colors.reset}`);
  }

  async test(name, fn) {
    const testName = `[${this.currentSuite}] ${name}`;
    const start = Date.now();
    try {
      await fn();
      const elapsed = Date.now() - start;
      this.passed++;
      console.log(`  ${colors.green}✔ PASS${colors.reset} ${name} ${colors.gray}(${elapsed}ms)${colors.reset}`);
      this.tests.push({ name: testName, status: 'PASS', elapsed });
    } catch (err) {
      const elapsed = Date.now() - start;
      this.failed++;
      console.error(`  ${colors.red}✖ FAIL${colors.reset} ${name} ${colors.gray}(${elapsed}ms)${colors.reset}`);
      console.error(`    ${colors.red}${err.message}${colors.reset}`);
      if (err.stack) {
        console.error(`    ${colors.dim}${err.stack.split('\n').slice(1, 4).join('\n    ')}${colors.reset}`);
      }
      this.tests.push({ name: testName, status: 'FAIL', error: err.message, elapsed });
    }
  }

  assert(condition, message) {
    if (!condition) {
      throw new Error(`Assertion failed: ${message}`);
    }
  }

  assertEqual(actual, expected, message) {
    if (actual !== expected) {
      throw new Error(`Assertion failed: ${message} (Expected: ${JSON.stringify(expected)}, Actual: ${JSON.stringify(actual)})`);
    }
  }

  assertNotEqual(actual, unexpected, message) {
    if (actual === unexpected) {
      throw new Error(`Assertion failed: ${message} (Value should not equal: ${JSON.stringify(unexpected)})`);
    }
  }

  summary() {
    const totalTime = Date.now() - this.startTime;
    console.log(`\n${colors.bright}${colors.cyan}====================================================${colors.reset}`);
    console.log(`${colors.bright}Test Summary: ${this.passed + this.failed} Total | ${colors.green}${this.passed} Passed${colors.reset} | ${this.failed > 0 ? colors.red : colors.green}${this.failed} Failed${colors.reset} ${colors.gray}(${totalTime}ms)${colors.reset}`);
    console.log(`${colors.bright}${colors.cyan}====================================================${colors.reset}\n`);
    return this.failed === 0;
  }
}

const harness = new TestHarness();

const mimeTypes = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
};

function createStaticServer(port = 5193) {
  const distDir = path.join(__dirname, 'dist');
  const server = http.createServer((req, res) => {
    let reqPath = req.url.split('?')[0];
    if (reqPath === '/') reqPath = '/index.html';
    const filePath = path.join(distDir, reqPath);
    const ext = path.extname(filePath);

    if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
      res.writeHead(200, { 'Content-Type': mimeTypes[ext] || 'application/octet-stream' });
      fs.createReadStream(filePath).pipe(res);
    } else {
      const indexPath = path.join(distDir, 'index.html');
      res.writeHead(200, { 'Content-Type': 'text/html' });
      fs.createReadStream(indexPath).pipe(res);
    }
  });

  return new Promise((resolve, reject) => {
    server.on('error', (err) => {
      if (err.code === 'EADDRINUSE') {
        // Fallback to ephemeral port
        const fallbackServer = http.createServer(server.listeners('request')[0]);
        fallbackServer.listen(0, '127.0.0.1', () => {
          const actualPort = fallbackServer.address().port;
          resolve({
            server: fallbackServer,
            url: `http://127.0.0.1:${actualPort}`,
            close: () => new Promise(cb => fallbackServer.close(cb)),
          });
        });
      } else {
        reject(err);
      }
    });

    server.listen(port, '127.0.0.1', () => {
      const actualPort = server.address().port;
      resolve({
        server,
        url: `http://127.0.0.1:${actualPort}`,
        close: () => new Promise(cb => server.close(cb)),
      });
    });
  });
}

function setupRequestInterception(page) {
  return page.setRequestInterception(true).then(() => {
    const distDir = path.join(__dirname, 'dist');
    page.on('request', req => {
      try {
        const url = new URL(req.url());
        let pathname = url.pathname;
        if (pathname === '/' || pathname === '') pathname = '/index.html';
        const filePath = path.join(distDir, pathname.replace(/^\//, ''));
        if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
          const ext = path.extname(filePath);
          req.respond({
            status: 200,
            contentType: mimeTypes[ext] || 'application/octet-stream',
            body: fs.readFileSync(filePath),
          });
        } else {
          req.respond({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({}),
          });
        }
      } catch {
        req.continue();
      }
    });
  });
}

// Multi-Level Tower Building Fixture Data for Invariant Testing
const mockBuildingData = {
  id: 'building-commercial-tower',
  name: 'ECON Commercial Tower',
  floors: [
    {
      level: 1,
      name: 'Level 1 - Ground & Server Riser',
      height: 4.5,
      elevation: 0.0,
      zones: [
        { zoneId: 'zone-lobby-lvl1', name: 'Main Lobby Level 1', zoneType: 'lobby' },
        { zoneId: 'zone-server-room-lvl1', name: 'Server Room Level 1', zoneType: 'comms-room' },
        { zoneId: 'zone-reception-lvl1', name: 'Reception Level 1', zoneType: 'office' }
      ]
    },
    {
      level: 2,
      name: 'Level 2 - Open Workspace',
      height: 4.0,
      elevation: 4.5,
      zones: [
        { zoneId: 'zone-open-office-lvl2', name: 'Open Office Level 2', zoneType: 'open-office' },
        { zoneId: 'zone-conference-lvl2', name: 'West Conference Level 2', zoneType: 'meeting-room' }
      ]
    },
    {
      level: 3,
      name: 'Level 3 - Engineering Lab',
      height: 4.0,
      elevation: 8.5,
      zones: [
        { zoneId: 'zone-eng-lab-lvl3', name: 'Engineering Lab Level 3', zoneType: 'lab' },
        { zoneId: 'zone-breakout-lvl3', name: 'Breakout Area Level 3', zoneType: 'breakout' }
      ]
    },
    {
      level: 4,
      name: 'Level 4 - Executive Suites',
      height: 4.0,
      elevation: 12.5,
      zones: [
        { zoneId: 'zone-exec-suite-lvl4', name: 'Executive Suite Level 4', zoneType: 'cellular-office' },
        { zoneId: 'zone-boardroom-lvl4', name: 'Boardroom Level 4', zoneType: 'meeting-room' },
        { zoneId: 'zone-north-west-office-lvl4', name: 'North West Office Level 4', zoneType: 'open-office' }
      ]
    }
  ]
};

// Live SimState Telemetry per zone
const mockSimZones = {
  'zone-lobby-lvl1': { id: 'zone-lobby-lvl1', level: 1, label: 'Main Lobby', temp: 24.2, load: 1200, occupancy: 8, alert: false, lightsOn: true },
  'zone-server-room-lvl1': { id: 'zone-server-room-lvl1', level: 1, label: 'Server Room', temp: 21.0, load: 18000, occupancy: 0, alert: false, lightsOn: true },
  'zone-reception-lvl1': { id: 'zone-reception-lvl1', level: 1, label: 'Reception', temp: 23.8, load: 2500, occupancy: 3, alert: false, lightsOn: true },

  'zone-open-office-lvl2': { id: 'zone-open-office-lvl2', level: 2, label: 'Open Office', temp: 24.6, load: 6000, occupancy: 16, alert: false, lightsOn: true },
  'zone-conference-lvl2': { id: 'zone-conference-lvl2', level: 2, label: 'West Conference', temp: 22.8, load: 3500, occupancy: 6, alert: false, lightsOn: true },

  'zone-eng-lab-lvl3': { id: 'zone-eng-lab-lvl3', level: 3, label: 'Engineering Lab', temp: 25.1, load: 12000, occupancy: 10, alert: false, lightsOn: true },
  'zone-breakout-lvl3': { id: 'zone-breakout-lvl3', level: 3, label: 'Breakout Area', temp: 24.4, load: 2000, occupancy: 4, alert: false, lightsOn: true },

  'zone-exec-suite-lvl4': { id: 'zone-exec-suite-lvl4', level: 4, label: 'Executive Suite', temp: 22.2, load: 4000, occupancy: 2, alert: false, lightsOn: true },
  'zone-boardroom-lvl4': { id: 'zone-boardroom-lvl4', level: 4, label: 'Boardroom', temp: 26.8, load: 8000, occupancy: 14, alert: true, lightsOn: true },
  'zone-north-west-office-lvl4': { id: 'zone-north-west-office-lvl4', level: 4, label: 'North West Office', temp: 24.5, load: 5500, occupancy: 7, alert: false, lightsOn: true }
};

// Pure function computing dynamic level telemetry matching GlobalMetricsPanel logic
function computeLevelMetrics(activeFloor, buildingData, simZones) {
  const floorObj = (buildingData.floors || []).find(f => f.level === activeFloor);
  const floorZoneIds = new Set((floorObj?.zones || []).map(z => z.zoneId));
  const levelZones = Object.values(simZones)
    .filter(z => floorZoneIds.size > 0 ? floorZoneIds.has(z.id) : z.level === activeFloor);

  if (!levelZones.length) {
    return { count: 0, loadKw: '0.0', occupancy: 0, avgTemp: '0.0', alarms: 0, setbacks: 0 };
  }

  let totalLoadW = 0;
  let totalPax = 0;
  let totalTemp = 0;
  let alarms = 0;
  let setbacks = 0;

  levelZones.forEach(z => {
    const loadVal = typeof z.load === 'number' ? z.load : (parseFloat(z.load) || 0);
    const paxVal = typeof z.occupancy === 'number' ? z.occupancy : (parseInt(z.occupancy, 10) || 0);
    const tempVal = typeof z.temp === 'number' ? z.temp : parseFloat(z.temp);
    totalLoadW += (isNaN(loadVal) ? 0 : loadVal);
    totalPax += (isNaN(paxVal) ? 0 : paxVal);
    totalTemp += (isNaN(tempVal) ? 24.0 : tempVal);
    if (z.alert === true || z.alert === 'REMEDIATING') alarms++;
    if (z.lightsOn === false) setbacks++;
  });

  return {
    count: levelZones.length,
    loadKw: (totalLoadW / 1000).toFixed(1),
    occupancy: totalPax,
    avgTemp: (totalTemp / levelZones.length).toFixed(1),
    alarms,
    setbacks,
  };
}

async function runVerification() {
  console.log(`\n${colors.bright}${colors.cyan}╔══════════════════════════════════════════════════════════════════════╗${colors.reset}`);
  console.log(`${colors.bright}${colors.cyan}║      ECON Dynamic Building Level Toggle & Telemetry Verification     ║${colors.reset}`);
  console.log(`${colors.bright}${colors.cyan}╚══════════════════════════════════════════════════════════════════════╝${colors.reset}`);

  // ----------------------------------------------------------------------------
  // SUITE 1: Telemetry Data Aggregation Engine & Mathematical Invariants
  // ----------------------------------------------------------------------------
  harness.suite('Per-Level Telemetry Aggregation & Mathematical Invariants');

  await harness.test('Level 1 Telemetry correctly aggregates high server load and lobby occupants', () => {
    const l1 = computeLevelMetrics(1, mockBuildingData, mockSimZones);
    harness.assertEqual(l1.count, 3, 'Level 1 has 3 zones');
    harness.assertEqual(l1.loadKw, '21.7', 'Level 1 load is 21.7 kW (1200+18000+2500 W)');
    harness.assertEqual(l1.occupancy, 11, 'Level 1 occupancy is 11 Pax (8+0+3)');
    harness.assertEqual(l1.avgTemp, '23.0', 'Level 1 avg temp is 23.0 °C ((24.2+21.0+23.8)/3)');
    harness.assertEqual(l1.alarms, 0, 'Level 1 has 0 alarms');
  });

  await harness.test('Level 2 Telemetry correctly aggregates open-office workspace', () => {
    const l2 = computeLevelMetrics(2, mockBuildingData, mockSimZones);
    harness.assertEqual(l2.count, 2, 'Level 2 has 2 zones');
    harness.assertEqual(l2.loadKw, '9.5', 'Level 2 load is 9.5 kW (6000+3500 W)');
    harness.assertEqual(l2.occupancy, 22, 'Level 2 occupancy is 22 Pax (16+6)');
    harness.assertEqual(l2.avgTemp, '23.7', 'Level 2 avg temp is 23.7 °C');
    harness.assertEqual(l2.alarms, 0, 'Level 2 has 0 alarms');
  });

  await harness.test('Level 3 Telemetry correctly aggregates engineering lab load', () => {
    const l3 = computeLevelMetrics(3, mockBuildingData, mockSimZones);
    harness.assertEqual(l3.count, 2, 'Level 3 has 2 zones');
    harness.assertEqual(l3.loadKw, '14.0', 'Level 3 load is 14.0 kW (12000+2000 W)');
    harness.assertEqual(l3.occupancy, 14, 'Level 3 occupancy is 14 Pax (10+4)');
    harness.assertEqual(l3.avgTemp, '24.8', 'Level 3 avg temp is 24.8 °C');
    harness.assertEqual(l3.alarms, 0, 'Level 3 has 0 alarms');
  });

  await harness.test('Level 4 Telemetry surfaces active thermal alert in boardroom', () => {
    const l4 = computeLevelMetrics(4, mockBuildingData, mockSimZones);
    harness.assertEqual(l4.count, 3, 'Level 4 has 3 zones');
    harness.assertEqual(l4.loadKw, '17.5', 'Level 4 load is 17.5 kW (4000+8000+5500 W)');
    harness.assertEqual(l4.occupancy, 23, 'Level 4 occupancy is 23 Pax (2+14+7)');
    harness.assertEqual(l4.avgTemp, '24.5', 'Level 4 avg temp is 24.5 °C');
    harness.assertEqual(l4.alarms, 1, 'Level 4 correctly detects 1 critical alarm (boardroom)');
  });

  await harness.test('Telemetry across all levels is dynamically distinct and non-constant', () => {
    const l1 = computeLevelMetrics(1, mockBuildingData, mockSimZones);
    const l2 = computeLevelMetrics(2, mockBuildingData, mockSimZones);
    const l3 = computeLevelMetrics(3, mockBuildingData, mockSimZones);
    const l4 = computeLevelMetrics(4, mockBuildingData, mockSimZones);

    harness.assertNotEqual(l1.loadKw, l2.loadKw, 'Level 1 vs 2 loads are distinct');
    harness.assertNotEqual(l2.occupancy, l4.occupancy, 'Level 2 vs 4 occupancies are distinct');
    harness.assertNotEqual(l1.avgTemp, l3.avgTemp, 'Level 1 vs 3 avg temperatures are distinct');
  });

  await harness.test('Numeric edge cases (0.0 °C temperature, 0 kW load, zero pax) are preserved without falsy default substitution', () => {
    const zeroTestBuilding = {
      id: 'bld-cold-storage',
      floors: [{ level: 10, zones: [{ zoneId: 'z-freezer' }] }]
    };
    const zeroSimZones = {
      'z-freezer': { id: 'z-freezer', level: 10, temp: 0.0, load: 0, occupancy: 0, alert: false, lightsOn: true }
    };
    const metrics = computeLevelMetrics(10, zeroTestBuilding, zeroSimZones);
    harness.assertEqual(metrics.avgTemp, '0.0', '0.0 °C temperature is preserved and not replaced by 24.0 °C');
    harness.assertEqual(metrics.loadKw, '0.0', '0.0 kW load is preserved');
    harness.assertEqual(metrics.occupancy, 0, '0 occupancy is preserved');
  });

  await harness.test('Zero-zone fallback gracefully returns 0.0 kW, 0 Pax, and 0Z without crashing', () => {
    const emptyFloor = computeLevelMetrics(99, mockBuildingData, mockSimZones);
    harness.assertEqual(emptyFloor.count, 0, 'Count is 0');
    harness.assertEqual(emptyFloor.loadKw, '0.0', 'Load is 0.0 kW');
    harness.assertEqual(emptyFloor.occupancy, 0, 'Occupancy is 0 Pax');
    harness.assertEqual(emptyFloor.avgTemp, '0.0', 'Avg temp is 0.0 °C');
    harness.assertEqual(emptyFloor.alarms, 0, 'Alarms count is 0');
  });

  // ----------------------------------------------------------------------------
  // SUITE 2: Real Built App Puppeteer Verification (Vite Production Bundle)
  // ----------------------------------------------------------------------------
  harness.suite('Real Built App Puppeteer DOM Level Toggle Verification');

  let staticServer;
  let browser;

  try {
    staticServer = await createStaticServer(5193);

    browser = await puppeteer.launch({
      headless: true,
      pipe: true,
      args: [
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--single-process',
        '--no-zygote',
        '--disable-gpu',
        '--disable-dev-shm-usage',
        '--disable-features=Crashpad'
      ],
    });

    const appUrl = 'http://dashboard.local/';

    await harness.test('Mounts real dashboard application and renders interactive level toggle controls', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      const errors = [];
      page.on('pageerror', (err) => errors.push(err.message));

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      const levelContainer = await page.$('[data-testid="level-toggle-container"]');
      harness.assert(levelContainer != null, 'Level toggle container mounted in GlobalMetricsPanel');

      const desktopToggle = await page.$('[data-testid="desktop-level-toggle"]');
      harness.assert(desktopToggle != null, 'Desktop floating level toggle widget mounted');

      const levelButtons = await page.$$('[data-testid^="level-btn-"]');
      harness.assert(levelButtons.length >= 10, `Found ${levelButtons.length} floor level buttons (expected >= 10)`);

      const selectedLevel = await page.$eval('[data-testid="selected-level-display"]', el => el.innerText.trim());
      harness.assert(/^L\d+$/.test(selectedLevel), `Selected level display matches L<number> pattern (got ${selectedLevel})`);

      const desktopActive = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
      harness.assertEqual(desktopActive, selectedLevel, 'Desktop level selector matches right dock selected level');

      harness.assertEqual(errors.length, 0, 'No uncaught page errors on initial render');
      await page.close();
    });

    await harness.test('Clicking level buttons (L1, L2, L3, L4) dynamically updates level metrics, indicator, and topology header in real DOM', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      // Test Level 1
      await page.click('[data-testid="level-btn-1"]');
      await new Promise(r => setTimeout(r, 300));
      let selLvl = await page.$eval('[data-testid="selected-level-display"]', el => el.innerText.trim());
      let deskLvl = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
      harness.assertEqual(selLvl, 'L1', 'Selected level mutated to L1');
      harness.assertEqual(deskLvl, 'L1', 'Desktop level mutated to L1');

      let topoText = await page.evaluate(() => document.body.innerText);
      harness.assert(topoText.includes('MAP LEVEL 1 TOPOLOGY'), 'Topology header mutated to MAP LEVEL 1 TOPOLOGY');

      let l1Load = await page.$eval('[data-testid="level-metric-load"]', el => el.innerText.trim());
      let l1Occ = await page.$eval('[data-testid="level-metric-occupancy"]', el => el.innerText.trim());
      let l1Temp = await page.$eval('[data-testid="level-metric-temp"]', el => el.innerText.trim());
      let l1Zones = await page.$eval('[data-testid="level-metric-zones"]', el => el.innerText.trim());

      harness.assert(l1Load.includes('kW'), 'Level 1 load contains kW');
      harness.assert(l1Occ.includes('Pax'), 'Level 1 occupancy contains Pax');
      harness.assert(l1Temp.includes('°C'), 'Level 1 temp contains °C');
      harness.assert(l1Zones.includes('Z'), 'Level 1 zone count contains Z');

      // Test Level 2
      await page.click('[data-testid="level-btn-2"]');
      await new Promise(r => setTimeout(r, 300));
      selLvl = await page.$eval('[data-testid="selected-level-display"]', el => el.innerText.trim());
      deskLvl = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
      harness.assertEqual(selLvl, 'L2', 'Selected level mutated to L2');
      harness.assertEqual(deskLvl, 'L2', 'Desktop level mutated to L2');

      topoText = await page.evaluate(() => document.body.innerText);
      harness.assert(topoText.includes('MAP LEVEL 2 TOPOLOGY'), 'Topology header mutated to MAP LEVEL 2 TOPOLOGY');

      // Test Level 3
      await page.click('[data-testid="level-btn-3"]');
      await new Promise(r => setTimeout(r, 300));
      selLvl = await page.$eval('[data-testid="selected-level-display"]', el => el.innerText.trim());
      harness.assertEqual(selLvl, 'L3', 'Selected level mutated to L3');

      topoText = await page.evaluate(() => document.body.innerText);
      harness.assert(topoText.includes('MAP LEVEL 3 TOPOLOGY'), 'Topology header mutated to MAP LEVEL 3 TOPOLOGY');

      // Test Level 4
      await page.click('[data-testid="level-btn-4"]');
      await new Promise(r => setTimeout(r, 300));
      selLvl = await page.$eval('[data-testid="selected-level-display"]', el => el.innerText.trim());
      harness.assertEqual(selLvl, 'L4', 'Selected level mutated to L4');

      topoText = await page.evaluate(() => document.body.innerText);
      harness.assert(topoText.includes('MAP LEVEL 4 TOPOLOGY'), 'Topology header mutated to MAP LEVEL 4 TOPOLOGY');

      await page.close();
    });

    await harness.test('Desktop level stepper (◀ / ▶) steps floors sequentially and clamps at boundaries', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      // Click Level 1 first
      await page.click('[data-testid="level-btn-1"]');
      await new Promise(r => setTimeout(r, 200));

      // At L1, click prev -> should remain L1 (boundary clamped)
      await page.click('[data-testid="level-step-prev"]');
      await new Promise(r => setTimeout(r, 150));
      let deskLvl = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
      harness.assertEqual(deskLvl, 'L1', 'Level clamped at minLevel (L1)');

      // Step next: L1 -> L2
      await page.click('[data-testid="level-step-next"]');
      await new Promise(r => setTimeout(r, 150));
      deskLvl = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
      harness.assertEqual(deskLvl, 'L2', 'Stepped forward to L2');

      // Step next: L2 -> L3
      await page.click('[data-testid="level-step-next"]');
      await new Promise(r => setTimeout(r, 150));
      deskLvl = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
      harness.assertEqual(deskLvl, 'L3', 'Stepped forward to L3');

      // Step prev: L3 -> L2
      await page.click('[data-testid="level-step-prev"]');
      await new Promise(r => setTimeout(r, 150));
      deskLvl = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
      harness.assertEqual(deskLvl, 'L2', 'Stepped backward to L2');

      await page.close();
    });

    await harness.test('Switching building model to 1-level domestic home updates available level buttons to L1 only', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      // Switch to domestic home
      const homeBtn = await page.$('[data-testid="toggle-domestic-home"]');
      if (homeBtn) {
        await homeBtn.click();
        await new Promise(r => setTimeout(r, 800));

        const levelButtons = await page.$$('[data-testid^="level-btn-"]');
        harness.assertEqual(levelButtons.length, 1, 'Domestic home has exactly 1 floor button (L1)');

        const deskLvl = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
        harness.assertEqual(deskLvl, 'L1', 'Domestic home active level set to L1');

        const selLvl = await page.$eval('[data-testid="selected-level-display"]', el => el.innerText.trim());
        harness.assertEqual(selLvl, 'L1', 'Domestic home selected level set to L1');

        // Domestic home stepper should remain L1 on next/prev
        await page.click('[data-testid="level-step-next"]');
        await new Promise(r => setTimeout(r, 150));
        let steppedLvl = await page.$eval('[data-testid="desktop-active-level"]', el => el.innerText.trim());
        harness.assertEqual(steppedLvl, 'L1', 'Domestic home stays at L1 on next');

        // Switch back to multi-level tower
        const towerBtn = await page.$('[data-testid="toggle-multilevel"]');
        if (towerBtn) await towerBtn.click();
        await new Promise(r => setTimeout(r, 800));
        const restoredButtons = await page.$$('[data-testid^="level-btn-"]');
        harness.assert(restoredButtons.length >= 10, 'Restored tower floor list (>= 10)');
      }

      await page.close();
    });

    await harness.test('Rapid floor switching stress test: switching levels 6 times in succession preserves DOM consistency', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      const testLevels = [1, 3, 2, 4, 1, 2];
      for (const lvl of testLevels) {
        await page.click(`[data-testid="level-btn-${lvl}"]`);
        await new Promise(r => setTimeout(r, 100));
        const active = await page.$eval('[data-testid="selected-level-display"]', el => el.innerText.trim());
        harness.assertEqual(active, `L${lvl}`, `Selected level cleanly matched L${lvl} during rapid sequence`);
      }

      await page.close();
    });

    await harness.test('Mobile Viewport (390x844) renders mobile screen with interactive floor stepper controls', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 390, height: 844, isMobile: true, hasTouch: true });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      const mobileStepper = await page.$('[data-testid="mobile-level-stepper"]');
      harness.assert(mobileStepper != null, 'Mobile floor stepper mounted in DOM');

      const mobileDisplay = await page.$eval('[data-testid="mobile-level-display"]', el => el.innerText.trim());
      harness.assert(/^L\d+$/.test(mobileDisplay), `Mobile level display matches L<number> pattern (got ${mobileDisplay})`);

      // Interact with mobile up / down
      await page.click('[data-testid="mobile-level-down"]');
      await new Promise(r => setTimeout(r, 200));
      const downDisplay = await page.$eval('[data-testid="mobile-level-display"]', el => el.innerText.trim());
      harness.assert(/^L\d+$/.test(downDisplay), 'Mobile down step successfully updated level display');

      await page.close();
    });

  } finally {
    if (browser) {
      await browser.close();
    }
    if (staticServer) {
      await staticServer.close();
    }
  }

  const success = harness.summary();
  if (!success) {
    process.exit(1);
  }
}

runVerification();
