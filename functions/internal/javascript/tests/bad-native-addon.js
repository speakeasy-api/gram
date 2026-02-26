// Simulates what happens when a native addon is required inside a bundle.
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
require("./binding.node");

export function handleToolCall() {
  return new Response("unreachable");
}
