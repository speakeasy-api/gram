import { isRelativePath } from "@/components/setup-steps/origin";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { ChevronRight, KeyRound, Waypoints } from "lucide-react";
import { useState } from "react";
import type { JSX, ReactNode } from "react";
import { CUSTOM_PLATFORM_KEY } from "./CustomSetupInstructions";
import { hasPlatformSetup } from "./platformSetups";
import type { CatalogEntry } from "./workloadCatalog";

const API_KEY_VALUE = "api-key";
// Prefixed so a catalog key can never be mistaken for the API key option.
const PLATFORM_PREFIX = "platform:";

/** Platforms whose issuer is known but whose preset has not shipped yet. */
const PLANNED = [
  { key: "github-actions", displayName: "GitHub Actions" },
  { key: "kubernetes", displayName: "Kubernetes" },
  { key: "spiffe", displayName: "SPIFFE" },
  { key: "gcp", displayName: "Google Cloud" },
];

function selectedValue(
  credential: "api-key" | "workload",
  platformKey: string | null,
): string | null {
  if (credential === "api-key") return API_KEY_VALUE;
  if (platformKey === null) return null;
  return PLATFORM_PREFIX + platformKey;
}

/** The short trade-off line under each option. */
function CredentialMeta({ children }: { children: ReactNode }): JSX.Element {
  return (
    <span className="text-muted-foreground mt-1 block font-mono text-xs">
      {children}
    </span>
  );
}

/**
 * A platform's logo in grayscale, so presets read as options in the
 * dashboard's own palette rather than as a row of brand colors.
 */
function PlatformLogo({ icon }: { icon?: string }): JSX.Element {
  if (icon !== undefined && isRelativePath(icon)) {
    return <img src={icon} alt="" className="size-5 shrink-0 grayscale" />;
  }
  return <Waypoints aria-hidden="true" className="text-foreground size-5" />;
}

/**
 * An option's title with a tag beside it. Part of the title, not the card's
 * trailing slot, so a click on the tag still selects the card.
 */
function TaggedTitle({
  title,
  tag,
}: {
  title: string;
  tag: string;
}): JSX.Element {
  return (
    <span className="flex items-center gap-2">
      <span className="truncate">{title}</span>
      <Badge variant="neutral" size="sm">
        <Badge.Text>{tag}</Badge.Text>
      </Badge>
    </span>
  );
}

/** A catalog platform this dashboard ships no working setup for. */
function UnavailablePreset({ entry }: { entry: CatalogEntry }): JSX.Element {
  return (
    <div
      aria-disabled="true"
      className="border-border flex items-start gap-3 border border-dashed p-3 opacity-60"
    >
      <PlatformLogo icon={entry.icon} />
      <div className="min-w-0 flex-1">
        <div className="font-medium">
          <TaggedTitle title={entry.displayName} tag="Unavailable" />
        </div>
        <Text muted small className="block">
          {entry.description}
        </Text>
        <Text muted small className="block">
          Setup is unavailable. Refresh or choose another option.
        </Text>
      </div>
    </div>
  );
}

/** The platforms Speakeasy already knows the issuer for, as one-click options. */
function PlatformPresets({
  entries,
  isPending,
  isError,
  onRetry,
  canFederate,
}: {
  entries: CatalogEntry[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  canFederate: boolean;
}): JSX.Element {
  if (isError) {
    return (
      <div role="alert" className="space-y-2 border p-3">
        <Text small>
          The platform presets could not be loaded. A custom OIDC issuer, under
          Advanced, still works.
        </Text>
        <Button variant="secondary" size="sm" onClick={onRetry}>
          Try again
        </Button>
      </div>
    );
  }
  if (isPending) return <Text muted>Loading platforms…</Text>;
  return (
    <>
      {entries.map((entry) => {
        if (!hasPlatformSetup(entry)) {
          return <UnavailablePreset key={entry.key} entry={entry} />;
        }
        return (
          <RadioCard
            key={entry.key}
            value={PLATFORM_PREFIX + entry.key}
            title={entry.displayName}
            leading={<PlatformLogo icon={entry.icon} />}
            // Trusting a platform and admitting a subject are writes against
            // the organization's workload identities.
            disabled={!canFederate}
            className="p-3"
          >
            <Text muted small>
              {entry.description}
            </Text>
            <CredentialMeta>
              No key minted · the platform's own token
            </CredentialMeta>
          </RadioCard>
        );
      })}
    </>
  );
}

/**
 * How the agent proves who it is, in order of how often each is the right
 * answer: an API key works anywhere, a preset needs no key on a platform
 * Speakeasy already knows, and a custom issuer is for a platform that mints
 * its own tokens but has no preset.
 */
export function CredentialPicker({
  credential,
  platformKey,
  entries,
  isPending,
  isError,
  onRetry,
  canFederate,
  disabled,
  onChooseApiKey,
  onChoosePlatform,
}: {
  credential: "api-key" | "workload";
  platformKey: string | null;
  entries: CatalogEntry[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  canFederate: boolean;
  /** Settled: the agent already signs in with one of these. */
  disabled: boolean;
  onChooseApiKey: () => void;
  onChoosePlatform: (key: string) => void;
}): JSX.Element {
  const customChosen =
    credential === "workload" && platformKey === CUSTOM_PLATFORM_KEY;
  // Starts open when the custom issuer is already chosen, so choosing
  // something else later does not snap the section shut.
  const [advancedOpen, setAdvancedOpen] = useState(customChosen);
  // Held open while the custom issuer is chosen, so the choice is never
  // hidden behind a collapsed section.
  const advancedShown = advancedOpen || customChosen;
  // Only an answered catalog can say which platforms it lacks. While it loads
  // or fails, every planned platform would read as absent.
  const planned =
    isPending || isError
      ? []
      : PLANNED.filter(
          (plan) => !entries.some((entry) => entry.key === plan.key),
        );

  return (
    <RadioCardGroup
      value={selectedValue(credential, platformKey)}
      onValueChange={(next) => {
        if (next === API_KEY_VALUE) {
          onChooseApiKey();
          return;
        }
        onChoosePlatform(next.slice(PLATFORM_PREFIX.length));
      }}
      disabled={disabled}
      aria-label="Credential"
      className="gap-4"
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <RadioCard
          value={API_KEY_VALUE}
          title={<TaggedTitle title="API key" tag="Default" />}
          leading={<KeyRound aria-hidden="true" className="size-5" />}
          className="p-3"
        >
          <Text muted small>
            Speakeasy mints a key for this agent, delivered by a one-line setup
            script or copied by hand. Works in any runtime.
          </Text>
          <CredentialMeta>
            Minted by Speakeasy · long-lived · revocable
          </CredentialMeta>
        </RadioCard>
        <PlatformPresets
          entries={entries}
          isPending={isPending}
          isError={isError}
          onRetry={onRetry}
          canFederate={canFederate}
        />
      </div>
      <Text muted small>
        <span className="text-foreground font-medium">
          Presets need no key.
        </span>{" "}
        The platform already signs a short-lived token for each run, and
        Speakeasy trusts that token instead, so there is nothing to mint, store
        or rotate.
        {planned.length > 0 &&
          ` Coming soon: ${planned.map((plan) => plan.displayName).join(", ")}.`}
        {!canFederate &&
          " Presets and custom issuers need permission to manage workload identities."}
      </Text>
      <Collapsible
        open={advancedShown}
        onOpenChange={setAdvancedOpen}
        disabled={customChosen}
      >
        <CollapsibleTrigger className="text-muted-foreground enabled:hover:text-foreground flex items-center gap-1 text-sm">
          <ChevronRight
            aria-hidden="true"
            className={cn(
              "size-3.5 transition-transform",
              advancedShown && "rotate-90",
            )}
          />
          Advanced: trust another platform's tokens
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-3 sm:w-1/2 sm:pr-1.5">
          <RadioCard
            value={PLATFORM_PREFIX + CUSTOM_PLATFORM_KEY}
            title={<TaggedTitle title="Custom OIDC issuer" tag="Advanced" />}
            leading={<Waypoints aria-hidden="true" className="size-5" />}
            disabled={!canFederate}
            className="p-3"
          >
            <Text muted small>
              Only when your platform already mints signed OIDC tokens for its
              workloads, such as a CI provider, a cluster or your own identity
              service. Reuse an issuer you already trust or register a new one.
              If it mints none, use an API key.
            </Text>
            <CredentialMeta>
              No key minted · reuse or register an issuer
            </CredentialMeta>
          </RadioCard>
        </CollapsibleContent>
      </Collapsible>
    </RadioCardGroup>
  );
}
