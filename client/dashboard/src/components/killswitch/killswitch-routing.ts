import {
  KillswitchCapabilityKey as KillswitchCapabilityKeys,
  type KillswitchCapabilityKey,
} from "@gram/client/models/components/killswitchcapabilitykey.js";

const CREATE_PARAM = "create";
const RECORD_PARAM = "killswitch";
const CREATE_CAPABILITY_PARAM = "createCapability";
const ORIGIN_SERVER_PARAM = "originServer";

export const MCP_TOOL_CALLS_CAPABILITY: KillswitchCapabilityKey =
  KillswitchCapabilityKeys.McpToolCalls;

/**
 * What the editor opens on. Killswitches are managed from the page of the
 * person they restrict, so the subject comes from that page rather than from
 * the link — a link that named a different member than the page it lands on
 * would open an editor for someone the reader is not looking at.
 */
export type KillswitchCreateContext = {
  userId: string;
  capabilityKey?: KillswitchCapabilityKey;
  originatingMcpServerId?: string;
};

/** The part of that context a link can carry: what the sender was looking at. */
export type KillswitchCreateParams = {
  capabilityKey?: KillswitchCapabilityKey;
  originatingMcpServerId?: string;
};

export type KillswitchCreateRoute = {
  open: boolean;
  params: KillswitchCreateParams;
};

export function parseKillswitchCreateRoute(
  params: URLSearchParams,
): KillswitchCreateRoute {
  if (params.get(CREATE_PARAM) !== "1") return { open: false, params: {} };

  const capability = params.get(CREATE_CAPABILITY_PARAM);
  return {
    open: true,
    params: {
      capabilityKey:
        capability === MCP_TOOL_CALLS_CAPABILITY
          ? MCP_TOOL_CALLS_CAPABILITY
          : undefined,
      originatingMcpServerId: params.get(ORIGIN_SERVER_PARAM) ?? undefined,
    },
  };
}

export function cleanKillswitchCreateRoute(
  params: URLSearchParams,
): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(CREATE_PARAM);
  next.delete(CREATE_CAPABILITY_PARAM);
  next.delete(ORIGIN_SERVER_PARAM);
  return next;
}

export function openKillswitchCreateRoute(
  params: URLSearchParams,
  create?: KillswitchCreateParams,
): URLSearchParams {
  const next = cleanKillswitchCreateRoute(params);
  next.set(CREATE_PARAM, "1");
  if (create?.capabilityKey) {
    next.set(CREATE_CAPABILITY_PARAM, create.capabilityKey);
  }
  if (create?.originatingMcpServerId) {
    next.set(ORIGIN_SERVER_PARAM, create.originatingMcpServerId);
  }
  return next;
}

/**
 * A link that opens the editor on a person's Access tab.
 *
 * The identity href already carries the window the sender had open, so the
 * editor's own parameters are merged into it rather than replacing it.
 */
export function killswitchCreateHref(
  identityAccessHref: string,
  create?: KillswitchCreateParams,
): string {
  const [path = "", search = ""] = identityAccessHref.split("?");
  const params = openKillswitchCreateRoute(new URLSearchParams(search), create);
  return `${path}?${params.toString()}`;
}

/** The killswitch whose record the access tab has open, if any. */
export function selectedKillswitchId(
  params: URLSearchParams,
): string | undefined {
  return params.get(RECORD_PARAM) || undefined;
}

export function openKillswitchRecordRoute(
  params: URLSearchParams,
  killswitchId: string,
): URLSearchParams {
  const next = cleanKillswitchCreateRoute(params);
  next.set(RECORD_PARAM, killswitchId);
  return next;
}

export function closeKillswitchRecordRoute(
  params: URLSearchParams,
): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(RECORD_PARAM);
  return next;
}

/**
 * A link straight to one killswitch's record on its subject's access tab.
 *
 * This is what the address that used to name a killswitch page resolves to,
 * so an audit-log entry still opens the exact record it refers to.
 */
export function killswitchRecordHref(
  identityAccessHref: string,
  killswitchId: string,
): string {
  const [path = "", search = ""] = identityAccessHref.split("?");
  const params = openKillswitchRecordRoute(
    new URLSearchParams(search),
    killswitchId,
  );
  return `${path}?${params.toString()}`;
}
