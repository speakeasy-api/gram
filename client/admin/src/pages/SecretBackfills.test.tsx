import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminSecretBackfillOrganization } from "@gram/admin-client/models/components/adminsecretbackfillorganization";
import type { AdminSecretBackfills } from "@gram/admin-client/models/components/adminsecretbackfills";

import { SecretBackfillsPage } from "./SecretBackfills";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSecretBackfills: vi.fn(),
  runMarketplaceTokenBackfill: vi.fn(),
  runRunnerKeyOrganizationBackfill: vi.fn(),
}));

vi.mock("@/lib/gramAdminClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminClient")>();
  return {
    ...actual,
    secretBackfillsQuery: () => ({
      ...actual.secretBackfillsQuery(),
      queryFn: () => mocks.getSecretBackfills(),
    }),
    runMarketplaceTokenBackfill: mocks.runMarketplaceTokenBackfill,
    runRunnerKeyOrganizationBackfill: mocks.runRunnerKeyOrganizationBackfill,
  };
});

const tokensLeft: AdminSecretBackfillOrganization = {
  organizationId: "org-tokens",
  name: "Tokens Org",
  slug: "tokens-org",
  legacyTokens: 2,
  unattributedConnections: 1,
  pendingRunnerKeys: 0,
};

const runnerKeysLeft: AdminSecretBackfillOrganization = {
  organizationId: "org-runner-keys",
  name: "Runner Keys Org",
  slug: "runner-keys-org",
  legacyTokens: 0,
  unattributedConnections: 0,
  pendingRunnerKeys: 3,
};

function backfills(
  organizations: AdminSecretBackfillOrganization[],
): AdminSecretBackfills {
  const total = (pick: (o: AdminSecretBackfillOrganization) => number) =>
    organizations.reduce((sum, o) => sum + pick(o), 0);
  return {
    marketplaceTokens: {
      attributed: 0,
      moved: 0,
      skipped: 0,
      needsOrganizationKey: 0,
      unattributed: total((o) => o.unattributedConnections),
      legacy: total((o) => o.legacyTokens),
      hashed: 0,
    },
    runnerKeyOrganizations: {
      filled: 0,
      pending: total((o) => o.pendingRunnerKeys),
      missingOrganization: 0,
    },
    organizations,
    platformKeyFingerprint: "fingerprint-fixture",
  };
}

function row(name: string): HTMLElement {
  const link = screen.getByRole("link", { name });
  const tableRow = link.closest("tr");
  if (!tableRow) throw new Error(`no row for ${name}`);
  return tableRow;
}

async function confirmApply() {
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Apply" }));
  return dialog;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getSecretBackfills.mockResolvedValue(
    backfills([runnerKeysLeft, tokensLeft]),
  );
  mocks.runMarketplaceTokenBackfill.mockResolvedValue({
    attributed: 1,
    moved: 2,
    skipped: 0,
    needsOrganizationKey: 0,
    unattributed: 0,
    legacy: 0,
    hashed: 2,
  });
  mocks.runRunnerKeyOrganizationBackfill.mockResolvedValue({
    filled: 3,
    pending: 0,
    missingOrganization: 0,
  });
});
afterEach(cleanup);

describe("Secret backfills by organization", () => {
  it("lists each organization with only the applies it has work for", async () => {
    await renderWithApp(<SecretBackfillsPage />);
    await screen.findByRole("link", { name: "Tokens Org" });

    const tokens = within(row("Tokens Org"));
    expect(
      tokens.getByRole("button", { name: "Apply tokens" }),
    ).not.toHaveProperty("disabled", true);
    expect(
      tokens.getByRole("button", { name: "Apply runner keys" }),
    ).toHaveProperty("disabled", true);

    const runnerKeys = within(row("Runner Keys Org"));
    expect(
      runnerKeys.getByRole("button", { name: "Apply tokens" }),
    ).toHaveProperty("disabled", true);
    expect(
      runnerKeys.getByRole("button", { name: "Apply runner keys" }),
    ).not.toHaveProperty("disabled", true);
  });

  it("applies the marketplace token backfill to one organization", async () => {
    await renderWithApp(<SecretBackfillsPage />);
    await screen.findByRole("link", { name: "Tokens Org" });

    mocks.getSecretBackfills.mockResolvedValue(backfills([runnerKeysLeft]));
    fireEvent.click(
      within(row("Tokens Org")).getByRole("button", { name: "Apply tokens" }),
    );
    const dialog = await confirmApply();
    expect(within(dialog).getByText(/to Tokens Org\?/)).toBeTruthy();

    await waitFor(() =>
      expect(mocks.runMarketplaceTokenBackfill).toHaveBeenCalledWith(
        "fingerprint-fixture",
        "org-tokens",
      ),
    );
    await waitFor(() =>
      expect(screen.queryByRole("link", { name: "Tokens Org" })).toBeNull(),
    );
    expect(
      screen.getByText(/Last run: Tokens Org: Moved 2 tokens/),
    ).toBeTruthy();
  });

  it("applies the runner key backfill to one organization", async () => {
    await renderWithApp(<SecretBackfillsPage />);
    await screen.findByRole("link", { name: "Runner Keys Org" });

    fireEvent.click(
      within(row("Runner Keys Org")).getByRole("button", {
        name: "Apply runner keys",
      }),
    );
    await confirmApply();

    await waitFor(() =>
      expect(mocks.runRunnerKeyOrganizationBackfill).toHaveBeenCalledWith(
        "org-runner-keys",
      ),
    );
  });

  it("applies to every organization from a card", async () => {
    await renderWithApp(<SecretBackfillsPage />);
    await screen.findByRole("link", { name: "Tokens Org" });

    const [marketplace] = screen.getAllByRole("button", {
      name: "Apply to all organizations",
    });
    if (!marketplace) throw new Error("no apply-to-all button");
    fireEvent.click(marketplace);
    const dialog = await confirmApply();
    expect(within(dialog).getByText(/to every organization\?/)).toBeTruthy();

    await waitFor(() =>
      expect(mocks.runMarketplaceTokenBackfill).toHaveBeenCalledWith(
        "fingerprint-fixture",
        undefined,
      ),
    );
  });

  it("filters organizations by name, slug or ID", async () => {
    await renderWithApp(<SecretBackfillsPage />);
    await screen.findByRole("link", { name: "Tokens Org" });

    fireEvent.change(screen.getByLabelText("Filter organizations"), {
      target: { value: "runner-keys" },
    });
    expect(screen.queryByRole("link", { name: "Tokens Org" })).toBeNull();
    expect(screen.getByRole("link", { name: "Runner Keys Org" })).toBeTruthy();
  });
});
