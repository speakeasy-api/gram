import { CodeBlock } from "@/components/code";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { Link as ExternalLink } from "@/components/ui/Link";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useAgentToken } from "@/hooks/useAgentToken";
import { useOrgRoutes } from "@/routes";
import { useQuery } from "@tanstack/react-query";
import React, { useState } from "react";
import { Link } from "react-router";

import {
  buildCloudAgentStartHook,
  buildCloudDefaultEnvironmentSnippet,
  buildCloudSetupScript,
  CLOUD_ORG_TOKEN_SENTINEL,
  CLOUD_SESSIONS_ANCHOR,
  MANIFEST_URL,
} from "./cloud-setup";

const LINK_CLASS = "underline underline-offset-2 hover:text-foreground";

type ReleasesManifest = {
  latest: Record<string, { version: string }>;
};

const INLINABLE_VERSION = /^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/;

function usePinnedAgentVersion() {
  const query = useQuery<ReleasesManifest>({
    queryKey: ["device-agent-releases"],
    queryFn: async () => {
      const res = await fetch(MANIFEST_URL, {
        headers: { Accept: "application/json" },
      });
      if (!res.ok) throw new Error(`release manifest: HTTP ${res.status}`);
      return res.json() as Promise<ReleasesManifest>;
    },
    staleTime: 5 * 60 * 1000,
    retry: 1,
  });
  const raw = query.data?.latest?.speakeasyd?.version;
  const version = raw && INLINABLE_VERSION.test(raw) ? raw : null;
  return { version, isError: query.isError, isLoading: query.isPending };
}

function InlineLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={LINK_CLASS}
    >
      {children}
    </a>
  );
}

function CloudSetupStep({
  n,
  title,
  children,
}: {
  n: number;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-baseline gap-3">
        <span className="text-eyebrow">{String(n).padStart(2, "0")}</span>
        <Text className="font-medium">{title}</Text>
      </div>
      {children}
    </div>
  );
}

function GenerateInlineButton({
  onClick,
  pending,
  disabled,
  existing,
}: {
  onClick: () => void;
  pending: boolean;
  disabled?: boolean;
  existing: boolean;
}) {
  const label = existing ? "Rotate token" : "Generate token";
  const pendingLabel = existing ? "Rotating…" : "Generating…";
  return (
    <Button
      variant="secondary"
      size="sm"
      onClick={onClick}
      disabled={pending || disabled}
      title={
        disabled
          ? "Generating an agent token requires the org:admin role."
          : existing
            ? "An agent token already exists — this rotates it and splices the new token into the setup script."
            : undefined
      }
      className="-my-1 inline-flex h-6 items-center px-2 py-0 align-middle text-xs"
    >
      <Button.LeftIcon>
        <Icon name="key-round" className="h-3 w-3" />
      </Button.LeftIcon>
      <Button.Text>{pending ? pendingLabel : label}</Button.Text>
    </Button>
  );
}

function CloudSetupScript({ version }: { version: string }) {
  const { name: orgName, slug: orgSlug } = useOrganization();
  const apiKeysHref = useOrgRoutes().apiKeys.href();
  const [rotateConfirmOpen, setRotateConfirmOpen] = useState(false);

  const buildScript = (orgToken: string) =>
    buildCloudSetupScript({
      version,
      orgSlug: orgSlug || "acme-corp",
      orgName: orgName || "Acme Corporation",
      orgToken,
    });

  const {
    generatedToken,
    autoCopied,
    isPending,
    isError,
    canGenerate,
    hasExistingAgentKey,
    generate,
  } = useAgentToken({ buildCopyText: buildScript });

  const script = buildScript(generatedToken ?? CLOUD_ORG_TOKEN_SENTINEL);

  const handleGenerateOrRotate = () => {
    if (hasExistingAgentKey) {
      setRotateConfirmOpen(true);
      return;
    }
    generate();
  };

  const slots = generatedToken
    ? undefined
    : {
        [CLOUD_ORG_TOKEN_SENTINEL]: {
          node: (
            <GenerateInlineButton
              onClick={handleGenerateOrRotate}
              pending={isPending}
              disabled={!canGenerate}
              existing={hasExistingAgentKey}
            />
          ),
          copyText: "spk_org_REPLACE_ME",
        },
      };

  return (
    <div className="flex flex-col gap-3">
      <Text small muted>
        Paste this into the environment&apos;s <strong>Setup script</strong>{" "}
        field. It runs as root on a cache miss only. Do not put the token in
        Anthropic&apos;s Environment variables field — it belongs in{" "}
        <code>managed.json</code> on disk.
      </Text>
      <CodeBlock language="bash" slots={slots}>
        {script}
      </CodeBlock>
      <Text small muted>
        Click{" "}
        <strong className="text-foreground">
          {hasExistingAgentKey ? "Rotate token" : "Generate token"}
        </strong>{" "}
        to mint the <code>org_token</code>. Pin this version; auto-update is
        disabled because the VM lives minutes. Add an <code>email</code> field
        (a shared org mailbox is fine) if you want policy to sync — without it
        the daemon starts but fetches nothing.
      </Text>

      {generatedToken && (
        <Alert variant="warning">
          <AlertTitle>
            {autoCopied
              ? "Setup script copied to your clipboard"
              : "Copy your setup script now"}
          </AlertTitle>
          <AlertDescription>
            {autoCopied
              ? "We've copied the full setup script — with the new org_token — to your clipboard."
              : "The new org_token is spliced into the script above — copy it now."}{" "}
            Anyone who can open the Claude environment can read this token.
            Manage or revoke agent tokens under Settings →{" "}
            <Link to={apiKeysHref} className={LINK_CLASS}>
              API Keys
            </Link>
            .
          </AlertDescription>
        </Alert>
      )}

      {isError && (
        <Alert variant="error">
          <AlertTitle>Couldn't generate a token</AlertTitle>
          <AlertDescription>
            Try again, or create one under Settings →{" "}
            <Link to={apiKeysHref} className={LINK_CLASS}>
              API Keys
            </Link>{" "}
            with the Agent scope.
          </AlertDescription>
        </Alert>
      )}

      <Dialog open={rotateConfirmOpen} onOpenChange={setRotateConfirmOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Rotate device agent token?</Dialog.Title>
            <Dialog.Description>
              This expires the token currently baked into your Claude Code
              environment.
            </Dialog.Description>
          </Dialog.Header>
          <Alert variant="error">
            <AlertTitle>Cloud sessions will stop syncing policy</AlertTitle>
            <AlertDescription>
              Re-paste the updated setup script and recreate the shared
              environment (or wait for a cache miss) so every session picks up
              the new <code>org_token</code>. Until then, policy will not sync.
            </AlertDescription>
          </Alert>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              onClick={() => setRotateConfirmOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              onClick={() => {
                setRotateConfirmOpen(false);
                generate();
              }}
            >
              <Button.Text>Rotate token</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

function SetupScriptStep() {
  const { version, isError, isLoading } = usePinnedAgentVersion();

  if (version) {
    return <CloudSetupScript version={version} />;
  }

  if (isLoading) {
    return (
      <Text small muted>
        Loading the latest release…
      </Text>
    );
  }

  return (
    <Text small muted>
      {isError
        ? "Couldn't load the latest release — open the "
        : "Loading the latest release… or open the "}
      <ExternalLink
        href={MANIFEST_URL}
        target="_blank"
        iconSuffixName="external-link"
      >
        release manifest
      </ExternalLink>{" "}
      for the current version, then refresh this page.
    </Text>
  );
}

/**
 * Cloud sessions walkthrough for Anthropic-hosted Claude Code on the web.
 * Rendered below the OS tiles on DeviceAgentSetup (standalone page and
 * onboarding Instrument agents).
 */
export function DeviceAgentCloudSetup(): React.JSX.Element {
  return (
    <div id={CLOUD_SESSIONS_ANCHOR} className="flex flex-col gap-8">
      <div>
        <p className="text-eyebrow mb-2">Cloud sessions</p>
        <h2 className="text-display-xs font-thin">Claude Code on the web</h2>
        <Text small muted className="mt-2">
          Claude Code on the web is an Anthropic Ubuntu 24.04 x86_64 VM. Paste
          the pieces below into a <strong>shared</strong> Anthropic-hosted
          environment (Team/Enterprise). The agent then pulls org policy from
          Gram and writes Claude&apos;s config — you do not paste Speakeasy
          observability hooks yourself. Self-hosted <code>ccpool_…</code>{" "}
          environments are not covered here.
        </Text>
      </div>

      <CloudSetupStep n={1} title="Network access">
        <Text small muted>
          Trusted network access does not include Gram. Set{" "}
          <strong className="text-foreground">Custom</strong>, check{" "}
          <strong className="text-foreground">
            Also include default list of common package managers
          </strong>{" "}
          (keeps GCS for the agent binaries and the usual registries), and add{" "}
          <code>app.getgram.ai</code> on its own line. Without that host the
          daemon cannot call <code>agent.getPlugins</code> or ingest hooks.
        </Text>
      </CloudSetupStep>

      <CloudSetupStep n={2} title="Setup script">
        <SetupScriptStep />
      </CloudSetupStep>

      <CloudSetupStep n={3} title="Start the agent">
        <Text small muted>
          Anthropic snapshots the filesystem and skips the setup script on later
          sessions; running processes are not in the snapshot. Merge this
          SessionStart hook into org{" "}
          <InlineLink href="https://claude.ai/admin-settings/claude-code">
            Managed Settings
          </InlineLink>{" "}
          or repo <code>.claude/settings.json</code>. User{" "}
          <code>~/.claude/settings.json</code> does not load in cloud. It only
          starts the agent — it is not Claude hook policy.
        </Text>
        <CodeBlock language="json">{buildCloudAgentStartHook()}</CodeBlock>
      </CloudSetupStep>

      <CloudSetupStep n={4} title="Make this the org default">
        <Text small muted>
          Anthropic has no lock-members-to-this-environment switch. After
          creating the shared environment at{" "}
          <InlineLink href="https://claude.ai/admin-settings">
            Claude admin settings
          </InlineLink>
          , set it as the org default at{" "}
          <InlineLink href="https://claude.ai/admin-settings/claude-code">
            claude.ai/admin-settings/claude-code
          </InlineLink>
          . That fills the selector on web, desktop, and mobile when a member
          has not picked one — members can still choose a personal or Default
          environment.
        </Text>
        <Text small muted>
          For CLI <code>claude --cloud</code>, merge this into Managed Settings
          (copy the <code>env_…</code> id from Claude after you create the
          environment). It wins over a user <code>/remote-env</code> value.{" "}
          <code>--environment</code> cannot target Anthropic-hosted{" "}
          <code>env_…</code> ids.
        </Text>
        <CodeBlock language="json">
          {buildCloudDefaultEnvironmentSnippet()}
        </CodeBlock>
      </CloudSetupStep>

      <Alert variant="info">
        <AlertTitle>First session may be pending</AlertTitle>
        <AlertDescription>
          Managed Claude hooks point at the observability plugin under{" "}
          <code>$HOME</code>. A fresh VM reports <code>pending</code> until
          Claude clones the marketplace. Server-managed{" "}
          <code>enabledPlugins</code> already does that clone in cloud sessions;
          enforcement arms on the next agent tick (~0.3s if the bundle is
          already there, otherwise after the first clone).
        </AlertDescription>
      </Alert>
    </div>
  );
}
