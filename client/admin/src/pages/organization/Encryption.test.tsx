import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminOrganizationEncryption } from "@gram/admin-client/models/components/adminorganizationencryption";
import type { AdminOrganizationEncryptionUseCase } from "@gram/admin-client/models/components/adminorganizationencryptionusecase";

import { encryptionRefreshInterval } from "@/lib/organizationEncryption";
import { Encryption } from "@/pages/organization/Encryption";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  enroll: vi.fn(),
  rollback: vi.fn(),
  run: vi.fn(),
  finalize: vi.fn(),
}));

vi.mock("@/lib/gramAdminClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminClient")>();
  return {
    ...actual,
    organizationEncryptionQuery: (organizationID: string) => ({
      queryKey: ["test", "organizationEncryption", organizationID],
      queryFn: () => mocks.get(organizationID),
      retry: false,
    }),
    enrollOrganizationEncryption: mocks.enroll,
    rollbackOrganizationEncryption: mocks.rollback,
    runOrganizationEncryption: mocks.run,
    finalizeOrganizationEncryption: mocks.finalize,
  };
});

const ORG = anOrganization();

function aUseCase(
  overrides: Partial<AdminOrganizationEncryptionUseCase> = {},
): AdminOrganizationEncryptionUseCase {
  return {
    useCase: "oauth-client-secrets",
    state: "platform_key",
    generation: 0,
    waitingValues: 4,
    unreadableValues: 0,
    unattributedValues: 0,
    platformCopies: 4,
    finalizeBlockers: [],
    ...overrides,
  };
}

function anEncryption(
  useCase: AdminOrganizationEncryptionUseCase,
): AdminOrganizationEncryption {
  return {
    organizationId: ORG.id,
    useCases: [useCase],
    minimumDualWriteSeconds: 86400,
    finalizeConfirmation: "destroy platform-key copies",
    runsAvailable: true,
  };
}

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
});

afterEach(() => {
  cleanup();
});

describe("Encryption", () => {
  it("enrolls an organization still on the platform key", async () => {
    mocks.get.mockResolvedValue(anEncryption(aUseCase()));
    mocks.enroll.mockResolvedValue(aUseCase({ state: "dual_write" }));
    await renderWithApp(<Encryption org={ORG} />);

    expect(await screen.findByText("OAuth client secrets")).toBeTruthy();
    expect(screen.getByText(/No key is provisioned yet/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Finalize" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Enroll" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      screen
        .getAllByRole("button", { name: "Enroll" })
        .find((button) => dialog.contains(button))!,
    );

    await waitFor(() =>
      expect(mocks.enroll).toHaveBeenCalledWith(ORG.id, "oauth-client-secrets"),
    );
  });

  it("blocks finalize and lists why while the soak is not over", async () => {
    mocks.get.mockResolvedValue(
      anEncryption(
        aUseCase({
          state: "dual_write",
          generation: 1,
          stateChangedAt: new Date("2026-11-21T00:00:00Z"),
          finalizeBlockers: ["in dual_write for 1h0m0s, needs 24h0m0s"],
        }),
      ),
    );
    await renderWithApp(<Encryption org={ORG} />);

    const finalize = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Finalize",
    });
    expect(finalize.disabled).toBe(true);
    expect(screen.getByText(/in dual_write for 1h0m0s/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Roll back" })).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Run backfill now" }),
    ).toBeTruthy();
  });

  it("shows an interrupted clear as incomplete and resumes it", async () => {
    mocks.get.mockResolvedValue(
      anEncryption(aUseCase({ state: "org_kms", generation: 2 })),
    );
    mocks.run.mockResolvedValue(aUseCase({ state: "org_kms", generation: 2 }));
    await renderWithApp(<Encryption org={ORG} />);

    expect(await screen.findByText("Clearing")).toBeTruthy();
    expect(screen.getByText(/in progress or was interrupted/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Roll back" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Resume clearing" }));
    await waitFor(() =>
      expect(mocks.run).toHaveBeenCalledWith(ORG.id, "oauth-client-secrets"),
    );
  });
});

describe("encryptionRefreshInterval", () => {
  const idle = anEncryption(
    aUseCase({ state: "dual_write", generation: 1, finalizeBlockers: [] }),
  );

  it("keeps refreshing after a run is requested, before the run shows as started", () => {
    const now = Date.parse("2026-11-24T12:00:00Z");
    expect(encryptionRefreshInterval(idle, null, now)).toBe(false);
    expect(encryptionRefreshInterval(idle, now - 30_000, now)).toBeGreaterThan(
      0,
    );
    expect(encryptionRefreshInterval(idle, now - 10 * 60_000, now)).toBe(false);
  });

  it("refreshes while a backfill or clear may be running", () => {
    const now = Date.parse("2026-11-24T12:00:00Z");
    const running = anEncryption(
      aUseCase({
        state: "dual_write",
        generation: 1,
        lastBackfillStartedAt: new Date(now - 60_000),
      }),
    );
    const clearing = anEncryption(
      aUseCase({ state: "org_kms", generation: 2 }),
    );
    expect(encryptionRefreshInterval(running, null, now)).toBeGreaterThan(0);
    expect(encryptionRefreshInterval(clearing, null, now)).toBeGreaterThan(0);
  });
});
