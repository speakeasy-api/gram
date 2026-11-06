import { Alert } from "@/components/ui/Alert";
import { useMcpServerAuthenticationReadiness } from "@/hooks/useMcpServerAuthenticationReadiness";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useSetupStep } from "./useSetupStep";

/**
 * One line at the top of the Authentication section for a user sent here to
 * finish setup: what is left and where, then that the server is ready once
 * the step is done. It reads the same readiness the sidebar bar does, so the
 * two never disagree.
 */
export function SetupStepBanner({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element | null {
  const step = useSetupStep();
  const { ready, loading } = useMcpServerAuthenticationReadiness(mcpServer);
  if (!step || loading) return null;

  const name = mcpServer.name?.trim() || "This server";
  if (ready) {
    return (
      <Alert variant="success" title={null}>
        {name} is ready to use. Go back to your chat to keep going.
      </Alert>
    );
  }
  return (
    <Alert variant="info" title="One step left">
      {step === "credentials"
        ? `Add the API key ${name} needs so Speakeasy can call it for your team. Fill in the required header under Custom Headers below, then save.`
        : `Sign in to ${name} so Speakeasy can call it for you. Use the sign-in option below.`}
    </Alert>
  );
}
