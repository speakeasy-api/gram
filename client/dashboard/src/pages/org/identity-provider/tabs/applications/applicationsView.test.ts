import { describe, expect, it } from "vitest";

import { humanizeOktaToken } from "./applicationsView";

describe("humanizeOktaToken", () => {
  it("humanizes Okta tokens with known spellings kept", () => {
    expect(humanizeOktaToken("OPENID_CONNECT")).toBe("OpenID Connect");
    expect(humanizeOktaToken("SAML_2_0")).toBe("SAML 2.0");
    expect(humanizeOktaToken("ACTIVE")).toBe("Active");
    expect(humanizeOktaToken("SOME_NEW_MODE")).toBe("Some new mode");
  });
});
