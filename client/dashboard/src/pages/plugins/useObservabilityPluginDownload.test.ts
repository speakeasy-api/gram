import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useObservabilityPluginDownload } from "./useObservabilityPluginDownload";

const mocks = vi.hoisted(() => ({ fetch: vi.fn() }));
vi.mock("@/contexts/Fetcher", () => ({ useFetcher: () => mocks }));
const revokeObjectURL = vi.fn();

beforeEach(() => {
  mocks.fetch.mockReset();
  revokeObjectURL.mockReset();
  vi.useFakeTimers();
  vi.stubGlobal("URL", {
    createObjectURL: vi.fn(() => "blob:plugin"),
    revokeObjectURL,
  });
});
afterEach(() => {
  cleanup();
  vi.runAllTimers();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("useObservabilityPluginDownload", () => {
  it.each([
    ['attachment; filename="quoted.zip"', "quoted.zip"],
    ["attachment; filename=bare.zip", "bare.zip"],
    [
      "attachment; filename=plain.zip; filename*=UTF-8''personal%20plugin.zip",
      "personal plugin.zip",
    ],
    [null, "fallback.zip"],
    ["attachment; filename*=UTF-8''bad%ZZ.zip", "fallback.zip"],
  ])("uses the download filename from %s", async (header, expected) => {
    mocks.fetch.mockResolvedValue(
      new Response(new Blob(["zip"]), {
        headers: header ? { "Content-Disposition": header } : {},
      }),
    );
    let filename: string | undefined;
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      filename = this.download;
    });
    const { result } = renderHook(() =>
      useObservabilityPluginDownload("claude", "fallback.zip"),
    );
    await act(() => result.current.download());
    expect(filename).toBe(expected);
    expect(mocks.fetch).toHaveBeenCalledWith(
      "/rpc/plugins.downloadObservabilityPlugin?platform=claude",
      {},
    );
    expect(result.current.isDownloading).toBe(false);
    expect(revokeObjectURL).not.toHaveBeenCalled();
    vi.runAllTimers();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:plugin");
  });
});
