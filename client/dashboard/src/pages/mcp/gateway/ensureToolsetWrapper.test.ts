import { expect, it, vi } from "vitest";
import { ensureToolsetWrapper } from "./ensureToolsetWrapper";
function setup(servers: { id: string }[] = []) {
  const list = vi.fn().mockResolvedValue({ mcpServers: servers });
  const create = vi.fn().mockResolvedValue({ id: "new-server" });
  const client = { mcpServers: { list, create } } as unknown as Parameters<
    typeof ensureToolsetWrapper
  >[0];
  return { client, list, create };
}
const toolset = { id: "toolset", name: "Source" };
it("fails closed when an uncertain wrapper write is invisible to reconciliation", async () => {
  const { client, create, list } = setup();
  await expect(ensureToolsetWrapper(client, toolset, true)).rejects.toThrow(
    /manually/i,
  );
  expect(list).toHaveBeenCalledWith({ toolsetId: toolset.id });
  expect(create).not.toHaveBeenCalled();
});
it("reuses the visible retained wrapper", async () => {
  const { client, create } = setup([{ id: "existing" }]);
  await expect(ensureToolsetWrapper(client, toolset, true)).resolves.toBe(
    "existing",
  );
  expect(create).not.toHaveBeenCalled();
});
it("rejects ambiguous reconciliation without creating", async () => {
  const { client, create } = setup([{ id: "one" }, { id: "two" }]);
  await expect(ensureToolsetWrapper(client, toolset, true)).rejects.toThrow(
    /Multiple/,
  );
  expect(create).not.toHaveBeenCalled();
});
it("creates a wrapper only on the initial attempt", async () => {
  const { client, create, list } = setup();
  await expect(ensureToolsetWrapper(client, toolset, false)).resolves.toBe(
    "new-server",
  );
  expect(list).not.toHaveBeenCalled();
  expect(create).toHaveBeenCalledWith({
    createMcpServerForm: {
      name: toolset.name,
      toolsetId: toolset.id,
      visibility: "private",
    },
  });
});
