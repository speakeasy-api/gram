import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { Input } from "@/components/ui/Input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { safeExternalHttpUrl } from "@/lib/safe-external-url";
import { cn } from "@/lib/utils";
import {
  ArrowUpRight,
  BookOpen,
  Check,
  ChevronDown,
  ChevronRight,
  Info,
  KeyRound,
  Loader2,
  Settings,
  X,
} from "lucide-react";
import type * as React from "react";
import { useState } from "react";
import { Link } from "react-router";
import type {
  ClientOption,
  ProviderOption,
  RegistrationChoice,
  RegistrationMethod,
  UserIdentityDraft,
} from "../drafts/useIdentityDraft";

/** What the provider control says before it knows. */
function providerLabel(
  draft: UserIdentityDraft,
  selected: ProviderOption | null,
): string {
  if (selected) return selected.name;
  if (draft.providerLoading) return "Checking\u2026";
  return "Choose an identity provider";
}

/**
 * The frameless combobox trigger the provider and registration pickers share.
 * AIM-230 asks for the provider row to read as settled configuration rather
 * than an unanswered form field, so the control carries no border until it is
 * hovered.
 */
function ScopeTrigger({
  children,
  className,
  ariaLabel,
  // Rest and ref both matter: this renders under <PopoverTrigger asChild>,
  // which clones it with the onClick, aria-expanded and ref that make the
  // menu open. Swallowing them leaves a button that renders and does nothing.
  ...props
}: React.ComponentProps<"button"> & { ariaLabel: string }): JSX.Element {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      {...props}
      className={cn(
        "text-muted-foreground hover:text-foreground hover:bg-muted -mx-1 inline-flex items-center gap-1 px-1 py-0.5 whitespace-nowrap disabled:pointer-events-none disabled:opacity-50",
        className,
      )}
    >
      {children}
    </button>
  );
}

function ProviderItem({
  option,
  selected,
  onSelect,
}: {
  option: ProviderOption;
  selected: boolean;
  onSelect: () => void;
}): JSX.Element {
  return (
    <CommandItem
      value={`${option.name} ${option.url}`}
      onSelect={onSelect}
      className="flex items-center justify-between gap-2"
    >
      <span className="flex min-w-0 flex-col">
        <span className="flex items-center gap-1.5">
          {option.name}
          {option.isNew ? (
            <span className="bg-warning-500 size-1.5 shrink-0 rounded-full" />
          ) : null}
        </span>
        <span className="text-muted-foreground font-mono text-xs">
          {option.url}
        </span>
      </span>
      {selected ? <Check className="size-4 shrink-0" /> : null}
    </CommandItem>
  );
}

/**
 * One Remote Identity Provider, the client the server uses with it, and how
 * to get a new one — the whole User Identity decision on a single settings
 * row. A connected client reads as a status; clearing it opens the choice of
 * an existing client, automatic registration or manual credentials. The
 * issuer and client records behind it stay on the Remote Identity Provider
 * pages.
 */
export function UserIdentityRow({
  draft,
  disabled,
  createHref,
  clientHref,
}: {
  draft: UserIdentityDraft;
  disabled: boolean;
  createHref: string;
  clientHref: (issuerId: string, clientId: string) => string;
}): JSX.Element {
  const [providerOpen, setProviderOpen] = useState(false);
  const { selected } = draft;
  const connectedHref =
    selected && draft.connectedClient
      ? clientHref(selected.id, draft.connectedClient.id)
      : null;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="relative flex min-w-0 items-center gap-3">
          <div className="bg-card flex size-10 shrink-0 items-center justify-center border">
            <KeyRound aria-hidden="true" className="size-4" />
          </div>
          <div className="flex min-w-0 flex-col gap-0.5">
            <div className="flex items-center gap-2">
              <Popover open={providerOpen} onOpenChange={setProviderOpen}>
                <PopoverTrigger asChild>
                  <ScopeTrigger
                    // Held while discovery is still running: this control is
                    // about to answer its own question, and offering "choose
                    // one" in the meantime invites a pick that the arriving
                    // default would appear to overwrite.
                    disabled={disabled || draft.providerLoading}
                    ariaLabel="Identity provider"
                    className={cn(
                      "gap-1.5",
                      selected && "text-foreground text-base font-medium",
                    )}
                  >
                    {providerLabel(draft, selected)}
                    <ChevronDown
                      aria-hidden="true"
                      className="text-muted-foreground size-3.5"
                    />
                  </ScopeTrigger>
                </PopoverTrigger>
                <PopoverContent align="start" className="w-80 p-0">
                  <Command>
                    <CommandList>
                      <CommandEmpty>No identity providers.</CommandEmpty>
                      {draft.providerGroups.map((group) => (
                        <CommandGroup key={group.tier} heading={group.tier}>
                          {group.options.length === 0 ? (
                            <Text muted variant="small" className="px-2 py-1.5">
                              None yet
                            </Text>
                          ) : (
                            group.options.map((option) => (
                              <ProviderItem
                                key={option.id}
                                option={option}
                                selected={option.id === selected?.id}
                                onSelect={() => {
                                  draft.selectProvider(option.id);
                                  setProviderOpen(false);
                                }}
                              />
                            ))
                          )}
                        </CommandGroup>
                      ))}
                    </CommandList>
                  </Command>
                  <div className="border-t p-2">
                    <Link
                      to={createHref}
                      className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm underline underline-offset-2"
                    >
                      Create a custom identity provider
                      <ArrowUpRight aria-hidden="true" className="size-3.5" />
                    </Link>
                  </div>
                </PopoverContent>
              </Popover>
              {selected?.isNew ? (
                <Badge
                  variant="warning"
                  size="sm"
                  title="No matching identity provider exists yet. One is created from what the upstream publishes when you save."
                >
                  <Badge.Text>Will be created</Badge.Text>
                </Badge>
              ) : null}
              <Text muted variant="small" className="font-mono text-xs">
                {selected?.url ?? ""}
              </Text>
            </div>
            {selected ? (
              <ClientStatus draft={draft} disabled={disabled} />
            ) : null}
          </div>
        </div>

        {draft.connected ? (
          <ConnectedSummary draft={draft} advancedHref={connectedHref} />
        ) : null}
        {draft.cleared ? (
          <Button
            variant="tertiary"
            size="sm"
            disabled={disabled}
            onClick={draft.cancelClear}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
        ) : null}
      </div>

      {draft.providerLoadFailed ? (
        <Alert variant="error" dismissible={false}>
          Couldn&apos;t load this server&apos;s identity providers. Refresh the
          page to try again.
        </Alert>
      ) : null}

      {draft.providerUnreachable ? (
        <Alert variant="warning" dismissible={false}>
          Couldn&apos;t reach the upstream&apos;s identity provider. Check the
          server URL, or choose a provider from the list.
        </Alert>
      ) : null}

      {selected && !draft.connected ? (
        <div className="space-y-4 pl-[52px]">
          <RegistrationChoices draft={draft} disabled={disabled} />
          <ChoiceDetails
            draft={draft}
            disabled={disabled}
            providerName={selected.name}
          />
        </div>
      ) : null}

      {draft.status.kind === "pending" ? (
        <div className="flex items-center gap-2">
          <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
          <Text muted small>
            Registering with {selected?.name ?? "the provider"}…
          </Text>
        </div>
      ) : null}
    </div>
  );
}

const STATUS_DOT = "size-2 shrink-0 rounded-full";

/**
 * The line under the provider: whether the server has a client, and the way
 * to clear it. Clearing only changes the draft; Save is what replaces it.
 */
function ClientStatus({
  draft,
  disabled,
}: {
  draft: UserIdentityDraft;
  disabled: boolean;
}): JSX.Element {
  if (draft.connected) {
    return (
      <span className="flex items-center gap-1.5">
        <span
          aria-hidden="true"
          className={cn(STATUS_DOT, "bg-[var(--fill-success-default)]")}
        />
        <Text small>Connected</Text>
        <Button
          variant="tertiary"
          size="xs"
          aria-label="Clear connection"
          tooltip="Clear connection"
          disabled={disabled}
          onClick={draft.clear}
          className="w-7 px-0"
        >
          <Button.LeftIcon>
            <X aria-hidden="true" />
          </Button.LeftIcon>
        </Button>
      </span>
    );
  }
  if (draft.cleared) {
    return (
      <span className="flex items-center gap-1.5">
        <span aria-hidden="true" className={cn(STATUS_DOT, "bg-warning-500")} />
        <Text small warning>
          Unconfigured client
        </Text>
      </span>
    );
  }
  return (
    <span className="flex items-center gap-1.5">
      <span
        aria-hidden="true"
        className={cn(STATUS_DOT, "border-muted-foreground border")}
      />
      <Text small muted>
        Not connected
      </Text>
    </span>
  );
}

function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

/** Who is using the connected client, and the way into its settings. */
function ConnectedSummary({
  draft,
  advancedHref,
}: {
  draft: UserIdentityDraft;
  advancedHref: string | null;
}): JSX.Element {
  const scopes = draft.connectedClient?.scopes ?? [];
  return (
    <div className="flex items-center gap-4">
      <Text small muted>
        {draft.signedIn !== null ? (
          <>
            <span className="text-foreground">
              {plural(draft.signedIn, "person", "people")}
            </span>{" "}
            signed in
          </>
        ) : null}
        {draft.signedIn !== null && scopes.length > 0 ? (
          <span className="mx-1.5">·</span>
        ) : null}
        {scopes.length > 0 ? <ScopeList scopes={scopes} /> : null}
      </Text>
      {advancedHref ? (
        <Button variant="tertiary" size="sm" asChild>
          <Link to={advancedHref}>
            <Button.LeftIcon>
              <Settings aria-hidden="true" />
            </Button.LeftIcon>
            <Button.Text>Advanced</Button.Text>
          </Link>
        </Button>
      ) : null}
    </div>
  );
}

/**
 * The connected client's scopes: a count that opens the list. The count is
 * the summary; which scopes were requested is the detail an operator checks
 * when a tool call comes back forbidden.
 */
function ScopeList({ scopes }: { scopes: string[] }): JSX.Element {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="hover:text-foreground underline decoration-dotted underline-offset-3"
        >
          <span className="text-foreground">{scopes.length}</span>{" "}
          {scopes.length === 1 ? "scope" : "scopes"}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-64 p-3">
        <Text small muted className="text-eyebrow mb-2 block">
          Requested scopes
        </Text>
        <ul className="space-y-1">
          {scopes.map((scope) => (
            <li key={scope} className="font-mono text-xs break-all">
              {scope}
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}

/** Why a card is unavailable, behind an info mark on its title. */
function UnavailableReason({ reason }: { reason: string }): JSX.Element {
  return (
    <HoverCard openDelay={150}>
      <HoverCardTrigger asChild>
        <span className="inline-flex cursor-help items-center gap-1">
          Not available
          <Info aria-label={reason} className="size-3" />
        </span>
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        <Text small className="block">
          {reason}
        </Text>
      </HoverCardContent>
    </HoverCard>
  );
}

/** The three ways to give a server a client, as one radio group. */
function RegistrationChoices({
  draft,
  disabled,
}: {
  draft: UserIdentityDraft;
  disabled: boolean;
}): JSX.Element {
  const name = draft.selected?.name ?? "the provider";
  const loading = draft.clientsLoading || draft.capabilitiesLoading;
  return (
    <RadioCardGroup
      orientation="horizontal"
      value={draft.choice}
      disabled={disabled || loading}
      onValueChange={(value) => draft.selectChoice(value as RegistrationChoice)}
      className="grid-flow-row grid-cols-1 md:grid-flow-col md:grid-cols-none"
    >
      <RadioCard
        value="existing"
        title="Existing client"
        disabled={!draft.existingAvailable}
      >
        {draft.existingAvailable ? (
          `Reuse a client already registered with ${name}.`
        ) : (
          <UnavailableReason
            reason={`No clients are registered with ${name} yet. Auto-Configure or Manual creates the first one.`}
          />
        )}
      </RadioCard>
      <RadioCard
        value="auto"
        title="Auto-Configure"
        disabled={!draft.automaticAvailable}
      >
        {draft.automaticAvailable ? (
          `Speakeasy registers a new client with ${name} when you save.`
        ) : (
          <UnavailableReason
            reason={`${name} supports neither a Client ID Metadata Document nor dynamic client registration, so Speakeasy can't register a client automatically.`}
          />
        )}
      </RadioCard>
      <RadioCard value="manual" title="Manual">
        {`Paste a client ID and secret issued by ${name}.`}
      </RadioCard>
    </RadioCardGroup>
  );
}

/** The fields the selected choice needs, under the cards. */
function ChoiceDetails({
  draft,
  disabled,
  providerName,
}: {
  draft: UserIdentityDraft;
  disabled: boolean;
  providerName: string;
}): JSX.Element | null {
  switch (draft.choice) {
    case "existing":
      return <ExistingClientField draft={draft} disabled={disabled} />;
    case "auto":
      return draft.methodChoiceAvailable ? (
        <AdvancedOptions>
          <RegistrationMethodField draft={draft} disabled={disabled} />
        </AdvancedOptions>
      ) : null;
    case "manual":
      return (
        <ManualCredentialsFields
          draft={draft}
          disabled={disabled}
          providerName={providerName}
        />
      );
  }
}

function ExistingClientField({
  draft,
  disabled,
}: {
  draft: UserIdentityDraft;
  disabled: boolean;
}): JSX.Element {
  return (
    <div className="max-w-md space-y-1.5">
      <Text small className="block font-medium">
        Client
      </Text>
      <Select
        value={draft.existingClientId ?? undefined}
        onValueChange={draft.selectExisting}
        disabled={disabled}
      >
        <SelectTrigger aria-label="Client">
          <SelectValue placeholder="Choose a client" />
        </SelectTrigger>
        <SelectContent>
          {draft.existingOptions.map((option: ClientOption) => (
            <SelectItem key={option.id} value={option.id}>
              {option.name}
              {option.hint ? (
                <span className="text-muted-foreground ml-2 font-mono text-xs">
                  {option.hint}
                </span>
              ) : null}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {draft.sameAsConnected ? (
        <Text muted small className="block">
          This is the client the server already uses. Choose another to replace
          it.
        </Text>
      ) : null}
    </div>
  );
}

/** A collapsed disclosure for settings most people leave alone. */
function AdvancedOptions({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  return (
    <Collapsible>
      <CollapsibleTrigger className="group text-foreground flex items-center gap-1.5 text-sm">
        <ChevronRight
          aria-hidden="true"
          className="size-3.5 transition-transform group-data-[state=open]:rotate-90"
        />
        Advanced
      </CollapsibleTrigger>
      <CollapsibleContent className="max-w-md space-y-1.5 pt-3 pl-5">
        {children}
      </CollapsibleContent>
    </Collapsible>
  );
}

const REGISTRATION_METHOD_OPTIONS: {
  value: RegistrationMethod;
  label: string;
}[] = [
  { value: "cimd", label: "CIMD" },
  { value: "dcr", label: "DCR" },
];

function RegistrationMethodField({
  draft,
  disabled,
}: {
  draft: UserIdentityDraft;
  disabled: boolean;
}): JSX.Element {
  return (
    <>
      <Text small className="block font-medium">
        Registration method
      </Text>
      <SegmentedControl
        value={draft.registrationMethod}
        onChange={draft.setRegistrationMethod}
        options={REGISTRATION_METHOD_OPTIONS}
        disabled={disabled}
      />
      <Text muted small className="block">
        CIMD publishes the client from Speakeasy; DCR registers it with the
        provider.
      </Text>
    </>
  );
}

function ManualCredentialsFields({
  draft,
  disabled,
  providerName,
}: {
  draft: UserIdentityDraft;
  disabled: boolean;
  providerName: string;
}): JSX.Element {
  // The guide URL comes from issuer metadata, so it is upstream input: only
  // render the action once it is known to be an ordinary http(s) link.
  const registrationGuideUrl = safeExternalHttpUrl(draft.registrationGuideUrl);
  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1">
          <Text muted small className="block">
            Client ID
          </Text>
          <Input
            value={draft.clientId}
            onChange={draft.setClientId}
            placeholder={`from ${providerName}`}
            disabled={disabled}
            aria-label="Client ID"
            noAutofill
          />
        </div>
        <div className="space-y-1">
          <Text muted small className="block">
            Client secret
          </Text>
          <Input
            type="password"
            value={draft.clientSecret}
            onChange={draft.setClientSecret}
            placeholder="Optional"
            disabled={disabled}
            aria-label="Client secret"
            noAutofill
          />
        </div>
      </div>
      <AdvancedOptions>
        <Text small className="block font-medium">
          Scope
        </Text>
        <Input
          value={draft.scopeText}
          onChange={draft.setScopeText}
          placeholder="read write"
          disabled={disabled}
          aria-label="Scope"
        />
        <Text muted small className="block">
          Space-separated. Leave blank to request the scopes {providerName}{" "}
          advertises.
        </Text>
      </AdvancedOptions>
      {registrationGuideUrl ? (
        <Button variant="secondary" size="sm" asChild>
          <a href={registrationGuideUrl} target="_blank" rel="noreferrer">
            <Button.LeftIcon>
              <BookOpen aria-hidden="true" className="size-3.5" />
            </Button.LeftIcon>
            <Button.Text>Open registration guide</Button.Text>
          </a>
        </Button>
      ) : null}
    </div>
  );
}
