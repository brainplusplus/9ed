# PRP: In-App Browser for 9ed

## Goal

Add a first-class Browser panel to 9ed so users can open web apps that are reachable from the machine running the 9ed server, including local development servers such as `localhost:3000`, and so chat agents can inspect and automate those pages through a controlled backend browser runtime.

## User Story

As a 9ed user, I want a Browser menu after Chat where I can open multiple browser tabs, navigate to local or internet URLs, inspect the rendered page, capture screenshots, and let the active AI agent interact with the page when I approve it.

Concrete example:

1. 9ed is running on a server or local workstation.
2. A Next.js app is running on that same machine at port `3000`.
3. The user opens the Browser panel and enters `localhost:3000`.
4. 9ed resolves that URL from the server side, not the client device, and renders the app inside 9ed.
5. The chat agent can later navigate, click, type, inspect, read console/network activity, and capture screenshots through 9ed-controlled browser tools.

## Product Scope

### MVP

- Add a Browser entry to the activity bar after Chat.
- Add a Browser panel with:
  - address bar
  - reload
  - back/forward UI placeholders
  - new tab
  - close tab
  - multi-tab tab strip
  - iframe-backed page view through a server-side reverse proxy
- Interpret bare hosts like `localhost:3000` as `http://localhost:3000`.
- Keep URL resolution server-side so `localhost` means the host running 9ed.
- Allow normal internet browsing when the 9ed server has network access.
- Strip frame-blocking response headers in the browser proxy so pages can render inside the 9ed iframe when technically possible.
- Add backend browser state APIs that can be used by the UI and future agent tools.
- Add a lazy Camoufox automation manager using `github.com/brainplusplus/go-camoufox` and its Playwright-compatible API path.
- Expose authenticated automation APIs for:
  - start/status
  - navigate
  - screenshot
  - inspect basic page metadata/text
  - click by selector
  - type by selector
  - evaluate JavaScript

### Post-MVP

- Live viewport streaming from Camoufox instead of iframe proxy.
- Full pointer/keyboard event forwarding into the remote browser.
- Console log and network event timelines persisted per tab.
- WebSocket proxying for development servers with HMR.
- Agent-native ACP tool advertisement and permission prompts wired directly into chat sessions.
- WebDriver BiDi attach endpoint brokered through 9ed auth.
- Persistent browser profiles per workspace.
- Download/upload handling.

## Runtime Recommendation

Use `go-camoufox` as the primary browser runtime for automation.

Rationale:

- 9ed backend is Go, so `go-camoufox` integrates naturally without a Node sidecar.
- `go-camoufox` supports a Playwright-compatible path, which gives ergonomic automation for click/type/screenshot/locator APIs.
- `go-camoufox` also supports WebDriver BiDi, which is valuable for future protocol-level interop.
- The runtime can stay lazy: 9ed does not need to launch a real browser until automation is requested.

The design should keep a `BrowserProvider` boundary so another provider, such as Chromium/Playwright, can be added later without replacing UI or HTTP APIs.

## Architecture

```text
frontend BrowserPanel
  -> /api/browser/tabs
  -> /api/browser/tabs/{id}/navigate
  -> iframe /api/browser/proxy/{tabID}/...

chat agent / future ACP browser tools
  -> /api/browser/automation/*
  -> internal/browser.Manager
  -> go-camoufox
  -> playwright-go compatible browser/page APIs
  -> optional WebDriver BiDi in a later phase
```

## Backend Design

### Package

Create `internal/browser`.

Responsibilities:

- Normalize and validate user URLs.
- Manage lightweight UI tabs.
- Reverse-proxy tab traffic from the server side.
- Lazily launch a Camoufox browser for automation.
- Keep automation scoped behind authenticated 9ed APIs.

### URL Policy

Allowed:

- `http://...`
- `https://...`
- bare hostnames like `localhost:3000`, normalized to `http://localhost:3000`

Rejected:

- `file://`
- `ftp://`
- `javascript:`
- empty or malformed URLs

Security note: local/private network access is intentionally allowed because this feature exists to inspect local development services. It must remain behind 9ed Basic Auth and future agent permission prompts.

### API

UI/state:

- `GET /api/browser/state`
- `POST /api/browser/tabs`
- `GET /api/browser/tabs`
- `POST /api/browser/tabs/{id}/navigate`
- `DELETE /api/browser/tabs/{id}`
- `ANY /api/browser/proxy/{id}/...`

Automation:

- `GET /api/browser/automation/status`
- `POST /api/browser/automation/start`
- `POST /api/browser/automation/navigate`
- `POST /api/browser/automation/click`
- `POST /api/browser/automation/type`
- `POST /api/browser/automation/evaluate`
- `GET /api/browser/automation/inspect`
- `GET /api/browser/automation/screenshot`

### Permission Model

MVP APIs are protected by existing 9ed Basic Auth.

Agent integration must later add explicit permission requests before an agent can:

- navigate to a new origin
- click/type into a page
- evaluate JavaScript
- access screenshots
- inspect localhost/private network pages

## Frontend Design

Add `BrowserPanel` under `frontend/src/components/browser`.

Layout:

- top tab strip with stable tab dimensions
- compact address toolbar
- iframe viewport filling remaining space
- status/error band only when useful

Avoid a marketing/empty feature description page. The panel should be usable immediately.

## Risks

- Some sites intentionally block iframe embedding. The proxy strips common frame blockers, but JavaScript frame detection can still break some pages.
- HMR WebSockets for local dev servers may need a dedicated WebSocket reverse proxy later.
- Camoufox may not be installed on a user's machine. The UI browser still works through the proxy; automation reports a clear runtime error.
- Full agent integration depends on how each ACP adapter exposes tools. The PRP keeps the API ready while avoiding a brittle adapter-specific hack.

## Acceptance Criteria

- Browser appears in the activity bar after Chat.
- Browser supports multiple tabs.
- `localhost:3000` navigates through the 9ed server-side proxy.
- Browser still supports external `https://` URLs.
- Backend exposes browser tab APIs and automation APIs.
- Automation manager builds against `go-camoufox`.
- Screenshot/inspect/click/type/evaluate endpoints are implemented and return clear errors if Camoufox cannot launch.
- `npm run build`, `npm run test`, `npm run go:test`, and `npm run check` pass or failures are documented.
- Release metadata is updated to `v0.1.4`.
