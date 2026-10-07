import { expect, test } from "vitest";
import {
  CLI_NOT_FOUND_MESSAGE,
  type CLIResolverDeps,
  resolveCLI,
} from "./cli.ts";

const localDevCLIPath = "/repo/cli/bin/gram";

type Fixture = {
  env?: Record<string, string | undefined>;
  localDevBuilt?: boolean;
  onPath?: string[];
  controlPlaneCLIs?: string[];
};

function deps({
  env = {},
  localDevBuilt = false,
  onPath = [],
  controlPlaneCLIs = [],
}: Fixture): CLIResolverDeps {
  return {
    env,
    localDevCLIPath,
    fileExists: (path) => localDevBuilt && path === localDevCLIPath,
    commandExists: async (command) => onPath.includes(command),
    isControlPlaneCLI: async (command) =>
      onPath.includes(command) && controlPlaneCLIs.includes(command),
  };
}

test("GRAM_CLI_PATH wins over every other lookup", async () => {
  const cli = await resolveCLI(
    deps({
      env: { GRAM_CLI_PATH: "/opt/tools/speakeasy", GRAM_DEV: "1" },
      localDevBuilt: true,
      onPath: ["speakeasy", "gram"],
      controlPlaneCLIs: ["speakeasy"],
    }),
  );

  expect(cli).toBe("/opt/tools/speakeasy");
});

test("blank GRAM_CLI_PATH is ignored", async () => {
  const cli = await resolveCLI(
    deps({ env: { GRAM_CLI_PATH: "  " }, onPath: ["gram"] }),
  );

  expect(cli).toBe("gram");
});

test("GRAM_DEV uses the local cli/bin/gram build when it exists", async () => {
  const cli = await resolveCLI(
    deps({
      env: { GRAM_DEV: "true" },
      localDevBuilt: true,
      onPath: ["speakeasy"],
      controlPlaneCLIs: ["speakeasy"],
    }),
  );

  expect(cli).toBe(localDevCLIPath);
});

test("GRAM_DEV falls through when the local build is missing", async () => {
  const cli = await resolveCLI(
    deps({ env: { GRAM_DEV: "1" }, onPath: ["gram"] }),
  );

  expect(cli).toBe("gram");
});

test("speakeasy is preferred when it is the Control Plane CLI", async () => {
  const cli = await resolveCLI(
    deps({ onPath: ["speakeasy", "gram"], controlPlaneCLIs: ["speakeasy"] }),
  );

  expect(cli).toBe("speakeasy");
});

test("speakeasy on PATH that is the SDK CLI falls back to gram", async () => {
  const cli = await resolveCLI(
    deps({ onPath: ["speakeasy", "gram"], controlPlaneCLIs: [] }),
  );

  expect(cli).toBe("gram");
});

test("missing CLI throws with install instructions", async () => {
  await expect(resolveCLI(deps({ onPath: ["speakeasy"] }))).rejects.toThrow(
    CLI_NOT_FOUND_MESSAGE,
  );
  expect(CLI_NOT_FOUND_MESSAGE).toContain("brew install speakeasy-api/tap/cli");
  expect(CLI_NOT_FOUND_MESSAGE).toContain("npm i -g @speakeasy-api/cli");
});

test("SPEAKEASY_AI_CLI_PATH wins over GRAM_CLI_PATH", async () => {
  const cli = await resolveCLI(
    deps({
      env: {
        SPEAKEASY_AI_CLI_PATH: "/opt/new/speakeasy",
        GRAM_CLI_PATH: "/opt/old/gram",
      },
    }),
  );

  expect(cli).toBe("/opt/new/speakeasy");
});

test("SPEAKEASY_AI_DEV uses the local build", async () => {
  const cli = await resolveCLI(
    deps({ env: { SPEAKEASY_AI_DEV: "1" }, localDevBuilt: true }),
  );

  expect(cli).toBe(localDevCLIPath);
});

test("SPEAKEASY_AI_DEV=false wins over GRAM_DEV", async () => {
  const cli = await resolveCLI(
    deps({
      env: { SPEAKEASY_AI_DEV: "false", GRAM_DEV: "1" },
      localDevBuilt: true,
      onPath: ["gram"],
    }),
  );

  expect(cli).toBe("gram");
});

test("plain SPEAKEASY_* names from the SDK generator are ignored", async () => {
  const cli = await resolveCLI(
    deps({
      env: {
        SPEAKEASY_CLI_PATH: "/opt/generator/speakeasy",
        SPEAKEASY_DEV: "1",
      },
      localDevBuilt: true,
      onPath: ["gram"],
    }),
  );

  expect(cli).toBe("gram");
});
