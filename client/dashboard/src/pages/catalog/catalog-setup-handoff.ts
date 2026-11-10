import {
  defaultServerGuardrailState,
  SERVER_GUARDRAIL_CATEGORIES,
  type ServerGuardrailState,
} from "@/pages/security/server-guardrails/server-guardrail-policy";
export type CatalogSetupHandoff = {
  name: string;
  guardrail?: ServerGuardrailState;
  customize?: boolean;
};

/** Non-secret choices from the existing modal; credentials never enter this record. */
export function writeCatalogSetupHandoff(
  key: string,
  handoff: CatalogSetupHandoff,
): boolean {
  try {
    sessionStorage.setItem(
      `${key}:handoff`,
      JSON.stringify({
        identityMode: "user",
        name: handoff.name,
        customize: handoff.customize,
        ...(handoff.guardrail
          ? {
              guardrail: {
                ...handoff.guardrail,
                categories: [...handoff.guardrail.categories],
                audiencePrincipalUrns: [
                  ...handoff.guardrail.audiencePrincipalUrns,
                ],
              },
            }
          : {}),
      }),
    );
    return true;
  } catch {
    return false;
  }
}
export function readCatalogSetupHandoff(
  key: string,
): CatalogSetupHandoff | null {
  try {
    const data: unknown = JSON.parse(
      sessionStorage.getItem(`${key}:handoff`) ?? "null",
    );
    if (
      !data ||
      typeof data !== "object" ||
      !("identityMode" in data) ||
      data.identityMode !== "user" ||
      !("name" in data) ||
      typeof data.name !== "string" ||
      !data.name.trim()
    )
      return null;
    let guardrail: ServerGuardrailState | undefined;
    if (
      "guardrail" in data &&
      data.guardrail &&
      typeof data.guardrail === "object"
    ) {
      const raw = data.guardrail;
      guardrail = defaultServerGuardrailState();
      if (!("categories" in raw) || !Array.isArray(raw.categories)) return null;
      const categories = raw.categories;
      guardrail.categories = new Set(
        SERVER_GUARDRAIL_CATEGORIES.filter((category) =>
          categories.includes(category),
        ),
      );
      if ("action" in raw && (raw.action === "flag" || raw.action === "block"))
        guardrail.action = raw.action;
      if (
        "score" in raw &&
        typeof raw.score === "number" &&
        raw.score >= 0.1 &&
        raw.score <= 10
      )
        guardrail.score = raw.score;
      if ("toolMode" in raw && raw.toolMode === "selected")
        guardrail.toolMode = "selected";
      if ("selectedTools" in raw && Array.isArray(raw.selectedTools))
        guardrail.selectedTools = raw.selectedTools.filter(
          (tool): tool is string => typeof tool === "string",
        );
      if ("userMessage" in raw && typeof raw.userMessage === "string")
        guardrail.userMessage = raw.userMessage;
      if ("audienceType" in raw && raw.audienceType === "targeted")
        guardrail.audienceType = "targeted";
      if (
        "audiencePrincipalUrns" in raw &&
        Array.isArray(raw.audiencePrincipalUrns)
      )
        guardrail.audiencePrincipalUrns = new Set(
          raw.audiencePrincipalUrns.filter(
            (urn): urn is string => typeof urn === "string",
          ),
        );
    }
    return {
      name: data.name.trim(),
      guardrail,
      customize: "customize" in data && data.customize === true,
    };
  } catch {
    return null;
  }
}
