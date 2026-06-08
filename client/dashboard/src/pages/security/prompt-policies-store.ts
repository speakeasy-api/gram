import { useCallback } from "react";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";

export type PolicyAction = "flag" | "block";

export type PromptPolicy = {
  id: string;
  /** Display name. When autoName is true this is derived from the instruction. */
  name: string;
  enabled: boolean;
  action: PolicyAction;
  promptInstruction: string;
  messageTypes: string[];
  autoName: boolean;
  /** Unix timestamp (ms) — JSON-safe. */
  createdAt: number;
  updatedAt: number;
};

type CreateInput = Omit<PromptPolicy, "id" | "createdAt" | "updatedAt">;
type UpdateInput = Partial<Omit<PromptPolicy, "id" | "createdAt">>;

const STORAGE_KEY = "gram-prompt-policies";

export function usePromptPoliciesStore() {
  const [policies, setPolicies] = useLocalStorageState<PromptPolicy[]>(
    STORAGE_KEY,
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
      setPolicies((prev) => [policy, ...prev]);
    },
    [setPolicies],
  );

  const update = useCallback(
    (id: string, input: UpdateInput) => {
      setPolicies((prev) =>
        prev.map((p) =>
          p.id === id ? { ...p, ...input, updatedAt: Date.now() } : p,
        ),
      );
    },
    [setPolicies],
  );

  const remove = useCallback(
    (id: string) => {
      setPolicies((prev) => prev.filter((p) => p.id !== id));
    },
    [setPolicies],
  );

  return { policies, create, update, remove };
}
