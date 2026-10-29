import { describe, expect, it, vi } from "vitest";
import { GramCore } from "@gram/client/core";
import { HTTPClient } from "@gram/client/lib/http";
import { remoteSessionsTestServerConnection } from "@gram/client/funcs/remoteSessionsTestServerConnection";
import { CommitServerIdentityConfigurationForm$outboundSchema } from "@gram/client/models/components/commitserveridentityconfigurationform";

describe("setup API contracts", () => {
  it("serializes the target server and ordinary session for the backend probe", async () => {
    const fetcher = vi.fn(
      async (_input: RequestInfo | URL) =>
        new Response(
          JSON.stringify({
            status: "ready",
            code: "ready",
            message: "Connected.",
            tool_count: 0,
          }),
          { headers: { "content-type": "application/json" } },
        ),
    );
    const client = new GramCore({
      serverURL: "https://platform.example",
      httpClient: new HTTPClient({ fetcher }),
    });
    const result = await remoteSessionsTestServerConnection(
      client,
      {
        testServerConnectionRequestBody: {
          mcpServerId: "00000000-0000-4000-8000-000000000001",
        },
      },
      {
        sessionHeaderGramSession: "ordinary-session",
        projectSlugHeaderGramProject: "project",
      },
    );
    expect(result).toEqual({
      ok: true,
      value: {
        status: "ready",
        code: "ready",
        message: "Connected.",
        toolCount: 0,
      },
    });
    const request = fetcher.mock.calls[0]![0] as Request;
    expect(request.url).toBe(
      "https://platform.example/rpc/remoteSessions.testServerConnection",
    );
    expect(request.method).toBe("POST");
    expect(await request.json()).toEqual({
      mcp_server_id: "00000000-0000-4000-8000-000000000001",
    });
    expect(request.headers.get("Gram-Session")).toBe("ordinary-session");
    expect(request.headers.get("Gram-Project")).toBe("project");
    expect(request.headers.get("Authorization")).toBeNull();
  });

  it("preserves the backend's shared-identity guard in a serialized save", () => {
    const body = CommitServerIdentityConfigurationForm$outboundSchema.parse({
      mcpServerId: "server",
      clientMode: "manual",
      providerId: "provider",
      targetServerOnly: true,
    });
    expect(body.target_server_only).toBe(true);
  });
});
