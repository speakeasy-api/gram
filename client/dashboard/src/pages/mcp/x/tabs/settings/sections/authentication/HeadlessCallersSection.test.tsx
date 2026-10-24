import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { HeadlessCallersSection } from "./HeadlessCallersSection";

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  profiles: [] as unknown[],
}));

vi.mock("@gram/client/react-query/issuanceProfiles.js", () => ({
  invalidateAllIssuanceProfiles: () => Promise.resolve(),
  useIssuanceProfiles: () => ({
    data: { items: mocks.profiles },
    isSuccess: true,
  }),
}));

vi.mock("@gram/client/react-query/setIssuanceProfile.js", () => ({
  useSetIssuanceProfileMutation: () => ({
    mutate: mocks.save,
    isPending: false,
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ userSessionClients: { list: () => undefined } }),
}));

vi.mock("@/components/sessions/collectPageItems", () => ({
  collectPageItems: () =>
    Promise.resolve([{ id: "usc-1", clientName: "Nightly report job" }]),
}));

vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({ data: { admissions: [], issuers: [] } }),
}));

const credentials = [
  {
    id: "ac-token",
    name: "Shared token",
    clientType: "static_credential",
    enabled: true,
  },
  {
    id: "ac-own",
    name: "Own account",
    clientType: "remote_session",
    enabled: true,
  },
] as unknown as AuthClient[];

function renderSection(canManage = true) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <HeadlessCallersSection
        mcpServerId="server-1"
        userSessionIssuerId="usi-1"
        credentials={credentials}
        canManage={canManage}
      />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.profiles = [];
});

describe("HeadlessCallersSection", () => {
  it("shows the credential each app uses on this server", () => {
    mocks.profiles = [
      {
        id: "p-1",
        name: "Nightly report job",
        userSessionIssuerId: "usi-1",
        userSessionClientId: "usc-1",
        choices: [{ mcpServerId: "server-1", authClientId: "ac-token" }],
        updatedAt: new Date(),
      },
    ];
    renderSection();

    expect(screen.getByText("Nightly report job")).toBeTruthy();
    expect(screen.getByText("Shared token")).toBeTruthy();
  });

  it("removes only this server's choice and keeps the others", () => {
    mocks.profiles = [
      {
        id: "p-1",
        name: "Nightly report job",
        userSessionIssuerId: "usi-1",
        userSessionClientId: "usc-1",
        choices: [
          { mcpServerId: "server-1", authClientId: "ac-token" },
          { mcpServerId: "server-2", authClientId: "ac-other" },
        ],
        updatedAt: new Date(),
      },
    ];
    renderSection();

    fireEvent.click(
      screen.getByRole("button", {
        name: "Remove the choice for Nightly report job",
      }),
    );

    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setIssuanceProfileRequestBody: expect.objectContaining({
          userSessionIssuerId: "usi-1",
          userSessionClientId: "usc-1",
          choices: [{ mcpServerId: "server-2", authClientId: "ac-other" }],
        }),
      },
    });
  });

  it("offers no changes to members who cannot manage credentials", () => {
    mocks.profiles = [
      {
        id: "p-1",
        name: "Nightly report job",
        userSessionIssuerId: "usi-1",
        userSessionClientId: "usc-1",
        choices: [{ mcpServerId: "server-1", authClientId: "ac-token" }],
        updatedAt: new Date(),
      },
    ];
    renderSection(false);

    expect(
      screen.queryByRole("button", { name: "Choose for an app" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", {
        name: "Remove the choice for Nightly report job",
      }),
    ).toBeNull();
  });
});
