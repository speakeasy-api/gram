import { describe, expect, it } from "vitest";
import type { IdentityProviderConnectionChecklistItem } from "@gram/client/models/components/identityproviderconnectionchecklistitem.js";

import {
  activeChecklistGroup,
  canConfirmReadiness,
  connectionStep,
  dpopObservation,
  groupChecklist,
  isConnectionChecked,
  isConnectionVerified,
} from "./connectionView";

describe("connectionStep", () => {
  it("asks for the client id until it is submitted", () => {
    expect(
      connectionStep({ status: "pending", clientIdSubmitted: false }),
    ).toBe("submit_client_id");
  });

  it("asks to verify once the client id is in", () => {
    expect(connectionStep({ status: "pending", clientIdSubmitted: true })).toBe(
      "verify",
    );
  });

  it("separates a clean verification from one that needs repair", () => {
    expect(
      connectionStep({ status: "verified", clientIdSubmitted: true }),
    ).toBe("connected");
    expect(
      connectionStep({ status: "degraded", clientIdSubmitted: true }),
    ).toBe("repair");
  });
});

describe("connection status predicates", () => {
  it("counts a degraded verification as checked", () => {
    expect(isConnectionChecked({ status: "degraded" })).toBe(true);
    expect(isConnectionChecked({ status: "pending" })).toBe(false);
  });

  it("gates the applications snapshot on a clean verification, like the server", () => {
    expect(isConnectionVerified({ status: "verified" })).toBe(true);
    expect(isConnectionVerified({ status: "degraded" })).toBe(false);
    expect(isConnectionVerified({ status: "pending" })).toBe(false);
  });

  it("accepts readiness confirmations on a degraded connection, like the server lock", () => {
    expect(canConfirmReadiness({ status: "verified" })).toBe(true);
    expect(canConfirmReadiness({ status: "degraded" })).toBe(true);
    expect(canConfirmReadiness({ status: "pending" })).toBe(false);
    expect(canConfirmReadiness({ status: "revoked" })).toBe(false);
  });
});

describe("groupChecklist", () => {
  const items: IdentityProviderConnectionChecklistItem[] = [
    {
      key: "create_api_services_app",
      group: "connect" as const,
      title: "",
      description: "",
      details: [],
      completed: true,
    },
    {
      key: "submit_client_id",
      group: "connect" as const,
      title: "",
      description: "",
      details: [],
    },
    {
      key: "register_ai_agent",
      group: "cross_app_access" as const,
      title: "",
      description: "",
      details: [],
    },
  ];

  it("orders Connect before Cross App Access and counts only observed completions", () => {
    const groups = groupChecklist(items);
    expect(groups.map((g) => g.id)).toEqual(["connect", "cross_app_access"]);
    expect(groups[0]?.completedCount).toBe(1);
    expect(groups[1]?.completedCount).toBe(0);
  });

  it("drops empty groups", () => {
    expect(groupChecklist(items.slice(0, 2)).map((g) => g.id)).toEqual([
      "connect",
    ]);
  });
});

describe("activeChecklistGroup", () => {
  it("opens Connect until verified, then the agent, and nothing once the agent steps are complete", () => {
    const step = (completed: boolean | undefined) => ({
      key: "record_ai_agent" as const,
      group: "cross_app_access" as const,
      title: "",
      description: "",
      details: [],
      completed,
    });
    const open = [step(true), step(undefined)];
    expect(activeChecklistGroup({ status: "pending", checklist: open })).toBe(
      "connect",
    );
    expect(activeChecklistGroup({ status: "degraded", checklist: open })).toBe(
      "cross_app_access",
    );
    expect(activeChecklistGroup({ status: "verified", checklist: open })).toBe(
      "cross_app_access",
    );
    expect(
      activeChecklistGroup({ status: "verified", checklist: [step(true)] }),
    ).toBeNull();
  });
});

describe("dpopObservation", () => {
  const base = {
    status: "verified" as const,
    lastError: undefined,
    checklist: [],
    dpopRequired: true,
  };
  const dpop = (completed?: boolean) => ({
    key: "dpop" as const,
    group: "connect" as const,
    title: "",
    description: "",
    details: [],
    completed,
  });

  it("is unknown before a check, after a failed check, or when unobserved", () => {
    expect(dpopObservation({ ...base, status: "pending" })).toBe("unknown");
    expect(dpopObservation({ ...base, lastError: "okta_unreachable" })).toBe(
      "unknown",
    );
    expect(dpopObservation({ ...base, checklist: [dpop()] })).toBe("unknown");
  });

  it("prefers the observed step over the stored requirement", () => {
    expect(dpopObservation(base)).toBe("protected");
    expect(dpopObservation({ ...base, dpopRequired: false })).toBe(
      "unprotected",
    );
    expect(dpopObservation({ ...base, checklist: [dpop(false)] })).toBe(
      "unprotected",
    );
  });
});
