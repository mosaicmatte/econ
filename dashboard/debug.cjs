const puppeteer = require('puppeteer');

(async () => {
  const browser = await puppeteer.launch({ args: ['--no-sandbox', '--disable-setuid-sandbox'] });
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
    await page.goto('http://localhost:5173', { waitUntil: 'networkidle2', timeout: 10000 });
    console.log('Page loaded successfully.');
    // Wait a bit to see if React crashes after mounting
    await new Promise(r => setTimeout(r, 2000));
  } catch (err) {
    console.error('Failed to load page:', err);
  } finally {
    await browser.close();
  }
})();
