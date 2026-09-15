import {
  TOOL_ANNOTATIONS,
  type ToolAnnotation,
} from "@/components/tool-selection/annotations";

interface PrefillAnnotation {
  name: ToolAnnotation;
  mode: "snapshot" | "live";
}

export type ConsentSelection = null | {
  annotations: PrefillAnnotation[];
  tools: string[];
};

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isToolAnnotation(value: unknown): value is ToolAnnotation {
  return (
    typeof value === "string" &&
    (TOOL_ANNOTATIONS as readonly string[]).includes(value)
  );
}

/**
 * Parses the server-rendered prefill bootstrap: the subject's stored
 * annotation grants (with their modes) and manually picked tool names,
 * re-previewed against the live inventory once it loads. Unknown shapes
 * read as no prefill rather than blocking the page — the prefill is a
 * convenience, not an authorization input.
 */
export function parsePrefillBootstrap(
  raw: string | undefined,
): ConsentSelection {
  if (!raw) return null;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!isRecord(value)) return null;
  const rawAnnotations = value["annotations"];
  const rawTools = value["tools"];
  if (!Array.isArray(rawAnnotations) || !Array.isArray(rawTools)) return null;
  const annotations: PrefillAnnotation[] = [];
  for (const entry of rawAnnotations) {
    if (!isRecord(entry)) return null;
    const name = entry["name"];
    const mode = entry["mode"];
    if (!isToolAnnotation(name)) return null;
    if (mode !== "snapshot" && mode !== "live") return null;
    annotations.push({ name, mode });
  }
  if (!rawTools.every((t): t is string => typeof t === "string")) return null;
  return { annotations, tools: rawTools };
}
