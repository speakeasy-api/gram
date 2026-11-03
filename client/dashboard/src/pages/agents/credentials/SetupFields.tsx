import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { ExternalLink } from "lucide-react";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { Fragment, useState, type JSX, type ReactNode } from "react";
import { Link } from "react-router";
import { RequireScope } from "@/components/require-scope";
import { useSdkClient } from "@/contexts/Sdk";
import { linkMcpServerUserSessionIssuer } from "@/lib/linkUserSessionIssuer";
import { ChangeIssuerConfirmDialog } from "@/pages/mcp/x/tabs/settings/sections/authentication/ChangeIssuerConfirmDialog";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { ConsoleRow, type ConsoleField } from "./ConsoleRow";
import type { SetupServer } from "./setupInstructions";
import { variableProblem, type CatalogVariable } from "./workloadCatalog";
import type { ProviderMismatch, SetupValues } from "./workloadTokenEndpoints";

/** One value a platform's policy asks the operator for. */
export function VariableField({
  variable,
  value,
  onChange,
}: {
  variable: CatalogVariable;
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  // Say nothing about an untouched field; an empty value already keeps
  // Continue disabled.
  const problem =
    value.trim().length > 0 ? variableProblem(variable, value) : null;
  const id = `setup-${variable.key}`;

  return (
    <Stack gap={2}>
      <Label htmlFor={id}>{variable.label}</Label>
      <Input
        id={id}
        value={value}
        placeholder={variable.placeholder}
        aria-invalid={problem !== null}
        // Only one of the two is ever on screen, so each case points at its
        // own id rather than naming a box that is not rendered.
        aria-describedby={
          problem !== null
            ? `${id}-error`
            : variable.help !== undefined
              ? `${id}-help`
              : undefined
        }
        onChange={onChange}
      />
      {problem !== null ? (
        <Text id={`${id}-error`} role="alert" small destructive>
          {problem}
        </Text>
      ) : (
        variable.help !== undefined && (
          <Text id={`${id}-help`} muted small>
            {variable.help}
          </Text>
        )
      )}
    </Stack>
  );
}

/** One value the operator types, where no policy declares it. */
export function TextField({
  id,
  label,
  value,
  onChange,
  placeholder,
  help,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  help?: string;
}): JSX.Element {
  return (
    <Stack gap={2}>
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        placeholder={placeholder}
        // Without this the help reads as loose text beside the field and a
        // screen reader announces the label alone.
        aria-describedby={help !== undefined ? `${id}-help` : undefined}
        onChange={onChange}
      />
      {help !== undefined && (
        <Text id={`${id}-help`} muted small>
          {help}
        </Text>
      )}
    </Stack>
  );
}

/**
 * The shared authorization server picker, with the recommended one marked.
 * Tokens are issued for the organization or project that owns the one chosen.
 */
function SharedServerSelect({
  setupValues,
  id,
}: {
  setupValues: SetupValues;
  id: string;
}): JSX.Element {
  return (
    <Select
      value={setupValues.selectedEndpointId}
      onValueChange={setupValues.onEndpointChange}
    >
      <SelectTrigger id={id} className="w-full">
        <SelectValue placeholder="Select a shared authorization server" />
      </SelectTrigger>
      <SelectContent>
        {setupValues.endpointGroups.map((group) => (
          <SelectGroup key={group.label}>
            <SelectLabel>{group.label}</SelectLabel>
            {group.options.map((option) => (
              <SelectItem key={option.id} value={option.id}>
                {option.id === setupValues.recommendedEndpointId
                  ? `${option.label} (recommended)`
                  : option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        ))}
      </SelectContent>
    </Select>
  );
}

/** The shared authorization server behind the values a step shows. */
export function ComputedStatus({
  setupValues,
}: {
  setupValues: SetupValues;
}): JSX.Element {
  if (setupValues.mismatch !== null) {
    return <ProviderMismatchNotice mismatch={setupValues.mismatch} />;
  }
  if (setupValues.unavailableReason !== null) {
    return (
      <Alert variant="info">
        <div className="text-sm">{setupValues.unavailableReason}</div>
      </Alert>
    );
  }
  return (
    <Stack gap={2}>
      <Label htmlFor="setup-token-endpoint">Shared authorization server</Label>
      <SharedServerSelect setupValues={setupValues} id="setup-token-endpoint" />
      <Text muted small>
        Tokens are issued for the organization or project that owns it.
      </Text>
    </Stack>
  );
}

/**
 * Says that an agent on an OIDC platform signs in through one shared
 * authorization server, lets the reader choose it, and names the servers that
 * aren't on it with a way to move them.
 */
export function SharedServerChoice({
  setupValues,
  onMoved,
}: {
  setupValues: SetupValues;
  onMoved: () => void;
}): JSX.Element {
  const count = setupValues.endpointGroups.reduce(
    (total, group) => total + group.options.length,
    0,
  );
  return (
    <Stack gap={3}>
      <Stack gap={1}>
        <Label htmlFor="agent-shared-server">Shared authorization server</Label>
        <Text muted small>
          An agent on GKE, Claude Tag or another platform signs in through a
          shared authorization server, and every MCP server it uses must be on
          the one you choose.{" "}
          {count === 1
            ? "This is the only one available."
            : "The recommended one is the server most of this agent's servers already use."}
        </Text>
      </Stack>
      {setupValues.checking ? (
        <Text muted small>
          Loading shared authorization servers…
        </Text>
      ) : count === 0 ? (
        <Alert variant="info">
          <div className="text-sm">
            {setupValues.unavailableReason ??
              "No shared authorization server is available yet."}
          </div>
        </Alert>
      ) : (
        <SharedServerSelect
          setupValues={setupValues}
          id="agent-shared-server"
        />
      )}
      {setupValues.mismatch !== null && count > 0 && (
        <ProviderMismatchNotice
          mismatch={setupValues.mismatch}
          onMoved={onMoved}
        />
      )}
    </Stack>
  );
}

/** Why a step cannot go on, in the error style every setup step shares. */
export function SetupError({ children }: { children: ReactNode }): JSX.Element {
  return (
    <Alert variant="error">
      <div className="text-sm">{children}</div>
    </Alert>
  );
}

/** Something on a step that will not work as the reader expects. */
export function SetupWarning({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  return (
    <Alert variant="warning">
      <div className="space-y-2 text-sm">{children}</div>
    </Alert>
  );
}

/**
 * The agent's servers that won't accept tokens from the chosen shared
 * authorization server, each with a way to move it there.
 */
function ProviderMismatchNotice({
  mismatch,
  onMoved,
}: {
  mismatch: ProviderMismatch;
  /**
   * Rereads the agent's servers after one is moved. Without it the notice
   * only links to each server's settings.
   */
  onMoved?: () => void;
}): JSX.Element {
  const target = mismatch.target;
  return (
    <Alert
      variant="warning"
      title={
        target
          ? `These servers aren't on ${target.slug}`
          : "These servers need a shared authorization server"
      }
    >
      <Stack gap={3}>
        <Text small>
          {target
            ? `They won't accept the token this agent gets from ${target.slug}. Move them there, choose another shared authorization server, or remove them from this agent.`
            : "Set up a shared authorization server for them, or remove them from this agent."}
        </Text>
        <ul className="divide-border w-full divide-y">
          {mismatch.servers.map(({ server, current }) => (
            <MismatchRow
              key={server.id}
              server={server}
              reason={
                current
                  ? `Uses ${current}.`
                  : "Uses its own sign-in, not a shared authorization server."
              }
              action={
                target && onMoved ? (
                  <MoveToProvider
                    server={server}
                    target={target}
                    onMoved={onMoved}
                  />
                ) : undefined
              }
            />
          ))}
        </ul>
      </Stack>
    </Alert>
  );
}

function MismatchRow({
  server,
  reason,
  action,
}: {
  server: SetupServer;
  reason: string;
  /** Fixes the server in place; replaces the settings link when given. */
  action?: ReactNode;
}): JSX.Element {
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2 first:pt-0 last:pb-0">
      <div className="min-w-0 flex-1">
        <Text small className="block truncate">
          {server.name}
        </Text>
        <Text muted small className="block">
          {reason}
        </Text>
      </div>
      {action ?? <SettingsLink server={server} />}
    </li>
  );
}

/** Opens the server's authentication settings in a new tab. */
function SettingsLink({ server }: { server: SetupServer }): JSX.Element | null {
  const routes = useRoutes({ projectSlug: server.projectSlug });
  if (!server.projectSlug) return null;
  return (
    <Button variant="secondary" size="sm" asChild>
      <Link
        to={`${routes.mcp.details.settings.href(
          mcpServerRouteParam({ id: server.id, slug: server.slug }),
        )}#authentication`}
        target="_blank"
        rel="noreferrer"
      >
        Open authentication settings
        <ExternalLink className="size-3.5" aria-hidden="true" />
      </Link>
    </Button>
  );
}

/**
 * Moves one server onto the shared provider the agent's token comes from,
 * after the same confirmation its settings page asks for: everyone connected
 * to it signs in again.
 */
function MoveToProvider({
  server,
  target,
  onMoved,
}: {
  server: SetupServer;
  target: { id: string; slug: string };
  onMoved: () => void;
}): JSX.Element {
  const client = useSdkClient();
  const [open, setOpen] = useState(false);
  const move = useMutation({
    mutationFn: () =>
      linkMcpServerUserSessionIssuer(client, server.id, target.id),
    onSuccess: () => {
      setOpen(false);
      toast.success(`${server.name} now uses ${target.slug}`);
      onMoved();
    },
  });
  return (
    <RequireScope
      scope="mcp:write"
      resourceId={server.resourceId}
      projectId={server.projectId}
      level="component"
    >
      {({ disabled }) =>
        disabled ? (
          <SettingsLink server={server} />
        ) : (
          <>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                move.reset();
                setOpen(true);
              }}
            >
              Move to {target.slug}
            </Button>
            <ChangeIssuerConfirmDialog
              open={open}
              onOpenChange={setOpen}
              target={{ attachImpactTarget: { mcpServerId: server.id } }}
              replacing
              candidate={target}
              onConfirm={() => move.mutate()}
              isPending={move.isPending}
              error={move.error}
            />
          </>
        )
      }
    </RequireScope>
  );
}

/**
 * The documents a step points at, on one row: stacked a line each they read
 * as a reading list standing between the reader and the step.
 */
export function Links({
  links,
}: {
  links: { href: string; label: string }[];
}): JSX.Element {
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1">
      {links.map((link) => (
        <a
          key={link.href}
          href={link.href}
          target="_blank"
          rel="noreferrer"
          className="text-foreground inline-flex items-center gap-1.5 text-sm underline underline-offset-4 hover:no-underline"
        >
          {link.label}
          <ExternalLink className="size-3.5 shrink-0" />
        </a>
      ))}
    </div>
  );
}

/**
 * The fields of a dialog in Claude's own console, in its order.
 *
 * That dialog is a compact form — a label, a box, a hint. Rendering each
 * field as a card of its own makes a page several times the height of the
 * thing it describes, which is the opposite of a guide: the reader holds two
 * screens side by side and the shapes do not match.
 */
export function Console({
  rows,
}: {
  rows: {
    label: ReactNode;
    link?: { href: string; label: string };
    fields?: ConsoleField[];
    hint?: ReactNode;
  }[];
}): JSX.Element {
  return (
    <ol className="border-border divide-border divide-y border">
      {rows.map((row, index) => (
        <ConsoleRow
          key={index}
          number={index + 1}
          label={row.label}
          link={row.link}
          fields={row.fields}
          hint={row.hint}
        />
      ))}
    </ol>
  );
}

/**
 * Why a value the snippet needs is not filled in, and what stands in for it
 * meanwhile. The reason comes first because it is what the reader acts on;
 * the placeholder line tells them the snippet is not ready to paste yet.
 */
export function PendingValues({
  placeholders,
  children,
}: {
  placeholders: string[];
  /** Why Speakeasy cannot fill the values in yet. */
  children?: ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-2">
      {children}
      <Text muted small className="block">
        Until then, the snippet below shows{" "}
        {placeholders.map((placeholder, index) => (
          <span key={placeholder}>
            {index > 0 && " and "}
            <code>{placeholder}</code>
          </span>
        ))}{" "}
        in {placeholders.length > 1 ? "their places" : "its place"}. Replace{" "}
        {placeholders.length > 1 ? "them" : "it"} before using it.
      </Text>
    </div>
  );
}

/** A project's own issuer for the URL a catalog flow trusts organization-wide. */
export type ProjectIssuer = {
  project: { id: string; name: string; slug: string };
  issuer: WorkloadIssuer;
};

/**
 * Names every project whose own issuer takes precedence over the
 * organization-wide rule for that project's MCP servers, and how to withdraw
 * each one.
 */
export function ShadowingIssuers({
  shadows,
  platformName,
}: {
  shadows: ProjectIssuer[];
  platformName: string;
}): JSX.Element {
  const one = shadows.length === 1;
  const names = shadows.map(({ project }, index) => (
    <Fragment key={project.id}>
      {index > 0 && ", "}
      <strong>{project.name || project.slug}</strong>
    </Fragment>
  ));
  return (
    <SetupWarning>
      <p>
        {one ? "The project " : "The projects "}
        {names} also {one ? "trusts" : "trust"} {platformName} on{" "}
        {one ? "its" : "their"} own, at the same issuer URL.{" "}
        {one ? "Its" : "Their"} MCP servers check a job against the
        project&apos;s own issuer instead of the rule saved here, so the job
        reaches them as another identity, or not at all. Projects you cannot
        open were not checked.
      </p>
      {/* DNO-1477 tracks a dashboard control for withdrawing a project's
            own issuer; until it ships, the API is the only way. */}
      <p>
        Removing an agent&apos;s access does not fix this: the project&apos;s
        own issuer has to be withdrawn, which also withdraws every rule admitted
        under it. The dashboard has no control for that yet, so ask someone who
        can manage workload identities to withdraw it.
      </p>
      <details>
        <summary className="cursor-pointer">Technical details</summary>
        <p className="mt-2">Withdraw each through the API:</p>
        <ul className="mt-1 list-disc space-y-1 pl-5">
          {shadows.map(({ project, issuer }) => (
            <li key={issuer.id}>
              <code>{`DELETE /rpc/workloadIdentities.withdrawIssuer?id=${issuer.id}`}</code>{" "}
              with the header <code>Gram-Project: {project.slug}</code>, for{" "}
              {project.name || project.slug}
            </li>
          ))}
        </ul>
      </details>
    </SetupWarning>
  );
}
