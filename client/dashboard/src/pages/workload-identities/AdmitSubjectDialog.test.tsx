import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { afterEach, expect, it, vi } from "vitest";
import { AdmitSubjectDialog } from "./AdmitSubjectDialog";
import { canAdmit } from "./subjectRule";

afterEach(cleanup);

function issuer(overrides: Partial<WorkloadIssuer> = {}): WorkloadIssuer {
  return {
    id: "11111111-1111-1111-1111-111111111111",
    organizationId: "org",
    projectId: "",
    name: "Example",
    issuer: "https://identity.example.com",
    jwksUri: "https://identity.example.com/jwks",
    allowWildcardAdmission: false,
    createdAt: new Date("2026-09-23T00:00:00Z"),
    updatedAt: new Date("2026-09-23T00:00:00Z"),
    ...overrides,
  };
}

function renderDialog(issuers: WorkloadIssuer[]): {
  onSubmit: ReturnType<typeof vi.fn>;
} {
  const onSubmit = vi.fn();

  render(
    <AdmitSubjectDialog
      open
      onOpenChange={() => {}}
      onSubmit={(values) => {
        onSubmit(values);
      }}
      isPending={false}
      issuers={issuers}
      agents={[{ id: "22222222-2222-2222-2222-222222222222", name: "poc" }]}
    />,
  );

  return { onSubmit };
}

function subjectField(): HTMLElement {
  return screen.getByLabelText("Subject");
}

// The message bound to the subject field. The wildcard caution's Alert also
// renders role="alert", so the role alone cannot tell them apart.
function subjectWarning(): HTMLElement | null {
  return document.getElementById("admit-subject-warning");
}

function wildcardCaution(): HTMLElement | null {
  return (
    screen
      .queryByText("This rule admits more than one identity")
      ?.closest<HTMLElement>('[role="alert"]') ?? null
  );
}

it("reads a terminated subject as a wildcard, with no separate control", () => {
  // The terminator states the breadth, and the value is kept literally because
  // that is what gets stored.
  renderDialog([issuer({ allowWildcardAdmission: true })]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/*" },
  });

  // No validation message: the rule is well formed. The caution is a separate
  // Alert and is asserted in its own test.
  expect(subjectWarning()).toBeNull();
  expect((subjectField() as HTMLInputElement).value).toBe(
    "wimse://identity.example.com/org/acme/agent/*",
  );
  expect(screen.queryByLabelText("Match")).toBeNull();
});

it("refuses a wildcard rule where the issuer forbids wildcards", () => {
  renderDialog([issuer({ allowWildcardAdmission: false })]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/*" },
  });

  // The submit gate is asserted directly below: with no agent chosen the
  // button is disabled regardless of this warning.
  const warning = subjectWarning();
  expect(warning?.textContent).toContain("does not permit wildcard matching");
});

it("blocks the submit on the warning alone, with everything else filled in", () => {
  // Driven through the button this cannot fail: canAdmit also requires an agent,
  // which the dialog's Radix select makes awkward to choose in jsdom, so the
  // button is disabled either way. Asserting the gate directly is what catches
  // the warning being dropped from it.
  const complete = {
    issuer: "https://identity.example.com",
    subject: "wimse://identity.example.com/org/acme/agent/a-1",
    agentId: "22222222-2222-2222-2222-222222222222",
    warning: null,
    issuerExists: true,
    matchKindPermitted: true,
  };

  expect(canAdmit(complete)).toBe(true);
  expect(canAdmit({ ...complete, warning: "would admit nothing" })).toBe(false);
  // Both guard stale dialog state rather than anything the user can see: an
  // issuer withdrawn elsewhere, or a wildcard rule kept after switching to an
  // issuer that forbids it. Either would submit a request the server must reject.
  expect(canAdmit({ ...complete, issuerExists: false })).toBe(false);
  expect(canAdmit({ ...complete, matchKindPermitted: false })).toBe(false);
});

it("does not warn about an exact subject with no star", () => {
  renderDialog([issuer()]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/a-1" },
  });

  expect(subjectWarning()).toBeNull();
});

it("marks the subject field invalid while the rule cannot be admitted", () => {
  // The default fixture issuer forbids wildcards, so a terminated subject is a
  // rule this issuer cannot carry.
  renderDialog([issuer()]);

  fireEvent.change(subjectField(), { target: { value: "repo:acme/deploy:*" } });

  expect(subjectField().getAttribute("aria-invalid")).toBe("true");
});

it("marks it invalid for a malformed wildcard too", () => {
  renderDialog([issuer({ allowWildcardAdmission: true })]);

  // An interior star: read as a wildcard whose terminator is misplaced, which is
  // the more useful of the two possible messages.
  fireEvent.change(subjectField(), { target: { value: "repo:acme/*/deploy" } });

  expect(subjectField().getAttribute("aria-invalid")).toBe("true");
  expect(subjectWarning()?.textContent).toContain('must end in "*"');
});

it("says nothing about wildcards until the subject asks for one", () => {
  // With no terminator typed there is nothing to report, and a standing notice
  // would be noise.
  renderDialog([issuer({ allowWildcardAdmission: false })]);

  expect(screen.queryByText(/does not permit wildcard matching/)).toBeNull();
});

it("names the subjects a wildcard rule would admit", () => {
  // The caution names the stem rather than warning in the abstract.
  renderDialog([issuer({ allowWildcardAdmission: true })]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/*" },
  });

  const caution = wildcardCaution();
  expect(caution?.textContent).toContain(
    "wimse://identity.example.com/org/acme/agent/",
  );
  expect(caution?.textContent).toContain("assigned by the issuer");
});

it("says nothing about breadth for an exact rule", () => {
  // An exact subject admits one identity, so there is nothing to caution about
  // and a standing warning would train the operator to ignore it.
  renderDialog([issuer({ allowWildcardAdmission: true })]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/a-1" },
  });

  expect(wildcardCaution()).toBeNull();
});
