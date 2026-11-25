import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
} from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantUpstreamCredentials } from "./AssistantUpstreamCredentials";

const mocks = vi.hoisted(() => ({
  profiles: [] as unknown[],
  canWrite: true,
  mutate: vi.fn(),
}));

vi.mock("@gram/client/react-query/assistantExecutionProfiles.js", () => {
  const key = (parameters: object) => ["profiles", parameters];
  return {
    queryKeyAssistantExecutionProfiles: key,
    invalidateAllAssistantExecutionProfiles: () => Promise.resolve(),
    useAssistantExecutionProfiles: (request: object) =>
      useQuery({
        queryKey: key(request),
        queryFn: () => ({ profiles: mocks.profiles }),
        initialData: { profiles: mocks.profiles },
        staleTime: Infinity,
      }),
  };
});

vi.mock(
  "@gram/client/react-query/setAssistantExecutionProfileChoice.js",
  () => ({
    useSetAssistantExecutionProfileChoiceMutation: (options: object) =>
      useMutation({ mutationFn: mocks.mutate, ...options }),
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

const sharedToken = {
  authClientId: "ac-1",
  name: "Shared token",
  clientType: "static_credential",
};

describe("AssistantUpstreamCredentials", () => {
  it("shows a withdrawn choice as not chosen", () => {
    mocks.profiles = [
      {
        triggerInstanceId: "t-1",
        triggerName: "Dashboard",
        executionContext: "userless",
        version: 2,
        servers: [
          {
            mcpServerId: "server-1",
            chosen: true,
            executionProfileChoiceSource: "admin",
            authClientId: "ac-retired",
            gapReason: "no_client",
            options: [sharedToken],
          },
        ],
      },
    ];
    renderSection();

    expect(
      screen.getByRole("combobox", { name: "Credential for Linear" })
        .textContent,
    ).toContain("Not chosen");
    expect(screen.getByText("No credential fits yet.")).toBeTruthy();
  });

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

  it("sends the picked credential with the version it was read at", async () => {
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
    mocks.mutate.mockResolvedValue(mocks.profiles[0]);
    renderSection();

    fireEvent.click(
      screen.getByRole("combobox", { name: "Credential for Linear" }),
    );
    fireEvent.click(
      screen.getByRole("option", { name: /Shared token · Static credential/ }),
    );

    await waitFor(() => expect(mocks.mutate).toHaveBeenCalled());
    expect(mocks.mutate.mock.calls[0]?.[0]).toEqual({
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

  it("waits for one save before the next and sends the version it made", async () => {
    const server = (id: string) => ({
      mcpServerId: id,
      chosen: false,
      gapReason: "selection_required",
      noAuthAllowed: false,
      options: [sharedToken],
    });
    mocks.profiles = [
      {
        triggerInstanceId: "t-1",
        triggerName: "Dashboard",
        executionContext: "userless",
        version: 3,
        servers: [server("server-1"), server("server-2")],
      },
    ];
    let finish: (profile: unknown) => void = () => {};
    mocks.mutate.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    renderSection();

    const [first, second] = screen.getAllByRole("combobox");
    fireEvent.click(first!);
    fireEvent.click(
      screen.getByRole("option", { name: /Shared token · Static credential/ }),
    );
    await waitFor(() => expect(second!.hasAttribute("disabled")).toBe(true));

    finish({
      ...(mocks.profiles[0] as object),
      version: 4,
      servers: [
        {
          ...server("server-1"),
          chosen: true,
          gapReason: undefined,
          authClientId: "ac-1",
        },
        server("server-2"),
      ],
    });
    await waitFor(() => expect(second!.hasAttribute("disabled")).toBe(false));
    mocks.mutate.mockResolvedValueOnce(mocks.profiles[0]);
    fireEvent.click(second!);
    fireEvent.click(
      screen.getByRole("option", { name: /Shared token · Static credential/ }),
    );
    await waitFor(() => expect(mocks.mutate).toHaveBeenCalledTimes(2));
    expect(
      mocks.mutate.mock.calls[1]?.[0].request
        .setExecutionProfileChoiceRequestBody.expectedVersion,
    ).toBe(4);
  });
});
