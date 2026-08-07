import { afterEach, describe, expect, it, vi } from "vitest";
import { downloadPluginPackage } from "./downloadPluginPackage";

afterEach(() => {
  vi.restoreAllMocks();
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
});
