import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { afterEach, expect, it, vi } from "vitest";
import { AdmitSubjectDialog } from "./AdmitSubjectDialog";

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

it("warns when an exact subject contains a star, and blocks the submit", () => {
  const { onSubmit } = renderDialog([issuer({ allowWildcardAdmission: true })]);

  fireEvent.change(subjectField(), {
    target: { value: "wimse://identity.example.com/org/acme/agent/*" },
  });

  // Advisory in the sense that the server also refuses it — but the submit is
  // blocked, because a rule that matches nothing reads as correct afterwards.
  const warning = screen.getByRole("alert");
  expect(warning.textContent).toContain("in full, literally");
  expect(admitButton().disabled).toBe(true);
  expect(onSubmit).not.toHaveBeenCalled();
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
