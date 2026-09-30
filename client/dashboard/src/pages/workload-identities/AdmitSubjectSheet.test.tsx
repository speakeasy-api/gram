import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { afterEach, expect, it } from "vitest";
import { AdmitSubjectSheet } from "./AdmitSubjectSheet";
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
    description: "",
    allowWildcardAdmission: false,
    tags: [],
    createdAt: new Date("2026-09-23T00:00:00Z"),
    updatedAt: new Date("2026-09-23T00:00:00Z"),
    ...overrides,
  };
}

function renderSheet(workloadIssuer: WorkloadIssuer): void {
  render(
    <AdmitSubjectSheet
      open
      onOpenChange={() => {}}
      onSubmit={() => {}}
      isPending={false}
      issuer={workloadIssuer}
      agents={[{ id: "22222222-2222-2222-2222-222222222222", name: "poc" }]}
    />,
  );
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
  renderSheet(issuer({ allowWildcardAdmission: true }));

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
  renderSheet(issuer({ allowWildcardAdmission: false }));

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/*" },
  });

  const warning = subjectWarning();
  expect(warning?.textContent).toContain("does not permit wildcard matching");
});

it("blocks the submit on the warning alone, with everything else filled in", () => {
  // Driven through the button this cannot fail: canAdmit also requires an agent,
  // which the sheet's Radix select makes awkward to choose in jsdom, so the
  // button is disabled either way. Asserting the gate directly is what catches
  // the warning being dropped from it.
  const complete = {
    subject: "wimse://identity.example.com/org/acme/agent/a-1",
    agentId: "22222222-2222-2222-2222-222222222222",
    warning: null,
    matchKindPermitted: true,
  };

  expect(canAdmit(complete)).toBe(true);
  expect(canAdmit({ ...complete, warning: "would admit nothing" })).toBe(false);
  // A wildcard rule under an issuer that forbids it would submit a request the
  // server must reject.
  expect(canAdmit({ ...complete, matchKindPermitted: false })).toBe(false);
});

it("does not warn about an exact subject with no star", () => {
  renderSheet(issuer());

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/a-1" },
  });

  expect(subjectWarning()).toBeNull();
});

it("marks the subject field invalid while the rule cannot be admitted", () => {
  // The default fixture issuer forbids wildcards, so a terminated subject is a
  // rule this issuer cannot carry.
  renderSheet(issuer());

  fireEvent.change(subjectField(), { target: { value: "repo:acme/deploy:*" } });

  expect(subjectField().getAttribute("aria-invalid")).toBe("true");
});

it("marks it invalid for a malformed wildcard too", () => {
  renderSheet(issuer({ allowWildcardAdmission: true }));

  // An interior star: read as a wildcard whose terminator is misplaced, which is
  // the more useful of the two possible messages.
  fireEvent.change(subjectField(), { target: { value: "repo:acme/*/deploy" } });

  expect(subjectField().getAttribute("aria-invalid")).toBe("true");
  expect(subjectWarning()?.textContent).toContain('must end in "*"');
});

it("says nothing about wildcards until the subject asks for one", () => {
  // With no terminator typed there is nothing to report, and a standing notice
  // would be noise.
  renderSheet(issuer({ allowWildcardAdmission: false }));

  expect(screen.queryByText(/does not permit wildcard matching/)).toBeNull();
});

it("names the subjects a wildcard rule would admit", () => {
  // The caution names the stem rather than warning in the abstract.
  renderSheet(issuer({ allowWildcardAdmission: true }));

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
  renderSheet(issuer({ allowWildcardAdmission: true }));

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/a-1" },
  });

  expect(wildcardCaution()).toBeNull();
});

it("admits under the page's platform without asking which issuer", () => {
  // The sheet opens from one platform's page, so the issuer is already decided.
  renderSheet(issuer());

  expect(screen.queryByLabelText("Issuer")).toBeNull();
  expect(screen.queryByText("Select a trusted issuer")).toBeNull();
});

it("warns in the sheet about whitespace before the terminator", () => {
  renderSheet(issuer({ allowWildcardAdmission: true }));

  // The space is stored as part of the subject, so the rule would match
  // nothing, and it is invisible in the field.
  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/ *" },
  });

  expect(subjectWarning()?.textContent).toContain("whitespace before");
  expect(subjectField().getAttribute("aria-invalid")).toBe("true");
});

function renderEdit(agentId: string): { submitted: () => number } {
  let submissions = 0;
  render(
    <AdmitSubjectSheet
      open
      onOpenChange={() => {}}
      onSubmit={() => {
        submissions += 1;
      }}
      isPending={false}
      issuer={issuer()}
      agents={[{ id: "22222222-2222-2222-2222-222222222222", name: "poc" }]}
      initial={{
        subject: "wimse://identity.example.com/org/acme/agent/a-1",
        name: "Deploy bot",
        tags: [],
        agentId,
        agentName: "",
      }}
    />,
  );
  return { submitted: () => submissions };
}

function saveButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "Save changes" });
}

it("saves a label change on access that has no agent assigned", () => {
  const { submitted } = renderEdit("");

  expect(saveButton().disabled).toBe(true);
  fireEvent.change(screen.getByLabelText("Label (optional)"), {
    target: { value: "Release bot" },
  });

  expect(saveButton().disabled).toBe(false);
  fireEvent.click(saveButton());
  expect(submitted()).toBe(1);
});

it("saves a label change while the assigned agent is no longer active", () => {
  renderEdit("33333333-3333-3333-3333-333333333333");

  fireEvent.change(screen.getByLabelText("Label (optional)"), {
    target: { value: "Release bot" },
  });

  expect(saveButton().disabled).toBe(false);
});

it("requires an agent before allowing new access", () => {
  renderSheet(issuer());

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/a-1" },
  });

  const allow = screen.getByRole<HTMLButtonElement>("button", {
    name: "Allow access",
  });
  expect(allow.disabled).toBe(true);
});
