import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { anOrganization } from "@/test/fixtures";

import { TrialFacts, TrialSummary } from "./TrialFacts";

const END = "2026-05-06T01:01:00Z";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

describe("TrialFacts", () => {
  it.each([
    ["running", "Running"],
    ["ending_soon", "Ending soon"],
  ] as const)("shows the live %s trial facts", (trialState, stateLabel) => {
    vi.stubEnv("TZ", "America/Los_Angeles");
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:30Z");

    render(
      <TrialFacts
        org={anOrganization({
          trial_state: trialState,
          trial_tier: "enterprise",
          trial_ends_at: END,
        })}
      />,
    );

    expect(
      screen.getByText("Trial state").nextElementSibling?.textContent,
    ).toBe(stateLabel);
    expect(screen.getByText("Trial tier").nextElementSibling?.textContent).toBe(
      "Enterprise",
    );
    expect(screen.getByText("End date").nextElementSibling?.textContent).toBe(
      new Date(END).toLocaleDateString(),
    );
    expect(screen.queryByText("Remaining")).toBeNull();
  });

  it("keeps No trial visible", () => {
    render(<TrialFacts org={anOrganization({ trial_state: "none" })} />);

    expect(screen.getByText("No trial")).toBeTruthy();
  });

  it("does not expose an unknown stored tier", () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:30Z");

    render(
      <TrialFacts
        org={anOrganization({
          trial_state: "running",
          trial_tier: "future_internal_tier",
          trial_ends_at: END,
        })}
      />,
    );

    expect(screen.getByText("Trial tier").nextElementSibling?.textContent).toBe(
      "Unknown",
    );
    expect(screen.queryByText("future_internal_tier")).toBeNull();
  });

  it("updates the summary on end-relative boundaries and cleans up its timer", () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:30.500Z");
    const view = render(
      <TrialSummary
        org={anOrganization({
          trial_state: "running",
          trial_tier: "enterprise",
          trial_ends_at: "2026-05-06T01:01:45Z",
        })}
      />,
    );

    expect(screen.getByText("1 hour 2 minutes left")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(14_500);
    });
    expect(screen.getByText("1 hour 1 minute left")).toBeTruthy();
    expect(vi.getTimerCount()).toBe(1);

    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("updates Details to expired at the exact end", () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:30Z");
    render(
      <TrialFacts
        org={anOrganization({
          trial_state: "running",
          trial_ends_at: "2026-05-06T00:01:45Z",
        })}
      />,
    );

    act(() => {
      vi.advanceTimersByTime(75_000);
    });

    expect(
      screen.getByText("Trial state").nextElementSibling?.textContent,
    ).toBe("Expired");
  });

  it("switches the summary to ended at the exact end", () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:30Z");
    const end = "2026-05-06T00:01:45Z";
    render(
      <TrialSummary
        org={anOrganization({
          trial_state: "running",
          trial_tier: "enterprise",
          trial_ends_at: end,
        })}
      />,
    );

    act(() => {
      vi.advanceTimersByTime(75_000);
    });

    expect(screen.getByText("Trial ended")).toBeTruthy();
    expect(screen.getByText(/End date/).textContent).toContain(
      new Date(end).toLocaleDateString(),
    );
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each([undefined, "not-a-date"])(
    "shows an unknown live end for %s",
    (end) => {
      vi.useFakeTimers();
      render(
        <TrialSummary
          org={anOrganization({ trial_state: "running", trial_ends_at: end })}
        />,
      );

      expect(screen.getByText("Trial status unknown")).toBeTruthy();
      expect(screen.getByText(/End date/).textContent).toContain("Unknown");
      expect(vi.getTimerCount()).toBe(0);
    },
  );

  it("labels an unknown future trial state safely", () => {
    render(
      <TrialFacts
        org={anOrganization({ trial_state: "future_state" as "running" })}
      />,
    );

    expect(
      screen.getByText("Trial state").nextElementSibling?.textContent,
    ).toBe("Unknown");
  });

  it.each([
    ["converted", "Converted", "Conversion date"],
    ["demoted", "Demoted", "Demotion date"],
    ["expired", "Expired", undefined],
  ] as const)(
    "shows completed %s trial history without a countdown",
    (trialState, stateLabel, lifecycleLabel) => {
      const end = "2026-04-30T23:00:00Z";
      const converted = "2026-05-01T23:00:00Z";
      const demoted = "2026-05-02T23:00:00Z";
      render(
        <TrialFacts
          org={anOrganization({
            trial_state: trialState,
            trial_tier: "enterprise",
            trial_ends_at: end,
            trial_converted_at: converted,
            trial_demoted_at: demoted,
          })}
        />,
      );

      expect(
        screen.getByText("Trial state").nextElementSibling?.textContent,
      ).toBe(stateLabel);
      expect(
        screen.getByText("Trial tier").nextElementSibling?.textContent,
      ).toBe("Enterprise");
      expect(screen.getByText("End date").nextElementSibling?.textContent).toBe(
        new Date(end).toLocaleDateString(),
      );

      if (lifecycleLabel) {
        const lifecycleDate = trialState === "converted" ? converted : demoted;
        expect(
          screen.getByText(lifecycleLabel).nextElementSibling?.textContent,
        ).toBe(new Date(lifecycleDate).toLocaleDateString());
      }
      expect(screen.queryByText("Remaining")).toBeNull();
      expect(screen.queryByText("Conversion date") !== null).toBe(
        trialState === "converted",
      );
      expect(screen.queryByText("Demotion date") !== null).toBe(
        trialState === "demoted",
      );
    },
  );

  it.each(["converted", "demoted", "expired"] as const)(
    "shows missing %s history as unknown",
    (trialState) => {
      render(<TrialFacts org={anOrganization({ trial_state: trialState })} />);

      expect(
        screen.getByText("Trial tier").nextElementSibling?.textContent,
      ).toBe("Unknown");
      expect(screen.getByText("End date").nextElementSibling?.textContent).toBe(
        "Unknown",
      );
      if (trialState !== "expired") {
        expect(
          screen.getByText(
            trialState === "converted" ? "Conversion date" : "Demotion date",
          ).nextElementSibling?.textContent,
        ).toBe("Unknown");
      }
    },
  );
});
