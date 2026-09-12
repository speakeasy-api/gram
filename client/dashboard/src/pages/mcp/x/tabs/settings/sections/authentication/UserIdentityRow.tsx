import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import { Input } from "@/components/ui/Input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import {
  ArrowUpRight,
  BookOpen,
  Check,
  ChevronDown,
  KeyRound,
  Loader2,
  Sparkles,
} from "lucide-react";
import type * as React from "react";
import { useState } from "react";
import { Link } from "react-router";
import type { ProviderOption, UserIdentityDraft } from "./useUserIdentityDraft";

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
 * One Remote Identity Provider, one registration choice, and the outcome —
 * the whole User Identity decision on a single settings row. The issuer and
 * client records behind it stay on the Remote Identity Provider pages.
 */
export function UserIdentityRow({
  draft,
  disabled,
  manageHref,
  createHref,
  inspectHref,
  onSwitchToAgent,
}: {
  draft: UserIdentityDraft;
  disabled: boolean;
  manageHref: string;
  createHref: string;
  inspectHref: string;
  onSwitchToAgent: () => void;
}): JSX.Element {
  const [providerOpen, setProviderOpen] = useState(false);
  const [clientOpen, setClientOpen] = useState(false);
  const { selected, status } = draft;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="relative flex min-w-0 items-center gap-3">
          <div className="bg-card flex size-10 shrink-0 items-center justify-center border">
            <KeyRound aria-hidden="true" className="size-4" />
          </div>
          <div className="flex min-w-0 flex-col gap-0.5">
            <div className="flex items-center gap-2">
              <Popover open={providerOpen} onOpenChange={setProviderOpen}>
                <PopoverTrigger asChild>
                  <ScopeTrigger
                    disabled={disabled}
                    ariaLabel="Identity provider"
                    className={cn(
                      "gap-1.5",
                      selected && "text-foreground text-base font-medium",
                    )}
                  >
                    {selected ? selected.name : "Choose an identity provider"}
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
            </div>
            <Text muted variant="small" className="block font-mono text-xs">
              {selected?.url ?? ""}
            </Text>
          </div>
        </div>

        {selected ? (
          <div className="relative flex shrink-0 flex-col items-end gap-0.5">
            <Popover open={clientOpen} onOpenChange={setClientOpen}>
              <PopoverTrigger asChild>
                <ScopeTrigger disabled={disabled} ariaLabel="Registration">
                  {!draft.existingClient && !draft.manualNeeded ? (
                    // text-default-success is green-700 — so dark next to
                    // muted body text that it reads as olive. The success
                    // fill token is the brighter mark green, and it flips to
                    // a legible shade in dark mode.
                    <Sparkles
                      aria-hidden="true"
                      className="size-3.5 text-[var(--fill-success-default)]"
                    />
                  ) : null}
                  <span className="text-sm">{draft.clientLabel}</span>
                  <ChevronDown aria-hidden="true" className="size-3.5" />
                </ScopeTrigger>
              </PopoverTrigger>
              <PopoverContent align="start" className="w-80 p-0">
                <Command>
                  <CommandList>
                    <CommandGroup>
                      <CommandItem
                        value="new-client"
                        onSelect={() => {
                          draft.selectClient(null);
                          setClientOpen(false);
                        }}
                        className="flex items-center justify-between gap-2"
                      >
                        <span className="flex min-w-0 flex-col">
                          <span className="flex items-center gap-1.5">
                            {draft.manualNeeded ? (
                              "New client"
                            ) : (
                              <>
                                <Sparkles
                                  aria-hidden="true"
                                  className="size-3.5"
                                />
                                Auto-Configure
                              </>
                            )}
                          </span>
                          <span className="text-muted-foreground font-mono text-xs">
                            {draft.newClientHint}
                          </span>
                        </span>
                        {!draft.existingClient ? (
                          <Check className="size-4 shrink-0" />
                        ) : null}
                      </CommandItem>
                    </CommandGroup>
                    <CommandGroup heading={`Existing on ${selected.name}`}>
                      {draft.clientOptions.length === 0 ? (
                        <Text muted variant="small" className="px-2 py-1.5">
                          {draft.clientsLoading ? "Loading…" : "None yet"}
                        </Text>
                      ) : (
                        draft.clientOptions.map((option) => (
                          <CommandItem
                            key={option.id}
                            value={option.name}
                            onSelect={() => {
                              draft.selectClient(option.id);
                              setClientOpen(false);
                            }}
                            className="flex items-center justify-between gap-2"
                          >
                            <span className="flex min-w-0 flex-col">
                              <span className="truncate">{option.name}</span>
                              <span className="text-muted-foreground font-mono text-xs">
                                {option.connections} connection
                                {option.connections === 1 ? "" : "s"}
                              </span>
                            </span>
                            {draft.existingClient?.id === option.id ? (
                              <Check className="size-4 shrink-0" />
                            ) : null}
                          </CommandItem>
                        ))
                      )}
                    </CommandGroup>
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>
            {draft.clientHasSessions === false ? (
              <Link
                to={inspectHref}
                className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-xs"
              >
                <span
                  aria-hidden="true"
                  className="bg-muted-foreground/50 size-1.5 shrink-0 rounded-full"
                />
                No one has connected yet
              </Link>
            ) : (
              <Text muted variant="small" className="block text-xs">
                {draft.clientCaption}
              </Text>
            )}
          </div>
        ) : null}
      </div>

      {draft.providerUnreachable ? (
        <Alert variant="warning" dismissible={false}>
          Couldn&apos;t reach the upstream&apos;s identity provider. Check the
          server URL, or choose a provider from the list.
        </Alert>
      ) : null}

      {draft.manualNeeded && selected ? (
        <div className="space-y-3">
          <Text muted small className="block">
            This provider can&apos;t register the server on its own. Register
            one with {selected.name} and paste what it gives you.
          </Text>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1">
              <Text muted small className="block">
                Client ID
              </Text>
              <Input
                value={draft.clientId}
                onChange={draft.setClientId}
                placeholder={`from ${selected.name}`}
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
          {draft.registrationGuideUrl ? (
            <Button variant="secondary" size="sm" asChild>
              <a
                href={draft.registrationGuideUrl}
                target="_blank"
                rel="noreferrer"
              >
                <Button.LeftIcon>
                  <BookOpen aria-hidden="true" className="size-3.5" />
                </Button.LeftIcon>
                <Button.Text>Open registration guide</Button.Text>
              </a>
            </Button>
          ) : null}
        </div>
      ) : null}

      {status.kind === "idle" ? (
        <Text muted small className="block">
          {draft.idleHint}
        </Text>
      ) : null}

      {status.kind === "pending" ? (
        <div className="flex items-center gap-2">
          <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
          <Text muted small>
            Registering with {selected?.name ?? "the provider"}…
          </Text>
        </div>
      ) : null}

      {status.kind === "refused" ? (
        <Alert variant="error" dismissible={false}>
          <div className="space-y-2">
            <Text small className="block font-medium">
              {selected?.name ?? "The provider"} refused to register this server
              automatically.
            </Text>
            <Text muted small className="block">
              {status.message ??
                "That is on the provider's side, not something to retry. Pick a way forward:"}
            </Text>
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="secondary" size="sm" onClick={onSwitchToAgent}>
                <Button.Text>Switch to Agent Identity</Button.Text>
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={draft.enterCredentialsManually}
              >
                <Button.Text>Enter credentials manually</Button.Text>
              </Button>
              <Button variant="tertiary" size="sm" asChild>
                <Link to={manageHref}>
                  <Button.Text>Manage identity providers</Button.Text>
                </Link>
              </Button>
            </div>
          </div>
        </Alert>
      ) : null}

      {status.kind === "unreachable" ? (
        <Alert variant="warning" dismissible={false}>
          {status.message ??
            `Couldn't reach ${selected?.name ?? "the provider"} to register this server. Save again to retry.`}
        </Alert>
      ) : null}
    </div>
  );
}
