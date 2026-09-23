import type { OnboardingProduct } from "@gram/client/models/components/onboardingproduct.js";
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
  groupByVendor,
  productIconSource,
  setProductPlan,
  toggleProduct,
  type Draft,
} from "../wizard-state";

/**
 * The products question: every product the platform has ingest code for,
 * grouped by vendor, each with its vendor's plans once selected.
 */
export function ProductPicker({
  products,
  draft,
  onChange,
  disabled = false,
}: {
  products: OnboardingProduct[];
  draft: Draft;
  onChange: (draft: Draft) => void;
  disabled?: boolean;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-6">
      {groupByVendor(products).map((group) => (
        <section key={group.vendor} className="flex flex-col gap-2">
          <h3 className="text-eyebrow">{group.label}</h3>
          <ul className="flex flex-col gap-2">
            {group.products.map((product) => (
              <ProductRow
                key={product.slug}
                product={product}
                selected={product.slug in draft.products}
                plan={draft.products[product.slug] ?? null}
                disabled={disabled}
                onToggle={() => onChange(toggleProduct(draft, product.slug))}
                onPlan={(planSlug) =>
                  onChange(setProductPlan(draft, product.slug, planSlug))
                }
              />
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function ProductRow({
  product,
  selected,
  plan,
  disabled,
  onToggle,
  onPlan,
}: {
  product: OnboardingProduct;
  selected: boolean;
  plan: string | null;
  disabled: boolean;
  onToggle: () => void;
  onPlan: (planSlug: string) => void;
}): JSX.Element {
  const checkboxId = `product-${product.slug}`;
  const needsPlan = selected && product.plans.length > 0 && !plan;

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
          source={productIconSource(product)}
          className="h-5 w-5"
        />
      </div>
      <label
        htmlFor={checkboxId}
        className="text-foreground min-w-0 flex-1 cursor-pointer text-sm font-medium"
      >
        {product.name}
      </label>
      {selected && product.plans.length > 0 ? (
        <Select value={plan ?? ""} onValueChange={onPlan} disabled={disabled}>
          <SelectTrigger
            className={cn("w-44", needsPlan && "border-warning-default")}
            aria-label={`${product.name} plan`}
          >
            <SelectValue placeholder="Which plan?" />
          </SelectTrigger>
          <SelectContent>
            {product.plans.map((candidate) => (
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
