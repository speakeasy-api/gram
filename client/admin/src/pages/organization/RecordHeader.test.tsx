import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { badgeTone } from "@/lib/badgeTone";
import { organizationDashboardUrl } from "@/lib/gramAdminApi";
import { fmtDateShort } from "@/lib/utils";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { RecordHeader } from "./RecordHeader";

beforeEach(() => {
  // UTC and this zone fall on different dates, so a created date read in the
  // reader's zone renders a day early and fails below. CI runs in UTC, where
  // that fault cannot appear at all.
  vi.stubEnv("TZ", "America/Los_Angeles");
});

afterEach(() => {
  vi.unstubAllEnvs();
  cleanup();
});

describe("RecordHeader", () => {
  it("shows the Open in Dashboard secondary action", async () => {
    const org = anOrganization();
    await renderWithApp(<RecordHeader org={org} />);

    const button = screen.getByRole("button", { name: /Open in Dashboard/ });
    const form = button.closest("form");
    expect(form?.getAttribute("method")).toBe("post");
    expect(form?.getAttribute("action")).toBe(organizationDashboardUrl(org.id));
  });

  it("leaves the admin record open in its original tab", async () => {
    await renderWithApp(<RecordHeader org={anOrganization()} />);

    const form = screen
      .getByRole("button", { name: /Open in Dashboard/ })
      .closest("form");
    expect(form?.getAttribute("target")).toBe("_blank");
    expect(form?.getAttribute("rel")).toBe("noopener");
  });

  it("identifies the new dashboard tab for assistive technology", async () => {
    await renderWithApp(<RecordHeader org={anOrganization()} />);

    const button = screen.getByRole("button", { name: /Open in Dashboard/ });
    expect(button.textContent).toBe(
      "Open in Dashboard (opens in the Gram dashboard)",
    );
    expect(button.querySelector(".sr-only")?.textContent).toBe(
      " (opens in the Gram dashboard)",
    );
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

  it("draws only the account type in the neutral tone", async () => {
    const org = anOrganization({
      account_type: "enterprise",
      trial_state: "ending_soon",
      trial_ends_at: "2026-05-06T00:00:00Z",
    });
    await renderWithApp(<RecordHeader org={org} />);

    const badges = screen
      .getByRole("heading", { name: org.name })
      .parentElement?.querySelectorAll('[data-slot="badge"]');
    expect(badges).toHaveLength(1);
    const badge = badges?.item(0);
    expect(badge?.textContent).toBe("enterprise");
    // happy-dom lays nothing out, so the class list is the whole account of a
    // colour a unit test can read. `Trial.test.tsx` reads a tone the same way.
    // Without it the account type can take the failure tone and say the record
    // is in trouble when it is not.
    expect(badge?.className).toContain(badgeTone.neutral);
  });

  it("keeps Open in Dashboard but no lifecycle or trial actions", async () => {
    const org = anOrganization({
      trial_state: "ending_soon",
      trial_ends_at: "2026-05-06T00:00:00Z",
    });
    await renderWithApp(<RecordHeader org={org} />);

    expect(
      screen.getByRole("button", { name: /Open in Dashboard/ }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: `Disable ${org.name}` }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: `Extend trial ${org.name}` }),
    ).toBeNull();
  });

  it("puts no Re-enable action in a disabled organization's header", async () => {
    const org = anOrganization({ disabled_at: "2026-02-01T00:00:00Z" });
    await renderWithApp(<RecordHeader org={org} />);

    expect(
      screen.getByRole("button", { name: /Open in Dashboard/ }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: `Re-enable ${org.name}` }),
    ).toBeNull();
  });
});
