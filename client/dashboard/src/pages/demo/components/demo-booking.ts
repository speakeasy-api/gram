// Cal.com routing-form UUID — same form the marketing site's /book-demo uses,
// so in-app bookings route identically. Rotate here if the Cal form changes.
export const CAL_ROUTING_FORM_ID = "80acef04-31fd-4b18-bcae-6ee91d352bb8";

export const PRODUCT_OPTIONS = [
  "AI Control Plane",
  "SDK Generation",
  "CLI Generation",
  "Terraform Generation",
] as const;

export type ProductInterest = (typeof PRODUCT_OPTIONS)[number];

export interface DemoFormData {
  firstName: string;
  lastName: string;
  email: string;
  referralSource: string;
  product: ProductInterest;
}

export type DemoFormErrors = Partial<Record<keyof DemoFormData, string>>;

export function splitDisplayName(displayName?: string): {
  firstName: string;
  lastName: string;
} {
  const trimmed = (displayName ?? "").trim();
  if (!trimmed) return { firstName: "", lastName: "" };
  const spaceIndex = trimmed.indexOf(" ");
  if (spaceIndex === -1) return { firstName: trimmed, lastName: "" };
  return {
    firstName: trimmed.slice(0, spaceIndex),
    lastName: trimmed.slice(spaceIndex + 1).trim(),
  };
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function validateDemoForm(data: DemoFormData): DemoFormErrors {
  const errors: DemoFormErrors = {};
  if (!data.firstName.trim()) errors.firstName = "First name is required";
  if (!data.lastName.trim()) errors.lastName = "Last name is required";
  if (!data.email.trim()) {
    errors.email = "Work email is required";
  } else if (!EMAIL_RE.test(data.email)) {
    errors.email = "Enter a valid email address";
  }
  if (!data.referralSource.trim())
    errors.referralSource = "This field is required";
  return errors;
}

// Mirrors the marketing site's encoding exactly (encodeURIComponent -> %20 for
// spaces) so in-app bookings route through the same Cal routing form.
export function buildCalLink(data: DemoFormData): string {
  const query = [
    `form=${CAL_ROUTING_FORM_ID}`,
    `first-name=${encodeURIComponent(data.firstName.trim())}`,
    `last-name=${encodeURIComponent(data.lastName.trim())}`,
    `email=${encodeURIComponent(data.email.trim())}`,
    `heard-about-us=${encodeURIComponent(data.referralSource.trim())}`,
    `interested-products=${encodeURIComponent(data.product)}`,
  ].join("&");
  return `router?${query}`;
}
