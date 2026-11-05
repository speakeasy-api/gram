import { expect, it } from "vitest";
import { inferenceCompletionEndpoint } from "./inferenceEndpoint";
it("uses the configured HTTPS origin for completions", () => {
  expect(
    inferenceCompletionEndpoint("https://api.example.test/base", false),
  ).toBe("https://api.example.test/chat/completions");
});
it.each([
  "http://api.example.test",
  "ftp://api.example.test",
  "not a URL",
  "https://user:secret@api.example.test",
  "https://api.example.test?token=abc",
  "https://api.example.test#frag",
  "https://api.example.test?",
  "https://api.example.test#",
  "http://localhost:8080?token=abc",
  "http://localhost:8080#",
])("rejects unsafe destination %s", (base) => {
  expect(inferenceCompletionEndpoint(base, false)).toBeNull();
  expect(inferenceCompletionEndpoint(base, true)).toBeNull();
});
it.each([
  "http://localhost:8080",
  "http://127.0.0.1:8080",
  "http://[::1]:8080",
])("only permits loopback HTTP in explicit local development: %s", (base) => {
  expect(inferenceCompletionEndpoint(base, false)).toBeNull();
  expect(inferenceCompletionEndpoint(base, true)).toContain(
    "/chat/completions",
  );
});
