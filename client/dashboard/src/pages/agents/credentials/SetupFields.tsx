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
import type { JSX, ReactNode } from "react";
import { Link } from "react-router";
import { ConsoleRow, type ConsoleField } from "./ConsoleRow";
import { SwitchServerSignIn } from "./SwitchServerSignIn";
import { useSwitchAvailability } from "./useSwitchAvailability";
import { variableProblem, type CatalogVariable } from "./workloadCatalog";
import type { SetupValues, UncoveredServer } from "./workloadTokenEndpoints";

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
 * Which user session issuer the derived values come from, and why there are
 * none when there are none.
 */
export function ComputedStatus({
  setupValues,
}: {
  setupValues: SetupValues;
}): JSX.Element {
  const leftOut = setupValues.uncoveredServers;
  if (setupValues.unavailableReason !== null) {
    return (
      <Stack gap={3}>
        <Alert variant={leftOut.length > 0 ? "warning" : "info"}>
          <div className="text-sm">{setupValues.unavailableReason}</div>
        </Alert>
        {leftOut.length > 0 && (
          <LeftOutServers
            servers={leftOut}
            onSelect={setupValues.onEndpointChange}
          />
        )}
      </Stack>
    );
  }
  return (
    <Stack gap={2}>
      <Label htmlFor="setup-token-endpoint">User session issuer</Label>
      <Select
        value={setupValues.selectedEndpointId}
        onValueChange={setupValues.onEndpointChange}
      >
        <SelectTrigger id="setup-token-endpoint" className="w-full">
          <SelectValue placeholder="Select an issuer" />
        </SelectTrigger>
        <SelectContent>
          {setupValues.endpointGroups.map((group) => (
            <SelectGroup key={group.label}>
              <SelectLabel>{group.label}</SelectLabel>
              {group.options.map((option) => (
                <SelectItem key={option.id} value={option.id}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>
      <Text muted small>
        Tokens are issued for the organization or project that owns this issuer.
      </Text>
      {leftOut.length > 0 && (
        <Alert
          variant="warning"
          title={`This token won't work for ${
            leftOut.length === 1
              ? "1 of this agent's servers"
              : `${leftOut.length} of this agent's servers`
          }`}
        >
          <LeftOutServers
            servers={leftOut}
            onSelect={setupValues.onEndpointChange}
          />
        </Alert>
      )}
    </Stack>
  );
}

/**
 * The agent's servers that refuse the selected issuer's token, each with the
 * one thing that fixes it: selecting the shared issuer it signs in through,
 * switching it to a shared one, or its own sign-in settings when switching
 * here is not safe or not allowed.
 */
function LeftOutServers({
  servers,
  onSelect,
}: {
  servers: UncoveredServer[];
  onSelect: (issuerId: string) => void;
}): JSX.Element {
  return (
    <ul className="divide-border w-full divide-y">
      {servers.map((entry) => (
        <li
          key={entry.server.id}
          className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2 first:pt-0 last:pb-0"
        >
          {entry.reason === "another_issuer" ? (
            <LeftOutRow
              name={entry.server.name}
              reason="Signs in through another user session issuer. A workload needs a separate token from that issuer for it."
              action={
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => onSelect(entry.server.issuerId!)}
                >
                  <Button.Text>Use its issuer</Button.Text>
                </Button>
              }
            />
          ) : entry.switchTo ? (
            <NotSharedRow server={entry.server} target={entry.switchTo} />
          ) : (
            <LeftOutRow
              name={entry.server.name}
              reason={`${NOT_SHARED} There is no shared user session issuer it could move to yet.`}
              action={<ServerSignInSettingsLink server={entry.server} />}
            />
          )}
        </li>
      ))}
    </ul>
  );
}

const NOT_SHARED =
  "Its user session issuer has no shared authorization server, so a workload can't get a token for it.";

/** A server whose issuer is not shared, with a shared one to move it to. */
function NotSharedRow({
  server,
  target,
}: {
  server: UncoveredServer["server"];
  target: { id: string; label: string };
}): JSX.Element {
  const availability = useSwitchAvailability(server);
  switch (availability) {
    case "ready":
      return (
        <LeftOutRow
          name={server.name}
          reason={NOT_SHARED}
          action={<SwitchServerSignIn server={server} target={target} />}
        />
      );
    case "checking":
      return (
        <LeftOutRow
          name={server.name}
          reason={NOT_SHARED}
          action={
            <Button variant="secondary" size="sm" disabled>
              <Button.Text>Switch to {target.label}</Button.Text>
            </Button>
          }
        />
      );
    case "people_connect":
      return (
        <LeftOutRow
          name={server.name}
          reason={`${NOT_SHARED} People connect their own accounts through its current sign-in, so move it to ${target.label} from its sign-in settings, where their connections can be set up again.`}
          action={<ServerSignInSettingsLink server={server} />}
        />
      );
    case "no_permission":
      return (
        <LeftOutRow
          name={server.name}
          reason={`${NOT_SHARED} Ask someone who can edit ${server.name} to switch it to ${target.label}.`}
          action={<ServerSignInSettingsLink server={server} />}
        />
      );
  }
}

function LeftOutRow({
  name,
  reason,
  action,
}: {
  name: string;
  reason: string;
  action: ReactNode;
}): JSX.Element {
  return (
    <>
      <div className="min-w-0 flex-1">
        <Text small className="block truncate">
          {name}
        </Text>
        <Text muted small className="block">
          {reason}
        </Text>
      </div>
      {action}
    </>
  );
}

function ServerSignInSettingsLink({
  server,
}: {
  server: UncoveredServer["server"];
}): JSX.Element | null {
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
        Open sign-in settings
        <ExternalLink className="size-3.5" aria-hidden="true" />
      </Link>
    </Button>
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
