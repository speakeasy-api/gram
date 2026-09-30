import { expect, it } from "vitest";
import { httpsUrlProblem } from "./issuerUrl";

const USERINFO = "Must not carry a username or password.";

it("accepts an https URL on a fully qualified domain", () => {
  expect(httpsUrlProblem("https://identity.example.com", true)).toBeNull();
  expect(
    httpsUrlProblem(
      "https://identity.example.com/.well-known/jwks.json",
      false,
    ),
  ).toBeNull();
});

it("refuses userinfo, including an empty one the server still rejects", () => {
  for (const url of [
    "https://user:pass@identity.example.com",
    "https://user@identity.example.com",
    "https://@identity.example.com",
    "https://:@identity.example.com",
  ]) {
    expect(httpsUrlProblem(url, true), url).toBe(USERINFO);
    expect(httpsUrlProblem(url, false), url).toBe(USERINFO);
  }
});

it("does not mistake an @ in the path for userinfo", () => {
  expect(
    httpsUrlProblem("https://identity.example.com/keys/@team", false),
  ).toBeNull();
});

it("refuses a query string or fragment on either URL, even a bare delimiter", () => {
  for (const url of [
    "https://identity.example.com?x=1",
    "https://identity.example.com?",
    "https://identity.example.com#",
  ]) {
    expect(httpsUrlProblem(url, true), url).not.toBeNull();
    expect(httpsUrlProblem(url, false), url).not.toBeNull();
  }
});
