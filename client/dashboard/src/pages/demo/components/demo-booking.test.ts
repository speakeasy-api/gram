import { describe, expect, it } from "vitest";
import {
  buildCalLink,
  CAL_ROUTING_FORM_ID,
  DemoFormData,
  splitDisplayName,
  validateDemoForm,
} from "./demo-booking";

describe("splitDisplayName", () => {
  it("splits a two-part name", () => {
    expect(splitDisplayName("Jane Smith")).toEqual({
      firstName: "Jane",
      lastName: "Smith",
    });
  });
  it("keeps everything after the first space as the last name", () => {
    expect(splitDisplayName("Jane Van Der Berg")).toEqual({
      firstName: "Jane",
      lastName: "Van Der Berg",
    });
  });
  it("treats a single token as the first name", () => {
    expect(splitDisplayName("Cher")).toEqual({
      firstName: "Cher",
      lastName: "",
    });
  });
  it("returns empty strings for missing/blank input", () => {
    expect(splitDisplayName(undefined)).toEqual({
      firstName: "",
      lastName: "",
    });
    expect(splitDisplayName("   ")).toEqual({ firstName: "", lastName: "" });
  });
  it("trims surrounding whitespace", () => {
    expect(splitDisplayName("  Jane Smith  ")).toEqual({
      firstName: "Jane",
      lastName: "Smith",
    });
  });
});

const validData: DemoFormData = {
  firstName: "Jane",
  lastName: "Smith",
  email: "jane@acme.com",
  referralSource: "Google",
  product: "AI Control Plane",
};

describe("validateDemoForm", () => {
  it("returns no errors for valid data", () => {
    expect(validateDemoForm(validData)).toEqual({});
  });
  it("flags every required field when empty", () => {
    const errors = validateDemoForm({
      firstName: "",
      lastName: "",
      email: "",
      referralSource: "",
      product: "AI Control Plane",
    });
    expect(errors.firstName).toBeTruthy();
    expect(errors.lastName).toBeTruthy();
    expect(errors.email).toBeTruthy();
    expect(errors.referralSource).toBeTruthy();
  });
  it("rejects a malformed email", () => {
    expect(
      validateDemoForm({ ...validData, email: "not-an-email" }).email,
    ).toBeTruthy();
  });
});

describe("buildCalLink", () => {
  it("targets the routing form and URL-encodes fields", () => {
    const link = buildCalLink(validData);
    expect(link.startsWith(`router?form=${CAL_ROUTING_FORM_ID}`)).toBe(true);
    expect(link).toContain("interested-products=AI%20Control%20Plane");
    expect(link).toContain("email=jane%40acme.com");
    expect(link).toContain("first-name=Jane");
    expect(link).toContain("last-name=Smith");
    expect(link).toContain("heard-about-us=Google");
  });
});
