#!/usr/bin/env -S node

//MISE description="Create and provision the Lima VM used for local assistant Firecracker runtimes"

import { $, chalk } from "zx";

if (process.platform !== "darwin") {
  console.log("Skipping Lima assistant runtime setup on non-macOS host.");
  process.exit(0);
}

const provider = process.env["GRAM_ASSISTANT_RUNTIME_PROVIDER"] || "local";
if (provider !== "local") {
  console.log(
    `Skipping Lima setup: GRAM_ASSISTANT_RUNTIME_PROVIDER=${provider}.`,
  );
  process.exit(0);
}

const instance =
  process.env["GRAM_ASSISTANT_RUNTIME_LIMA_INSTANCE"] || "gram-firecracker";
const cpus = process.env["GRAM_ASSISTANT_RUNTIME_LIMA_CPUS"] || "4";
const memoryGiB = process.env["GRAM_ASSISTANT_RUNTIME_LIMA_MEMORY_GIB"] || "8";
const diskGiB = process.env["GRAM_ASSISTANT_RUNTIME_LIMA_DISK_GIB"] || "40";

function parseInstances(
  stdout: string,
): Array<{ name?: string; status?: string }> {
  const trimmed = stdout.trim();
  if (!trimmed) {
    return [];
  }
  const parsed = JSON.parse(trimmed) as
    | Array<{ name?: string; status?: string }>
    | { name?: string; status?: string };
  return Array.isArray(parsed) ? parsed : [parsed];
}

const listResult =
  await $`bash -lc 'limactl list --format json 2>/dev/null || true'`.quiet();
const existing = parseInstances(listResult.stdout).find(
  (item) => item.name === instance,
);

if (!existing) {
  console.log(chalk.greenBright(`Creating Lima instance ${instance}`));
  await $({
    stdio: "inherit",
  })`limactl start --name=${instance} --vm-type=vz --nested-virt --mount-type=virtiofs --mount-writable --network=vzNAT --cpus=${cpus} --memory=${memoryGiB} --disk=${diskGiB} template://ubuntu-24.04`;
} else if (existing.status !== "Running") {
  console.log(chalk.greenBright(`Starting Lima instance ${instance}`));
  await $({ stdio: "inherit" })`limactl start ${instance}`;
} else {
  console.log(
    chalk.greenBright(`Lima instance ${instance} is already running.`),
  );
}

console.log(chalk.greenBright(`Provisioning Lima instance ${instance}`));
await $({
  stdio: "inherit",
})`limactl shell --start ${instance} sudo bash -lc "export DEBIAN_FRONTEND=noninteractive; apt-get update; apt-get install -y ca-certificates curl iproute2 iptables jq"`;

const hostIPResult =
  await $`limactl shell --start ${instance} bash -lc "getent ahostsv4 host.lima.internal | awk 'NR==1{print \\$1}'"`.quiet();
const hostIP = hostIPResult.stdout.trim();
if (!hostIP) {
  throw new Error("Failed to resolve host.lima.internal inside the Lima guest");
}

await $`touch mise.local.toml`;
await $`mise set --file mise.local.toml GRAM_ASSISTANT_RUNTIME_SERVER_IP=${hostIP}`;

console.log(
  chalk.greenBright(
    `Updated GRAM_ASSISTANT_RUNTIME_SERVER_IP in mise.local.toml to ${hostIP}`,
  ),
);
