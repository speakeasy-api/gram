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
const grant: AgentPolicyGrantForm = {
  effect: "allow",
  scope: "mcp:connect",
  selector: { resourceKind: "mcp", resourceId: "*" },
};
const signal = () => new AbortController().signal;

describe("scoped credential discovery", () => {
  it("sends toolset_id on every actual generated GET, with no unscoped request", async () => {
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
    expect(requests).toHaveLength(2);
    expect(
      requests.map((request) => {
        expect(request.method).toBe("GET");
        const url = new URL(request.url);
        expect(url.pathname).toBe("/rpc/agents.listDelegableGrants");
        expect(url.searchParams.get("agent_id")).toBe("agent_example");
        expect(url.searchParams.has("resources")).toBe(false);
        expect(url.searchParams.has("project_id")).toBe(false);
        return url.searchParams.get("toolset_id");
      }),
    ).toEqual([first.resourceId, second.resourceId]);
  });
  it("starts scoped requests concurrently and publishes only the complete aggregate", async () => {
    const pending: Array<(grants: AgentPolicyGrantForm[]) => void> = [];
    const listDelegableGrants = vi.fn(
      () =>
        new Promise<AgentPolicyGrantForm[]>((resolve) => {
          pending.push(resolve);
        }),
    );
    let settled = false;
    const controller = new AbortController();
    const result = discoverKeyServerGrants(
      { listDelegableGrants },
      "agent_example",
      [first, second],
      controller.signal,
    ).then((grants) => {
      settled = true;
      return grants;
    });
    expect(listDelegableGrants).toHaveBeenCalledTimes(2);
    expect(listDelegableGrants).toHaveBeenNthCalledWith(
      1,
      { agentId: "agent_example", toolsetId: first.resourceId },
      undefined,
      { signal: controller.signal },
    );
    pending[0]!([grant]);
    await Promise.resolve();
    expect(settled).toBe(false);
    pending[1]!([grant]);
    expect((await result).map((item) => item.selector)).toEqual([
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
  it("rejects the aggregate when one scoped call fails", async () => {
    const listDelegableGrants = vi
      .fn()
      .mockResolvedValueOnce([grant])
      .mockRejectedValueOnce(new Error("forbidden"));
    await expect(
      discoverKeyServerGrants(
        { listDelegableGrants },
        "agent_example",
        [first, second],
        signal(),
      ),
    ).rejects.toThrow("forbidden");
    expect(listDelegableGrants).toHaveBeenCalledTimes(2);
  });
  it("does not expand a grant from one response onto another inventory resource", async () => {
    const listDelegableGrants = vi
      .fn()
      .mockResolvedValueOnce([
        {
          ...grant,
          selector: { ...grant.selector, resourceId: second.resourceId },
        },
      ])
      .mockResolvedValueOnce([]);
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
    expect(listDelegableGrants).toHaveBeenCalledTimes(1);
  });
});
