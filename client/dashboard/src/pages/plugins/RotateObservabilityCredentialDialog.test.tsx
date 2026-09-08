import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  RotateObservabilityCredentialDialog,
  type RotateObservabilityCredentialResult,
} from "./RotateObservabilityCredentialDialog";

const state = vi.hoisted(() => ({
  fetch: vi.fn(),
  invalidateKeys: vi.fn(),
  invalidatePublish: vi.fn(),
}));

vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: state.fetch }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("@gram/client/react-query/listAPIKeys", () => ({
  invalidateAllListAPIKeys: state.invalidateKeys,
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  invalidateAllPublishStatus: state.invalidatePublish,
}));

vi.mock("@/components/code", () => ({
  CodeBlock: ({
    children,
    copyLabel = "code",
  }: {
    children: string;
    copyLabel?: string;
  }) => (
    <div>
      <button aria-label={`Copy ${copyLabel}`} />
      <pre>{children}</pre>
    </div>
  ),
}));

const rotated: RotateObservabilityCredentialResult = {
  key: "gram_local_rotated_hooks_key",
  key_prefix: "gram_local_",
  previous_key_fate: "grace",
  previous_keys: [
    {
      id: "00000000-0000-0000-0000-000000000001",
      name: "plugins-hooks-download-20260713-104500-abcdef",
      key_prefix: "gram_local_",
    },
  ],
  previous_keys_expire_at: "2026-09-12T00:00:00Z",
  marketplace_republished: false,
  marketplace_update_deferred: false,
};

beforeEach(() => {
  state.fetch.mockReset();
  state.invalidateKeys.mockReset();
  state.invalidatePublish.mockReset();
});

afterEach(cleanup);

describe("RotateObservabilityCredentialDialog", () => {
  it("keeps a rotated key visible until explicit acknowledgement", async () => {
    state.fetch.mockResolvedValue({
      ok: true,
      json: async () => rotated,
    });
    const onOpenChange = vi.fn<(open: boolean) => void>(() => {});
    const onDownload = vi.fn();

    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={onOpenChange}
        isDownloading={false}
        onDownload={onDownload}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));

    expect(await screen.findByText(rotated.key!)).toBeDefined();
    expect(
      screen.getByRole("button", { name: "Copy observability credential" }),
    ).toBeDefined();
    expect(state.fetch).toHaveBeenCalledWith(
      "/rpc/plugins.rotateObservabilityCredential",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ previous_key_fate: "grace" }),
      }),
    );
    expect(state.invalidateKeys).toHaveBeenCalledOnce();
    expect(state.invalidatePublish).toHaveBeenCalledOnce();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onOpenChange).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "I have saved the key" }),
    );
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("revokes immediately when that fate is selected", async () => {
    state.fetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        ...rotated,
        previous_key_fate: "revoke_immediately",
        previous_keys_expire_at: undefined,
      }),
    });

    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={() => {}}
        isDownloading={false}
        onDownload={() => {}}
      />,
    );

    fireEvent.click(
      screen.getByRole("radio", {
        name: /Revoke the previous key immediately/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));

    await waitFor(() => {
      expect(state.fetch).toHaveBeenCalledWith(
        "/rpc/plugins.rotateObservabilityCredential",
        expect.objectContaining({
          body: JSON.stringify({ previous_key_fate: "revoke_immediately" }),
        }),
      );
    });
    expect(
      await screen.findByText(
        "The previous key was revoked immediately and no longer authenticates.",
      ),
    ).toBeDefined();
  });
});
