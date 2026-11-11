import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OwnScopeToolLogsNotice } from "./OwnScopeToolLogsNotice";

const requestAccess = vi.hoisted(() => ({
  mutateAsync: vi.fn(() => Promise.resolve({ sentToCount: 1 })),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1" }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false }),
}));

vi.mock("@gram/client/react-query/requestAccess.js", () => ({
  useRequestAccessMutation: () => ({
    mutateAsync: requestAccess.mutateAsync,
    isPending: false,
  }),
}));

afterEach(cleanup);

describe("OwnScopeToolLogsNotice", () => {
  it("says what is shown and asks an admin for log:read on this project", async () => {
    render(<OwnScopeToolLogsNotice />);

    expect(
      screen.getByText(/Showing your sessions and your agents' activity/),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));

    expect(await screen.findByText("Request sent")).toBeTruthy();
    expect(requestAccess.mutateAsync).toHaveBeenCalledWith({
      request: {
        requestAccessForm: { scope: "log:read", resourceId: "project-1" },
      },
    });
  });
});
