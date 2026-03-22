const express = require("express");
const { chromium } = require("playwright");
const fs = require("fs");

const app = express();
app.use(express.json());

const COOKIES_PATH = "/data/cookies.json";
const PORT = 3100;

let browser;

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

function cookiesForURL(url) {
  const cookies = loadCookies();
  const hostname = new URL(url).hostname;
  for (const [domain, domainCookies] of Object.entries(cookies)) {
    if (hostname.includes(domain)) {
      return domainCookies;
    }
  }
  return [];
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
