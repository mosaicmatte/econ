import puppeteer from 'puppeteer';

(async () => {
  const browser = await puppeteer.launch({ headless: 'new' });
  const page = await browser.newPage();
  
  await page.setViewport({ width: 375, height: 812 });
  page.on('console', msg => console.log('BROWSER LOG:', msg.type(), msg.text()));
  page.on('pageerror', err => console.log('PAGE ERROR:', err.message));
  
  try {
    await page.goto('http://localhost:5173', { waitUntil: 'domcontentloaded' });
    await new Promise(r => setTimeout(r, 2000));
    
    const data = await page.evaluate(() => {
      const isMobileApp = document.querySelector('div')?.innerText.includes('Mobile Viewer');
      return { 
        isMobileApp,
        htmlStart: document.body.innerHTML.substring(0, 500)
      };
    });
    console.log('MOBILE DATA:', JSON.stringify(data));
  } catch (e) {
    console.log('Navigation error:', e.message);
  }
  
  await browser.close();
  process.exit(0);
})();
