#!/usr/bin/env node
// Fill {{KEY}} placeholders in a prompt template with literal values.
// Usage: render.mjs <template> KEY=value [KEY=value ...] > out
// Unlike sed, no value can break out of its placeholder, and a placeholder
// left unfilled is an error rather than text the agent reads.

import fs from "node:fs";

const [template, ...pairs] = process.argv.slice(2);
const values = new Map();
for (const pair of pairs) {
  const i = pair.indexOf("=");
  if (i < 1) throw new Error(`not KEY=value: ${pair}`);
  values.set(pair.slice(0, i), pair.slice(i + 1));
}
const out = fs
  .readFileSync(template ?? "", "utf8")
  .replace(/\{\{([A-Z_]+)\}\}/g, (match, key) => {
    if (!values.has(key)) throw new Error(`no value for ${match}`);
    return values.get(key);
  });
process.stdout.write(out);
