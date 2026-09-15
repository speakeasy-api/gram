import { CodeBlock } from "@/components/code";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Link as ExternalLink } from "@/components/ui/Link";
import { Text } from "@/components/ui/Text";
import { useAgentToken } from "@/hooks/useAgentToken";
import { useOrgRoutes } from "@/routes";
import { useQuery } from "@tanstack/react-query";
import React, { useId, useState } from "react";
import { Link } from "react-router";

import {
  buildCloudDefaultEnvironmentSnippet,
  buildCloudSetupCommand,
  CLOUD_ORG_TOKEN_SENTINEL,
  MANIFEST_URL,
  PINNED_AGENT_VERSION,
  RELEASE_SHA256,
} from "./cloud-setup";

const LINK_CLASS = "underline underline-offset-2 hover:text-foreground";
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

type ReleasesManifest = {
  latest: Record<
    string,
    {
      version: string;
      artifacts: {
        goos: string;
        goarch: string;
        sha256: string;
      }[];
    }
  >;
};

function usePinnedAgentDaemon() {
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
  const release = query.data?.latest?.speakeasyd;
  const version =
    release?.version && PINNED_AGENT_VERSION.test(release.version)
      ? release.version
      : null;
  const artifact = release?.artifacts.find(
    (candidate) => candidate.goos === "linux" && candidate.goarch === "amd64",
  );
  const sha256 =
    artifact?.sha256 && RELEASE_SHA256.test(artifact.sha256)
      ? artifact.sha256
      : null;
  return {
    release: version && sha256 ? { version, sha256 } : null,
    isError: query.isError,
    isLoading: query.isPending,
  };
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

function GenerateInlineButton({
  onClick,
  pending,
  disabled,
  disabledReason,
  existing,
}: {
  onClick: () => void;
  pending: boolean;
  disabled?: boolean;
  disabledReason?: string;
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
          ? disabledReason
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

function CloudSetupScript({
  version,
  sha256,
}: {
  version: string;
  sha256: string;
}) {
  const apiKeysHref = useOrgRoutes().apiKeys.href();
  const identityEmailId = useId();
  const [identityEmail, setIdentityEmail] = useState("");
  const [rotateConfirmOpen, setRotateConfirmOpen] = useState(false);

  const buildScript = (orgToken: string) =>
    buildCloudSetupCommand({
      version,
      sha256,
      orgToken,
      email: identityEmail.trim(),
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

  const hasIdentityEmail = EMAIL_PATTERN.test(identityEmail.trim());
  const script = hasIdentityEmail
    ? buildScript(generatedToken ?? CLOUD_ORG_TOKEN_SENTINEL)
    : "# Enter a reporting email above to generate the setup script.";

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
              disabled={!canGenerate || !hasIdentityEmail}
              disabledReason={
                hasIdentityEmail
                  ? "Generating an agent token requires the org:admin role."
                  : "Enter the reporting email before generating a token."
              }
              existing={hasExistingAgentKey}
            />
          ),
          copyText: "spk_org_REPLACE_ME",
        },
      };

  return (
    <div className="flex flex-col gap-5">
      <Alert variant="info">
        <AlertTitle>Remote sessions use managed enrollment</AlertTitle>
        <AlertDescription>
          Interactive machines can enroll through a browser sign-in. A headless
          shared VM cannot, so this flow uses managed enrollment: an admin
          provides the shared reporting identity and an agent-scoped{" "}
          <code>org_token</code>.
        </AlertDescription>
      </Alert>
      <Text small muted>
        Paste this into the environment&apos;s <strong>Setup script</strong>{" "}
        field. It installs the pinned agent, writes managed enrollment, and
        registers a SessionStart hook that starts the agent in each session.
        From there the agent&apos;s normal policy sync installs plugins and
        keeps enforcement reconciled — the same flow as any other machine.
      </Text>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={identityEmailId}>Shared session identity</Label>
        <Input
          id={identityEmailId}
          type="email"
          value={identityEmail}
          onChange={setIdentityEmail}
          placeholder="claude-code-web@your-company.com"
          validate={(value) =>
            EMAIL_PATTERN.test(value.trim()) || "Enter a valid email"
          }
        />
        <Text small muted>
          Every session using this shared environment receives the policy and
          attribution of this identity. Use a dedicated org member or service
          account with the intended permissions, not your own email.
        </Text>
      </div>
      <CodeBlock language="bash" slots={slots}>
        {script}
      </CodeBlock>
      <Text small muted>
        Click{" "}
        <strong className="text-foreground">
          {hasExistingAgentKey ? "Rotate token" : "Generate token"}
        </strong>{" "}
        to mint the <code>org_token</code>. Pin this version; auto-update is
        disabled because the VM lives minutes. Anyone who can use the
        environment can read the token from the setup script or the resulting
        agent configuration.
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
            Anyone who can use the Claude environment can read this token.
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
              This expires the token currently stored in your Claude Code
              environment.
            </Dialog.Description>
          </Dialog.Header>
          <Alert variant="error">
            <AlertTitle>Remote sessions will stop syncing policy</AlertTitle>
            <AlertDescription>
              Save the updated setup script in the shared environment. Anthropic
              rebuilds the cached filesystem when the script changes, so
              subsequent sessions pick up the new <code>org_token</code>. Until
              then, policy will not sync.
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

export function RemoteSetupScriptStep(): React.JSX.Element {
  const { release, isError, isLoading } = usePinnedAgentDaemon();

  if (release) {
    return (
      <CloudSetupScript version={release.version} sha256={release.sha256} />
    );
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

export function RemoteNetworkAccessStep(): React.JSX.Element {
  return (
    <div className="flex flex-col gap-4">
      <Text muted>
        Create a <strong>shared</strong> Anthropic-hosted environment in{" "}
        <InlineLink href="https://claude.ai/admin-settings/cloud-environments">
          Cloud environments
        </InlineLink>{" "}
        for Claude Code on the web.
      </Text>
      <Text small muted>
        Trusted network access does not include Gram. Set{" "}
        <strong className="text-foreground">Custom</strong>, check{" "}
        <strong className="text-foreground">
          Also include default list of common package managers
        </strong>{" "}
        so GCS and package registries remain available, and add this host on its
        own line:
      </Text>
      <CodeBlock language="text">app.getgram.ai</CodeBlock>
      <Text small muted>
        Without this host the agent cannot fetch policy or send hook events.
      </Text>
    </div>
  );
}

export function RemoteOrganizationDefaultStep(): React.JSX.Element {
  return (
    <div className="flex flex-col gap-4">
      <Text small muted>
        Anthropic has no lock-members-to-this-environment switch. After creating
        the shared environment at{" "}
        <InlineLink href="https://claude.ai/admin-settings/cloud-environments">
          Cloud environments
        </InlineLink>
        , set it as the org default at{" "}
        <InlineLink href="https://claude.ai/admin-settings/claude-code">
          Claude Code admin settings
        </InlineLink>
        . This preselects it on web, desktop, and mobile when a member has not
        chosen another environment.
      </Text>
      <Text small muted>
        For CLI <code>claude --cloud</code>, merge this into Managed Settings
        after replacing <code>env_…</code> with the shared environment&apos;s
        ID. The managed value overrides a user&apos;s <code>/remote-env</code>{" "}
        default.
      </Text>
      <CodeBlock language="json">
        {buildCloudDefaultEnvironmentSnippet()}
      </CodeBlock>
      <Alert variant="info">
        <AlertTitle>The first session may briefly show pending</AlertTitle>
        <AlertDescription>
          On a fresh VM, Claude must clone the org&apos;s observability plugin
          before the agent can enforce its managed hooks. Server-managed{" "}
          <code>enabledPlugins</code> triggers that clone; the agent reconciles
          again after the bundle appears.
        </AlertDescription>
      </Alert>
    </div>
  );
}
