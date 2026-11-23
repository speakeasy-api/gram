import { useSearchParams } from "react-router";

/**
 * The open setup step a Platform MCP "Finish setup" link names, read from the
 * settings page's `setup` query parameter.
 */
export type SetupStep = "credentials" | "oauth-client" | "sign-in";

export function useSetupStep(): SetupStep | null {
  const [searchParams] = useSearchParams();
  const step = searchParams.get("setup");
  return step === "credentials" || step === "oauth-client" || step === "sign-in"
    ? step
    : null;
}
