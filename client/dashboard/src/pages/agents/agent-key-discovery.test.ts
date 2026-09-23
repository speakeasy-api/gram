import { describe, expect, it, vi } from "vitest";
import { Agents } from "@gram/client/sdk/agents.js";
import { HTTPClient } from "@gram/client/lib/http.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import { discoverKeyServerGrants } from "./agent-key-discovery";

const first = {
  resourceId: "toolset_one",
  projectId: "project_one",
  kind: "Hosted",
};
const second = {
  resourceId: "remote_two",
  projectId: "project_two",
  kind: "Remote",
};
const pinned = (
  resourceId: string,
  projectId: string,
): AgentPolicyGrantForm => ({
  effect: "allow",
  scope: "mcp:connect",
  selector: { resourceKind: "mcp", resourceId, projectId },
});
const signal = () => new AbortController().signal;

describe("scoped credential discovery", () => {
  it("sends every resource as toolset_ids on one actual generated GET, with no unscoped request", async () => {
    const requests: Request[] = [];
    const agents = new Agents({
      serverURL: "https://gram.example",
      httpClient: new HTTPClient({
        fetcher: async (input) => {
          requests.push(input as Request);
          return new Response("[]", {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        },
      }),
    });
    await discoverKeyServerGrants(
      agents,
      "agent_example",
      [first, second],
      signal(),
    );
    expect(requests).toHaveLength(1);
    const request = requests[0]!;
    expect(request.method).toBe("GET");
    const url = new URL(request.url);
    expect(url.pathname).toBe("/rpc/agents.listDelegableGrants");
    expect(url.searchParams.get("agent_id")).toBe("agent_example");
    expect(url.searchParams.has("toolset_id")).toBe(false);
    expect(url.searchParams.getAll("toolset_ids")).toEqual([
      first.resourceId,
      second.resourceId,
    ]);
  });
  it("narrows each pinned candidate onto its own server", async () => {
    const listDelegableGrants = vi
      .fn()
      .mockResolvedValue([
        pinned(first.resourceId, first.projectId),
        pinned(second.resourceId, second.projectId),
      ]);
    const controller = new AbortController();
    const grants = await discoverKeyServerGrants(
      { listDelegableGrants },
      "agent_example",
      [first, second],
      controller.signal,
    );
    expect(listDelegableGrants).toHaveBeenCalledExactlyOnceWith(
      {
        agentId: "agent_example",
        toolsetIds: [first.resourceId, second.resourceId],
      },
      undefined,
      { signal: controller.signal },
    );
    expect(grants.map((item) => item.selector)).toEqual([
      {
        resourceKind: "mcp",
        resourceId: first.resourceId,
        projectId: first.projectId,
      },
      {
        resourceKind: "mcp",
        resourceId: second.resourceId,
        projectId: second.projectId,
      },
    ]);
  });
  it("splits large inventories into sequential batches and rejects if one fails", async () => {
    const inventory = Array.from({ length: 150 }, (_, i) => ({
      resourceId: `toolset_${i}`,
      projectId: "project_one",
      kind: "Hosted",
    }));
    let inFlight = 0;
    const listDelegableGrants = vi.fn(
      async ({ toolsetIds }: { toolsetIds?: string[] }) => {
        inFlight++;
        expect(inFlight).toBe(1);
        await Promise.resolve();
        inFlight--;
        if (toolsetIds?.length === 50) throw new Error("forbidden");
        return [];
      },
    );
    await expect(
      discoverKeyServerGrants(
        { listDelegableGrants },
        "agent_example",
        inventory,
        signal(),
      ),
    ).rejects.toThrow("forbidden");
    expect(
      listDelegableGrants.mock.calls.map(([req]) => req.toolsetIds?.length),
    ).toEqual([100, 50]);
  });
  it("never expands a wildcard candidate onto the batch", async () => {
    const listDelegableGrants = vi
      .fn()
      .mockResolvedValue([pinned("*", first.projectId)]);
    expect(
      await discoverKeyServerGrants(
        { listDelegableGrants },
        "agent_example",
        [first, second],
        signal(),
      ),
    ).toEqual([]);
  });
  it("skips empty, unsupported, and duplicate inventory without unscoped fallback", async () => {
    const listDelegableGrants = vi.fn().mockResolvedValue([]);
    expect(
      await discoverKeyServerGrants(
        { listDelegableGrants },
        "agent_example",
        [],
        signal(),
      ),
    ).toEqual([]);
    expect(listDelegableGrants).not.toHaveBeenCalled();
    await discoverKeyServerGrants(
      { listDelegableGrants },
      "agent_example",
      [
        first,
        first,
        { ...second, kind: "Unproxied" },
        { ...second, resourceId: "" },
      ],
      signal(),
    );
    expect(listDelegableGrants).toHaveBeenCalledExactlyOnceWith(
      { agentId: "agent_example", toolsetIds: [first.resourceId] },
      undefined,
      expect.anything(),
    );
  });
});
