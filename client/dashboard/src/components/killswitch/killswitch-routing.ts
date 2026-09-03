import {
  KillswitchCapabilityKey as KillswitchCapabilityKeys,
  type KillswitchCapabilityKey,
} from "@gram/client/models/components/killswitchcapabilitykey.js";

const CREATE_PARAM = "create";
const CREATE_USER_PARAM = "createUser";
const CREATE_CAPABILITY_PARAM = "createCapability";
const ORIGIN_SERVER_PARAM = "originServer";

export const MCP_TOOL_CALLS_CAPABILITY: KillswitchCapabilityKey =
  KillswitchCapabilityKeys.McpToolCalls;

export type KillswitchCreateContext = {
  userId: string;
  capabilityKey?: KillswitchCapabilityKey;
  originatingMcpServerId?: string;
};

export type KillswitchCreateRoute = {
  open: boolean;
  context?: KillswitchCreateContext;
};

export function parseKillswitchCreateRoute(
  params: URLSearchParams,
): KillswitchCreateRoute {
  if (params.get(CREATE_PARAM) !== "1") return { open: false };

  const userId = params.get(CREATE_USER_PARAM);
  if (!userId) return { open: true };

  const capability = params.get(CREATE_CAPABILITY_PARAM);
  const originatingMcpServerId = params.get(ORIGIN_SERVER_PARAM) ?? undefined;
  return {
    open: true,
    context: {
      userId,
      capabilityKey:
        capability === MCP_TOOL_CALLS_CAPABILITY
          ? MCP_TOOL_CALLS_CAPABILITY
          : undefined,
      originatingMcpServerId,
    },
  };
}

export function cleanKillswitchCreateRoute(
  params: URLSearchParams,
): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(CREATE_PARAM);
  next.delete(CREATE_USER_PARAM);
  next.delete(CREATE_CAPABILITY_PARAM);
  next.delete(ORIGIN_SERVER_PARAM);
  return next;
}

export function openKillswitchCreateRoute(
  params: URLSearchParams,
  context?: KillswitchCreateContext,
): URLSearchParams {
  const next = cleanKillswitchCreateRoute(params);
  next.set(CREATE_PARAM, "1");
  if (context) {
    next.set(CREATE_USER_PARAM, context.userId);
    if (context.capabilityKey) {
      next.set(CREATE_CAPABILITY_PARAM, context.capabilityKey);
    }
    if (context.originatingMcpServerId) {
      next.set(ORIGIN_SERVER_PARAM, context.originatingMcpServerId);
    }
  }
  return next;
}

export function killswitchCreateHref(
  baseHref: string,
  context: KillswitchCreateContext,
): string {
  const params = openKillswitchCreateRoute(new URLSearchParams(), context);
  return `${baseHref}?${params.toString()}`;
}
