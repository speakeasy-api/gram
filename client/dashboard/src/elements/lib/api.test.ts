import type { ElementsConfig } from "@/elements/types";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/utils", () => ({
  getServerURL: () => "https://ai.speakeasy.com",
}));

describe("getApiUrl", () => {
  beforeEach(() => {
    vi.resetModules();
  });

  async function loadGetApiUrl(gramApiUrl: string | undefined) {
    vi.stubGlobal("__GRAM_API_URL__", gramApiUrl);
    const { getApiUrl } = await import("./api");
    return getApiUrl;
  }

  it("uses config.api.url when set", async () => {
    const getApiUrl = await loadGetApiUrl("https://env.example.com");
    const config: ElementsConfig = {
      projectSlug: "test",
      api: { url: "https://config.example.com", session: "test-key" },
    };

    expect(getApiUrl(config)).toBe("https://config.example.com");
  });

  it("falls back to __GRAM_API_URL__ when config.api.url is not set", async () => {
    const getApiUrl = await loadGetApiUrl("https://env.example.com");
    const config: ElementsConfig = {
      projectSlug: "test",
      api: { session: "test-key" },
    };

    expect(getApiUrl(config)).toBe("https://env.example.com");
  });

  it("falls back to __GRAM_API_URL__ when config.api is undefined", async () => {
    const getApiUrl = await loadGetApiUrl("https://env.example.com");
    const config: ElementsConfig = {
      projectSlug: "test",
    };

    expect(getApiUrl(config)).toBe("https://env.example.com");
  });

  it("falls back to the server URL when both config.api.url and __GRAM_API_URL__ are not set", async () => {
    const getApiUrl = await loadGetApiUrl("");
    const config: ElementsConfig = {
      projectSlug: "test",
    };

    expect(getApiUrl(config)).toBe("https://ai.speakeasy.com");
  });

  it("falls back to the server URL when __GRAM_API_URL__ is undefined", async () => {
    const getApiUrl = await loadGetApiUrl(undefined);
    const config: ElementsConfig = {
      projectSlug: "test",
    };

    expect(getApiUrl(config)).toBe("https://ai.speakeasy.com");
  });

  it("skips empty string config.api.url and uses __GRAM_API_URL__", async () => {
    const getApiUrl = await loadGetApiUrl("https://env.example.com");
    const config: ElementsConfig = {
      projectSlug: "test",
      api: { url: "", session: "test-key" },
    };

    expect(getApiUrl(config)).toBe("https://env.example.com");
  });

  it("removes trailing slashes from the URL", async () => {
    const getApiUrl = await loadGetApiUrl("");
    const config: ElementsConfig = {
      projectSlug: "test",
      api: { url: "https://config.example.com///", session: "test-key" },
    };

    expect(getApiUrl(config)).toBe("https://config.example.com");
  });

  it("removes trailing slashes from __GRAM_API_URL__", async () => {
    const getApiUrl = await loadGetApiUrl("https://env.example.com//");
    const config: ElementsConfig = {
      projectSlug: "test",
    };

    expect(getApiUrl(config)).toBe("https://env.example.com");
  });
});
