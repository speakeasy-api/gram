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
// itself), so it hands the port over instead: serve the page, close the
// listener, spawn `mise run wake`. That leaves a window where nothing is
// listening, which is why the interstitial polls from the browser — the page is
// already loaded, so a refused fetch is just another retry.
//
// Started detached by `mise run pause`; killed by `mise run wake` (and it exits
// on its own once it has triggered a wake).

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

const PAGE = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Resuming stack…</title>
<style>
  :root { color-scheme: light dark; }
  body {
    margin: 0; min-height: 100vh; display: grid; place-items: center;
    font: 15px/1.5 ui-sans-serif, system-ui, -apple-system, sans-serif;
    background: Canvas; color: CanvasText;
  }
  main { text-align: center; max-width: 30rem; padding: 2rem; }
  h1 { font-size: 1.25rem; font-weight: 600; margin: 0 0 .5rem; }
  p { margin: 0; opacity: .7; }
  code { font-family: ui-monospace, SFMono-Regular, monospace; }
  .dot {
    display: inline-block; width: .5rem; height: .5rem; margin-right: .5rem;
    border-radius: 50%; background: currentColor; animation: pulse 1.2s infinite;
  }
  @keyframes pulse { 0%, 100% { opacity: .25 } 50% { opacity: 1 } }
</style>
</head>
<body>
<main>
  <h1><span class="dot"></span>Resuming stack</h1>
  <p>This worktree's containers and services are starting. The page reloads
     when the dashboard answers — usually well under a minute.</p>
  <p id="elapsed"></p>
  <p><small>Logs: <code>mise run git:workstatus</code>, or
     <code>pitchfork logs dashboard</code>.</small></p>
</main>
<script>
  const started = Date.now();
  const elapsed = document.getElementById("elapsed");
  setInterval(() => {
    elapsed.textContent = Math.round((Date.now() - started) / 1000) + "s";
  }, 1000);
  // The parker closes its listener as soon as it has answered this request, so
  // the next few probes are refused connections — expected, not an error.
  // Cache-busted so a probe can't be answered from the bfcache.
  async function poll() {
    try {
      await fetch("/?__wake=" + Date.now(), { cache: "no-store" });
      location.replace("/");
      return;
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
  });
  res.end(PAGE, () => {
    // Free the port before `wake` starts vite, and only once the page is
    // actually on the wire — closing earlier would strand this response.
    server.close();
    wake();
    // The browser polls from here; nothing else needs this process. Give the
    // socket a moment to drain, then leave the port to vite.
    setTimeout(() => process.exit(0), 500);
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

for (const sig of ["SIGINT", "SIGTERM"] as const) {
  process.on(sig, () => {
    if (pidFile) fs.rmSync(pidFile, { force: true });
    process.exit(0);
  });
}
process.on("exit", () => {
  if (pidFile) fs.rmSync(pidFile, { force: true });
});
