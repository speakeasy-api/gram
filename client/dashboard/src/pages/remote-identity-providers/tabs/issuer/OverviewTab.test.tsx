import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OverviewTab } from "./OverviewTab";

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1", projects: [] }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "default" }),
}));
vi.mock("@gram/client/react-query/listProjects.js", () => ({
  useListProjects: () => ({ data: undefined }),
}));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

afterEach(cleanup);

function issuer(omitScopeFallback?: boolean): RemoteSessionIssuer {
  return {
    id: "issuer-1",
    slug: "example-idp",
    issuer: "https://idp.example.com",
    scopesSupported: ["openid"],
    omitScopeFallback,
  } as RemoteSessionIssuer;
}

function value(label: string): HTMLElement {
  return screen.getByText(label).closest<HTMLElement>("[data-info-field]")!;
}

describe("issuer overview scope fallback", () => {
  // NULL and false both read as the default.
  it("reads an unset or false setting as sending the supported scopes", () => {
    render(<OverviewTab issuer={issuer()} />);
    expect(value("Scope fallback").textContent).toContain(
      "Every advertised scope",
    );
    cleanup();
    render(<OverviewTab issuer={issuer(false)} />);
    expect(value("Scope fallback").textContent).toContain(
      "Every advertised scope",
    );
  });

  it("reads true as sending none", () => {
    render(<OverviewTab issuer={issuer(true)} />);
    expect(value("Scope fallback").textContent).toContain(
      "Authorization server defaults",
    );
  });
});
