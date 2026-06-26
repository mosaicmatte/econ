import puppeteer from 'puppeteer';

(async () => {
  const browser = await puppeteer.launch({ headless: 'new' });
  const page = await browser.newPage();
  
  page.on('console', msg => {
    if (msg.type() === 'error') {
      console.log('BROWSER ERROR:', msg.text());
    }
  });
  
  page.on('pageerror', err => {
    console.log('PAGE ERROR:', err.message);
  });

  try {
    await page.goto('http://localhost:5173', { waitUntil: 'networkidle0', timeout: 5000 });
  } catch (e) {
    console.log('Nav:', e.message);
  }

  await new Promise(r => setTimeout(r, 2000));
  
  const bodyHtml = await page.evaluate(() => document.body.innerHTML);
  console.log("HTML length:", bodyHtml.length);
  if (bodyHtml.length < 500) {
    console.log("HTML output:", bodyHtml);
  }
  
  await browser.close();
  process.exit(0);
})();
