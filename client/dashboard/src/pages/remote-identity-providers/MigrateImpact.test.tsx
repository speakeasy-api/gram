import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { MigrateImpact } from "./MigrateImpact";

afterEach(cleanup);

it("explains the identity-chaining blocker and how to rebind", () => {
  render(
    <MigrateImpact
      isLoading={false}
      hasFailed={false}
      clientCount={1}
      emaBindingCount={2}
      mcpServerNames={[]}
      endpointMismatches={[]}
      conflictingMcpServerNames={[]}
      warnings={[]}
    />,
  );

  expect(screen.getByRole("alert").textContent).toContain(
    "2 active identity-chaining bindings block consolidation.",
  );
  expect(screen.getByRole("alert").textContent).toContain(
    "Explicitly unlink these bindings before consolidating",
  );
  expect(screen.getByRole("alert").textContent).toContain(
    "prepare new bindings for the target provider.",
  );
});

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
