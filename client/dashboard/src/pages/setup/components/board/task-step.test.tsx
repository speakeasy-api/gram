import { describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { AnthropicObservabilityStep } from "../steps";
import { TaskStep, type TaskStepProps } from "./task-step";
import { ONBOARDING_TASKS, ONBOARDING_WORKSTREAMS } from "./tasks";
import { SETUP_TASK_SLUGS } from "../../task-slugs";
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
    ].map((name) => [name, () => null]),
  ),
);
describe("workstream task coverage", () => {
  it("routes Anthropic observability to the shared platform setup step", () => {
    const onComplete = vi.fn<TaskStepProps["onComplete"]>();
    const onClose = vi.fn<TaskStepProps["onClose"]>();
    const step = TaskStep({
      taskId: "anthropic-observability",
      onComplete,
      onClose,
      onOpenTask: vi.fn<TaskStepProps["onOpenTask"]>(),
    });

    expect(step.type).toBe(AnthropicObservabilityStep);
    expect(step.props.onComplete).toBe(onComplete);
    expect(step.props.onBack).toBe(onClose);
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
      expect(ids).toContain(key === "enable-logging" ? "confirm-traffic" : key);
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
        TaskStep({
          taskId: task.id,
          onComplete: vi.fn<TaskStepProps["onComplete"]>(),
          onClose: vi.fn<TaskStepProps["onClose"]>(),
          onOpenTask: vi.fn<TaskStepProps["onOpenTask"]>(),
        }),
      ).toBeTruthy();
    },
  );
});
