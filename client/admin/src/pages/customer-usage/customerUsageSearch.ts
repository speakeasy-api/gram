import { z } from "zod";

import type { SpendProductID } from "@/pages/organization/spendBreakdownUtils";

export const ALL_PRODUCT_IDS: readonly SpendProductID[] = [
  "agent_session_storage",
  "risk_content_scans",
  "mcp_egress",
];

export const CUSTOMER_USAGE_SORTS = [
  "spend",
  "change",
  "name",
  "plan",
] as const;
export type CustomerUsageSort = (typeof CUSTOMER_USAGE_SORTS)[number];

export const CUSTOMER_PLANS = ["enterprise", "pro", "payg"] as const;
export type CustomerPlan = (typeof CUSTOMER_PLANS)[number];

export type CustomerUsageInterval = "daily" | "weekly" | "monthly";

// Every control on the page lives in the URL so a view can be bookmarked or
// pasted to a colleague. Defaults are left out of the URL: an absent value is
// the default, which keeps the plain page address the default view.
const searchSchema = z.object({
  q: z.string().optional().catch(undefined),
  // The selected plans. Absent means every plan.
  plans: z.array(z.enum(CUSTOMER_PLANS)).optional().catch(undefined),
  sort: z.enum(CUSTOMER_USAGE_SORTS).optional().catch(undefined),
  // The selected products. Absent means all of them; an empty list means none.
  products: z
    .array(z.enum(ALL_PRODUCT_IDS as [SpendProductID, ...SpendProductID[]]))
    .optional()
    .catch(undefined),
  interval: z.enum(["daily", "weekly", "monthly"]).optional().catch(undefined),
  // Cumulative is on by default, so only `false` is ever stored.
  cumulative: z.boolean().optional().catch(undefined),
});

export type CustomerUsageSearch = z.infer<typeof searchSchema>;

export function customerUsageSearch(
  search: Record<string, unknown>,
): CustomerUsageSearch {
  return searchSchema.parse(search);
}

export type CustomerUsageControls = {
  q: string;
  // Empty means every plan.
  plans: ReadonlySet<CustomerPlan>;
  sort: CustomerUsageSort;
  products: ReadonlySet<SpendProductID>;
  interval: CustomerUsageInterval;
  cumulative: boolean;
};

export function customerUsageControls(
  search: CustomerUsageSearch,
): CustomerUsageControls {
  return {
    q: search.q ?? "",
    plans: new Set(search.plans ?? []),
    sort: search.sort ?? "spend",
    products: new Set(search.products ?? ALL_PRODUCT_IDS),
    interval: search.interval ?? "monthly",
    cumulative: search.cumulative ?? true,
  };
}

// The URL form of a product selection: absent when every product is selected,
// otherwise the selected products in display order.
export function productsSearchValue(
  selected: ReadonlySet<SpendProductID>,
): SpendProductID[] | undefined {
  const products = ALL_PRODUCT_IDS.filter((id) => selected.has(id));
  return products.length === ALL_PRODUCT_IDS.length ? undefined : products;
}

// The URL form of a plan selection: absent when no plan or every plan is
// selected, since both mean "show every plan", otherwise the selected plans in
// display order.
export function plansSearchValue(
  selected: ReadonlySet<CustomerPlan>,
): CustomerPlan[] | undefined {
  const plans = CUSTOMER_PLANS.filter((plan) => selected.has(plan));
  return plans.length === 0 || plans.length === CUSTOMER_PLANS.length
    ? undefined
    : plans;
}
