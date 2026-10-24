import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { afterEach, expect, it, vi } from "vitest";
import {
  AssertionReuseField,
  WorkloadIssuerPolicy,
} from "./WorkloadIssuerPolicy";

const mocks = vi.hoisted(() => ({ update: vi.fn() }));
vi.mock("@gram/client/react-query/updateWorkloadIssuer.js", () => ({
  useUpdateWorkloadIssuerMutation: () => ({
    mutate: mocks.update,
    isPending: false,
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));

const issuer = {
  id: "issuer_example",
  name: "Example cluster",
  allowAssertionReuse: false,
} as WorkloadIssuer;
function setup(allowAssertionReuse = false) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <WorkloadIssuerPolicy issuer={{ ...issuer, allowAssertionReuse }} />
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  mocks.update.mockReset();
});

it("requires explicit opt-in and save for the exact issuer", () => {
  setup();
  expect(screen.getByText("Assertion reuse off (jti/uti)")).toBeTruthy();
  fireEvent.click(
    screen.getByRole("button", { name: "Edit Example cluster issuer policy" }),
  );
  const checkbox = screen.getByRole("checkbox", {
    name: "Allow assertion reuse",
  });
  expect((checkbox as HTMLInputElement).checked).toBe(false);
  fireEvent.click(checkbox);
  expect(mocks.update).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save issuer policy" }));
  expect(mocks.update).toHaveBeenCalledWith({
    request: {
      updateWorkloadIssuerForm: { id: issuer.id, allowAssertionReuse: true },
    },
  });
});

it("loads the saved opt-in and explicitly sends false to restore single-use", () => {
  setup(true);
  expect(screen.getByText("Assertion reuse allowed")).toBeTruthy();
  fireEvent.click(
    screen.getByRole("button", { name: "Edit Example cluster issuer policy" }),
  );
  const checkbox = screen.getByRole("checkbox", {
    name: "Allow assertion reuse",
  });
  expect((checkbox as HTMLInputElement).checked).toBe(true);
  fireEvent.click(checkbox);
  fireEvent.click(screen.getByRole("button", { name: "Save issuer policy" }));
  expect(mocks.update).toHaveBeenCalledWith({
    request: {
      updateWorkloadIssuerForm: { id: issuer.id, allowAssertionReuse: false },
    },
  });
});

it("does not save a cancelled opt-in", () => {
  setup();
  fireEvent.click(
    screen.getByRole("button", { name: "Edit Example cluster issuer policy" }),
  );
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(mocks.update).not.toHaveBeenCalled();
});

it("explains projected tokens, retained checks, and issuer-wide impact", () => {
  render(<AssertionReuseField checked={false} onChange={vi.fn()} />);
  expect(screen.getByText(/projected service account tokens/)).toBeTruthy();
  expect(screen.getByText(/jti or uti are single-use/)).toBeTruthy();
  expect(screen.getByText(/every workload using this issuer/)).toBeTruthy();
});
