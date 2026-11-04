import assert from "node:assert/strict";
import { test } from "node:test";
import { loopbackURL, scopedCSV } from "./bootstrap.ts";

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
test("bootstrap preserves existing rows and scopes only the issuer resolver", () => {
  const source =
    "distinct_id,flag,enabled\n*,existing-flag,true\nother,internal-session-issuer,false\n";
  const result = scopedCSV(source, "org_fixture");
  assert.equal(result, source + "org_fixture,internal-session-issuer,true\n");
  assert.equal(scopedCSV(result, "org_fixture"), result);
  for (const id of ["*", "", "org,other", "org\nother"])
    assert.throws(() => scopedCSV(source, id));
});
