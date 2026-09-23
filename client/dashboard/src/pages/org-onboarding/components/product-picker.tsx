import type { OnboardingProduct } from "@gram/client/models/components/onboardingproduct.js";
import type { OnboardingProvider } from "@gram/client/models/components/onboardingprovider.js";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Checkbox } from "@/components/ui/Checkbox";
import { cn } from "@/lib/utils";
import {
  providerIconSource,
  selectedProviders,
  toggleProduct,
  type Draft,
} from "../wizard-state";

/**
 * The products question: the products of the providers the admin picked,
 * grouped by provider. Each inherits its provider's plan, so there is
 * nothing to ask per product.
 */
export function ProductPicker({
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
  const groups = selectedProviders(draft, providers);
  if (groups.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        Pick at least one provider first.
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-6">
      {groups.map((provider) => {
        const plan = provider.plans.find(
          (candidate) => candidate.slug === draft.providers[provider.slug],
        );
        return (
          <section key={provider.slug} className="flex flex-col gap-2">
            <h3 className="text-eyebrow">
              {provider.name}
              {plan ? ` · ${plan.name}` : ""}
            </h3>
            <ul className="flex flex-col gap-2">
              {provider.products.map((product) => (
                <ProductRow
                  key={product.slug}
                  product={product}
                  providerSlug={provider.slug}
                  selected={draft.products.includes(product.slug)}
                  disabled={disabled}
                  onToggle={() => onChange(toggleProduct(draft, product.slug))}
                />
              ))}
            </ul>
          </section>
        );
      })}
    </div>
  );
}

function ProductRow({
  product,
  providerSlug,
  selected,
  disabled,
  onToggle,
}: {
  product: OnboardingProduct;
  providerSlug: string;
  selected: boolean;
  disabled: boolean;
  onToggle: () => void;
}): JSX.Element {
  const checkboxId = `product-${product.slug}`;
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
        aria-label={product.name}
      />
      <div className="bg-secondary flex h-9 w-9 flex-shrink-0 items-center justify-center">
        <AgentProviderIcon
          source={product.sourceIds[0] ?? providerIconSource(providerSlug)}
          className="h-5 w-5"
        />
      </div>
      <label
        htmlFor={checkboxId}
        className="text-foreground min-w-0 flex-1 cursor-pointer text-sm font-medium"
      >
        {product.name}
      </label>
    </li>
  );
}
