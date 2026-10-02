import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type { RotateObservabilityCredentialResult } from "@gram/client/models/components/rotateobservabilitycredentialresult.js";
import type { ObservabilityDownloadPlatform } from "./observability-platforms";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

type MutateVariables = {
  request: {
    rotateObservabilityCredentialRequestBody: { previousKeyFate: string };
  };
};

const testState = vi.hoisted(() => ({
  isPending: false,
  mutate: vi.fn(),
  reset: vi.fn(),
  invalidateKeys: vi.fn(),
  invalidatePublish: vi.fn(),
}));

vi.mock("@gram/client/react-query/rotateObservabilityCredential", () => ({
  useRotateObservabilityCredentialMutation: () => ({
    isPending: testState.isPending,
    mutate: testState.mutate,
    reset: testState.reset,
  }),
}));

vi.mock("@gram/client/react-query/listAPIKeys", () => ({
  invalidateAllListAPIKeys: testState.invalidateKeys,
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  invalidateAllPublishStatus: testState.invalidatePublish,
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

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

import { RotateObservabilityCredentialDialog } from "./RotateObservabilityCredentialDialog";
import { rotationErrorCopy } from "./rotation-error-copy";

const rotated: RotateObservabilityCredentialResult = {
  key: "gram_local_rotated_hooks_key",
  keyPrefix: "gram_local_",
  previousKeyFate: "grace",
  previousKeys: [
    {
      id: "00000000-0000-0000-0000-000000000001",
      name: "plugins-hooks-download-20260713-104500-abcdef",
      keyPrefix: "gram_local_",
      expiresAt: new Date("2026-09-12T00:00:00Z"),
    },
  ],
  previousKeysExpireAt: new Date("2026-09-12T00:00:00Z"),
  previousKeysRetired: true,
  marketplaceRepublished: false,
  marketplaceUpdateDeferred: false,
};

/** Resolves the pending rotation with `result`, as the real mutation would. */
function resolveRotation(result: RotateObservabilityCredentialResult): void {
  const [, options] = testState.mutate.mock.calls[0] as [
    MutateVariables,
    { onSuccess: (data: RotateObservabilityCredentialResult) => void },
  ];

  act(() => {
    options.onSuccess(result);
  });
}

function requestedFate(): string {
  const [variables] = testState.mutate.mock.calls[0] as [MutateVariables];

  return variables.request.rotateObservabilityCredentialRequestBody
    .previousKeyFate;
}

/** Minimal stand-in for an SDK error carrying an HTTP status. */
function gramErrorWithStatus(status: number): unknown {
  return { statusCode: status, message: "server wording that must not leak" };
}

// Typed no-ops: the props are void-returning, and a bare vi.fn() is not.
const noopOpenChange: (open: boolean) => void = () => {};
const noopDownload: (
  platform: ObservabilityDownloadPlatform,
) => void = () => {};

beforeEach(() => {
  testState.isPending = false;
  testState.mutate.mockReset();
  testState.reset.mockReset();
  testState.invalidateKeys.mockReset();
  testState.invalidatePublish.mockReset();
});

afterEach(cleanup);

describe("RotateObservabilityCredentialDialog", () => {
  it("keeps a rotated key visible until explicit acknowledgement", async () => {
    const onOpenChange = vi.fn<(open: boolean) => void>();

    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={onOpenChange}
        isDownloading={false}
        onDownload={noopDownload}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));
    expect(requestedFate()).toBe("grace");

    resolveRotation(rotated);

    expect(await screen.findByText(rotated.key)).toBeDefined();
    expect(
      screen.getByRole("button", { name: "Copy observability credential" }),
    ).toBeDefined();
    expect(testState.invalidateKeys).toHaveBeenCalledOnce();
    expect(testState.invalidatePublish).toHaveBeenCalledOnce();

    // The plaintext key is shown once, so Escape must not dismiss it.
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onOpenChange).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "I have saved the key" }),
    );
    expect(onOpenChange).toHaveBeenCalledWith(false);
    // The mutation cache holds the same plaintext key, so dismissing the
    // one-time reveal has to clear it too.
    expect(testState.reset).toHaveBeenCalled();
  });

  it("reports the chosen fate when the previous key is revoked immediately", async () => {
    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={noopOpenChange}
        isDownloading={false}
        onDownload={noopDownload}
      />,
    );

    fireEvent.click(
      screen.getByRole("radio", {
        name: /Stop the current key from working now/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));
    expect(requestedFate()).toBe("revoke_immediately");

    resolveRotation({
      ...rotated,
      previousKeyFate: "revoke_immediately",
      previousKeys: [{ ...rotated.previousKeys[0]!, expiresAt: undefined }],
      previousKeysExpireAt: undefined,
    });

    // Daniel's ask: say what it means for the customer, not that a key was
    // "revoked".
    expect(
      await screen.findByText(
        /stopped working\. Installations still using it cannot send observability data/,
      ),
    ).toBeDefined();
  });

  it("warns when a published marketplace still carries the previous credential", async () => {
    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={noopOpenChange}
        isDownloading={false}
        onDownload={noopDownload}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));
    resolveRotation({ ...rotated, marketplaceUpdateDeferred: true });

    // The copy must not name a cause: a deferred update can equally mean the
    // organization is not cleared or that publishing is unavailable.
    const deferred = await screen.findByText(
      /Your marketplace package was not updated/,
    );
    expect(deferred.textContent).not.toMatch(/not cleared|approved|latest/);
  });

  it("reports each previous key's real deadline rather than this rotation's", async () => {
    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={noopOpenChange}
        isDownloading={false}
        onDownload={noopDownload}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));

    // A key already inside a shorter window keeps its earlier deadline, so the
    // dialog must not promise every key lasts until the rotation's own.
    const earlier = new Date("2026-09-08T00:00:00Z");
    const latest = new Date("2026-09-12T00:00:00Z");
    resolveRotation({
      ...rotated,
      previousKeys: [
        { ...rotated.previousKeys[0]!, expiresAt: latest },
        {
          id: "00000000-0000-0000-0000-000000000002",
          name: "plugins-hooks-20260701-090000-abc123",
          keyPrefix: "gram_local_",
          expiresAt: earlier,
        },
      ],
    });

    const copy = await screen.findByText(/at the latest/);
    expect(copy.textContent).toContain(latest.toLocaleString());
    expect(copy.textContent).toContain(earlier.toLocaleString());
  });

  it("flags a rotation that published a key but left the previous ones live", async () => {
    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={noopOpenChange}
        isDownloading={false}
        onDownload={noopDownload}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));
    resolveRotation({
      ...rotated,
      previousKeys: [],
      previousKeysExpireAt: undefined,
      previousKeysRetired: false,
    });

    expect(
      await screen.findByText(/previous keys were left untouched/),
    ).toBeDefined();
    // The server reports no previous keys when it retired none, so the ordinary
    // fate alert would claim none were in use — the opposite of the warning.
    expect(
      screen.queryByText(/No previous observability keys were in use/),
    ).toBe(null);
  });

  it("turns a rotation failure into the next step, without server wording", () => {
    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={noopOpenChange}
        isDownloading={false}
        onDownload={noopDownload}
      />,
    );

    expect(rotationErrorCopy(gramErrorWithStatus(412))).toBe(
      "Publish your marketplace package before creating a new observability key. Nothing was changed.",
    );
    expect(rotationErrorCopy(gramErrorWithStatus(400))).toMatch(
      /Turn on the Observability plugin/,
    );
    // A failure that changed nothing has to read differently from a partial
    // rotation, and never echo the server's own wording.
    expect(rotationErrorCopy(new Error("previous key fate"))).toBe(
      "The new key could not be created. Nothing was changed — try again.",
    );
  });
});
