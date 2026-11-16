import type { GramCore } from "@gram/client/core.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  lookUpProvider,
  normalizeIssuer,
  ProviderLookupError,
  providerSlug,
  turnOffEma,
  turnOnEma,
} from "./ema";

const mocks = vi.hoisted(() => ({
  fetchMetadata: vi.fn(),
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
}));

const ok = (value: unknown) => Promise.resolve({ ok: true, value });

vi.mock(
  "@gram/client/funcs/organizationRemoteSessionIssuersFetchMetadata.js",
  () => ({
    organizationRemoteSessionIssuersFetchMetadata: mocks.fetchMetadata,
  }),
);
vi.mock("@gram/client/funcs/organizationRemoteSessionIssuersList.js", () => ({
  organizationRemoteSessionIssuersList: mocks.list,
}));
vi.mock("@gram/client/funcs/organizationRemoteSessionIssuersCreate.js", () => ({
  organizationRemoteSessionIssuersCreate: mocks.create,
}));
vi.mock("@gram/client/funcs/organizationUserSessionIssuersUpdate.js", () => ({
  organizationUserSessionIssuersUpdate: mocks.update,
}));

const core = {} as GramCore;
const provider = { id: "usi-1", slug: "acme-workforce" } as UserSessionIssuer;
const draft = {
  issuer: "https://login.example.com",
  jwksUri: "https://login.example.com/keys",
  discoveryWarnings: [],
  oidc: true,
  passthrough: false,
  clientIdMetadataDocumentSupported: false,
  authorizationResponseIssParameterSupported: false,
  backchannelLogoutSupported: false,
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.update.mockImplementation(() => ok({ ...provider }));
});

describe("normalizeIssuer", () => {
  it.each([
    ["login.example.com", "https://login.example.com"],
    ["https://login.example.com/", "https://login.example.com"],
    [
      "https://example.com/oauth2/default",
      "https://example.com/oauth2/default",
    ],
  ])("reads %s as %s", (address, issuer) => {
    expect(normalizeIssuer(address)).toBe(issuer);
  });

  it.each(["", "http://login.example.com"])("refuses %j", (address) => {
    expect(normalizeIssuer(address)).toBeUndefined();
  });
});

describe("providerSlug", () => {
  it("names the provider after its host, avoiding taken slugs", () => {
    expect(providerSlug("https://acme.okta.com", new Set())).toBe(
      "acme-okta-com",
    );
    expect(
      providerSlug("https://acme.okta.com", new Set(["acme-okta-com"])),
    ).toBe("acme-okta-com-2");
  });
});

describe("lookUpProvider", () => {
  it("refuses a provider that publishes no signing keys", async () => {
    mocks.fetchMetadata.mockImplementation(() =>
      ok({ ...draft, jwksUri: undefined }),
    );
    await expect(lookUpProvider(core, "login.example.com")).rejects.toThrow(
      /doesn't publish signing keys/,
    );
  });

  it("says when nothing answers at the address", async () => {
    mocks.fetchMetadata.mockImplementation(() =>
      Promise.resolve({ ok: false, error: new Error("boom") }),
    );
    await expect(
      lookUpProvider(core, "login.example.com"),
    ).rejects.toBeInstanceOf(ProviderLookupError);
  });
});

describe("turnOnEma", () => {
  const found = { issuer: "https://login.example.com", draft };

  it("reuses the organization's record of the identity provider", async () => {
    mocks.list.mockImplementation(() =>
      ok({
        result: {
          items: [
            {
              issuer: {
                id: "remote-1",
                projectId: "",
                slug: "login",
                issuer: "https://login.example.com/",
              },
            },
          ],
        },
      }),
    );
    await turnOnEma(core, provider, found);
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.update).toHaveBeenCalledWith(core, {
      updateOrganizationUserSessionIssuerForm: {
        id: "usi-1",
        trustedRemoteSessionIssuerId: "remote-1",
      },
    });
  });

  it("adds the identity provider when the organization has none for it", async () => {
    mocks.list.mockImplementation(() => ok({ result: { items: [] } }));
    mocks.create.mockImplementation(() => ok({ id: "remote-new" }));
    await turnOnEma(core, provider, found);
    expect(mocks.create).toHaveBeenCalledWith(core, {
      createIssuerRequestBody: expect.objectContaining({
        issuer: "https://login.example.com",
        slug: "login-example-com",
        jwksUri: "https://login.example.com/keys",
      }),
    });
    expect(mocks.update).toHaveBeenCalledWith(core, {
      updateOrganizationUserSessionIssuerForm: {
        id: "usi-1",
        trustedRemoteSessionIssuerId: "remote-new",
      },
    });
  });
});

describe("turnOffEma", () => {
  it("clears the trusted identity provider", async () => {
    await turnOffEma(core, provider);
    expect(mocks.update).toHaveBeenCalledWith(core, {
      updateOrganizationUserSessionIssuerForm: {
        id: "usi-1",
        trustedRemoteSessionIssuerId: "",
      },
    });
  });
});
