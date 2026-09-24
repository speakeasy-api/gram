import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SETUP_CARDS } from "../setup-cards";
import { SetupTaskContent } from "./setup-task-content";
import { StepContainer } from "./step-container";

const access = vi.hoisted(() => ({
  allowedProject: "",
  requestProject: "default",
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => access.requestProject,
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
  useRBAC: () => {
    const allowed = (_scopes: unknown, resourceId: string) =>
      !!access.allowedProject &&
      (resourceId === "org-a" || resourceId === access.allowedProject);
    return { hasAllScopes: allowed, hasAnyScope: allowed, isLoading: false };
  },
}));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  access.allowedProject = "";
  access.requestProject = "default";
});

function stubStep(key: string) {
  return vi
    .spyOn(SETUP_CARDS[key]!, "Step")
    .mockImplementation(({ onComplete, onClose }) => (
      <StepContainer
        title={key}
        description="Setup task"
        onContinue={onComplete}
        markDoneLabel="Complete"
      >
        <button onClick={onClose}>Skip</button>
      </StepContainer>
    ));
}

function renderCard(taskKey: string) {
  const props = {
    taskKey,
    projectSlug: "default",
    onComplete: vi.fn<() => void>(),
    onClose: vi.fn<() => void>(),
    onSupport: vi.fn<() => void>(),
  };
  return { props, view: render(<SetupTaskContent {...props} />) };
}

describe("SetupTaskContent", () => {
  it("renders nothing for a task without a card", () => {
    const { view } = renderCard("unknown-task");
    expect(view.container.innerHTML).toBe("");
  });

  it("wires the card's callbacks and the shared support action", () => {
    access.allowedProject = "project-default";
    stubStep("connect-idp");
    const { props } = renderCard("connect-idp");

    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    fireEvent.click(screen.getByRole("button", { name: "Get support" }));
    expect(props.onComplete).toHaveBeenCalledOnce();
    expect(props.onClose).toHaveBeenCalledOnce();
    expect(props.onSupport).toHaveBeenCalledOnce();
  });

  it.each(Object.keys(SETUP_CARDS))("does not mount %s for readers", (key) => {
    const step = stubStep(key);
    renderCard(key);
    expect(step).not.toHaveBeenCalled();
    expect(screen.getByText(/Ask an organization administrator/)).toBeTruthy();
  });

  it.each(
    Object.entries(SETUP_CARDS)
      .filter(([, card]) => card.projectScopes)
      .map(([key]) => key),
  )(
    "authorizes %s against the request project rather than another project",
    (key) => {
      access.allowedProject = "project-other";
      const step = stubStep(key);
      const { props, view } = renderCard(key);
      expect(step).not.toHaveBeenCalled();
      access.requestProject = "other";
      view.rerender(<SetupTaskContent {...props} />);
      expect(step).toHaveBeenCalled();
      step.mockClear();
      access.requestProject = "missing";
      view.rerender(<SetupTaskContent {...props} />);
      expect(step).not.toHaveBeenCalled();
    },
  );
});
