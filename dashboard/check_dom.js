import puppeteer from 'puppeteer';

(async () => {
  const browser = await puppeteer.launch({ headless: 'new' });
  const page = await browser.newPage();
  
  page.on('console', msg => console.log('BROWSER LOG:', msg.type(), msg.text()));
  page.on('pageerror', err => console.log('PAGE ERROR:', err.message));
  
  try {
    await page.goto('http://localhost:5173', { waitUntil: 'domcontentloaded' });
    await new Promise(r => setTimeout(r, 2000));
    const data = await page.evaluate(() => {
      const children = document.querySelector('.hud-container')?.children.length;
      const leftDock = document.querySelector('.hud-container > div:last-child')?.innerText;
      return { children, leftDock };
    });
    console.log('DOM DATA:', JSON.stringify(data));
  } catch (e) {
    console.log('Navigation error:', e.message);
  }
  
  await browser.close();
  process.exit(0);
})();
