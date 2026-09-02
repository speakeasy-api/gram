#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Hold this worktree's site port while the stack is paused and wake it on the first browser hit"
//MISE dir="{{ config_root }}"
//MISE hide=true

// `mise run pause` leaves the stack complete but stopped, which also leaves the
// dashboard URL dead — so a bookmark, a `wt list` link or an agent's browser
// step lands on a connection error and the developer has to know that `mise run
// wake` exists. This task parks on the site port in the stack's place: the
// first request gets an interstitial that starts the wake and reloads itself
// when the real dashboard answers.
//
// The parker cannot proxy the dashboard once it is up (vite wants the port
// itself), so it hands the port over instead. It keeps serving the interstitial
// to every request -- a second tab, a reload, a probe -- while the wake it
// started brings the containers up, and `mise run wake` kills it at the last
// moment, immediately before vite binds. That leaves only a couple of seconds
// where nothing is listening, which the interstitial covers by polling from the
// browser: the page is already loaded, so a refused fetch is just another retry.
//
// Started detached by `mise run pause`. Killed by `mise run wake`; the timeout
// below is the backstop for a wake that dies before it gets there, since a
// parker that outlived its wake would hold the port against vite forever.

import { spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import https from "node:https";
import path from "node:path";

const port = Number(process.env["GRAM_SITE_PORT"]);
if (!Number.isInteger(port) || port <= 0) {
  console.error("GRAM_SITE_PORT is not set to a port number");
  process.exit(1);
}

const gitDir = process.env["GRAM_PARK_GIT_DIR"];
const pidFile = gitDir ? path.join(gitDir, "gram-stack-parked.pid") : null;
const wakeLog = gitDir ? path.join(gitDir, "gram-stack-wake.log") : null;

// Same key/cert vite serves with, so the browser sees one origin across the
// handover instead of an https:// URL that suddenly speaks plain HTTP.
function tls(): { key: Buffer; cert: Buffer } | null {
  const keyFile = process.env["GRAM_SSL_KEY_FILE"];
  const certFile = process.env["GRAM_SSL_CERT_FILE"];
  if (!keyFile || !certFile) return null;
  try {
    return { key: fs.readFileSync(keyFile), cert: fs.readFileSync(certFile) };
  } catch {
    return null;
  }
}

// The Speakeasy mark from client/dashboard/src/assets/speakeasy-icon.svg,
// inlined because the parker serves one self-contained response with no dev
// server behind it to fetch assets from.
function mark(size: number): string {
  return `<svg width="${size}" height="${size}" viewBox="0 0 300 300" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
<rect x="4.5" y="4.5" width="291" height="291" rx="43.5" fill="white"/>
<rect x="4.5" y="4.5" width="291" height="291" rx="43.5" stroke="url(#speakeasy-mark-${size})" stroke-width="9"/>
<path d="M181.781 215.464L81.7665 201.253L74.0996 207.903L181.781 223.198L225.901 184.921L218.244 183.837L181.781 215.464Z" fill="#111111"/>
<path d="M181.781 192.261L225.901 153.984L218.244 152.899L181.781 184.517L146.095 179.45L97.0809 172.494L81.7665 170.315L74.0996 176.965L89.4237 179.134L74.0996 192.424L181.781 207.719L225.901 169.452L218.244 168.367L181.79 199.995L130.771 192.74L138.428 186.1L181.781 192.261Z" fill="#111111"/>
<path d="M181.781 169.045L81.7665 154.834L74.0996 161.484L181.781 176.779L225.901 138.512L218.244 137.428L181.781 169.045Z" fill="#111111"/>
<path d="M218.244 106.478L202.92 119.767L181.781 138.105L140.443 132.232L81.7665 123.894L74.0996 130.543L132.776 138.872L125.119 145.522L81.7569 139.362L74.0996 146.011L181.781 161.307L225.901 123.04L210.577 120.861L225.901 107.562L218.244 106.478Z" fill="#111111"/>
<path d="M225.901 92.0971L118.22 76.8018L74.0996 115.069L181.781 130.374L225.901 92.0971Z" fill="#111111"/>
<defs><linearGradient id="speakeasy-mark-${size}" x1="150" y1="0" x2="150" y2="300" gradientUnits="userSpaceOnUse">
<stop stop-color="#320F1E"/><stop offset="0.125625" stop-color="#C83228"/><stop offset="0.250625" stop-color="#FB873F"/>
<stop offset="0.375625" stop-color="#D2DC91"/><stop offset="0.500625" stop-color="#5A8250"/><stop offset="0.620625" stop-color="#002314"/>
<stop offset="0.740625" stop-color="#00143C"/><stop offset="0.860625" stop-color="#2873D7"/><stop offset="0.970625" stop-color="#9BC3FF"/>
</linearGradient></defs>
</svg>`;
}

const PAGE = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Resuming stack…</title>
<style>
  /* A still of the login screen (client/dashboard/src/pages/login) under a
     scrim, so the wake reads as "your dashboard, one moment" rather than as an
     error page on an unfamiliar host. Static and inert on purpose: nothing here
     can be fetched while the stack is down, so the panel is a facsimile, not
     the real thing -- and the whole still is aria-hidden. */
  :root {
    --surface: hsl(0, 0%, 98%);
    --card: #fff;
    --edge: hsl(0, 0%, 86%);
    --edge-soft: hsl(0, 0%, 92%);
    --muted: hsl(0, 0%, 46%);
    --muted-strong: hsl(0, 0%, 33%);
    --cta: hsl(0, 0%, 20%);
    --f-display: "Tobias", "Times New Roman", serif;
    --f-sans: "Diatype", "Inter", system-ui, -apple-system, sans-serif;
    --f-mono: "Diatype Mono", ui-monospace, SFMono-Regular, monospace;
  }
  * { box-sizing: border-box; }
  html, body { height: 100%; }
  body {
    margin: 0; background: var(--surface); color: #000;
    font-family: var(--f-sans); font-size: 15px; line-height: 1.5;
  }
  .mono { font-family: var(--f-mono); letter-spacing: .08em; text-transform: uppercase; }

  /* --- login still ------------------------------------------------------ */
  .shell { display: flex; flex-direction: column; min-height: 100vh; }
  .gradient {
    height: 4px; flex: none;
    background: linear-gradient(90deg, #320F1E, #C83228 12.5%, #FB873F 25%,
      #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF);
  }
  header {
    height: 64px; flex: none; display: flex; align-items: center;
    justify-content: space-between; padding: 0 40px;
    background: var(--card); border-bottom: 1px solid var(--edge);
  }
  header .left { display: flex; align-items: center; gap: 12px; font-size: 13px; }
  header .right { font-size: 13px; color: var(--muted); }
  .body { flex: 1; display: grid; grid-template-columns: 1fr; }
  @media (min-width: 1280px) { .body { grid-template-columns: 1fr 1fr; } }
  /* Stand-in for the agent-session showcase, which animates live data. */
  .showcase { display: none; padding: 64px 48px; }
  @media (min-width: 1280px) { .showcase { display: block; } }
  .skel { border: 1px solid var(--edge-soft); background: var(--card); padding: 20px; }
  .skel + .skel { margin-top: 16px; }
  .bar { height: 10px; background: var(--edge-soft); }
  .bar + .bar { margin-top: 10px; }
  .panel {
    display: flex; flex-direction: column; align-items: center;
    justify-content: center; gap: 24px; padding: 64px 32px;
    background: var(--card); border-left: 1px solid var(--edge-soft);
  }
  .lockup { display: flex; align-items: center; gap: 14px; }
  .wordmark { font-family: var(--f-display); font-size: 34px; letter-spacing: -.01em; }
  .pillars { display: flex; gap: 8px; }
  .pillars span { border: 1px solid var(--edge); padding: 5px 11px; font-size: 11px; }
  .cta {
    width: 280px; height: 40px; display: flex; align-items: center;
    justify-content: center; background: var(--cta); color: #fff;
    font-family: var(--f-mono); font-size: 15px; text-transform: uppercase;
  }
  .fine { font-size: 11px; color: var(--muted); font-family: var(--f-mono); }

  /* --- overlay ---------------------------------------------------------- */
  .overlay {
    position: fixed; inset: 0; display: grid; place-items: center;
    padding: 24px; text-align: center;
    /* Light enough that the login screen still reads through it -- the point
       is "your dashboard is coming back", not a page of its own. */
    background: hsl(0 0% 98% / .55);
    -webkit-backdrop-filter: blur(3px);
    backdrop-filter: blur(3px);
  }
  /* A panel, not free-floating text: the still behind it has its own copy in
     the same weight, and centred text over centred text is unreadable. Square
     corners to match the dashboard's design language. */
  .overlay main {
    background: hsl(0 0% 100% / .82);
    border: 1px solid var(--edge-soft);
    padding: 44px 56px 40px;
    max-width: 34rem;
  }
  .overlay h1 {
    font-family: var(--f-display); font-weight: 400; font-size: 34px;
    margin: 24px 0 10px; letter-spacing: -.01em;
  }
  .overlay p { margin: 0 auto; max-width: 30rem; color: var(--muted-strong); }
  .spinner { animation: spin 1.1s steps(12) infinite; color: #111; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .meta { margin-top: 20px; font-family: var(--f-mono); font-size: 12px; color: var(--muted); }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
</style>
</head>
<body>
<div class="shell" aria-hidden="true">
  <div class="gradient"></div>
  <header>
    <span class="left mono">${mark(22)} Speakeasy AI Control Plane</span>
    <span class="right mono">Login</span>
  </header>
  <div class="body">
    <div class="showcase">
      <div class="skel"><div class="bar" style="width:40%"></div><div class="bar" style="width:80%"></div><div class="bar" style="width:65%"></div></div>
      <div class="skel"><div class="bar" style="width:55%"></div><div class="bar" style="width:70%"></div></div>
      <div class="skel"><div class="bar" style="width:30%"></div><div class="bar" style="width:85%"></div><div class="bar" style="width:50%"></div></div>
    </div>
    <div class="panel">
      <div class="lockup">${mark(40)}<span class="wordmark">Speakeasy</span></div>
      <div>
        <p style="font-size:16px;margin:0">Securely scale AI usage across your organization.</p>
        <p style="font-size:14px;margin:6px 0 0;color:var(--muted-strong)">Control plane to govern Agents, MCP and Skills</p>
      </div>
      <div class="pillars mono"><span>Observe</span><span>Secure</span><span>Connect</span><span>Distribute</span></div>
      <div class="cta">Log in</div>
      <p class="fine">Single sign-on through your identity provider.</p>
    </div>
  </div>
</div>

<div class="overlay" role="status" aria-live="polite">
  <main>
    <!-- lucide "loader" -->
    <svg class="spinner" xmlns="http://www.w3.org/2000/svg" width="64" height="64"
         viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
         stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M12 2v4"/><path d="m16.2 7.8 2.9-2.9"/><path d="M18 12h4"/>
      <path d="m16.2 16.2 2.9 2.9"/><path d="M12 18v4"/><path d="m4.9 19.1 2.9-2.9"/>
      <path d="M2 12h4"/><path d="m4.9 4.9 2.9 2.9"/>
    </svg>
    <h1>Resuming stack</h1>
    <p>This worktree's containers and services are starting. The page reloads
       as soon as the dashboard answers.</p>
    <p class="meta"><span id="elapsed">0s</span> &middot; logs: pitchfork logs dashboard</p>
  </main>
</div>

<script>
  const started = Date.now();
  const elapsed = document.getElementById("elapsed");
  setInterval(() => {
    elapsed.textContent = Math.round((Date.now() - started) / 1000) + "s";
  }, 1000);
  // Two things answer this origin in turn: the parker (which marks its
  // responses) and then vite. Refused connections in between are the couple of
  // seconds after the parker lets go — expected, not an error. Cache-busted so
  // a probe can't be answered from the bfcache.
  async function poll() {
    try {
      const res = await fetch("/?__wake=" + Date.now(), { cache: "no-store" });
      if (!res.headers.get("x-gram-parked")) {
        location.replace("/");
        return;
      }
    } catch {}
    setTimeout(poll, 1000);
  }
  setTimeout(poll, 2000);
</script>
</body>
</html>
`;

let waking = false;

function wake(): void {
  if (waking) return;
  waking = true;

  setTimeout(() => {
    console.error(
      "wake did not take the port within 10 minutes; releasing it. " +
        "Check the wake log, then run `mise run wake` by hand.",
    );
    process.exit(1);
  }, WAKE_TIMEOUT_MS).unref();

  const out = wakeLog
    ? fs.openSync(wakeLog, "a")
    : ("ignore" as unknown as number);
  const child = spawn("mise", ["run", "wake"], {
    detached: true,
    stdio: ["ignore", out, out],
    cwd: process.cwd(),
  });
  child.unref();
}

const creds = tls();
const handler = (
  _req: http.IncomingMessage,
  res: http.ServerResponse,
): void => {
  res.writeHead(200, {
    "content-type": "text/html; charset=utf-8",
    "cache-control": "no-store",
    // How the page in the browser tells this parker apart from the dashboard
    // it is waiting for: the parker keeps answering on the same origin, so a
    // successful fetch alone would send the page reloading into itself.
    "x-gram-parked": "1",
  });
  res.end(PAGE, () => {
    wake();
  });
};

const server = creds
  ? https.createServer(creds, handler)
  : http.createServer(handler);

server.on("error", (err: NodeJS.ErrnoException) => {
  // EADDRINUSE means the stack (or another parker) already holds the port —
  // nothing to park. Exit quietly so `pause` does not report a failure.
  if (err.code === "EADDRINUSE") process.exit(0);
  console.error(err);
  process.exit(1);
});

server.listen(port, () => {
  if (pidFile) fs.writeFileSync(pidFile, String(process.pid));
  console.log(`Parked on port ${port}; first request wakes the stack.`);
});

// Backstop, generous enough to cover a cold `wake` (image pulls, a slow
// ClickHouse) but bounded, so a wake that died on its way to killing the parker
// cannot leave the port held. Only armed once a wake is actually running -- an
// untouched parker on a paused worktree is meant to sit there indefinitely.
const WAKE_TIMEOUT_MS = 10 * 60 * 1000;

for (const sig of ["SIGINT", "SIGTERM"] as const) {
  process.on(sig, () => {
    if (pidFile) fs.rmSync(pidFile, { force: true });
    process.exit(0);
  });
}
process.on("exit", () => {
  if (pidFile) fs.rmSync(pidFile, { force: true });
});
