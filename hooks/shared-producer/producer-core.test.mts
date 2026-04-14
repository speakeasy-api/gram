import assert from "node:assert/strict";
import test from "node:test";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";

import {
  CAPTURE_LIMITS,
  buildCaptureUploadRequest,
  buildEnrichedHookPayload,
  buildSkillMetadata,
  computeCanonicalContentSha256,
  createDeterministicZipBuffer,
  discoverSkillRoot,
  extractSkillName,
  hasXGramIgnoreFrontmatter,
  isSkillToolName,
  listDiscoveryRoots,
  resolveAgent,
  resolveResolutionStatus,
  stripRegistryManagedFrontmatter,
} from "./producer-core.mts";

async function makeTempDir(prefix: string): Promise<string> {
  return mkdtemp(path.join(os.tmpdir(), prefix));
}

interface WriteSkillOptions {
  skillMd?: string;
  asDir?: boolean;
}

async function writeSkill(
  baseDir: string,
  relPath: string,
  { skillMd = "# skill\n", asDir = true }: WriteSkillOptions = {},
): Promise<string> {
  const fullDir = path.join(baseDir, relPath);
  await mkdir(fullDir, { recursive: true });
  if (asDir) {
    await writeFile(path.join(fullDir, "SKILL.md"), skillMd, "utf8");
  }
  return fullDir;
}

function requireValue<T>(value: T | null | undefined, message: string): T {
  assert.ok(value != null, message);
  return value as T;
}

test("isSkillToolName accepts Skill and mcp tool names", () => {
  assert.equal(isSkillToolName("Skill"), true);
  assert.equal(isSkillToolName("skill"), true);
  assert.equal(isSkillToolName("SKILL"), true);
  assert.equal(isSkillToolName("mcp__foo__Skill"), true);
  assert.equal(isSkillToolName("Write"), false);
});

test("extractSkillName reads skill from tool input object", () => {
  const payload = {
    tool_name: "Skill",
    tool_input: { skill: "golang" },
  };

  assert.equal(extractSkillName(payload), "golang");
});

test("extractSkillName reads nested tool input JSON string", () => {
  const payload = {
    tool_name: "mcp__local__Skill",
    tool_input: JSON.stringify({
      request: { args: { skill_name: "frontend" } },
    }),
  };

  assert.equal(extractSkillName(payload), "frontend");
});

test("resolveAgent reads argv and validates values", () => {
  const result = resolveAgent(["--agent=claude"], {});
  assert.equal(result.agent, "claude");
  assert.equal(result.error, null);
});

test("resolveAgent supports space-separated --agent value", () => {
  const result = resolveAgent(["--agent", "cursor"], {});
  assert.equal(result.agent, "cursor");
  assert.equal(result.error, null);
  assert.equal(result.source, "argv");
});

test("resolveAgent returns error for unsupported agent", () => {
  const result = resolveAgent(["--agent=other"], {});
  assert.equal(result.agent, null);
  assert.equal(result.source, "argv");
  assert.match(result.error ?? "", /unsupported agent/);
});

test("resolveAgent source stays argv when argv and env match", () => {
  const result = resolveAgent(["--agent=claude"], {
    GRAM_HOOK_AGENT: "claude",
  });
  assert.equal(result.agent, "claude");
  assert.equal(result.source, "argv");
});

test("resolveResolutionStatus honors allowed override", () => {
  assert.equal(
    resolveResolutionStatus({
      GRAM_SKILLS_RESOLUTION_STATUS: "capture_skipped_policy",
    }),
    "capture_skipped_policy",
  );
});

test("resolveResolutionStatus falls back to null for invalid override", () => {
  assert.equal(
    resolveResolutionStatus({
      GRAM_SKILLS_RESOLUTION_STATUS: "not_a_real_status",
    }),
    null,
  );
});

test("listDiscoveryRoots returns agent-specific deterministic roots", () => {
  const claudeRoots = listDiscoveryRoots("claude", {
    projectDir: "/repo",
    homeDir: "/home/dev",
  });
  assert.deepEqual(
    claudeRoots.map((r) => ({
      root: r.discoveryRoot,
      scope: r.scope,
      path: r.rootPath,
    })),
    [
      {
        root: "project_agents",
        scope: "project",
        path: "/repo/.agents/skills",
      },
      {
        root: "project_claude",
        scope: "project",
        path: "/repo/.claude/skills",
      },
      { root: "user_agents", scope: "user", path: "/home/dev/.agents/skills" },
      { root: "user_claude", scope: "user", path: "/home/dev/.claude/skills" },
    ],
  );

  const cursorRoots = listDiscoveryRoots("cursor", {
    projectDir: "/repo",
    homeDir: "/home/dev",
  });
  assert.deepEqual(
    cursorRoots.map((r) => ({
      root: r.discoveryRoot,
      scope: r.scope,
      path: r.rootPath,
    })),
    [
      {
        root: "project_agents",
        scope: "project",
        path: "/repo/.agents/skills",
      },
      {
        root: "project_cursor",
        scope: "project",
        path: "/repo/.cursor/skills",
      },
      { root: "user_agents", scope: "user", path: "/home/dev/.agents/skills" },
      { root: "user_cursor", scope: "user", path: "/home/dev/.cursor/skills" },
    ],
  );
});

test("discoverSkillRoot uses precedence order for claude", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  await writeSkill(projectDir, ".claude/skills/golang", {
    skillMd: "# vendor\n",
  });
  await writeSkill(projectDir, ".agents/skills/golang", {
    skillMd: "# preferred\n",
  });

  const discovered = await discoverSkillRoot("golang", {
    agent: "claude",
    projectDir,
    homeDir,
  });

  assert.equal(discovered.resolutionStatus, "resolved");
  assert.equal(discovered.scope, "project");
  assert.equal(discovered.discoveryRoot, "project_agents");
});

test("discoverSkillRoot returns invalid_skill_root when directory exists without SKILL.md", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  await writeSkill(projectDir, ".agents/skills/golang", { asDir: false });

  const discovered = await discoverSkillRoot("golang", {
    agent: "claude",
    projectDir,
    homeDir,
  });

  assert.equal(discovered.resolutionStatus, "invalid_skill_root");
  assert.equal(discovered.scope, null);
});

test("discoverSkillRoot continues to later roots after invalid earlier candidate", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  await writeSkill(projectDir, ".agents/skills/golang", { asDir: false });
  await writeSkill(projectDir, ".claude/skills/golang", {
    skillMd: "# valid later\n",
  });

  const discovered = await discoverSkillRoot("golang", {
    agent: "claude",
    projectDir,
    homeDir,
  });

  assert.equal(discovered.resolutionStatus, "resolved");
  assert.equal(discovered.discoveryRoot, "project_claude");
});

test("hasXGramIgnoreFrontmatter handles dotted and nested metadata forms", () => {
  const dotted = `---\nmetadata.x-gram-ignore: "true"\n---\n# skill\n`;
  const nested = `---\nmetadata:\n  x-gram-ignore: true\n---\n# skill\n`;
  const absent = `---\nmetadata:\n  x-gram-ignore: false\n---\n# skill\n`;

  assert.equal(hasXGramIgnoreFrontmatter(dotted), true);
  assert.equal(hasXGramIgnoreFrontmatter(nested), true);
  assert.equal(hasXGramIgnoreFrontmatter(absent), false);
});

test("stripRegistryManagedFrontmatter removes metadata.skill_uuid and metadata.x-gram-*", () => {
  const src = `---\nmetadata:\n  skill_uuid: abc\n  x-gram-ignore: true\n  x-gram-note: foo\n  author: keep\nmetadata.skill_uuid: def\nmetadata.x-gram-other: yes\n---\n# skill\n`;
  const cleaned = stripRegistryManagedFrontmatter(src);

  assert.equal(cleaned.includes("skill_uuid:"), false);
  assert.equal(cleaned.includes("x-gram-ignore:"), false);
  assert.equal(cleaned.includes("x-gram-note:"), false);
  assert.equal(cleaned.includes("x-gram-other:"), false);
  assert.equal(cleaned.includes("author: keep"), true);
});

test("stripRegistryManagedFrontmatter removes empty metadata block after stripping", () => {
  const src = `---\nmetadata:\n  skill_uuid: abc\n  x-gram-ignore: true\n---\n# skill\n`;
  const cleaned = stripRegistryManagedFrontmatter(src);
  assert.equal(cleaned.includes("metadata:"), false);
});

test("computeCanonicalContentSha256 is stable across CRLF/LF and key order", async () => {
  const dirA = await makeTempDir("gram-producer-hash-a-");
  const dirB = await makeTempDir("gram-producer-hash-b-");

  const skillA = await writeSkill(dirA, "skill", {
    skillMd: `---\nmetadata:\n  x-gram-note: remove\n  author: keep\n---\n# skill\n`,
  });
  await writeFile(path.join(skillA, "a.txt"), "hello\r\nworld\r\n", "utf8");
  await writeFile(path.join(skillA, "b.txt"), "content\n", "utf8");

  const skillB = await writeSkill(dirB, "skill", {
    skillMd: `---\nmetadata:\n  author: keep\n  x-gram-note: also-remove\n---\n# skill\n`,
  });
  await writeFile(path.join(skillB, "b.txt"), "content\n", "utf8");
  await writeFile(path.join(skillB, "a.txt"), "hello\nworld\n", "utf8");

  const hashA = await computeCanonicalContentSha256(skillA);
  const hashB = await computeCanonicalContentSha256(skillB);

  assert.equal(hashA.errorStatus, null);
  assert.equal(hashB.errorStatus, null);
  assert.equal(hashA.contentSha256, hashB.contentSha256);
});

test("computeCanonicalContentSha256 returns capture_skipped_file_limit when over file cap", async () => {
  const root = await makeTempDir("gram-producer-limit-files-");
  const skillDir = await writeSkill(root, "skill", { skillMd: "# skill\n" });

  await writeFile(path.join(skillDir, "a.txt"), "a", "utf8");
  await writeFile(path.join(skillDir, "b.txt"), "b", "utf8");

  const result = await computeCanonicalContentSha256(skillDir, {
    ...CAPTURE_LIMITS,
    maxFileCount: 1,
  });

  assert.equal(result.errorStatus, "capture_skipped_file_limit");
});

test("computeCanonicalContentSha256 enforces limits inside nested directories", async () => {
  const root = await makeTempDir("gram-producer-limit-nested-");
  const skillDir = await writeSkill(root, "skill", { skillMd: "# skill\n" });

  await mkdir(path.join(skillDir, "nested"), { recursive: true });
  await writeFile(path.join(skillDir, "nested", "a.txt"), "a", "utf8");
  await writeFile(path.join(skillDir, "nested", "b.txt"), "b", "utf8");

  const result = await computeCanonicalContentSha256(skillDir, {
    ...CAPTURE_LIMITS,
    maxFileCount: 1,
  });

  assert.equal(result.errorStatus, "capture_skipped_file_limit");
});

test("computeCanonicalContentSha256 returns capture_skipped_size_limit when file too large", async () => {
  const root = await makeTempDir("gram-producer-limit-size-");
  const skillDir = await writeSkill(root, "skill", { skillMd: "# skill\n" });

  await writeFile(path.join(skillDir, "big.txt"), "0123456789", "utf8");

  const result = await computeCanonicalContentSha256(skillDir, {
    ...CAPTURE_LIMITS,
    maxIndividualFileBytes: 5,
  });

  assert.equal(result.errorStatus, "capture_skipped_size_limit");
});

test("buildSkillMetadata includes hash + asset_format when resolved and hashable", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  const skillDir = await writeSkill(projectDir, ".agents/skills/golang", {
    skillMd: "# skill\n",
  });
  await writeFile(path.join(skillDir, "a.txt"), "hello\n", "utf8");

  const payload = { tool_name: "Skill", tool_input: { skill: "golang" } };
  const result = requireValue(
    await buildSkillMetadata(payload, {
      agent: "claude",
      projectDir,
      homeDir,
      gramKey: "k",
      gramProject: "p",
    }),
    "expected metadata for resolved skill",
  );

  const skill = result.metadata.skills[0];
  assert.equal(skill.resolution_status, "resolved");
  assert.equal(skill.asset_format, "zip");
  assert.match(
    requireValue(skill.content_sha256, "expected content hash"),
    /^[a-f0-9]{64}$/,
  );
  assert.equal(result.uploadRequest?.method, "POST");
});

test("buildSkillMetadata does not force resolved override when discovery is unresolved", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  const payload = { tool_name: "Skill", tool_input: { skill: "missing" } };
  const result = requireValue(
    await buildSkillMetadata(payload, {
      agent: "claude",
      projectDir,
      homeDir,
      resolutionStatus: "resolved",
    }),
    "expected metadata for unresolved skill",
  );

  assert.equal(
    result.metadata.skills[0].resolution_status,
    "unresolved_name_only",
  );
  assert.equal("content_sha256" in result.metadata.skills[0], false);
  assert.equal(result.uploadRequest, null);
});

test("buildSkillMetadata returns skipped_by_author when x-gram-ignore is true", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  await writeSkill(projectDir, ".agents/skills/golang", {
    skillMd: `---\nmetadata:\n  x-gram-ignore: true\n---\n# skill\n`,
  });

  const payload = { tool_name: "Skill", tool_input: { skill: "golang" } };
  const result = requireValue(
    await buildSkillMetadata(payload, {
      agent: "claude",
      projectDir,
      homeDir,
    }),
    "expected metadata for ignored skill",
  );

  assert.equal(
    result.metadata.skills[0].resolution_status,
    "skipped_by_author",
  );
  assert.equal("content_sha256" in result.metadata.skills[0], false);
  assert.equal(result.uploadRequest, null);
});

test("buildSkillMetadata marks missing credentials before upload", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  const skillDir = await writeSkill(projectDir, ".agents/skills/golang", {
    skillMd: "# skill\n",
  });
  await writeFile(path.join(skillDir, "a.txt"), "hello\n", "utf8");

  const payload = { tool_name: "Skill", tool_input: { skill: "golang" } };
  const result = requireValue(
    await buildSkillMetadata(payload, {
      agent: "claude",
      projectDir,
      homeDir,
      gramProject: "proj",
    }),
    "expected metadata for missing-credentials path",
  );

  assert.equal(
    result.metadata.skills[0].resolution_status,
    "capture_skipped_missing_credentials",
  );
  assert.equal("content_sha256" in result.metadata.skills[0], false);
  assert.equal(result.uploadRequest, null);
});

test("buildSkillMetadata maps hash limit failure to capture_skipped_* status", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  const skillDir = await writeSkill(projectDir, ".agents/skills/golang", {
    skillMd: "# skill\n",
  });
  await writeFile(path.join(skillDir, "a.txt"), "x", "utf8");
  await writeFile(path.join(skillDir, "b.txt"), "y", "utf8");

  const payload = { tool_name: "Skill", tool_input: { skill: "golang" } };
  const result = requireValue(
    await buildSkillMetadata(payload, {
      agent: "claude",
      projectDir,
      homeDir,
      limits: {
        ...CAPTURE_LIMITS,
        maxFileCount: 1,
      },
    }),
    "expected metadata for limit failure",
  );

  assert.equal(
    result.metadata.skills[0].resolution_status,
    "capture_skipped_file_limit",
  );
  assert.equal("content_sha256" in result.metadata.skills[0], false);
  assert.equal(result.uploadRequest, null);
});

test("buildEnrichedHookPayload preserves existing additional_data", async () => {
  const projectDir = await makeTempDir("gram-producer-project-");
  const homeDir = await makeTempDir("gram-producer-home-");

  const skillDir = await writeSkill(projectDir, ".agents/skills/golang", {
    skillMd: "# skill\n",
  });
  await writeFile(path.join(skillDir, "a.txt"), "hello\n", "utf8");

  const payload = {
    hook_event_name: "PostToolUse",
    tool_name: "Skill",
    tool_input: { skill: "golang" },
    additional_data: { trace_hint: "abc" },
  };

  const result = await buildEnrichedHookPayload(payload, {
    agent: "claude",
    projectDir,
    homeDir,
    gramKey: "k",
    gramProject: "p",
  });

  assert.ok(typeof result.payload === "object" && result.payload !== null);
  const payloadRecord = result.payload as {
    additional_data?: {
      trace_hint?: string;
      skills?: Array<{ name?: string }>;
    };
  };

  assert.equal(payloadRecord.additional_data?.trace_hint, "abc");
  assert.equal(payloadRecord.additional_data?.skills?.[0]?.name, "golang");
});

test("buildEnrichedHookPayload passthrough on non-record payload", async () => {
  const result = await buildEnrichedHookPayload("not-an-object");
  assert.equal(result.payload, "not-an-object");
  assert.equal(result.uploadRequest, null);
});

test("createDeterministicZipBuffer creates non-empty zip bytes", async () => {
  const root = await makeTempDir("gram-producer-zip-");
  const skillDir = await writeSkill(root, "skill", { skillMd: "# skill\n" });
  await writeFile(path.join(skillDir, "a.txt"), "hello\n", "utf8");

  const zipResult = await createDeterministicZipBuffer(skillDir);
  assert.equal(zipResult.errorStatus, null);

  const zipBuffer = requireValue(zipResult.zipBuffer, "expected zip buffer");
  assert.ok(zipBuffer.length > 0);
  assert.equal(zipBuffer.readUInt32LE(0), 0x04034b50);
});

test("buildCaptureUploadRequest shapes skills.capture request", () => {
  const archive = Buffer.from("zip");
  const req = requireValue(
    buildCaptureUploadRequest(
      {
        name: "golang",
        scope: "project",
        discovery_root: "project_agents",
        source_type: "local_filesystem",
        content_sha256: "a".repeat(64),
        asset_format: "zip",
        resolution_status: "resolved",
      },
      archive,
      {
        serverURL: "https://app.getgram.ai",
        gramKey: "key",
        gramProject: "proj",
      },
    ),
    "expected shaped upload request",
  );

  assert.equal(req.method, "POST");
  assert.equal(req.url, "https://app.getgram.ai/rpc/skills.capture");
  assert.equal(req.headers["Content-Type"], "application/zip");
  assert.equal(req.headers["X-Gram-Skill-Name"], "golang");
  assert.equal(req.headers["Gram-Key"], "key");

  const expectedBodySha = createHash("sha256").update(archive).digest("hex");
  assert.equal(req.headers["X-Gram-Skill-Content-Sha256"], expectedBodySha);
});

test("extractSkillName returns null for non-skill tool names", () => {
  const payload = { tool_name: "Write", tool_input: { skill: "golang" } };
  assert.equal(extractSkillName(payload), null);
});
