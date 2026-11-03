import { asSchema } from "ai";
import { describe, expect, it, vi } from "vitest";
import {
  createLoadSkillTool,
  listServedSkills,
  serverServesSkills,
  SKILLS_EXTENSION,
} from "./mcpSkills";

function jsonResponse(body: unknown, contentType = "application/json") {
  const text =
    contentType === "text/event-stream"
      ? `event: message\ndata: ${JSON.stringify(body)}\n\n`
      : JSON.stringify(body);
  return new Response(text, { headers: { "content-type": contentType } });
}

function skillEntry(name: string, description: string) {
  return {
    uri: `skill://proj/${name}/SKILL.md`,
    frontmatter: { name, description },
    resources: [],
  };
}

describe("serverServesSkills", () => {
  it("reads the Skills extension from the server capabilities", () => {
    const client = (capabilities: unknown) =>
      ({
        initializeResult: { capabilities },
        readResource: vi.fn(),
      }) as unknown as Parameters<typeof serverServesSkills>[0];

    expect(
      serverServesSkills(client({ extensions: { [SKILLS_EXTENSION]: {} } })),
    ).toBe(true);
    expect(serverServesSkills(client({ extensions: {} }))).toBe(false);
    expect(serverServesSkills(client({}))).toBe(false);
  });
});

describe("listServedSkills", () => {
  it("sends skills/list with the transport headers and follows cursors", async () => {
    const fetchFn = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        jsonResponse({
          jsonrpc: "2.0",
          id: "skills-list-0",
          result: { skills: [skillEntry("a", "Does A.")], nextCursor: "next" },
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse(
          {
            jsonrpc: "2.0",
            id: "skills-list-1",
            result: { skills: [skillEntry("b", "Does B."), { uri: 1 }] },
          },
          "text/event-stream",
        ),
      );

    const skills = await listServedSkills({
      url: "https://example.test/mcp/slug",
      headers: { "Gram-Chat-Session": "token" },
      protocolVersion: "2025-11-25",
      fetchFn,
    });

    expect(skills).toEqual([
      { uri: "skill://proj/a/SKILL.md", name: "a", description: "Does A." },
      { uri: "skill://proj/b/SKILL.md", name: "b", description: "Does B." },
    ]);
    expect(fetchFn).toHaveBeenCalledTimes(2);

    const [url, init] = fetchFn.mock.calls[1]!;
    expect(url).toBe("https://example.test/mcp/slug");
    const headers = init?.headers as Record<string, string>;
    expect(headers["Gram-Chat-Session"]).toBe("token");
    expect(headers["MCP-Protocol-Version"]).toBe("2025-11-25");
    expect(headers["Mcp-Method"]).toBe("skills/list");
    const body = JSON.parse(init?.body as string);
    expect(body.method).toBe("skills/list");
    expect(body.params.cursor).toBe("next");
    expect(body.params._meta).toEqual({
      "io.modelcontextprotocol/protocolVersion": "2025-11-25",
      "io.modelcontextprotocol/clientCapabilities": {},
    });
  });

  it("skips stream messages that are not the response", async () => {
    const stream = [
      `data: ${JSON.stringify({ jsonrpc: "2.0", method: "notifications/message", params: {} })}`,
      `data: ${JSON.stringify({ jsonrpc: "2.0", id: "skills-list-0", result: { skills: [skillEntry("a", "Does A.")] } })}`,
      "",
    ].join("\n\n");
    const fetchFn = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(stream, {
        headers: { "content-type": "text/event-stream" },
      }),
    );

    const skills = await listServedSkills({
      url: "https://example.test/mcp/slug",
      headers: {},
      protocolVersion: undefined,
      fetchFn,
    });
    expect(skills.map((skill) => skill.name)).toEqual(["a"]);
  });

  it("fails on a repeated cursor instead of returning a partial catalog", async () => {
    const fetchFn = vi
      .fn<typeof fetch>()
      .mockImplementation(async (_url, init) =>
        jsonResponse({
          jsonrpc: "2.0",
          id: JSON.parse(init?.body as string).id,
          result: { skills: [skillEntry("a", "Does A.")], nextCursor: "same" },
        }),
      );

    await expect(
      listServedSkills({
        url: "https://example.test/mcp/slug",
        headers: {},
        protocolVersion: undefined,
        fetchFn,
      }),
    ).rejects.toThrow("repeated a cursor");
  });

  it("rejects a JSON response to another request", async () => {
    const fetchFn = vi
      .fn<typeof fetch>()
      .mockResolvedValue(
        jsonResponse({ jsonrpc: "2.0", id: "other", result: { skills: [] } }),
      );

    await expect(
      listServedSkills({
        url: "https://example.test/mcp/slug",
        headers: {},
        protocolVersion: undefined,
        fetchFn,
      }),
    ).rejects.toThrow("another request");
  });

  it("throws on a JSON-RPC error", async () => {
    const fetchFn = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse({
        jsonrpc: "2.0",
        id: "skills-list-0",
        error: { code: -32601, message: "Method not found" },
      }),
    );

    await expect(
      listServedSkills({
        url: "https://example.test/mcp/slug",
        headers: {},
        protocolVersion: undefined,
        fetchFn,
      }),
    ).rejects.toThrow("Method not found");
  });
});

describe("createLoadSkillTool", () => {
  it("lists skills in its description and reads the named skill", async () => {
    const readResource = vi.fn().mockResolvedValue({
      contents: [{ uri: "skill://proj/a/SKILL.md", text: "# A" }],
    });
    const loadSkill = createLoadSkillTool(
      { initializeResult: {} as never, readResource },
      [{ uri: "skill://proj/a/SKILL.md", name: "a", description: "Does A." }],
    );

    expect(loadSkill.description).toContain("- a: Does A.");

    const result = await loadSkill.execute!({ name: "a" }, {
      toolCallId: "1",
      messages: [],
    } as never);
    expect(readResource).toHaveBeenCalledWith({
      uri: "skill://proj/a/SKILL.md",
    });
    expect(result).toEqual({ content: [{ type: "text", text: "# A" }] });

    const missing = await loadSkill.execute!({ name: "nope" }, {
      toolCallId: "2",
      messages: [],
    } as never);
    expect(missing).toMatchObject({ isError: true });
  });

  it("distinguishes same-named skills from different projects", async () => {
    const readResource = vi.fn().mockResolvedValue({
      contents: [{ uri: "skill://project-b/deploy/SKILL.md", text: "# B" }],
    });
    const skills = ["project-a", "project-b"].map((project) => ({
      uri: `skill://${project}/deploy/SKILL.md`,
      name: "deploy",
      description: `Deploy ${project}`,
    }));
    skills.push({
      uri: "skill://project-a/lint/SKILL.md",
      name: "lint",
      description: "Lint.",
    });
    const loadSkill = createLoadSkillTool(
      { initializeResult: {} as never, readResource },
      skills,
    );

    const schema = (await asSchema(loadSkill.inputSchema).jsonSchema) as {
      properties: { name: { enum: string[] } };
    };
    expect(schema.properties.name.enum).toEqual([
      "skill://project-a/deploy/SKILL.md",
      "skill://project-b/deploy/SKILL.md",
      "lint",
    ]);
    expect(loadSkill.description).toContain(
      "- skill://project-b/deploy/SKILL.md: Deploy project-b",
    );

    await loadSkill.execute!({ name: "skill://project-b/deploy/SKILL.md" }, {
      toolCallId: "1",
      messages: [],
    } as never);
    expect(readResource).toHaveBeenCalledWith({
      uri: "skill://project-b/deploy/SKILL.md",
    });
  });
});
