import { useOrganization } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useState } from "react";
import {
  defaultServerGuardrailName,
  defaultServerGuardrailState,
  validateServerGuardrail,
  type ServerGuardrailState,
  type ServerGuardrailValidation,
} from "./server-guardrail-policy";
import { useCreateServerGuardrail } from "./useCreateServerGuardrail";

export type NewServerGuardrailOutcome =
  | { status: "skipped" }
  | { status: "created"; name: string }
  | { status: "failed"; name: string; message: string };

/**
 * The opt-in guardrail on a create-server form. The policy needs the server's
 * id, so `createFor` runs only after the server exists; a failure never undoes
 * the server and is returned for the caller to show, since the fix (adding the
 * guardrail from the server's Guardrails tab) is a separate step.
 */
export interface NewServerGuardrail {
  /** False when the flag is off or the user cannot manage policies. */
  available: boolean;
  enabled: boolean;
  setEnabled: (enabled: boolean) => void;
  state: ServerGuardrailState;
  updateState: (
    update: (current: ServerGuardrailState) => ServerGuardrailState,
  ) => void;
  /** Blocks submit while an enabled guardrail is incomplete. */
  validation: ServerGuardrailValidation;
  createFor: (
    mcpServer: Pick<McpServer, "id" | "name" | "slug">,
  ) => Promise<NewServerGuardrailOutcome>;
}

export function useNewServerGuardrail(): NewServerGuardrail {
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const flag = useFeatureFlag(FEATURE_FLAGS.mcpScopedPolicies);
  // Same gate as the policies it creates.
  const available =
    flag.status === "enabled" && hasScope("org:admin", organization.id);
  const [enabled, setEnabled] = useState(false);
  const [state, setState] = useState<ServerGuardrailState>(
    defaultServerGuardrailState,
  );
  const guardrail = useCreateServerGuardrail();

  const active = available && enabled;
  const validation: ServerGuardrailValidation = active
    ? validateServerGuardrail(state)
    : { ok: true };

  const createFor = async (
    mcpServer: Pick<McpServer, "id" | "name" | "slug">,
  ): Promise<NewServerGuardrailOutcome> => {
    if (!active) return { status: "skipped" };
    const name = defaultServerGuardrailName(
      mcpServer.name ?? mcpServer.slug ?? "",
    );
    try {
      await guardrail.create({ state, mcpServerIds: [mcpServer.id], name });
      return { status: "created", name };
    } catch (err) {
      return {
        status: "failed",
        name,
        message: err instanceof Error ? err.message : String(err),
      };
    }
  };

  return {
    available,
    enabled,
    setEnabled,
    state,
    updateState: (
      update: (current: ServerGuardrailState) => ServerGuardrailState,
    ) => setState(update),
    validation,
    createFor,
  };
}

/** The user-facing sentence for a guardrail that could not be created. */
export function guardrailFailureMessage(
  outcome: Extract<NewServerGuardrailOutcome, { status: "failed" }>,
): string {
  return `MCP server added, but the guardrail could not be created: ${outcome.message}. Add it from the server's Guardrails tab.`;
}
