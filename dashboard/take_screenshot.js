import puppeteer from 'puppeteer';
(async () => {
  const browser = await puppeteer.launch({ headless: 'new' });
  const page = await browser.newPage();
  await page.setViewport({ width: 1200, height: 800 });
  await page.goto('http://localhost:5173', { waitUntil: 'networkidle0', timeout: 5000 }).catch(()=>{});
  console.log('Waiting 8 seconds...');
  await new Promise(r => setTimeout(r, 8000));
  await page.screenshot({ path: 'screenshot.png' });
  console.log('Saved screenshot.png');
  await browser.close();
})();
