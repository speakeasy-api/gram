import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { TrialCard } from "./TrialCard";

const ADDED = "2026-05-06T12:00:00Z";
const PEER_ADDED = "2026-04-01T12:00:00Z";

afterEach(cleanup);

function cardStatus(): string | null | undefined {
  return screen.getByText("Trial card").nextElementSibling?.textContent;
}

describe("TrialCard", () => {
  it("renders nothing when no card was offered", async () => {
    const { container } = await renderWithApp(
      <TrialCard org={anOrganization()} />,
    );

    expect(
      container.querySelector('[data-slot="organization-trial-card"]'),
    ).toBeNull();
  });

  it("shows when the card was added without a reuse warning", async () => {
    await renderWithApp(
      <TrialCard org={anOrganization({ trial_card_added_at: ADDED })} />,
    );

    expect(cardStatus()).toBe(`Added ${new Date(ADDED).toLocaleDateString()}`);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("shows a prepaid card rejection", async () => {
    await renderWithApp(
      <TrialCard
        org={anOrganization({
          trial_card_added_at: ADDED,
          trial_card_rejection_reason: "prepaid_card",
        })}
      />,
    );

    expect(cardStatus()).toBe(
      `Rejected ${new Date(ADDED).toLocaleDateString()}: prepaid card`,
    );
  });

  it("shows a reused card rejection", async () => {
    await renderWithApp(
      <TrialCard
        org={anOrganization({
          trial_card_added_at: ADDED,
          trial_card_rejection_reason: "card_used",
        })}
      />,
    );

    expect(cardStatus()).toBe(
      `Rejected ${new Date(ADDED).toLocaleDateString()}: already started another organization's trial`,
    );
  });

  it("links every organization that offered the same card with its outcome", async () => {
    await renderWithApp(
      <TrialCard
        org={anOrganization({
          trial_card_added_at: ADDED,
          trial_card_rejection_reason: "card_used",
          organizations_sharing_trial_card: [
            {
              id: "org_peer_1",
              name: "Peer One",
              slug: "peer-one",
              card_added_at: PEER_ADDED,
            },
            {
              id: "org_peer_2",
              name: "Peer Two",
              slug: "peer-two",
              card_added_at: PEER_ADDED,
              rejection_reason: "card_used",
            },
          ],
        })}
      />,
    );

    const warning = screen.getByRole("status");
    expect(warning.textContent).toContain(
      "2 other organizations offered this card",
    );
    const [first, second] = warning.querySelectorAll("li");
    expect(first?.textContent).toContain("peer-one");
    expect(first?.textContent).toContain("started its trial");
    expect(second?.textContent).toContain(
      "rejected: already started another organization's trial",
    );
    expect(
      screen.getByRole("link", { name: "Peer One" }).getAttribute("href"),
    ).toBe("/organizations/org_peer_1");
    expect(
      screen.getByRole("link", { name: "Peer Two" }).getAttribute("href"),
    ).toBe("/organizations/org_peer_2");
  });

  it("uses singular copy for one other organization", async () => {
    await renderWithApp(
      <TrialCard
        org={anOrganization({
          trial_card_added_at: ADDED,
          organizations_sharing_trial_card: [
            {
              id: "org_peer_1",
              name: "Peer One",
              slug: "peer-one",
              card_added_at: PEER_ADDED,
              rejection_reason: "card_used",
            },
          ],
        })}
      />,
    );

    expect(screen.getByRole("status").textContent).toContain(
      "Another organization offered this card",
    );
  });
});
