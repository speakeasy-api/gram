import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { RotateObservabilityCredentialResult } from "@gram/client/models/components/rotateobservabilitycredentialresult.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

type MutateVariables = {
  request: {
    rotateObservabilityCredentialRequestBody: { previousKeyFate: string };
  };
};

const testState = vi.hoisted(() => ({
  isPending: false,
  mutate: vi.fn(),
  invalidateKeys: vi.fn(),
  invalidatePublish: vi.fn(),
}));

vi.mock("@gram/client/react-query/rotateObservabilityCredential", () => ({
  useRotateObservabilityCredentialMutation: () => ({
    isPending: testState.isPending,
    mutate: testState.mutate,
    reset: vi.fn(),
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

const rotated: RotateObservabilityCredentialResult = {
  key: "gram_local_rotated_hooks_key",
  keyPrefix: "gram_local_",
  previousKeyFate: "grace",
  previousKeys: [
    {
      id: "00000000-0000-0000-0000-000000000001",
      name: "plugins-hooks-download-20260713-104500-abcdef",
      keyPrefix: "gram_local_",
    },
  ],
  previousKeysExpireAt: new Date("2026-09-12T00:00:00Z"),
  marketplaceRepublished: false,
  marketplaceUpdateDeferred: false,
};

/** Resolves the pending rotation with `result`, as the real mutation would. */
function resolveRotation(result: RotateObservabilityCredentialResult) {
  const [, options] = testState.mutate.mock.calls[0] as [
    MutateVariables,
    { onSuccess: (data: RotateObservabilityCredentialResult) => Promise<void> },
  ];

  return options.onSuccess(result);
}

function requestedFate(): string {
  const [variables] = testState.mutate.mock.calls[0] as [MutateVariables];

  return variables.request.rotateObservabilityCredentialRequestBody
    .previousKeyFate;
}

beforeEach(() => {
  testState.isPending = false;
  testState.mutate.mockReset();
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
        onDownload={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));
    expect(requestedFate()).toBe("grace");

    await resolveRotation(rotated);

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
  });

  it("reports the chosen fate when the previous key is revoked immediately", async () => {
    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={vi.fn()}
        isDownloading={false}
        onDownload={vi.fn()}
      />,
    );

    fireEvent.click(
      screen.getByRole("radio", {
        name: /Revoke the previous key immediately/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));
    expect(requestedFate()).toBe("revoke_immediately");

    await resolveRotation({
      ...rotated,
      previousKeyFate: "revoke_immediately",
      previousKeysExpireAt: undefined,
    });

    expect(
      await screen.findByText(
        "The previous key was revoked immediately and no longer authenticates.",
      ),
    ).toBeDefined();
  });

  it("warns when a published marketplace still carries the previous credential", async () => {
    render(
      <RotateObservabilityCredentialDialog
        open
        onOpenChange={vi.fn()}
        isDownloading={false}
        onDownload={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Rotate credential" }));
    await resolveRotation({ ...rotated, marketplaceUpdateDeferred: true });

    expect(await screen.findByText(/could not be updated yet/)).toBeDefined();
  });
});
