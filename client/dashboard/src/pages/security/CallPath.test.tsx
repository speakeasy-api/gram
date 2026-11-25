import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { CallPath } from "./CallPath";

afterEach(cleanup);
it("does not label an unknown phase as a scanned request or response", () => {
  const result: RiskResult = {
    createdAt: new Date(0),
    id: "finding-test",
    policyId: "policy-test",
    policyVersion: 1,
    source: "test",
    phase: unrecognized("future_phase"),
    enforcementOutcome: "logged",
  };
  render(
    <CallPath
      result={result}
      siblings={[result]}
      serverName="Example server"
      surfaceLabel="Policy scan"
      rating={null}
    />,
  );
  expect(screen.getByText("Phase unknown")).toBeDefined();
  expect(screen.queryByText(/Scanned (request|response)/)).toBeNull();
  expect(screen.queryByText(/forwarded|delivered/)).toBeNull();
});
