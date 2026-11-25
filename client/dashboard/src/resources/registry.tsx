import type { IconName } from "@/components/ui/Icon/names";
import type { ReactNode } from "react";
import { ResourceView, type ResourceLocation } from "./kit/ResourceView";
import type { ChatDef, ResourceModule } from "./kit/types";
import { mcpServersDefinition } from "./mcp-servers/definition";
import { mcpServersSdkData } from "./mcp-servers/data.sdk";
import { catalogDefinition } from "./catalog/definition";
import { catalogSdkData } from "./catalog/data.sdk";

/**
 * A resource with its row and record types erased, so mini apps with
 * different models can sit in one list. Everything a host needs is here.
 */
export interface RegisteredResource {
  id: string;
  noun: { one: string; many: string };
  area: string;
  description: string;
  icon: IconName;
  chat: ChatDef;
  /** A hook: how many things the list holds, or undefined while unknown. */
  useCount: () => number | undefined;
  render: (props: {
    location: ResourceLocation;
    onNavigate: (location: ResourceLocation) => void;
    onOpenRelated: (resource: string, id: string) => void;
    hideBackLink?: boolean;
  }) => ReactNode;
}

function registerResource<Row extends object, Rec>(
  module: ResourceModule<Row, Rec>,
): RegisteredResource {
  const { definition } = module;
  return {
    id: definition.id,
    noun: definition.noun,
    area: definition.area,
    description: definition.list.description,
    icon: definition.list.empty.icon,
    useCount: () => {
      const list = module.data.useList();
      return list.status === "ready" ? list.data.length : undefined;
    },
    chat: definition.chat,
    render: (props) => <ResourceView module={module} {...props} />,
  };
}

/** Every mini app the prototype can open, backed by live data. */
export const RESOURCES: RegisteredResource[] = [
  registerResource({
    definition: mcpServersDefinition,
    data: mcpServersSdkData,
  }),
  registerResource({ definition: catalogDefinition, data: catalogSdkData }),
];

export function findResource(id: string): RegisteredResource | undefined {
  return RESOURCES.find((r) => r.id === id);
}

/**
 * A tool's own name, without the transport's namespacing: server toolsets
 * arrive as `mcp__p-platform_find_mcp` or `platform__get_mcp`.
 */
function bareToolName(name: string): string {
  // `mcp__p-platform_get_mcp` (assistant), `platform--get_mcp` (code mode).
  const last = name.split(/__|--/).pop() ?? name;
  return last.replace(/^p-[a-z0-9-]+?_/, "");
}

/** Calls made inside a code-mode script: `tool('mcp__p-platform_get_mcp', {mcp_id = 'x'})`. */
function scriptCalls(input: unknown): { name: string; args: unknown }[] {
  const script =
    input && typeof input === "object"
      ? ((input as Record<string, unknown>)["script"] ??
        (input as Record<string, unknown>)["code"])
      : undefined;
  if (typeof script !== "string") return [];
  const calls: { name: string; args: unknown }[] = [];
  // Lua `tool('name', {k = 'v'})` and Python `tools.call("name", {"k": "v"})`.
  const pattern =
    /(?:tools\.call|tool)\(\s*['"]([^'"]+)['"]\s*(?:,\s*\{([^}]*)\})?/g;
  for (const match of script.matchAll(pattern)) {
    const args: Record<string, string> = {};
    for (const pair of (match[2] ?? "").matchAll(
      /['"]?([a-z_]+)['"]?\s*[:=]\s*['"]([^'"]+)['"]/g,
    )) {
      args[pair[1]!] = pair[2]!;
    }
    calls.push({ name: match[1]!, args });
  }
  return calls;
}

export type PageRoute = { resource: string; location: ResourceLocation };

/** Every page an assistant tool call points at, in call order. */
export function routeToolCall(
  toolName: string,
  input: unknown,
  output: unknown,
): PageRoute[] {
  // Code mode runs the real tools inside a script.
  const inner = scriptCalls(input);
  if (inner.length > 0) {
    return inner.flatMap((call) => {
      const route = routeDirect(call.name, call.args, undefined);
      return route ? [route] : [];
    });
  }
  // Progressive discovery wraps the real tool in call_platform_tool.
  if (bareToolName(toolName) === "call_platform_tool" && isRecord(input)) {
    const name = input["name"];
    if (typeof name === "string") {
      const route = routeDirect(name, input["arguments"], output);
      return route ? [route] : [];
    }
  }
  const route = routeDirect(toolName, input, output);
  return route ? [route] : [];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === "object";
}

/**
 * Picks the page to open from a turn's candidates: the one whose keywords
 * the user's message uses most, the latest call breaking ties.
 */
export function pickRoute(
  candidates: PageRoute[],
  userText: string,
): PageRoute | undefined {
  const words = new Set(userText.toLowerCase().match(/[a-z]+/g) ?? []);
  let best: PageRoute | undefined;
  let bestScore = -1;
  for (const candidate of candidates) {
    const keywords = findResource(candidate.resource)?.chat.keywords ?? [];
    const score = keywords.filter((k) => words.has(k)).length;
    if (score >= bestScore) {
      best = candidate;
      bestScore = score;
    }
  }
  return best;
}

function routeDirect(
  toolName: string,
  input: unknown,
  output: unknown,
): { resource: string; location: ResourceLocation } | undefined {
  const bare = bareToolName(toolName);
  for (const resource of RESOURCES) {
    const route = resource.chat.tools[bare];
    if (!route) continue;
    if (route.opens === "list") {
      return { resource: resource.id, location: { view: "list" } };
    }
    const id = route.id(input, output);
    if (id) return { resource: resource.id, location: { view: "record", id } };
    return route.listFallback === false
      ? undefined
      : { resource: resource.id, location: { view: "list" } };
  }
  return undefined;
}
