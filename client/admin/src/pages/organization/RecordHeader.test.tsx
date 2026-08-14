import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { badgeTone } from "@/lib/badgeTone";
import type { TrialState } from "@/lib/gramAdminApi";
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

  it("leaves the record open behind Open in Gram, and names no referrer", async () => {
    await renderWithApp(<RecordHeader org={anOrganization()} />);

    const link = screen.getByRole("link", { name: /Open in Gram/ });
    // The operator is reading a record and following a link out of it. Taking
    // the tab loses the record they are working through.
    expect(link.getAttribute("target")).toBe("_blank");
    // The admin address carries the organization the operator is looking at.
    // `noopener` is implied for `_blank`; suppressing the referrer is not.
    expect(link.getAttribute("rel")).toContain("noreferrer");
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

  it("ends the account type with the separator, so a wrapped line never opens with one", async () => {
    const org = anOrganization({ account_type: "enterprise" });
    await renderWithApp(<RecordHeader org={org} />);

    const fact = screen.getByText(/Created/).closest("p")?.firstElementChild;
    expect(fact?.textContent).toBe("enterprise");
    // Inside the fact it follows, not between the two: the line wraps, and a
    // separator that starts the next fact would open the second line with it.
    expect(fact?.querySelector('[aria-hidden="true"]')).toBeTruthy();
  });

  it("draws the account type in the neutral tone", async () => {
    // No trial, so the only badge beside the name is this one.
    const org = anOrganization({
      account_type: "enterprise",
      trial_state: "none",
    });
    await renderWithApp(<RecordHeader org={org} />);

    const badge = screen
      .getByRole("heading", { name: org.name })
      .parentElement?.querySelector('[data-slot="badge"]');
    expect(badge?.textContent).toBe("enterprise");
    // happy-dom lays nothing out, so the class list is the whole account of a
    // colour a unit test can read. `Trial.test.tsx` reads a tone the same way.
    // Without it the account type can take the failure tone and say the record
    // is in trouble when it is not.
    expect(badge?.className).toContain(badgeTone.neutral);
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

  it("shows no trial mark on a record with no trial", async () => {
    // `Trial` renders a bare dash for `none`, which is right in a table cell
    // and reads as a stray hyphen beside a record name.
    await renderWithApp(
      <RecordHeader org={anOrganization({ trial_state: "none" })} />,
    );

    expect(screen.queryByText("-")).toBeNull();
    expect(screen.queryByText(TRIAL_LABELS.none)).toBeNull();
  });

  it("shows no trial mark on a record the server sends no trial state for", async () => {
    // A gate written as `!== "none"` alone draws the dash here.
    await renderWithApp(<RecordHeader org={anOrganization()} />);

    expect(screen.queryByText("-")).toBeNull();
    expect(screen.queryByText(TRIAL_LABELS.none)).toBeNull();
  });

  it("still shows the dash when the server sends a state the client does not know", async () => {
    // `Trial` renders the same bare dash for `none` and for an unrecognised
    // value. Only `none` is "no trial"; an unrecognised state says this build
    // is behind the server, and hiding it would hide that.
    await renderWithApp(
      <RecordHeader
        org={anOrganization({ trial_state: "paused" as TrialState })}
      />,
    );

    expect(screen.getByText("Trial state not recognised")).toBeTruthy();
  });

  it("carries the record's lifecycle action and not the trial's", async () => {
    // A live trial, so the trial action would be offered if this bar drew it.
    // It belongs in the callout, beside the deadline it acts on.
    await renderWithApp(
      <RecordHeader
        org={anOrganization({
          trial_state: "ending_soon",
          trial_ends_at: "2026-05-06T00:00:00Z",
        })}
      />,
    );

    const labels = screen.queryAllByRole("button").map((b) => b.textContent);
    expect(labels).toContain("Disable");
    expect(labels).not.toContain("Extend trial");
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
