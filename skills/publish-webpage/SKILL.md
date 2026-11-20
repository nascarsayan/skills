---
name: publish-webpage
description: Publishes pages on the user's Astro site (https://sayann-vm.cerebras.aws, git repo sayann-cerebras/personal-site) and answers page comments. Use when asked to publish, add, or update a webpage, article, diagram page, or standalone app, or to reply to site comments.
---

# Publish Webpage

The site is an Astro repo in git: `main` is the live site. A page is live only once it is committed and deployed. `valet repos path site` prints the checkout. Every step goes through `valet web`.

## Article (the default)

1. `valet web new <slug> --title "..." --description "..."`: creates and stages `src/pages/<slug>/index.mdx` (lowercase, hyphenated slug). The home page lists it from its frontmatter; nothing else to register.
2. Write the page in MDX. Reuse `src/components/content/*` (Diagram, Step, Callout, Kpis, Timeline, Explorer) and copy patterns from an existing page such as `src/pages/cs-6408/`. Diagrams are typed `_*.diagram.ts` specs beside the page. Every `##`/`###` heading gets an anchor automatically.
3. Inspect what the page pulls in: no secrets, no raw logs, links to harnesses instead of copies.
4. `valet web build` builds a preview into `.dist-dev/`; fix every error it reports.
5. Check the rendered page in a real browser and look at the screenshot before reporting (see Screenshots).
6. Commit in the site repo, then `valet web deploy` (refuses uncommitted changes; builds `dist/`, which Caddy serves at once).
7. Verify: `curl --cacert "$(valet repos path web)/caddy-root.crt" -sI "https://$(hostname -f)/<slug>/"` returns 200. Report the URL.

## Standalone app

Self-contained web apps live in the sandbox repo under `sayann/Sites/<App>/` (PascalCase; `valet repos path sites`), served as-is (git metadata and .env files hidden). Register each in two places: `STANDALONE` in the site's `src/config/routes.ts` (lists it on /apps/), and a `handle_path /<slug>/*` block in the valet repo's `web/Caddyfile` whose `root` is the app's served folder under `{$VALET_WEB_SITES}`. Then `valet web up` (reloads Caddy; it reads the Caddyfile from the repo), and commit + `valet web deploy` for the site. Apps open in a new tab from the site, since they have no way back.

## Comments

Readers comment per page or per section (the form at the end of every article). Reply as `llm-bot`:

1. `valet web comments list` shows threads whose last message is not llm-bot's (`--all`, `--page <slug>`).
2. Answer each from the page's content and sources: `valet web comments reply <id> "<text>"`. It prints the comment's URL.

Server and Caddy: `valet web up`. The comment and dev servers are parts `comments` and `devserver` of the `valet-daemon` user unit (`valet daemon status`, `valet daemon logs <part>`, `valet daemon restart <part>`); Caddy is the `caddy` system unit.

## Screenshots

Use `chrome-headless-shell` (Chrome for Testing headless build; on PATH). Load the page over plain loopback HTTP, never with certificate checks off:

- preview build: `(cd .dist-dev && timeout 120 python3 -m http.server 8766 --bind 127.0.0.1 &)`, then `http://127.0.0.1:8766/<slug>/`
- live-reload dev server (`valet web dev start`, the one at `https://<fqdn>:8443/`): `http://127.0.0.1:4340/<slug>/`

```sh
chrome-headless-shell --no-sandbox --hide-scrollbars --window-size=1400,1750 \
  --virtual-time-budget=8000 --screenshot="$USERLAND/Scratch/<task>/<slug>.webp" "http://127.0.0.1:8766/<slug>/"
```

Then Read the image. Use `.webp` (half the size of PNG; `.jpg` also works); never `.png`. `--virtual-time-budget` lets scripts and animations settle; raise `--window-size` height to reach content further down (the CLI cannot scroll). Dark theme: add `--force-dark-mode --blink-settings=preferredColorScheme=0`.

To screenshot after a click (open a box, maximize, a reveal-on-scroll figure), serve the page through a small loopback proxy that inserts a `<script>` before `</body>`: on `load`, add `is-in` to `[data-reveal],[data-reveal-stagger],figure.dg`, then click the target. Pick the step with a query parameter (`?st=max`). Keep the proxy in `$USERLAND/Scratch/<task>/`, never in the site repo.
