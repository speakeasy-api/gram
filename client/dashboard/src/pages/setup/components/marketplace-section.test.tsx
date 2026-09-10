import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MarketplaceSection } from "./marketplace-section";

const publishStatus = vi.hoisted(() => ({
  current: {
    data: { connected: false } as Record<string, unknown> | undefined,
    isLoading: false,
  },
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => publishStatus.current,
  invalidateAllPublishStatus: vi.fn(),
}));
vi.mock("@gram/client/react-query/publishPlugins", () => ({
  usePublishPluginsMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@/pages/plugins/PublishDialog", () => ({
  PublishDialog: ({ open, mode }: { open: boolean; mode: string }) =>
    open ? <div>Publish dialog: {mode}</div> : null,
}));

afterEach(cleanup);
beforeEach(() => {
  publishStatus.current = { data: { connected: false }, isLoading: false };
});

it("keeps the publish prompt when the status read fails", () => {
  // A non-401 failure used to reach the page error boundary and replace the
  // whole card; it has to degrade to "not published" instead.
  publishStatus.current = { data: undefined, isLoading: false };

  render(<MarketplaceSection index={1} description="Needed for this card." />);

  // The section title renders in every state, so assert the degraded body:
  // the publish prompt, not the published repo row.
  expect(
    screen.getByRole("button", { name: "Publish marketplace" }),
  ).toBeTruthy();
});

it("counts a published repo as done when collaborators are not required", () => {
  publishStatus.current = {
    data: {
      connected: true,
      marketplaceUrl: "https://app.example.com/marketplace/tok.git",
      hasCollaborators: false,
    },
    isLoading: false,
  };

  render(<MarketplaceSection index={1} description="Needed for this card." />);

  // The step number is replaced by a check once the outcome lands. Asserting
  // on the published body instead would pass whatever `complete` did.
  expect(screen.queryByText("1")).toBeNull();
});

it("stays complete on a card requiring collaborators when the check is unknown", () => {
  // getPublishStatus omits the flag when the collaborator lookup failed, and
  // says the dashboard must read that as unknown rather than false. A
  // transient GitHub error must not hold the step open.
  publishStatus.current = {
    data: {
      connected: true,
      marketplaceUrl: "https://app.example.com/marketplace/tok.git",
    },
    isLoading: false,
  };

  render(
    <MarketplaceSection
      index={1}
      description="Needed for this card."
      requiresCollaborators
    />,
  );

  expect(screen.queryByText("1")).toBeNull();
});

it("holds the step open without collaborators when the card requires them", () => {
  // Claude.ai syncs the repo through its own GitHub App and cannot read one
  // nobody has access to, so publishing alone is not the outcome there.
  publishStatus.current = {
    data: {
      connected: true,
      marketplaceUrl: "https://app.example.com/marketplace/tok.git",
      hasCollaborators: false,
    },
    isLoading: false,
  };

  render(
    <MarketplaceSection
      index={1}
      description="Needed for this card."
      requiresCollaborators
    />,
  );

  expect(screen.getByText("1")).toBeTruthy();
});

it("completes for a collaborator-requiring card once access is granted", () => {
  publishStatus.current = {
    data: {
      connected: true,
      marketplaceUrl: "https://app.example.com/marketplace/tok.git",
      hasCollaborators: true,
    },
    isLoading: false,
  };

  render(
    <MarketplaceSection
      index={1}
      description="Needed for this card."
      requiresCollaborators
    />,
  );

  expect(screen.queryByText("1")).toBeNull();
});

describe("MarketplaceSection", () => {
  it("offers to publish when no marketplace exists yet", () => {
    render(<MarketplaceSection index={1} description="Needed here." />);

    expect(screen.getByText("Publish plugin marketplace")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Publish marketplace" }),
    );
    expect(screen.getByText("Publish dialog: publish")).toBeTruthy();
  });

  it("shows the published repo and lets admins manage collaborators", () => {
    publishStatus.current = {
      data: {
        connected: true,
        repoUrl: "https://github.com/acme/acme-speakeasy",
        marketplaceUrl: "https://app.example.com/marketplace/tok.git",
        repoOwner: "acme",
        repoName: "acme-speakeasy",
      },
      isLoading: false,
    };

    render(
      <MarketplaceSection
        index={1}
        description="Needed here."
        publishedHint="Pick this repo."
      />,
    );

    expect(screen.getByText("Published")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "acme/acme-speakeasy" }),
    ).toBeTruthy();
    expect(screen.getByText(/Pick this repo\./)).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Manage collaborators" }),
    );
    expect(screen.getByText("Publish dialog: manage")).toBeTruthy();
  });
});
