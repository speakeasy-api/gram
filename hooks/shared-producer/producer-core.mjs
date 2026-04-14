import { readFile } from "node:fs/promises";

import {
  CAPTURE_LIMITS,
  DEFAULT_RESOLUTION_STATUS,
  RESOLUTION_STATUSES,
  SUPPORTED_AGENTS,
} from "./constants.mjs";
import {
  discoverSkillRoot,
  extractSkillName,
  isSkillToolName,
  listDiscoveryRoots,
} from "./discovery.mjs";
import {
  hasXGramIgnoreFrontmatter,
  stripRegistryManagedFrontmatter,
} from "./frontmatter.mjs";
import {
  buildCaptureUploadRequest,
  computeCanonicalContentSha256,
  createDeterministicZipBuffer,
} from "./packaging.mjs";

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
  computeCanonicalContentSha256,
  createDeterministicZipBuffer,
  buildCaptureUploadRequest,
};

function normalizeString(value) {
  if (typeof value !== "string") {
    return null;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

export function resolveAgent(argv = process.argv.slice(2), env = process.env) {
  let rawValue = null;
  let source = null;

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

  const normalized = normalizeString(rawValue)?.toLowerCase() ?? null;

  if (!normalized) {
    return {
      agent: null,
      source,
      error: "missing agent (use --agent=claude|cursor or GRAM_HOOK_AGENT)",
    };
  }

  if (!SUPPORTED_AGENTS.includes(normalized)) {
    return {
      agent: null,
      source: "argv_or_env",
      error: `unsupported agent '${normalized}' (expected one of: ${SUPPORTED_AGENTS.join(", ")})`,
    };
  }

  return {
    agent: normalized,
    source,
    error: null,
  };
}

export function resolveResolutionStatus(env = process.env) {
  const status = normalizeString(env.GRAM_SKILLS_RESOLUTION_STATUS);
  if (!status) {
    return null;
  }

  return RESOLUTION_STATUSES.includes(status) ? status : null;
}

export async function buildSkillMetadata(payload, options = {}) {
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
  let resolutionStatus = options.resolutionStatus ?? discovery.resolutionStatus;
  let contentSha256 = null;
  let archiveBuffer = null;

  if (
    !options.resolutionStatus &&
    discovery.resolutionStatus === "resolved" &&
    discovery.skillMdPath
  ) {
    try {
      const skillMdContent = await readFile(discovery.skillMdPath, "utf8");
      if (hasXGramIgnoreFrontmatter(skillMdContent)) {
        resolutionStatus = "skipped_by_author";
      }
    } catch {
      resolutionStatus = "invalid_skill_root";
    }

    if (resolutionStatus === "resolved" && discovery.skillDir) {
      const hashResult = await computeCanonicalContentSha256(
        discovery.skillDir,
        limits,
      );
      if (hashResult.errorStatus) {
        resolutionStatus = hashResult.errorStatus;
      } else {
        contentSha256 = hashResult.contentSha256;

        const zipResult = await createDeterministicZipBuffer(
          discovery.skillDir,
          limits,
        );
        if (zipResult.errorStatus) {
          resolutionStatus = zipResult.errorStatus;
          contentSha256 = null;
        } else {
          archiveBuffer = zipResult.zipBuffer;
        }
      }
    }
  }

  const skill = {
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

  if (contentSha256) {
    skill.content_sha256 = contentSha256;
    skill.asset_format = "zip";
  }

  const metadata = {
    skills: [skill],
  };

  const uploadRequest =
    resolutionStatus === "resolved" && archiveBuffer
      ? buildCaptureUploadRequest(skill, archiveBuffer, {
          serverURL: options.serverURL,
          gramKey: options.gramKey,
          gramProject: options.gramProject,
        })
      : null;

  return {
    metadata,
    uploadRequest,
  };
}

function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export async function buildEnrichedHookPayload(payload, options = {}) {
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
