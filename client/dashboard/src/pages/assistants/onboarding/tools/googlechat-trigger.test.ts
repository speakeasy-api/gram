import { describe, expect, it, vi } from "vitest";
import { buildAssistantTools } from "./useOnboardingTools";

vi.mock("@/contexts/Sdk", () => ({ useSdkClient: vi.fn() }));
vi.mock("@/elements", () => ({ defineFrontendTool: (tool: unknown) => tool }));

const projectKey = "GOOGLE_CHAT_PROJECT_NUMBER";
const accountKey = "GOOGLE_CHAT_SERVICE_ACCOUNT_JSON";
const populated = [projectKey, accountKey].map((name) => ({
  name,
  value: "<REDACTED>",
}));

function fixture(
  entries: { name: string; value: string }[] | undefined,
  resume = false,
) {
  const assistant = {
    id: "assistant",
    name: "Test",
    instructions: "",
    toolsets: [],
  };
  const trigger = {
    id: "trigger",
    name: "Google Chat",
    definitionSlug: "googlechat",
    targetKind: "assistant",
    targetRef: assistant.id,
    environmentId: "selected",
    status: "paused",
  };
  const sdk = {
    assistants: {
      get: vi.fn().mockResolvedValue(assistant),
      update: vi.fn(),
      create: vi.fn(),
    },
    environments: {
      list: vi.fn().mockResolvedValue({
        environments: [
          { id: "other", slug: "other", entries: populated },
          ...(entries === undefined
            ? []
            : [{ id: "selected", slug: "selected", entries }]),
        ],
      }),
      create: vi.fn(),
      updateBySlug: vi.fn(),
    },
    triggers: {
      list: vi.fn().mockResolvedValue({ triggers: resume ? [trigger] : [] }),
      create: vi.fn().mockResolvedValue(trigger),
      resume: vi.fn().mockResolvedValue({ ...trigger, status: "active" }),
    },
  };
  const draft = {
    assistantId: assistant.id,
    assistant,
    assistantEnv: { id: "other", slug: "other" },
    invalidateAll: vi.fn(),
  };
  const tools = buildAssistantTools({
    sdk,
    draft,
    organizationId: "org",
  } as unknown as Parameters<typeof buildAssistantTools>[0]);
  const { execute } = tools.create_trigger as unknown as {
    execute: (
      args: Record<string, unknown>,
    ) => Promise<{ ok: boolean; error?: string }>;
  };
  return {
    sdk,
    run: (environmentId = "selected") =>
      execute({
        name: "Google Chat",
        definition_slug: "googlechat",
        config: {},
        environment_id: environmentId,
      }),
  };
}

describe.each([false, true])(
  "Google Chat credential preflight (resume=%s)",
  (resume) => {
    it.each([
      ["missing environment", undefined],
      ["missing keys", []],
      [
        "empty declarations",
        populated.map((entry) => ({ ...entry, value: "<EMPTY>" })),
      ],
      ["blank values", populated.map((entry) => ({ ...entry, value: " " }))],
      ["project only", [populated[0]!]],
      ["service account only", [populated[1]!]],
    ] as const)("rejects %s without writes", async (_, entries) => {
      const { sdk, run } = fixture(
        entries === undefined ? undefined : [...entries],
        resume,
      );
      const result = await run();
      expect(result.ok).toBe(false);
      expect(result.error).toContain("request_environment_secrets");
      expect(result.error).toMatch(
        /GOOGLE_CHAT_(PROJECT_NUMBER|SERVICE_ACCOUNT_JSON)/,
      );
      expect(result.error).not.toContain("<REDACTED>");
      for (const write of [
        sdk.assistants.create,
        sdk.assistants.update,
        sdk.environments.create,
        sdk.environments.updateBySlug,
        sdk.triggers.create,
        sdk.triggers.resume,
      ])
        expect(write).not.toHaveBeenCalled();
    });

    it("creates or resumes only with both populated keys in the actual environment", async () => {
      const { sdk, run } = fixture(populated, resume);
      expect((await run()).ok).toBe(true);
      expect(
        resume ? sdk.triggers.resume : sdk.triggers.create,
      ).toHaveBeenCalledOnce();
      expect(
        resume ? sdk.triggers.create : sdk.triggers.resume,
      ).not.toHaveBeenCalled();
      if (!resume)
        expect(sdk.triggers.create).toHaveBeenCalledWith(
          expect.objectContaining({
            createTriggerInstanceForm: expect.objectContaining({
              environmentId: "selected",
            }),
          }),
        );
    });
  },
);

it("checks the stored environment on resume, not a populated override", async () => {
  const { sdk, run } = fixture([], true);
  expect((await run("other")).ok).toBe(false);
  expect(sdk.triggers.resume).not.toHaveBeenCalled();
  expect(sdk.triggers.create).not.toHaveBeenCalled();
});
