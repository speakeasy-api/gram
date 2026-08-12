import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  downloadPluginPackage,
  usePluginPackageDownload,
} from "./downloadPluginPackage";

const { toast } = vi.hoisted(() => ({
  toast: { error: vi.fn() },
}));

vi.mock("sonner", () => ({ toast }));

afterEach(() => {
  vi.restoreAllMocks();
  vi.clearAllMocks();
});

describe("downloadPluginPackage", () => {
  it("requests an Agent Plugin package and revokes its object URL", async () => {
    const download = vi.fn().mockResolvedValue({
      headers: {
        "content-disposition": ['attachment; filename="portable.zip"'],
      },
      result: new Uint8Array([1, 2, 3]),
    });
    const createObjectURL = vi
      .spyOn(URL, "createObjectURL")
      .mockReturnValue("blob:portable");
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL");
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});

    await downloadPluginPackage(
      { plugins: { downloadPluginPackage: download } } as never,
      "plugin-id",
      "agent-plugin",
    );

    expect(download).toHaveBeenCalledWith({
      pluginId: "plugin-id",
      platform: "agent-plugin",
    });
    expect(createObjectURL).toHaveBeenCalledOnce();
    expect(click).toHaveBeenCalledOnce();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:portable");
  });

  it("guards the in-flight lifecycle and reports failures", async () => {
    let rejectDownload!: (error: Error) => void;
    const request = new Promise<never>((_resolve, reject) => {
      rejectDownload = reject;
    });
    const download = vi.fn(() => request);
    const onMenuOpenChange = vi.fn<(open: boolean) => void>();
    const client = { plugins: { downloadPluginPackage: download } } as never;
    const { result } = renderHook(() =>
      usePluginPackageDownload(client, "plugin-id", onMenuOpenChange),
    );

    let firstDownload!: Promise<void>;
    await act(async () => {
      firstDownload = result.current.download("agent-plugin");
      await result.current.download("codex");
    });

    expect(download).toHaveBeenCalledOnce();
    expect(onMenuOpenChange).toHaveBeenCalledOnce();
    expect(onMenuOpenChange).toHaveBeenCalledWith(false);
    expect(result.current.isDownloading).toBe(true);

    await act(async () => {
      rejectDownload(new Error("failed"));
      await firstDownload;
    });

    expect(toast.error).toHaveBeenCalledWith(
      "Failed to download plugin package",
    );
    expect(result.current.isDownloading).toBe(false);
  });

  it("isolates in-flight state when the plugin changes", async () => {
    let rejectFirst!: (error: Error) => void;
    let rejectSecond!: (error: Error) => void;
    const firstRequest = new Promise<never>((_resolve, reject) => {
      rejectFirst = reject;
    });
    const secondRequest = new Promise<never>((_resolve, reject) => {
      rejectSecond = reject;
    });
    const download = vi
      .fn()
      .mockReturnValueOnce(firstRequest)
      .mockReturnValueOnce(secondRequest);
    const client = { plugins: { downloadPluginPackage: download } } as never;
    const onMenuOpenChange = vi.fn<(open: boolean) => void>();
    const { result, rerender } = renderHook(
      ({ pluginId }) =>
        usePluginPackageDownload(client, pluginId, onMenuOpenChange),
      { initialProps: { pluginId: "first-plugin" } },
    );

    let firstDownload!: Promise<void>;
    await act(async () => {
      firstDownload = result.current.download("agent-plugin");
    });
    expect(result.current.isDownloading).toBe(true);

    rerender({ pluginId: "second-plugin" });
    expect(result.current.isDownloading).toBe(false);

    let secondDownload!: Promise<void>;
    await act(async () => {
      secondDownload = result.current.download("agent-plugin");
    });
    expect(download).toHaveBeenCalledTimes(2);
    expect(result.current.isDownloading).toBe(true);

    await act(async () => {
      rejectFirst(new Error("first failed"));
      await firstDownload;
    });
    expect(result.current.isDownloading).toBe(true);

    await act(async () => {
      rejectSecond(new Error("second failed"));
      await secondDownload;
    });
    expect(result.current.isDownloading).toBe(false);
  });
});
