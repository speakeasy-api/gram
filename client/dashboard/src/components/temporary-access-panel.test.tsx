import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TemporaryAccessPanel } from "./temporary-access-panel";

const revoke = vi.hoisted(() => ({ mutate: vi.fn() }));
const grants = vi.hoisted(() => ({ value: [] as object[] }));

vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({ id: "admin-1" }),
}));

vi.mock("@/hooks/useScopeCatalogue", () => ({
  useScopeCatalogue: () => ({
    catalogue: new Map([
      ["chat:read", { slug: "chat:read", label: "View agent sessions" }],
    ]),
    tasks: [],
  }),
}));

vi.mock("@gram/client/react-query/temporaryGrants.js", () => ({
  useTemporaryGrants: () => ({ data: { grants: grants.value } }),
  invalidateAllTemporaryGrants: vi.fn(),
}));

vi.mock("@gram/client/react-query/revokeTemporaryGrant.js", () => ({
  useRevokeTemporaryGrantMutation: () => ({
    mutate: revoke.mutate,
    isPending: false,
  }),
}));

function grant(id: string, resourceId: string, minutesLeft: number) {
  return {
    id,
    principalUrn: "user:admin-1",
    scope: "chat:read",
    selector: { resourceKind: "chat", resourceId },
    status: "active",
    expiresAt: new Date(Date.now() + minutesLeft * 60_000),
    remainingSeconds: minutesLeft * 60,
    otherAccess: [],
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

function renderPanel() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TemporaryAccessPanel />
    </QueryClientProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  revoke.mutate.mockReset();
  grants.value = [];
  window.localStorage.clear();
});

describe("TemporaryAccessPanel", () => {
  it("stays hidden without active temporary access", () => {
    grants.value = [{ ...grant("g", "*", 30), status: "expired" }];
    renderPanel();
    expect(screen.queryByRole("region", { name: "Temporary access" })).toBe(
      null,
    );
  });

  it("lists each grant with what it covers and the time left", () => {
    grants.value = [
      grant("all", "*", 125),
      grant("one", "0f1e2d3c-0000-4000-8000-000000000001", 30),
    ];
    renderPanel();

    const panel = screen.getByRole("region", { name: "Temporary access" });
    expect(panel.textContent).toContain("2");
    const rows = screen.getAllByRole("listitem");
    // Soonest deadline first.
    expect(rows[0]!.textContent).toContain("Session 0f1e2d3c");
    expect(rows[0]!.textContent).toMatch(/(29|30) min left/);
    expect(rows[1]!.textContent).toContain("All sessions");
    expect(rows[1]!.textContent).toMatch(/2 h (4|5) min left/);
  });

  it("asks once before revoking", () => {
    grants.value = [grant("all", "*", 60)];
    renderPanel();

    fireEvent.click(
      screen.getByRole("button", { name: "Revoke chat:read on All sessions" }),
    );
    expect(revoke.mutate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    expect(revoke.mutate).toHaveBeenCalledWith(
      { request: { riskIDRequestBody: { id: "all" } } },
      expect.anything(),
    );
  });

  it("keeps the grant when the admin backs out", () => {
    grants.value = [grant("all", "*", 60)];
    renderPanel();

    fireEvent.click(
      screen.getByRole("button", { name: "Revoke chat:read on All sessions" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Keep" }));
    expect(revoke.mutate).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Revoke chat:read on All sessions" }),
    ).toBeTruthy();
  });

  it("collapses to its header", () => {
    grants.value = [grant("all", "*", 60)];
    renderPanel();

    fireEvent.click(screen.getByRole("button", { name: /Temporary access/ }));
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
    expect(
      screen.getByRole("region", { name: "Temporary access" }).textContent,
    ).toContain("ends");
  });
});
