import { expect, it, vi } from "vitest";

// The handwritten API and ordinary queries must not evaluate every generated
// SDK operation just to share authentication state or invalidate one query.
// A factory runs only if the module is actually imported, including transitively.
vi.mock("@/lib/gramAdminClient", () => {
  throw new Error("Lightweight query consumers loaded the full SDK wrapper");
});

it("loads ordinary queries without the full SDK wrapper", async () => {
  await expect(import("./adminQueries")).resolves.toHaveProperty(
    "organizationsListQuery",
  );
});

it("loads the handwritten API without the full SDK wrapper", async () => {
  await expect(import("./gramAdminApi")).resolves.toHaveProperty("getSession");
});

it("loads record navigation without the full SDK wrapper", async () => {
  await expect(import("@/components/record-nav")).resolves.toHaveProperty(
    "RecordNav",
  );
});
