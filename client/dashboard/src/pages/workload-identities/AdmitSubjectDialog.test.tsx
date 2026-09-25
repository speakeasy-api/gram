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

function admitButton(): HTMLButtonElement {
  return screen.getByRole("button", {
    name: "Admit workload",
  }) as HTMLButtonElement;
}

it("treats a pasted terminated rule as a wildcard rather than warning", () => {
  // The dialog owns the terminator now, so a value arriving with one is read as
  // intent rather than as the mistake it used to be: the match kind switches and
  // the stem loses the star it would otherwise double.
  renderDialog([issuer({ allowWildcardAdmission: true })]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/*" },
  });

  expect(screen.queryByRole("alert")).toBeNull();
  expect((subjectField() as HTMLInputElement).value).toBe(
    "wimse://identity.example.com/org/acme/agent/",
  );
});

it("still warns about a literal star where the issuer forbids wildcards", () => {
  // The switch above is only available when the issuer permits wildcards. Where
  // it does not there is nothing to switch to, so the rule really would be an
  // exact subject containing a star — which matches nothing — and the warning has
  // to stand.
  const { onSubmit } = renderDialog([
    issuer({ allowWildcardAdmission: false }),
  ]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/*" },
  });

  const warning = screen.getByRole("alert");
  expect(warning.textContent).toContain("in full, literally");
  expect(admitButton().disabled).toBe(true);
  expect(onSubmit).not.toHaveBeenCalled();
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
  // issuer withdrawn elsewhere, or a wildcard left selected under an issuer that
  // forbids it. Either would submit a request the server must reject.
  expect(canAdmit({ ...complete, issuerExists: false })).toBe(false);
  expect(canAdmit({ ...complete, matchKindPermitted: false })).toBe(false);
});

it("does not warn about an exact subject with no star", () => {
  renderDialog([issuer()]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/a-1" },
  });

  expect(screen.queryByRole("alert")).toBeNull();
});

it("marks the subject field invalid while the rule would match nothing", () => {
  renderDialog([issuer()]);

  fireEvent.change(subjectField(), { target: { value: "repo:acme/deploy:*" } });

  expect(subjectField().getAttribute("aria-invalid")).toBe("true");
});

it("says so when the sole trusted issuer forbids wildcard matching", () => {
  // Two gates, neither implied by the other: the issuer has to permit wildcard
  // matching before a rule can ask for it, and the dialog says which state it is
  // in rather than only refusing on submit.
  renderDialog([issuer({ allowWildcardAdmission: false })]);

  expect(screen.getByText(/does not permit wildcard matching/)).toBeTruthy();
});

it("stays quiet about wildcards when the issuer permits them", () => {
  renderDialog([issuer({ allowWildcardAdmission: true })]);

  expect(screen.queryByText(/does not permit wildcard matching/)).toBeNull();
});

it("names the subjects a wildcard rule would admit", () => {
  // The caution replaces the setup-time switch on the issuer. It has to be
  // concrete to be worth reading, so it names the stem rather than warning in
  // the abstract.
  renderDialog([issuer({ allowWildcardAdmission: true })]);

  // Pasting a terminated rule is what switches the match kind — the Match control
  // is a Radix Select, so there is no native change event to fire at it.
  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/*" },
  });

  const caution = screen.getByRole("status");
  expect(caution.textContent).toContain(
    "wimse://identity.example.com/org/acme/agent/",
  );
  expect(caution.textContent).toContain("assigned by the issuer");
});

it("says nothing about breadth for an exact rule", () => {
  // An exact subject admits one identity, so there is nothing to caution about
  // and a standing warning would train the operator to ignore it.
  renderDialog([issuer({ allowWildcardAdmission: true })]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/a-1" },
  });

  expect(screen.queryByRole("status")).toBeNull();
});
