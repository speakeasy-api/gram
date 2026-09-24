import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SETUP_CARDS } from "../setup-cards";
import { SetupTaskContent } from "./setup-task-content";
import { StepContainer } from "./step-container";

const access = vi.hoisted(() => ({
  allowedProject: "",
  requestProject: "default",
  granted: new Set<string>(),
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
    const allowed = (scopes: string[], resourceId: string) =>
      !!access.allowedProject &&
      (resourceId === "org-a" || resourceId === access.allowedProject) &&
      scopes.every((scope) => access.granted.has(scope));
    return { hasAllScopes: allowed, hasAnyScope: allowed, isLoading: false };
  },
}));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  access.allowedProject = "";
  access.requestProject = "default";
  access.granted = new Set();
});

function grantAll() {
  access.granted = new Set([
    "org:admin",
    ...Object.values(SETUP_CARDS).flatMap((card) => card.projectScopes ?? []),
  ]);
}

function stubStep(key: string) {
  return vi
    .spyOn(SETUP_CARDS[key]!, "Step")
    .mockImplementation(({ onComplete, onClose, projectSlug }) => (
      <StepContainer
        title={`${key} in ${projectSlug}`}
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
    onComplete: vi.fn<() => void>(),
    onClose: vi.fn<() => void>(),
    onSupport: vi.fn<() => void>(),
  };
  return { props, view: render(<SetupTaskContent {...props} />) };
}

const PROJECT_CARDS = Object.entries(SETUP_CARDS)
  .filter(([, card]) => card.projectScopes)
  .map(([key]) => key);

describe("SetupTaskContent", () => {
  it("renders nothing for a task without a card", () => {
    const { view } = renderCard("unknown-task");
    expect(view.container.innerHTML).toBe("");
  });

  it.each(Object.keys(SETUP_CARDS))(
    "mounts %s for admins with its callbacks, support, and request project",
    (key) => {
      access.allowedProject = "project-other";
      access.requestProject = "other";
      grantAll();
      stubStep(key);
      const { props } = renderCard(key);

      expect(screen.getByText(`${key} in other`)).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Complete" }));
      fireEvent.click(screen.getByRole("button", { name: "Skip" }));
      fireEvent.click(screen.getByRole("button", { name: "Get support" }));
      expect(props.onComplete).toHaveBeenCalledOnce();
      expect(props.onClose).toHaveBeenCalledOnce();
      expect(props.onSupport).toHaveBeenCalledOnce();
    },
  );

  it.each(Object.keys(SETUP_CARDS))("does not mount %s for readers", (key) => {
    const step = stubStep(key);
    renderCard(key);
    expect(step).not.toHaveBeenCalled();
    expect(screen.getByText(/Ask an organization administrator/)).toBeTruthy();
  });

  it.each(Object.keys(SETUP_CARDS))(
    "does not mount %s without org:admin",
    (key) => {
      access.allowedProject = "project-default";
      grantAll();
      access.granted.delete("org:admin");
      const step = stubStep(key);
      renderCard(key);
      expect(step).not.toHaveBeenCalled();
    },
  );

  it.each(
    Object.entries(SETUP_CARDS).flatMap(([key, card]) =>
      (card.projectScopes ?? []).map((scope) => [key, scope] as const),
    ),
  )("does not mount %s without %s", (key, missing) => {
    access.allowedProject = "project-default";
    grantAll();
    access.granted.delete(missing);
    const step = stubStep(key);
    renderCard(key);
    expect(step).not.toHaveBeenCalled();
  });

  it.each(PROJECT_CARDS)(
    "authorizes %s against the request project rather than another project",
    (key) => {
      access.allowedProject = "project-other";
      grantAll();
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
