import { Alert } from "@/components/ui/Alert";
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
import { ExternalLink } from "lucide-react";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { Fragment, type JSX, type ReactNode } from "react";
import { ConsoleRow, type ConsoleField } from "./ConsoleRow";
import { variableProblem, type CatalogVariable } from "./workloadCatalog";
import type { SetupValues } from "./workloadTokenEndpoints";

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
  if (setupValues.unavailableReason !== null) {
    return (
      <Alert variant="info">
        <div className="text-sm">{setupValues.unavailableReason}</div>
      </Alert>
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
    <Alert variant="warning">
      <div className="space-y-2 text-sm">
        <p>
          {one ? "The project " : "The projects "}
          {names} also {one ? "trusts" : "trust"} {platformName} on{" "}
          {one ? "its" : "their"} own, at the same issuer URL.{" "}
          {one ? "Its" : "Their"} MCP servers check a job against the
          project&apos;s own issuer instead of the rule saved here, so the job
          reaches them as another identity, or not at all.
        </p>
        {/* DNO-1477 tracks a dashboard control for withdrawing a project's
            own issuer; until it ships, the API is the only way. */}
        <p>
          Removing an agent&apos;s access does not fix this: the project&apos;s
          own issuer has to be withdrawn, which also withdraws every rule
          admitted under it. The dashboard has no control for that yet, so ask
          someone who can manage workload identities to withdraw it.
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
      </div>
    </Alert>
  );
}
