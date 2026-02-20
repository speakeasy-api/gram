import type {
  Deployment,
  DeploymentLogEvent,
} from "@gram/client/models/components";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@gram/client/react-query/index.js", () => ({
  useDeployment: vi.fn(() => ({ data: undefined, isLoading: false })),
  useDeploymentLogs: vi.fn(() => ({ data: undefined, isLoading: false })),
  useLatestDeployment: vi.fn(() => ({ data: undefined, isLoading: false })),
  useListToolsets: vi.fn(() => ({ data: undefined })),
}));

import {
  useDeployment,
  useDeploymentLogs,
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import {
  computeFailedSources,
  flattenToolUrns,
  useFailedDeploymentSources,
} from "./useFailedDeploymentSources";

const mockLatest = vi.mocked(useLatestDeployment);
const mockDeployment = vi.mocked(useDeployment);
const mockLogs = vi.mocked(useDeploymentLogs);
const mockToolsets = vi.mocked(useListToolsets);

/** Runs a hook in a throwaway React render and returns its result. */
function renderHook<T>(hook: () => T): T {
  let result: T;
  function Capture() {
    result = hook();
    return null;
  }
  renderToStaticMarkup(createElement(Capture));
  return result!;
}

function setLatestDeployment(deployment: Record<string, unknown>) {
  mockLatest.mockReturnValue({
    data: { deployment },
    isLoading: false,
  } as ReturnType<typeof useLatestDeployment>);
}

function setSpecificDeployment(deployment: Record<string, unknown>) {
  mockDeployment.mockReturnValue({
    data: deployment,
    isLoading: false,
  } as ReturnType<typeof useDeployment>);
}

function setLogs(events: Record<string, unknown>[], status?: string) {
  mockLogs.mockReturnValue({
    data: { events, status },
    isLoading: false,
  } as ReturnType<typeof useDeploymentLogs>);
}

function setToolsets(toolsets: Record<string, unknown>[]) {
  mockToolsets.mockReturnValue({
    data: { toolsets },
  } as ReturnType<typeof useListToolsets>);
}

// -- Test helpers for pure function tests --

function makeDeployment(
  overrides: Partial<Deployment> & { id: string; status: string },
): Deployment {
  return {
    openapiv3Assets: [],
    functionsAssets: [],
    externalMcps: [],
    clonedFrom: undefined,
    createdAt: new Date(),
    externalId: undefined,
    externalMcpToolCount: 0,
    externalUrl: undefined,
    functionsToolCount: 0,
    githubPr: undefined,
    githubRepo: undefined,
    githubSha: undefined,
    idempotencyKey: undefined,
    openapiv3ToolCount: 0,
    organizationId: "org-1",
    packages: [],
    projectId: "proj-1",
    userId: "user-1",
    ...overrides,
  };
}

function makeEvent(
  overrides: Partial<DeploymentLogEvent> & { id: string; event: string },
): DeploymentLogEvent {
  return {
    createdAt: new Date().toISOString(),
    message: "",
    ...overrides,
  };
}

// -- flattenToolUrns --

describe("flattenToolUrns", () => {
  it("flattens URNs from multiple toolsets", () => {
    const result = flattenToolUrns([
      { toolUrns: ["tools:http:a:op1", "tools:http:a:op2"] },
      { toolUrns: ["tools:function:b:run"] },
    ]);
    expect(result).toEqual([
      "tools:http:a:op1",
      "tools:http:a:op2",
      "tools:function:b:run",
    ]);
  });

  it("handles toolsets with missing toolUrns", () => {
    const result = flattenToolUrns([{}, { toolUrns: ["tools:http:a:op1"] }]);
    expect(result).toEqual(["tools:http:a:op1"]);
  });

  it("returns empty array for empty input", () => {
    expect(flattenToolUrns([])).toEqual([]);
  });
});

// -- computeFailedSources --

describe("computeFailedSources", () => {
  it("returns no failures when there are no error events and status is not failed", () => {
    const deployment = makeDeployment({ id: "d1", status: "completed" });
    const result = computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns: [],
      events: [makeEvent({ id: "1", event: "source.info", message: "ok" })],
    });
    expect(result.hasFailures).toBe(false);
    expect(result.failedSources).toEqual([]);
    expect(result.generalErrors).toEqual([]);
  });

  it("reports hasFailures when status is failed even with no error events", () => {
    const deployment = makeDeployment({ id: "d1", status: "failed" });
    const result = computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns: [],
      events: [],
    });
    expect(result.hasFailures).toBe(true);
    expect(result.failedSources).toEqual([]);
  });

  it("groups errors by attachment and maps to sources", () => {
    const deployment = makeDeployment({
      id: "d1",
      status: "failed",
      openapiv3Assets: [
        { id: "a1", name: "Pet Store", slug: "pet-store", assetId: "x" },
      ],
    });
    const events = [
      makeEvent({
        id: "e1",
        event: "source.error",
        message: "bad schema",
        attachmentId: "a1",
      }),
      makeEvent({
        id: "e2",
        event: "source.error",
        message: "missing field",
        attachmentId: "a1",
      }),
    ];

    const result = computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns: [],
      events,
    });

    expect(result.failedSources).toHaveLength(1);
    expect(result.failedSources[0].slug).toBe("pet-store");
    expect(result.failedSources[0].type).toBe("openapi");
    expect(result.failedSources[0].errors).toHaveLength(2);
  });

  it("puts unmatched attachment errors into generalErrors", () => {
    const deployment = makeDeployment({
      id: "d1",
      status: "failed",
    });
    const result = computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns: [],
      events: [
        makeEvent({
          id: "e1",
          event: "deploy.error",
          message: "unknown",
          attachmentId: "no-match",
        }),
      ],
    });
    expect(result.failedSources).toHaveLength(0);
    expect(result.generalErrors).toHaveLength(1);
  });

  it("puts errors without attachmentId into generalErrors", () => {
    const deployment = makeDeployment({
      id: "d1",
      status: "failed",
    });
    const result = computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns: [],
      events: [
        makeEvent({
          id: "e1",
          event: "deploy.error",
          message: "general failure",
        }),
      ],
    });
    expect(result.generalErrors).toHaveLength(1);
    expect(result.generalErrors[0].message).toBe("general failure");
  });

  it("counts tool URNs only from compareDeployment sources", () => {
    const failedDeployment = makeDeployment({
      id: "d-failed",
      status: "failed",
      openapiv3Assets: [
        { id: "a1", name: "Pet Store", slug: "pet-store", assetId: "x" },
      ],
    });

    // Compare deployment has pet-store but NOT "other"
    const compareDeployment = makeDeployment({
      id: "d-compare",
      status: "completed",
      openapiv3Assets: [
        { id: "a1", name: "Pet Store", slug: "pet-store", assetId: "x" },
      ],
    });

    const toolUrns = [
      "tools:http:pet-store:listPets",
      "tools:http:pet-store:getPet",
      "tools:http:other:listOther", // not in compareDeployment
    ];

    const result = computeFailedSources({
      failedDeployment,
      compareDeployment,
      toolUrns,
      events: [
        makeEvent({
          id: "e1",
          event: "source.error",
          attachmentId: "a1",
        }),
      ],
    });

    expect(result.failedSources[0].toolCount).toBe(2);
  });

  it("excludes tool URNs from sources not in compareDeployment", () => {
    const failedDeployment = makeDeployment({
      id: "d-failed",
      status: "failed",
      openapiv3Assets: [
        { id: "a1", name: "Pet Store", slug: "pet-store", assetId: "x" },
      ],
    });

    // Compare deployment does NOT include pet-store
    const compareDeployment = makeDeployment({
      id: "d-compare",
      status: "completed",
      openapiv3Assets: [
        { id: "a2", name: "Other API", slug: "other", assetId: "y" },
      ],
    });

    const toolUrns = [
      "tools:http:pet-store:listPets",
      "tools:http:pet-store:getPet",
    ];

    const result = computeFailedSources({
      failedDeployment,
      compareDeployment,
      toolUrns,
      events: [
        makeEvent({
          id: "e1",
          event: "source.error",
          attachmentId: "a1",
        }),
      ],
    });

    // pet-store URNs are filtered out because pet-store isn't in compareDeployment
    expect(result.failedSources[0].toolCount).toBe(0);
  });

  it("handles multiple failed sources across different types", () => {
    const deployment = makeDeployment({
      id: "d1",
      status: "failed",
      openapiv3Assets: [
        { id: "a1", name: "Pet Store", slug: "pet-store", assetId: "x" },
      ],
      functionsAssets: [
        {
          id: "fn1",
          name: "My Func",
          slug: "my-func",
          assetId: "y",
          runtime: "node",
        },
      ],
      externalMcps: [
        {
          id: "mcp1",
          name: "GitHub",
          slug: "github",
          registryId: "r1",
          registryServerSpecifier: "s1",
        },
      ],
    });

    const toolUrns = [
      "tools:http:pet-store:listPets",
      "tools:function:my-func:run",
      "tools:externalmcp:github:proxy",
    ];

    const result = computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns,
      events: [
        makeEvent({
          id: "e1",
          event: "source.error",
          attachmentId: "a1",
        }),
        makeEvent({
          id: "e2",
          event: "build.error",
          attachmentId: "fn1",
        }),
        makeEvent({
          id: "e3",
          event: "source.error",
          attachmentId: "mcp1",
        }),
      ],
    });

    expect(result.failedSources).toHaveLength(3);
    const types = result.failedSources.map((s) => s.type).sort();
    expect(types).toEqual(["externalmcp", "function", "openapi"]);
    expect(
      result.failedSources.find((s) => s.type === "openapi")!.toolCount,
    ).toBe(1);
    expect(
      result.failedSources.find((s) => s.type === "function")!.toolCount,
    ).toBe(1);
    expect(
      result.failedSources.find((s) => s.type === "externalmcp")!.toolCount,
    ).toBe(1);
  });

  it("filters non-error events before processing", () => {
    const deployment = makeDeployment({
      id: "d1",
      status: "failed",
      openapiv3Assets: [
        { id: "a1", name: "Pet Store", slug: "pet-store", assetId: "x" },
      ],
    });

    const result = computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns: [],
      events: [
        makeEvent({
          id: "e1",
          event: "source.info",
          message: "started",
          attachmentId: "a1",
        }),
        makeEvent({
          id: "e2",
          event: "source.error",
          message: "failed",
          attachmentId: "a1",
        }),
      ],
    });

    expect(result.failedSources).toHaveLength(1);
    expect(result.failedSources[0].errors).toHaveLength(1);
    expect(result.failedSources[0].errors[0].message).toBe("failed");
  });

  it("counts URNs across multiple toolsets", () => {
    const deployment = makeDeployment({
      id: "d1",
      status: "failed",
      openapiv3Assets: [
        { id: "a1", name: "Pet Store", slug: "pet-store", assetId: "x" },
      ],
    });

    const toolUrns = flattenToolUrns([
      { toolUrns: ["tools:http:pet-store:listPets"] },
      { toolUrns: ["tools:http:pet-store:getPet"] },
    ]);

    const result = computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns,
      events: [
        makeEvent({
          id: "e1",
          event: "source.error",
          attachmentId: "a1",
        }),
      ],
    });

    expect(result.failedSources[0].toolCount).toBe(2);
  });
});

// -- useFailedDeploymentSources hook (integration) --

describe("useFailedDeploymentSources", () => {
  it("returns no failures when deployment and logs are absent", () => {
    const result = renderHook(() => useFailedDeploymentSources());
    expect(result.hasFailures).toBe(false);
    expect(result.failedSources).toEqual([]);
    expect(result.generalErrors).toEqual([]);
  });

  it("returns no failures when there are no error events", () => {
    setLatestDeployment({
      id: "dep-1",
      status: "completed",
      openapiv3Assets: [],
      functionsAssets: [],
      externalMcps: [],
    });
    setLogs([
      { id: "1", event: "source.info", message: "all good", attachmentId: "" },
    ]);
    setToolsets([]);

    const result = renderHook(() => useFailedDeploymentSources());
    expect(result.hasFailures).toBe(false);
    expect(result.failedSources).toEqual([]);
  });

  it("groups errors by source attachment", () => {
    setLatestDeployment({
      id: "dep-1",
      status: "failed",
      openapiv3Assets: [
        { id: "asset-1", name: "Pet Store", slug: "pet-store" },
      ],
      functionsAssets: [],
      externalMcps: [],
    });
    setLogs([
      {
        id: "e1",
        event: "source.error",
        message: "bad schema",
        attachmentId: "asset-1",
      },
      {
        id: "e2",
        event: "source.error",
        message: "missing field",
        attachmentId: "asset-1",
      },
    ]);
    setToolsets([]);

    const result = renderHook(() => useFailedDeploymentSources());
    expect(result.hasFailures).toBe(true);
    expect(result.failedSources).toHaveLength(1);
    expect(result.failedSources[0].slug).toBe("pet-store");
    expect(result.failedSources[0].type).toBe("openapi");
    expect(result.failedSources[0].errors).toHaveLength(2);
  });

  it("puts unmatched attachment errors into generalErrors", () => {
    setLatestDeployment({
      id: "dep-1",
      status: "failed",
      openapiv3Assets: [],
      functionsAssets: [],
      externalMcps: [],
    });
    setLogs([
      {
        id: "e1",
        event: "deploy.error",
        message: "unknown source",
        attachmentId: "no-match",
      },
    ]);
    setToolsets([]);

    const result = renderHook(() => useFailedDeploymentSources());
    expect(result.failedSources).toHaveLength(0);
    expect(result.generalErrors).toHaveLength(1);
  });

  it("counts tool URNs from toolsets for each failed source", () => {
    setLatestDeployment({
      id: "dep-1",
      status: "failed",
      openapiv3Assets: [
        { id: "asset-1", name: "Pet Store", slug: "pet-store" },
      ],
      functionsAssets: [],
      externalMcps: [],
    });
    setLogs([
      {
        id: "e1",
        event: "source.error",
        message: "fail",
        attachmentId: "asset-1",
      },
    ]);
    setToolsets([
      {
        toolUrns: [
          "tools:http:pet-store:listPets",
          "tools:http:pet-store:getPet",
          "tools:http:other:listOther",
        ],
      },
    ]);

    const result = renderHook(() => useFailedDeploymentSources());
    expect(result.failedSources[0].toolCount).toBe(2);
  });

  it("handles externalMcps source type", () => {
    setLatestDeployment({
      id: "dep-1",
      status: "failed",
      openapiv3Assets: [],
      functionsAssets: [],
      externalMcps: [{ id: "mcp-1", name: "My MCP", slug: "my-mcp" }],
    });
    setLogs([
      {
        id: "e1",
        event: "source.error",
        message: "timeout",
        attachmentId: "mcp-1",
      },
    ]);
    setToolsets([]);

    const result = renderHook(() => useFailedDeploymentSources());
    expect(result.failedSources[0].type).toBe("externalmcp");
  });

  it("handles functionsAssets source type", () => {
    setLatestDeployment({
      id: "dep-1",
      status: "failed",
      openapiv3Assets: [],
      functionsAssets: [{ id: "fn-1", name: "My Func", slug: "my-func" }],
      externalMcps: [],
    });
    setLogs([
      {
        id: "e1",
        event: "build.error",
        message: "compile failed",
        attachmentId: "fn-1",
      },
    ]);
    setToolsets([{ toolUrns: ["tools:function:my-func:run"] }]);

    const result = renderHook(() => useFailedDeploymentSources());
    expect(result.failedSources[0].type).toBe("function");
    expect(result.failedSources[0].toolCount).toBe(1);
  });

  it("errors without attachmentId go to generalErrors", () => {
    setLatestDeployment({
      id: "dep-1",
      status: "failed",
      openapiv3Assets: [],
      functionsAssets: [],
      externalMcps: [],
    });
    setLogs([
      {
        id: "e1",
        event: "deploy.error",
        message: "general failure",
      },
    ]);
    setToolsets([]);

    const result = renderHook(() => useFailedDeploymentSources());
    expect(result.generalErrors).toHaveLength(1);
    expect(result.generalErrors[0].message).toBe("general failure");
  });

  it("uses specific deployment when deploymentId is provided", () => {
    setSpecificDeployment({
      id: "specific-dep",
      status: "failed",
      openapiv3Assets: [],
      functionsAssets: [],
      externalMcps: [],
    });
    setLogs([
      {
        id: "e1",
        event: "deploy.error",
        message: "boom",
      },
    ]);
    setToolsets([]);

    const result = renderHook(() => useFailedDeploymentSources("specific-dep"));
    expect(result.hasFailures).toBe(true);
  });
});
