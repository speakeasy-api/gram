import { expect, it } from "vitest";
import { addOinName, currentOinNames } from "./registryOktaNames";

const record = `{
  "server": { "name": "com.notion/mcp", "description": "Notion", "version": "1" },
  "_meta": {
    "com.speakeasy.ai/catalog": { "documentationUrl": "https://example.test" },
    "n": 9007199254740993
  }
}`;

it("adds a name to the Okta namespace without touching the rest", () => {
  const next = addOinName(record, "notion");
  expect("text" in next).toBe(true);
  const text = (next as { text: string }).text;
  expect(text).toContain("9007199254740993");
  expect(text).toContain('"documentationUrl": "https://example.test"');
  expect(currentOinNames(text)).toEqual(["notion"]);
  expect(JSON.parse(text)._meta["com.speakeasy.ai/okta"]).toEqual({
    oinNames: ["notion"],
  });
});

it("appends to an existing list once and keeps sibling fields", () => {
  const seeded = (
    addOinName(
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
    ) as { text: string }
  ).text;
  const again = (addOinName(seeded, "linear") as { text: string }).text;
  expect(JSON.parse(again)._meta["com.speakeasy.ai/okta"]).toEqual({
    oinNames: ["integrator-4080826_linear_1", "linear"],
    xaaIssuer: "https://auth.linear.com",
  });
});

it("refuses primitives, arrays and duplicate keys on the edit path", () => {
  expect(addOinName('{"_meta": 5}', "x")).toHaveProperty("error");
  expect(addOinName('{"_meta": []}', "x")).toHaveProperty("error");
  expect(
    addOinName('{"_meta": {"com.speakeasy.ai/okta": "x"}}', "x"),
  ).toHaveProperty("error");
  expect(
    addOinName('{"_meta": {"a": 1}, "_meta": {"b": 2}}', "x"),
  ).toHaveProperty("error");
  expect(
    addOinName(
      '{"_meta": {"com.speakeasy.ai/okta": {"oinNames": []}, "com.speakeasy.ai/okta": {}}}',
      "x",
    ),
  ).toHaveProperty("error");
  const ok = addOinName('{"_meta": {"other": true}}', "x") as {
    text: string;
  };
  expect(JSON.parse(ok.text)._meta).toEqual({
    other: true,
    "com.speakeasy.ai/okta": { oinNames: ["x"] },
  });
});

it("reports text that is not an editable JSON object", () => {
  expect(addOinName("not json", "x")).toHaveProperty("error");
  expect(addOinName("[]", "x")).toHaveProperty("error");
  expect(
    addOinName('{"_meta":{"com.speakeasy.ai/okta":{"oinNames":"x"}}}', "x"),
  ).toHaveProperty("error");
  expect(currentOinNames("nope")).toEqual([]);
});
