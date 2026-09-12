import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import type { RemoteSessionIssuer } from "@gram/admin-client/models/components/remotesessionissuer";
import { IssuerConfiguration } from "./IssuerConfiguration";
const issuer = {
  id: "example",
  name: "",
  slug: "example",
  issuer: "https://issuer.example",
  organizationId: "",
  projectId: "",
  createdAt: new Date(),
  updatedAt: new Date(),
  oidc: false,
  passthrough: false,
  clientIdMetadataDocumentSupported: false,
} satisfies RemoteSessionIssuer;
afterEach(cleanup);
function value(label: string) {
  return screen.getByText(label, { selector: "dt" }).parentElement!;
}
it("renders stable explicit labels and nullable capability semantics", () => {
  render(<IssuerConfiguration issuer={issuer} />);
  expect(within(value("Name")).getByText("—")).toBeTruthy();
  expect(within(value("Authorization")).getByText("—")).toBeTruthy();
  expect(
    within(value("PKCE Code Challenge Methods")).getByText("Not captured"),
  ).toBeTruthy();
  expect(
    within(value("Client ID Metadata Document")).getByText("Not supported"),
  ).toBeTruthy();
});
it("omits project ownership", () => {
  render(<IssuerConfiguration issuer={issuer} />);
  expect(screen.queryByText("Project", { selector: "dt" })).toBeNull();
});
it("distinguishes explicitly empty PKCE and safely links documentation", () => {
  render(
    <IssuerConfiguration
      issuer={{
        ...issuer,
        codeChallengeMethodsSupported: [],
        clientSetupDocumentationUrl: "https://docs.example/setup",
        serviceDocumentation: "javascript:alert(1)",
      }}
    />,
  );
  expect(
    within(value("PKCE Code Challenge Methods")).getByText("None advertised"),
  ).toBeTruthy();
  const link = screen.getByRole("link", { name: "https://docs.example/setup" });
  expect(link.getAttribute("target")).toBe("_blank");
  expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  expect(
    screen.queryByRole("link", { name: "javascript:alert(1)" }),
  ).toBeNull();
  expect(screen.getByText("javascript:alert(1)")).toBeTruthy();
});
