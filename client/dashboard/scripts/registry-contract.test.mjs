import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { compile } from "../../../server/internal/mcpregistry/contract/formats.mjs";
import { Validator, format } from "@cfworker/json-schema";
const read = (name) =>
  JSON.parse(
    readFileSync(
      new URL(
        "../../../server/internal/mcpregistry/contract/" + name,
        import.meta.url,
      ),
      "utf8",
    ),
  );
test("unknown required formats fail initialization", () => {
  assert.throws(() =>
    compile(
      { type: "string", format: "unsupported-required-format" },
      Validator,
      format,
    ),
  );
});
const schema = read("./record.schema.json");
for (const c of read("./conformance.json"))
  test(c.name, () => {
    assert.equal(
      compile(schema, Validator, format).validate(c.record).valid,
      c.valid,
    );
  });

for (const bad of [
  { minLength: "bad" },
  { format: 42 },
  { $schema: "https://example.invalid/unknown" },
  { type: "nonsense" },
])
  test("reject malformed schema " + JSON.stringify(bad), () =>
    assert.throws(() => compile(bad, Validator, format)),
  );

test("two-record starter baseline", () => {
  const manifest = read("../baseline/manifest.json");
  assert.equal(manifest.count, 2);
  assert.equal(manifest.records.length, 2);
  const validator = compile(schema, Validator, format);
  for (const record of manifest.records)
    assert.equal(
      validator.validate(read("../baseline/" + record.file)).valid,
      true,
    );
});
