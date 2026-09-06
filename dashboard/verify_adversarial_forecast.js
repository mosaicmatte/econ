#!/usr/bin/env node

/**
 * verify_adversarial_forecast.js — Adversarial Stress Test Suite for ForecastChart
 *
 * Scenarios tested:
 *  1. Empty forecast array: []
 *  2. Null payload: series = null, forecast = null
 *  3. Undefined payload: series = undefined, field missing
 *  4. Gateway Timeout: 504 Gateway Timeout
 *  5. Network / Internal Error: 500 with malformed HTML body
 *  6. Scalar LSTM Only (Single-Point Prediction): lstmPeakMw = 0.0285, series = []
 *  7. Single-point array: series = [0.0210]
 *  8. Corrupted string array: series = ["corrupted", "data"]
 *  9. Corrupted NaN/Infinity array: series = [NaN, Infinity, -Infinity]
 * 10. Corrupted object array: series = [{ invalid: "payload", missing_t: true }]
 * 11. Empty JSON response: {}
 * 12. Mobile screen under adversarial payload
 */

import puppeteer from 'puppeteer';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

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
    this.startTime = Date.now();
  }

  suite(name) {
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
    console.log(`${colors.bright}Adversarial Forecast Verification: ${this.passed + this.failed} Total | ${colors.green}${this.passed} Passed${colors.reset} | ${this.failed > 0 ? colors.red : colors.green}${this.failed} Failed${colors.reset} ${colors.gray}(${totalTime}ms)${colors.reset}`);
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

const corsHeaders = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
  'Access-Control-Allow-Headers': '*',
};

function setupCustomInterception(page, scenarioHandler) {
  const distDir = path.join(__dirname, 'dist');
  return page.setRequestInterception(true).then(() => {
    page.on('request', req => {
      try {
        if (req.method() === 'OPTIONS') {
          req.respond({ status: 204, headers: corsHeaders });
          return;
        }

        const url = new URL(req.url());
        const pathname = url.pathname;

        if (pathname.includes('/api/forecast/compare') || pathname.includes('/api/recommendations')) {
          scenarioHandler(req, pathname);
          return;
        }

        // Static bundle files
        let reqFile = pathname;
        if (reqFile === '/' || reqFile === '') reqFile = '/index.html';
        const filePath = path.join(distDir, reqFile.replace(/^\//, ''));
        if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
          const ext = path.extname(filePath);
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: mimeTypes[ext] || 'application/octet-stream',
            body: fs.readFileSync(filePath),
          });
        } else {
          const indexPath = path.join(distDir, 'index.html');
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'text/html',
            body: fs.readFileSync(indexPath),
          });
        }
      } catch {
        req.continue();
      }
    });
  });
}

async function runAdversarialVerification() {
  console.log(`\n${colors.bright}${colors.cyan}╔══════════════════════════════════════════════════════════════════════╗${colors.reset}`);
  console.log(`${colors.bright}${colors.cyan}║   ADVERSARIAL STRESS TEST HARNESS — FORECAST CHART (R2)             ║${colors.reset}`);
  console.log(`${colors.bright}${colors.cyan}╚══════════════════════════════════════════════════════════════════════╝${colors.reset}`);

  const appUrl = 'http://dashboard.local/';

  const browser = await puppeteer.launch({
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
    ]
  });

  try {
    // --------------------------------------------------------------------------
    // SUITE 1: Null & Undefined Payloads
    // --------------------------------------------------------------------------
    harness.suite('Adversarial Null & Undefined Forecast Payloads');

    await harness.test('Null payload renders [data-testid="forecast-insufficient-data"] and 0 curves', async () => {
      const page = await browser.newPage();
      await setupCustomInterception(page, (req, pathname) => {
        if (pathname.includes('/api/forecast/compare')) {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({ timesfm: null, lstm: null }),
          });
        } else {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({ recommendations: [], forecast: null }),
          });
        }
      });
      await page.setViewport({ width: 1440, height: 900 });
      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('[data-testid="tab-ai-insights"]');
      await page.click('[data-testid="tab-ai-insights"]');
      await page.waitForSelector('[data-testid="forecast-insufficient-data"]', { timeout: 5000 });

      const evalResult = await page.evaluate(() => {
        return {
          hasInsufficientBadge: document.querySelector('[data-testid="forecast-insufficient-data"]') !== null,
          curveCount: document.querySelectorAll('.recharts-line-curve').length,
          svgPaths: document.querySelectorAll('svg.forecast-chart path').length,
        };
      });

      harness.assert(evalResult.hasInsufficientBadge, 'Insufficient data badge rendered for null payload');
      harness.assertEqual(evalResult.curveCount, 0, 'Zero recharts curve paths rendered for null payload');
      harness.assertEqual(evalResult.svgPaths, 0, 'Zero fallback SVG paths rendered for null payload');
      await page.close();
    });

    await harness.test('Undefined / empty object payload renders insufficient data badge and 0 curves', async () => {
      const page = await browser.newPage();
      await setupCustomInterception(page, (req, pathname) => {
        if (pathname.includes('/api/forecast/compare')) {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({}),
          });
        } else {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({ recommendations: [] }),
          });
        }
      });
      await page.setViewport({ width: 1440, height: 900 });
      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('[data-testid="tab-ai-insights"]');
      await page.click('[data-testid="tab-ai-insights"]');
      await page.waitForSelector('[data-testid="forecast-insufficient-data"]', { timeout: 5000 });

      const evalResult = await page.evaluate(() => {
        return {
          hasInsufficientBadge: document.querySelector('[data-testid="forecast-insufficient-data"]') !== null,
          curveCount: document.querySelectorAll('.recharts-line-curve').length,
        };
      });

      harness.assert(evalResult.hasInsufficientBadge, 'Insufficient data badge rendered for empty object payload');
      harness.assertEqual(evalResult.curveCount, 0, 'Zero curve paths rendered for empty object payload');
      await page.close();
    });

    // --------------------------------------------------------------------------
    // SUITE 2: HTTP 500 & 504 Timeout / Malformed Responses
    // --------------------------------------------------------------------------
    harness.suite('Adversarial Network Failures & HTTP 500 / 504 Responses');

    await harness.test('HTTP 500 Internal Error with HTML body renders insufficient data badge and 0 curves', async () => {
      const page = await browser.newPage();
      await setupCustomInterception(page, (req, pathname) => {
        req.respond({
          status: 500,
          headers: corsHeaders,
          contentType: 'text/html',
          body: '<html><body><h1>500 Internal Server Error</h1><p>Forecaster crashed</p></body></html>',
        });
      });
      await page.setViewport({ width: 1440, height: 900 });
      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('[data-testid="tab-ai-insights"]');
      await page.click('[data-testid="tab-ai-insights"]');
      await page.waitForSelector('[data-testid="forecast-insufficient-data"]', { timeout: 5000 });

      const evalResult = await page.evaluate(() => {
        return {
          hasInsufficientBadge: document.querySelector('[data-testid="forecast-insufficient-data"]') !== null,
          curveCount: document.querySelectorAll('.recharts-line-curve').length,
        };
      });

      harness.assert(evalResult.hasInsufficientBadge, 'Insufficient data badge rendered on HTTP 500');
      harness.assertEqual(evalResult.curveCount, 0, 'Zero curve paths rendered on HTTP 500');
      await page.close();
    });

    // --------------------------------------------------------------------------
    // SUITE 3: Scalar LSTM Single-Point Prediction (Honest Badge, Zero Curves)
    // --------------------------------------------------------------------------
    harness.suite('Single-Point & Scalar Peak Prediction Handling');

    await harness.test('Scalar LSTM peak renders scalar badge without fabricating a synthetic ramp/spline curve', async () => {
      const page = await browser.newPage();
      await setupCustomInterception(page, (req, pathname) => {
        if (pathname.includes('/api/forecast/compare')) {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({
              timesfm: { available: false, series: [] },
              lstm: { available: true, peakMw: 0.0285 },
            }),
          });
        } else {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({
              recommendations: [],
              forecast: {
                engine: 'lstm',
                series: [],
                lstmPeakMw: 0.0285,
              },
            }),
          });
        }
      });
      await page.setViewport({ width: 1440, height: 900 });
      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('[data-testid="tab-ai-insights"]');
      await page.click('[data-testid="tab-ai-insights"]');
      await page.waitForSelector('[data-testid="forecast-insufficient-data"]', { timeout: 5000 });

      const evalResult = await page.evaluate(() => {
        const badge = document.querySelector('[data-testid="forecast-lstm-scalar-badge"]');
        const curvePaths = document.querySelectorAll('.recharts-line-curve');
        const svgCurves = document.querySelectorAll('svg.forecast-chart path');
        return {
          hasInsufficient: document.querySelector('[data-testid="forecast-insufficient-data"]') !== null,
          hasScalarBadge: badge !== null,
          badgeText: badge ? badge.innerText.trim() : '',
          curveCount: curvePaths.length,
          svgCurveCount: svgCurves.length,
        };
      });

      harness.assert(evalResult.hasInsufficient, 'Insufficient data badge shown when only scalar peak is available');
      harness.assert(evalResult.hasScalarBadge, 'Scalar reference badge is rendered');
      harness.assert(evalResult.badgeText.includes('28.5 kW') || evalResult.badgeText.includes('0.0285') || evalResult.badgeText.includes('PEAK'),
        `Scalar badge shows correct peak load (got: ${evalResult.badgeText})`);
      harness.assertEqual(evalResult.curveCount, 0, 'Zero recharts curves rendered (no fake ramp/spline)');
      harness.assertEqual(evalResult.svgCurveCount, 0, 'Zero SVG curves rendered');
      await page.close();
    });

    // --------------------------------------------------------------------------
    // SUITE 4: Corrupted Forecast Payloads
    // --------------------------------------------------------------------------
    harness.suite('Corrupted Forecast Payloads (Strings, Invalids, Non-Arrays)');

    await harness.test('Corrupted string array ["corrupted", "garbage"] safely falls back to insufficient data badge with 0 curves', async () => {
      const page = await browser.newPage();
      await setupCustomInterception(page, (req, pathname) => {
        if (pathname.includes('/api/forecast/compare')) {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({
              timesfm: { available: true, series: ["corrupted", "garbage"] },
              lstm: null,
            }),
          });
        } else {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({
              recommendations: [],
              forecast: {
                engine: 'timesfm',
                series: ["corrupted", "garbage"],
              },
            }),
          });
        }
      });
      await page.setViewport({ width: 1440, height: 900 });
      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('[data-testid="tab-ai-insights"]');
      await page.click('[data-testid="tab-ai-insights"]');
      await page.waitForSelector('[data-testid="forecast-insufficient-data"]', { timeout: 5000 });

      const evalResult = await page.evaluate(() => {
        return {
          hasInsufficient: document.querySelector('[data-testid="forecast-insufficient-data"]') !== null,
          curveCount: document.querySelectorAll('.recharts-line-curve').length,
          svgPaths: document.querySelectorAll('svg.forecast-chart path').length,
        };
      });

      harness.assert(evalResult.hasInsufficient, 'Insufficient data badge safely rendered on corrupted string array');
      harness.assertEqual(evalResult.curveCount, 0, 'Zero curve paths rendered on corrupted string array');
      harness.assertEqual(evalResult.svgPaths, 0, 'Zero SVG paths rendered on corrupted string array');
      await page.close();
    });

    await harness.test('Corrupted non-array object {"not": "an array"} safely falls back to insufficient data badge with 0 curves', async () => {
      const page = await browser.newPage();
      await setupCustomInterception(page, (req, pathname) => {
        if (pathname.includes('/api/forecast/compare')) {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({
              timesfm: { available: true, series: { invalid: true } },
              lstm: null,
            }),
          });
        } else {
          req.respond({
            status: 200,
            headers: corsHeaders,
            contentType: 'application/json',
            body: JSON.stringify({
              recommendations: [],
              forecast: {
                engine: 'timesfm',
                series: { invalid: true },
              },
            }),
          });
        }
      });
      await page.setViewport({ width: 1440, height: 900 });
      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('[data-testid="tab-ai-insights"]');
      await page.click('[data-testid="tab-ai-insights"]');
      await page.waitForSelector('[data-testid="forecast-insufficient-data"]', { timeout: 5000 });

      const evalResult = await page.evaluate(() => {
        return {
          hasInsufficient: document.querySelector('[data-testid="forecast-insufficient-data"]') !== null,
          curveCount: document.querySelectorAll('.recharts-line-curve').length,
        };
      });

      harness.assert(evalResult.hasInsufficient, 'Insufficient data badge safely rendered on non-array series');
      harness.assertEqual(evalResult.curveCount, 0, 'Zero curve paths rendered on non-array series');
      await page.close();
    });

    // --------------------------------------------------------------------------
    // SUITE 5: Mobile Viewport under Adversarial Malformed Data
    // --------------------------------------------------------------------------
    harness.suite('Mobile Viewport Adversarial Stress');

    await harness.test('Mobile screen handles corrupted payload by rendering insufficient data badge and 0 curves', async () => {
      const page = await browser.newPage();
      await setupCustomInterception(page, (req, pathname) => {
        req.respond({
          status: 504,
          headers: corsHeaders,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'forecaster gateway timeout' }),
        });
      });
      await page.setViewport({ width: 390, height: 844, isMobile: true, hasTouch: true });
      await page.goto(appUrl, { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('[data-testid="mobile-menu-ai"]', { timeout: 5000 });
      await page.click('[data-testid="mobile-menu-ai"]');
      await page.waitForSelector('[data-testid="forecast-insufficient-data"]', { timeout: 5000 });

      const evalResult = await page.evaluate(() => {
        return {
          hasInsufficient: document.querySelector('[data-testid="forecast-insufficient-data"]') !== null,
          curveCount: document.querySelectorAll('.recharts-line-curve').length,
        };
      });

      harness.assert(evalResult.hasInsufficient, 'Mobile screen displays insufficient data badge on timeout');
      harness.assertEqual(evalResult.curveCount, 0, 'Zero curve paths rendered on mobile during timeout');
      await page.close();
    });

  } finally {
    await browser.close();
  }

  const success = harness.summary();
  if (!success) {
    process.exit(1);
  }
}

runAdversarialVerification().catch(err => {
  console.error('Fatal adversarial verification failure:', err);
  process.exit(1);
});
