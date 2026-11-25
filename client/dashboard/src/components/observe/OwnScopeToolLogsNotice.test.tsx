import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OwnScopeToolLogsNotice } from "./OwnScopeToolLogsNotice";

const requestAccess = vi.hoisted(() => ({
  mutateAsync: vi.fn(() => Promise.resolve({ sentToCount: 1 })),
}));

// The request button looks up the viewer's pending access requests when the
// rollout is on.
vi.mock("@gram/client/react-query/accessRequests.js", () => ({
  useAccessRequests: () => ({
    data: { requests: [] },
    refetch: () => Promise.resolve(),
  }),
  invalidateAllAccessRequests: () => Promise.resolve(),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({ invalidateQueries: () => Promise.resolve() }),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", name: "Default" }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false }),
}));

vi.mock("@/hooks/useScopeCatalogue", () => ({
  // log:read can be given temporarily, so it can be requested.
  useScopeCatalogue: () => ({
    catalogue: new Map([["log:read", { temporaryGrantable: true }]]),
    tasks: [],
  }),
}));

vi.mock("@gram/client/react-query/requestAccess.js", () => ({
  useRequestAccessMutation: () => ({
    mutateAsync: requestAccess.mutateAsync,
    isPending: false,
  }),
}));

// The real control is a Radix select, which jsdom cannot drive; a native
// select with the same label and choices stands in for it.
vi.mock("@/components/request-duration-select", () => ({
  RequestDurationSelect: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (value: string) => void;
  }) => (
    <label>
      How long do you need it?
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {["none", "1h", "2h", "4h", "eod"].map((key) => (
          <option key={key} value={key}>
            {key}
          </option>
        ))}
      </select>
    </label>
  ),
}));

afterEach(cleanup);

describe("OwnScopeToolLogsNotice", () => {
  it("says what is missing and requests it from a dialog on the tool logs page", async () => {
    render(<OwnScopeToolLogsNotice />);

    expect(
      screen.getByText(
        /You don't have access to every member's tool logs, but you can request it/,
      ),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByRole("dialog")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("How long do you need it?"), {
      target: { value: "2h" },
    });
    fireEvent.change(screen.getByLabelText(/Why do you need it/), {
      target: { value: "Checking a failed refund" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send request" }));

    expect(await screen.findByText("Request sent")).toBeTruthy();
    expect(requestAccess.mutateAsync).toHaveBeenCalledWith({
      request: {
        requestAccessForm: {
          scope: "log:read",
          resourceId: "project-1",
          resourceName: "Default",
          duration: "2h",
          message: "Checking a failed refund",
        },
      },
    });
  });
});
