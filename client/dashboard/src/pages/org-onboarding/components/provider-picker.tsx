import type { OnboardingProvider } from "@gram/client/models/components/onboardingprovider.js";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Checkbox } from "@/components/ui/Checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { cn } from "@/lib/utils";
import {
  providerIconSource,
  setProviderPlan,
  toggleProvider,
  type Draft,
} from "../wizard-state";

/**
 * The providers question: every vendor the platform has ingest code for.
 * A selected provider that sells plans asks which one the organization is
 * on; that plan applies to every product of the provider.
 */
export function ProviderPicker({
  providers,
  draft,
  onChange,
  disabled = false,
}: {
  providers: OnboardingProvider[];
  draft: Draft;
  onChange: (draft: Draft) => void;
  disabled?: boolean;
}): JSX.Element {
  return (
    <ul className="flex flex-col gap-2">
      {providers.map((provider) => (
        <ProviderRow
          key={provider.slug}
          provider={provider}
          selected={provider.slug in draft.providers}
          plan={draft.providers[provider.slug] ?? null}
          disabled={disabled}
          onToggle={() => onChange(toggleProvider(draft, provider))}
          onPlan={(planSlug) =>
            onChange(setProviderPlan(draft, provider.slug, planSlug))
          }
        />
      ))}
    </ul>
  );
}

function ProviderRow({
  provider,
  selected,
  plan,
  disabled,
  onToggle,
  onPlan,
}: {
  provider: OnboardingProvider;
  selected: boolean;
  plan: string | null;
  disabled: boolean;
  onToggle: () => void;
  onPlan: (planSlug: string) => void;
}): JSX.Element {
  const checkboxId = `provider-${provider.slug}`;
  const needsPlan = selected && provider.plans.length > 0 && !plan;
  const productNames = provider.products.map((product) => product.name);

  return (
    <li
      className={cn(
        "bg-card flex items-center gap-4 border p-4",
        selected ? "border-foreground/40" : "border-border",
      )}
    >
      <Checkbox
        id={checkboxId}
        checked={selected}
        disabled={disabled}
        onCheckedChange={onToggle}
        aria-label={provider.name}
      />
      <div className="bg-secondary flex h-9 w-9 flex-shrink-0 items-center justify-center">
        <AgentProviderIcon
          source={providerIconSource(provider.slug)}
          className="h-5 w-5"
        />
      </div>
      <label htmlFor={checkboxId} className="min-w-0 flex-1 cursor-pointer">
        <span className="text-foreground block text-sm font-medium">
          {provider.name}
        </span>
        <span className="text-muted-foreground block truncate text-xs">
          {productNames.join(" · ")}
        </span>
      </label>
      {selected && provider.plans.length > 0 ? (
        <Select value={plan ?? ""} onValueChange={onPlan} disabled={disabled}>
          <SelectTrigger
            className={cn("w-48", needsPlan && "border-warning-default")}
            aria-label={`${provider.name} plan`}
          >
            <SelectValue placeholder="Which plan?" />
          </SelectTrigger>
          <SelectContent>
            {provider.plans.map((candidate) => (
              <SelectItem key={candidate.slug} value={candidate.slug}>
                {candidate.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}
    </li>
  );
}
