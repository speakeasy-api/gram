import { describe, expect, it } from "vitest";
import { isValidDataExportEndpointURL } from "./data-export-validation";

describe("isValidDataExportEndpointURL", () => {
  it.each([
    "http://collector.example.com",
    "https://collector.example.com/v1/traces",
    "  https://collector.example.com  ",
    "HTTPS://collector.example.com",
  ])("accepts HTTP and HTTPS endpoint %s", (endpoint) => {
    expect(isValidDataExportEndpointURL(endpoint)).toBe(true);
  });

  it.each([
    "https://user@collector.example.com",
    "https://user:password@collector.example.com",
    "https://@collector.example.com",
  ])("rejects endpoint userinfo in %s", (endpoint) => {
    expect(isValidDataExportEndpointURL(endpoint)).toBe(false);
  });

  it.each([
    "https://collector.example.com?format=json",
    "https://collector.example.com?",
  ])("rejects endpoint queries in %s", (endpoint) => {
    expect(isValidDataExportEndpointURL(endpoint)).toBe(false);
  });

  it.each([
    "https://collector.example.com#traces",
    "https://collector.example.com#",
  ])("rejects endpoint fragments in %s", (endpoint) => {
    expect(isValidDataExportEndpointURL(endpoint)).toBe(false);
  });

  it.each([
    "https://collector.example.com\\otlp",
    "https://collector.exam\tple.com",
    "https://collector.example.com/\notlp",
  ])("rejects endpoint syntax normalized by the browser in %s", (endpoint) => {
    expect(isValidDataExportEndpointURL(endpoint)).toBe(false);
  });

  it.each(["http:/traces", "https:///traces", "https://"])(
    "rejects hostless endpoint %s",
    (endpoint) => {
      expect(isValidDataExportEndpointURL(endpoint)).toBe(false);
    },
  );
});
