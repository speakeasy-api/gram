import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SetupSteps } from "./SetupSteps";

afterEach(cleanup);

const steps = [
  { id: "intro", title: "Before you start" },
  { id: "values", title: "Your values" },
  { id: "done", title: "Finish up" },
];

function renderSteps(
  props: Partial<Parameters<typeof SetupSteps<(typeof steps)[number]>>[0]>,
) {
  return render(
    <SetupSteps
      steps={steps}
      activeStepId="intro"
      isReachable={() => true}
      onStepChange={() => {}}
      renderStep={(step) => <p>{`Body of ${step.id}`}</p>}
      footer={<button type="button">Continue</button>}
      {...props}
    />,
  );
}

it("marks the active step and counts it", () => {
  renderSteps({ activeStepId: "values" });
  expect(
    screen
      .getByRole("button", { name: "Step 2: Your values" })
      .getAttribute("aria-current"),
  ).toBe("step");
  expect(screen.getByText("2/3")).toBeTruthy();
  expect(screen.getByText("Step 2")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Continue" })).toBeTruthy();
});

it("keeps off-screen steps mounted but inert", () => {
  renderSteps({ activeStepId: "values" });
  const bodyOf = (id: string) =>
    screen.getByText(`Body of ${id}`).parentElement!;
  expect(bodyOf("values").hasAttribute("inert")).toBe(false);
  expect(bodyOf("intro").hasAttribute("inert")).toBe(true);
  expect(bodyOf("done").hasAttribute("inert")).toBe(true);
});

it("shows the first step for an unknown id", () => {
  renderSteps({ activeStepId: "nope" });
  expect(
    screen
      .getByRole("button", { name: "Step 1: Before you start" })
      .getAttribute("aria-current"),
  ).toBe("step");
});

it("disables the dash of a step that cannot be shown", () => {
  const onStepChange = vi.fn<(id: string) => void>();
  renderSteps({ isReachable: (index) => index < 2, onStepChange });
  const finish = screen.getByRole("button", {
    name: "Step 3: Finish up",
  }) as HTMLButtonElement;
  expect(finish.disabled).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Step 2: Your values" }));
  expect(onStepChange).toHaveBeenCalledWith("values");
});

it("leaves out the progress for a single step", () => {
  renderSteps({ steps: [steps[0]!] });
  expect(screen.queryByRole("button", { name: /^Step / })).toBeNull();
  expect(screen.queryByText("1/1")).toBeNull();
  expect(screen.queryByText("Step 1")).toBeNull();
  expect(screen.getByText("Before you start")).toBeTruthy();
  expect(screen.getByText("Body of intro")).toBeTruthy();
});
