import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { afterEach, expect, it, vi } from "vitest";
import { RemoveSubjectDialog } from "./RemoveSubjectDialog";
import { removeConfirmation } from "./removeConfirmation";

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
    <RemoveSubjectDialog
      admission={machine}
      onOpenChange={() => {}}
      onConfirm={(machine) => {
        onConfirm(machine);
      }}
      isPending={false}
    />,
  );
  const remove = screen.getByRole("button", {
    name: "Remove machine",
  }) as HTMLButtonElement;
  const field = screen.getByLabelText(/to confirm/);
  return { onConfirm, remove, field };
}

it("asks for the subject, even where the machine has a label", () => {
  expect(removeConfirmation(admission())).toBe(
    "repo:acme/payments-api:ref:refs/heads/main",
  );
  expect(removeConfirmation(admission({ name: "" }))).toBe(
    "repo:acme/payments-api:ref:refs/heads/main",
  );
});

it("stays locked until the confirmation is typed exactly", () => {
  const { onConfirm, remove, field } = renderDialog(admission());

  expect(remove.disabled).toBe(true);

  // The label is not enough, and neither is a near miss on the subject.
  fireEvent.change(field, { target: { value: "Payments deploy" } });
  expect(remove.disabled).toBe(true);

  fireEvent.change(field, {
    target: { value: "repo:acme/payments-api:ref:refs/heads/mai" },
  });
  expect(remove.disabled).toBe(true);

  fireEvent.change(field, {
    target: { value: "repo:acme/payments-api:ref:refs/heads/main" },
  });
  expect(remove.disabled).toBe(false);

  fireEvent.click(remove);
  expect(onConfirm).toHaveBeenCalledTimes(1);
});

it("warns that existing sessions survive the removal", () => {
  renderDialog(admission());

  expect(
    screen.getByText(/Sessions it already holds are not revoked/),
  ).toBeTruthy();
});

it("confirms a subject stored with surrounding whitespace", () => {
  const { remove, field } = renderDialog(admission({ subject: " spaced " }));

  fireEvent.change(field, { target: { value: " spaced " } });

  expect(remove.disabled).toBe(false);
});
