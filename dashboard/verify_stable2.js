import puppeteer from 'puppeteer';
(async () => {
  const browser = await puppeteer.launch({ headless: 'new' });
  const page = await browser.newPage();
  await page.setViewport({ width: 1200, height: 800 });
  let errorCount = 0;
  page.on('pageerror', err => { errorCount++; console.log('PAGE ERROR:', err.message); });
  page.on('console', msg => { if (msg.type() === 'error') console.log('CONSOLE ERROR:', msg.text()); });
  await page.goto('http://localhost:5173', { waitUntil: 'networkidle0', timeout: 8000 }).catch(()=>{});
  
  for (let i = 5; i <= 30; i += 5) {
    await new Promise(r => setTimeout(r, 5000));
    await page.screenshot({ path: `stable_${i}s.png` });
    console.log(`Screenshot at ${i}s - errors so far: ${errorCount}`);
  }
  
  console.log(`FINAL: ${errorCount} errors total after 30 seconds`);
  await browser.close();
})();
