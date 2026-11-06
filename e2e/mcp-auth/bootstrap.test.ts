import assert from "node:assert/strict";
import { test } from "node:test";
import { loopbackURL, scopedCSV, originalFlagsPath } from "./bootstrap.ts";

test("bootstrap accepts only credential-free loopback URLs", () => {
  for (const value of [
    "https://localhost:8443",
    "http://127.0.0.1:8080",
    "http://[::1]:8080",
  ])
    assert.equal(loopbackURL(value), value);
  for (const value of [
    undefined,
    "https://example.com",
    "https://localhost.example.com",
    "https://user:secret@localhost",
    "file:///tmp/local",
    "https://localhost?token=secret",
  ])
    assert.throws(() => loopbackURL(value));
});
test("bootstrap preserves existing rows and scopes only the EMA flags", () => {
  const source =
    "distinct_id,flag,enabled\n*,existing-flag,true\nother,internal-session-issuer,false\n";
  const result = scopedCSV(source, "org_fixture");
  assert.equal(
    result,
    source +
      "org_fixture,internal-session-issuer,true\norg_fixture,sign-in-enforcement,true\n",
  );
  assert.equal(scopedCSV(result, "org_fixture"), result);
  // A CSV from an earlier bootstrap gains only the row it lacks.
  const earlier = source + "org_fixture,internal-session-issuer,true\n";
  assert.equal(scopedCSV(earlier, "org_fixture"), result);
  for (const id of ["*", "", "org,other", "org\nother"])
    assert.throws(() => scopedCSV(source, id));
});

test("bootstrap uses the last matching row and preserves enabled rows in the middle", () => {
  const enabled =
    "distinct_id,flag,enabled\norg_fixture,internal-session-issuer,true\norg_fixture,sign-in-enforcement,true\nother,unrelated,false\n";
  assert.equal(scopedCSV(enabled, "org_fixture"), enabled);
  const disabled = enabled + "org_fixture,internal-session-issuer,false\n";
  assert.equal(
    scopedCSV(disabled, "org_fixture"),
    disabled + "org_fixture,internal-session-issuer,true\n",
  );
});

test("bootstrap rebuilds organization rollouts from the original resolver input", () => {
  const output = "/local/generated.csv";
  assert.equal(
    originalFlagsPath(output, output, {
      previousEffective: {
        GRAM_LOCAL_FEATURE_FLAGS_CSV: "/local/original.csv",
      },
    }),
    "/local/original.csv",
  );
  assert.equal(
    originalFlagsPath(output, output, {
      previousEffective: { GRAM_LOCAL_FEATURE_FLAGS_CSV: null },
    }),
    undefined,
  );
  assert.equal(originalFlagsPath("/local/new.csv", output), "/local/new.csv");
  assert.throws(
    () => originalFlagsPath(output, output),
    /ORIGINAL_FLAGS_SNAPSHOT_REQUIRED/,
  );
  assert.throws(
    () =>
      originalFlagsPath(output, output, {
        previousEffective: { GRAM_LOCAL_FEATURE_FLAGS_CSV: output },
      }),
    /RECURSIVE_FLAGS_SNAPSHOT/,
  );
});
