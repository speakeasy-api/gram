import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { AnthropicInferenceHooksStep } from "../steps/anthropic-inference-hooks-step";
import { TaskStep, TaskStepContent, type TaskStepProps } from "./task-step";
import { ONBOARDING_TASKS, ONBOARDING_WORKSTREAMS } from "./tasks";
import { SETUP_TASK_SLUGS } from "../../task-slugs";
const protectedHook = vi.hoisted(() => vi.fn());
vi.mock("../steps/anthropic-inference-hooks-step", () => ({
  AnthropicInferenceHooksStep: () => {
    protectedHook();
    return null;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-a" }),
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAllScopes: () => false,
    hasAnyScope: () => false,
    isLoading: false,
  }),
}));
vi.mock("../enable-logging-section", () => ({
  EnableLoggingSection: () => {
    protectedHook();
    return null;
  },
}));
afterEach(() => {
  cleanup();
  protectedHook.mockClear();
});
vi.mock("../steps", () =>
  Object.fromEntries(
    [
      "AdditionalAgentConfigStep",
      "AnthropicAdminControlsStep",
      "AnthropicObservabilityStep",
      "ConfigurePoliciesStep",
      "ConfirmTrafficStep",
      "ConnectIdpStep",
      "CreateMarketplaceStep",
      "DirectorySyncStep",
      "DistributeServersStep",
      "IdentityProviderStep",
      "InstrumentAgentsStep",
      "PlatformMCPSetupStep",
    ].map((name) => [
      name,
      () => {
        protectedHook();
        return null;
      },
    ]),
  ),
);
describe("workstream task coverage", () => {
  it.each(ONBOARDING_TASKS)(
    "does not mount protected $id hooks for readers",
    ({ id }) => {
      render(
        <TaskStep
          taskId={id}
          onComplete={vi.fn<() => void>()}
          onClose={vi.fn<() => void>()}
        />,
      );
      expect(protectedHook).not.toHaveBeenCalled();
      expect(
        screen.getByText(/Ask an organization administrator/),
      ).toBeTruthy();
    },
  );
  it("routes Anthropic observability to the inference-hook setup flow", () => {
    const onComplete = vi.fn<TaskStepProps["onComplete"]>();
    const onClose = vi.fn<TaskStepProps["onClose"]>();
    const step = TaskStepContent({
      taskId: "anthropic-observability",
      onComplete,
      onClose,
    });

    expect(step.type).toBe(AnthropicInferenceHooksStep);
    expect(step.props.onComplete).toBe(onComplete);
  });
  it("includes every main task and every server-supported key", () => {
    const ids = ONBOARDING_TASKS.map((task) => task.id);
    const server = readFileSync(
      "../../server/internal/organizations/setup_tasks.go",
      "utf8",
    );
    const catalog = server
      .split("var setupTaskCatalog = []setupTaskDefinition{")[1]!
      .split("\n}")[0]!;
    for (const key of [
      ...Object.keys(SETUP_TASK_SLUGS),
      ...Array.from(catalog.matchAll(/Key: "([^"]+)"/g), (match) => match[1]),
    ])
      expect(ids).toContain(key);
  });
  it.each(ONBOARDING_TASKS)(
    "renders $id and places it in exactly one workstream",
    (task) => {
      expect(
        ONBOARDING_WORKSTREAMS.flatMap((stream) => stream.taskIds).filter(
          (id) => id === task.id,
        ),
      ).toHaveLength(1);
      expect(
        TaskStepContent({
          taskId: task.id,
          onComplete: vi.fn<TaskStepProps["onComplete"]>(),
          onClose: vi.fn<TaskStepProps["onClose"]>(),
        }),
      ).toBeTruthy();
    },
  );
});
