import { describe, expect, it } from "vitest";
import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import { toolMetadataToServerTools } from "./remoteToolMetadata";

function meta(
  overrides: Partial<ToolMetadata> & { toolName: string },
): ToolMetadata {
  return {
    mcpServerId: "server-1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  } as ToolMetadata;
}

describe("toolMetadataToServerTools", () => {
  it("maps tool names and synthesizes ids scoped to the server", () => {
    const tools = toolMetadataToServerTools("srv", [
      meta({ toolName: "search" }),
      meta({ toolName: "delete_record" }),
    ]);

    expect(tools).toHaveLength(2);
    expect(tools[0]).toMatchObject({ id: "srv:search", name: "search" });
    expect(tools[1]).toMatchObject({
      id: "srv:delete_record",
      name: "delete_record",
    });
  });

  it("carries the four annotation hints through as the disposition-hint object", () => {
    const [tool] = toolMetadataToServerTools("srv", [
      meta({
        toolName: "wipe",
        readOnlyHint: false,
        destructiveHint: true,
        idempotentHint: false,
        openWorldHint: true,
      }),
    ]);

    expect(tool!.annotations).toEqual({
      readOnlyHint: false,
      destructiveHint: true,
      idempotentHint: false,
      openWorldHint: true,
    });
  });

  it("leaves unset hints undefined rather than defaulting them", () => {
    const [tool] = toolMetadataToServerTools("srv", [
      meta({ toolName: "peek" }),
    ]);

    expect(tool!.annotations).toEqual({
      readOnlyHint: undefined,
      destructiveHint: undefined,
      idempotentHint: undefined,
      openWorldHint: undefined,
    });
  });

  it("keeps ids unique per server so two servers' same-named tools don't collide", () => {
    const [a] = toolMetadataToServerTools("srv-a", [meta({ toolName: "run" })]);
    const [b] = toolMetadataToServerTools("srv-b", [meta({ toolName: "run" })]);

    expect(a!.id).not.toBe(b!.id);
  });

  it("returns an empty list for a server with no synced metadata", () => {
    expect(toolMetadataToServerTools("srv", [])).toEqual([]);
  });
});
