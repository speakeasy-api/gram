import { useCallback, useEffect, useState } from "react";
import { DETECTION_RULES, type RuleCategory } from "./policy-data";

/** Severity levels assigned to a detection rule. Drives how findings show
 *  up in dashboards and (eventually) which actions a policy is allowed to
 *  take. Ordered low to high. */
export const SEVERITY_LEVELS = [
  "info",
  "low",
  "medium",
  "high",
  "critical",
] as const;

export type SeverityLevel = (typeof SEVERITY_LEVELS)[number];

export const SEVERITY_META: Record<
  SeverityLevel,
  { label: string; description: string; badgeClass: string }
> = {
  info: {
    label: "Info",
    description: "Informational signal, no action recommended",
    badgeClass: "bg-muted text-muted-foreground border-border",
  },
  low: {
    label: "Low",
    description: "Minor risk, review periodically",
    badgeClass:
      "bg-blue-500/10 text-blue-700 dark:text-blue-300 border-blue-500/30",
  },
  medium: {
    label: "Medium",
    description: "Notable risk, review when surfaced",
    badgeClass:
      "bg-yellow-500/10 text-yellow-800 dark:text-yellow-300 border-yellow-500/30",
  },
  high: {
    label: "High",
    description: "Serious risk, investigate promptly",
    badgeClass:
      "bg-orange-500/10 text-orange-800 dark:text-orange-300 border-orange-500/30",
  },
  critical: {
    label: "Critical",
    description: "Highest risk, immediate response required",
    badgeClass:
      "bg-red-500/10 text-red-700 dark:text-red-300 border-red-500/30",
  },
};

/** Default severity for builtin rules. Driven by category since the
 *  underlying detectors are uniform within a category. Individual rules
 *  can override via the Detection Rules page (stored locally for now). */
const CATEGORY_DEFAULT_SEVERITY: Record<RuleCategory, SeverityLevel> = {
  secrets: "high",
  financial: "medium",
  pii: "medium",
  government_ids: "medium",
  healthcare: "medium",
  prompt_injection: "medium",
  off_policy: "medium",
  shadow_mcp: "medium",
  destructive_tool: "medium",
  cli_destructive: "medium",
  custom: "medium",
};

/** Generic rule descriptions for categories where rule entries carry only
 *  a title. We don't have per-rule prose for the gitleaks/Presidio catalog
 *  so we surface a category-level explanation. */
const CATEGORY_RULE_DESCRIPTION: Record<RuleCategory, string> = {
  secrets:
    "Regex-backed detector tuned to the issuing service's token format. Flags credentials that match the canonical shape of the underlying provider.",
  financial:
    "Pattern + checksum detector for financial identifiers. Validates the structure (length, check digit, BIN range) before reporting a match.",
  pii: "Pattern detector for personal identifiable information embedded in free-form text. Anchors to the canonical format of the field.",
  government_ids:
    "Pattern + checksum detector for government-issued identifiers, validated against the issuer's format and check-digit rules.",
  healthcare:
    "Pattern detector for healthcare identifiers and clinical references in free-form text.",
  prompt_injection:
    "Hybrid detector that combines classifier scoring with regex and keyword heuristics to flag attempts to override the agent's instructions.",
  off_policy:
    "Classifier-backed detector for requests that fall outside the organization's acceptable-use policy.",
  shadow_mcp:
    "Detects MCP tool calls in Cursor and Claude Code that didn't originate from a Speakeasy-issued MCP server. Requires Speakeasy hooks on the agent.",
  destructive_tool:
    "Flags tool calls whose Gram tool definition is annotated as destructive. Requires Speakeasy hooks and Gram-issued tool metadata.",
  cli_destructive:
    "Pattern detector for destructive shell, git, database, and cloud CLI invocations passed through tool arguments.",
  custom:
    "Organization-defined regex pattern. Matches anywhere in the scanned payload.",
};

export type BuiltinRule = {
  id: string;
  title: string;
  description: string;
  category: RuleCategory;
  defaultSeverity: SeverityLevel;
};

/** Synthetic single-rule entries for categories where the category itself
 *  acts as the detector (no granular sub-rules to expose). */
const SYNTHETIC_CATEGORY_RULES: Partial<
  Record<RuleCategory, { id: string; title: string }>
> = {
  prompt_injection: {
    id: "prompt_injection.default",
    title: "Prompt Injection",
  },
  shadow_mcp: {
    id: "shadow_mcp.default",
    title: "Unverified MCP Tool Call",
  },
  destructive_tool: {
    id: "destructive_tool.default",
    title: "Destructive Tool Annotation",
  },
};

/** Flattened, category-keyed view of every builtin rule. Drives the
 *  Detection Rules listing and uniqueness checks for custom rule ids. */
export const BUILTIN_RULES_BY_CATEGORY: Record<RuleCategory, BuiltinRule[]> = (
  Object.keys(DETECTION_RULES) as RuleCategory[]
).reduce(
  (acc, category) => {
    const catalog = DETECTION_RULES[category];
    const description = CATEGORY_RULE_DESCRIPTION[category];
    const severity = CATEGORY_DEFAULT_SEVERITY[category];
    if (catalog.length > 0) {
      acc[category] = catalog.map((r) => ({
        id: r.id,
        title: r.title,
        description,
        category,
        defaultSeverity: severity,
      }));
      return acc;
    }
    const synthetic = SYNTHETIC_CATEGORY_RULES[category];
    if (synthetic) {
      acc[category] = [
        {
          id: synthetic.id,
          title: synthetic.title,
          description,
          category,
          defaultSeverity: severity,
        },
      ];
      return acc;
    }
    acc[category] = [];
    return acc;
  },
  {} as Record<RuleCategory, BuiltinRule[]>,
);

/** All builtin rule ids, used for custom rule id collision checks. */
export const BUILTIN_RULE_IDS = new Set<string>(
  Object.values(BUILTIN_RULES_BY_CATEGORY).flatMap((rules) =>
    rules.map((r) => r.id),
  ),
);

export type CustomDetectionRule = {
  id: string;
  title: string;
  description: string;
  regex: string;
  severity: SeverityLevel;
  createdAt: string;
  updatedAt: string;
};

const STORAGE_KEY = "gram.detection-rules.v1";

type StoredState = {
  customRules: CustomDetectionRule[];
};

const EMPTY_STATE: StoredState = {
  customRules: [],
};

function readState(): StoredState {
  if (typeof window === "undefined") return EMPTY_STATE;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return EMPTY_STATE;
    const parsed = JSON.parse(raw) as Partial<StoredState>;
    return {
      customRules: parsed.customRules ?? [],
    };
  } catch {
    return EMPTY_STATE;
  }
}

function writeState(state: StoredState) {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  window.dispatchEvent(new CustomEvent(STORAGE_EVENT));
}

const STORAGE_EVENT = "gram.detection-rules.changed";

/** Local-storage backed store for severity overrides + custom rules.
 *  Mocked client-side until the server endpoints land. */
export function useDetectionRulesStore() {
  const [state, setState] = useState<StoredState>(() => readState());

  useEffect(() => {
    if (typeof window === "undefined") return;
    const onChange = () => setState(readState());
    window.addEventListener(STORAGE_EVENT, onChange);
    window.addEventListener("storage", onChange);
    return () => {
      window.removeEventListener(STORAGE_EVENT, onChange);
      window.removeEventListener("storage", onChange);
    };
  }, []);

  const addCustomRule = useCallback(
    (rule: Omit<CustomDetectionRule, "createdAt" | "updatedAt">) => {
      const now = new Date().toISOString();
      setState((prev) => {
        const updated = {
          ...prev,
          customRules: [
            ...prev.customRules,
            { ...rule, createdAt: now, updatedAt: now },
          ],
        };
        writeState(updated);
        return updated;
      });
    },
    [],
  );

  const updateCustomRule = useCallback(
    (
      id: string,
      patch: Partial<Omit<CustomDetectionRule, "id" | "createdAt">>,
    ) => {
      const now = new Date().toISOString();
      setState((prev) => {
        const updated = {
          ...prev,
          customRules: prev.customRules.map((r) =>
            r.id === id ? { ...r, ...patch, updatedAt: now } : r,
          ),
        };
        writeState(updated);
        return updated;
      });
    },
    [],
  );

  const removeCustomRule = useCallback((id: string) => {
    setState((prev) => {
      const updated = {
        ...prev,
        customRules: prev.customRules.filter((r) => r.id !== id),
      };
      writeState(updated);
      return updated;
    });
  }, []);

  return {
    customRules: state.customRules,
    addCustomRule,
    updateCustomRule,
    removeCustomRule,
  };
}

/** Validate a proposed custom rule id. Returns an error message if the id
 *  collides with a builtin or an existing custom rule, or is malformed. */
export function validateCustomRuleId(
  id: string,
  existingCustomIds: string[],
): string | null {
  const trimmed = id.trim();
  if (!trimmed) return "Rule ID is required";
  if (!/^[a-z0-9_.-]+$/i.test(trimmed)) {
    return "Use letters, digits, underscores, dots, or hyphens only";
  }
  if (BUILTIN_RULE_IDS.has(trimmed)) {
    return "This ID collides with a built-in rule";
  }
  if (existingCustomIds.includes(trimmed)) {
    return "Another custom rule already uses this ID";
  }
  return null;
}

/** Validate a proposed regex pattern. Tries to compile and surface a human
 *  message if the engine rejects it. */
export function validateRegex(pattern: string): string | null {
  const trimmed = pattern.trim();
  if (!trimmed) return "Regex is required";
  try {
    new RegExp(trimmed);
    return null;
  } catch (err) {
    return err instanceof Error ? err.message : "Invalid regex";
  }
}
