import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import DeployStep from "./deploy-step";
const state = vi.hoisted(() => ({
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
      tools: [
        { type: "http", openapiv3DocumentId: "document", toolUrn: "urn:tool" },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/deploymentLogs.js", () => ({
  useDeploymentLogs: () => ({}),
}));
vi.mock("./step/use-step", () => ({
  useStep: () => ({ state: "completed", isCurrentStep: false }),
}));
vi.mock("./stepper/use-stepper", () => ({
  useStepper: () => ({ meta: state.meta }),
}));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
afterEach(cleanup);
beforeEach(() => {
  vi.resetAllMocks();
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
  render(
    <DeployStep
      gateway={{
        gatewayId: "gateway",
        createdServerId: "server",
        complete: vi.fn(),
      }}
    />,
  );
  await new Promise((resolve) => {
    setTimeout(resolve, 0);
  });
  expect(state.create).not.toHaveBeenCalled();
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
  const complete = renderGateway();
  expect(await screen.findByText(/Wrapper unavailable/)).toBeTruthy();
  expect(state.meta.current.toolset).toMatchObject({ id: "toolset" });
  expect(complete).not.toHaveBeenCalled();
  await retry();
  await waitFor(() => expect(complete).toHaveBeenCalledWith("server"));
  expect(state.create).toHaveBeenCalledTimes(1);
  expect(state.update).toHaveBeenCalledTimes(1);
  expect(state.wrapper).toHaveBeenCalledTimes(2);
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
  state.listWrappers.mockRejectedValueOnce(new Error("Read unavailable"));
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
