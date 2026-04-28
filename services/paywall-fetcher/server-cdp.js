const express = require("express");
const { chromium } = require("playwright");

const app = express();
app.use(express.json());

const PORT = process.env.PORT || 3100;
const CDP_URL = process.env.CDP_URL || "http://[::1]:9222";

let defaultContext;

// POST /fetch { url: "https://..." }
// Opens a new tab in your real browser session, extracts content, closes it.
app.post("/fetch", async (req, res) => {
  const { url } = req.body;
  if (!url) return res.status(400).json({ error: "url required" });

  let page;
  try {
    page = await defaultContext.newPage();
    await page.goto(url, { waitUntil: "domcontentloaded", timeout: 30000 });

    // Behave like a real reader: wait for full load, scroll a bit
    await page.waitForTimeout(2000 + Math.random() * 2000);
    await page.evaluate(() => window.scrollBy(0, 300 + Math.random() * 500));
    await page.waitForTimeout(1000 + Math.random() * 1500);

    const result = await page.evaluate(() => {
      const selectors = [
        "article", '[role="article"]',
        ".article-body", ".article__body", ".story-body",
        ".post-content", ".entry-content",
        "#article-body", ".article-content",
        ".wsj-snippet-body", ".meteredContent",
        ".StoryBodyCompanionColumn", "main",
      ];

      let articleEl = null;
      for (const sel of selectors) {
        articleEl = document.querySelector(sel);
        if (articleEl && articleEl.textContent.trim().length > 200) break;
      }
      if (!articleEl) articleEl = document.body;

      articleEl.querySelectorAll(
        "script, style, nav, footer, .ad, .advertisement, .social-share, .related-articles, [data-ad], .newsletter-signup"
      ).forEach((el) => el.remove());

      const paragraphs = articleEl.querySelectorAll("p");
      let text =
        paragraphs.length > 0
          ? Array.from(paragraphs)
              .map((p) => p.textContent.trim())
              .filter((t) => t.length > 0)
              .join("\n\n")
          : articleEl.textContent.trim();

      return { title: document.title, content: text };
    });

    let content = result.content
      .replace(/[ \t]+/g, " ")
      .replace(/\n{3,}/g, "\n\n")
      .trim();

    if (content.length > 50000) {
      content = content.substring(0, 50000) + "\n\n[Truncated]";
    }

    res.json({ title: result.title, content, url });

    // Linger briefly like a real reader before closing
    await page.waitForTimeout(2000 + Math.random() * 3000);
  } catch (e) {
    console.error(`Fetch error for ${url}:`, e.message);
    res.status(500).json({ error: e.message });
  } finally {
    if (page) await page.close();
  }
});

app.get("/health", (req, res) =>
  res.json({ ok: true, mode: "cdp-live", pages: defaultContext.pages().length })
);

(async () => {
  try {
    const browser = await chromium.connectOverCDP(CDP_URL);
    defaultContext = browser.contexts()[0];
    if (!defaultContext) {
      console.error("No default browser context found. Is Chrome running with --remote-debugging-port?");
      process.exit(1);
    }
    console.log(`Connected to Chrome CDP at ${CDP_URL}`);
    console.log(`Default context has ${defaultContext.pages().length} pages`);
    app.listen(PORT, () => console.log(`Paywall fetcher (CDP) listening on :${PORT}`));
  } catch (e) {
    console.error(`Failed to connect to Chrome CDP at ${CDP_URL}:`, e.message);
    console.error("Make sure Chrome is running with: --remote-debugging-port=9222");
    process.exit(1);
  }
})();
