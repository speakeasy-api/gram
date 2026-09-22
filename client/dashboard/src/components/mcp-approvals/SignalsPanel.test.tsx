import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { SignalsPanel } from "./SignalsPanel";
import type { EvidenceSignal } from "./signals";

afterEach(cleanup);

const SIGNALS: EvidenceSignal[] = [
  {
    id: "advisories-found",
    tone: "concern",
    headline: "2 published advisories name this package",
    section: "advisories",
  },
  {
    id: "acts-on-behalf",
    tone: "watch",
    headline: "27 of 61 tools declare that they act on your behalf",
    section: "capabilities",
  },
  {
    id: "destructive-tools",
    tone: "watch",
    headline: "4 of 61 tools declare destructive effects",
    section: "capabilities",
  },
  {
    id: "gap-domain_lookup_failed",
    tone: "unknown",
    headline: "The domain registry could not be consulted",
  },
  {
    id: "advisories-clean",
    tone: "clean",
    headline: "OSV lists no published advisories for this package",
    section: "advisories",
  },
];

describe("SignalsPanel", () => {
  it("renders nothing when there is nothing to rank", () => {
    const { container } = render(<SignalsPanel signals={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("groups the findings by how much they should interrupt the reader", () => {
    render(<SignalsPanel signals={SIGNALS} />);

    expect(screen.getByRole("heading", { name: "Concerns" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Notable" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Unknown" })).toBeTruthy();
  });

  it("uses one word per tone for both the heading and the tally", () => {
    render(<SignalsPanel signals={SIGNALS} />);
    expect(screen.getByText("2 notable")).toBeTruthy();
    expect(screen.getByText("1 unknown")).toBeTruthy();
  });

  it("tallies only the tones that have something in them", () => {
    // "0 concerns" is the reassurance this page must never give.
    render(
      <SignalsPanel signals={SIGNALS.filter((s) => s.tone === "watch")} />,
    );
    expect(screen.getByText("2 notable")).toBeTruthy();
    expect(screen.queryByText(/0 concerns/)).toBeNull();
  });

  it("keeps clean checks behind a toggle", () => {
    render(<SignalsPanel signals={SIGNALS} />);

    expect(screen.queryByText(/OSV lists no published advisories/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show 1" }));
    expect(screen.getByText(/OSV lists no published advisories/)).toBeTruthy();
  });

  it("links a finding to the section that shows its working", () => {
    render(<SignalsPanel signals={SIGNALS} />);

    const link = screen.getByRole("link", { name: "Advisories" });
    expect(link.getAttribute("href")).toBe("#evidence-advisories");
  });

  it("links once per run of findings that share a section", () => {
    // Six consecutive rows about the tool listing produced six identical
    // links down the gutter, which read as chrome rather than as a way out.
    render(<SignalsPanel signals={SIGNALS} />);
    expect(screen.getAllByRole("link", { name: "Tools" })).toHaveLength(1);
  });
});
