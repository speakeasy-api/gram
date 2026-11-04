import { createInterface } from "node:readline";
import { startMockAuthServer } from "./mock-server.ts";

const arg = process.argv[2];
if (arg === "--help") {
  console.log(
    "mise run mcp-auth:serve [port]\nDefault: random loopback port. Controls: reset, expire, revoke, events, quit.",
  );
  process.exit(0);
}
const port = arg === undefined ? 0 : Number(arg);
if (!Number.isInteger(port) || port < 0 || port > 65535) {
  throw new Error("Port must be an integer from 0 to 65535.");
}
const fixture = await startMockAuthServer({ host: "127.0.0.1", port });
console.log(
  JSON.stringify(
    {
      notice:
        "Synthetic local fixtures only. Configure Speakeasy through the dashboard; no records are seeded.",
      origin: fixture.origin,
      providers: fixture.providers,
      profiles: fixture.profiles,
      controls: "reset | expire | revoke | events | quit",
    },
    null,
    2,
  ),
);
const input = createInterface({ input: process.stdin, terminal: false });
let stopping = false;
async function stop() {
  if (stopping) return;
  stopping = true;
  input.close();
  await fixture.close();
}
input.on("line", (line) => {
  switch (line.trim()) {
    case "reset":
      fixture.reset();
      break;
    case "expire":
      fixture.expireTokens();
      break;
    case "revoke":
      fixture.revokeTokens();
      break;
    case "events":
      console.log(JSON.stringify(fixture.events, null, 2));
      break;
    case "quit":
      void stop();
      break;
    default:
      console.log("Use reset, expire, revoke, events or quit.");
  }
});
process.once("SIGINT", () => {
  void stop();
});
process.once("SIGTERM", () => {
  void stop();
});
