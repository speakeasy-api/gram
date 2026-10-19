import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CreateOrganizationSignInSheet } from "./CreateOrganizationSignInSheet";
import { signInNameError, signInSlug } from "./createOrganizationSignIn";
import { invalidateAllOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";
import { invalidateIssuers } from "./invalidateIssuers";
import type { IssuerTier } from "./useIssuerTier";

const testState = vi.hoisted(() => ({
  mutate: vi.fn(),
  reset: vi.fn(),
  onSuccess: undefined as ((issuer: UserSessionIssuer) => unknown) | undefined,
  isError: false,
}));

vi.mock(
  "@gram/client/react-query/createOrganizationUserSessionIssuer.js",
  () => ({
    useCreateOrganizationUserSessionIssuerMutation: (options: {
      onSuccess: (issuer: UserSessionIssuer) => unknown;
    }) => {
      testState.onSuccess = options.onSuccess;
      return {
        mutate: testState.mutate,
        reset: testState.reset,
        isPending: false,
        isError: testState.isError,
      };
    },
  }),
);
vi.mock("./invalidateIssuers", () => ({ invalidateIssuers: vi.fn() }));
vi.mock("@gram/client/react-query/organizationGateway.js", () => ({
  invalidateAllOrganizationGateway: vi.fn(),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/components/ui/Select", () => ({
  Select: ({
    value,
    onValueChange,
    children,
  }: {
    value: string;
    onValueChange: (value: string) => void;
    children: React.ReactNode;
  }) => (
    <select
      aria-label="Session length"
      value={value}
      onChange={(event) => onValueChange(event.target.value)}
    >
      {children}
    </select>
  ),
  SelectTrigger: () => null,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  SelectItem: ({
    value,
    children,
  }: {
    value: string;
    children: React.ReactNode;
  }) => <option value={value}>{children}</option>,
}));

beforeEach(() => {
  testState.mutate.mockReset();
  testState.reset.mockReset();
  testState.onSuccess = undefined;
  testState.isError = false;
  vi.mocked(toast.success).mockReset();
});

afterEach(() => {
  cleanup();
});

const issuer = (slug: string) => ({ slug }) as UserSessionIssuer;

type TierState = Pick<
  IssuerTier,
  "items" | "isLoading" | "isError" | "isFetching" | "retry" | "refresh"
>;

function tier(overrides: Partial<TierState> = {}): TierState {
  return {
    items: [],
    isLoading: false,
    isError: false,
    isFetching: false,
    retry: () => {},
    refresh: () => Promise.resolve(),
    ...overrides,
  };
}

/** Lets the refresh started on opening finish. */
async function settle() {
  await act(async () => {});
}

const OKTA_HREF =
  "/org/identity?tab=identity-providers&provider=okta&view=setup#okta-sign-in";

function show(
  organizationTier = tier(),
  onOpenChange: (open: boolean) => void = vi.fn(),
  onCreated: (issuer: UserSessionIssuer) => void = vi.fn(),
) {
  return render(
    <MemoryRouter>
      <CreateOrganizationSignInSheet
        open
        onOpenChange={onOpenChange}
        organizationTier={organizationTier}
        oktaSetupHref={OKTA_HREF}
        onCreated={onCreated}
      />
    </MemoryRouter>,
  );
}

function createButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "Create" });
}

function chooseSpeakeasy() {
  fireEvent.click(screen.getByRole("radio", { name: /Speakeasy login/ }));
}

describe("signInSlug", () => {
  it("turns a name into lowercase words joined by hyphens", () => {
    expect(signInSlug("  Speakeasy sign-in! ")).toBe("speakeasy-sign-in");
  });
});

describe("signInNameError", () => {
  it("refuses a name another organization-wide sign-in has", () => {
    expect(
      signInNameError("Speakeasy Sign-in", [{ slug: "speakeasy-sign-in" }]),
    ).toBe("An organization-wide sign-in already has this name.");
    expect(signInNameError("linear", [{ slug: "speakeasy-sign-in" }])).toBe(
      null,
    );
  });
});

describe("CreateOrganizationSignInSheet", () => {
  it("asks how people log in before anything else", () => {
    show();
    expect(screen.getByText("How do people log in?")).toBeDefined();
    expect(screen.queryByLabelText("Name")).toBeNull();
    expect(createButton().disabled).toBe(true);
  });

  it("describes each way to log in", () => {
    show();
    expect(
      screen.getByText("People log in with their Speakeasy account."),
    ).toBeDefined();
    expect(
      screen.getByText("People log in through your company’s Okta."),
    ).toBeDefined();
  });

  it("sends Okta sign-ins to the Okta setup instead of a form", () => {
    show();
    fireEvent.click(screen.getByRole("radio", { name: /Okta/ }));
    expect(screen.queryByLabelText("Name")).toBeNull();
    expect(
      screen.getByText(
        "Okta sign-in is set up from your Okta connection. You need a verified Okta connection first.",
      ),
    ).toBeDefined();
    const links = screen.getAllByRole("link");
    expect(links).toHaveLength(1);
    const [link] = links;
    expect(link?.textContent).toBe("Go to Okta sign-in");
    expect(link?.getAttribute("href")).toBe(OKTA_HREF);
    expect(screen.queryByRole("button", { name: "Create" })).toBeNull();
  });

  it("asks Okta orgs with a sign-in to trust Okta on it instead", () => {
    show(tier({ items: [issuer("linear-ema")] }));
    fireEvent.click(screen.getByRole("radio", { name: /Okta/ }));
    expect(
      screen.getByText(
        "To keep the organization gateway on, open Okta sign-in and choose Trust Okta sign-in on linear-ema instead of creating a new one.",
      ),
    ).toBeDefined();
  });

  it("asks for a clear name and a session length for Speakeasy login", () => {
    show();
    chooseSpeakeasy();
    expect(screen.getByText(/Name it for what it’s for/)).toBeDefined();
    expect(screen.getByPlaceholderText("Speakeasy sign-in")).toBeDefined();
    expect(
      (screen.getByLabelText("Session length") as HTMLSelectElement).value,
    ).toBe(String(24 * 14));
    expect(createButton().disabled).toBe(true);
  });

  it("won't create one with a name already in use", () => {
    show(tier({ items: [issuer("taken")] }));
    chooseSpeakeasy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Taken" },
    });
    expect(
      screen.getByText("An organization-wide sign-in already has this name."),
    ).toBeDefined();
    expect(createButton().disabled).toBe(true);
  });

  it("caps the name and explains names it can't use", () => {
    show();
    chooseSpeakeasy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "!!!" },
    });
    expect(screen.getByText("Use letters or numbers")).toBeDefined();
    expect(createButton().disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "a".repeat(101) },
    });
    expect(screen.getByText("Name is too long")).toBeDefined();
    expect(createButton().disabled).toBe(true);
    expect(signInSlug(`${"a".repeat(99)}-b`)).toBe("a".repeat(99));
  });

  it("waits while the list of sign-ins refreshes", () => {
    show(tier({ isFetching: true }));
    chooseSpeakeasy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Speakeasy sign-in" },
    });
    expect(createButton().disabled).toBe(true);
  });

  it("waits for the full list of sign-ins before creating", () => {
    show(tier({ isLoading: true }));
    chooseSpeakeasy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Speakeasy sign-in" },
    });
    expect(createButton().disabled).toBe(true);
  });

  it("won't create while the list of sign-ins failed to load", () => {
    show(tier({ isError: true }));
    chooseSpeakeasy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Speakeasy sign-in" },
    });
    expect(
      screen.getByText("Couldn’t check your existing sign-ins."),
    ).toBeDefined();
    expect(createButton().disabled).toBe(true);
  });

  it("warns that a second one turns off the organization gateway", async () => {
    show(tier({ items: [issuer("existing")] }));
    await settle();
    chooseSpeakeasy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Speakeasy sign-in" },
    });
    expect(
      screen.getByText(
        "existing is already organization-wide. With two, the organization gateway is off until only one is left.",
      ),
    ).toBeDefined();
    expect(createButton().disabled).toBe(true);
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Turn off the organization gateway",
      }),
    );
    expect(createButton().disabled).toBe(false);
  });

  it("shows no gateway warning for the first one", () => {
    show();
    chooseSpeakeasy();
    expect(screen.queryByText(/organization gateway/)).toBeNull();
  });

  it("creates it with the chosen length, refreshes, then opens it", async () => {
    const onOpenChange = vi.fn();
    const onCreated = vi.fn();
    show(
      tier(),
      (open) => {
        onOpenChange(open);
      },
      (created) => {
        onCreated(created);
      },
    );
    await settle();
    chooseSpeakeasy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Speakeasy sign-in" },
    });
    expect(screen.getByText("Saved as speakeasy-sign-in")).toBeDefined();
    fireEvent.change(screen.getByLabelText("Session length"), {
      target: { value: "24" },
    });
    fireEvent.click(createButton());
    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        createOrganizationUserSessionIssuerForm: {
          slug: "speakeasy-sign-in",
          authnChallengeMode: "interactive",
          sessionDurationHours: 24,
        },
      },
    });

    const created = issuer("speakeasy-sign-in");
    await testState.onSuccess?.(created);
    expect(invalidateIssuers).toHaveBeenCalled();
    expect(invalidateAllOrganizationGateway).toHaveBeenCalled();
    expect(toast.success).toHaveBeenCalledWith("Created speakeasy-sign-in");
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onCreated).toHaveBeenCalledWith(created);
  });

  it("closes only after the lists have refreshed", async () => {
    let finish = () => {};
    vi.mocked(invalidateIssuers).mockReturnValueOnce(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );
    const onOpenChange = vi.fn();
    show(tier(), (open) => {
      onOpenChange(open);
    });
    const done = testState.onSuccess?.(issuer("speakeasy-sign-in"));
    await Promise.resolve();
    expect(onOpenChange).not.toHaveBeenCalled();
    finish();
    await done;
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("reloads the list on opening and waits for it before creating", async () => {
    let finish = () => {};
    const refresh = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    show(tier({ refresh }));
    expect(refresh).toHaveBeenCalledTimes(1);
    chooseSpeakeasy();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Speakeasy sign-in" },
    });
    expect(createButton().disabled).toBe(true);
    await act(async () => {
      finish();
    });
    expect(createButton().disabled).toBe(false);
  });

  it("offers a retry when the list of sign-ins failed to load", () => {
    const retry = vi.fn();
    show(
      tier({
        isError: true,
        retry: () => {
          retry();
        },
      }),
    );
    chooseSpeakeasy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it("clears an earlier failure when it opens", () => {
    show();
    expect(testState.reset).toHaveBeenCalled();
  });

  it("says when it couldn't create", () => {
    testState.isError = true;
    show();
    chooseSpeakeasy();
    expect(
      screen.getByText("Couldn’t create the sign-in. Try again."),
    ).toBeDefined();
  });
});
