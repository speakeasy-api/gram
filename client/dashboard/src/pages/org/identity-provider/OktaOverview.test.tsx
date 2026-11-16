import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { OktaOverview } from "./OktaOverview";

const lookup = vi.hoisted(() => ({
  state: {
    isIdle: true,
    isPending: false,
    isSuccess: false,
    isError: false,
    data: undefined as { jwksUri?: string } | undefined,
  },
  mutate: vi.fn(),
  reset: vi.fn(),
}));

vi.mock(
  "@gram/client/react-query/fetchOrganizationRemoteSessionIssuerMetadata.js",
  () => ({
    useFetchOrganizationRemoteSessionIssuerMetadataMutation: () => ({
      ...lookup.state,
      mutate: lookup.mutate,
      reset: lookup.reset,
    }),
  }),
);

afterEach(() => {
  cleanup();
  lookup.mutate.mockReset();
  lookup.reset.mockReset();
  lookup.state = {
    isIdle: true,
    isPending: false,
    isSuccess: false,
    isError: false,
    data: undefined,
  };
});

function show(): void {
  render(
    <MemoryRouter>
      <OktaOverview
        connection={{ kind: "not-connected" }}
        emaReturn={undefined}
        mcpAuthenticationHref="/org/user-session-issuers"
      />
    </MemoryRouter>,
  );
}

function checkDomain(domain: string): void {
  fireEvent.change(screen.getByLabelText("Okta domain"), {
    target: { value: domain },
  });
  const form = screen.getByLabelText("Okta domain").closest("form");
  if (!form) throw new Error("the domain field sits in a form");
  fireEvent.submit(form);
}

it("asks for the Okta domain before anything was checked", () => {
  show();
  expect(screen.getByText("Needs your Okta domain")).toBeTruthy();
});

it("refuses an address that is not an Okta domain without checking it", () => {
  show();
  checkDomain("acme");
  expect(screen.getByText(/Enter an Okta address/)).toBeTruthy();
  expect(lookup.mutate).not.toHaveBeenCalled();
});

it.each([
  [
    "pending",
    { isIdle: false, isPending: true, isSuccess: false, isError: false },
    "Checking",
  ],
  [
    "ready",
    {
      isIdle: false,
      isPending: false,
      isSuccess: true,
      isError: false,
      data: { jwksUri: "https://acme.okta.com/oauth2/v1/keys" },
    },
    "Ready",
  ],
  [
    "failed",
    { isIdle: false, isPending: false, isSuccess: false, isError: true },
    "Couldn't use this domain",
  ],
  [
    "keyless",
    {
      isIdle: false,
      isPending: false,
      isSuccess: true,
      isError: false,
      data: {},
    },
    "Couldn't use this domain",
  ],
])(
  "shows the %s state of the Enterprise Managed Auth check",
  (_, state, badge) => {
    lookup.state = { data: undefined, ...state };
    show();
    checkDomain("acme.okta.com");
    expect(lookup.mutate).toHaveBeenCalledWith({
      request: {
        fetchIssuerMetadataRequestBody: { issuer: "https://acme.okta.com" },
      },
    });
    const features = screen.getByRole("region", {
      name: "What Okta can do here",
    });
    expect(within(features).getByText(badge)).toBeTruthy();
  },
);

it("forgets a check once the domain is edited", () => {
  lookup.state = {
    isIdle: false,
    isPending: false,
    isSuccess: true,
    isError: false,
    data: { jwksUri: "https://acme.okta.com/oauth2/v1/keys" },
  };
  show();
  checkDomain("acme.okta.com");
  expect(screen.getByText("Ready")).toBeTruthy();

  fireEvent.change(screen.getByLabelText("Okta domain"), {
    target: { value: "other.okta.com" },
  });
  expect(lookup.reset).toHaveBeenCalled();
  expect(screen.getByText("Needs your Okta domain")).toBeTruthy();
});
