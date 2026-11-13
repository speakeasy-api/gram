import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import {
  TAILSCALE_TRUST_CREDENTIALS_URL,
  TailscaleTrustCredentialsLink,
} from "./NetworkIngressCredentialsFields";

afterEach(() => {
  cleanup();
});

describe("TailscaleTrustCredentialsLink", () => {
  it("opens the Tailscale trust credentials page in a new tab", () => {
    render(<TailscaleTrustCredentialsLink />);

    const link = screen.getByRole("link", { name: "Trust credentials" });
    expect(link.getAttribute("href")).toBe(TAILSCALE_TRUST_CREDENTIALS_URL);
    expect(TAILSCALE_TRUST_CREDENTIALS_URL).toBe(
      "https://console.tailscale.com/admin/settings/trust-credentials",
    );
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  });
});
