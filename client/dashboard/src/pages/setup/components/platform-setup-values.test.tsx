import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PlatformSetupStep } from "../types";

const mocks = vi.hoisted(() => ({
  publishStatus: undefined as undefined | Record<string, string>,
  marketplaceName: "acme-speakeasy",
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({ data: mocks.publishStatus }),
}));
vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettings: () => ({
    data: { effectiveName: mocks.marketplaceName },
  }),
}));
vi.mock("@gram/client/react-query/createAPIKey", () => ({
  useCreateAPIKeyMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme" }),
  useProjectSlugForRequests: () => "default",
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ deviceAgent: { href: () => "/acme/device-agent" } }),
}));

import { usePlatformPlaceholders } from "./platform-setup-values";

function step(code: string): PlatformSetupStep {
  return { title: "step", code, language: "text" };
}

beforeEach(() => {
  mocks.publishStatus = {
    repoOwner: "acme",
    repoName: "acme-plugins",
    repoUrl: "https://github.com/acme/acme-plugins",
    marketplaceUrl: "https://app.example.com/marketplace/tok.git",
  };
  mocks.marketplaceName = "acme-speakeasy";
});

describe("usePlatformPlaceholders", () => {
  it("fills a snippet once every value it needs has resolved", () => {
    const { result } = renderHook(() => usePlatformPlaceholders());

    expect(
      result.current.snippetFor(step("{{GRAM_REPO_OWNER}}/{{GRAM_REPO_NAME}}")),
    ).toBe("acme/acme-plugins");
  });

  it("withholds a repo snippet when the publish status could not be read", () => {
    // throwOnError:false means a failed read arrives as undefined rather than
    // throwing, so the repo fields are empty and the snippet would read "/".
    mocks.publishStatus = undefined;
    const { result } = renderHook(() => usePlatformPlaceholders());

    expect(
      result.current.snippetFor(step("{{GRAM_REPO_OWNER}}/{{GRAM_REPO_NAME}}")),
    ).toBeUndefined();
    expect(
      result.current.snippetFor(step("{{GRAM_REPO_URL}}")),
    ).toBeUndefined();
  });

  it("withholds a snippet whose marketplace name has not resolved", () => {
    mocks.marketplaceName = "";
    const { result } = renderHook(() => usePlatformPlaceholders());

    expect(
      result.current.snippetFor(
        step("{{GRAM_CLAUDE_PLUGIN_NAME}}@{{GRAM_MARKETPLACE_NAME}}"),
      ),
    ).toBeUndefined();
  });

  it("withholds a snippet that needs an API key before one is minted", () => {
    const { result } = renderHook(() => usePlatformPlaceholders());
    const keyed: PlatformSetupStep = {
      title: "keyed",
      code: "Gram-Key={{GRAM_API_KEY}}",
      requiresApiKey: true,
    };

    expect(result.current.snippetFor(keyed)).toBeUndefined();
    expect(result.current.snippetFor(keyed, "gram_live_x")).toBe(
      "Gram-Key=gram_live_x",
    );
  });
});
