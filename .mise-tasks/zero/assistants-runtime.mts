#!/usr/bin/env -S node

//MISE description="Configure local assistant runtime environment variables"

import { $, chalk } from "zx";
import { mkdirSync } from "node:fs";

const platform = process.platform;
const arch = process.arch === "arm64" ? "aarch64" : "x86_64";
const localEnv = (name: string, fallback: string) =>
  process.env[name] && process.env[name] !== "unset"
    ? process.env[name]
    : fallback;

const args: string[] = [
  `GRAM_ASSISTANT_RUNTIME_FIRECRACKER_BIN={{config_root}}/agents/runtime-artifacts/${arch}/firecracker`,
  `GRAM_ASSISTANT_RUNTIME_KERNEL_PATH={{config_root}}/agents/runtime-artifacts/${arch}/vmlinux.bin`,
  `GRAM_ASSISTANT_RUNTIME_ROOTFS_PATH={{config_root}}/agents/runtime-artifacts/${arch}/assistant-rootfs.ext4`,
  `GRAM_ASSISTANT_RUNTIME_WORKDIR={{config_root}}/local/assistant-runtimes`,
  `GRAM_ASSISTANT_RUNTIME_GUEST_PORT=${localEnv("GRAM_ASSISTANT_RUNTIME_GUEST_PORT", "8081")}`,
  `GRAM_RUNNER_ADDR=${localEnv("GRAM_RUNNER_ADDR", `0.0.0.0:${localEnv("GRAM_ASSISTANT_RUNTIME_GUEST_PORT", "8081")}`)}`,
];

if (platform === "darwin") {
  args.push(`GRAM_ASSISTANT_RUNTIME_HOST_KIND=lima`);
  args.push(
    `GRAM_ASSISTANT_RUNTIME_LIMA_INSTANCE=${localEnv("GRAM_ASSISTANT_RUNTIME_LIMA_INSTANCE", "gram-firecracker")}`,
  );
  args.push(`GRAM_ASSISTANT_RUNTIME_SERVER_HOSTNAME=host.lima.internal`);
} else {
  args.push(`GRAM_ASSISTANT_RUNTIME_HOST_KIND=linux`);
  args.push(`GRAM_ASSISTANT_RUNTIME_SERVER_HOSTNAME=gram.local`);
}

mkdirSync("./local/assistant-runtimes", { recursive: true });
await $`touch mise.local.toml`;
await $`mise set --file mise.local.toml ${args}`;

console.log(
  chalk.greenBright(
    `Configured assistant runtime env for ${platform === "darwin" ? "macOS + Lima" : "local Linux"}.`,
  ),
);

if (platform === "darwin") {
  console.log(
    chalk.yellow(
      `Firecracker runtime commands will run through the Lima instance named \`${localEnv("GRAM_ASSISTANT_RUNTIME_LIMA_INSTANCE", "gram-firecracker")}\`.`,
    ),
  );
}
console.log(
  chalk.yellow(
    "Assistant runtimes now use assistant-specific callback hostnames and preserve the normal dev TLS/server URL setup.",
  ),
);
