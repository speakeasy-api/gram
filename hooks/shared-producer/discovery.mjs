import path from "node:path";
import { stat } from "node:fs/promises";

import {
  DEFAULT_RESOLUTION_STATUS,
  DISCOVERY_ROOTS_BY_AGENT,
} from "./constants.mjs";

const SKILL_NAME_KEYS = Object.freeze(["skill", "skill_name"]);

function normalizeString(value) {
  if (typeof value !== "string") {
    return null;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function normalizeSkillLookupName(value) {
  const name = normalizeString(value);
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

function extractSkillNameFromValue(value, depth = 0) {
  if (depth > 4 || value == null) {
    return null;
  }

  const direct = normalizeString(value);
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

  if (!isRecord(value)) {
    return null;
  }

  for (const key of SKILL_NAME_KEYS) {
    const maybeName = normalizeString(value[key]);
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

async function pathExistsWithType(targetPath, expectedType) {
  try {
    const fileInfo = await stat(targetPath);
    if (expectedType === "dir") {
      return fileInfo.isDirectory();
    }
    if (expectedType === "file") {
      return fileInfo.isFile();
    }
    return false;
  } catch {
    return false;
  }
}

export function isSkillToolName(toolName) {
  const normalized = normalizeString(toolName)?.toLowerCase();
  if (!normalized) {
    return false;
  }

  return normalized === "skill" || normalized.endsWith("__skill");
}

export function extractSkillName(payload) {
  if (!isRecord(payload)) {
    return null;
  }

  if (!isSkillToolName(payload.tool_name)) {
    return null;
  }

  return extractSkillNameFromValue(payload.tool_input);
}

export function listDiscoveryRoots(agent, options = {}) {
  if (!agent || !DISCOVERY_ROOTS_BY_AGENT[agent]) {
    return [];
  }

  const projectDir = normalizeString(options.projectDir)
    ? path.resolve(options.projectDir)
    : null;
  const homeDir = normalizeString(options.homeDir)
    ? path.resolve(options.homeDir)
    : null;

  return DISCOVERY_ROOTS_BY_AGENT[agent]
    .map((entry) => {
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
    .filter((entry) => entry !== null);
}

export async function discoverSkillRoot(skillName, options = {}) {
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

  const roots = listDiscoveryRoots(options.agent, {
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
