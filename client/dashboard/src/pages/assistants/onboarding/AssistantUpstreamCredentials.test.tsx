import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantUpstreamCredentials } from "./AssistantUpstreamCredentials";

const mocks = vi.hoisted(() => ({
  profiles: [] as unknown[],
  canWrite: true,
  mutate: vi.fn(),
}));

vi.mock("@gram/client/react-query/assistantExecutionProfiles.js", () => ({
  invalidateAllAssistantExecutionProfiles: () => Promise.resolve(),
  useAssistantExecutionProfiles: () => ({
    data: { profiles: mocks.profiles },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock(
  "@gram/client/react-query/setAssistantExecutionProfileChoice.js",
  () => ({
    useSetAssistantExecutionProfileChoiceMutation: () => ({
      mutate: mocks.mutate,
      isPending: false,
    }),
  }),
);

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: { mcpServers: [{ id: "server-1", name: "Linear", slug: "linear" }] },
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
}));

function renderSection() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AssistantUpstreamCredentials
        assistantId="assistant-1"
        canWrite={mocks.canWrite}
      />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  mocks.profiles = [];
  mocks.canWrite = true;
  mocks.mutate.mockReset();
});

describe("AssistantUpstreamCredentials", () => {
  it("says why a server has no credential yet", () => {
    mocks.profiles = [
      {
        triggerInstanceId: "t-1",
        triggerName: "Dashboard",
        executionContext: "user_backed",
        version: 1,
        servers: [
          {
            mcpServerId: "server-1",
            chosen: false,
            gapReason: "selection_required",
            options: [
              {
                authClientId: "ac-1",
                name: "Shared token",
                clientType: "static_credential",
              },
              {
                authClientId: "ac-2",
                name: "Linear",
                clientType: "remote_session",
              },
            ],
          },
        ],
      },
    ];
    renderSection();

    expect(screen.getByText("Dashboard")).toBeTruthy();
    expect(screen.getByText("When acting for a person")).toBeTruthy();
    expect(
      screen.getByText("No credential chosen yet. Choose one."),
    ).toBeTruthy();
  });

  it("does not let members without write access change a choice", () => {
    mocks.canWrite = false;
    mocks.profiles = [
      {
        triggerInstanceId: "t-1",
        triggerName: "Dashboard",
        executionContext: "userless",
        version: 1,
        servers: [
          {
            mcpServerId: "server-1",
            chosen: true,
            authClientId: "ac-1",
            source: "admin",
            options: [
              {
                authClientId: "ac-1",
                name: "Shared token",
                clientType: "static_credential",
              },
            ],
          },
        ],
      },
    ];
    renderSection();

    const select = screen.getByRole("combobox", {
      name: "Credential for Linear",
    });
    expect(select.hasAttribute("disabled")).toBe(true);
  });

  it("explains a server list with nothing to choose", () => {
    renderSection();

    expect(
      screen.getByText(
        "This assistant reaches no MCP server that signs in upstream.",
      ),
    ).toBeTruthy();
  });

  it("sends the picked credential with the version it was read at", () => {
    mocks.profiles = [
      {
        triggerInstanceId: "t-1",
        triggerName: "Dashboard",
        executionContext: "userless",
        version: 3,
        servers: [
          {
            mcpServerId: "server-1",
            chosen: false,
            gapReason: "selection_required",
            noAuthAllowed: false,
            options: [
              {
                authClientId: "ac-1",
                name: "Shared token",
                clientType: "static_credential",
              },
            ],
          },
        ],
      },
    ];
    renderSection();

    fireEvent.click(
      screen.getByRole("combobox", { name: "Credential for Linear" }),
    );
    fireEvent.click(
      screen.getByRole("option", { name: /Shared token · Static credential/ }),
    );

    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        gramProject: "default",
        setExecutionProfileChoiceRequestBody: {
          assistantId: "assistant-1",
          triggerInstanceId: "t-1",
          executionContext: "userless",
          mcpServerId: "server-1",
          expectedVersion: 3,
          automatic: undefined,
          noAuth: undefined,
          authClientId: "ac-1",
        },
      },
    });
  });
});
