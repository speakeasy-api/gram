/** One remote endpoint a registry entry advertises. */
export interface RegistryRemote {
  url: string;
  type?: string;
  /** Names of `{placeholders}` the URL template still carries. */
  placeholders: string[];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** The `{name}` placeholders left in a URL template, in order, once each. */
export function urlPlaceholders(url: string): string[] {
  const names = [...url.matchAll(/\{([^{}]+)\}/g)].flatMap((match) =>
    match[1] ? [match[1]] : [],
  );
  return [...new Set(names)];
}

/**
 * The remotes listed under `server.remotes` in a registry record. The record
 * is staff-edited JSON that may not validate yet, so anything malformed is
 * skipped rather than thrown.
 */
export function registryRemotes(dataJson: string): RegistryRemote[] {
  let data: unknown;
  try {
    data = JSON.parse(dataJson);
  } catch {
    return [];
  }
  const server = isRecord(data) ? data.server : undefined;
  const remotes = isRecord(server) ? server.remotes : undefined;
  if (!Array.isArray(remotes)) return [];

  return remotes.flatMap((remote): RegistryRemote[] => {
    if (!isRecord(remote) || typeof remote.url !== "string" || !remote.url) {
      return [];
    }
    return [
      {
        url: remote.url,
        type: typeof remote.type === "string" ? remote.type : undefined,
        placeholders: urlPlaceholders(remote.url),
      },
    ];
  });
}
