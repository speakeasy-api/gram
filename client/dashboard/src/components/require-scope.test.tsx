import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RequestScopeAccess } from "./require-scope";

const mutateAsync = vi.hoisted(() =>
  vi.fn(() => Promise.resolve({ sentToCount: 1 })),
);

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false, isLoading: false }),
}));

vi.mock("@gram/client/react-query/requestAccess.js", () => ({
  useRequestAccessMutation: () => ({ mutateAsync, isPending: false }),
}));

vi.mock("@/hooks/useScopeCatalogue", () => ({
  useScopeCatalogue: () => ({ catalogue: new Map(), tasks: [] }),
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: undefined }),
}));

afterEach(cleanup);

describe("RequestScopeAccess", () => {
  it("starts over when the resource it would ask for changes", async () => {
    const { rerender } = render(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByText("Request sent")).toBeTruthy();
    expect(mutateAsync).toHaveBeenCalledWith({
      request: {
        requestAccessForm: { scope: "log:read", resourceId: "project-1" },
      },
    });

    rerender(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-2" />,
    );

    expect(screen.getByRole("button", { name: "Request Access" })).toBeTruthy();
    expect(screen.queryByText("Request sent")).toBeNull();
  });
});
