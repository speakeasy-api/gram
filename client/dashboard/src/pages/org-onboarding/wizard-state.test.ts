import { describe, expect, it } from "vitest";
import type { OnboardingProduct } from "@gram/client/models/components/onboardingproduct.js";
import {
  draftFromAnswers,
  draftProblem,
  EMPTY_DRAFT,
  groupByVendor,
  productsProblem,
  resumeStep,
  setProductPlan,
  toggleProduct,
  toSaveRequest,
} from "./wizard-state";

const cursor: OnboardingProduct = {
  slug: "cursor",
  name: "Cursor",
  vendor: "cursor",
  sourceIds: ["cursor"],
  plans: [
    { slug: "cursor-pro", vendor: "cursor", name: "Pro" },
    { slug: "cursor-teams", vendor: "cursor", name: "Teams" },
  ],
};

const opencode: OnboardingProduct = {
  slug: "opencode",
  name: "opencode",
  vendor: "opencode",
  sourceIds: ["opencode"],
  plans: [],
};

const products = [cursor, opencode];

describe("productsProblem", () => {
  it("needs at least one product", () => {
    expect(productsProblem(EMPTY_DRAFT, products)).toBe(
      "Pick at least one product.",
    );
  });

  it("needs a plan for products that have plans", () => {
    const draft = toggleProduct(EMPTY_DRAFT, "cursor");
    expect(productsProblem(draft, products)).toBe(
      "Pick the plan you have for Cursor.",
    );
    expect(
      productsProblem(setProductPlan(draft, "cursor", "cursor-pro"), products),
    ).toBeNull();
  });

  it("accepts planless products as they are", () => {
    expect(
      productsProblem(toggleProduct(EMPTY_DRAFT, "opencode"), products),
    ).toBeNull();
  });
});

describe("draftProblem", () => {
  it("walks the questions in order", () => {
    let draft = setProductPlan(
      toggleProduct(EMPTY_DRAFT, "cursor"),
      "cursor",
      "cursor-pro",
    );
    expect(draftProblem(draft, products)).toBe("Say whether you use an MDM.");
    draft = { ...draft, mdmVendor: "none" };
    expect(draftProblem(draft, products)).toBe(
      "Pick the use case to reach first.",
    );
    draft = { ...draft, useCase: "observability" };
    expect(draftProblem(draft, products)).toBeNull();
  });
});

describe("toggleProduct", () => {
  it("adds and removes a product, forgetting its plan", () => {
    const added = setProductPlan(
      toggleProduct(EMPTY_DRAFT, "cursor"),
      "cursor",
      "cursor-pro",
    );
    expect(added.products).toEqual({ cursor: "cursor-pro" });
    expect(toggleProduct(added, "cursor").products).toEqual({});
  });
});

describe("toSaveRequest", () => {
  it("omits the plan for planless products", () => {
    const draft = {
      products: { cursor: "cursor-teams", opencode: null },
      mdmVendor: "jamf",
      useCase: "security",
    };
    expect(toSaveRequest(draft)).toEqual({
      products: [
        { productSlug: "cursor", planSlug: "cursor-teams" },
        { productSlug: "opencode", planSlug: undefined },
      ],
      mdmVendor: "jamf",
      useCase: "security",
    });
  });

  it("refuses an incomplete draft", () => {
    expect(() => toSaveRequest(EMPTY_DRAFT)).toThrow("draft is incomplete");
  });
});

describe("draftFromAnswers", () => {
  it("round-trips saved answers", () => {
    const draft = draftFromAnswers({
      products: [
        { productSlug: "cursor", planSlug: "cursor-pro" },
        { productSlug: "opencode" },
      ],
      mdmVendor: "intune",
      useCase: "cost-tracking",
      updatedAt: new Date("2026-09-23T00:00:00Z"),
    });
    expect(draft).toEqual({
      products: { cursor: "cursor-pro", opencode: null },
      mdmVendor: "intune",
      useCase: "cost-tracking",
    });
  });
});

describe("resumeStep", () => {
  it("opens on the questions until answers are saved", () => {
    expect(resumeStep(undefined)).toBe("products");
    expect(resumeStep({ steps: [], done: false })).toBe("products");
    expect(
      resumeStep({
        answers: {
          products: [],
          mdmVendor: "none",
          useCase: "observability",
          updatedAt: new Date(0),
        },
        steps: [],
        done: false,
      }),
    ).toBe("next-step");
  });
});

describe("groupByVendor", () => {
  it("keeps reference order inside each vendor", () => {
    const groups = groupByVendor([
      cursor,
      opencode,
      { ...cursor, slug: "cursor-2", name: "Cursor 2" },
    ]);
    expect(groups.map((group) => group.vendor)).toEqual(["cursor", "opencode"]);
    expect(groups[0]?.products.map((product) => product.slug)).toEqual([
      "cursor",
      "cursor-2",
    ]);
    expect(groups[1]?.label).toBe("Open source");
  });
});
