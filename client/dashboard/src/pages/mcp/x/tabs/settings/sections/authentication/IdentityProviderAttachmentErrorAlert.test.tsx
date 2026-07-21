import { ProxyRegistrationError } from "@/lib/proxyRegisterUpstreamClient";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { IdentityProviderAttachmentErrorAlert } from "./IdentityProviderAttachmentErrorAlert";

afterEach(cleanup);

describe("IdentityProviderAttachmentErrorAlert", () => {
  it("renders the registration status as the title and IdP detail as the body", () => {
    render(
      <IdentityProviderAttachmentErrorAlert
        error={
          new ProxyRegistrationError(
            400,
            "The callback URL is not allowed by this identity provider.",
          )
        }
      />,
    );

    expect(screen.getByText("Registration failed (HTTP 400)")).toBeDefined();
    expect(
      screen.getByText(
        "The callback URL is not allowed by this identity provider.",
      ),
    ).toBeDefined();
  });

  it("uses an attachment title for non-registration failures", () => {
    render(
      <IdentityProviderAttachmentErrorAlert
        error={new Error("The provider could not be saved.")}
      />,
    );

    expect(
      screen.getByText("Failed to attach identity provider"),
    ).toBeDefined();
    expect(screen.getByText("The provider could not be saved.")).toBeDefined();
  });
});
