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

/**
 * Lists the skills a server serves. `@ai-sdk/mcp` has no method for extension
 * requests, so `skills/list` goes out as a plain JSON-RPC POST carrying the
 * same headers as the client's transport and the negotiated protocol version.
 * Every page is followed, and entries without a name are skipped.
 */
export async function listServedSkills({
  url,
  headers,
  protocolVersion,
  fetchFn = (...args) => globalThis.fetch(...args),
}: {
  url: string;
  headers: Record<string, string>;
  protocolVersion: string | undefined;
  fetchFn?: typeof fetch;
}): Promise<ServedSkill[]> {
  const skills: ServedSkill[] = [];
  let cursor: string | undefined;
  // A misbehaving server could hand back the same cursor forever.
  for (let page = 0; page < 20; page++) {
    const params: Record<string, unknown> = cursor ? { cursor } : {};
    if (protocolVersion) {
      params._meta = {
        "io.modelcontextprotocol/protocolVersion": protocolVersion,
      };
    }
    const response = await fetchFn(url, {
      method: "POST",
      headers: {
        ...headers,
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
        ...(protocolVersion && { "MCP-Protocol-Version": protocolVersion }),
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: `skills-list-${page}`,
        method: "skills/list",
        params,
      }),
    });
    if (!response.ok) {
      throw new Error(`skills/list failed with HTTP ${response.status}`);
    }

    const message = await readJSONRPCMessage(response);
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
    if (!cursor) break;
  }
  return skills;
}

interface SkillsListMessage {
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
 * Reads the JSON-RPC response from either a JSON body or a Streamable HTTP
 * event stream, where it is the first `data:` line that parses.
 */
async function readJSONRPCMessage(
  response: Response,
): Promise<SkillsListMessage> {
  const body = await response.text();
  if (!response.headers.get("content-type")?.includes("text/event-stream")) {
    return JSON.parse(body) as SkillsListMessage;
  }
  for (const line of body.split("\n")) {
    if (!line.startsWith("data:")) continue;
    try {
      return JSON.parse(line.slice("data:".length)) as SkillsListMessage;
    } catch {
      continue;
    }
  }
  throw new Error("skills/list returned no message");
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
  const byName = new Map(skills.map((skill) => [skill.name, skill]));
  const index = skills
    .map((skill) => `- ${skill.name}: ${skill.description}`)
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
          enum: skills.map((skill) => skill.name),
          description: "Name of the skill to load.",
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
