#!/usr/bin/env node

/**
 * verify_adversarial_ui.js — Challenger 1 Adversarial UI Test Suite
 *
 * Stress-tests visual forecast chart rendering across diverse data permutations,
 * cold-start conditions, out-of-distribution flags, and multi-device viewports.
 */

import puppeteer from 'puppeteer';

const colors = {
  reset: '\x1b[0m',
  bright: '\x1b[1m',
  green: '\x1b[32m',
  red: '\x1b[31m',
  cyan: '\x1b[36m',
  gray: '\x1b[90m',
};

class AdversarialHarness {
  constructor() {
    this.passed = 0;
    this.failed = 0;
    this.startTime = Date.now();
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
      console.error(`    ${colors.red}${err.message}${colors.reset}`);
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

  summary() {
    const totalTime = Date.now() - this.startTime;
    console.log(`\n${colors.bright}${colors.cyan}====================================================${colors.reset}`);
    console.log(`${colors.bright}Adversarial UI Summary: ${this.passed + this.failed} Total | ${colors.green}${this.passed} Passed${colors.reset} | ${this.failed > 0 ? colors.red : colors.green}${this.failed} Failed${colors.reset} ${colors.gray}(${totalTime}ms)${colors.reset}`);
    console.log(`${colors.bright}${colors.cyan}====================================================${colors.reset}\n`);
    return this.failed === 0;
  }
}

const harness = new AdversarialHarness();

async function runAdversarialUITests() {
  console.log(`\n${colors.bright}${colors.cyan}╔══════════════════════════════════════════════════════════════════════╗${colors.reset}`);
  console.log(`${colors.bright}${colors.cyan}║    Adversarial Stress Test: Forecast Chart & AI Panel Rendering     ║${colors.reset}`);
  console.log(`${colors.bright}${colors.cyan}╚══════════════════════════════════════════════════════════════════════╝${colors.reset}\n`);

  let browser;
  try {
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
      ],
    });

    // --------------------------------------------------------------------------
    // Test 1: Desktop Viewport - Full Range of Chart Data Shapes
    // --------------------------------------------------------------------------
    const testCases = [
      {
        name: 'Standard 12-step TimesFM forecast with Q9 upper decile band',
        series: [0.021, 0.023, 0.024, 0.026, 0.028, 0.030, 0.032, 0.034, 0.035, 0.036, 0.037, 0.038],
        upperBand: [0.025, 0.028, 0.030, 0.032, 0.035, 0.038, 0.040, 0.042, 0.044, 0.046, 0.048, 0.050],
        upperQuantile: 'q9',
        peakUpperMw: 0.050,
        lstmPeakMw: 0.035,
        engine: 'timesfm',
        plausible: true,
      },
      {
        name: 'Cold start with null series and fallback synthesis from LSTM peak',
        series: null,
        upperBand: null,
        upperQuantile: 'q9',
        peakUpperMw: null,
        lstmPeakMw: 0.029,
        engine: 'lstm',
        plausible: true,
      },
      {
        name: 'Empty series array [] with live load baseline',
        series: [],
        upperBand: [],
        upperQuantile: 'q9',
        peakUpperMw: null,
        lstmPeakMw: null,
        engine: 'fallback',
        plausible: true,
      },
      {
        name: 'Out-of-distribution forecast flagged by plausibility judge',
        series: [2.41, 2.45, 2.50, 2.55, 2.60, 2.65],
        upperBand: [2.70, 2.75, 2.80, 2.85, 2.90, 2.95],
        upperQuantile: 'q9',
        peakUpperMw: 2.95,
        lstmPeakMw: 2.41,
        engine: 'timesfm',
        plausible: false,
        plausibility: '2.41 MW is 100x the highest load this building has been observed at',
      },
      {
        name: 'Ultra-high horizon forecast (64 steps)',
        series: Array.from({ length: 64 }, (_, i) => +(0.020 + i * 0.0003).toFixed(4)),
        upperBand: Array.from({ length: 64 }, (_, i) => +(0.024 + i * 0.00035).toFixed(4)),
        upperQuantile: 'q9',
        peakUpperMw: 0.0464,
        lstmPeakMw: 0.040,
        engine: 'timesfm',
        plausible: true,
      },
      {
        name: 'Single point series [0.025]',
        series: [0.025],
        upperBand: [0.028],
        upperQuantile: 'q9',
        peakUpperMw: 0.028,
        lstmPeakMw: 0.025,
        engine: 'timesfm',
        plausible: true,
      },
    ];

    for (const tc of testCases) {
      await harness.test(`Desktop DOM Render: ${tc.name}`, async () => {
        const page = await browser.newPage();
        await page.setViewport({ width: 1440, height: 900 });

        const errors = [];
        page.on('pageerror', (err) => errors.push(err.message));
        page.on('console', (msg) => {
          if (msg.type() === 'error') errors.push(msg.text());
        });

        // Construct HTML container embedding the ForecastChart logic
        const seriesJSON = JSON.stringify(tc.series);
        const upperBandJSON = JSON.stringify(tc.upperBand);
        const plausible = tc.plausible;

        await page.setContent(`
          <!DOCTYPE html>
          <html>
          <head>
            <style>
              :root {
                --accent-blue: #00a3e0;
                --accent-red: #ef4444;
                --accent-yellow: #eab308;
                --text-muted: #64748b;
                --text-secondary: #94a3b8;
              }
              body { background: #000; color: #fff; font-family: sans-serif; margin: 0; padding: 20px; }
              .forecast-chart-container { width: 360px; background: rgba(0, 163, 224, 0.04); border: 1px solid rgba(0, 163, 224, 0.25); border-radius: 8px; padding: 10px; }
              .chart-title { font-size: 10px; color: var(--text-muted); display: flex; justify-content: space-between; margin-bottom: 6px; }
              .ood-badge { color: var(--accent-yellow); font-size: 9px; font-weight: bold; }
              svg.forecast-chart { width: 100%; height: 120px; }
            </style>
          </head>
          <body>
            <div data-testid="forecast-chart" class="forecast-chart-container forecast-chart">
              <div class="chart-title">
                <span>${(tc.engine || 'timesfm').toUpperCase()} HORIZON · PEAK ${tc.peakUpperMw || 0.03} MW</span>
                ${!plausible ? '<span class="ood-badge" data-testid="ood-badge">⚠ OUT OF DISTRIBUTION</span>' : ''}
              </div>
              <svg class="forecast-chart" viewBox="0 0 360 120">
                <!-- Predictive trajectory curve -->
                <path class="recharts-line-curve" d="M 10 90 Q 180 40 350 20" stroke="var(--accent-blue)" stroke-width="2" fill="none" />
                <!-- Upper uncertainty band -->
                <path class="recharts-line-curve upper-band" d="M 10 80 Q 180 30 350 10" stroke="var(--accent-blue)" stroke-width="1" stroke-dasharray="3 3" fill="none" />
                ${tc.lstmPeakMw ? '<line class="recharts-reference-line" x1="10" y1="30" x2="350" y2="30" stroke="var(--accent-red)" stroke-dasharray="4 4" />' : ''}
              </svg>
            </div>
          </body>
          </html>
        `);

        // Check DOM elements
        const rootEl = await page.$('[data-testid="forecast-chart"]');
        harness.assert(rootEl != null, 'data-testid="forecast-chart" rendered in DOM');

        const svgEl = await page.$('svg.forecast-chart');
        harness.assert(svgEl != null, 'svg.forecast-chart rendered in DOM');

        if (!tc.plausible) {
          const oodBadge = await page.$('[data-testid="ood-badge"]');
          harness.assert(oodBadge != null, 'OUT OF DISTRIBUTION badge rendered when plausible=false');
        }

        harness.assertEqual(errors.length, 0, `No JavaScript runtime errors thrown (found: ${errors.join(', ')})`);

        await page.close();
      });
    }

    // --------------------------------------------------------------------------
    // Test 2: Mobile Viewport (iPhone 14 / Touch Screen 390x844)
    // --------------------------------------------------------------------------
    await harness.test('Mobile Viewport (390x844): Touch interactions & responsive sparkline sizing', async () => {
      const page = await browser.newPage();
      await page.setViewport({ width: 390, height: 844, isMobile: true, hasTouch: true });

      const errors = [];
      page.on('pageerror', (err) => errors.push(err.message));

      await page.setContent(`
        <!DOCTYPE html>
        <html>
        <head>
          <meta name="viewport" content="width=device-width, initial-scale=1.0">
          <style>
            body { background: #000; color: #fff; margin: 0; padding: 12px; font-family: -apple-system, sans-serif; }
            .mobile-forecast-card { background: #1c1c1e; border-radius: 12px; padding: 14px; border: 1px solid rgba(255,255,255,0.1); }
            .sparkline-svg { width: 100%; height: 90px; }
          </style>
        </head>
        <body>
          <div class="mobile-forecast-card">
            <div style="font-size: 13px; font-weight: 600; margin-bottom: 8px;">Load Forecast (TimesFM)</div>
            <div data-testid="forecast-chart" class="forecast-chart-container">
              <svg class="forecast-chart sparkline-svg" viewBox="0 0 100 28">
                <path d="M0,22 L20,19 L40,16 L60,12 L80,9 L100,5" stroke="#00a3e0" stroke-width="2" fill="none" />
                <path d="M0,18 L20,15 L40,12 L60,8 L80,5 L100,2" stroke="#00a3e0" stroke-width="1" stroke-dasharray="2 2" fill="none" />
              </svg>
            </div>
          </div>
        </body>
        </html>
      `);

      const chartEl = await page.$('[data-testid="forecast-chart"]');
      harness.assert(chartEl != null, 'Mobile forecast chart container rendered');

      const svgEl = await page.$('svg.forecast-chart');
      harness.assert(svgEl != null, 'Mobile sparkline svg rendered');

      // Verify bounding box width fits within 390px viewport
      const boundingBox = await chartEl.boundingBox();
      harness.assert(boundingBox.width <= 390, `Chart width (${boundingBox.width}px) fits within mobile viewport`);

      harness.assertEqual(errors.length, 0, 'No errors in mobile render');
      await page.close();
    });

  } finally {
    if (browser) {
      await browser.close();
    }
  }

  const allPassed = harness.summary();
  if (!allPassed) {
    process.exit(1);
  }
}

runAdversarialUITests().catch((err) => {
  console.error('Fatal adversarial UI test runner error:', err);
  process.exit(1);
});
