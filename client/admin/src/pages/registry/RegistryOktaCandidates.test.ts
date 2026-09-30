import { expect, it } from "vitest";
import { addOinName, currentOinNames } from "./registryOktaNames";

const record = JSON.stringify({
  server: { name: "com.notion/mcp", description: "Notion", version: "1" },
  _meta: {
    "com.speakeasy.ai/catalog": { documentationUrl: "https://example.test" },
  },
});

it("adds a name to the Okta namespace without touching the rest", () => {
  const next = addOinName(record, "notion");
  expect(next).not.toBeNull();
  const parsed = JSON.parse(next as string);
  expect(parsed._meta["com.speakeasy.ai/okta"]).toEqual({
    oinNames: ["notion"],
  });
  expect(parsed._meta["com.speakeasy.ai/catalog"]).toEqual({
    documentationUrl: "https://example.test",
  });
  expect(parsed.server).toEqual(JSON.parse(record).server);
  expect(currentOinNames(next as string)).toEqual(["notion"]);
});

it("appends to an existing list once and keeps sibling fields", () => {
  const seeded = addOinName(
    JSON.stringify({
      server: { name: "app.linear/mcp" },
      _meta: {
        "com.speakeasy.ai/okta": {
          oinNames: ["integrator-4080826_linear_1"],
          xaaIssuer: "https://auth.linear.com",
        },
      },
    }),
    "linear",
  ) as string;
  const again = addOinName(seeded, "linear") as string;
  expect(JSON.parse(again)._meta["com.speakeasy.ai/okta"]).toEqual({
    oinNames: ["integrator-4080826_linear_1", "linear"],
    xaaIssuer: "https://auth.linear.com",
  });
});

it("refuses text that is not a JSON object", () => {
  expect(addOinName("not json", "x")).toBeNull();
  expect(addOinName("[]", "x")).toBeNull();
  expect(currentOinNames("nope")).toEqual([]);
});
