#!/usr/bin/env node

import os from "node:os";
import { readFile } from "node:fs/promises";
import { stdin, stderr, stdout } from "node:process";

import {
  buildEnrichedHookPayload,
  resolveAgent,
  resolveResolutionStatus,
} from "./producer-core.mjs";
import { markUploadSeen, shouldSuppressUpload } from "./cache.mjs";
import { spawnDetachedUploadWorker } from "./upload.mjs";

async function readStdin() {
  if (stdin.isTTY) {
    return "";
  }

  const chunks = [];
  for await (const chunk of stdin) {
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString("utf8");
}

function parseJSONOrNull(input) {
  const trimmed = input.trim();
  if (!trimmed) {
    return null;
  }

  try {
    return JSON.parse(trimmed);
  } catch {
    return null;
  }
}

function resolveRawPayloadSource(
  argv = process.argv.slice(2),
  env = process.env,
) {
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--payload-file") {
      return { kind: "file", value: argv[i + 1] ?? null };
    }
    if (arg.startsWith("--payload-file=")) {
      return { kind: "file", value: arg.slice("--payload-file=".length) };
    }
  }

  const fromEnv = env.GRAM_HOOK_PAYLOAD_FILE;
  if (fromEnv) {
    return { kind: "file", value: fromEnv };
  }

  return { kind: "stdin", value: null };
}

async function loadPayloadFromFile(filePath) {
  if (!filePath) {
    return null;
  }

  try {
    const content = await readFile(filePath, "utf8");
    return parseJSONOrNull(content);
  } catch {
    return null;
  }
}

function writeLine(stream, line) {
  stream.write(`${line}\n`);
}

function parseTTLFromEnv(env = process.env) {
  const raw = Number(env.GRAM_SKILLS_UPLOAD_CACHE_TTL_MS);
  if (!Number.isFinite(raw) || raw <= 0) {
    return undefined;
  }
  return raw;
}

async function main() {
  const agentResolution = resolveAgent();

  if (agentResolution.error) {
    writeLine(stderr, `[gram-skills-producer] ${agentResolution.error}`);
  }

  const payloadSource = resolveRawPayloadSource();
  const rawPayload =
    payloadSource.kind === "file"
      ? await loadPayloadFromFile(payloadSource.value)
      : parseJSONOrNull(await readStdin());

  if (!rawPayload) {
    writeLine(stdout, "{}");
    return;
  }

  const resolutionStatus = resolveResolutionStatus();

  const enrichedResult = await buildEnrichedHookPayload(rawPayload, {
    resolutionStatus: resolutionStatus ?? undefined,
    agent: agentResolution.agent,
    projectDir: process.cwd(),
    homeDir: os.homedir(),
    serverURL: process.env.GRAM_HOOKS_SERVER_URL,
    gramKey: process.env.GRAM_API_KEY,
    gramProject: process.env.GRAM_PROJECT_SLUG,
  });

  writeLine(stdout, JSON.stringify(enrichedResult.payload));

  const uploadEnabled = process.env.GRAM_SKILLS_UPLOAD_ENABLED === "true";
  if (uploadEnabled && enrichedResult.uploadRequest) {
    const skill = enrichedResult.payload?.additional_data?.skills?.[0];

    const suppress = await shouldSuppressUpload({
      homeDir: os.homedir(),
      ttlMs: parseTTLFromEnv(),
      project: process.env.GRAM_PROJECT_SLUG,
      skillName: skill?.name,
      canonicalContentSha256: skill?.content_sha256,
    }).catch(() => false);

    if (!suppress) {
      const spawned = await spawnDetachedUploadWorker(
        enrichedResult.uploadRequest,
      ).catch((error) => {
        writeLine(
          stderr,
          `[gram-skills-producer] upload worker spawn failed: ${error?.message ?? String(error)}`,
        );
        return { spawned: false };
      });

      if (spawned?.spawned) {
        await markUploadSeen({
          homeDir: os.homedir(),
          ttlMs: parseTTLFromEnv(),
          project: process.env.GRAM_PROJECT_SLUG,
          skillName: skill?.name,
          canonicalContentSha256: skill?.content_sha256,
        }).catch(() => {});
      }
    }
  }
}

await main().catch((err) => {
  writeLine(
    stderr,
    `[gram-skills-producer] unexpected error: ${err?.message ?? String(err)}`,
  );
  writeLine(stdout, "{}");
});
