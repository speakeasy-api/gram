import path from "node:path";
import { stat } from "node:fs/promises";

import {
  DEFAULT_RESOLUTION_STATUS,
  DISCOVERY_ROOTS_BY_AGENT,
  SUPPORTED_AGENTS,
  type DiscoveryRootName,
  type ResolutionStatus,
  type SkillScope,
  type SupportedAgent,
} from "./constants.mts";
import { asNonEmptyString, isJsonObject, type JsonObject } from "./types.mts";

const SKILL_NAME_KEYS = ["skill", "skill_name"] as const;

export interface DiscoveryRootPath {
  discoveryRoot: DiscoveryRootName;
  scope: SkillScope;
  rootPath: string;
}

export interface DiscoverSkillRootOptions {
  agent?: SupportedAgent | null;
  projectDir?: string | null;
  homeDir?: string | null;
}

export interface DiscoverSkillRootResult {
  resolutionStatus: ResolutionStatus;
  scope: SkillScope | null;
  discoveryRoot: DiscoveryRootName | null;
  skillDir: string | null;
  skillMdPath: string | null;
}

function normalizeSkillLookupName(value: any): string | null {
  const name = asNonEmptyString(value);
  if (!name) {
    return null;
  }

  if (
    name.includes("/") ||
    name.includes("\\") ||
    name === "." ||
    name === ".."
  ) {
    return null;
  }

  return name;
}

function extractSkillNameFromValue(value: any, depth = 0): string | null {
  if (depth > 4 || value == null) {
    return null;
  }

  const direct = asNonEmptyString(value);
  if (direct) {
    if (depth === 0 && (direct.startsWith("{") || direct.startsWith("["))) {
      try {
        return extractSkillNameFromValue(JSON.parse(direct), depth + 1);
      } catch {
        return null;
      }
    }

    return depth === 0 ? direct : null;
  }

  if (Array.isArray(value)) {
    for (const item of value) {
      const nested = extractSkillNameFromValue(item, depth + 1);
      if (nested) {
        return nested;
      }
    }
    return null;
  }

  if (!isJsonObject(value)) {
    return null;
  }

  for (const key of SKILL_NAME_KEYS) {
    const maybeName = asNonEmptyString(value[key]);
    if (maybeName) {
      return maybeName;
    }
  }

  for (const nestedValue of Object.values(value)) {
    const nested = extractSkillNameFromValue(nestedValue, depth + 1);
    if (nested) {
      return nested;
    }
  }

  return null;
}

async function pathExistsWithType(
  targetPath: string,
  expectedType: "dir" | "file",
): Promise<boolean> {
  try {
    const fileInfo = await stat(targetPath);
    if (expectedType === "dir") {
      return fileInfo.isDirectory();
    }
    return fileInfo.isFile();
  } catch {
    return false;
  }
}

export function isSupportedAgent(value: any): value is SupportedAgent {
  if (typeof value !== "string") {
    return false;
  }

  return SUPPORTED_AGENTS.some((agent) => agent === value);
}

export function isSkillToolName(toolName: any): boolean {
  const normalized = asNonEmptyString(toolName)?.toLowerCase();
  if (!normalized) {
    return false;
  }

  return normalized === "skill" || normalized.endsWith("__skill");
}

export function extractSkillName(payload: any): string | null {
  if (!isJsonObject(payload)) {
    return null;
  }

  if (!isSkillToolName(payload.tool_name)) {
    return null;
  }

  return extractSkillNameFromValue(payload.tool_input);
}

export function listDiscoveryRoots(
  agent: SupportedAgent | null | undefined,
  options: DiscoverSkillRootOptions = {},
): DiscoveryRootPath[] {
  if (!agent || !DISCOVERY_ROOTS_BY_AGENT[agent]) {
    return [];
  }

  const rawProjectDir = asNonEmptyString(options.projectDir);
  const rawHomeDir = asNonEmptyString(options.homeDir);

  const projectDir = rawProjectDir ? path.resolve(rawProjectDir) : null;
  const homeDir = rawHomeDir ? path.resolve(rawHomeDir) : null;

  return DISCOVERY_ROOTS_BY_AGENT[agent]
    .map<DiscoveryRootPath | null>((entry) => {
      const baseDir = entry.scope === "project" ? projectDir : homeDir;
      if (!baseDir) {
        return null;
      }

      return {
        discoveryRoot: entry.discoveryRoot,
        scope: entry.scope,
        rootPath: path.join(baseDir, ...entry.segments),
      };
    })
    .filter((entry): entry is DiscoveryRootPath => entry !== null);
}

export async function discoverSkillRoot(
  skillName: any,
  options: DiscoverSkillRootOptions = {},
): Promise<DiscoverSkillRootResult> {
  const lookupName = normalizeSkillLookupName(skillName);
  if (!lookupName) {
    return {
      resolutionStatus: DEFAULT_RESOLUTION_STATUS,
      scope: null,
      discoveryRoot: null,
      skillDir: null,
      skillMdPath: null,
    };
  }

  const roots = listDiscoveryRoots(options.agent ?? null, {
    projectDir: options.projectDir,
    homeDir: options.homeDir,
  });

  let foundInvalidCandidate = false;

  for (const root of roots) {
    const skillDir = path.join(root.rootPath, lookupName);
    if (!(await pathExistsWithType(skillDir, "dir"))) {
      continue;
    }

    const skillMdPath = path.join(skillDir, "SKILL.md");
    if (await pathExistsWithType(skillMdPath, "file")) {
      return {
        resolutionStatus: "resolved",
        scope: root.scope,
        discoveryRoot: root.discoveryRoot,
        skillDir,
        skillMdPath,
      };
    }

    foundInvalidCandidate = true;
  }

  return {
    resolutionStatus: foundInvalidCandidate
      ? "invalid_skill_root"
      : DEFAULT_RESOLUTION_STATUS,
    scope: null,
    discoveryRoot: null,
    skillDir: null,
    skillMdPath: null,
  };
}
