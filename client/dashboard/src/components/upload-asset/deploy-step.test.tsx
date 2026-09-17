import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import DeployStep from "./deploy-step";
const state = vi.hoisted(() => ({
  listTools: vi.fn(),
  cachedTools: [
    { type: "http", openapiv3DocumentId: "document", toolUrn: "urn:tool" },
  ],
  evolve: vi.fn(),
  getDeployment: vi.fn(),
  stepState: "completed",
  current: false,
  setStep: vi.fn(),
  setStepper: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  wrapper: vi.fn(),
  listWrappers: vi.fn(),
  capture: vi.fn(),
  meta: {
    current: {
      assetName: "Example API",
      uploadResult: { asset: { id: "asset" } },
      deployment: {
        id: "deployment",
        openapiv3Assets: [{ id: "document", slug: "example_api" }],
      },
      toolset: null,
    },
  },
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    deployments: {
      evolveDeployment: state.evolve,
      getById: state.getDeployment,
    },
    tools: { list: state.listTools },
    toolsets: { create: state.create, updateBySlug: state.update },
    mcpServers: { create: state.wrapper, list: state.listWrappers },
  }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: state.capture }),
}));
vi.mock("@/hooks/toolTypes", () => ({
  useListTools: () => ({
    data: {
      tools: state.cachedTools,
    },
  }),
}));
vi.mock("@gram/client/react-query/deploymentLogs.js", () => ({
  useDeploymentLogs: () => ({}),
}));
vi.mock("./step/use-step", () => ({
  useStep: () => ({
    state: state.stepState,
    isCurrentStep: state.current,
    setState: state.setStep,
  }),
}));
vi.mock("./stepper/use-stepper", () => ({
  useStepper: () => ({ meta: state.meta, setState: state.setStepper }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ deployments: { deployment: { Link: () => null } } }),
}));
afterEach(cleanup);
beforeEach(() => {
  vi.resetAllMocks();
  state.cachedTools = [
    { type: "http", openapiv3DocumentId: "document", toolUrn: "urn:tool" },
  ];
  state.listTools.mockResolvedValue({ tools: [] });
  state.stepState = "completed";
  state.current = false;
  state.meta.current.deployment = {
    id: "deployment",
    openapiv3Assets: [{ id: "document", slug: "example_api" }],
  };
  state.meta.current.toolset = null;
  state.listWrappers.mockResolvedValue({ mcpServers: [] });
  state.create.mockResolvedValue({
    id: "toolset",
    slug: "example",
    name: "Example API",
  });
  state.update.mockResolvedValue({});
  state.wrapper.mockResolvedValue({ id: "server" });
});
it("attaches the server wrapper, not the deployment or toolset, after seeding tools", async () => {
  const complete = vi.fn().mockResolvedValue(undefined);
  render(
    <DeployStep
      gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
    />,
  );
  await waitFor(() => expect(complete).toHaveBeenCalledWith("server"));
  expect(state.wrapper).toHaveBeenCalledWith({
    createMcpServerForm: {
      name: "Example API",
      toolsetId: "toolset",
      visibility: "private",
    },
  });
  expect(state.update.mock.invocationCallOrder[0]).toBeLessThan(
    state.wrapper.mock.invocationCallOrder[0]!,
  );
});
it("does not create again while an already-created server awaits attachment", async () => {
  const complete = vi.fn();
  render(
    <DeployStep
      gateway={{
        gatewayId: "gateway",
        createdServerId: "server",
        complete,
      }}
    />,
  );
  await act(async () => {});
  await waitFor(() => {
    expect(state.create).not.toHaveBeenCalled();
    expect(state.update).not.toHaveBeenCalled();
    expect(state.wrapper).not.toHaveBeenCalled();
    expect(complete).not.toHaveBeenCalled();
  });
});
it("leaves standalone toolset creation unchanged", async () => {
  render(<DeployStep />);
  await waitFor(() => expect(state.update).toHaveBeenCalled());
  expect(state.wrapper).not.toHaveBeenCalled();
});

const renderGateway = () => {
  const complete = vi.fn().mockResolvedValue(undefined);
  render(
    <DeployStep
      gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
    />,
  );
  return complete;
};
const retry = async () => {
  fireEvent.click(await screen.findByRole("button", { name: /retry/i }));
};

it("shows wrapper failures and retries without recreating or reseeding the toolset", async () => {
  state.wrapper.mockRejectedValueOnce(new Error("Wrapper unavailable"));
  state.listWrappers.mockResolvedValue({
    mcpServers: [{ id: "server", toolsetId: "toolset" }],
  });
  const complete = renderGateway();
  expect(await screen.findByText(/Wrapper unavailable/)).toBeTruthy();
  expect(state.meta.current.toolset).toMatchObject({ id: "toolset" });
  expect(complete).not.toHaveBeenCalled();
  await retry();
  await waitFor(() => expect(complete).toHaveBeenCalledWith("server"));
  expect(state.create).toHaveBeenCalledTimes(1);
  expect(state.update).toHaveBeenCalledTimes(1);
  expect(state.wrapper).toHaveBeenCalledTimes(1);
  expect(state.listWrappers).toHaveBeenCalledWith({ toolsetId: "toolset" });
});

it("reuses a wrapper after its create response was lost", async () => {
  state.wrapper.mockRejectedValueOnce(new Error("Response lost"));
  state.listWrappers.mockResolvedValue({
    mcpServers: [{ id: "recovered", toolsetId: "toolset" }],
  });
  const complete = renderGateway();
  await retry();
  await waitFor(() => expect(complete).toHaveBeenCalledWith("recovered"));
  expect(state.create).toHaveBeenCalledTimes(1);
  expect(state.update).toHaveBeenCalledTimes(1);
  expect(state.wrapper).toHaveBeenCalledTimes(1);
});

it("keeps retries safe when wrapper reconciliation reads fail", async () => {
  state.wrapper.mockRejectedValueOnce(new Error("Response lost"));
  state.listWrappers
    .mockRejectedValueOnce(new Error("Read unavailable"))
    .mockResolvedValueOnce({
      mcpServers: [{ id: "server", toolsetId: "toolset" }],
    });
  const complete = renderGateway();
  await retry();
  expect(await screen.findByText(/Read unavailable/)).toBeTruthy();
  expect(state.wrapper).toHaveBeenCalledTimes(1);
  expect(complete).not.toHaveBeenCalled();
  await retry();
  await waitFor(() => expect(complete).toHaveBeenCalledWith("server"));
  expect(state.listWrappers).toHaveBeenCalledTimes(2);
  expect(state.create).toHaveBeenCalledTimes(1);
});

it("blocks ambiguous reconciliation instead of creating or attaching a wrapper", async () => {
  state.wrapper.mockRejectedValueOnce(new Error("Response lost"));
  state.listWrappers.mockResolvedValue({
    mcpServers: [
      { id: "one", toolsetId: "toolset" },
      { id: "two", toolsetId: "toolset" },
    ],
  });
  const complete = renderGateway();
  await retry();
  await waitFor(() => expect(state.listWrappers).toHaveBeenCalledTimes(1));
  expect(await screen.findByRole("button", { name: /retry/i })).toBeTruthy();
  expect(state.wrapper).toHaveBeenCalledTimes(1);
  expect(complete).not.toHaveBeenCalled();
});

it("resumes tool seeding after a failed update without recreating the toolset", async () => {
  state.update.mockRejectedValueOnce(new Error("Seeding unavailable"));
  const complete = renderGateway();
  await retry();
  await waitFor(() => expect(complete).toHaveBeenCalledWith("server"));
  expect(state.create).toHaveBeenCalledTimes(1);
  expect(state.update).toHaveBeenCalledTimes(2);
  expect(state.listWrappers).not.toHaveBeenCalled();
});

it("retries attachment without recreating the successful wrapper", async () => {
  const complete = vi
    .fn()
    .mockRejectedValueOnce(new Error("Attachment unavailable"))
    .mockResolvedValue(undefined);
  render(
    <DeployStep
      gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
    />,
  );
  await retry();
  await waitFor(() => expect(complete).toHaveBeenCalledTimes(2));
  expect(state.wrapper).toHaveBeenCalledTimes(1);
  expect(state.create).toHaveBeenCalledTimes(1);
});

it("fails closed after a lost toolset create response", async () => {
  state.create.mockRejectedValueOnce(new Error("Response lost"));
  const complete = renderGateway();
  expect(await screen.findByText(/manually.*toolset/i)).toBeTruthy();
  expect(screen.queryByRole("button", { name: /retry/i })).toBeNull();
  expect(state.create).toHaveBeenCalledTimes(1);
  expect(state.update).not.toHaveBeenCalled();
  expect(state.wrapper).not.toHaveBeenCalled();
  expect(complete).not.toHaveBeenCalled();
});

it.each(["create", "update", "wrapper"] as const)(
  "keeps cancellation blocked during %s and attachment",
  async (stage) => {
    let settle!: (value: unknown) => void;
    state[stage].mockReturnValueOnce(
      new Promise((resolve) => {
        settle = resolve;
      }),
    );
    const pending = vi.fn<(pending: boolean) => void>();
    let attach!: () => void;
    const complete = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          attach = resolve;
        }),
    );
    render(
      <DeployStep
        onPendingChange={pending}
        gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
      />,
    );
    await waitFor(() => expect(state[stage]).toHaveBeenCalled());
    expect(pending).toHaveBeenLastCalledWith(true);
    await act(async () => {
      settle(
        stage === "create"
          ? { id: "toolset", slug: "example", name: "Example API" }
          : { id: "server" },
      );
    });
    await waitFor(() => expect(complete).toHaveBeenCalled());
    expect(pending).toHaveBeenLastCalledWith(true);
    await act(async () => {
      attach();
    });
    expect(pending).toHaveBeenLastCalledWith(false);
  },
);

it.each(["create", "update", "wrapper"] as const)(
  "does not continue creation or attachment after unmount during %s",
  async (stage) => {
    let settle!: (value: unknown) => void;
    state[stage].mockReturnValueOnce(
      new Promise((resolve) => {
        settle = resolve;
      }),
    );
    const complete = vi.fn();
    const view = render(
      <DeployStep
        gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
      />,
    );
    await waitFor(() => expect(state[stage]).toHaveBeenCalled());
    view.unmount();
    await act(async () => {
      settle(
        stage === "create"
          ? { id: "toolset", slug: "example" }
          : { id: "server" },
      );
    });
    expect(complete).not.toHaveBeenCalled();
    if (stage === "create") expect(state.update).not.toHaveBeenCalled();
    if (stage !== "wrapper") expect(state.wrapper).not.toHaveBeenCalled();
  },
);

it("retries polling the retained deployment without evolving it again", async () => {
  state.stepState = "idle";
  state.current = true;
  state.getDeployment
    .mockRejectedValueOnce(new Error("Polling unavailable"))
    .mockResolvedValueOnce({
      id: "deployment",
      status: "completed",
      openapiv3Assets: [],
      openapiv3ToolCount: 0,
    });
  const pending = vi.fn<(pending: boolean) => void>();
  const error = vi.spyOn(console, "error").mockImplementation(() => {});
  render(
    <DeployStep
      onPendingChange={pending}
      gateway={{
        gatewayId: "gateway",
        createdServerId: null,
        complete: vi.fn(),
      }}
    />,
  );
  expect(await screen.findByText(/Polling unavailable/)).toBeTruthy();
  expect(pending).toHaveBeenLastCalledWith(false);
  expect(state.evolve).not.toHaveBeenCalled();
  await retry();
  await waitFor(() =>
    expect(state.setStepper).toHaveBeenCalledWith("completed"),
  );
  expect(state.getDeployment).toHaveBeenCalledTimes(2);
  expect(state.evolve).not.toHaveBeenCalled();
  error.mockRestore();
});

it("blocks cancellation while deployment creation is unresolved and ignores completion after unmount", async () => {
  state.stepState = "idle";
  state.current = true;
  state.meta.current.deployment = null as never;
  let settle!: (value: unknown) => void;
  state.evolve.mockReturnValue(
    new Promise((resolve) => {
      settle = resolve;
    }),
  );
  const pending = vi.fn<(pending: boolean) => void>();
  const complete = vi.fn();
  const view = render(
    <DeployStep
      onPendingChange={pending}
      gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
    />,
  );
  await waitFor(() => expect(state.evolve).toHaveBeenCalledTimes(1));
  expect(pending).toHaveBeenLastCalledWith(true);
  view.unmount();
  await act(async () => {
    settle({ deployment: { id: "deployment", status: "completed" } });
  });
  expect(state.setStep).not.toHaveBeenCalled();
  expect(state.create).not.toHaveBeenCalled();
  expect(complete).not.toHaveBeenCalled();
});

it("does not create resources from partial tools after polling fails", async () => {
  state.stepState = "idle";
  state.current = true;
  state.getDeployment.mockRejectedValue(new Error("Polling unavailable"));
  const error = vi.spyOn(console, "error").mockImplementation(() => {});
  const complete = vi.fn();
  const view = render(
    <DeployStep
      gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
    />,
  );
  await screen.findByText(/Polling unavailable/);
  state.stepState = "failed";
  await act(async () => {
    view.rerender(
      <DeployStep
        gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
      />,
    );
  });
  expect(state.create).not.toHaveBeenCalled();
  expect(complete).not.toHaveBeenCalled();
  error.mockRestore();
});

it.each(["empty", "partial"])(
  "refreshes %s cached tools after a polling retry before creating the toolset",
  async (cache) => {
    state.stepState = "idle";
    state.current = true;
    state.getDeployment
      .mockRejectedValueOnce(new Error("Polling unavailable"))
      .mockResolvedValueOnce({
        ...state.meta.current.deployment,
        status: "completed",
      });
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    let settle!: (value: unknown) => void;
    state.listTools.mockReturnValueOnce(
      new Promise((resolve) => {
        settle = resolve;
      }),
    );
    const complete = vi.fn();
    const pending = vi.fn<(pending: boolean) => void>();
    const element = (
      <DeployStep
        onPendingChange={pending}
        gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
      />
    );
    const view = render(element);
    await screen.findByText(/Polling unavailable/);
    state.stepState = "failed";
    if (cache === "empty") state.cachedTools = [];
    view.rerender(element);
    await retry();
    await waitFor(() =>
      expect(state.listTools).toHaveBeenCalledWith({
        deploymentId: "deployment",
      }),
    );
    expect(state.create).not.toHaveBeenCalled();
    expect(pending).toHaveBeenLastCalledWith(true);
    state.cachedTools = [
      { type: "http", openapiv3DocumentId: "document", toolUrn: "urn:stale" },
    ];
    view.rerender(
      <DeployStep
        onPendingChange={pending}
        gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
      />,
    );
    expect(state.create).not.toHaveBeenCalled();
    state.stepState = "completed";
    await act(async () => {
      settle({
        tools: ["urn:tool", "urn:second"].map((toolUrn) => ({
          httpToolDefinition: { toolUrn, openapiv3DocumentId: "document" },
        })),
      });
    });
    await waitFor(() => expect(complete).toHaveBeenCalledWith("server"));
    expect(state.update).toHaveBeenCalledWith({
      slug: "example",
      updateToolsetRequestBody: { toolUrns: ["urn:tool", "urn:second"] },
    });
    expect(state.evolve).not.toHaveBeenCalled();
    error.mockRestore();
  },
);

it("ignores a terminal tools response after cancellation", async () => {
  state.stepState = "idle";
  state.current = true;
  state.getDeployment.mockResolvedValue({
    ...state.meta.current.deployment,
    status: "completed",
  });
  let settle!: (value: unknown) => void;
  state.listTools.mockReturnValueOnce(
    new Promise((resolve) => {
      settle = resolve;
    }),
  );
  const complete = vi.fn();
  const pending = vi.fn<(pending: boolean) => void>();
  const view = render(
    <DeployStep
      onPendingChange={pending}
      gateway={{ gatewayId: "gateway", createdServerId: null, complete }}
    />,
  );
  await waitFor(() => expect(state.listTools).toHaveBeenCalled());
  expect(pending).toHaveBeenLastCalledWith(true);
  view.unmount();
  await act(async () => {
    settle({
      tools: [
        {
          httpToolDefinition: {
            toolUrn: "urn:tool",
            openapiv3DocumentId: "document",
          },
        },
      ],
    });
  });
  expect(state.setStep).not.toHaveBeenCalled();
  expect(state.create).not.toHaveBeenCalled();
  expect(complete).not.toHaveBeenCalled();
});
