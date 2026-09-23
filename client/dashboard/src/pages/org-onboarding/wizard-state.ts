import type { OnboardingAnswers } from "@gram/client/models/components/onboardinganswers.js";
import type { OnboardingProduct } from "@gram/client/models/components/onboardingproduct.js";
import type { OnboardingState } from "@gram/client/models/components/onboardingstate.js";
import type {
  SaveAnswersRequestBody,
  SaveAnswersRequestBodyMdmVendor,
  SaveAnswersRequestBodyUseCase,
} from "@gram/client/models/components/saveanswersrequestbody.js";

export type WizardStepId = "products" | "mdm" | "use-case" | "next-step";

export interface WizardStepSpec {
  id: WizardStepId;
  title: string;
  description: string;
}

/** The wizard's four screens, in order. The first three are questions. */
export const WIZARD_STEPS: WizardStepSpec[] = [
  {
    id: "products",
    title: "Products",
    description: "Which AI products your people use, and on which plan.",
  },
  {
    id: "mdm",
    title: "Device management",
    description: "Whether an MDM can push configuration for you.",
  },
  {
    id: "use-case",
    title: "Use case",
    description: "The one outcome to reach first.",
  },
  {
    id: "next-step",
    title: "Next step",
    description: "One action, verified against real traffic.",
  },
];

export function stepIndex(id: WizardStepId): number {
  return WIZARD_STEPS.findIndex((step) => step.id === id);
}

/** What the admin has answered so far, before it is saved. */
export interface Draft {
  /** Selected product slug → declared plan slug, or null while unpicked. */
  products: Record<string, string | null>;
  mdmVendor: string | null;
  useCase: string | null;
}

export const EMPTY_DRAFT: Draft = {
  products: {},
  mdmVendor: null,
  useCase: null,
};

export function draftFromAnswers(
  answers: OnboardingAnswers | undefined,
): Draft {
  if (!answers) return EMPTY_DRAFT;
  const products: Record<string, string | null> = {};
  for (const product of answers.products) {
    products[product.productSlug] = product.planSlug ?? null;
  }
  return { products, mdmVendor: answers.mdmVendor, useCase: answers.useCase };
}

/**
 * Why the products step cannot continue yet, or null when it can. Every
 * selected product that has plans needs one; at least one product is needed.
 */
export function productsProblem(
  draft: Draft,
  products: OnboardingProduct[],
): string | null {
  const selected = Object.keys(draft.products);
  if (selected.length === 0) return "Pick at least one product.";
  for (const slug of selected) {
    const product = products.find((candidate) => candidate.slug === slug);
    if (!product) continue;
    if (product.plans.length > 0 && !draft.products[slug]) {
      return `Pick the plan you have for ${product.name}.`;
    }
  }
  return null;
}

/** Why the whole draft cannot be saved yet, or null when it can. */
export function draftProblem(
  draft: Draft,
  products: OnboardingProduct[],
): string | null {
  const problem = productsProblem(draft, products);
  if (problem) return problem;
  if (!draft.mdmVendor) return "Say whether you use an MDM.";
  if (!draft.useCase) return "Pick the use case to reach first.";
  return null;
}

export function toSaveRequest(draft: Draft): SaveAnswersRequestBody {
  if (!draft.mdmVendor || !draft.useCase) {
    throw new Error("draft is incomplete");
  }
  return {
    products: Object.entries(draft.products).map(([productSlug, planSlug]) => ({
      productSlug,
      planSlug: planSlug ?? undefined,
    })),
    // The choices come from the reference data, so they are already valid;
    // the server rejects anything else.
    mdmVendor: draft.mdmVendor as SaveAnswersRequestBodyMdmVendor,
    useCase: draft.useCase as SaveAnswersRequestBodyUseCase,
  };
}

/** Where the wizard opens: the questions until they are saved, then the step. */
export function resumeStep(state: OnboardingState | undefined): WizardStepId {
  return state?.answers ? "next-step" : "products";
}

export function toggleProduct(draft: Draft, slug: string): Draft {
  const products = { ...draft.products };
  if (slug in products) {
    delete products[slug];
  } else {
    products[slug] = null;
  }
  return { ...draft, products };
}

export function setProductPlan(
  draft: Draft,
  slug: string,
  planSlug: string,
): Draft {
  return { ...draft, products: { ...draft.products, [slug]: planSlug } };
}

/** Products grouped by vendor, keeping the reference order within a group. */
export function groupByVendor(
  products: OnboardingProduct[],
): { vendor: string; label: string; products: OnboardingProduct[] }[] {
  const groups = new Map<string, OnboardingProduct[]>();
  for (const product of products) {
    const group = groups.get(product.vendor) ?? [];
    group.push(product);
    groups.set(product.vendor, group);
  }
  return Array.from(groups, ([vendor, members]) => ({
    vendor,
    label: vendorLabel(vendor),
    products: members,
  }));
}

export function vendorLabel(vendor: string): string {
  switch (vendor) {
    case "anthropic":
      return "Anthropic";
    case "openai":
      return "OpenAI";
    case "cursor":
      return "Cursor";
    case "github":
      return "GitHub";
    case "litellm":
      return "LiteLLM";
    default:
      return "Open source";
  }
}

/** The icon source the provider icon set knows for a product. */
export function productIconSource(product: OnboardingProduct): string {
  switch (product.vendor) {
    case "anthropic":
      return "claude";
    case "openai":
      return "codex";
    default:
      return product.sourceIds[0] ?? product.slug;
  }
}
