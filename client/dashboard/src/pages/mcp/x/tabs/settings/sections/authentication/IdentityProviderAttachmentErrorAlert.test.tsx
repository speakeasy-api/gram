import { ProxyRegistrationError } from "@/lib/proxyRegisterUpstreamClient";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProviderAttachmentErrorAlert } from "./IdentityProviderAttachmentErrorAlert";

const scrollIntoView = vi.fn();
const originalScrollIntoView = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  "scrollIntoView",
);

beforeEach(() => {
  scrollIntoView.mockReset();
  HTMLElement.prototype.scrollIntoView = (
    arg?: boolean | ScrollIntoViewOptions,
  ): void => {
    scrollIntoView(arg);
  };
});

afterEach(() => {
  cleanup();
  if (originalScrollIntoView) {
    Object.defineProperty(
      HTMLElement.prototype,
      "scrollIntoView",
      originalScrollIntoView,
    );
  } else {
    Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
  }
});

describe("IdentityProviderAttachmentErrorAlert", () => {
  it("renders the registration status as the title and IdP detail as the body", () => {
    render(
      <IdentityProviderAttachmentErrorAlert
        error={
          new ProxyRegistrationError(
            400,
            "the callback URL is not allowed by this identity provider.",
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

  it("uses a non-duplicative body when the IdP returns no details", () => {
    render(
      <IdentityProviderAttachmentErrorAlert
        error={new ProxyRegistrationError(502)}
      />,
    );

    expect(screen.getByText("Registration failed (HTTP 502)")).toBeDefined();
    expect(
      screen.getByText("No additional error details were provided."),
    ).toBeDefined();
  });

  it("scrolls the alert into view when an error appears", () => {
    render(
      <IdentityProviderAttachmentErrorAlert
        error={new ProxyRegistrationError(400, "Registration rejected.")}
      />,
    );

    expect(scrollIntoView).toHaveBeenCalledWith({
      behavior: "smooth",
      block: "center",
    });
  });
});
