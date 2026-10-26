import { GramLogo } from "@/components/gram-logo";
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
import { ChevronRight, Waypoints } from "lucide-react";
import { useState } from "react";
import type { JSX, ReactNode } from "react";
import { CUSTOM_PLATFORM_KEY } from "./CustomSetupInstructions";
import { hasPlatformSetup } from "./platformSetups";
import type { CatalogEntry } from "./workloadCatalog";

const API_KEY_VALUE = "api-key";
const DEVICE_AGENT_VALUE = "device-agent";
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
  credential: "api-key" | "device-agent" | "workload",
  platformKey: string | null,
): string | null {
  if (credential === "api-key") return API_KEY_VALUE;
  if (credential === "device-agent") return DEVICE_AGENT_VALUE;
  if (platformKey === null) return null;
  return PLATFORM_PREFIX + platformKey;
}

/**
 * An option's mark in a hairline square, the tile the MDM rollout table gives
 * each vendor logo. Logos keep their own colors.
 */
function MarkTile({ children }: { children: ReactNode }): JSX.Element {
  return (
    <span className="bg-card flex size-8 shrink-0 items-center justify-center border">
      {children}
    </span>
  );
}

/**
 * The operating systems the device agent installs on, as a triangle of logos
 * inside one tile. The Apple mark is monochrome black, so it alone flips in
 * dark mode.
 */
const DEVICE_AGENT_OS_LOGOS = [
  {
    src: "/icons/platforms/linux.svg",
    invertInDark: false,
    position: "top-1 left-1/2 -translate-x-1/2",
  },
  {
    src: "/icons/platforms/windows.svg",
    invertInDark: false,
    position: "bottom-1 left-1",
  },
  {
    src: "/icons/platforms/macos.svg",
    invertInDark: true,
    position: "right-1 bottom-1",
  },
];

/** One tile like the other options, so the title lines up with the rest. */
function DeviceAgentMark(): JSX.Element {
  return (
    <span className="bg-card relative size-8 shrink-0 border">
      {DEVICE_AGENT_OS_LOGOS.map((logo) => (
        <img
          key={logo.src}
          src={logo.src}
          alt=""
          className={cn(
            "absolute size-2.5 object-contain",
            logo.position,
            logo.invertInDark && "dark:invert",
          )}
        />
      ))}
    </span>
  );
}

/** A catalog platform's logo, or a generic mark when it ships none. */
function PlatformMark({ icon }: { icon?: string }): JSX.Element {
  return (
    <MarkTile>
      {icon !== undefined && isRelativePath(icon) ? (
        <img src={icon} alt="" className="size-5 object-contain" />
      ) : (
        <Waypoints aria-hidden="true" className="size-4" />
      )}
    </MarkTile>
  );
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
      className="border-border flex items-center gap-3 border border-dashed px-4 py-3 opacity-60"
    >
      <PlatformMark icon={entry.icon} />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">
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
            leading={<PlatformMark icon={entry.icon} />}
            leadingAlign="card"
            // Trusting a platform and admitting a subject are writes against
            // the organization's workload identities.
            disabled={!canFederate}
          >
            {entry.description}
          </RadioCard>
        );
      })}
    </>
  );
}

/**
 * How the agent proves who it is: a preset needs no key on a platform
 * Speakeasy already knows, an API key works anywhere, and a custom issuer is for a platform that mints
 * its own tokens but has no preset. The device agent is an API key too, but
 * one scoped to running the device agent rather than reaching MCP servers.
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
  deviceAgent,
  onChooseApiKey,
  onChooseDeviceAgent,
  onChoosePlatform,
}: {
  credential: "api-key" | "device-agent" | "workload";
  platformKey: string | null;
  entries: CatalogEntry[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  canFederate: boolean;
  /** Settled: the agent already signs in with one of these. */
  disabled: boolean;
  /**
   * Whether the device agent is offered, and why it cannot be chosen when it
   * is offered but refused. Undefined hides the option.
   */
  deviceAgent?: { blocked: string | null };
  onChooseApiKey: () => void;
  onChooseDeviceAgent: () => void;
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
        if (next === DEVICE_AGENT_VALUE) {
          onChooseDeviceAgent();
          return;
        }
        onChoosePlatform(next.slice(PLATFORM_PREFIX.length));
      }}
      disabled={disabled}
      aria-label="Target platform"
      size="sm"
      showIndicator={false}
      className="gap-4"
    >
      <div className="grid gap-2">
        <PlatformPresets
          entries={entries}
          isPending={isPending}
          isError={isError}
          onRetry={onRetry}
          canFederate={canFederate}
        />
        <RadioCard
          value={API_KEY_VALUE}
          title="API key"
          leading={
            <MarkTile>
              <GramLogo variant="icon" className="w-4" />
            </MarkTile>
          }
          leadingAlign="card"
        >
          Speakeasy mints a key you install with one command or paste by hand.
          Works in any runtime.
        </RadioCard>
        {deviceAgent !== undefined && (
          <RadioCard
            value={DEVICE_AGENT_VALUE}
            title="Device agent"
            leading={<DeviceAgentMark />}
            leadingAlign="card"
            disabled={deviceAgent.blocked !== null}
          >
            {deviceAgent.blocked ??
              "Runs on a Windows, macOS or Linux machine no person uses. Syncs plugins and sends hook events; reaches no MCP servers."}
          </RadioCard>
        )}
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
        <CollapsibleContent className="mt-3">
          <RadioCard
            value={PLATFORM_PREFIX + CUSTOM_PLATFORM_KEY}
            title="Custom OIDC issuer"
            leading={<PlatformMark />}
            leadingAlign="card"
            disabled={!canFederate}
          >
            For a platform that already mints signed OIDC tokens, such as a CI
            provider or a cluster. If yours mints none, use an API key.
          </RadioCard>
        </CollapsibleContent>
      </Collapsible>
    </RadioCardGroup>
  );
}
