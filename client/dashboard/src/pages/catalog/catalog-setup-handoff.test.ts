import { expect, it } from "vitest";
import { readCatalogSetupHandoff, writeCatalogSetupHandoff } from "./catalog-setup-handoff";
import { defaultServerGuardrailState } from "@/pages/security/server-guardrails/server-guardrail-policy";
it("round-trips only the User Identity handoff and its existing modal choices", () => {
  sessionStorage.clear();
  expect(readCatalogSetupHandoff("draft")).toBeNull();
  const guardrail = defaultServerGuardrailState();
  guardrail.categories = new Set(["pii", "secrets"]);
  guardrail.audienceType = "targeted";
  guardrail.audiencePrincipalUrns = new Set(["urn:example:principal"]);
  guardrail.action = "block";
  guardrail.score = 3;
  guardrail.userMessage = "Review the selected tools before retrying.";
  guardrail.toolMode = "selected";
  guardrail.selectedTools = ["example_tool"];
  expect(
    writeCatalogSetupHandoff("draft", {
      name: "Example Snowflake",
      guardrail,
      customize: true,
    }),
  ).toBe(true);
  expect(readCatalogSetupHandoff("draft")).toEqual({
    name: "Example Snowflake",
    guardrail,
    customize: true,
  });
  sessionStorage.setItem(
    "draft:handoff",
    JSON.stringify({ name: "Example Snowflake", identityMode: "none" }),
  );
  expect(readCatalogSetupHandoff("draft")).toBeNull();
});
