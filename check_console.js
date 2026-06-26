import puppeteer from 'puppeteer';

(async () => {
  const browser = await puppeteer.launch({ headless: true });
  const page = await browser.newPage();
  
  page.on('console', msg => {
    if (msg.type() === 'error') {
      console.log('BROWSER ERROR:', msg.text());
    }
  });

  page.on('pageerror', error => {
    console.log('PAGE ERROR:', error.message);
  });

  try {
    await page.goto('http://localhost:5188', { waitUntil: 'networkidle2', timeout: 5000 });
  } catch (e) {
    console.log('Timeout or failed to load:', e.message);
  }
  
  await new Promise(resolve => setTimeout(resolve, 2000));
  await browser.close();
})();
