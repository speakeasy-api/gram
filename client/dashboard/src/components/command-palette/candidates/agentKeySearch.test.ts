import { describe, expect, it, vi } from "vitest";
import { fetchAgentKeyNames } from "./agentKeySearch";

describe("fetchAgentKeyNames", () => {
  it("limits concurrent requests and caches only names and routing IDs", async () => {
    const resolvers: Array<() => void> = [];
    let active = 0;
    let peak = 0;
    const list = vi.fn(async (agentId: string) => {
      active += 1;
      peak = Math.max(peak, active);
      await new Promise<void>((resolve) => {
        resolvers.push(resolve);
      });
      active -= 1;
      return {
        keys: [{ id: `${agentId}-key`, name: "Deploy key", key: "secret" }],
      };
    });
    const ids = Array.from({ length: 9 }, (_, index) => `agent-${index}`);
    const pending = fetchAgentKeyNames(ids, list, new AbortController().signal);
    expect(list).toHaveBeenCalledTimes(4);
    resolvers.splice(0).forEach((resolve) => resolve());
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(8));
    expect(active).toBe(4);
    resolvers.splice(0).forEach((resolve) => resolve());
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(9));
    resolvers.splice(0).forEach((resolve) => resolve());
    const rows = await pending;
    expect(peak).toBe(4);
    expect(rows).toHaveLength(9);
    expect(rows[0]).toEqual({
      agentId: "agent-0",
      id: "agent-0-key",
      name: "Deploy key",
    });
    expect(JSON.stringify(rows)).not.toContain("secret");
  });

  it("does not start a later batch after the palette closes", async () => {
    const abort = new AbortController();
    const list = vi.fn(async () => {
      abort.abort();
      return { keys: [] };
    });
    await expect(
      fetchAgentKeyNames(["a", "b", "c", "d", "e"], list, abort.signal),
    ).rejects.toThrow();
    expect(list).toHaveBeenCalledTimes(4);
  });
});
