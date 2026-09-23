import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PlatformSetupStep } from "./types";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
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
  useCreateAPIKeyMutation: () => ({ mutate: mocks.mutate }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ deviceAgent: { href: () => "/acme/device-agent" } }),
}));

import { AGENT_PLATFORMS } from "./setup-data";
import {
  usePlatformApiKeys,
  usePlatformPlaceholders,
} from "./platform-setup-values";

function step(code: string): PlatformSetupStep {
  return { title: "step", code, language: "text" };
}

beforeEach(() => {
  mocks.mutate.mockReset();
  mocks.publishStatus = {
    repoOwner: "acme",
    repoName: "acme-plugins",
    repoUrl: "https://github.com/acme/acme-plugins",
    marketplaceUrl: "https://app.example.com/marketplace/tok.git",
    claudeObservabilityPlugin: "acme-observability",
    codexObservabilityPlugin: "acme-observability-codex",
    cursorObservabilityPlugin: "acme-observability-cursor",
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

  it("uses the slugs the plugins were published under, not the current name", () => {
    // An organization that renamed after publishing still has the old slug in
    // its repo. Rebuilding it from the current name would point Claude and
    // Cursor at a plugin that is not there, which they accept in silence. The
    // slugs here share no prefix with the current name the org goes by
    // ("acme", "acme-speakeasy"), so anything derived locally fails this.
    mocks.publishStatus = {
      ...mocks.publishStatus!,
      claudeObservabilityPlugin: "before-the-rename-observability",
      cursorObservabilityPlugin: "before-the-rename-observability-cursor",
    };
    const { result } = renderHook(() => usePlatformPlaceholders());

    expect(result.current.snippetFor(step("{{GRAM_CLAUDE_PLUGIN_NAME}}"))).toBe(
      "before-the-rename-observability",
    );
    expect(result.current.snippetFor(step("{{GRAM_CURSOR_PLUGIN_NAME}}"))).toBe(
      "before-the-rename-observability-cursor",
    );
  });

  it("withholds a plugin-slug snippet when the server reports none", () => {
    // Absent when observability is disabled for the project, or when the read
    // failed — either way the snippet would name nothing.
    mocks.publishStatus = {
      repoOwner: "acme",
      repoName: "acme-plugins",
      repoUrl: "https://github.com/acme/acme-plugins",
      marketplaceUrl: "https://app.example.com/marketplace/tok.git",
    };
    const { result } = renderHook(() => usePlatformPlaceholders());

    expect(
      result.current.snippetFor(step("{{GRAM_CLAUDE_PLUGIN_NAME}}")),
    ).toBeUndefined();
    expect(
      result.current.snippetFor(step("{{GRAM_CURSOR_PLUGIN_NAME}}")),
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

const platform = (id: string) => AGENT_PLATFORMS.find((p) => p.id === id)!;
const callbacks = (index = 0) =>
  mocks.mutate.mock.calls[index]![1] as {
    onSuccess: (data: { key?: string }) => void;
    onError: (error: Error) => void;
  };

describe("usePlatformApiKeys", () => {
  it("mints once for concurrent Anthropic ensures and exposes the same key", () => {
    const { result } = renderHook(() =>
      usePlatformApiKeys({ shareAnthropicKey: true }),
    );
    const ensure = result.current.ensure;
    act(() => {
      ensure(platform("claude-cowork"));
      ensure(platform("claude"));
    });
    expect(mocks.mutate).toHaveBeenCalledOnce();
    expect(mocks.mutate.mock.calls[0]![0].request.createKeyForm).toMatchObject({
      name: expect.stringContaining("Claude Code and Cowork hooks"),
      scopes: ["hooks"],
    });
    expect(result.current.pending).toEqual({
      claude: true,
      "claude-cowork": true,
    });
    act(() => {
      callbacks().onSuccess({ key: "EXAMPLE_SHARED_KEY" });
      ensure(platform("claude"));
    });
    expect(result.current.keys).toEqual({
      claude: "EXAMPLE_SHARED_KEY",
      "claude-cowork": "EXAMPLE_SHARED_KEY",
    });
    expect(result.current.pending).toEqual({
      claude: false,
      "claude-cowork": false,
    });
    expect(mocks.mutate).toHaveBeenCalledOnce();
  });

  it.each(["error", "missing token"])(
    "shares %s and allows one explicit retry",
    (failure) => {
      const { result } = renderHook(() =>
        usePlatformApiKeys({ shareAnthropicKey: true }),
      );
      act(() => result.current.ensure(platform("claude")));
      act(() => {
        if (failure === "error") callbacks().onError(new Error("Mint failed"));
        else callbacks().onSuccess({});
      });
      expect(result.current.errors.claude).toBe(
        failure === "error"
          ? "Mint failed"
          : "API key token missing from response.",
      );
      expect(result.current.errors["claude-cowork"]).toBe(
        result.current.errors.claude,
      );
      expect(result.current.pending).toEqual({
        claude: false,
        "claude-cowork": false,
      });
      expect(result.current.keys).toEqual({});
      expect(mocks.mutate).toHaveBeenCalledOnce();
      act(() => {
        result.current.ensure(platform("claude-cowork"));
        result.current.ensure(platform("claude"));
      });
      expect(mocks.mutate).toHaveBeenCalledTimes(2);
      expect(result.current.errors).toEqual({});
      act(() => callbacks(1).onSuccess({ key: "EXAMPLE_RETRY_KEY" }));
      expect(result.current.keys.claude).toBe("EXAMPLE_RETRY_KEY");
      expect(result.current.keys["claude-cowork"]).toBe("EXAMPLE_RETRY_KEY");
    },
  );

  it("keeps other platforms and hook instances isolated", () => {
    const { result } = renderHook(() => ({
      shared: usePlatformApiKeys({ shareAnthropicKey: true }),
      standalone: usePlatformApiKeys(),
    }));
    act(() => {
      result.current.shared.ensure(platform("claude"));
      result.current.shared.ensure({
        ...platform("cursor"),
        setupSteps: [{ title: "Key", requiresApiKey: true }],
      });
      result.current.standalone.ensure(platform("claude"));
      result.current.standalone.ensure(platform("claude-cowork"));
    });
    expect(mocks.mutate).toHaveBeenCalledTimes(4);
    act(() => {
      callbacks(0).onSuccess({ key: "EXAMPLE_SHARED_KEY" });
      callbacks(1).onSuccess({ key: "EXAMPLE_CURSOR_KEY" });
      callbacks(2).onSuccess({ key: "EXAMPLE_STANDALONE_KEY" });
      callbacks(3).onSuccess({ key: "EXAMPLE_STANDALONE_COWORK_KEY" });
    });
    expect(result.current.shared.keys.cursor).toBe("EXAMPLE_CURSOR_KEY");
    expect(result.current.shared.keys.claude).toBe("EXAMPLE_SHARED_KEY");
    expect(result.current.shared.keys["claude-cowork"]).toBe(
      "EXAMPLE_SHARED_KEY",
    );
    expect(result.current.standalone.keys.claude).toBe(
      "EXAMPLE_STANDALONE_KEY",
    );
    expect(result.current.standalone.keys["claude-cowork"]).toBe(
      "EXAMPLE_STANDALONE_COWORK_KEY",
    );
  });

  it("does not mint for steps without an API key", () => {
    const { result } = renderHook(() => usePlatformApiKeys());
    act(() =>
      result.current.ensure({
        ...platform("claude"),
        setupSteps: [{ title: "No key needed" }],
      }),
    );
    expect(mocks.mutate).not.toHaveBeenCalled();
  });
});
