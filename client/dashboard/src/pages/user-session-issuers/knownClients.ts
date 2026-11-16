import type { CimdClientPreset } from "@gram/client/models/components/cimdclientpreset.js";

/** One client Speakeasy allows, with every document it may present. */
export type KnownClient = {
  key: string;
  name: string;
  documents: { name: string; url: string; isPattern: boolean }[];
};

/**
 * Speakeasy's catalog as one entry per client people recognize. The catalog
 * lists documents, and a client can publish several ("ChatGPT" and "ChatGPT
 * (connectors)"), so documents share an entry when their names match once a
 * trailing parenthetical is dropped. Disabled entries admit nothing and are
 * left out.
 */
export function groupKnownClients(presets: CimdClientPreset[]): KnownClient[] {
  const byName = new Map<string, KnownClient>();
  for (const preset of presets) {
    if (!preset.enabled) continue;
    const name = baseName(preset.displayName);
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

function baseName(displayName: string): string {
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
      ? record.redirect_uris.filter(
          (value): value is string => typeof value === "string",
        )
      : [];
    const website =
      typeof record.client_uri === "string" ? record.client_uri : null;
    return { callbackUrls: redirects, website };
  } catch {
    return { callbackUrls: [], website: null };
  }
}
