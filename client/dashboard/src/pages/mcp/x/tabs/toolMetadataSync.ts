import type {
  ProxiedMcpTool,
  ProxiedMcpToolAnnotations,
} from "@/hooks/useProxiedMcpTools";
import type { ToolMetadataForm } from "@gram/client/models/components/toolmetadataform.js";
import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import type { ToolMetadataByName } from "./useToolMetadata";

/** The annotation fields Speakeasy mirrors from a tool's MCP `annotations` object. */
export const METADATA_FIELDS = [
  "title",
  "readOnlyHint",
  "destructiveHint",
  "idempotentHint",
  "openWorldHint",
] as const;

export type MetadataField = (typeof METADATA_FIELDS)[number];

export type FieldChange = {
  field: MetadataField;
  /** What Speakeasy has stored. Undefined means the hint is unset. */
  stored: string | boolean | undefined;
  /** What the live session advertises. Undefined means the hint is unset. */
  advertised: string | boolean | undefined;
};

export type ToolDrift =
  /** Advertised by the session with nothing stored for it yet. */
  | { kind: "new"; toolName: string; advertised: ProxiedMcpToolAnnotations }
  /** Stored, still advertised, but the two disagree on at least one field. */
  | { kind: "changed"; toolName: string; changes: FieldChange[] }
  /** Stored but no longer advertised by the session. */
  | { kind: "removed"; toolName: string };

type LiveTools = Record<string, ProxiedMcpTool>;

function advertisedField(
  annotations: ProxiedMcpToolAnnotations | undefined,
  field: MetadataField,
): string | boolean | undefined {
  return annotations?.[field];
}

function storedField(
  stored: ToolMetadata,
  field: MetadataField,
): string | boolean | undefined {
  return stored[field];
}

/**
 * Compare Speakeasy's stored metadata against what the live MCP session advertises.
 *
 * The session is the source of truth: this is what a sync would change, in the
 * order the Inspect tab lists it. An empty result means the table already
 * mirrors the session.
 */
export function computeDrift(
  live: LiveTools,
  stored: ToolMetadataByName,
): ToolDrift[] {
  const drift: ToolDrift[] = [];

  for (const [toolName, tool] of Object.entries(live)) {
    const entry = stored[toolName];
    if (!entry) {
      drift.push({
        kind: "new",
        toolName,
        advertised: tool.annotations ?? {},
      });
      continue;
    }

    const changes = METADATA_FIELDS.flatMap((field) => {
      const a = advertisedField(tool.annotations, field);
      const s = storedField(entry, field);
      return a === s ? [] : [{ field, stored: s, advertised: a }];
    });

    if (changes.length > 0) {
      drift.push({ kind: "changed", toolName, changes });
    }
  }

  for (const toolName of Object.keys(stored)) {
    if (!(toolName in live)) {
      drift.push({ kind: "removed", toolName });
    }
  }

  return drift.sort((a, b) => a.toolName.localeCompare(b.toolName));
}

function advertisedToForm(
  toolName: string,
  tool: ProxiedMcpTool,
): ToolMetadataForm {
  const annotations = tool.annotations;
  return {
    toolName,
    title: annotations?.title,
    readOnlyHint: annotations?.readOnlyHint,
    destructiveHint: annotations?.destructiveHint,
    idempotentHint: annotations?.idempotentHint,
    openWorldHint: annotations?.openWorldHint,
  };
}

function sortForms(forms: ToolMetadataForm[]): ToolMetadataForm[] {
  return forms.sort((a, b) => a.toolName.localeCompare(b.toolName));
}

/**
 * The payload for addToolMetadataBatch: only tools the session advertises that
 * Speakeasy has never stored. Returns null when there are none.
 *
 * A tool appearing for the first time has no stored value to disagree with, so
 * recording it needs no confirmation. That endpoint is strictly additive and
 * rejects a tool that already has an entry, so this sends nothing else — a
 * conflict means our view of stored state was stale and we should reload it.
 */
export function newToolsBatch(
  live: LiveTools,
  stored: ToolMetadataByName,
): ToolMetadataForm[] | null {
  const missing = Object.entries(live).filter(([name]) => !stored[name]);
  if (missing.length === 0) return null;

  return sortForms(missing.map(([name, tool]) => advertisedToForm(name, tool)));
}

/**
 * The payload that makes the stored set mirror the session exactly.
 *
 * Only live tools are sent, so anything stored that the session no longer
 * advertises is removed by omission — the destructive half of a sync, which is
 * why this one is never run without the user asking for it.
 */
export function fullSyncBatch(live: LiveTools): ToolMetadataForm[] {
  return sortForms(
    Object.entries(live).map(([name, tool]) => advertisedToForm(name, tool)),
  );
}
