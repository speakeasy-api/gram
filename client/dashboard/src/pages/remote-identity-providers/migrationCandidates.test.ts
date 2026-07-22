import type { OrganizationRemoteSessionIssuer } from "@gram/client/models/components/organizationremotesessionissuer.js";
import { describe, expect, it } from "vitest";

import { migrationCandidates } from "./migrationCandidates";

// The picker mirrors the server's scope ladder. Only the fields the filter
// reads are meaningful here; the rest of the issuer shape is filler.
function issuer(
  id: string,
  projectId: string,
): OrganizationRemoteSessionIssuer {
  return {
    clientCount: 0,
    issuer: {
      id,
      projectId,
      organizationId: "org-a",
      slug: id,
      issuer: "https://idp.example.com",
      oidc: false,
      passthrough: false,
      clientIdMetadataDocumentSupported: false,
      createdAt: new Date(0),
      updatedAt: new Date(0),
    },
  } as OrganizationRemoteSessionIssuer;
}

const organizational = (id: string) => issuer(id, "");

describe("migrationCandidates", () => {
  it("never offers the source as its own target", () => {
    const source = organizational("a");
    const candidates = migrationCandidates(source, [source]);
    expect(candidates).toHaveLength(0);
  });

  it("lets a project issuer target its own project or any organizational issuer", () => {
    const source = issuer("source", "project-1");
    const sameProject = issuer("same-project", "project-1");
    const otherProject = issuer("other-project", "project-2");
    const orgLevel = organizational("org-level");

    const candidates = migrationCandidates(source, [
      source,
      sameProject,
      otherProject,
      orgLevel,
    ]);

    expect(candidates.map((c) => c.issuer.id)).toEqual([
      "same-project",
      "org-level",
    ]);
  });

  it("lets an organizational issuer target only other organizational issuers", () => {
    const source = organizational("source");
    const orgLevel = organizational("org-level");
    const projectLevel = issuer("project-level", "project-1");

    const candidates = migrationCandidates(source, [
      source,
      orgLevel,
      projectLevel,
    ]);

    expect(candidates.map((c) => c.issuer.id)).toEqual(["org-level"]);
  });

  it("offers scope-legal targets whose endpoints differ, leaving parity to the preflight", () => {
    const source = issuer("source", "project-1");
    const divergent = issuer("divergent", "project-1");
    divergent.issuer.issuer = "https://other-idp.example.com";

    const candidates = migrationCandidates(source, [source, divergent]);

    expect(candidates.map((c) => c.issuer.id)).toEqual(["divergent"]);
  });
});
