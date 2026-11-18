import type { MCPClient } from "@ai-sdk/mcp";
import { jsonSchema, tool, type Tool } from "ai";

/**
 * Capability label of the MCP Skills extension (SEP-2640). A server that
 * serves skills declares it under `capabilities.extensions`.
 */
export const SKILLS_EXTENSION = "io.modelcontextprotocol/skills";

/** Name of the tool that loads a served skill into the conversation. */
export const LOAD_SKILL_TOOL_NAME = "load_skill";

/** A skill a server serves, as `skills/list` describes it. */
export interface ServedSkill {
  /** URI of the skill's SKILL.md, read with `resources/read`. */
  uri: string;
  name: string;
  description: string;
}

/** What the load-skill tool returns: the skill's SKILL.md as MCP tool content. */
export interface LoadSkillResult {
  content: Array<{ type: "text"; text: string }>;
  isError?: boolean;
}

type SkillsClient = Pick<MCPClient, "initializeResult" | "readResource">;

/** Reports whether the connected server declared the Skills extension. */
export function serverServesSkills(client: SkillsClient): boolean {
  const capabilities = client.initializeResult?.capabilities as
    | { extensions?: Record<string, unknown> }
    | undefined;
  return capabilities?.extensions?.[SKILLS_EXTENSION] !== undefined;
}

/** Most `skills/list` pages followed before discovery gives up. */
const MAX_SKILL_PAGES = 20;

/** How long one `skills/list` request may take before discovery gives up. */
const SKILLS_LIST_TIMEOUT_MS = 10_000;

/**
 * Lists the skills a server serves. `@ai-sdk/mcp` has no method for extension
 * requests, so `skills/list` goes out as a plain JSON-RPC POST carrying the
 * same headers as the client's transport, the negotiated protocol version,
 * and the request metadata MCP 2026-07-28 requires on every request. Every
 * page is followed and entries without a name are skipped. A request that
 * times out, a repeated cursor, or more than MAX_SKILL_PAGES pages fails
 * discovery rather than returning a partial catalog.
 */
export async function listServedSkills({
  url,
  headers,
  protocolVersion,
  fetchFn = (...args) => globalThis.fetch(...args),
  timeoutMs = SKILLS_LIST_TIMEOUT_MS,
}: {
  url: string;
  headers: Record<string, string>;
  protocolVersion: string | undefined;
  fetchFn?: typeof fetch;
  timeoutMs?: number;
}): Promise<ServedSkill[]> {
  const skills: ServedSkill[] = [];
  const seenCursors = new Set<string>();
  let cursor: string | undefined;
  for (let page = 0; ; page++) {
    if (page === MAX_SKILL_PAGES) {
      throw new Error(
        `skills/list returned more than ${MAX_SKILL_PAGES} pages`,
      );
    }

    const params: Record<string, unknown> = cursor ? { cursor } : {};
    if (protocolVersion) {
      params._meta = {
        "io.modelcontextprotocol/protocolVersion": protocolVersion,
        "io.modelcontextprotocol/clientCapabilities": {},
      };
    }
    const id = `skills-list-${page}`;
    const response = await fetchFn(url, {
      method: "POST",
      headers: {
        ...headers,
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
        "Mcp-Method": "skills/list",
        ...(protocolVersion && { "MCP-Protocol-Version": protocolVersion }),
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id,
        method: "skills/list",
        params,
      }),
      signal: AbortSignal.timeout(timeoutMs),
    });
    if (!response.ok) {
      throw new Error(`skills/list failed with HTTP ${response.status}`);
    }

    const message = await readJSONRPCResponse(response, id);
    if (message.error) {
      throw new Error(
        `skills/list failed: ${message.error.message ?? "error"}`,
      );
    }

    for (const entry of message.result?.skills ?? []) {
      const name = entry.frontmatter?.name;
      if (typeof entry.uri !== "string" || typeof name !== "string") continue;
      const description = entry.frontmatter?.description;
      skills.push({
        uri: entry.uri,
        name,
        description: typeof description === "string" ? description : "",
      });
    }

    cursor = message.result?.nextCursor || undefined;
    if (!cursor) return skills;
    if (seenCursors.has(cursor)) {
      throw new Error("skills/list repeated a cursor");
    }
    seenCursors.add(cursor);
  }
}

interface SkillsListMessage {
  id?: unknown;
  result?: {
    skills?: Array<{
      uri?: unknown;
      frontmatter?: { name?: unknown; description?: unknown };
    }>;
    nextCursor?: string;
  };
  error?: { message?: string };
}

/**
 * Reads the JSON-RPC response to request `id` from either a JSON body or a
 * Streamable HTTP event stream, where notifications and other messages may
 * precede it.
 */
async function readJSONRPCResponse(
  response: Response,
  id: string,
): Promise<SkillsListMessage> {
  const body = await response.text();
  if (!response.headers.get("content-type")?.includes("text/event-stream")) {
    const message = JSON.parse(body) as SkillsListMessage;
    if (message.id !== id) {
      throw new Error("skills/list returned a response to another request");
    }
    return message;
  }
  for (const line of body.split("\n")) {
    if (!line.startsWith("data:")) continue;
    let message: SkillsListMessage;
    try {
      message = JSON.parse(line.slice("data:".length)) as SkillsListMessage;
    } catch {
      continue;
    }
    if (message.id === id) return message;
  }
  throw new Error("skills/list returned no response");
}

/**
 * Gives each skill a name the model can pick it by. Skill names are only
 * unique within a project, and a gateway can serve skills from several
 * projects, so a name shared by more than one skill is qualified with the
 * skill's URI (`skill://<project>/<name>/SKILL.md`) instead.
 */
function selectableSkillNames(skills: ServedSkill[]): Map<string, ServedSkill> {
  const counts = new Map<string, number>();
  for (const skill of skills) {
    counts.set(skill.name, (counts.get(skill.name) ?? 0) + 1);
  }
  const byName = new Map<string, ServedSkill>();
  for (const skill of skills) {
    const key = counts.get(skill.name) === 1 ? skill.name : skill.uri;
    if (!byName.has(key)) byName.set(key, skill);
  }
  return byName;
}

/**
 * Builds the tool that loads a served skill. The available skills are listed
 * in the tool's description, so the model sees what each one is for and loads
 * a skill's SKILL.md (through `resources/read`, which the server records as a
 * skill load) only when the task calls for it.
 */
export function createLoadSkillTool(
  client: SkillsClient,
  skills: ServedSkill[],
): Tool<{ name: string }, LoadSkillResult> {
  const byName = selectableSkillNames(skills);
  const index = [...byName]
    .map(([name, skill]) => `- ${name}: ${skill.description}`)
    .join("\n");

  return tool({
    description: [
      "Load a skill served by this MCP server. A skill is a set of instructions for a specific kind of task.",
      "When the user's request matches a skill's description, load that skill before acting and follow its instructions.",
      "",
      "Available skills:",
      index,
    ].join("\n"),
    inputSchema: jsonSchema<{ name: string }>({
      type: "object",
      properties: {
        name: {
          type: "string",
          enum: [...byName.keys()],
          description: "Name of the skill to load, as listed above.",
        },
      },
      required: ["name"],
      additionalProperties: false,
    }),
    execute: async ({ name }): Promise<LoadSkillResult> => {
      const skill = byName.get(name);
      if (!skill) {
        return {
          isError: true,
          content: [{ type: "text" as const, text: `No skill named ${name}.` }],
        };
      }
      const result = await client.readResource({ uri: skill.uri });
      const text = result.contents
        .map((content) => ("text" in content ? content.text : ""))
        .filter(Boolean)
        .join("\n");
      return { content: [{ type: "text" as const, text }] };
    },
  });
}
