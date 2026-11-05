import { startScenarioHost } from "./scenario-host.ts";

const [runDir, lifetime] = process.argv.slice(2);
if (!runDir)
  throw new Error("Usage: mock-host.ts <run-dir> [--exit-with-parent]");
const host = await startScenarioHost(runDir);
let stopping = false;
const stop = () => {
  if (stopping) return;
  stopping = true;
  void host.close().finally(() => process.exit(0));
};
process.on("SIGTERM", stop);
process.on("SIGINT", stop);
if (lifetime === "--exit-with-parent") {
  process.stdin.on("end", stop);
  process.stdin.resume();
}
