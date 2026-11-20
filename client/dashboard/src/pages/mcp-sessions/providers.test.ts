import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { describe, expect, it } from "vitest";
import { initialProvider } from "./providers";

const provider = (
  id: string,
  slug: string,
  projectId = "",
): UserSessionIssuer => ({ id, slug, projectId }) as UserSessionIssuer;

const shared = provider("org_other", "assistant-shared");
const orgDefault = provider("org_default", "default");
const own = provider("project_own", "inference", "project_a");
const providers = [shared, orgDefault, own];

describe("initialProvider", () => {
  it("opens the one a link asks for", () => {
    expect(
      initialProvider(providers, {
        requested: "project_own",
        last: "org_other",
      }),
    ).toBe(own);
  });

  it("falls back to the one picked last", () => {
    expect(
      initialProvider(providers, { requested: "gone", last: "org_other" }),
    ).toBe(shared);
  });

  it("otherwise opens the organization's default", () => {
    expect(initialProvider(providers, { last: "gone" })).toBe(orgDefault);
  });

  it("never takes a project's provider named default for the organization's", () => {
    const projectDefault = provider("project_default", "default", "project_a");
    expect(initialProvider([projectDefault, shared], {})).toBe(projectDefault);
    expect(initialProvider([shared, projectDefault], {})).toBe(shared);
  });

  it("has nothing to open without providers", () => {
    expect(initialProvider([], { last: "org_default" })).toBeUndefined();
  });
});
