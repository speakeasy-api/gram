// Simulates what happens when sync-rpc tries to resolve its worker file
// at import time inside a single-file bundle.
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
require.resolve("./xhr-sync-worker.js");

export function handleToolCall() {
  return new Response("unreachable");
}
