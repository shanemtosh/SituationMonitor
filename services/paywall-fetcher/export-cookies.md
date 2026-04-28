# Cookie Export for Paywall Fetcher

## Quick method: browser extension

1. Install "Cookie-Editor" extension ([Chrome](https://chromewebstore.google.com/detail/cookie-editor/hlkenndednhfkekhgcdicdfddnkalmdm) / [Firefox](https://addons.mozilla.org/en-US/firefox/addon/cookie-editor/))
2. Log in to the publication (ft.com, wsj.com, nytimes.com) in your browser
3. Click the Cookie-Editor icon → "Export" → "Export as JSON"
4. Run the curl command below, pasting the exported JSON

```bash
# Replace DOMAIN and paste the JSON array from Cookie-Editor
curl -X POST http://situation.mto.sh:3100/cookies \
  -H 'Content-Type: application/json' \
  -d '{"DOMAIN": PASTE_JSON_HERE}'
```

Example for FT:
```bash
curl -X POST http://127.0.0.1:3100/cookies \
  -H 'Content-Type: application/json' \
  -d '{"ft.com": [{"name":"FTSession","value":"abc123",...}]}'
```

## Cookie-Editor JSON → Playwright format

Cookie-Editor exports cookies in a format that needs minor conversion.
The fetcher accepts Playwright's cookie format:
- `name`, `value`, `domain`, `path` (required)
- `expires` (unix timestamp), `httpOnly`, `secure`, `sameSite`

Cookie-Editor's format is close enough — the fetcher will use them as-is.

## Verify

```bash
curl http://127.0.0.1:3100/cookies
# Should show: {"domains":[{"domain":"ft.com","count":61},{"domain":"wsj.com","count":...}]}
```
