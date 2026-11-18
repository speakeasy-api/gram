import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminOrganizationHooksRollout } from "@/lib/gramAdminApi";
import { HooksRolloutOverride } from "@/pages/organization/HooksRolloutOverride";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getOrganizationHooksRollout: vi.fn(),
  setOrganizationHooksRollout: vi.fn(),
  clearOrganizationHooksRollout: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getOrganizationHooksRollout: mocks.getOrganizationHooksRollout,
    setOrganizationHooksRollout: mocks.setOrganizationHooksRollout,
    clearOrganizationHooksRollout: mocks.clearOrganizationHooksRollout,
  };
});

const ORG = anOrganization();

const LEGACY: AdminOrganizationHooksRollout = {
  organization_id: ORG.id,
  current_version: 46,
  source: "legacy_flag",
};

const OVERRIDDEN: AdminOrganizationHooksRollout = {
  organization_id: ORG.id,
  current_version: 46,
  source: "organization",
  override: {
    version: 44,
    set_by: "operator@example.com",
    set_at: "2026-01-01T00:00:00Z",
  },
  default_pin: {
    version: 46,
    set_by: "operator@example.com",
    set_at: "2026-01-02T00:00:00Z",
  },
  effective_version: 44,
  eligible: false,
};

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("HooksRolloutOverride", () => {
  it("sets an override for an organization the legacy flag decides", async () => {
    mocks.getOrganizationHooksRollout.mockResolvedValue(LEGACY);
    const updated: AdminOrganizationHooksRollout = {
      ...LEGACY,
      source: "organization",
      override: OVERRIDDEN.override,
      effective_version: 46,
      eligible: true,
    };
    mocks.setOrganizationHooksRollout.mockImplementation(async () => {
      mocks.getOrganizationHooksRollout.mockResolvedValue(updated);
      return updated;
    });
    await renderWithApp(<HooksRolloutOverride org={ORG} />);

    expect(await screen.findByText("Unknown")).toBeTruthy();
    expect(
      screen.getByText(/the legacy hooks-rollout PostHog flag decides/),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Clear override" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Set override" }));
    await waitFor(() =>
      expect(mocks.setOrganizationHooksRollout).toHaveBeenCalledWith({
        organizationID: ORG.id,
        version: 46,
      }),
    );
    expect(await screen.findByText("Cleared for version 46")).toBeTruthy();
  });

  it("clears an existing override", async () => {
    mocks.getOrganizationHooksRollout.mockResolvedValue(OVERRIDDEN);
    const updated: AdminOrganizationHooksRollout = {
      ...OVERRIDDEN,
      source: "default",
      override: undefined,
      effective_version: 46,
      eligible: true,
    };
    mocks.clearOrganizationHooksRollout.mockImplementation(async () => {
      mocks.getOrganizationHooksRollout.mockResolvedValue(updated);
      return updated;
    });
    await renderWithApp(<HooksRolloutOverride org={ORG} />);

    expect(await screen.findByText("Held below version 46")).toBeTruthy();
    expect(
      screen.getByText("Pinned by its own override to hooks version 44.", {
        exact: false,
      }),
    ).toBeTruthy();
    expect(
      (screen.getByLabelText("Hooks version override") as HTMLInputElement)
        .value,
    ).toBe("44");

    fireEvent.click(screen.getByRole("button", { name: "Clear override" }));
    await waitFor(() =>
      expect(mocks.clearOrganizationHooksRollout).toHaveBeenCalledWith(ORG.id),
    );
    expect(
      await screen.findByText("Follows the default pin, hooks version 46.", {
        exact: false,
      }),
    ).toBeTruthy();
  });

  it("offers no override for a canary organization", async () => {
    mocks.getOrganizationHooksRollout.mockResolvedValue({
      organization_id: ORG.id,
      current_version: 46,
      source: "canary",
      eligible: true,
    } satisfies AdminOrganizationHooksRollout);
    await renderWithApp(<HooksRolloutOverride org={ORG} />);

    expect(await screen.findByText(/Canary organization/)).toBeTruthy();
    expect(screen.queryByLabelText("Hooks version override")).toBeNull();
  });
});
