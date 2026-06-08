import { useCallback } from "react";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import type { PolicyAction, PolicyMessageType } from "./policy-data";

export type PromptPolicy = {
  id: string;
  /** Display name. When autoName is true this is derived from the instruction. */
  name: string;
  enabled: boolean;
  action: PolicyAction;
  promptInstruction: string;
  messageTypes: PolicyMessageType[];
  autoName: boolean;
  /** Unix timestamp (ms) — JSON-safe. */
  createdAt: number;
  updatedAt: number;
};

type CreateInput = Omit<PromptPolicy, "id" | "createdAt" | "updatedAt">;
type UpdateInput = Partial<Omit<PromptPolicy, "id" | "createdAt">>;

function storageKey(orgId: string) {
  return `gram-prompt-policies-${orgId}`;
}

function safe(prev: PromptPolicy[]): PromptPolicy[] {
  return Array.isArray(prev) ? prev : [];
}

function usePromptPoliciesStoreImpl(orgId: string) {
  const [policies, setPolicies] = useLocalStorageState<PromptPolicy[]>(
    storageKey(orgId),
    [],
  );

  const create = useCallback(
    (input: CreateInput) => {
      const policy: PromptPolicy = {
        ...input,
        id: crypto.randomUUID(),
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };
      setPolicies((prev) => [policy, ...safe(prev)]);
    },
    [setPolicies],
  );

  const update = useCallback(
    (id: string, input: UpdateInput) => {
      setPolicies((prev) =>
        safe(prev).map((p) =>
          p.id === id ? { ...p, ...input, updatedAt: Date.now() } : p,
        ),
      );
    },
    [setPolicies],
  );

  const remove = useCallback(
    (id: string) => {
      setPolicies((prev) => safe(prev).filter((p) => p.id !== id));
    },
    [setPolicies],
  );

  return { policies: safe(policies), create, update, remove };
}

export function usePromptPoliciesStore(
  orgId: string,
): ReturnType<typeof usePromptPoliciesStoreImpl> {
  return usePromptPoliciesStoreImpl(orgId);
}
