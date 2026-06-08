import { useCallback, useEffect } from "react";
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
type UpdateInput = Partial<
  Omit<PromptPolicy, "id" | "createdAt" | "updatedAt">
>;

function storageKey(orgId: string) {
  return `gram-prompt-policies-${orgId}`;
}

function isValidPolicy(p: unknown): p is PromptPolicy {
  return (
    p !== null &&
    typeof p === "object" &&
    typeof (p as Record<string, unknown>).id === "string"
  );
}

function safe(prev: unknown): PromptPolicy[] {
  return Array.isArray(prev) ? prev.filter(isValidPolicy) : [];
}

function usePromptPoliciesStoreImpl(orgId: string) {
  const key = storageKey(orgId);
  const [policies, setPolicies] = useLocalStorageState<PromptPolicy[]>(key, []);

  useEffect(() => {
    try {
      const item = window.localStorage.getItem(key);
      setPolicies(item !== null ? (JSON.parse(item) as PromptPolicy[]) : []);
    } catch {
      setPolicies([]);
    }
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps

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
