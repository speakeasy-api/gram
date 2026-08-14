import { cleanup, fireEvent, screen } from "@testing-library/react";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminOrganization } from "@/lib/gramAdminApi";
import { renderWithApp } from "@/test/harness";

import { PeekPanel } from "./PeekPanel";

const ORG: AdminOrganization = {
  id: "org_placeholder_one",
  name: "Placeholder One",
  slug: "placeholder-one",
  account_type: "pro",
  workos_id: "org_workos_placeholder_identifier",
  whitelisted: true,
  free_trial_started_at: "2026-02-01T00:00:00Z",
  free_trial_ends_at: "2026-05-06T00:00:00Z",
  member_count: 3,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
};

const COPY_CONFIRM_MS = 1500;

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString();
}

function iconOf(button: HTMLElement): SVGElement {
  const icon = button.querySelector("svg");
  if (!icon) throw new Error("the copy control needs an icon");
  return icon;
}

const writeText = vi.fn<(text: string) => Promise<void>>(() =>
  Promise.resolve(),
);

function noop(): void {}

beforeEach(() => {
  writeText.mockClear();
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
    writable: true,
  });
});

afterEach(cleanup);

describe("PeekPanel", () => {
  it("renders the record it was handed, field by field", async () => {
    await renderWithApp(<PeekPanel org={ORG} onClose={noop} />);

    const { workos_id: workosID, free_trial_ends_at: trialEndsAt } = ORG;
    if (!workosID || !trialEndsAt) {
      throw new Error("the record under test needs its optional fields set");
    }

    expect(screen.getByRole("heading", { name: ORG.name })).toBeTruthy();
    expect(screen.getAllByRole("term").map((term) => term.textContent)).toEqual(
      ["Type", "Trial ends", "Members", "Created", "Org id", "WorkOS id"],
    );
    expect(
      screen.getAllByRole("definition").map((value) => value.textContent),
    ).toEqual([
      ORG.account_type,
      shortDate(trialEndsAt),
      String(ORG.member_count),
      shortDate(ORG.created_at),
      ORG.id,
      workosID,
    ]);
  });

  it("renders a dash for the optional fields a record leaves unset", async () => {
    await renderWithApp(
      <PeekPanel
        org={{ ...ORG, workos_id: undefined, free_trial_ends_at: undefined }}
        onClose={noop}
      />,
    );

    const values = screen.getAllByRole("definition");
    expect(values.at(1)?.textContent).toBe("-");
    expect(values.at(-1)?.textContent).toBe("-");
    expect(screen.queryByRole("button", { name: "Copy WorkOS id" })).toBeNull();
  });

  it("copies the whole WorkOS id and confirms with a check", async () => {
    await renderWithApp(<PeekPanel org={ORG} onClose={noop} />);

    const { workos_id: workosID } = ORG;
    if (!workosID) throw new Error("the record under test needs a WorkOS id");

    const button = screen.getByRole("button", { name: "Copy WorkOS id" });
    expect(iconOf(button).classList.contains("lucide-copy")).toBe(true);
    expect(iconOf(button).classList.contains("lucide-check")).toBe(false);

    vi.useFakeTimers();
    try {
      fireEvent.click(button);

      expect(writeText).toHaveBeenCalledWith(workosID);

      const confirmed = screen.getByRole("button", {
        name: "WorkOS id copied",
      });
      expect(iconOf(confirmed).classList.contains("lucide-check")).toBe(true);
      expect(iconOf(confirmed).classList.contains("lucide-copy")).toBe(false);

      act(() => {
        vi.advanceTimersByTime(COPY_CONFIRM_MS);
      });
      expect(
        screen.getByRole("button", { name: "Copy WorkOS id" }),
      ).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  it("copies the org id from its own control", async () => {
    await renderWithApp(<PeekPanel org={ORG} onClose={noop} />);

    fireEvent.click(screen.getByRole("button", { name: "Copy Org id" }));

    expect(writeText).toHaveBeenCalledWith(ORG.id);
  });

  it("takes focus when it opens, so the keyboard reaches it", async () => {
    await renderWithApp(<PeekPanel org={ORG} onClose={noop} />);

    expect(document.activeElement).toBe(
      screen.getByRole("complementary", { name: "Organization peek" }),
    );
  });

  it("closes from its own control", async () => {
    const onClose = vi.fn<() => void>();
    await renderWithApp(<PeekPanel org={ORG} onClose={onClose} />);

    fireEvent.click(screen.getByRole("button", { name: "Close peek" }));

    expect(onClose).toHaveBeenCalled();
  });
});
