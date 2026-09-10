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

  expect(screen.getByText("Publish plugin marketplace")).toBeTruthy();
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
