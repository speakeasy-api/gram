// Code generated from registry.Definitions by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

import type { Scope } from "@gram/client/models/components/rolegrant.js";

/** The resource kind each scope's checks and grant selectors name. */
export const scopeResourceKinds: Readonly<Record<string, string>> = {
  "agent:authorize": "agent",
  "agent:create": "agent",
  "agent:read": "agent",
  "agent:transfer": "agent",
  "agent:write": "agent",
  "assistant:blocked_read": "assistant",
  "assistant:blocked_write": "assistant",
  "assistant:read": "assistant",
  "assistant:write": "assistant",
  "channels:admin": "project",
  "channels:connect": "project",
  "channels:read_all": "project",
  "chat:read": "chat",
  "chat:write": "chat",
  "environment:blocked_read": "environment",
  "environment:blocked_write": "environment",
  "environment:read": "environment",
  "environment:write": "environment",
  "inference:execute": "inference",
  "log:read": "project",
  "mcp:auth": "mcp_auth_client",
  "mcp:blocked_auth": "mcp_auth_client",
  "mcp:blocked_connect": "mcp",
  "mcp:blocked_read": "mcp",
  "mcp:blocked_write": "mcp",
  "mcp:connect": "mcp",
  "mcp:read": "mcp",
  "mcp:write": "mcp",
  "memory:read": "project",
  "memory:write": "project",
  "org:admin": "org",
  "org:blocked_admin": "org",
  "org:blocked_read": "org",
  "org:device_agent_sync": "org",
  "org:hooks_ingest": "org",
  "org:read": "org",
  "plugin:blocked_write": "project",
  "plugin:write": "project",
  "project:blocked_read": "project",
  "project:blocked_write": "project",
  "project:read": "project",
  "project:write": "project",
  "risk_policy:block": "risk_policy",
  "risk_policy:bypass": "risk_policy",
  "risk_policy:evaluate": "risk_policy",
  "root": "*",
  "skill:blocked_read": "skill",
  "skill:blocked_write": "skill",
  "skill:read": "skill",
  "skill:write": "skill",
  "workload:blocked_read": "workload",
  "workload:blocked_write": "workload",
  "workload:read": "workload",
  "workload:write": "workload",
};

/**
 * The exclusion scopes a check of each scope must clear: its own exclusion,
 * then the broader blocks that also cover it. A block on read also takes
 * write away.
 */
export const scopeExclusions: Readonly<
  Record<string, readonly string[]>
> = {
  "assistant:read": [
    "assistant:blocked_read",
  ],
  "assistant:write": [
    "assistant:blocked_write",
    "assistant:blocked_read",
  ],
  "environment:read": [
    "environment:blocked_read",
  ],
  "environment:write": [
    "environment:blocked_write",
    "environment:blocked_read",
  ],
  "mcp:auth": [
    "mcp:blocked_auth",
  ],
  "mcp:connect": [
    "mcp:blocked_connect",
  ],
  "mcp:read": [
    "mcp:blocked_read",
  ],
  "mcp:write": [
    "mcp:blocked_write",
    "mcp:blocked_read",
  ],
  "org:admin": [
    "org:blocked_admin",
    "org:blocked_read",
  ],
  "org:read": [
    "org:blocked_read",
  ],
  "plugin:write": [
    "plugin:blocked_write",
  ],
  "project:read": [
    "project:blocked_read",
  ],
  "project:write": [
    "project:blocked_write",
    "project:blocked_read",
  ],
  "risk_policy:evaluate": [
    "risk_policy:bypass",
  ],
  "skill:read": [
    "skill:blocked_read",
  ],
  "skill:write": [
    "skill:blocked_write",
    "skill:blocked_read",
  ],
  "workload:read": [
    "workload:blocked_read",
  ],
  "workload:write": [
    "workload:blocked_write",
    "workload:blocked_read",
  ],
};

/**
 * The grant keys a probe or a connection may leave out and still match a
 * grant limited on them, per scope, in the API's spelling.
 */
export const scopeOpenKeys: Readonly<
  Record<string, { readonly probe: readonly string[]; readonly connection: readonly string[] }>
> = {
  "org:hooks_ingest": { probe: ["projectId"], connection: [] },
  "mcp:connect": { probe: ["tool", "disposition"], connection: ["tool", "disposition"] },
  "risk_policy:bypass": { probe: ["serverUrl", "serverIdentity"], connection: [] },
  "risk_policy:block": { probe: ["serverUrl", "serverIdentity"], connection: [] },
};

// Assigned in Check's static block, so only this module builds checks.
let newCheck: (
  scope: Scope,
  resourceKind: string,
  resourceId: string,
  dimensions: Readonly<Record<string, string>>,
  ids: readonly string[],
  options?: CheckOptions,
) => Check;

interface CheckOptions {
  readonly connection?: boolean;
  readonly collection?: boolean;
}

/**
 * One permission question, built by a constructor below and never by hand.
 * Each constructor is the Go ref of the same name (authz.mcpServer(id,
 * projectId).write() is authz.MCPServer(id, project).Write()), so every
 * argument the server needs is required. An id that cannot name a resource
 * (empty, "*", a "$" marker, or containing whitespace) makes the check
 * invalid, and an invalid check is never allowed.
 */
export class Check {
  readonly scope: Scope;
  readonly resourceKind: string;
  readonly resourceId: string;
  /** Dimensions the check carries, in the API's spelling (projectId). */
  readonly dimensions: Readonly<Record<string, string>>;
  /** Built as a connection, which names no tool. */
  readonly connection: boolean;
  /** Names every resource of its kind in one parent. */
  readonly collection: boolean;
  readonly invalid: boolean;
  readonly #probe: boolean;

  private constructor(fields: CheckFields, probe: boolean) {
    this.scope = fields.scope;
    this.resourceKind = fields.resourceKind;
    this.resourceId = fields.resourceId;
    this.dimensions = fields.dimensions;
    this.connection = fields.connection;
    this.collection = fields.collection;
    this.invalid = fields.invalid;
    this.#probe = probe;
  }

  static {
    newCheck = (
      scope: Scope,
      resourceKind: string,
      resourceId: string,
      dimensions: Readonly<Record<string, string>>,
      ids: readonly string[],
      options?: CheckOptions,
    ): Check =>
      new Check(
        {
          scope,
          resourceKind,
          resourceId,
          dimensions,
          connection: options?.connection ?? false,
          collection: options?.collection ?? false,
          invalid: ids.some((id) => !namesResource(id)),
        },
        false,
      );
  }

  get isProbe(): boolean {
    return this.#probe;
  }

  /**
   * The discovery question: could the caller act on some resource matching
   * the check. On one resource it may leave out the keys its permission
   * opens on a probe, as Check.Probe does on the server. On a collection it
   * asks about any resource of the kind in that parent, which the server
   * answers by listing them.
   */
  probe(): Check {
    return new Check(this, true);
  }
}

interface CheckFields {
  readonly scope: Scope;
  readonly resourceKind: string;
  readonly resourceId: string;
  readonly dimensions: Readonly<Record<string, string>>;
  readonly connection: boolean;
  readonly collection: boolean;
  readonly invalid: boolean;
}

/** Mirrors idFromString in authz/ids.go, spaces as Go's unicode.IsSpace. */
function namesResource(id: string): boolean {
  return (
    id !== "" &&
    id.toLowerCase() !== "00000000-0000-0000-0000-000000000000" &&
    id !== "*" &&
    !id.startsWith("$") &&
    !/[\t\n\v\f\r\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028-\u2029\u202f\u205f\u3000]/.test(id)
  );
}

/** One org resource: authz.Org(id). */
export function org(id: string): {
  readonly read: () => Check;
  readonly admin: () => Check;
  readonly deviceAgentSync: () => Check;
  readonly hooksIngest: (projectId: string) => Check;
} {
  return {
    /** org:read: View organization */
    read: (): Check =>
      newCheck("org:read", "org", id, {}, [id]),
    /** org:admin: Manage organization */
    admin: (): Check =>
      newCheck("org:admin", "org", id, {}, [id]),
    /** org:device_agent_sync: Sync device agent plugins */
    deviceAgentSync: (): Check =>
      newCheck("org:device_agent_sync", "org", id, {}, [id]),
    /** org:hooks_ingest: Send hook events */
    hooksIngest: (projectId: string): Check =>
      newCheck("org:hooks_ingest", "org", id, { projectId: projectId }, [id, projectId]),
  };
}

/** One chat resource: authz.Chat(id). */
export function chat(id: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** chat:read: View agent sessions */
    read: (): Check =>
      newCheck("chat:read", "chat", id, {}, [id]),
    /** chat:write: Manage agent sessions */
    write: (): Check =>
      newCheck("chat:write", "chat", id, {}, [id]),
  };
}

/** Every chat resource of one parent: authz.Chats(project). */
export function chats(projectId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** chat:read: View agent sessions */
    read: (): Check =>
      newCheck("chat:read", "chat", projectId, {}, [projectId], { collection: true }),
    /** chat:write: Manage agent sessions */
    write: (): Check =>
      newCheck("chat:write", "chat", projectId, {}, [projectId], { collection: true }),
  };
}

/** One project resource: authz.Project(id). */
export function project(id: string): {
  readonly channelsConnect: () => Check;
  readonly channelsAdmin: () => Check;
  readonly channelsReadAll: () => Check;
  readonly read: () => Check;
  readonly write: () => Check;
  readonly pluginWrite: () => Check;
  readonly logRead: () => Check;
  readonly memoryRead: () => Check;
  readonly memoryWrite: () => Check;
} {
  return {
    /** channels:connect: Connect to Channels */
    channelsConnect: (): Check =>
      newCheck("channels:connect", "project", id, {}, [id]),
    /** channels:admin: Manage Channels */
    channelsAdmin: (): Check =>
      newCheck("channels:admin", "project", id, {}, [id]),
    /** channels:read_all: Read all Channels messages */
    channelsReadAll: (): Check =>
      newCheck("channels:read_all", "project", id, {}, [id]),
    /** project:read: View projects */
    read: (): Check =>
      newCheck("project:read", "project", id, {}, [id]),
    /** project:write: Manage projects */
    write: (): Check =>
      newCheck("project:write", "project", id, {}, [id]),
    /** plugin:write: Publish plugins */
    pluginWrite: (): Check =>
      newCheck("plugin:write", "project", id, {}, [id]),
    /** log:read: View tool logs */
    logRead: (): Check =>
      newCheck("log:read", "project", id, {}, [id]),
    /** memory:read: Team memory */
    memoryRead: (): Check =>
      newCheck("memory:read", "project", id, {}, [id]),
    /** memory:write: Team memos */
    memoryWrite: (): Check =>
      newCheck("memory:write", "project", id, {}, [id]),
  };
}

/** One mcp resource: authz.MCPServer(id, project). */
export function mcpServer(id: string, projectId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
  readonly connect: () => Check;
} {
  return {
    /** mcp:read: View MCP servers */
    read: (): Check =>
      newCheck("mcp:read", "mcp", id, { projectId: projectId }, [id, projectId]),
    /** mcp:write: Manage MCP servers */
    write: (): Check =>
      newCheck("mcp:write", "mcp", id, { projectId: projectId }, [id, projectId]),
    /** mcp:connect: Connect to MCP servers */
    connect: (): Check =>
      newCheck("mcp:connect", "mcp", id, { projectId: projectId }, [id, projectId], { connection: true }),
  };
}

/** Every mcp resource of one parent: authz.MCPServers(project). */
export function mcpServers(projectId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
  readonly connect: () => Check;
} {
  return {
    /** mcp:read: View MCP servers */
    read: (): Check =>
      newCheck("mcp:read", "mcp", projectId, { projectId: projectId }, [projectId], { collection: true }),
    /** mcp:write: Manage MCP servers */
    write: (): Check =>
      newCheck("mcp:write", "mcp", projectId, { projectId: projectId }, [projectId], { collection: true }),
    /** mcp:connect: Connect to MCP servers */
    connect: (): Check =>
      newCheck("mcp:connect", "mcp", projectId, { projectId: projectId }, [projectId], { connection: true, collection: true }),
  };
}

/** One environment resource: authz.Environment(id, project). */
export function environment(id: string, projectId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** environment:read: View environments */
    read: (): Check =>
      newCheck("environment:read", "environment", id, { projectId: projectId }, [id, projectId]),
    /** environment:write: Manage environments */
    write: (): Check =>
      newCheck("environment:write", "environment", id, { projectId: projectId }, [id, projectId]),
  };
}

/** Every environment resource of one parent: authz.Environments(project). */
export function environments(projectId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** environment:read: View environments */
    read: (): Check =>
      newCheck("environment:read", "environment", projectId, { projectId: projectId }, [projectId], { collection: true }),
    /** environment:write: Manage environments */
    write: (): Check =>
      newCheck("environment:write", "environment", projectId, { projectId: projectId }, [projectId], { collection: true }),
  };
}

/** One skill resource: authz.Skill(id). */
export function skill(id: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** skill:read: View skills */
    read: (): Check =>
      newCheck("skill:read", "skill", id, {}, [id]),
    /** skill:write: Manage skills */
    write: (): Check =>
      newCheck("skill:write", "skill", id, {}, [id]),
  };
}

/** Every skill resource of one parent: authz.Skills(project). */
export function skills(projectId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** skill:read: View skills */
    read: (): Check =>
      newCheck("skill:read", "skill", projectId, {}, [projectId], { collection: true }),
    /** skill:write: Manage skills */
    write: (): Check =>
      newCheck("skill:write", "skill", projectId, {}, [projectId], { collection: true }),
  };
}

/** One assistant resource: authz.Assistant(id, project). */
export function assistant(id: string, projectId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** assistant:read: View assistants */
    read: (): Check =>
      newCheck("assistant:read", "assistant", id, { projectId: projectId }, [id, projectId]),
    /** assistant:write: Manage assistants */
    write: (): Check =>
      newCheck("assistant:write", "assistant", id, { projectId: projectId }, [id, projectId]),
  };
}

/** Every assistant resource of one parent: authz.Assistants(project). */
export function assistants(projectId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** assistant:read: View assistants */
    read: (): Check =>
      newCheck("assistant:read", "assistant", projectId, { projectId: projectId }, [projectId], { collection: true }),
    /** assistant:write: Manage assistants */
    write: (): Check =>
      newCheck("assistant:write", "assistant", projectId, { projectId: projectId }, [projectId], { collection: true }),
  };
}

/** One agent resource: authz.Agent(id). */
export function agent(id: string): {
  readonly read: () => Check;
  readonly create: () => Check;
  readonly write: () => Check;
  readonly authorize: () => Check;
  readonly transfer: () => Check;
} {
  return {
    /** agent:read: View agents */
    read: (): Check =>
      newCheck("agent:read", "agent", id, {}, [id]),
    /** agent:create: Create agents */
    create: (): Check =>
      newCheck("agent:create", "agent", id, {}, [id]),
    /** agent:write: Manage agents */
    write: (): Check =>
      newCheck("agent:write", "agent", id, {}, [id]),
    /** agent:authorize: Authorize agent credentials */
    authorize: (): Check =>
      newCheck("agent:authorize", "agent", id, {}, [id]),
    /** agent:transfer: Transfer agent ownership */
    transfer: (): Check =>
      newCheck("agent:transfer", "agent", id, {}, [id]),
  };
}

/** Every agent resource of one parent: authz.Agents(org). */
export function agents(orgId: string): {
  readonly read: () => Check;
  readonly create: () => Check;
  readonly write: () => Check;
  readonly authorize: () => Check;
  readonly transfer: () => Check;
} {
  return {
    /** agent:read: View agents */
    read: (): Check =>
      newCheck("agent:read", "agent", orgId, {}, [orgId], { collection: true }),
    /** agent:create: Create agents */
    create: (): Check =>
      newCheck("agent:create", "agent", orgId, {}, [orgId], { collection: true }),
    /** agent:write: Manage agents */
    write: (): Check =>
      newCheck("agent:write", "agent", orgId, {}, [orgId], { collection: true }),
    /** agent:authorize: Authorize agent credentials */
    authorize: (): Check =>
      newCheck("agent:authorize", "agent", orgId, {}, [orgId], { collection: true }),
    /** agent:transfer: Transfer agent ownership */
    transfer: (): Check =>
      newCheck("agent:transfer", "agent", orgId, {}, [orgId], { collection: true }),
  };
}

/** One workload resource: authz.Workload(id). */
export function workload(id: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** workload:read: View workload identities */
    read: (): Check =>
      newCheck("workload:read", "workload", id, {}, [id]),
    /** workload:write: Manage workload identities */
    write: (): Check =>
      newCheck("workload:write", "workload", id, {}, [id]),
  };
}

/** Every workload resource of one parent: authz.Workloads(org). */
export function workloads(orgId: string): {
  readonly read: () => Check;
  readonly write: () => Check;
} {
  return {
    /** workload:read: View workload identities */
    read: (): Check =>
      newCheck("workload:read", "workload", orgId, {}, [orgId], { collection: true }),
    /** workload:write: Manage workload identities */
    write: (): Check =>
      newCheck("workload:write", "workload", orgId, {}, [orgId], { collection: true }),
  };
}
