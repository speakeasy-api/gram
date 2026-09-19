import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";

import IdentityProviderLegacyRedirect from "./IdentityProviderLegacyRedirect";
import { legacyOktaTab } from "./identityProviderQueries";

afterEach(cleanup);

function Landing(): JSX.Element {
  const location = useLocation();
  return (
    <span data-testid="landing">
      {location.pathname}
      {location.search}
      {location.hash}
    </span>
  );
}

function renderAt(url: string): string {
  cleanup();
  render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route
          path="/:orgSlug/okta"
          element={<IdentityProviderLegacyRedirect />}
        />
        <Route path="/:orgSlug/identity" element={<Landing />} />
      </Routes>
    </MemoryRouter>,
  );
  return screen.getByTestId("landing").textContent ?? "";
}

describe("IdentityProviderLegacyRedirect", () => {
  it("maps the old Okta sub-tabs onto the identity concern tabs, keeping the hash", () => {
    expect(renderAt("/acme/okta?tab=cross-app-access#agent")).toBe(
      "/acme/identity?tab=enterprise-managed-auth&provider=okta&view=cross-app-access#agent",
    );
    expect(renderAt("/acme/okta?tab=connection#connection")).toBe(
      "/acme/identity?tab=enterprise-managed-auth&provider=okta&view=setup#connection",
    );
  });

  it("removes the legacy okta query parameter while preserving other parameters", () => {
    expect(
      renderAt(
        "/acme/okta?tab=cross-app-access&okta=connection&filter=active#agent",
      ),
    ).toBe(
      "/acme/identity?tab=enterprise-managed-auth&filter=active&provider=okta&view=cross-app-access#agent",
    );
  });

  it.each(["__proto__", "constructor", "toString"])(
    "defaults inherited object member %s to provider",
    (tab) => {
      expect(renderAt(`/acme/okta?tab=${tab}`)).toBe(
        "/acme/identity?tab=enterprise-managed-auth&provider=okta&view=setup",
      );
      expect(legacyOktaTab(tab)).toBe("provider");
    },
  );

  it("defaults to the provider tab for unknown or missing tabs", () => {
    expect(renderAt("/acme/okta")).toBe(
      "/acme/identity?tab=enterprise-managed-auth&provider=okta&view=setup",
    );
    expect(renderAt("/acme/okta?tab=bogus")).toBe(
      "/acme/identity?tab=enterprise-managed-auth&provider=okta&view=setup",
    );
  });
});

describe("legacyOktaTab", () => {
  it("maps known legacy tab values and defaults a missing value", () => {
    expect(legacyOktaTab("connection")).toBe("provider");
    expect(legacyOktaTab("applications")).toBe("applications");
    expect(legacyOktaTab(null)).toBe("provider");
  });
});
