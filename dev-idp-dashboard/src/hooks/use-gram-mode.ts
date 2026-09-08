import { useQuery } from "@tanstack/react-query";
import type { Backend, CurrentUser, Mode } from "@/lib/devidp";

export interface EnvVarReadout {
  name: string;
  description: string;
  sensitive: boolean;
  is_set: boolean;
  /** Actual value for non-sensitive vars; null when unset or masked. */
  value: string | null;
}

export interface GramMode {
  /** Identity backend dev-idp is running, from GRAM_DEVIDP_BACKEND. */
  backend: Backend;
  /** The currentUser slot that is authoritative for that backend. */
  mode: Mode | null;
  currentUser: CurrentUser | null;
  meta: {
    env: EnvVarReadout[];
  };
}

async function fetchGramMode(): Promise<GramMode> {
  const res = await fetch("/api/gram-mode");
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

export function useGramMode() {
  return useQuery({
    queryKey: ["gram-mode"],
    queryFn: fetchGramMode,
  });
}
