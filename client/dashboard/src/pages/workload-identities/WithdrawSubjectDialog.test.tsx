import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { afterEach, expect, it, vi } from "vitest";
import { WithdrawSubjectDialog } from "./WithdrawSubjectDialog";
import { withdrawConfirmation } from "./withdrawConfirmation";

afterEach(cleanup);

function admission(
  overrides: Partial<WorkloadAdmission> = {},
): WorkloadAdmission {
  return {
    id: "33333333-3333-3333-3333-333333333333",
    organizationId: "org",
    projectId: "",
    workloadIssuerId: "11111111-1111-1111-1111-111111111111",
    issuer: "https://ci-identity.example.com",
    issuerName: "Example CI",
    subject: "repo:acme/payments-api:ref:refs/heads/main",
    matchKind: "exact",
    name: "Payments deploy",
    tags: [],
    agentId: "22222222-2222-2222-2222-222222222222",
    agentName: "Release assistant",
    wildcardActive: false,
    createdAt: new Date("2026-09-28T00:00:00Z"),
    updatedAt: new Date("2026-09-28T00:00:00Z"),
    ...overrides,
  };
}

function renderDialog(machine: WorkloadAdmission) {
  const onConfirm = vi.fn();
  render(
    <WithdrawSubjectDialog
      admission={machine}
      onOpenChange={() => {}}
      onConfirm={(machine) => {
        onConfirm(machine);
      }}
      isPending={false}
    />,
  );
  const withdraw = screen.getByRole("button", {
    name: "Withdraw machine",
  }) as HTMLButtonElement;
  const field = screen.getByLabelText(/to confirm/);
  return { onConfirm, withdraw, field };
}

it("asks for the subject, even where the machine has a label", () => {
  expect(withdrawConfirmation(admission())).toBe(
    "repo:acme/payments-api:ref:refs/heads/main",
  );
  expect(withdrawConfirmation(admission({ name: "" }))).toBe(
    "repo:acme/payments-api:ref:refs/heads/main",
  );
});

it("stays locked until the confirmation is typed exactly", () => {
  const { onConfirm, withdraw, field } = renderDialog(admission());

  expect(withdraw.disabled).toBe(true);

  // The label is not enough, and neither is a near miss on the subject.
  fireEvent.change(field, { target: { value: "Payments deploy" } });
  expect(withdraw.disabled).toBe(true);

  fireEvent.change(field, {
    target: { value: "repo:acme/payments-api:ref:refs/heads/mai" },
  });
  expect(withdraw.disabled).toBe(true);

  fireEvent.change(field, {
    target: { value: "repo:acme/payments-api:ref:refs/heads/main" },
  });
  expect(withdraw.disabled).toBe(false);

  fireEvent.click(withdraw);
  expect(onConfirm).toHaveBeenCalledTimes(1);
});

it("warns that existing sessions survive the withdrawal", () => {
  renderDialog(admission());

  expect(
    screen.getByText(/Sessions it already holds are not revoked/),
  ).toBeTruthy();
});

it("confirms a subject stored with surrounding whitespace", () => {
  const { withdraw, field } = renderDialog(admission({ subject: " spaced " }));

  fireEvent.change(field, { target: { value: " spaced " } });

  expect(withdraw.disabled).toBe(false);
});
