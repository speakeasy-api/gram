import { readFile } from "node:fs/promises";

import {
  CAPTURE_LIMITS,
  DEFAULT_RESOLUTION_STATUS,
  RESOLUTION_STATUSES,
  SUPPORTED_AGENTS,
  type CaptureLimits,
  type DiscoveryRootName,
  type ResolutionStatus,
  type SkillScope,
  type SupportedAgent,
} from "./constants.mts";
import {
  discoverSkillRoot,
  extractSkillName,
  isSkillToolName,
  isSupportedAgent,
  listDiscoveryRoots,
  type DiscoverSkillRootResult,
} from "./discovery.mts";
import {
  hasXGramIgnoreFrontmatter,
  stripRegistryManagedFrontmatter,
} from "./frontmatter.mts";
import {
  buildCanonicalArchiveSnapshot,
  buildCaptureUploadRequest,
  computeCanonicalContentSha256,
  createDeterministicZipBuffer,
  type CaptureUploadRequest,
  type SkillUploadMetadata,
} from "./packaging.mts";
import { asNonEmptyString, isJsonObject, type JsonObject } from "./types.mts";

export {
  RESOLUTION_STATUSES,
  DEFAULT_RESOLUTION_STATUS,
  SUPPORTED_AGENTS,
  CAPTURE_LIMITS,
  isSkillToolName,
  extractSkillName,
  listDiscoveryRoots,
  discoverSkillRoot,
  hasXGramIgnoreFrontmatter,
  stripRegistryManagedFrontmatter,
  buildCanonicalArchiveSnapshot,
  computeCanonicalContentSha256,
  createDeterministicZipBuffer,
  buildCaptureUploadRequest,
};

export interface ResolveAgentResult {
  agent: SupportedAgent | null;
  source: "argv" | "env" | null;
  error: string | null;
}

export interface BuildSkillMetadataOptions {
  resolutionStatus?: ResolutionStatus | null;
  agent?: SupportedAgent | null;
  projectDir?: string | null;
  homeDir?: string | null;
  serverURL?: string | null;
  gramKey?: string | null;
  gramProject?: string | null;
  limits?: CaptureLimits;
}

export interface SkillMetadataEnvelope {
  skills: [SkillUploadMetadata];
}

export interface BuildSkillMetadataResult {
  metadata: SkillMetadataEnvelope;
  uploadRequest: CaptureUploadRequest | null;
}

function isRecord(value: any): value is JsonObject {
  return isJsonObject(value);
}

export function resolveAgent(
  argv: readonly string[] = process.argv.slice(2),
  env: NodeJS.ProcessEnv = process.env,
): ResolveAgentResult {
  let rawValue: string | null = null;
  let source: ResolveAgentResult["source"] = null;

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--agent") {
      rawValue = argv[i + 1] ?? null;
      source = "argv";
      break;
    }
    if (arg.startsWith("--agent=")) {
      rawValue = arg.slice("--agent=".length);
      source = "argv";
      break;
    }
  }

  if (!rawValue) {
    rawValue = env.GRAM_HOOK_AGENT ?? null;
    if (rawValue) {
      source = "env";
    }
  }

  const normalized = asNonEmptyString(rawValue)?.toLowerCase() ?? null;

  if (!normalized) {
    return {
      agent: null,
      source,
      error: "missing agent (use --agent=claude|cursor or GRAM_HOOK_AGENT)",
    };
  }

  if (!isSupportedAgent(normalized)) {
    return {
      agent: null,
      source,
      error: `unsupported agent '${normalized}' (expected one of: ${SUPPORTED_AGENTS.join(", ")})`,
    };
  }

  return {
    agent: normalized,
    source,
    error: null,
  };
}

function isResolutionStatus(value: any): value is ResolutionStatus {
  if (typeof value !== "string") {
    return false;
  }

  return RESOLUTION_STATUSES.some((status) => status === value);
}

function asResolutionStatus(value: any): ResolutionStatus | null {
  const normalized = asNonEmptyString(value);
  if (!normalized) {
    return null;
  }

  return isResolutionStatus(normalized) ? normalized : null;
}

export function resolveResolutionStatus(
  env: NodeJS.ProcessEnv = process.env,
): ResolutionStatus | null {
  return asResolutionStatus(env.GRAM_SKILLS_RESOLUTION_STATUS);
}

function buildBaseSkill(
  skillName: string,
  resolutionStatus: ResolutionStatus,
  discovery: DiscoverSkillRootResult,
): SkillUploadMetadata {
  const skill: SkillUploadMetadata = {
    name: skillName,
    source_type: "local_filesystem",
    resolution_status: resolutionStatus,
  };

  if (discovery.scope) {
    skill.scope = discovery.scope;
  }

  if (discovery.discoveryRoot) {
    skill.discovery_root = discovery.discoveryRoot;
  }

  return skill;
}

export async function buildSkillMetadata(
  payload: any,
  options: BuildSkillMetadataOptions = {},
): Promise<BuildSkillMetadataResult | null> {
  const skillName = extractSkillName(payload);
  if (!skillName) {
    return null;
  }

  const discovery = await discoverSkillRoot(skillName, {
    agent: options.agent,
    projectDir: options.projectDir,
    homeDir: options.homeDir,
  });

  const limits = options.limits ?? CAPTURE_LIMITS;
  const forcedResolutionStatus = asResolutionStatus(options.resolutionStatus);
  let resolutionStatus: ResolutionStatus =
    forcedResolutionStatus ?? discovery.resolutionStatus;
  let contentSha256: string | null = null;
  let archiveBuffer: Buffer | null = null;

  if (discovery.resolutionStatus === "resolved" && discovery.skillMdPath) {
    try {
      const skillMdContent = await readFile(discovery.skillMdPath, "utf8");
      if (hasXGramIgnoreFrontmatter(skillMdContent)) {
        resolutionStatus = "skipped_by_author";
      }
    } catch {
      resolutionStatus = "invalid_skill_root";
    }

    if (resolutionStatus === "resolved" && discovery.skillDir) {
      const snapshot = await buildCanonicalArchiveSnapshot(
        discovery.skillDir,
        limits,
      );
      if (snapshot.errorStatus) {
        resolutionStatus = snapshot.errorStatus;
      } else {
        contentSha256 = snapshot.contentSha256;
        archiveBuffer = snapshot.zipBuffer;
      }
    }
  }

  if (
    discovery.resolutionStatus !== "resolved" &&
    forcedResolutionStatus === "resolved"
  ) {
    resolutionStatus = discovery.resolutionStatus;
  }

  const claudeSessionID =
    options.agent === "claude" && isRecord(payload)
      ? asNonEmptyString(payload.session_id)
      : null;

  if (
    resolutionStatus === "resolved" &&
    archiveBuffer &&
    !asNonEmptyString(options.gramKey) &&
    !claudeSessionID
  ) {
    resolutionStatus = "capture_skipped_missing_credentials";
    contentSha256 = null;
    archiveBuffer = null;
  }

  const skill = buildBaseSkill(skillName, resolutionStatus, discovery);

  if (contentSha256) {
    skill.content_sha256 = contentSha256;
    skill.asset_format = "zip";
  }

  const metadata: SkillMetadataEnvelope = {
    skills: [skill],
  };

  const uploadRequest =
    resolutionStatus === "resolved" && archiveBuffer
      ? buildCaptureUploadRequest(skill, archiveBuffer, {
          serverURL: options.serverURL,
          gramKey: options.gramKey,
          gramProject: options.gramProject,
          claudeSessionID,
          endpointPath: claudeSessionID
            ? "/rpc/skills.captureClaude"
            : "/rpc/skills.capture",
        })
      : null;

  return {
    metadata,
    uploadRequest,
  };
}

export async function buildEnrichedHookPayload<TPayload = any>(
  payload: TPayload,
  options: BuildSkillMetadataOptions = {},
): Promise<{ payload: TPayload; uploadRequest: CaptureUploadRequest | null }> {
  if (!isRecord(payload)) {
    return {
      payload,
      uploadRequest: null,
    };
  }

  const skillResult = await buildSkillMetadata(payload, options);
  if (!skillResult?.metadata) {
    return {
      payload,
      uploadRequest: null,
    };
  }

  const existingAdditionalData = isRecord(payload.additional_data)
    ? payload.additional_data
    : {};

  return {
    payload: {
      ...payload,
      additional_data: {
        ...existingAdditionalData,
        ...skillResult.metadata,
      },
    },
    uploadRequest: skillResult.uploadRequest,
  };
}
