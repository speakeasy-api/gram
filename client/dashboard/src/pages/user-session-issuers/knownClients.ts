import type { CimdClientPreset } from "@gram/client/models/components/cimdclientpreset.js";

/** One client Speakeasy allows, with every document it may present. */
export type KnownClient = {
  key: string;
  name: string;
  documents: { name: string; url: string; isPattern: boolean }[];
};

/**
 * Speakeasy's catalog as one entry per client people recognize. The catalog
 * lists documents, and a client can publish several: "ChatGPT" and "ChatGPT
 * (connectors)" are one client. A qualified name joins the entry named
 * exactly like its stem, and otherwise stands on its own, so "Anthropic
 * (Claude)" and "Anthropic (Claude Code)" stay two clients rather than one
 * vendor. Disabled entries admit nothing and are left out.
 */
export function groupKnownClients(presets: CimdClientPreset[]): KnownClient[] {
  const enabled = presets.filter((preset) => preset.enabled);
  const plainNames = new Set(
    enabled
      .map((preset) => preset.displayName)
      .filter((name) => stem(name) === name),
  );
  const byName = new Map<string, KnownClient>();
  for (const preset of enabled) {
    const base = stem(preset.displayName);
    const name = plainNames.has(base) ? base : preset.displayName;
    const key = name.toLowerCase();
    const entry = byName.get(key) ?? { key, name, documents: [] };
    entry.documents.push({
      name: preset.displayName,
      url: preset.clientIdMetadataUri,
      isPattern: preset.isPattern,
    });
    byName.set(key, entry);
  }
  return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name));
}

function stem(displayName: string): string {
  const trimmed = displayName.replace(/\s*\([^)]*\)\s*$/, "").trim();
  return trimmed === "" ? displayName : trimmed;
}

/** Up to two letters standing in for a client without a logo. */
export function monogram(name: string): string {
  const [first, second] = name.split(/\s+/).filter(Boolean);
  if (first && second)
    return (first.charAt(0) + second.charAt(0)).toUpperCase();
  return name.slice(0, 2);
}

/** What a checked client document says about itself, for the Add preview. */
export type DocumentSummary = {
  callbackUrls: string[];
  website: string | null;
};

/**
 * Reads the parts of a client document a person checks before allowing it.
 * The document is the client's own JSON, so anything missing or malformed
 * reads as absent rather than failing the preview.
 */
export function summarizeDocument(raw: string | undefined): DocumentSummary {
  if (!raw) return { callbackUrls: [], website: null };
  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) {
      return { callbackUrls: [], website: null };
    }
    const record = parsed as Record<string, unknown>;
    const redirects = Array.isArray(record.redirect_uris)
      ? record.redirect_uris.filter(isWebAddress)
      : [];
    const website = isWebAddress(record.client_uri) ? record.client_uri : null;
    return { callbackUrls: redirects, website };
  } catch {
    return { callbackUrls: [], website: null };
  }
}

/**
 * The document is the client's own, so only absolute http(s) addresses are
 * shown: anything else (a script URL, a relative path) is not something a
 * person can judge from the preview.
 */
function isWebAddress(value: unknown): value is string {
  if (typeof value !== "string") return false;
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:";
  } catch {
    return false;
  }
}
