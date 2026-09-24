import { describe, expect, it } from "vitest";
import type { SupportCoverageCell } from "@gram/client/models/components/supportcoveragecell.js";
import {
  activeAgentCoverageLabel,
  capabilities,
  cellKey,
  footprintOf,
  gapsClosedBy,
  indexCells,
  methods,
  surfaces,
  type CapabilityId,
  type SurfaceId,
} from "./support-matrix-model";

function cell(
  capability: CapabilityId,
  surface: SurfaceId,
  status: SupportCoverageCell["status"],
): SupportCoverageCell {
  return { capability, surface, status, value: 0, detail: "", lastSeen: "" };
}

describe("support matrix model", () => {
  // hook_source folding moved to the server (internal/agentsurface) and is
  // covered by its own Go tests. The copy that used to live here silently
  // dropped unrecognized sources, so the page reported full coverage while
  // hiding real activity.

  it.each([
    ["device", 60, "devices running the agent within 60 minutes"],
    [
      "user",
      60,
      "devices whose assigned user has an active agent within 60 minutes",
    ],
    ["device", undefined, "devices running the agent"],
    [undefined, undefined, "devices whose assigned user has an active agent"],
  ] as const)(
    "formats %s attestation coverage",
    (attestation, minutes, expected) => {
      expect(activeAgentCoverageLabel(attestation, minutes)).toBe(expected);
    },
  );

  it("defines integration footprints using known surfaces and capabilities", () => {
    const surfaceIds = new Set(surfaces.map((surface) => surface.id));
    const capabilityIds = new Set(
      capabilities.map((capability) => capability.id),
    );

    for (const method of methods) {
      expect(method.surfaces.every((surface) => surfaceIds.has(surface))).toBe(
        true,
      );
      expect(
        method.capabilities.every((capability) =>
          capabilityIds.has(capability),
        ),
      ).toBe(true);
    }
  });

  it("indexes cells by capability and surface", () => {
    const index = indexCells([
      cell("session", "cursor", "observed"),
      cell("shadow", "cowork", "pending"),
    ]);

    expect(index.get(cellKey("session", "cursor"))?.status).toBe("observed");
    expect(index.get(cellKey("shadow", "cowork"))?.status).toBe("pending");
    expect(index.get(cellKey("session", "cowork"))).toBeUndefined();
  });

  it("counts only unobserved cells as gaps an integration would close", () => {
    const method = {
      id: "test",
      name: "Test",
      description: "",
      setup: "",
      surfaces: ["cursor", "codex"] as SurfaceId[],
      capabilities: ["session", "cost"] as CapabilityId[],
    };

    // One of the four cells the method reaches is already reporting, so it is
    // not a gap. Ranking on the raw footprint instead would recommend an
    // integration that adds nothing.
    const index = indexCells([cell("session", "cursor", "observed")]);

    expect(footprintOf(method).size).toBe(4);
    expect(gapsClosedBy(method, index).size).toBe(3);
    expect(gapsClosedBy(method, index).has(cellKey("session", "cursor"))).toBe(
      false,
    );
  });

  it("treats a pending cell as a gap", () => {
    const method = {
      id: "test",
      name: "Test",
      description: "",
      setup: "",
      surfaces: ["cowork"] as SurfaceId[],
      capabilities: ["shadow"] as CapabilityId[],
    };

    // "Not yet reportable" is still missing coverage from the operator's
    // point of view, so an integration that would report it counts.
    const index = indexCells([cell("shadow", "cowork", "pending")]);
    expect(gapsClosedBy(method, index).size).toBe(1);
  });
});
