import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { afterEach, expect, it, vi } from "vitest";
import { WithdrawIssuerDialog } from "./WithdrawIssuerDialog";

afterEach(cleanup);

const platform: WorkloadIssuer = {
  id: "11111111-1111-1111-1111-111111111111",
  organizationId: "org",
  projectId: "",
  name: "Example CI",
  description: "",
  issuer: "https://ci-identity.example.com",
  jwksUri: "https://ci-identity.example.com/jwks",
  allowWildcardAdmission: false,
  tags: [],
  createdAt: new Date("2026-09-28T00:00:00Z"),
  updatedAt: new Date("2026-09-28T00:00:00Z"),
};

function renderDialog(machineCount: number) {
  const onConfirm = vi.fn();
  render(
    <WithdrawIssuerDialog
      issuer={platform}
      open
      onOpenChange={() => {}}
      onConfirm={(target) => {
        onConfirm(target);
      }}
      isPending={false}
      machineCount={machineCount}
    />,
  );
  const stop = screen.getByRole("button", {
    name: "Stop trusting",
  }) as HTMLButtonElement;
  const field = screen.getByLabelText(/issuer URL to confirm/);
  return { onConfirm, stop, field };
}

it("stays locked until the issuer URL is typed exactly", () => {
  const { onConfirm, stop, field } = renderDialog(2);

  expect(stop.disabled).toBe(true);

  // The platform's name is not enough.
  fireEvent.change(field, { target: { value: "Example CI" } });
  expect(stop.disabled).toBe(true);

  fireEvent.change(field, {
    target: { value: "https://ci-identity.example.com" },
  });
  expect(stop.disabled).toBe(false);

  fireEvent.click(stop);
  expect(onConfirm).toHaveBeenCalledWith(platform);
});

it("warns that the machines under it go too", () => {
  renderDialog(2);

  expect(
    screen.getByText(/All 2 machines allowed under it are withdrawn too/),
  ).toBeTruthy();
});

it("says nothing else changes when no machine is allowed", () => {
  renderDialog(0);

  expect(screen.getByText(/nothing else changes/)).toBeTruthy();
});
