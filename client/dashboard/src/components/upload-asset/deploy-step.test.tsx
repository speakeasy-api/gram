import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import {
  act,
  cleanup,
  render as renderView,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import DeployStep from "./deploy-step";
const render = (children: ReactNode) => {
  const client = new QueryClient();
  return renderView(children, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    ),
  });
};
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
  capture: vi.fn(),
  meta: {
    current: {
      assetName: "Example API",
      uploadResult: { asset: { id: "asset" } },
      deployment: {
        id: "deployment",
        openapiv3Assets: [{ id: "document", slug: "example_api" }],
      },
      existingDocument: null as { name: string; slug: string } | null,
      toolset: null,
    },
  },
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
  useSdkClient: () => ({
    deployments: {
      evolveDeployment: state.evolve,
      getById: state.getDeployment,
    },
    tools: { list: state.listTools },
    toolsets: {
      create: state.create,
      updateBySlug: state.update,
    },
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
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

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
  state.meta.current.existingDocument = null;
  state.create.mockResolvedValue({
    id: "toolset",
    slug: "example",
    name: "Example API",
  });
  state.update.mockResolvedValue({});
});
it("leaves standalone toolset creation unchanged", async () => {
  render(<DeployStep />);
  await waitFor(() => expect(state.update).toHaveBeenCalled());
});

it("preserves standalone source updates without duplicating toolsets", async () => {
  state.meta.current.existingDocument = {
    name: "Example API",
    slug: "example_api",
  };
  render(<DeployStep />);
  await act(async () => {});
  expect(state.create).not.toHaveBeenCalled();
});

it("evolves the original document slug and invalidates source caches before completing", async () => {
  state.stepState = "idle";
  state.current = true;
  state.meta.current.deployment = null as never;
  state.meta.current.existingDocument = {
    name: "Original API",
    slug: "original",
  };
  state.evolve.mockResolvedValue({
    deployment: {
      id: "new-deployment",
      status: "completed",
      openapiv3Assets: [],
    },
  });
  const invalidate = vi.spyOn(QueryClient.prototype, "invalidateQueries");
  render(<DeployStep />);
  await waitFor(() =>
    expect(state.setStepper).toHaveBeenCalledWith("completed"),
  );
  expect(state.evolve).toHaveBeenCalledWith({
    evolveForm: {
      nonBlocking: true,
      upsertOpenapiv3Assets: [
        { assetId: "asset", name: "Original API", slug: "original" },
      ],
    },
  });
  expect(invalidate).toHaveBeenCalledTimes(7);
  expect(state.create).not.toHaveBeenCalled();
});
