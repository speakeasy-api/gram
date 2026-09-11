import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { StepSupportProvider } from "../step-container";
import { AnthropicInferenceHooksStep } from "../steps/anthropic-inference-hooks-step";
import { TaskStep, TaskStepContent, type TaskStepProps } from "./task-step";
import { ONBOARDING_TASKS, ONBOARDING_WORKSTREAMS } from "./tasks";
import { SETUP_TASK_SLUGS } from "../../task-slugs";
const protectedHook = vi.hoisted(() => vi.fn());
const access = vi.hoisted(() => ({
  allowedProject: "",
  requestProject: "default",
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => access.requestProject,
}));
vi.mock("../steps/anthropic-inference-hooks-step", () => ({
  AnthropicInferenceHooksStep: () => {
    protectedHook();
    return null;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org-a",
    projects: [
      { id: "project-default", slug: "default" },
      { id: "project-other", slug: "other" },
    ],
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAllScopes: (_scopes: unknown, resourceId: string) =>
      !!access.allowedProject &&
      (resourceId === "org-a" || resourceId === access.allowedProject),
    hasAnyScope: (_scopes: unknown, resourceId: string) =>
      !!access.allowedProject &&
      (resourceId === "org-a" || resourceId === access.allowedProject),
    isLoading: false,
  }),
}));
vi.mock("../enable-logging-section", () => ({
  EnableLoggingSection: ({ index }: { index: number }) => {
    protectedHook();
    return <span>Logging section {index}</span>;
  },
}));
afterEach(() => {
  cleanup();
  protectedHook.mockClear();
  access.allowedProject = "";
  access.requestProject = "default";
});
vi.mock("../steps", () =>
  Object.fromEntries(
    [
      "AdditionalAgentConfigStep",
      "AnthropicAdminControlsStep",
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
  it.each(["distribute-servers", "anthropic-observability"] as const)(
    "authorizes %s against the request project rather than another project",
    (taskId) => {
      access.allowedProject = "project-other";
      const props = {
        taskId,
        onComplete: vi.fn<() => void>(),
        onClose: vi.fn<() => void>(),
      };
      const view = render(<TaskStep {...props} />);
      expect(protectedHook).not.toHaveBeenCalled();
      access.requestProject = "other";
      view.rerender(<TaskStep {...props} />);
      expect(protectedHook).toHaveBeenCalledOnce();
      protectedHook.mockClear();
      access.requestProject = "missing";
      view.rerender(<TaskStep {...props} />);
      expect(protectedHook).not.toHaveBeenCalled();
    },
  );
  it("gives logging a one-based section and support footer", () => {
    const support = vi.fn<() => void>();
    const complete = vi.fn<() => void>();
    render(
      <StepSupportProvider onSupport={support}>
        <TaskStepContent
          taskId="enable-logging"
          onComplete={complete}
          onClose={() => {}}
        />
      </StepSupportProvider>,
    );
    expect(screen.getByText("Logging section 1")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Get support" }));
    expect(support).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(complete).toHaveBeenCalledOnce();
  });
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
      resolve(
        import.meta.dirname,
        "../../../../../../../server/internal/organizations/setup_tasks.go",
      ),
      "utf8",
    );
    const catalog = server.match(
      /var setupTaskCatalog = \[\]setupTaskDefinition\{([\s\S]*?)^\}/m,
    )?.[1];
    expect(catalog).toBeDefined();
    const serverKeys = Array.from(
      catalog!.matchAll(/\bKey:\s*"([^"]+)"/g),
      (match) => match[1],
    );
    expect(serverKeys.length).toBeGreaterThan(0);
    for (const key of [...Object.keys(SETUP_TASK_SLUGS), ...serverKeys])
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
