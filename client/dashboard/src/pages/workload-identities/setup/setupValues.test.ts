import { expect, it } from "vitest";
import { groupTokenEndpoints } from "./setupValues";

function endpoint(
  id: string,
  slug: string,
  projectId = "",
  projectName = "",
): Parameters<typeof groupTokenEndpoints>[0][number] {
  return {
    userSessionIssuerId: id,
    userSessionIssuerSlug: slug,
    projectId,
    projectName,
    issuer: `https://gram.example.com/oauth/usi/${id}`,
    tokenEndpoint: `https://gram.example.com/oauth/usi/${id}/token`,
    mcpHost: "gram.example.com",
  };
}

it("groups issuers by owner, organization first, in the server's order", () => {
  const groups = groupTokenEndpoints([
    endpoint("o1", "org-issuer"),
    endpoint("p1", "alpha-issuer", "proj-a", "Alpha"),
    endpoint("p2", "beta-issuer", "proj-b", "Beta"),
    endpoint("p3", "alpha-second", "proj-a", "Alpha"),
  ]);
  expect(groups).toEqual([
    { label: "Organization", options: [{ id: "o1", label: "org-issuer" }] },
    {
      label: "Alpha",
      options: [
        { id: "p1", label: "alpha-issuer" },
        { id: "p3", label: "alpha-second" },
      ],
    },
    { label: "Beta", options: [{ id: "p2", label: "beta-issuer" }] },
  ]);
});

it("names an issuer by its id when it has no slug", () => {
  const groups = groupTokenEndpoints([endpoint("o1", "")]);
  expect(groups[0]?.options[0]?.label).toBe("o1");
});
