---
name: publish-webpage
description: Publishes pages on the user's Astro site (https://sayann-vm.cerebras.aws, git repo sayann-cerebras/personal-site) and answers page comments. Use when asked to publish, add, or update a webpage, article, diagram page, or standalone app, or to reply to site comments.
---

# Publish Webpage

The site is an Astro repo in git: `main` is the live site. A page is live only once it is committed and deployed. `wt site` enters the repo. Every step goes through `dev web`.

## Article (the default)

1. `dev web new <slug> --title "..." --description "..."`: creates and stages `src/pages/<slug>/index.mdx` (lowercase, hyphenated slug). The home page lists it from its frontmatter; nothing else to register.
2. Write the page in MDX. Reuse `src/components/content/*` (Diagram, Step, Callout, Kpis, Timeline, Explorer) and copy patterns from an existing page such as `src/pages/cs-6408/`. Diagrams are typed `_*.diagram.ts` specs beside the page. Every `##`/`###` heading gets an anchor automatically.
3. Inspect what the page pulls in: no secrets, no raw logs, links to harnesses instead of copies.
4. `dev web build` builds a preview into `.dist-dev/`; fix every error it reports.
5. Check the rendered page in a real browser (headless Firefox) and look at the screenshot before reporting.
6. Commit in the site repo, then `dev web deploy` (refuses uncommitted changes; builds `dist/`, which Caddy serves at once).
7. Verify: `curl --cacert "$(dev cd dev)/web/caddy-root.crt" -sI "https://$(hostname -f)/<slug>/"` returns 200. Report the URL.

## Standalone app

Self-contained HTML apps live in `~/ws/www/<slug>/`, served as-is. Register each in two places: `STANDALONE` in the site's `src/config/routes.ts` (lists it on /apps/), and the `@standalone` matcher in the dev repo's `web/Caddyfile`. Then `dev web up` (reloads Caddy; it reads the Caddyfile from the repo), and commit + `dev web deploy` for the site. Apps open in a new tab from the site, since they have no way back.

## Comments

Readers comment per page or per section (the form at the end of every article). Reply as `llm-bot`:

1. `dev web comments list` shows threads whose last message is not llm-bot's (`--all`, `--page <slug>`).
2. Answer each from the page's content and sources: `dev web comments reply <id> "<text>"`. It prints the comment's URL.

Server and Caddy: `dev web up` (units `dev-web-comments` (user) and `dev-web-caddy` (system)). Headless Firefox needs the site CA: `dev cd dev`/web/caddy-root.crt.
