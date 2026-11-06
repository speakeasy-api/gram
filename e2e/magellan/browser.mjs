#!/usr/bin/env node
// The only browser command the Magellan agent may run. It passes a fixed set
// of playwright-cli commands through to the logged-in browser session and
// refuses the rest:
//
// - nothing that reads or replaces the login: cookies, storage, state files,
//   request/response headers;
// - nothing that runs code: `eval`, `run-code`, route mocks;
// - nothing that reads local files into the page: `upload`, `drop`;
// - no navigation off the site under test;
// - no options except the few listed below, and screenshots only into the
//   run's output directory.
//
// The config file sits next to the generated launcher, where the agent
// cannot write. Usage: browser.mjs <config.json> <command> [args ...]

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

// command -> options it may take (each as `--name` or `--name=value`)
export const ALLOWED = {
  goto: [],
  "tab-new": [],
  "tab-list": [],
  "tab-select": [],
  "tab-close": [],
  "go-back": [],
  "go-forward": [],
  reload: [],
  snapshot: [],
  find: [],
  click: [],
  dblclick: [],
  hover: [],
  drag: [],
  select: [],
  check: [],
  uncheck: [],
  fill: ["--submit"],
  type: ["--submit"],
  press: [],
  keydown: [],
  keyup: [],
  mousemove: [],
  mousedown: [],
  mouseup: [],
  mousewheel: [],
  resize: [],
  "dialog-accept": [],
  "dialog-dismiss": [],
  console: [],
  requests: [],
  "request-body": [],
  "response-body": [],
  "generate-locator": [],
  highlight: ["--hide"],
  screenshot: ["--filename", "--full-page"],
};

// Returns the playwright-cli arguments to run, or throws with the reason.
export function plan(config, argv) {
  const [command, ...rest] = argv;
  if (!command || !Object.hasOwn(ALLOWED, command)) {
    throw new Error(
      `"${command ?? ""}" is not allowed. Allowed: ${Object.keys(ALLOWED).join(", ")}`,
    );
  }
  const site = new URL(config.siteURL);
  const args = [];
  for (const arg of rest) {
    if (arg.startsWith("-")) {
      const [name, ...value] = arg.split("=");
      if (!ALLOWED[command].includes(name)) {
        throw new Error(`option ${name} is not allowed for ${command}`);
      }
      if (name === "--filename") {
        const base = value.join("=");
        if (!/^[A-Za-z0-9._-]{1,80}\.png$/.test(base) || base.startsWith(".")) {
          throw new Error("--filename must be a plain name ending in .png");
        }
        args.push(`--filename=${path.join(config.outDir, base)}`);
        continue;
      }
      if (value.length) throw new Error(`option ${name} takes no value`);
      args.push(name);
      continue;
    }
    args.push(arg);
  }
  if (command === "goto" || (command === "tab-new" && args.length)) {
    let url;
    try {
      url = new URL(args[0] ?? "", site);
    } catch {
      throw new Error("not a URL");
    }
    if (url.origin !== site.origin) {
      throw new Error(`only ${site.origin} may be opened`);
    }
    args[0] = url.href;
  }
  return [`-s=${config.session}`, command, ...args];
}

const isMain = import.meta.url === `file://${process.argv[1]}`;
if (isMain) {
  const [configPath, ...argv] = process.argv.slice(2);
  const config = JSON.parse(fs.readFileSync(configPath ?? "", "utf8"));
  let args;
  try {
    args = plan(config, argv);
  } catch (err) {
    console.error(`magellan-browser: ${err.message}`);
    process.exit(2);
  }
  const [bin, ...prefix] = config.backend;
  const result = spawnSync(bin, [...prefix, ...args], {
    cwd: config.cwd,
    stdio: "inherit",
  });
  process.exit(result.status ?? 1);
}
