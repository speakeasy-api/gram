import { pluralize } from "@/lib/format";

type SharedServer = { id: string; name: string; slug: string };

/** The shared-server fields readEMA returns; hiddenServerCount is missing when the list couldn't be read. */
export type SharedServers = {
  servers?: SharedServer[];
  hiddenServerCount?: number;
  serversComplete?: boolean;
};

/** The other servers a binding applies to, in words, or null when none. */
export function sharedServersText(
  servers: SharedServer[] | undefined,
  hiddenCount: number | undefined,
  mcpServerId: string | undefined,
): string | null {
  if (!mcpServerId) return null;
  const names = (servers ?? [])
    .filter((server) => server.id !== mcpServerId)
    .map((server) => server.name || server.slug || "an unnamed server");
  const hidden = hiddenCount ?? 0;
  if (hidden > 0)
    names.push(
      names.length > 0
        ? `${pluralize(hidden, "other server")} you can’t see`
        : `${pluralize(hidden, "server")} you can’t see`,
    );
  if (names.length === 0) return null;
  return new Intl.ListFormat("en", {
    style: "long",
    type: "conjunction",
  }).format(names);
}

const MAY_ALSO_CHANGE =
  "Other servers that use this sign-in and service may change too.";

/**
 * What turning the setting on or off, or switching its credential, here also
 * changes, for a confirmation, or null when it is known to change nothing
 * else. A list that couldn't be read or is incomplete is said so, never shown
 * as the whole list.
 */
export function sharedImpactText(
  shared: SharedServers | undefined,
  mcpServerId: string,
  change: "on" | "off" | "switch",
): string | null {
  if (shared?.hiddenServerCount === undefined)
    return "Speakeasy couldn’t list the other servers that use this sign-in and service, so they may change too.";
  const names = sharedServersText(
    shared.servers,
    shared.hiddenServerCount,
    mcpServerId,
  );
  const parts: string[] = [];
  if (names)
    parts.push(
      change === "switch"
        ? `This also changes the credential for ${names}, on the same sign-in and service.`
        : `This also turns it ${change} for ${names}, on the same sign-in and service.`,
    );
  if (shared.serversComplete !== true) parts.push(MAY_ALSO_CHANGE);
  return parts.length > 0 ? parts.join(" ") : null;
}
