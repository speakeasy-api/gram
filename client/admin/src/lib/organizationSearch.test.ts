// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  organizationsSearchSchema,
  type OrganizationsSearch,
} from "./organizationSearch";

vi.mock("@/pages/organizations/index", () => {
  throw new Error("Search validation loaded the organizations page");
});

// A param the schema drops is absent from the parsed search, so every expected
// value below is the whole object the route sees.
describe("organizationsSearchSchema", () => {
  const cases: [string, Record<string, unknown>, OrganizationsSearch][] = [
    ["defaults to unrestricted", {}, {}],
    [
      "reads canonical active",
      { disabledStatus: "active" },
      { disabledStatus: "active" },
    ],
    [
      "reads canonical disabled",
      { disabledStatus: "disabled" },
      { disabledStatus: "disabled" },
    ],
    [
      "canonical all overrides old restrictions",
      { disabledStatus: "all", disabledOnly: true, disabled: "active" },
      {},
    ],
    [
      "canonical active overrides binary",
      { disabledStatus: "active", disabledOnly: true },
      { disabledStatus: "active" },
    ],
    ["invalid canonical falls back to all", { disabledStatus: "invalid" }, {}],
    ["array canonical falls back to all", { disabledStatus: ["active"] }, {}],
    [
      "invalid canonical permits legacy translation",
      { disabledStatus: "invalid", disabledOnly: true },
      { disabledStatus: "disabled" },
    ],
    [
      "reads disabled only",
      { disabledOnly: true },
      { disabledStatus: "disabled" },
    ],
    [
      "reads a handwritten boolean",
      { disabledOnly: "true" },
      { disabledStatus: "disabled" },
    ],
    ["canonicalizes false", { disabledOnly: false }, {}],
    ["canonicalizes handwritten false", { disabledOnly: "false" }, {}],
    ["rejects invalid boolean", { disabledOnly: "yes" }, {}],
    ["rejects numeric boolean", { disabledOnly: 1 }, {}],
    ["rejects array boolean", { disabledOnly: [true] }, {}],
    [
      "new false overrides legacy active",
      { disabledOnly: false, disabled: "active" },
      {},
    ],
    [
      "new true overrides legacy active",
      { disabledOnly: true, disabled: "active" },
      { disabledStatus: "disabled" },
    ],
    [
      "preserves legacy active bookmark",
      { disabled: "active" },
      { disabledStatus: "active" },
    ],
    ["reads a hand-written type", { type: "free" }, { type: ["free"] }],
    [
      "keeps a type the picker does not offer",
      { type: "startup" },
      { type: ["startup"] },
    ],
    [
      "reads a list of types",
      { type: ["pro", "enterprise"] },
      { type: ["pro", "enterprise"] },
    ],
    [
      "sorts a chosen set into the picker's order",
      { type: ["enterprise", "free"] },
      { type: ["free", "enterprise"] },
    ],
    ["drops a type named twice", { type: ["pro", "pro"] }, { type: ["pro"] }],
    [
      "keeps an unrecognised type after the ones the picker offers",
      { type: ["startup", "pro"] },
      { type: ["pro", "startup"] },
    ],
    ["drops an empty list of types", { type: [] }, {}],
    [
      "sorts trial states into the order the picker offers them",
      { trial: ["expired", "running"] },
      { trial: ["running", "expired"] },
    ],
    [
      "drops a trial state the server does not derive",
      { trial: ["hibernating"] },
      {},
    ],
    [
      "reads a status the picker offers",
      { disabled: ["disabled"] },
      { disabledStatus: "disabled" },
    ],
    [
      "reads the flag this list used to carry as both statuses",
      { disabled: true },
      {},
    ],
    ["drops a status outside the two", { disabled: ["retired"] }, {}],
    ["reads an all-digit term the router coerced", { q: 123 }, { q: "123" }],
    ["reads a boolean term the router coerced", { q: true }, { q: "true" }],
    ["reads a null term the router coerced", { q: null }, { q: "null" }],
    ["drops a term that is a list, not a word", { q: ["acme"] }, {}],
    ["drops a term that is only whitespace", { q: "   " }, {}],
    ["trims the term a pasted link carries", { q: "  acme  " }, { q: "acme" }],
    ["reads a direction in the union", { dir: "desc" }, { dir: "desc" }],
    ["drops a direction outside the union", { dir: "sideways" }, {}],
    ["drops the old disabled flag when it is off", { disabled: false }, {}],
    ["drops a key the schema does not declare", { page: 2 }, {}],
  ];

  it.each(cases)("%s", (_name, search, expected) => {
    expect(organizationsSearchSchema(search)).toEqual(expected);
  });
});
