import type { OnboardingAnswers } from "@gram/client/models/components/onboardinganswers.js";
import type { OnboardingProvider } from "@gram/client/models/components/onboardingprovider.js";
import type { OnboardingState } from "@gram/client/models/components/onboardingstate.js";
import type {
  SaveStackRequestBody,
  SaveStackRequestBodyMdmVendor,
} from "@gram/client/models/components/savestackrequestbody.js";

/**
 * The wizard's screens, in order. The first three make up stage one,
 * the organization's stack; the use case is picked between the stages and
 * decides what stage two shows; the steps screen is stage two.
 */
export type WizardScreen =
  | "providers"
  | "products"
  | "mdm"
  | "use-case"
  | "steps";

export interface WizardScreenSpec {
  id: WizardScreen;
  title: string;
  description: string;
}

/** Stage one, in order: what the organization runs. */
export const STACK_SCREENS: WizardScreenSpec[] = [
  {
    id: "providers",
    title: "Providers and plans",
    description:
      "Which AI vendors your organization uses, and the plan you are on with each.",
  },
  {
    id: "products",
    title: "Products",
    description: "Which of their products your people actually use.",
  },
  {
    id: "mdm",
    title: "Device management",
    description: "Whether an MDM can push configuration for you.",
  },
];

/** Between the stages: decides what stage two shows. */
export const USE_CASE_SCREEN: WizardScreenSpec = {
  id: "use-case",
  title: "Use case",
  description: "The one outcome to reach first.",
};

/** Stage two: the use case's own steps, verified against real traffic. */
export const STEPS_SCREEN: WizardScreenSpec = {
  id: "steps",
  title: "Next step",
  description: "One action at a time, verified against real traffic.",
};

/**
 * What the sidebar lists while stage one is active: the stack screens, then
 * the use case. The use case counts toward neither bar; it sits here so the
 * reader can reach it once the stack is saved.
 */
export const STAGE_ONE_SCREENS: WizardScreenSpec[] = [
  ...STACK_SCREENS,
  USE_CASE_SCREEN,
];

/** Every screen, in order. */
export const WIZARD_SCREENS: WizardScreenSpec[] = [
  ...STAGE_ONE_SCREENS,
  STEPS_SCREEN,
];

export function screenSpec(id: WizardScreen): WizardScreenSpec {
  return (
    WIZARD_SCREENS.find((screen) => screen.id === id) ?? WIZARD_SCREENS[0]!
  );
}

/**
 * Bar one's fill, from 0 to 1, by where the reader is: the share of the
 * stack screens moved past, and full from the use case screen onward. It
 * follows position, not what is saved, so stepping back into the stack
 * drops it again.
 */
export function stackProgress(screen: WizardScreen): number {
  const index = STACK_SCREENS.findIndex((spec) => spec.id === screen);
  return index < 0 ? 1 : index / STACK_SCREENS.length;
}

/**
 * Bar two's fill, from 0 to 1: empty until the reader is on the steps
 * screen, then the share of the use case's steps that are verified, and
 * full once the use case is covered.
 */
export function stepsProgress(
  screen: WizardScreen,
  state: OnboardingState | undefined,
): number {
  if (screen !== "steps" || !state) return 0;
  if (state.done) return 1;
  if (state.steps.length === 0) return 0;
  const verified = state.steps.filter((step) => step.verifiedAt).length;
  return verified / state.steps.length;
}

/** What the admin has answered about their stack, before it is saved. */
export interface Draft {
  /** Selected provider slug → declared plan slug, or null while unpicked. */
  providers: Record<string, string | null>;
  /** Selected product slugs, each of a selected provider. */
  products: string[];
  mdmVendor: string | null;
}

export const EMPTY_DRAFT: Draft = {
  providers: {},
  products: [],
  mdmVendor: null,
};

export function draftFromAnswers(
  answers: OnboardingAnswers | undefined,
): Draft {
  if (!answers) return EMPTY_DRAFT;
  const providers: Record<string, string | null> = {};
  for (const provider of answers.providers) {
    providers[provider.providerSlug] = provider.planSlug ?? null;
  }
  return {
    providers,
    products: [...answers.productSlugs],
    mdmVendor: answers.mdmVendor,
  };
}

/** The selected providers, in reference order. */
export function selectedProviders(
  draft: Draft,
  providers: OnboardingProvider[],
): OnboardingProvider[] {
  return providers.filter((provider) => provider.slug in draft.providers);
}

/**
 * Why the providers screen cannot continue yet, or null when it can. At
 * least one provider is needed, and every provider that sells plans needs
 * the one the organization is on.
 */
export function providersProblem(
  draft: Draft,
  providers: OnboardingProvider[],
): string | null {
  const selected = selectedProviders(draft, providers);
  if (Object.keys(draft.providers).length === 0) {
    return "Pick at least one provider.";
  }
  for (const provider of selected) {
    if (provider.plans.length > 0 && !draft.providers[provider.slug]) {
      return `Pick the plan you have with ${provider.name}.`;
    }
  }
  return null;
}

/**
 * Why the products screen cannot continue yet, or null when it can. At least
 * one product is needed, and each must belong to a selected provider.
 */
export function productsProblem(
  draft: Draft,
  providers: OnboardingProvider[],
): string | null {
  if (draft.products.length === 0) return "Pick at least one product.";
  const allowed = new Set(
    selectedProviders(draft, providers).flatMap((provider) =>
      provider.products.map((product) => product.slug),
    ),
  );
  for (const slug of draft.products) {
    if (!allowed.has(slug)) {
      return "Every product must belong to a provider you picked.";
    }
  }
  return null;
}

/** Why the stack cannot be saved yet, or null when it can. */
export function draftProblem(
  draft: Draft,
  providers: OnboardingProvider[],
): string | null {
  return (
    providersProblem(draft, providers) ??
    productsProblem(draft, providers) ??
    (draft.mdmVendor ? null : "Say whether you use an MDM.")
  );
}

export function toSaveStackRequest(draft: Draft): SaveStackRequestBody {
  if (!draft.mdmVendor) {
    throw new Error("draft is incomplete");
  }
  return {
    providers: Object.entries(draft.providers).map(
      ([providerSlug, planSlug]) => ({
        providerSlug,
        planSlug: planSlug ?? undefined,
      }),
    ),
    productSlugs: [...draft.products],
    // The choice comes from the reference data, so it is already valid; the
    // server rejects anything else.
    mdmVendor: draft.mdmVendor as SaveStackRequestBodyMdmVendor,
  };
}

/** Where the wizard opens, from the server's stage. */
export function resumeScreen(state: OnboardingState | undefined): WizardScreen {
  switch (state?.stage) {
    case "use-case":
      return "use-case";
    case "steps":
    case "done":
      return "steps";
    case "stack":
    case undefined:
      return "providers";
  }
}

/**
 * Adds or removes a provider. Removing one also forgets its plan and drops
 * its products from the selection.
 */
export function toggleProvider(
  draft: Draft,
  provider: OnboardingProvider,
): Draft {
  const providers = { ...draft.providers };
  if (provider.slug in providers) {
    delete providers[provider.slug];
    const gone = new Set(provider.products.map((product) => product.slug));
    return {
      ...draft,
      providers,
      products: draft.products.filter((slug) => !gone.has(slug)),
    };
  }
  providers[provider.slug] = null;
  return { ...draft, providers };
}

export function setProviderPlan(
  draft: Draft,
  providerSlug: string,
  planSlug: string,
): Draft {
  return {
    ...draft,
    providers: { ...draft.providers, [providerSlug]: planSlug },
  };
}

export function toggleProduct(draft: Draft, slug: string): Draft {
  if (draft.products.includes(slug)) {
    return { ...draft, products: draft.products.filter((s) => s !== slug) };
  }
  return { ...draft, products: [...draft.products, slug] };
}

/**
 * The first sentence of a step's description, for the rail; the panel shows
 * the whole thing.
 */
export function firstSentence(text: string): string {
  const end = text.search(/[.!?](\s|$)/);
  return end < 0 ? text : text.slice(0, end + 1);
}

/** The icon source the provider icon set knows for a provider. */
export function providerIconSource(providerSlug: string): string {
  switch (providerSlug) {
    case "anthropic":
      return "claude";
    case "openai":
      return "codex";
    case "github":
      return "copilot";
    default:
      return providerSlug;
  }
}
