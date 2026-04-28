const express = require("express");
const { chromium } = require("playwright");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { execSync } = require("child_process");

const app = express();
app.use(express.json());

const PORT = 3100;
const CHROME_PATH = process.env.CHROME_PATH || "/usr/bin/chromium";
const SOURCE_PROFILE = process.env.CHROME_USER_DATA || path.join(os.homedir(), ".config/chromium");
const WORK_PROFILE = path.join(os.homedir(), ".cache/paywall-fetcher-profile");

// Copy cookie/storage files from real Chrome profile to work profile
function syncProfile() {
  fs.mkdirSync(path.join(WORK_PROFILE, "Default"), { recursive: true });

  const filesToCopy = [
    "Default/Cookies",
    "Default/Login Data",
    "Default/Web Data",
    "Default/Preferences",
    "Default/Secure Preferences",
    "Local State",
  ];

  for (const f of filesToCopy) {
    const src = path.join(SOURCE_PROFILE, f);
    const dst = path.join(WORK_PROFILE, f);
    try {
      fs.mkdirSync(path.dirname(dst), { recursive: true });
      fs.copyFileSync(src, dst);
      // Also copy WAL/SHM files for SQLite databases
      for (const suffix of ["-wal", "-shm", "-journal"]) {
        try { fs.copyFileSync(src + suffix, dst + suffix); } catch(e) {}
      }
    } catch (e) {
      // File may not exist, that's ok
    }
  }
  console.log("Profile synced from", SOURCE_PROFILE);
}

let browser;

app.post("/fetch", async (req, res) => {
  const { url } = req.body;
  if (!url) return res.status(400).json({ error: "url required" });

  let page;
  try {
    page = await browser.newPage();
    await page.goto(url, { waitUntil: "domcontentloaded", timeout: 30000 });
    await page.waitForTimeout(3000);

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
      let text = "";
      if (paragraphs.length > 0) {
        text = Array.from(paragraphs)
          .map((p) => p.textContent.trim())
          .filter((t) => t.length > 0)
          .join("\n\n");
      } else {
        text = articleEl.textContent.trim();
      }
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
  } catch (e) {
    console.error(`Fetch error for ${url}:`, e.message);
    res.status(500).json({ error: e.message });
  } finally {
    if (page) await page.close();
  }
});

// POST /sync - re-sync cookies from real Chrome profile
app.post("/sync", (req, res) => {
  try {
    syncProfile();
    res.json({ ok: true });
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

app.get("/health", (req, res) => res.json({ ok: true, mode: "native-profile" }));

(async () => {
  syncProfile();

  browser = await chromium.launchPersistentContext(WORK_PROFILE, {
    executablePath: CHROME_PATH,
    headless: true,
    args: [
      "--no-sandbox",
      "--disable-blink-features=AutomationControlled",
      "--disable-dev-shm-usage",
    ],
    ignoreDefaultArgs: ["--enable-automation"],
  });

  console.log(`Paywall fetcher (native profile) listening on :${PORT}`);
  console.log(`Chrome: ${CHROME_PATH}`);
  console.log(`Source profile: ${SOURCE_PROFILE}`);
  console.log(`Work profile: ${WORK_PROFILE}`);
  app.listen(PORT);
})();
