import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentGatewayInstall } from "./AgentGatewayInstall";

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://app.example.test",
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function setup(clipboard: unknown) {
  vi.stubGlobal("navigator", { clipboard });
  render(<AgentGatewayInstall agentID="test-agent" secret="test-agent-key" />);
}

describe("AgentGatewayInstall clipboard failures", () => {
  it.each([
    ["unavailable", undefined],
    [
      "throws synchronously",
      {
        writeText: () => {
          throw new Error("unavailable");
        },
      },
    ],
    ["rejects", { writeText: () => Promise.reject(new Error("denied")) }],
  ])(
    "offers manual copying when the Clipboard API %s",
    async (_, clipboard) => {
      setup(clipboard);
      fireEvent.click(screen.getAllByRole("button", { name: "Copy" })[0]!);
      expect((await screen.findByRole("alert")).textContent).toContain(
        "Select the text and copy it manually",
      );
      expect(screen.getByRole("button", { name: "Copy failed" })).toBeTruthy();
    },
  );

  it("keeps the redeemable command out of the copied follow-up prompt", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    setup({ writeText });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ code: "redeemable-code" }),
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Generate install command" }),
    );
    const prompt = await screen.findByText(
      /I ran the Speakeasy agent install command/,
    );
    fireEvent.click(prompt.parentElement!.querySelector("button")!);
    expect(writeText).toHaveBeenCalledWith(
      expect.stringContaining("Do not read or print credentials"),
    );
    expect(writeText.mock.calls[0]?.[0]).not.toContain("redeemable-code");
    expect(writeText.mock.calls[0]?.[0]).not.toContain("test-agent-key");
  });
});
