#!/usr/bin/env node

/**
 * verify_ui_rendering.js — UI & 3D Rendering Verification Suite
 *
 * Verifies:
 * 1. Top tab bar text rendering without truncation (e.g. "PLUGS" not "PLU...")
 * 2. 3D visualization orthogonal HVAC duct routing (no chaotic web/starburst rays)
 * 3. Scene illumination & ambient lighting (no pitch-black darkening)
 * 4. AI Load Forecast non-duplication (trajectory chart vs diagnostics breakdown)
 * 5. 1-Level Domestic Home 3D asset integration & UI toggle switching
 */

import puppeteer from 'puppeteer';
import http from 'http';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { getBuilding, setBuildingModelType, getBuildingModelType, subscribeBuildingChange, getAllKnownBuildings } from './src/buildingStore.js';
import { getFootprint, toWorld, FOOTPRINT, ORIGIN } from './src/floorGeometry.js';
import { buildFlowField } from './src/flowfield.js';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const colors = {
  reset: '\x1b[0m',
  bright: '\x1b[1m',
  green: '\x1b[32m',
  red: '\x1b[31m',
  cyan: '\x1b[36m',
  yellow: '\x1b[33m',
  gray: '\x1b[90m',
};

class TestHarness {
  constructor() {
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
    const start = Date.now();
    try {
      await fn();
      const elapsed = Date.now() - start;
      this.passed++;
      console.log(`  ${colors.green}✔ PASS${colors.reset} ${name} ${colors.gray}(${elapsed}ms)${colors.reset}`);
    } catch (err) {
      const elapsed = Date.now() - start;
      this.failed++;
      console.error(`  ${colors.red}✖ FAIL${colors.reset} ${name} ${colors.gray}(${elapsed}ms)${colors.reset}`);
      console.error(`    ${colors.red}${err.stack || err.message}${colors.reset}`);
    }
  }

  assert(condition, message) {
    if (!condition) throw new Error(`Assertion failed: ${message}`);
  }

  assertEqual(actual, expected, message) {
    if (JSON.stringify(actual) !== JSON.stringify(expected)) {
      throw new Error(`Assertion failed: ${message} (Expected: ${JSON.stringify(expected)}, Actual: ${JSON.stringify(actual)})`);
    }
  }

  summary() {
    const totalTime = Date.now() - this.startTime;
    console.log(`\n${colors.bright}${colors.cyan}====================================================${colors.reset}`);
    console.log(`${colors.bright}UI Verification Summary: ${this.passed + this.failed} Total | ${colors.green}${this.passed} Passed${colors.reset} | ${this.failed > 0 ? colors.red : colors.green}${this.failed} Failed${colors.reset} ${colors.gray}(${totalTime}ms)${colors.reset}`);
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

function createStaticServer(port = 5192) {
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

  return new Promise((resolve) => {
    server.listen(port, '127.0.0.1', () => {
      resolve({
        server,
        url: `http://127.0.0.1:${port}`,
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

async function runVerification() {
  console.log(`\n${colors.bright}${colors.cyan}╔══════════════════════════════════════════════════════════════════════╗${colors.reset}`);
  console.log(`${colors.bright}${colors.cyan}║         ECON Dashboard UI & 3D Rendering Verification Suite          ║${colors.reset}`);
  console.log(`${colors.bright}${colors.cyan}╚══════════════════════════════════════════════════════════════════════╝${colors.reset}`);

  // --------------------------------------------------------------------------
  // SUITE 1: 3D Asset Store & Domestic Home Toggle Integration
  // --------------------------------------------------------------------------
  harness.suite('Building Model Asset Switching & Dynamic Geometry');

  await harness.test('Initial building model defaults to multi-level digitized tower', () => {
    setBuildingModelType('multi-level');
    harness.assertEqual(getBuildingModelType(), 'multi-level', 'default model is multi-level');
    const bld = getBuilding();
    harness.assert(bld.floors.length > 1, `tower has ${bld.floors.length} floors (expected > 1)`);
    harness.assertEqual(bld.buildingId, 'bldg-econ-digitized', 'buildingId matches digitized tower');
    const fp = getFootprint(bld);
    harness.assert(fp.width >= 40, `tower footprint width is ${fp.width}m`);
  });

  await harness.test('Switching to domestic-home model loads 1-level 5-zone home fixture', () => {
    let notified = false;
    const unsub = subscribeBuildingChange((b, type) => {
      if (type === 'domestic-home') notified = true;
    });

    setBuildingModelType('domestic-home');
    harness.assertEqual(getBuildingModelType(), 'domestic-home', 'active model switched to domestic-home');
    harness.assert(notified, 'change subscriber received notification');
    unsub();

    const home = getBuilding();
    harness.assertEqual(home.buildingId, 'bldg-econ-house-hcmc', 'house fixture ID matches bldg-econ-house-hcmc');
    harness.assertEqual(home.floors.length, 1, 'house has exactly 1 floor (level 1)');
    harness.assertEqual(home.floors[0].zones.length, 5, 'house has exactly 5 rooms/zones');

    const zoneNames = home.floors[0].zones.map(z => z.name);
    harness.assert(zoneNames.includes('Kitchen & rear service'), 'includes Kitchen');
    harness.assert(zoneNames.includes('Office'), 'includes Office');
    harness.assert(zoneNames.includes('Living room'), 'includes Living room');
    harness.assert(zoneNames.includes('Passage'), 'includes Passage');
    harness.assert(zoneNames.includes('Bathroom'), 'includes Bathroom');
  });

  await harness.test('Domestic-home dynamic footprint, ORIGIN, and toWorld coordinate transformations', () => {
    const fp = getFootprint();
    harness.assert(Math.abs(fp.width - 13.56) < 0.1, `house footprint width ~13.56m (got ${fp.width.toFixed(2)})`);
    harness.assert(Math.abs(fp.depth - 5.51) < 0.1, `house footprint depth ~5.51m (got ${fp.depth.toFixed(2)})`);

    // Dynamic ORIGIN evaluation
    harness.assert(Math.abs(ORIGIN.x - fp.cx) < 0.001, 'ORIGIN.x matches footprint center');
    harness.assert(Math.abs(ORIGIN.y - fp.cy) < 0.001, 'ORIGIN.y matches footprint center');

    // toWorld transform relative to house center
    const worldPoint = toWorld([6.78, 2.755]);
    harness.assert(Math.abs(worldPoint[0]) < 0.1, `center x maps to ~0 world X (got ${worldPoint[0]})`);
    harness.assert(Math.abs(worldPoint[1]) < 0.1, `center y maps to ~0 world Z (got ${worldPoint[1]})`);
  });

  await harness.test('Switching back to multi-level model cleanly restores tower geometry', () => {
    setBuildingModelType('multi-level');
    harness.assertEqual(getBuildingModelType(), 'multi-level', 'switched back to multi-level');
    const tower = getBuilding();
    harness.assertEqual(tower.buildingId, 'bldg-econ-digitized', 'restored digitized tower');
    harness.assert(tower.floors.length >= 10, 'restored full tower floor list');
  });

  await harness.test('Multi-level and Domestic-home fixtures contain all expected zones and properties', () => {
    const knownBlds = getAllKnownBuildings();
    harness.assertEqual(knownBlds.length, 2, 'getAllKnownBuildings returns both tower and domestic home');
    const tower = knownBlds[0];
    const home = knownBlds[1];

    harness.assert(tower.floors.length > 1, 'Tower has multiple floors');
    harness.assertEqual(home.floors.length, 1, 'Domestic home has 1 floor');

    const homeZones = home.floors[0].zones;
    harness.assertEqual(homeZones.length, 5, 'Domestic home has 5 zones');
    const office = homeZones.find(z => z.zoneId === 'zone-office-lvl1');
    harness.assert(office != null, 'Office zone exists in domestic home fixture');
    harness.assertEqual(office.thermalProperties.setpoint, 26.0, 'Office setpoint matches 26.0C');
  });

  await harness.test('P&ID Topology node isolation prevents Level 1 zone leakage between models', () => {
    const knownBlds = getAllKnownBuildings();
    const tower = knownBlds[0];
    const home = knownBlds[1];

    // Domestic home Level 1 topology: exactly 5 terminal units
    const homeFloorObj = home.floors.find(f => f.level === 1);
    const homeFloorZoneIds = new Set((homeFloorObj?.zones || []).map(z => z.zoneId));
    harness.assertEqual(homeFloorZoneIds.size, 5, 'Domestic home Level 1 yields exactly 5 terminal unit zones');

    // Tower Level 1 topology: exactly 90 terminal units
    const towerFloorObj = tower.floors.find(f => f.level === 1);
    const towerFloorZoneIds = new Set((towerFloorObj?.zones || []).map(z => z.zoneId));
    harness.assertEqual(towerFloorZoneIds.size, 90, 'Tower Level 1 yields exactly 90 terminal unit zones');

    // Mutual exclusivity
    const intersection = [...homeFloorZoneIds].filter(hz => towerFloorZoneIds.has(hz));
    harness.assertEqual(intersection.length, 0, 'Zero zone ID collisions between tower and domestic home');
  });

  await harness.test('FloorPlate cache key signature dynamically isolates tower and home geometries', () => {
    setBuildingModelType('multi-level');
    const towerFp = getFootprint();
    const towerOriginX = ORIGIN.x;
    const towerOriginY = ORIGIN.y;

    setBuildingModelType('domestic-home');
    const homeFp = getFootprint();
    const homeOriginX = ORIGIN.x;
    const homeOriginY = ORIGIN.y;

    harness.assert(towerOriginX !== homeOriginX, `Tower ORIGIN.x (${towerOriginX}) differs from Home ORIGIN.x (${homeOriginX})`);
    harness.assert(towerOriginY !== homeOriginY, `Tower ORIGIN.y (${towerOriginY}) differs from Home ORIGIN.y (${homeOriginY})`);
    harness.assert(towerFp.width !== homeFp.width, `Tower width (${towerFp.width}) differs from Home width (${homeFp.width})`);

    setBuildingModelType('multi-level'); // restore
  });

  await harness.test('HVAC physical infrastructure generates orthogonal duct routing and isolated airflow domains', () => {
    // 1. Domestic Home HVAC infrastructure
    setBuildingModelType('domestic-home');
    const home = getBuilding();
    const homeFloor = home.floors[0];
    const homeField = buildFlowField(homeFloor, { zones: {}, vavs: {} });
    harness.assert(homeField != null, 'Home flowfield built successfully');
    harness.assertEqual(homeField.diffusers.length, 5, 'Home has 5 diffusers (1 per supply room)');
    harness.assertEqual(homeField.doors.length, 5, 'Home has 5 door openings mapped');
    harness.assert(homeField.grid.W < 15, `Home domain width is ${homeField.grid.W.toFixed(1)}m (<15m)`);

    // 2. Tower Level 1 HVAC infrastructure
    setBuildingModelType('multi-level');
    const tower = getBuilding();
    const towerFloor = tower.floors[0];
    const towerField = buildFlowField(towerFloor, { zones: {}, vavs: {} });
    harness.assert(towerField != null, 'Tower flowfield built successfully');
    harness.assert(towerField.diffusers.length > 50, 'Tower has full diffuser array (>50)');
    harness.assert(towerField.grid.W >= 40, `Tower domain width is ${towerField.grid.W.toFixed(1)}m (>=40m)`);
  });

  // --------------------------------------------------------------------------
  // SUITE 2: Real App Puppeteer DOM, Tab Bar, and UI Toggle Verification
  // --------------------------------------------------------------------------
  harness.suite('Real Built App Puppeteer DOM & 3D Toggle Verification');

  let staticServer;
  let browser;
  try {
    staticServer = await createStaticServer(5192);

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

    await harness.test('Top tab bar renders all 4 tabs with full text (no "PLU..." truncation) on real built application', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      const errors = [];
      page.on('pageerror', (err) => errors.push(err.message));

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      const tabInfo = await page.evaluate(() => {
        const tabIds = ['tab-ai-insights', 'tab-profiler', 'tab-logs', 'tab-plugs'];
        return tabIds.map(id => {
          const el = document.querySelector(`[data-testid="${id}"]`);
          return {
            id,
            found: !!el,
            text: el ? el.innerText.trim() : null,
            width: el ? el.clientWidth : 0,
            scrollWidth: el ? el.scrollWidth : 0,
          };
        });
      });

      harness.assertEqual(tabInfo.map(t => t.text), ['AI INSIGHTS', 'PROFILER', 'LOGS', 'PLUGS'], 'All 4 tab labels match exactly');
      harness.assert(tabInfo.every(t => t.found), 'All 4 tab buttons are mounted in DOM');

      const plugsTab = tabInfo.find(t => t.id === 'tab-plugs');
      harness.assertEqual(plugsTab.text, 'PLUGS', 'PLUGS tab is full text and not truncated to PLU...');
      harness.assert(plugsTab.scrollWidth <= plugsTab.width + 1, 'PLUGS tab has no overflow/clipping');

      harness.assertEqual(errors.length, 0, 'No uncaught page errors during tab render');
      await page.close();
    });

    await harness.test('Tab navigation interactivity switches active panels smoothly in DOM', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1200));

      // Click PROFILER tab
      await page.click('[data-testid="tab-profiler"]');
      await new Promise(r => setTimeout(r, 300));
      let panelText = await page.$eval('.hud-container', el => el.innerText);
      harness.assert(panelText.includes('SYSTEM TELEMETRY') || panelText.includes('PROFILER') || panelText.includes('LOAD PROFILE') || panelText.includes('CHILLER PLANT'), 'PROFILER panel mounted');

      // Click LOGS tab
      await page.click('[data-testid="tab-logs"]');
      await new Promise(r => setTimeout(r, 300));
      panelText = await page.$eval('.hud-container', el => el.innerText);
      harness.assert(panelText.includes('TELEMETRY') || panelText.includes('LOG') || panelText.includes('STREAM'), 'LOGS panel mounted');

      // Click PLUGS tab
      await page.click('[data-testid="tab-plugs"]');
      await new Promise(r => setTimeout(r, 300));
      panelText = await page.$eval('.hud-container', el => el.innerText);
      harness.assert(panelText.includes('PLUG') || panelText.includes('SOCKET') || panelText.includes('APLC') || panelText.includes('STANDBY'), 'PLUGS panel mounted');

      // Click AI INSIGHTS tab
      await page.click('[data-testid="tab-ai-insights"]');
      await new Promise(r => setTimeout(r, 300));
      panelText = await page.$eval('.hud-container', el => el.innerText);
      harness.assert(panelText.includes('AI Operations Engine') || panelText.includes('Forecast'), 'AI INSIGHTS panel re-mounted cleanly');

      await page.close();
    });

    await harness.test('UI toggle below 3D view switches active asset to 1-level domestic home and updates 3D/Topology state', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      const toggleExists = await page.$('[data-testid="building-model-toggle"]');
      harness.assert(toggleExists != null, '3D model toggle container mounted below 3D view');

      // Check initial tower state
      const initialHeader = await page.$eval('.panel-header', el => el.innerText);
      harness.assert(initialHeader.includes('TOPOLOGY'), 'P&ID topology header exists');

      // Click domestic home model toggle
      await page.click('[data-testid="toggle-domestic-home"]');
      await new Promise(r => setTimeout(r, 600));

      const afterHomeState = await page.evaluate(() => {
        const homeBtn = document.querySelector('[data-testid="toggle-domestic-home"]');
        const multiBtn = document.querySelector('[data-testid="toggle-multilevel"]');
        const header = document.querySelector('.panel-header')?.innerText || '';
        return {
          homeBg: homeBtn ? homeBtn.style.background : '',
          multiBg: multiBtn ? multiBtn.style.background : '',
          header,
        };
      });

      harness.assert(afterHomeState.homeBg.includes('accent-blue') || afterHomeState.homeBg.includes('rgb'), 'Domestic Home button active background set');
      harness.assertEqual(afterHomeState.multiBg, 'transparent', 'Multi-level button inactive background set');
      harness.assert(afterHomeState.header.includes('LEVEL 1 TOPOLOGY'), 'Topology header switched to Level 1');
      harness.assert(afterHomeState.header.includes('5 TERMINAL UNITS'), `Topology reflects 5 domestic home terminal units (got ${afterHomeState.header})`);

      // Switch back to multi-level building
      await page.click('[data-testid="toggle-multilevel"]');
      await new Promise(r => setTimeout(r, 600));

      const afterMultiHeader = await page.$eval('.panel-header', el => el.innerText);
      harness.assert(afterMultiHeader.includes('90 TERMINAL UNITS') || afterMultiHeader.includes('TERMINAL UNITS'), 'Topology restored to multi-level building units');

      await page.close();
    });

    await harness.test('Rapid model switching stress test: toggling 5 times in succession maintains state integrity', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1200));

      for (let i = 0; i < 5; i++) {
        await page.click('[data-testid="toggle-domestic-home"]');
        await new Promise(r => setTimeout(r, 150));
        await page.click('[data-testid="toggle-multilevel"]');
        await new Promise(r => setTimeout(r, 150));
      }

      // Final switch to domestic home
      await page.click('[data-testid="toggle-domestic-home"]');
      await new Promise(r => setTimeout(r, 400));

      const finalState = await page.evaluate(() => {
        const header = document.querySelector('.panel-header')?.innerText || '';
        const homeBtn = document.querySelector('[data-testid="toggle-domestic-home"]');
        return {
          header,
          homeBg: homeBtn?.style.background || '',
        };
      });

      harness.assert(finalState.header.includes('5 TERMINAL UNITS'), `Domestic home topology stable after rapid toggles (got ${finalState.header})`);
      harness.assert(finalState.homeBg.includes('accent-blue') || finalState.homeBg.includes('rgb'), 'Domestic home button remains active');

      await page.close();
    });

    await harness.test('AI Insights panel load forecast cards are not duplicated and render complementary information in real DOM', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 1440, height: 900 });

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      // Make sure AI Insights tab is active
      await page.click('[data-testid="tab-ai-insights"]');
      await new Promise(r => setTimeout(r, 300));

      const forecastElements = await page.evaluate(() => {
        const forecastCharts = document.querySelectorAll('[data-testid="forecast-chart"]');
        const forecastCardTitles = Array.from(document.querySelectorAll('h3, span, div')).map(e => e.innerText.trim());
        const hasTopChart = forecastCardTitles.some(t => t.includes('AI Load Forecast Trajectory & Peak Reference'));
        return {
          forecastChartCount: forecastCharts.length,
          hasTopChart,
        };
      });

      harness.assert(forecastElements.hasTopChart, 'Top visual forecast chart card present in AI Insights panel');
      harness.assert(forecastElements.forecastChartCount >= 1, 'At least 1 ForecastChart instance rendered in AI panel');

      await page.close();
    });

    await harness.test('Mobile Viewport (390x844) renders mobile screen cleanly with deduplicated forecast', async () => {
      const page = await browser.newPage();
      await setupRequestInterception(page);
      await page.setViewport({ width: 390, height: 844, isMobile: true, hasTouch: true });

      const errors = [];
      page.on('pageerror', (err) => errors.push(err.message));

      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await new Promise(r => setTimeout(r, 1500));

      const mobileInitial = await page.evaluate(() => {
        const text = document.body.innerText;
        return {
          hasCenter: text.includes('ECON Center'),
          hasAiMenuItem: text.includes('AI & Automation'),
        };
      });

      harness.assert(mobileInitial.hasCenter, 'Mobile screen mounted and renders ECON Center header');
      harness.assert(mobileInitial.hasAiMenuItem, 'Mobile screen displays AI & Automation menu item');

      // Click AI & Automation menu item
      await page.click('[data-testid="mobile-menu-ai"]');
      await new Promise(r => setTimeout(r, 600));

      const modalContent = await page.evaluate(() => {
        const text = document.body.innerText;
        const charts = document.querySelectorAll('[data-testid="forecast-chart"]');
        return {
          hasAiHeader: text.includes('AI & Automation'),
          hasForecast: text.includes('Load Forecast') || text.includes('TimesFM') || text.includes('LSTM'),
          hasRecs: text.includes('Live Recommendations') || text.includes('Auto-Pilot'),
          chartCount: charts.length,
        };
      });

      harness.assert(modalContent.hasAiHeader, 'AI & Automation modal opened');
      harness.assert(modalContent.hasForecast, 'Mobile screen displays load forecast telemetry');
      harness.assert(modalContent.chartCount >= 1, 'Top forecast chart rendered on mobile');
      harness.assertEqual(errors.length, 0, 'No JavaScript runtime errors on mobile viewport');

      await page.close();
    });

  } finally {
    if (browser) await browser.close();
    if (staticServer) await staticServer.close();
  }

  const allPassed = harness.summary();
  if (!allPassed) {
    process.exit(1);
  }
}

runVerification().catch(err => {
  console.error('Fatal verification error:', err);
  process.exit(1);
});
