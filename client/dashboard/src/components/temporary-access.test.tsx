import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SelfGrantAccess } from "./temporary-access";

const createGrant = vi.hoisted(() => ({ mutate: vi.fn() }));
const challenges = vi.hoisted(() => ({ value: [] as object[] }));

vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({ id: "admin-1" }),
}));

vi.mock("@/hooks/useScopeCatalogue", () => ({
  useScopeCatalogue: () => ({
    catalogue: new Map([
      [
        "chat:read",
        {
          slug: "chat:read",
          label: "View agent sessions",
          resourceType: "chat",
          temporaryGrantable: true,
        },
      ],
    ]),
    tasks: [],
  }),
}));

vi.mock("@gram/client/react-query/challenges.js", () => ({
  useChallenges: () => ({
    data: { challenges: challenges.value, total: challenges.value.length },
    isLoading: false,
  }),
  invalidateAllChallenges: vi.fn(),
}));

vi.mock("@gram/client/react-query/createTemporaryGrant.js", () => ({
  useCreateTemporaryGrantMutation: () => ({
    mutate: createGrant.mutate,
    isPending: false,
  }),
}));

function renderWithClient(ui: React.ReactElement) {
  const client = new QueryClient();
  return render(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>,
  );
}

function openAndFill(reason: string) {
  fireEvent.click(
    screen.getByRole("button", { name: /grant myself temporary access/i }),
  );
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: reason },
  });
}

afterEach(cleanup);
beforeEach(() => {
  createGrant.mutate.mockReset();
  challenges.value = [];
});

describe("SelfGrantAccess", () => {
  it("needs a reason before granting", () => {
    renderWithClient(<SelfGrantAccess scope="chat:read" resourceId="chat-1" />);
    fireEvent.click(
      screen.getByRole("button", { name: /grant myself temporary access/i }),
    );
    expect(screen.getByRole("button", { name: "Grant access" })).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("grants the viewer this chat for two hours and resolves their denials", () => {
    challenges.value = [
      {
        id: "denied-here",
        outcome: "deny",
        principalUrn: "user:admin-1",
        scope: "chat:read",
        resourceKind: "chat",
        resourceId: "chat-1",
        selector: { resource_kind: "chat", resource_id: "chat-1" },
      },
      {
        id: "denied-elsewhere",
        outcome: "deny",
        principalUrn: "user:admin-1",
        scope: "chat:read",
        resourceKind: "chat",
        resourceId: "chat-2",
        selector: { resource_kind: "chat", resource_id: "chat-2" },
      },
    ];
    const before = Date.now();
    renderWithClient(
      <SelfGrantAccess
        scope="chat:read"
        resourceId="chat-1"
        coverage={{ one: "This session", all: "All sessions" }}
      />,
    );
    openAndFill("Investigate finding");
    fireEvent.click(screen.getByRole("button", { name: "Grant access" }));

    expect(createGrant.mutate).toHaveBeenCalledTimes(1);
    const form =
      createGrant.mutate.mock.calls[0]![0].request.createTemporaryGrantForm;
    expect(form).toMatchObject({
      principalUrn: "user:admin-1",
      scope: "chat:read",
      selector: { resourceKind: "chat", resourceId: "chat-1" },
      reason: "Investigate finding",
      challengeIds: ["denied-here"],
    });
    const lead = form.expiresAt.getTime() - before;
    expect(lead).toBeGreaterThanOrEqual(2 * 60 * 60_000 - 1000);
    expect(lead).toBeLessThanOrEqual(2 * 60 * 60_000 + 1000);
  });

  it("covers every chat when the admin widens the grant", () => {
    challenges.value = [
      {
        id: "denied-here",
        outcome: "deny",
        principalUrn: "user:admin-1",
        scope: "chat:read",
        resourceKind: "chat",
        resourceId: "chat-1",
        selector: { resource_kind: "chat", resource_id: "chat-1" },
      },
      {
        id: "denied-elsewhere",
        outcome: "deny",
        principalUrn: "user:admin-1",
        scope: "chat:read",
        resourceKind: "chat",
        resourceId: "chat-2",
        selector: { resource_kind: "chat", resource_id: "chat-2" },
      },
    ];
    renderWithClient(
      <SelfGrantAccess
        scope="chat:read"
        resourceId="chat-1"
        coverage={{ one: "This session", all: "All sessions" }}
      />,
    );
    openAndFill("Audit");
    fireEvent.click(screen.getByRole("button", { name: "All sessions" }));
    fireEvent.click(screen.getByRole("button", { name: "Grant access" }));

    const form =
      createGrant.mutate.mock.calls[0]![0].request.createTemporaryGrantForm;
    expect(form.selector).toEqual({ resourceKind: "chat", resourceId: "*" });
    // A grant on every chat answers the denial on the other chat too.
    expect(form.challengeIds).toEqual(["denied-here", "denied-elsewhere"]);
  });

  it("runs onGranted once the grant is created", async () => {
    const onGranted = vi.fn();
    createGrant.mutate.mockImplementation(
      (_req: unknown, opts: { onSuccess: () => void }) => opts.onSuccess(),
    );
    renderWithClient(
      <SelfGrantAccess
        scope="chat:read"
        resourceId="chat-1"
        onGranted={() => {
          onGranted();
        }}
      />,
    );
    openAndFill("Investigate");
    fireEvent.click(screen.getByRole("button", { name: "Grant access" }));
    await waitFor(() => expect(onGranted).toHaveBeenCalledTimes(1));
  });
});
