import type { Employee } from "@/components/observe/insightsEmployeesData";
import { describe, expect, it } from "vitest";
import { identityHasAccount, identityKindOf } from "./identityKind";

const telemetryIdentity: Employee = {
  id: "usage:external_example",
  name: "external_example",
  email: "",
  role: "",
  status: "not_enrolled",
  tokenCount: 10,
  lastActivity: "—",
  lastActivityTimestamp: null,
  accounts: [],
  mostRecentAccount: null,
  hasPersonalAccount: false,
  roleIds: [],
  department: "",
  teams: [],
};

describe("identityKindOf", () => {
  it("does not infer an agent from an unmatched bare telemetry identifier", () => {
    expect(identityKindOf(telemetryIdentity)).toBe("unknown");
    expect(identityHasAccount(telemetryIdentity)).toBe(false);
  });

  it.each([
    { email: "person@example.com", name: "External person" },
    { email: "", name: "person@example.com" },
  ])("recognizes an unmatched person from $email / $name", (fields) => {
    const identity = { ...telemetryIdentity, ...fields };

    expect(identityKindOf(identity)).toBe("person");
    expect(identityHasAccount(identity)).toBe(false);
  });

  it("recognizes a directory member without requiring an email address", () => {
    const identity = { ...telemetryIdentity, id: "user_example" };

    expect(identityKindOf(identity)).toBe("person");
    expect(identityHasAccount(identity)).toBe(true);
  });

  it.each(["usage:external_example", "agent:agent_example", "user_example"])(
    "gives explicit registration precedence over person-like fields for %s",
    (id) => {
      const identity: Employee = {
        ...telemetryIdentity,
        id,
        registeredAgentId: "agent_example",
        email: "automation@example.com",
        name: "Automation@example.com",
      };

      expect(identityKindOf(identity)).toBe("agent");
      expect(identityHasAccount(identity)).toBe(false);
    },
  );
});
