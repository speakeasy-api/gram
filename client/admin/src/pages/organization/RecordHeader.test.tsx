import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TRIAL_LABELS } from "@/lib/trialLabels";
import { fmtDateShort } from "@/lib/utils";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { RecordHeader } from "./RecordHeader";

const mocks = vi.hoisted(() => ({
  impersonationUrl: vi.fn<(slug: string) => string | undefined>(),
}));

// The one dependency a test has to move. `__GRAM_APP_URL__` is substituted at
// build time, so the not-configured case cannot be reached any other way.
vi.mock("@/lib/impersonation", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/impersonation")>();
  return { ...actual, impersonationUrl: mocks.impersonationUrl };
});

const APP_LINK = "https://app.example.test/rpc/auth.login?redirect=%2Ftest-org";

beforeEach(() => {
  // UTC and this zone fall on different dates, so a created date read in the
  // reader's zone renders a day early and fails below. CI runs in UTC, where
  // that fault cannot appear at all.
  vi.stubEnv("TZ", "America/Los_Angeles");
  mocks.impersonationUrl.mockReset();
  mocks.impersonationUrl.mockReturnValue(APP_LINK);
});

afterEach(() => {
  vi.unstubAllEnvs();
  cleanup();
});

describe("RecordHeader", () => {
  it("omits Open in Gram when impersonationUrl returns undefined", async () => {
    // Undefined whenever __GRAM_APP_URL__ is unset at build time. Rendering the
    // anchor anyway gives the operator a link that takes focus and goes
    // nowhere, which is worse than no link.
    mocks.impersonationUrl.mockReturnValue(undefined);

    await renderWithApp(<RecordHeader org={anOrganization()} />);

    // By its words, not only by its role. An anchor rendered with no `href` is
    // the shape this mistake takes, and such an anchor has no link role at all:
    // an absence asserted by role alone passes while the dead control is on
    // screen.
    expect(screen.queryByText("Open in Gram")).toBeNull();
    expect(screen.queryByRole("link", { name: /Open in Gram/ })).toBeNull();
  });

  it("shows Open in Gram when an app URL is configured", async () => {
    await renderWithApp(<RecordHeader org={anOrganization()} />);

    const link = screen.getByRole("link", { name: /Open in Gram/ });
    expect(link.getAttribute("href")).toBe(APP_LINK);
  });

  it("impersonates through the record's slug, not its id", async () => {
    // The server reads the first path segment of `redirect` back as the
    // organization, so an id there lands the operator nowhere.
    const org = anOrganization();
    await renderWithApp(<RecordHeader org={org} />);

    expect(mocks.impersonationUrl).toHaveBeenCalledWith(org.slug);
  });

  it("shows the account type and created date on the meta line", async () => {
    const org = anOrganization({
      account_type: "enterprise",
      created_at: "2026-01-15T00:00:00Z",
    });
    await renderWithApp(<RecordHeader org={org} />);

    const meta = screen.getByText(/Created/).closest("p");
    expect(meta?.textContent).toContain("enterprise");
    expect(meta?.textContent).toContain(fmtDateShort(org.created_at));
  });

  it("names the record and its trial state", async () => {
    const org = anOrganization({
      trial_state: "ending_soon",
      trial_ends_at: "2026-05-06T00:00:00Z",
    });
    await renderWithApp(<RecordHeader org={org} />);

    expect(screen.getByRole("heading", { name: org.name })).toBeTruthy();
    expect(screen.getByText(TRIAL_LABELS.ending_soon)).toBeTruthy();
  });

  it("offers Re-enable rather than Disable for a disabled organization", async () => {
    const org = anOrganization({ disabled_at: "2026-02-01T00:00:00Z" });
    await renderWithApp(<RecordHeader org={org} />);

    expect(
      screen.getByRole("button", { name: `Re-enable ${org.name}` }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: `Disable ${org.name}` }),
    ).toBeNull();
  });
});
