import { unrecognized } from "@gram/client/types/unrecognized.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { customFlows } from "./customFlowsFixture";
import { FormSheet } from "./FormSheet";

let flows = structuredClone(customFlows);
vi.mock("@gram/client/react-query/workloadCustomFlows.js", () => ({
  useWorkloadCustomFlows: () => ({
    data: flows,
    isPending: false,
    isError: false,
  }),
}));

afterEach(() => {
  cleanup();
  flows = structuredClone(customFlows);
});

it.each(["field", "format", "type"] as const)(
  "does not render a partial form or submit when an input has an unknown %s",
  (property) => {
    const issuer = flows.registerPlatform.steps
      .flatMap((step) => step.blocks)
      .find((block) => block.field === "issuer")!;
    issuer[property] = unrecognized("future_value");
    const onSubmit = vi.fn<() => void>();
    render(
      <FormSheet
        open
        onOpenChange={() => {}}
        flow="registerPlatform"
        renderers={{}}
        canSubmit
        isPending={false}
        onSubmit={onSubmit}
      />,
    );
    expect(screen.getByText("This form needs a newer dashboard")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Register" })).toBeNull();
    expect(document.querySelector("form")).toBeNull();
    expect(onSubmit).not.toHaveBeenCalled();
  },
);

it("keeps supported flows available when a different flow is unsupported", () => {
  flows.editPlatform.steps[0]!.blocks[0]!.type = unrecognized("future_block");
  const onSubmit = vi.fn<() => void>();
  render(
    <FormSheet
      open
      onOpenChange={() => {}}
      flow="registerPlatform"
      renderers={{}}
      canSubmit
      isPending={false}
      onSubmit={onSubmit}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Register" }));
  expect(onSubmit).toHaveBeenCalledOnce();
});
