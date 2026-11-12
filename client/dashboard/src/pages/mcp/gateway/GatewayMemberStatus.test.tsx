import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemberStatusBadge } from "./GatewayMembersSection";

vi.mock("@/components/ui/Icon", () => ({ Icon: () => <span /> }));
vi.mock("@/components/sources/SourceCard", () => ({
  SourceMcpIcon: () => null,
}));

afterEach(cleanup);

function renderBadge(props: Partial<ComponentProps<typeof MemberStatusBadge>>) {
  render(
    <TooltipProvider>
      <MemoryRouter>
        <MemberStatusBadge classification="proxied" name="Linear" {...props} />
      </MemoryRouter>
    </TooltipProvider>,
  );
}

it("shows what the viewer gets, with Connect while sign-in is needed", () => {
  const onConnect = vi.fn<() => void>();
  renderBadge({ viewer: { kind: "sign_in" }, onConnect });
  expect(screen.getByText("Sign in needed")).toBeTruthy();
  fireEvent.click(
    screen.getByRole("button", { name: "Connect your account for Linear" }),
  );
  expect(onConnect).toHaveBeenCalled();
});

it("flips to Available after connecting, with no action", () => {
  renderBadge({
    viewer: { kind: "available" },
    onConnect: vi.fn<() => void>(),
  });
  expect(screen.getByText("Available")).toBeTruthy();
  expect(screen.queryByRole("button")).toBeNull();
});

it("offers a retry when the member's tools couldn't be read", () => {
  const retry = vi.fn<() => void>();
  renderBadge({
    name: "Slack",
    viewer: { kind: "unavailable", why: "Slack didn't answer.", retry },
  });
  expect(screen.getByText("Unavailable")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Check Slack again" }));
  expect(retry).toHaveBeenCalled();
});

it("reads Checking while loading, and Unknown only for an unresolved server", () => {
  renderBadge({ viewer: { kind: "checking" } });
  expect(screen.getByText("Checking…")).toBeTruthy();
  cleanup();
  renderBadge({ classification: "unknown" });
  expect(screen.getByText("Unknown")).toBeTruthy();
});
