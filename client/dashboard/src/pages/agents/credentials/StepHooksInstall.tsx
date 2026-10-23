import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { JSX } from "react";
import { Link } from "react-router";
import { isCredentialSafe } from "./gateway";
import { CodeBlock } from "./StepInstall";
import { WizardStepHeader } from "./WizardChrome";

/**
 * The install step for an agent that reaches no MCP server. There is no
 * gateway to point a runtime at and no setup link to run: the setup script
 * only adds the gateway to MCP clients. What such an agent needs is the
 * observability plugin, with this agent's key in its environment so the
 * events it sends are recorded under the agent.
 *
 * The variable names are the ones the plugin's hook binary reads.
 */

const HOOKS_KEY_ENV = "SPEAKEASY_AI_HOOKS_API_KEY";
const HOOKS_PROJECT_ENV = "SPEAKEASY_AI_HOOKS_PROJECT_SLUG";
const HOOKS_SERVER_ENV = "SPEAKEASY_AI_HOOKS_SERVER_URL";

const TITLE = "Connect the agent's hooks";

export function StepHooksInstall({
  secret,
  projectSlug,
  onIssue,
  issuing,
}: {
  secret: string | null;
  /** The project the agent's hook grant is bound to. */
  projectSlug: string;
  /** Issue a key, for a step reached without one to show. */
  onIssue: () => void;
  issuing: boolean;
}): JSX.Element {
  const routes = useRoutes();
  // Absolute, always: the runtime runs outside the browser. An empty build
  // setting means the current origin is what serves the API.
  const serverURL = getServerURL() || window.location.origin;

  // The key goes on the wire with every hook event, so a plaintext endpoint
  // would hand it to anyone on the path. Loopback is exempt, as it is for the
  // gateway.
  if (!isCredentialSafe(serverURL)) {
    return (
      <div className="space-y-5">
        <WizardStepHeader
          title={TITLE}
          description="This deployment cannot carry an agent key."
        />
        <Text role="alert">
          The API is served over plaintext HTTP at {serverURL}. Setup
          instructions are withheld because they would send the agent key
          unencrypted. Configure an HTTPS server URL and reload.
        </Text>
      </div>
    );
  }

  if (!secret) {
    return (
      <div className="space-y-5">
        <WizardStepHeader
          title={TITLE}
          description="This agent needs a key before its hooks can send events."
        />
        <div className="border-border max-w-md space-y-3 border p-5">
          <Text muted small className="block">
            A key can only be read at the moment it is issued, so one that
            already exists cannot be shown here.
          </Text>
          <Button disabled={issuing} onClick={onIssue}>
            {issuing ? "Issuing…" : "Issue a key"}
          </Button>
        </div>
      </div>
    );
  }

  const environment = [
    `export ${HOOKS_SERVER_ENV}="${serverURL}"`,
    `export ${HOOKS_PROJECT_ENV}="${projectSlug}"`,
    `export ${HOOKS_KEY_ENV}="${secret}"`,
  ].join("\n");

  return (
    <div className="space-y-6">
      <WizardStepHeader
        title={TITLE}
        description="This agent reaches no MCP server. Its runtime sends hook events through the Speakeasy observability plugin, signed with this agent's key."
      />
      <section className="space-y-2">
        <h3 className="text-eyebrow">1. Install the observability plugin</h3>
        <Text muted small className="block">
          Add the plugin to the runtime the agent works in: Claude Code, Codex,
          Cursor or OpenClaw. Each one is on the plugins page.
        </Text>
        {/* A new tab, because the key below is shown once: leaving this page
            to find the plugin would lose it. */}
        <Button size="sm" variant="secondary" asChild>
          <Link to={routes.plugins.href()} target="_blank" rel="noreferrer">
            Open plugins
          </Link>
        </Button>
      </section>
      <section className="space-y-2">
        <h3 className="text-eyebrow">2. Give it this agent's key</h3>
        <CodeBlock
          value={environment}
          label="Hooks environment"
          language="shell"
          breakAnywhere
        />
        <Text muted small className="block">
          Set these where the runtime is started, then restart it: hook
          processes inherit its environment. The key is shown only once, so
          store it in a secret manager.
        </Text>
      </section>
      <Text muted small className="block">
        This key sends hook events to {projectSlug} and lets the device agent
        sync this agent's plugins. It cannot call any MCP server.
      </Text>
    </div>
  );
}
