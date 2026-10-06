import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { MigrateImpact } from "./MigrateImpact";

afterEach(cleanup);

it.each([
  [1, "binding blocks", "this binding", "a new binding"],
  [2, "bindings block", "these bindings", "new bindings"],
] as const)(
  "explains the identity-chaining blocker for %i bindings",
  (count, blocker, existing, replacement) => {
    render(
      <MigrateImpact
        isLoading={false}
        hasFailed={false}
        clientCount={1}
        emaBindingCount={count}
        mcpServerNames={[]}
        endpointMismatches={[]}
        conflictingMcpServerNames={[]}
        warnings={[]}
      />,
    );

    expect(screen.getByRole("alert").textContent).toContain(
      `${count} active identity-chaining ${blocker} consolidation.`,
    );
    expect(screen.getByRole("alert").textContent).toContain(
      `Explicitly unlink ${existing} before consolidating`,
    );
    expect(screen.getByRole("alert").textContent).toContain(
      `prepare ${replacement} for the target provider.`,
    );
  },
);

it("does not report an identity-chaining blocker when none exists", () => {
  render(
    <MigrateImpact
      isLoading={false}
      hasFailed={false}
      clientCount={1}
      emaBindingCount={0}
      mcpServerNames={[]}
      endpointMismatches={[]}
      conflictingMcpServerNames={[]}
      warnings={[]}
    />,
  );

  expect(screen.queryByRole("alert")).toBeNull();
});
