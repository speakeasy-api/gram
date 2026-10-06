import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CreateMarketplaceStep } from "./create-marketplace-step";

const status = vi.hoisted(() => ({
  data: undefined as Record<string, unknown> | undefined,
  query: vi.fn(),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: (...args: unknown[]) => {
    status.query(...args);
    return { data: status.data, isLoading: false };
  },
  invalidateAllPublishStatus: vi.fn(),
}));
vi.mock("@gram/client/react-query/publishPlugins", () => ({
  usePublishPluginsMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@/pages/plugins/MarketplaceCard", () => ({
  MarketplaceCard: () => <p>Published marketplace</p>,
}));
vi.mock("@/pages/plugins/PublishDialog", () => ({
  PublishDialog: ({ open }: { open: boolean }) =>
    open ? <p>Publish dialog</p> : null,
}));
afterEach(cleanup);
beforeEach(() => {
  status.data = undefined;
  status.query.mockClear();
});

it("keeps failed status reads in the publish prompt", () => {
  render(<CreateMarketplaceStep onComplete={() => {}} onBack={() => {}} />);
  expect(status.query).toHaveBeenCalledWith(undefined, undefined, {
    throwOnError: false,
  });
  expect(
    screen.getByRole("button", { name: "Setup Plugin Marketplace" }),
  ).toBeTruthy();
});

it("does not complete a connection without a marketplace URL", () => {
  status.data = {
    connected: true,
    repoUrl: "https://github.com/example/marketplace",
  };
  const complete = vi.fn<() => void>();
  render(<CreateMarketplaceStep onComplete={complete} onBack={() => {}} />);
  fireEvent.click(
    screen.getByRole("button", { name: "Setup Plugin Marketplace" }),
  );
  expect(screen.getByText("Publish dialog")).toBeTruthy();
  expect(complete).not.toHaveBeenCalled();
});

it("continues once a usable marketplace URL exists", () => {
  status.data = {
    connected: true,
    marketplaceUrl: "https://example.com/marketplace/synthetic.git",
  };
  const complete = vi.fn<() => void>();
  render(<CreateMarketplaceStep onComplete={complete} onBack={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  expect(complete).toHaveBeenCalledOnce();
});
