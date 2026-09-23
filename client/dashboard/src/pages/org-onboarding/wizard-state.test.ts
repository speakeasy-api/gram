import { describe, expect, it } from "vitest";
import type { OnboardingAnswers } from "@gram/client/models/components/onboardinganswers.js";
import type { OnboardingProvider } from "@gram/client/models/components/onboardingprovider.js";
import {
  draftFromAnswers,
  draftProblem,
  EMPTY_DRAFT,
  firstSentence,
  productsProblem,
  providersProblem,
  resumeScreen,
  selectedProviders,
  setProviderPlan,
  stackProgress,
  STAGE_ONE_SCREENS,
  stepsProgress,
  toggleProduct,
  toggleProvider,
  toSaveStackRequest,
  WIZARD_SCREENS,
} from "./wizard-state";

const anthropic: OnboardingProvider = {
  slug: "anthropic",
  name: "Anthropic",
  plans: [
    { slug: "anthropic-team", name: "Team" },
    { slug: "anthropic-enterprise", name: "Enterprise" },
  ],
  products: [
    {
      slug: "claude-code-cli",
      name: "Claude Code (CLI)",
      sourceIds: ["claude-code"],
    },
    { slug: "cowork", name: "Cowork", sourceIds: ["cowork"] },
  ],
};

const opencode: OnboardingProvider = {
  slug: "opencode",
  name: "opencode",
  plans: [],
  products: [{ slug: "opencode", name: "opencode", sourceIds: ["opencode"] }],
};

const providers = [anthropic, opencode];

describe("providersProblem", () => {
  it("needs at least one provider", () => {
    expect(providersProblem(EMPTY_DRAFT, providers)).toBe(
      "Pick at least one provider.",
    );
  });

  it("needs a plan for providers that sell plans", () => {
    const draft = toggleProvider(EMPTY_DRAFT, anthropic);
    expect(providersProblem(draft, providers)).toBe(
      "Pick the plan you have with Anthropic.",
    );
    expect(
      providersProblem(
        setProviderPlan(draft, "anthropic", "anthropic-team"),
        providers,
      ),
    ).toBeNull();
  });

  it("accepts planless providers as they are", () => {
    expect(
      providersProblem(toggleProvider(EMPTY_DRAFT, opencode), providers),
    ).toBeNull();
  });
});

describe("productsProblem", () => {
  it("needs at least one product of a selected provider", () => {
    let draft = setProviderPlan(
      toggleProvider(EMPTY_DRAFT, anthropic),
      "anthropic",
      "anthropic-team",
    );
    expect(productsProblem(draft, providers)).toBe(
      "Pick at least one product.",
    );
    draft = toggleProduct(draft, "opencode");
    expect(productsProblem(draft, providers)).toBe(
      "Every product must belong to a provider you picked.",
    );
    draft = toggleProduct(toggleProduct(draft, "opencode"), "cowork");
    expect(productsProblem(draft, providers)).toBeNull();
  });
});

describe("draftProblem", () => {
  it("walks the stack questions in order", () => {
    let draft = setProviderPlan(
      toggleProvider(EMPTY_DRAFT, anthropic),
      "anthropic",
      "anthropic-enterprise",
    );
    expect(draftProblem(draft, providers)).toBe("Pick at least one product.");
    draft = toggleProduct(draft, "claude-code-cli");
    expect(draftProblem(draft, providers)).toBe("Say whether you use an MDM.");
    draft = { ...draft, mdmVendor: "none" };
    expect(draftProblem(draft, providers)).toBeNull();
  });
});

describe("toggleProvider", () => {
  it("drops a provider's plan and products when it is removed", () => {
    let draft = setProviderPlan(
      toggleProvider(EMPTY_DRAFT, anthropic),
      "anthropic",
      "anthropic-team",
    );
    draft = toggleProduct(toggleProvider(draft, opencode), "cowork");
    draft = toggleProduct(draft, "opencode");
    expect(draft.products).toEqual(["cowork", "opencode"]);

    draft = toggleProvider(draft, anthropic);
    expect(draft.providers).toEqual({ opencode: null });
    expect(draft.products).toEqual(["opencode"]);
  });
});

describe("selectedProviders", () => {
  it("keeps reference order", () => {
    const draft = toggleProvider(
      toggleProvider(EMPTY_DRAFT, opencode),
      anthropic,
    );
    expect(selectedProviders(draft, providers).map((p) => p.slug)).toEqual([
      "anthropic",
      "opencode",
    ]);
  });
});

describe("toSaveStackRequest", () => {
  it("omits the plan for planless providers", () => {
    const draft = {
      providers: { anthropic: "anthropic-team", opencode: null },
      products: ["claude-code-cli", "opencode"],
      mdmVendor: "jamf",
    };
    expect(toSaveStackRequest(draft)).toEqual({
      providers: [
        { providerSlug: "anthropic", planSlug: "anthropic-team" },
        { providerSlug: "opencode", planSlug: undefined },
      ],
      productSlugs: ["claude-code-cli", "opencode"],
      mdmVendor: "jamf",
    });
  });

  it("refuses an incomplete draft", () => {
    expect(() => toSaveStackRequest(EMPTY_DRAFT)).toThrow(
      "draft is incomplete",
    );
  });
});

describe("draftFromAnswers", () => {
  it("round-trips saved answers", () => {
    const draft = draftFromAnswers({
      providers: [
        { providerSlug: "anthropic", planSlug: "anthropic-enterprise" },
        { providerSlug: "opencode" },
      ],
      productSlugs: ["cowork", "opencode"],
      mdmVendor: "intune",
      updatedAt: new Date("2026-09-23T00:00:00Z"),
    });
    expect(draft).toEqual({
      providers: { anthropic: "anthropic-enterprise", opencode: null },
      products: ["cowork", "opencode"],
      mdmVendor: "intune",
    });
  });
});

describe("resumeScreen", () => {
  it("follows the server's stage", () => {
    expect(resumeScreen(undefined)).toBe("providers");
    expect(resumeScreen({ stage: "stack", steps: [], done: false })).toBe(
      "providers",
    );
    expect(resumeScreen({ stage: "use-case", steps: [], done: false })).toBe(
      "use-case",
    );
    expect(resumeScreen({ stage: "steps", steps: [], done: false })).toBe(
      "steps",
    );
    expect(resumeScreen({ stage: "done", steps: [], done: true })).toBe(
      "steps",
    );
  });
});

describe("STAGE_ONE_SCREENS", () => {
  it("lists the stack screens and then the use case", () => {
    expect(STAGE_ONE_SCREENS.map((screen) => screen.id)).toEqual([
      "providers",
      "products",
      "mdm",
      "use-case",
    ]);
    expect(WIZARD_SCREENS.map((screen) => screen.id)).toEqual([
      "providers",
      "products",
      "mdm",
      "use-case",
      "steps",
    ]);
  });
});

describe("firstSentence", () => {
  it("keeps the first sentence and a dotted name inside it", () => {
    expect(
      firstSentence(
        "In the Claude.ai admin console, point inference hooks at the platform. Every conversation starts arriving.",
      ),
    ).toBe(
      "In the Claude.ai admin console, point inference hooks at the platform.",
    );
    expect(firstSentence("Add the key.")).toBe("Add the key.");
    expect(firstSentence("No period at all")).toBe("No period at all");
  });
});

describe("stackProgress", () => {
  it("fills as the reader moves past each stack screen", () => {
    expect(stackProgress("providers")).toBe(0);
    expect(stackProgress("products")).toBeCloseTo(1 / 3);
    expect(stackProgress("mdm")).toBeCloseTo(2 / 3);
  });

  it("is full from the use case screen onward", () => {
    expect(stackProgress("use-case")).toBe(1);
    expect(stackProgress("steps")).toBe(1);
  });
});

describe("stepsProgress", () => {
  const step = (slug: string, verified: boolean) => ({
    slug,
    title: slug,
    description: "",
    evidence: "",
    techniqueSlug: "gateway",
    destination: "plugins" as const,
    verifiedAt: verified ? new Date("2026-09-23T00:00:00Z") : undefined,
  });
  const answers: OnboardingAnswers = {
    providers: [],
    productSlugs: [],
    mdmVendor: "none",
    useCase: "observability",
    updatedAt: new Date("2026-09-23T00:00:00Z"),
  };

  const partly = {
    stage: "steps" as const,
    answers,
    steps: [
      step("a", true),
      step("b", false),
      step("c", false),
      step("d", false),
    ],
    done: false,
  };

  it("is empty while the reader is not on the steps screen", () => {
    expect(stepsProgress("steps", undefined)).toBe(0);
    expect(stepsProgress("providers", partly)).toBe(0);
    expect(stepsProgress("use-case", partly)).toBe(0);
  });

  it("is the share of verified steps on the steps screen", () => {
    expect(stepsProgress("steps", partly)).toBe(0.25);
  });

  it("is full once the use case is covered", () => {
    expect(
      stepsProgress("steps", { stage: "done", answers, steps: [], done: true }),
    ).toBe(1);
  });
});
