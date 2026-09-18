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
vi.mock("@gram/client/react-query/listAssets.js", () => ({
  useListAssets: vi.fn(() => ({ data: undefined })),
}));

import { useActiveDeployment } from "@gram/client/react-query/activeDeployment.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListAssets } from "@gram/client/react-query/listAssets.js";
import { sourceAssetId, useProjectSources } from "./source-list";

const mockLatest = vi.mocked(useLatestDeployment);
const mockActive = vi.mocked(useActiveDeployment);
const mockAssets = vi.mocked(useListAssets);

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
      {
        id: `doc-${assetSuffix}`,
        assetId: `file-${assetSuffix}`,
        name: "api",
        slug: "api",
      },
    ],
    functionsAssets: [
      {
        id: `fn-${assetSuffix}`,
        assetId: `bundle-${assetSuffix}`,
        name: "fn",
        slug: "fn",
      },
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

  it("carries the slug and the file's facts once the assets arrive", () => {
    mockActive.mockReturnValue({
      data: { deployment: deployment("dep-active", "completed", "active") },
      isLoading: false,
      isError: false,
    } as unknown as ReturnType<typeof useActiveDeployment>);
    const createdAt = new Date("2026-01-01T00:00:00Z");
    const updatedAt = new Date("2026-02-01T00:00:00Z");
    mockAssets.mockReturnValue({
      data: {
        assets: [
          {
            id: "file-active",
            contentType: "application/json",
            createdAt,
            updatedAt,
          },
        ],
      },
    } as unknown as ReturnType<typeof useListAssets>);

    const { sources } = runHook(() => useProjectSources());

    expect(sources[0]).toMatchObject({
      slug: "api",
      assetId: "file-active",
      contentType: "application/json",
      createdAt,
      updatedAt,
    });
    // The bundle isn't in the asset list yet: the source still lists, just
    // without the facts only the file carries.
    expect(sources[1]).toMatchObject({ slug: "fn", assetId: "bundle-active" });
    expect(sources[1]?.contentType).toBeUndefined();
  });
});
