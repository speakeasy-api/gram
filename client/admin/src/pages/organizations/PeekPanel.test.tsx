import { cleanup, fireEvent, screen } from "@testing-library/react";
import { act, useState, type JSX } from "react";
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
  // The stale pair, dated apart from the real trial on purpose. A panel back
  // on `free_trial_ends_at` then shows the wrong date rather than the right
  // one by coincidence.
  free_trial_started_at: "2026-02-01T00:00:00Z",
  free_trial_ends_at: "2026-11-12T00:00:00Z",
  trial_state: "running",
  trial_ends_at: "2026-05-06T00:00:00Z",
  member_count: 3,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
};

const OTHER_ORG: AdminOrganization = {
  ...ORG,
  id: "org_placeholder_two",
  name: "Placeholder Two",
};

const COPY_CONFIRM_MS = 1500;

// Peek swaps the record under a mounted panel, so a test does too rather than
// re-rendering, which would reset the state under test on its own.
function Peeking(): JSX.Element {
  const [org, setOrg] = useState(ORG);
  return (
    <>
      <button onClick={() => setOrg(OTHER_ORG)}>next record</button>
      <PeekPanel org={org} onClose={noop} />
    </>
  );
}

// UTC, because the panel reads these dates in UTC. See `utils.test.ts`: an
// API date is midnight UTC, and rendering it in the reader's own zone names the
// day before west of Greenwich.
function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { timeZone: "UTC" });
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

    const { workos_id: workosID, trial_ends_at: trialEndsAt } = ORG;
    if (!workosID || !trialEndsAt) {
      throw new Error("the record under test needs its optional fields set");
    }

    expect(screen.getByRole("heading", { name: ORG.name })).toBeTruthy();
    expect(screen.getAllByRole("term").map((term) => term.textContent)).toEqual(
      ["Type", "Trial", "Members", "Created", "Org id", "WorkOS id"],
    );
    expect(
      screen.getAllByRole("definition").map((value) => value.textContent),
    ).toEqual([
      ORG.account_type,
      // The same state-then-date reading as the row, out of the same helper.
      `Running ends ${shortDate(trialEndsAt)}`,
      String(ORG.member_count),
      shortDate(ORG.created_at),
      ORG.id,
      workosID,
    ]);
  });

  it("renders a dash for the optional fields a record leaves unset", async () => {
    await renderWithApp(
      <PeekPanel
        org={{
          ...ORG,
          workos_id: undefined,
          // The stale pair stays set. A panel that never trialled has to read
          // as a dash even while the defaulted column still dates it.
          trial_state: "none",
          trial_ends_at: undefined,
        }}
        onClose={noop}
      />,
    );

    const values = screen.getAllByRole("definition");
    // A dash for the eye, and "No trial" for a reader that would otherwise be
    // told nothing at all. `Trial.test.tsx` holds the two halves apart.
    expect(values.at(1)?.textContent).toBe("-No trial");
    expect(values.at(1)?.querySelector('[data-slot="badge"]')).toBeNull();
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

      // The confirmation waits on the clipboard promise, so flush microtasks.
      // Fake timers do not stub those.
      await act(async () => {});

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

  it("drops the confirmation when peek moves to another record", async () => {
    await renderWithApp(<Peeking />);

    fireEvent.click(screen.getByRole("button", { name: "Copy Org id" }));
    await act(async () => {});
    expect(screen.getByRole("button", { name: "Org id copied" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "next record" }));

    const control = screen.getByRole("button", { name: "Copy Org id" });
    expect(iconOf(control).classList.contains("lucide-copy")).toBe(true);
  });

  it("keeps a write that lands after the move off the new record", async () => {
    let land = noop;
    writeText.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          land = () => resolve();
        }),
    );
    await renderWithApp(<Peeking />);

    fireEvent.click(screen.getByRole("button", { name: "Copy Org id" }));
    fireEvent.click(screen.getByRole("button", { name: "next record" }));

    await act(async () => {
      land();
    });

    const control = screen.getByRole("button", { name: "Copy Org id" });
    expect(iconOf(control).classList.contains("lucide-copy")).toBe(true);
  });

  it("leaves the control inert where the browser gives no clipboard", async () => {
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      configurable: true,
      writable: true,
    });
    await renderWithApp(<PeekPanel org={ORG} onClose={noop} />);

    // React reports a throw from a handler as an uncaught error rather than
    // letting fireEvent raise it, so the listener is what the assertion reads.
    const uncaught = vi.fn<(event: ErrorEvent) => void>();
    window.addEventListener("error", uncaught);
    try {
      fireEvent.click(screen.getByRole("button", { name: "Copy Org id" }));
    } finally {
      window.removeEventListener("error", uncaught);
    }

    expect(uncaught).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Copy Org id" })).toBeTruthy();
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
