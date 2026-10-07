import type { AdminCustomerUsage } from "@gram/admin-client/models/components/admincustomerusage";

import {
  formatSpendUsd,
  spendCostIsZero,
  sumSpendCosts,
  type SpendProductID,
} from "@/pages/organization/spendBreakdownUtils";
import {
  CUSTOMER_PLANS,
  type CustomerPlan,
  type CustomerUsageControls,
  type CustomerUsageSort,
} from "./customerUsageSearch";

export type CustomerUsage = AdminCustomerUsage;

// "All plans" when no plan or every plan is selected, otherwise the selected
// plans' names.
export function planFilterLabel(selected: ReadonlySet<CustomerPlan>): string {
  const plans = CUSTOMER_PLANS.filter((plan) => selected.has(plan));
  if (plans.length === 0 || plans.length === CUSTOMER_PLANS.length) {
    return "All plans";
  }
  return plans.map((plan) => PLAN_LABELS[plan]).join(", ");
}

export const PLAN_LABELS: Record<CustomerPlan, string> = {
  enterprise: "Enterprise",
  pro: "Pro",
  payg: "PAYG",
};

// The order the "Plan type" sort walks.
const PLAN_ORDER: Record<string, number> = { enterprise: 0, pro: 1, payg: 2 };

// A signed dollar change. A sub-cent decrease keeps formatSpendUsd's ">-$0.01"
// marker; other decreases use a minus sign.
export function formatSignedUsd(value: string): string {
  const formatted = formatSpendUsd(value);
  if (value.trim().startsWith("-")) {
    return formatted.startsWith("-") ? `−${formatted.slice(1)}` : formatted;
  }
  return spendCostIsZero(value) ? formatted : `+${formatted}`;
}

// A whole-number percent change. A real change that rounds to zero reads as
// "<0.5%" with its direction, so it is never labeled the same as no change.
export function formatChangePercent(percent: number): string {
  if (percent !== 0 && Math.abs(percent) < 0.5) {
    return percent > 0 ? "+<0.5%" : "−<0.5%";
  }
  const rounded = Math.round(percent);
  if (rounded > 0) return `+${rounded}%`;
  if (rounded < 0) return `−${Math.abs(rounded)}%`;
  return "0%";
}

// The pin button's element id, so the page can return focus to it after the
// card moves between the Pinned and Other customers grids.
export function pinButtonID(organizationID: string): string {
  return `customer-usage-pin-${organizationID}`;
}

export type CustomerChange =
  // No previous cycle to compare with: the organization is newer than that.
  | { kind: "none" }
  // Nothing was spent in the comparison period, and something has been now.
  | { kind: "new"; changeUsd: string }
  | { kind: "change"; changeUsd: string; percent: number };

export type CustomerUsageSummary = {
  customer: CustomerUsage;
  // Estimated spend this cycle to date, over the selected products.
  cycleCostUsd: string;
  change: CustomerChange;
};

function negate(value: string): string {
  return value.startsWith("-") ? value.slice(1) : `-${value}`;
}

function sumOrZero(values: readonly string[]): string {
  return values.length === 0 ? "0" : sumSpendCosts(values);
}

export function summarizeCustomer(
  customer: CustomerUsage,
  selected: ReadonlySet<SpendProductID>,
): CustomerUsageSummary {
  const cycleCostUsd = sumOrZero(
    customer.products
      .filter((product) => selected.has(product.id))
      .map((product) => product.costUsd),
  );
  if (!customer.previousPeriod) {
    return { customer, cycleCostUsd, change: { kind: "none" } };
  }

  const previousCostUsd = sumOrZero(
    customer.previousPeriodCosts
      .filter((cost) => selected.has(cost.productId))
      .map((cost) => cost.costUsd),
  );
  const changeUsd = sumSpendCosts([cycleCostUsd, negate(previousCostUsd)]);
  if (spendCostIsZero(previousCostUsd)) {
    return spendCostIsZero(cycleCostUsd)
      ? {
          customer,
          cycleCostUsd,
          change: { kind: "change", changeUsd, percent: 0 },
        }
      : { customer, cycleCostUsd, change: { kind: "new", changeUsd } };
  }
  return {
    customer,
    cycleCostUsd,
    change: {
      kind: "change",
      changeUsd,
      percent: (Number(changeUsd) / Number(previousCostUsd)) * 100,
    },
  };
}

// A customer with no usage this cycle belongs in the "No usage this cycle"
// section. A customer whose usage could not be read stays in the main grid,
// where its error is visible.
export function hasNoUsage(summary: CustomerUsageSummary): boolean {
  return (
    summary.customer.error === undefined &&
    spendCostIsZero(summary.cycleCostUsd)
  );
}

function changeMagnitude(change: CustomerChange): number | undefined {
  return change.kind === "none"
    ? undefined
    : Math.abs(Number(change.changeUsd));
}

function byName(
  left: CustomerUsageSummary,
  right: CustomerUsageSummary,
): number {
  return left.customer.name.localeCompare(right.customer.name, "en", {
    sensitivity: "base",
  });
}

function bySpend(
  left: CustomerUsageSummary,
  right: CustomerUsageSummary,
): number {
  return Number(right.cycleCostUsd) - Number(left.cycleCostUsd);
}

export function compareCustomers(
  sort: CustomerUsageSort,
): (left: CustomerUsageSummary, right: CustomerUsageSummary) => number {
  switch (sort) {
    case "spend":
      return (left, right) => bySpend(left, right) || byName(left, right);
    case "change":
      // Absolute dollar change, either direction. Customers with nothing to
      // compare against go last rather than reading as "no change".
      return (left, right) => {
        const leftChange = changeMagnitude(left.change);
        const rightChange = changeMagnitude(right.change);
        if (leftChange === undefined || rightChange === undefined) {
          if (leftChange !== rightChange) {
            return leftChange === undefined ? 1 : -1;
          }
          return bySpend(left, right) || byName(left, right);
        }
        return (
          rightChange - leftChange ||
          bySpend(left, right) ||
          byName(left, right)
        );
      };
    case "name":
      return byName;
    case "plan":
      return (left, right) =>
        (PLAN_ORDER[left.customer.accountType] ?? 99) -
          (PLAN_ORDER[right.customer.accountType] ?? 99) ||
        bySpend(left, right) ||
        byName(left, right);
  }
}

export type CustomerUsageView = {
  // Pinned customers, first and whatever their usage, so a pinned customer
  // with $0 this cycle still sits at the top.
  pinned: CustomerUsageSummary[];
  // Customers with usage this cycle, or whose usage could not be read.
  active: CustomerUsageSummary[];
  // Customers with $0 this cycle.
  idle: CustomerUsageSummary[];
};

export function customerUsageView(
  customers: readonly CustomerUsage[],
  controls: Pick<CustomerUsageControls, "q" | "plans" | "sort" | "products">,
  pinnedIDs: ReadonlySet<string> = new Set(),
): CustomerUsageView {
  const term = controls.q.trim().toLocaleLowerCase("en");
  const summaries = customers
    .filter(
      (customer) =>
        (controls.plans.size === 0 ||
          controls.plans.has(customer.accountType as CustomerPlan)) &&
        (term === "" || customer.name.toLocaleLowerCase("en").includes(term)),
    )
    .map((customer) => summarizeCustomer(customer, controls.products))
    .sort(compareCustomers(controls.sort));

  const view: CustomerUsageView = { pinned: [], active: [], idle: [] };
  for (const summary of summaries) {
    if (pinnedIDs.has(summary.customer.organizationId)) {
      view.pinned.push(summary);
    } else {
      (hasNoUsage(summary) ? view.idle : view.active).push(summary);
    }
  }
  return view;
}
