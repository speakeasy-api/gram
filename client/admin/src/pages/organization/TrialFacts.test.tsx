import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { anOrganization } from "@/test/fixtures";

import { TrialFacts } from "./TrialFacts";

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

    expect(screen.getByText("State").nextElementSibling?.textContent).toBe(
      stateLabel,
    );
    expect(screen.getByText("Tier").nextElementSibling?.textContent).toBe(
      "Enterprise",
    );
    expect(screen.getByText("Ends").nextElementSibling?.textContent).toBe(
      new Date(END).toLocaleDateString(),
    );
    expect(screen.getByText("Remaining").nextElementSibling?.textContent).toBe(
      "1 hour 1 minute",
    );
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

    expect(screen.getByText("Tier").nextElementSibling?.textContent).toBe(
      "Unknown",
    );
    expect(screen.queryByText("future_internal_tier")).toBeNull();
  });

  it("updates on end-relative minute boundaries and cleans up its timer", () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:30.500Z");
    const view = render(
      <TrialFacts
        org={anOrganization({
          trial_state: "running",
          trial_tier: "enterprise",
          trial_ends_at: "2026-05-06T01:01:45Z",
        })}
      />,
    );

    const remaining = () =>
      screen.getByText("Remaining").nextElementSibling?.textContent;
    expect(remaining()).toBe("1 hour 2 minutes");

    act(() => {
      vi.advanceTimersByTime(14_499);
    });
    expect(remaining()).toBe("1 hour 2 minutes");
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(remaining()).toBe("1 hour 1 minute");
    expect(vi.getTimerCount()).toBe(1);

    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each([
    ["2026-05-09T00:00:30Z", "4 days", "72 hours"],
    ["2026-05-07T00:00:30Z", "25 hours", "24 hours"],
  ])("updates at the formatter boundary ending at %s", (end, before, after) => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:00Z");
    render(
      <TrialFacts
        org={anOrganization({
          trial_state: "running",
          trial_ends_at: end,
        })}
      />,
    );

    const remaining = () =>
      screen.getByText("Remaining").nextElementSibling?.textContent;
    expect(remaining()).toBe(before);
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(remaining()).toBe(after);
  });

  it("switches a live trial to expired at its exact end", () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:30Z");
    const end = "2026-05-06T00:01:45Z";
    render(
      <TrialFacts
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

    expect(screen.getByText("State").nextElementSibling?.textContent).toBe(
      "Expired",
    );
    expect(screen.queryByText("Remaining")).toBeNull();
    expect(
      screen.getByText("Original end").nextElementSibling?.textContent,
    ).toBe(new Date(end).toLocaleDateString());
    expect(screen.getByText("Tier").nextElementSibling?.textContent).toBe(
      "Enterprise",
    );
    expect(vi.getTimerCount()).toBe(0);
  });

  it("refreshes a stale clock when transitioning into a live trial", () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-05-06T00:00:00Z");
    const view = render(
      <TrialFacts org={anOrganization({ trial_state: "converted" })} />,
    );
    vi.setSystemTime("2026-05-06T00:30:00Z");

    view.rerender(
      <TrialFacts
        org={anOrganization({
          trial_state: "running",
          trial_ends_at: "2026-05-06T01:00:00Z",
        })}
      />,
    );

    expect(screen.getByText("Remaining").nextElementSibling?.textContent).toBe(
      "30 minutes",
    );
  });

  it.each([undefined, "not-a-date"])(
    "shows an unknown live end for %s",
    (end) => {
      vi.useFakeTimers();
      render(
        <TrialFacts
          org={anOrganization({
            trial_state: "running",
            trial_ends_at: end,
          })}
        />,
      );

      expect(screen.getByText("Ends").nextElementSibling?.textContent).toBe(
        "Unknown",
      );
      expect(
        screen.getByText("Remaining").nextElementSibling?.textContent,
      ).toBe("Unknown");
      expect(vi.getTimerCount()).toBe(0);
    },
  );

  it("labels an unknown future trial state safely", () => {
    render(
      <TrialFacts
        org={anOrganization({ trial_state: "future_state" as "running" })}
      />,
    );

    expect(screen.getByText("State").nextElementSibling?.textContent).toBe(
      "Unknown",
    );
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

      expect(screen.getByText("State").nextElementSibling?.textContent).toBe(
        stateLabel,
      );
      expect(screen.getByText("Tier").nextElementSibling?.textContent).toBe(
        "Enterprise",
      );
      expect(
        screen.getByText("Original end").nextElementSibling?.textContent,
      ).toBe(new Date(end).toLocaleDateString());

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

      expect(screen.getByText("Tier").nextElementSibling?.textContent).toBe(
        "Unknown",
      );
      expect(
        screen.getByText("Original end").nextElementSibling?.textContent,
      ).toBe("Unknown");
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
