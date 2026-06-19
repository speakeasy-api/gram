import { describe, expect, it } from "vitest";
import { selectedUserEmails } from "./observeTargetFilters";

describe("selectedUserEmails", () => {
  it("normalizes selected email filters", () => {
    expect(
      selectedUserEmails([
        {
          display: "Alice@Example.com, bob@example.com",
          filters: [" Alice@Example.com ", "bob@example.com", ""],
          path: "user.email",
        },
      ]),
    ).toEqual(["alice@example.com", "bob@example.com"]);
  });
});
