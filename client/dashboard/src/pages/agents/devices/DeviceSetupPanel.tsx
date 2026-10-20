import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useCreateWorkloadDeviceJoinTokenMutation } from "@gram/client/react-query/createWorkloadDeviceJoinToken.js";
import { useCallback, useEffect, useRef, useState, type JSX } from "react";
import { CodeBlock } from "../provision/StepProvision";
import { Countdown } from "./Countdown";
import {
  MANAGED_CONFIG_PATH,
  controlPlaneURL,
  managedConfig,
  type WatchedToken,
} from "./device-enrollment";
import {
  useCreateDeviceInstallCommand,
  type CreateDeviceInstallCommand,
  type DeviceInstallCommand,
} from "./device-install-command";
import type { HooksProject } from "./useHooksProjects";

/**
 * Getting a headless box enrolled: one command when the server can mint it,
 * and the bootstrap token with its config file for anyone setting the box up
 * by hand. Both lead to the same place, a box enrolled as this agent.
 */
export function DeviceSetupPanel({
  agentId,
  hooksProject,
  lifetimeSeconds,
  onTokenCreated,
  createInstallCommand: injected,
}: {
  agentId: string;
  hooksProject: HooksProject;
  lifetimeSeconds: number;
  /** A token now exists for a box to redeem; the caller watches for it. */
  onTokenCreated: (watched: WatchedToken) => void;
  /** Overrides the install-command endpoint, for tests. */
  createInstallCommand?: CreateDeviceInstallCommand | null;
}): JSX.Element {
  const fromServer = useCreateDeviceInstallCommand();
  const createInstallCommand = injected === undefined ? fromServer : injected;
  return (
    <div className="space-y-6">
      {createInstallCommand === null ? (
        <div className="border-border space-y-1 border border-dashed p-4">
          <Text small className="font-medium">
            One-command install is coming soon
          </Text>
          <Text muted small>
            Until then, set the box up by hand with a bootstrap token.
          </Text>
        </div>
      ) : (
        <InstallCommand
          agentId={agentId}
          hooksProject={hooksProject}
          lifetimeSeconds={lifetimeSeconds}
          create={createInstallCommand}
          onTokenCreated={onTokenCreated}
        />
      )}
      <ManualSetup
        agentId={agentId}
        hooksProject={hooksProject}
        lifetimeSeconds={lifetimeSeconds}
        onTokenCreated={onTokenCreated}
        // With no command to run, setting up by hand is the only way, so it
        // is not folded away.
        defaultOpen={createInstallCommand === null}
      />
    </div>
  );
}

function InstallCommand({
  agentId,
  hooksProject,
  lifetimeSeconds,
  create,
  onTokenCreated,
}: {
  agentId: string;
  hooksProject: HooksProject;
  lifetimeSeconds: number;
  create: CreateDeviceInstallCommand;
  onTokenCreated: (watched: WatchedToken) => void;
}): JSX.Element {
  const [command, setCommand] = useState<DeviceInstallCommand | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const mint = useCallback(() => {
    setPending(true);
    setError(null);
    create({
      agentId,
      hooksProjectSlug: hooksProject.slug,
      expiresInSeconds: lifetimeSeconds,
    })
      .then((next) => {
        setCommand(next);
        onTokenCreated({ id: next.joinTokenId });
      })
      .catch((failure: unknown) =>
        setError(
          failure instanceof Error
            ? failure.message
            : "Could not prepare the install command. Try again.",
        ),
      )
      .finally(() => setPending(false));
  }, [create, agentId, hooksProject.slug, lifetimeSeconds, onTokenCreated]);

  // Once per panel: every command carries a fresh bootstrap token, so a
  // re-render must not mint another.
  const minted = useRef(false);
  useEffect(() => {
    if (minted.current) return;
    minted.current = true;
    mint();
  }, [mint]);

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <Text className="font-medium">Run this on the box</Text>
        <Text muted small>
          As root, on Linux. It installs the Speakeasy device service and
          enrolls the box as this agent.
        </Text>
      </div>
      {command ? (
        <>
          <CodeBlock
            value={command.command}
            label="install command"
            language="shell"
          />
          <Text muted small>
            The command works once and <Countdown to={command.codeExpiresAt} />.
            The bootstrap token it carries{" "}
            <Countdown to={command.tokenExpiresAt} />.{" "}
            <button
              type="button"
              className="underline"
              disabled={pending}
              onClick={mint}
            >
              Get a new command
            </button>
          </Text>
          <details>
            <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-sm">
              Save and read the script first
            </summary>
            <div className="mt-2 space-y-2">
              <CodeBlock
                value={command.downloadCommand}
                label="download the script"
                language="shell"
              />
              <Text muted small>
                Downloading spends the command. The saved script carries the
                bootstrap token, so delete it once it has run.
              </Text>
            </div>
          </details>
        </>
      ) : (
        <Text muted small>
          {pending ? "Preparing the install command…" : null}
        </Text>
      )}
      {error && (
        <div role="alert" className="space-y-2">
          <Text small>{error}</Text>
          <Button variant="secondary" size="sm" onClick={mint}>
            Try again
          </Button>
        </div>
      )}
    </div>
  );
}

/**
 * Setting the box up by hand: a bootstrap token, shown once, and the config
 * file the device service reads it from.
 */
export function ManualSetup({
  agentId,
  hooksProject,
  lifetimeSeconds,
  onTokenCreated,
  defaultOpen = false,
}: {
  agentId: string;
  hooksProject: HooksProject;
  lifetimeSeconds: number;
  onTokenCreated: (watched: WatchedToken) => void;
  defaultOpen?: boolean;
}): JSX.Element {
  const create = useCreateWorkloadDeviceJoinTokenMutation();
  // Held only while this panel is mounted. Nothing writes it to the query
  // cache or to storage, and the server keeps only its hash.
  const [issued, setIssued] = useState<{
    token: string;
    expiresAt: Date;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);

  const issue = () => {
    setError(null);
    create.mutate(
      {
        request: {
          createDeviceJoinTokenRequestBody: {
            agentId,
            expiresInSeconds: lifetimeSeconds,
          },
        },
      },
      {
        onSuccess: (result) => {
          setIssued({ token: result.token, expiresAt: result.expiresAt });
          onTokenCreated({ expiresAt: result.expiresAt });
        },
        onError: (failure) =>
          setError(
            failure instanceof Error && failure.message
              ? failure.message
              : "Could not create a bootstrap token. Try again.",
          ),
      },
    );
  };

  return (
    <details open={defaultOpen || undefined}>
      <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-sm">
        Set up manually
      </summary>
      <div className="mt-3 space-y-4">
        {issued === null ? (
          <div className="space-y-3">
            <Text muted small>
              Creates a single-use bootstrap token for this agent. It is shown
              once, here, inside the config file the device service reads.
            </Text>
            <Button
              variant="secondary"
              size="sm"
              disabled={create.isPending}
              onClick={issue}
            >
              {create.isPending ? "Creating…" : "Create bootstrap token"}
            </Button>
            {error && (
              <Text role="alert" small>
                {error}
              </Text>
            )}
          </div>
        ) : (
          <>
            <Text muted small>
              This bootstrap token is shown only once and{" "}
              <Countdown to={issued.expiresAt} />. Copy the file now.
            </Text>
            <div data-demo-secret>
              <CodeBlock
                value={managedConfig({
                  joinToken: issued.token,
                  hooksProjectSlug: hooksProject.slug,
                  controlPlaneURL: controlPlaneURL(),
                })}
                label={MANAGED_CONFIG_PATH}
                language="json"
                breakAnywhere
              />
            </div>
            <CodeBlock
              value={[
                `sudo chown root:<group> ${MANAGED_CONFIG_PATH}`,
                `sudo chmod 0640 ${MANAGED_CONFIG_PATH}`,
              ].join("\n")}
              label="permissions"
              language="shell"
            />
            <Text muted small>
              Use the group of the account that runs the AI tools, and run{" "}
              <code className="text-xs">speakeasyd</code> as that account. The
              token enrolls the box the first time the service starts.
            </Text>
          </>
        )}
      </div>
    </details>
  );
}
