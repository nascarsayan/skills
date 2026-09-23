---
name: publish-webpage
description: Publishes webpages and visualizations as HTTPS subroutes of the Caddy experiment hub. Use when asked to publish or update a webpage under ~/ws/www.
---

# Publish Webpage

Publish at `~/ws/www/$slug`. Routes stay live-linked and listed.

## Workflow

1. Resolve the source inside `~/ws`. Require a readable `index.html`.
2. Choose a lowercase, hyphenated slug. Default to sanitized basename. Reject empty slugs, `/`, and `..`.
3. Inspect dependencies, secrets, and root-relative URLs. Stop on unsafe findings. Include only required web assets; exclude logs, scripts, and raw evidence unless loaded.
4. Add a meaningful `<title>` and preferably `<meta name="description">`. The root index falls back to the first `<h1>`, first paragraph, and humanized slug.
5. Build a hidden staging directory beside the destination. Add relative symlinks only for required source assets, then rename it. Never link the whole source directory. Preserve an existing destination and ask before replacement.
6. Verify the route and root index with `curl --cacert ~/ws/web/caddy-root.crt` and a browser. Report URLs and evidence.

## Reloading

Source changes appear after browser refresh; no Caddy reload. `Cache-Control: no-store` prevents stale assets. For Caddyfile changes, run `cd ~/ws/web && docker compose exec caddy caddy reload --config /etc/caddy/Caddyfile`.
