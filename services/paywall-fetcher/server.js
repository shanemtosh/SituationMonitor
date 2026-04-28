const express = require("express");
const { chromium } = require("playwright-extra");
const StealthPlugin = require("puppeteer-extra-plugin-stealth");
const fs = require("fs");

chromium.use(StealthPlugin());

const app = express();
app.use(express.json());

const COOKIES_PATH = "/data/cookies.json";
const PORT = 3100;

let browser;

// Dismiss common cookie/consent/GDPR dialogs that block page interaction.
async function dismissConsentDialogs(page) {
  // Common consent button selectors
  const consentButtons = [
    // Generic consent managers
    'button[title="Accept"]',
    'button[title="Accept All"]',
    'button[title="Accept all"]',
    'button[aria-label="Accept"]',
    'button[aria-label="Accept All"]',
    '#onetrust-accept-btn-handler',
    '.onetrust-accept-btn-handler',
    '[data-testid="accept-button"]',
    // SP consent (FT, etc.)
    'button.sp_choice_type_11',
    'button[title="Accept all"]',
    // CMP consent frameworks
    '.cmp-accept-all',
    '#cmp-btn-accept',
    '[data-action="accept"]',
    // Common patterns
    'button:has-text("Accept All")',
    'button:has-text("Accept all")',
    'button:has-text("I Accept")',
    'button:has-text("I agree")',
    'button:has-text("Agree")',
    'button:has-text("OK")',
    'button:has-text("Continue")',
  ];

  // Try to click consent buttons in iframes first
  for (const frame of page.frames()) {
    for (const sel of consentButtons) {
      try {
        const btn = frame.locator(sel).first();
        if (await btn.isVisible({ timeout: 500 })) {
          await btn.click({ timeout: 2000 });
          await page.waitForTimeout(500);
          return;
        }
      } catch (e) { /* not found, try next */ }
    }
  }

  // Try main page
  for (const sel of consentButtons) {
    try {
      const btn = page.locator(sel).first();
      if (await btn.isVisible({ timeout: 500 })) {
        await btn.click({ timeout: 2000 });
        await page.waitForTimeout(500);
        return;
      }
    } catch (e) { /* not found, try next */ }
  }

  // Nuclear option: remove overlay iframes that contain "consent" or "cookie"
  try {
    await page.evaluate(() => {
      document.querySelectorAll('div[id*="consent"], div[id*="cookie"], div[id*="sp_message"]').forEach(el => {
        el.remove();
      });
      // Also remove any overlay that blocks interaction
      document.querySelectorAll('[aria-modal="true"]').forEach(el => {
        el.remove();
      });
    });
  } catch (e) { /* best effort */ }
}

// Load saved cookies (array of Playwright cookie objects grouped by domain)
// Format: { "nytimes.com": [...cookies], "wsj.com": [...cookies] }
function loadCookies() {
  try {
    if (fs.existsSync(COOKIES_PATH)) {
      return JSON.parse(fs.readFileSync(COOKIES_PATH, "utf8"));
    }
  } catch (e) {
    console.error("Failed to load cookies:", e.message);
  }
  return {};
}

// Normalize Cookie-Editor format to Playwright format
function normalizeCookies(cookies) {
  return cookies.map(c => {
    const norm = { name: c.name, value: c.value, domain: c.domain, path: c.path || "/" };
    // Map sameSite values
    const ss = (c.sameSite || "").toLowerCase();
    if (ss === "no_restriction" || ss === "none") norm.sameSite = "None";
    else if (ss === "lax") norm.sameSite = "Lax";
    else if (ss === "strict") norm.sameSite = "Strict";
    else norm.sameSite = "Lax"; // safe default
    if (c.expirationDate) norm.expires = c.expirationDate;
    if (c.httpOnly) norm.httpOnly = true;
    if (c.secure) norm.secure = true;
    return norm;
  });
}

function cookiesForURL(url) {
  const cookies = loadCookies();
  const hostname = new URL(url).hostname;
  const matched = [];
  for (const [domain, domainCookies] of Object.entries(cookies)) {
    if (hostname.includes(domain)) {
      matched.push(...domainCookies);
    }
  }
  return normalizeCookies(matched);
}

// POST /fetch { url: "https://..." }
// Returns { title, content, url }
app.post("/fetch", async (req, res) => {
  const { url } = req.body;
  if (!url) return res.status(400).json({ error: "url required" });

  let context;
  try {
    context = await browser.newContext({
      userAgent:
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    });

    const cookies = cookiesForURL(url);
    if (cookies.length > 0) {
      await context.addCookies(cookies);
    }

    const page = await context.newPage();
    await page.goto(url, { waitUntil: "domcontentloaded", timeout: 30000 });

    // Wait a bit for dynamic content
    await page.waitForTimeout(2000);

    // Dismiss consent dialogs that may block content
    await dismissConsentDialogs(page);

    // Extract article content using common selectors
    const result = await page.evaluate(() => {
      // Try common article selectors
      const selectors = [
        "article",
        '[role="article"]',
        ".article-body",
        ".article__body",
        ".story-body",
        ".post-content",
        ".entry-content",
        "#article-body",
        ".article-content",
        ".wsj-snippet-body",
        ".meteredContent",
        ".StoryBodyCompanionColumn",
        "main",
      ];

      let articleEl = null;
      for (const sel of selectors) {
        articleEl = document.querySelector(sel);
        if (articleEl && articleEl.textContent.trim().length > 200) break;
      }

      if (!articleEl) articleEl = document.body;

      // Remove unwanted elements
      const removes = articleEl.querySelectorAll(
        "script, style, nav, footer, .ad, .advertisement, .social-share, .related-articles, [data-ad], .newsletter-signup"
      );
      removes.forEach((el) => el.remove());

      // Get paragraphs
      const paragraphs = articleEl.querySelectorAll("p");
      let text = "";
      if (paragraphs.length > 0) {
        text = Array.from(paragraphs)
          .map((p) => p.textContent.trim())
          .filter((t) => t.length > 0)
          .join("\n\n");
      } else {
        text = articleEl.textContent.trim();
      }

      return {
        title: document.title,
        content: text,
      };
    });

    // Normalize whitespace
    let content = result.content
      .replace(/[ \t]+/g, " ")
      .replace(/\n{3,}/g, "\n\n")
      .trim();

    // Truncate if huge
    if (content.length > 50000) {
      content = content.substring(0, 50000) + "\n\n[Truncated]";
    }

    res.json({ title: result.title, content, url });
  } catch (e) {
    console.error(`Fetch error for ${url}:`, e.message);
    res.status(500).json({ error: e.message });
  } finally {
    if (context) await context.close();
  }
});

// POST /login - automate login to a publication and save cookies
// Body: { "domain": "ft.com", "loginUrl": "https://...", "email": "...", "password": "...", "selectors": {...} }
app.post("/login", async (req, res) => {
  const { domain, loginUrl, email, password, selectors } = req.body;
  if (!domain || !loginUrl || !email || !password) {
    return res.status(400).json({ error: "domain, loginUrl, email, password required" });
  }

  let context;
  try {
    context = await browser.newContext({
      userAgent:
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    });
    const page = await context.newPage();

    // Navigate to login page
    await page.goto(loginUrl, { waitUntil: "domcontentloaded", timeout: 30000 });
    await page.waitForTimeout(2000);

    // Dismiss cookie/consent dialogs that block interaction
    await dismissConsentDialogs(page);

    const sel = selectors || {};
    const emailSel = sel.email || 'input[type="email"], input[name="email"], input[name="username"], #email, #username';
    const passSel = sel.password || 'input[type="password"], #password';
    const submitSel = sel.submit || 'button[type="submit"], input[type="submit"]';
    const emailSubmitSel = sel.emailSubmit || null; // for multi-step logins (e.g., FT)

    // Fill email
    await page.fill(emailSel, email);

    // Some sites have multi-step login (email first, then password)
    if (emailSubmitSel) {
      await page.click(emailSubmitSel);
      await page.waitForTimeout(3000);
      await dismissConsentDialogs(page);
    }

    // Check if password field is visible (multi-step might show it after email submit)
    try {
      await page.waitForSelector(passSel, { state: "visible", timeout: 5000 });
      await page.fill(passSel, password);
      await page.click(submitSel);
    } catch (e) {
      // Password field not found — might be email-only step, try submitting email form
      if (!emailSubmitSel) {
        await page.click(submitSel);
        await page.waitForTimeout(3000);
        await dismissConsentDialogs(page);
        // Now try password
        await page.waitForSelector(passSel, { state: "visible", timeout: 10000 });
        await page.fill(passSel, password);
        await page.click(submitSel);
      } else {
        throw e;
      }
    }

    // Wait for navigation after login
    await page.waitForTimeout(5000);

    // Capture cookies
    const cookies = await context.cookies();
    const existing = loadCookies();
    existing[domain] = cookies;
    fs.writeFileSync(COOKIES_PATH, JSON.stringify(existing, null, 2));

    const finalUrl = page.url();
    console.log(`Login ${domain}: captured ${cookies.length} cookies, landed on ${finalUrl}`);

    res.json({
      ok: true,
      domain,
      cookieCount: cookies.length,
      finalUrl,
    });
  } catch (e) {
    console.error(`Login error for ${domain}:`, e.message);
    res.status(500).json({ error: e.message, domain });
  } finally {
    if (context) await context.close();
  }
});

// POST /login/interactive - open a browser for manual login, then capture cookies
// Body: { "domain": "ft.com", "startUrl": "https://accounts.ft.com/login" }
// Step 1: POST to start - returns { sessionId }
// Step 2: Complete login manually via the Playwright browser
// Step 3: POST /login/interactive/save { sessionId, domain } to capture cookies
const activeSessions = new Map();

app.post("/login/interactive", async (req, res) => {
  const { domain, startUrl } = req.body;
  if (!domain || !startUrl) {
    return res.status(400).json({ error: "domain, startUrl required" });
  }

  try {
    const context = await browser.newContext({
      userAgent:
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    });
    const page = await context.newPage();
    await page.goto(startUrl, { waitUntil: "domcontentloaded", timeout: 30000 });

    const sessionId = Date.now().toString(36);
    activeSessions.set(sessionId, { context, page, domain });

    // Auto-cleanup after 5 minutes
    setTimeout(() => {
      if (activeSessions.has(sessionId)) {
        activeSessions.get(sessionId).context.close().catch(() => {});
        activeSessions.delete(sessionId);
      }
    }, 5 * 60 * 1000);

    console.log(`Interactive session ${sessionId} started for ${domain}`);
    res.json({ sessionId, domain, message: "Session open. Complete login, then POST /login/interactive/save" });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// POST /login/interactive/navigate - navigate the session to a URL or take a screenshot
app.post("/login/interactive/navigate", async (req, res) => {
  const { sessionId, url, action } = req.body;
  const session = activeSessions.get(sessionId);
  if (!session) return res.status(404).json({ error: "session not found" });

  try {
    if (url) {
      await session.page.goto(url, { waitUntil: "domcontentloaded", timeout: 30000 });
      await session.page.waitForTimeout(2000);
    }

    if (action === "screenshot") {
      const buf = await session.page.screenshot({ fullPage: false });
      res.set("Content-Type", "image/png");
      return res.send(buf);
    }

    // Return current page state
    const currentUrl = session.page.url();
    const title = await session.page.title();
    const text = await session.page.evaluate(() => document.body.innerText.substring(0, 2000));
    res.json({ currentUrl, title, textPreview: text });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// POST /login/interactive/type - type into a field or click a button
app.post("/login/interactive/type", async (req, res) => {
  const { sessionId, selector, text, click, js } = req.body;
  const session = activeSessions.get(sessionId);
  if (!session) return res.status(404).json({ error: "session not found" });

  try {
    // Run arbitrary JS first (useful for removing overlays)
    if (js) {
      await session.page.evaluate(js);
      await session.page.waitForTimeout(500);
    }
    if (text && selector) {
      await session.page.fill(selector, text);
    }
    if (click) {
      await session.page.click(click, { timeout: 5000, force: true });
      await session.page.waitForTimeout(2000);
    }
    const currentUrl = session.page.url();
    const title = await session.page.title();
    res.json({ ok: true, currentUrl, title });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// POST /login/interactive/save - capture cookies from session and save them
app.post("/login/interactive/save", async (req, res) => {
  const { sessionId } = req.body;
  const session = activeSessions.get(sessionId);
  if (!session) return res.status(404).json({ error: "session not found" });

  try {
    const cookies = await session.context.cookies();
    const existing = loadCookies();
    existing[session.domain] = cookies;
    fs.writeFileSync(COOKIES_PATH, JSON.stringify(existing, null, 2));

    const finalUrl = session.page.url();
    console.log(`Interactive login ${session.domain}: saved ${cookies.length} cookies from ${finalUrl}`);

    await session.context.close();
    activeSessions.delete(sessionId);

    res.json({ ok: true, domain: session.domain, cookieCount: cookies.length, finalUrl });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// GET /health
app.get("/health", (req, res) => res.json({ ok: true }));

// POST /cookies - update saved cookies
// Body: { "nytimes.com": [...cookies], "wsj.com": [...cookies] }
app.post("/cookies", (req, res) => {
  try {
    fs.writeFileSync(COOKIES_PATH, JSON.stringify(req.body, null, 2));
    res.json({ ok: true, domains: Object.keys(req.body) });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

// GET /cookies - list configured domains
app.get("/cookies", (req, res) => {
  const cookies = loadCookies();
  const domains = Object.entries(cookies).map(([domain, c]) => ({
    domain,
    count: c.length,
  }));
  res.json({ domains });
});

(async () => {
  browser = await chromium.launch({
    args: ["--no-sandbox", "--disable-setuid-sandbox"],
  });
  console.log(`Paywall fetcher listening on :${PORT}`);
  app.listen(PORT);
})();
