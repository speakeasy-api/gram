import { describe, expect, it } from "vitest";
import {
  getEnvContent,
  getPeerDeps,
  getElementsInstall,
} from "./elementsCodeGen";

describe("getEnvContent", () => {
  it("renders with provided API key", () => {
    expect(getEnvContent({ apiKey: "sk_test_123" })).toMatchSnapshot();
  });

  it("renders with placeholder when no key provided", () => {
    expect(getEnvContent({ apiKey: null })).toMatchSnapshot();
  });
});

describe("getPeerDeps", () => {
  it("renders for nextjs", () => {
    expect(getPeerDeps({ framework: "nextjs" })).toMatchSnapshot();
  });

  it("renders for react", () => {
    expect(getPeerDeps({ framework: "react" })).toMatchSnapshot();
  });
});

describe("getElementsInstall", () => {
  it("renders for nextjs", () => {
    expect(getElementsInstall({ framework: "nextjs" })).toMatchSnapshot();
  });

  it("renders for react", () => {
    expect(getElementsInstall({ framework: "react" })).toMatchSnapshot();
  });
});
