import type { Deployment } from "@gram/client/models/components/deployment.js";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@gram/client/react-query/latestDeployment.js", () => ({
  useLatestDeployment: vi.fn(() => ({
    data: undefined,
    isLoading: false,
    isError: false,
  })),
}));
vi.mock("@gram/client/react-query/activeDeployment.js", () => ({
  useActiveDeployment: vi.fn(() => ({
    data: undefined,
    isLoading: false,
    isError: false,
  })),
}));

import { useActiveDeployment } from "@gram/client/react-query/activeDeployment.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { sourceAssetId, useProjectSources } from "./source-list";

const mockLatest = vi.mocked(useLatestDeployment);
const mockActive = vi.mocked(useActiveDeployment);

/** Runs a hook in a throwaway React render and returns its result. */
function runHook<T>(hook: () => T): T {
  let result: T | undefined;
  function Probe() {
    result = hook();
    return null;
  }
  renderToStaticMarkup(createElement(Probe));
  return result as T;
}

function deployment(id: string, status: string, assetSuffix: string) {
  return {
    id,
    status,
    openapiv3Assets: [
      { id: `doc-${assetSuffix}`, assetId: `file-${assetSuffix}`, name: "api" },
    ],
    functionsAssets: [
      { id: `fn-${assetSuffix}`, assetId: `bundle-${assetSuffix}`, name: "fn" },
    ],
  } as unknown as Deployment;
}

describe("useProjectSources", () => {
  it("lists the active deployment's sources, not a newer failed one", () => {
    // Every push mints new asset ids. Tools are generated from the active
    // (last completed) deployment, so the ids the tools carry are the
    // active deployment's — a failed newer push must not swap them out.
    mockLatest.mockReturnValue({
      data: { deployment: deployment("dep-new", "failed", "new") },
      isLoading: false,
      isError: false,
    } as unknown as ReturnType<typeof useLatestDeployment>);
    mockActive.mockReturnValue({
      data: { deployment: deployment("dep-active", "completed", "active") },
      isLoading: false,
      isError: false,
    } as unknown as ReturnType<typeof useActiveDeployment>);

    const { sources } = runHook(() => useProjectSources());

    expect(sources.map(sourceAssetId)).toEqual(["doc-active", "fn-active"]);
  });
});
