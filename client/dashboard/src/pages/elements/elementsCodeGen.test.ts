import { describe, expect, it } from "vitest";
import {
  type CodeGenParams,
  getEnvContent,
  getPeerDeps,
  getElementsInstall,
  getNextjsApiRoute,
  getDangerousApiKeyComponentCode,
  getDangerousApiKeyEnvContent,
  getSessionComponentCode,
  getViteApiRoute,
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

const defaultParams: CodeGenParams = {
  apiKey: "sk_test",
  framework: "nextjs",
  projectSlug: "my-project",
  mcpUrl: "https://app.getgram.ai/mcp/my-project",
  config: {
    mcp: "",
    variant: "standalone",
    colorScheme: "system",
    density: "normal",
    radius: "soft",
    welcomeTitle: "Welcome",
    welcomeSubtitle: "How can I help you today?",
    composerPlaceholder: "Send a message...",
    showModelPicker: false,
    systemPrompt: "",
    modalTitle: "Chat",
    modalPosition: "bottom-right",
    modalDefaultOpen: false,
    expandToolGroupsByDefault: false,
  },
};

describe("getSessionComponentCode", () => {
  it("renders for nextjs with defaults", () => {
    expect(getSessionComponentCode(defaultParams)).toMatchSnapshot();
  });

  it("renders for react with defaults", () => {
    expect(
      getSessionComponentCode({ ...defaultParams, framework: "react" }),
    ).toMatchSnapshot();
  });

  it("renders with widget variant and modal options", () => {
    expect(
      getSessionComponentCode({
        ...defaultParams,
        config: {
          ...defaultParams.config,
          variant: "widget",
          modalDefaultOpen: true,
          modalPosition: "top-left",
          modalTitle: "Help",
        },
      }),
    ).toMatchSnapshot();
  });
});

describe("getDangerousApiKeyEnvContent", () => {
  it("renders with provided API key", () => {
    expect(
      getDangerousApiKeyEnvContent({ apiKey: "sk_test_123" }),
    ).toMatchSnapshot();
  });

  it("renders with placeholder when no key provided", () => {
    expect(getDangerousApiKeyEnvContent({ apiKey: null })).toMatchSnapshot();
  });
});

describe("getDangerousApiKeyComponentCode", () => {
  it("renders for nextjs with defaults", () => {
    expect(getDangerousApiKeyComponentCode(defaultParams)).toMatchSnapshot();
  });

  it("renders for react with defaults", () => {
    expect(
      getDangerousApiKeyComponentCode({ ...defaultParams, framework: "react" }),
    ).toMatchSnapshot();
  });

  it("renders with widget variant and modal options", () => {
    expect(
      getDangerousApiKeyComponentCode({
        ...defaultParams,
        config: {
          ...defaultParams.config,
          variant: "widget",
          modalDefaultOpen: true,
          modalPosition: "top-left",
          modalTitle: "Help",
        },
      }),
    ).toMatchSnapshot();
  });
});

describe("getNextjsApiRoute", () => {
  it("renders the Next.js session API route", () => {
    expect(getNextjsApiRoute()).toMatchSnapshot();
  });
});

describe("getViteApiRoute", () => {
  it("renders the Express session endpoint", () => {
    expect(getViteApiRoute()).toMatchSnapshot();
  });
});
