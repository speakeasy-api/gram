import type { RemoteSessionIssuer } from "@gram/admin-client/models/components/remotesessionissuer";
import {
  cleanup,
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { IssuerEditor } from "./IssuerEditor";
const api = vi.hoisted(() => ({
  create: vi.fn(),
  discover: vi.fn(),
  update: vi.fn(),
  refresh: vi.fn(),
  upload: vi.fn(),
  duplicates: vi.fn().mockResolvedValue({ matches: [] }),
}));
vi.mock("@/lib/gramAdminClient", () => ({
  adminCreateGlobalIssuer: api.create,
  adminUpdateGlobalIssuer: api.update,
  adminRefreshGlobalIssuerMetadata: api.refresh,
  adminUploadPlatformImage: api.upload,
  adminFetchGlobalIssuerMetadata: api.discover,
  adminGetGlobalIssuerDuplicatePreflightQuery: (request: unknown) => ({
    queryKey: ["duplicates", request],
    queryFn: () => api.duplicates(request),
  }),
}));
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    params,
    children,
    onClick,
  }: {
    params: { issuerId: string };
    children: React.ReactNode;
    onClick: () => void;
  }) => (
    <a href={`/remote-session-issuers/${params.issuerId}`} onClick={onClick}>
      {children}
    </a>
  ),
}));
vi.mock("./IssuerLogo", () => ({
  IssuerLogo: ({ id }: { id: string }) => <span>Logo {id}</span>,
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
function mount(issuer?: RemoteSessionIssuer) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <IssuerEditor issuer={issuer} onDone={vi.fn<() => void>()} />
    </QueryClientProvider>,
  );
}
it("derives name and slug independently without overwriting edits", () => {
  mount();
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://first.example" },
  });
  expect(
    (screen.getByLabelText("Display name") as HTMLInputElement).value,
  ).toBe("first.example");
  fireEvent.change(screen.getByLabelText("Display name"), {
    target: { value: "Custom" },
  });
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://second.example" },
  });
  expect(
    (screen.getByLabelText("Display name") as HTMLInputElement).value,
  ).toBe("Custom");
  expect((screen.getByLabelText("Slug") as HTMLInputElement).value).toBe(
    "second-example",
  );
});
it("retains a failed create draft and reports the failure", async () => {
  api.create.mockRejectedValue(new Error("Save refused"));
  mount();
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://first.example" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create issuer" }));
  await waitFor(() => expect(api.create).toHaveBeenCalled());
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Save refused",
  );
  expect((screen.getByLabelText("Issuer URL") as HTMLInputElement).value).toBe(
    "https://first.example",
  );
});
it("resets only nonempty discovered endpoints and clears all four on URL change", async () => {
  api.discover.mockResolvedValue({
    issuer: "https://first.example",
    authorizationEndpoint: "https://first.example/auth",
    tokenEndpoint: "https://first.example/token",
    discoveryWarnings: [],
    clientIdMetadataDocumentSupported: false,
  });
  mount();
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://first.example" },
  });
  fireEvent.change(screen.getByLabelText("Registration endpoint"), {
    target: { value: "https://manual.example/register" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Discover" }));
  await waitFor(() =>
    expect(
      (screen.getByLabelText("Authorization endpoint") as HTMLInputElement)
        .value,
    ).toBe("https://first.example/auth"),
  );
  expect(screen.queryByRole("button", { name: "Reset endpoints" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Authorization endpoint"), {
    target: { value: "https://manual.example/auth" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Reset endpoints" }));
  expect(
    (screen.getByLabelText("Authorization endpoint") as HTMLInputElement).value,
  ).toBe("https://first.example/auth");
  expect(
    (screen.getByLabelText("Registration endpoint") as HTMLInputElement).value,
  ).toBe("https://manual.example/register");
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://second.example" },
  });
  for (const label of [
    "Authorization endpoint",
    "Token endpoint",
    "Registration endpoint",
    "JWKS URI",
  ])
    expect((screen.getByLabelText(label) as HTMLInputElement).value).toBe("");
  expect(screen.getByRole("button", { name: "Discover" })).toBeTruthy();
});
it("preserves manually entered create endpoints before any discovery snapshot", () => {
  mount();
  fireEvent.change(screen.getByLabelText("Authorization endpoint"), {
    target: { value: "https://manual.example/auth" },
  });
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://second.example" },
  });
  expect(
    (screen.getByLabelText("Authorization endpoint") as HTMLInputElement).value,
  ).toBe("https://manual.example/auth");
});

const savedIssuer = {
  id: "saved",
  issuer: "https://saved.example",
  name: "Saved provider",
  slug: "saved",
  logoAssetId: "old-logo",
  authorizationEndpoint: "https://saved.example/auth",
  tokenEndpoint: "https://saved.example/token",
  clientIdMetadataDocumentSupported: false,
  createdAt: new Date(),
  updatedAt: new Date(),
  oidc: false,
  passthrough: false,
  organizationId: "",
  projectId: "",
} satisfies RemoteSessionIssuer;
it("clears saved endpoints on URL edits before discovery", () => {
  mount(savedIssuer);
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://changed.example" },
  });
  expect(
    (screen.getByLabelText("Authorization endpoint") as HTMLInputElement).value,
  ).toBe("");
  expect(
    screen.queryByRole("button", { name: "Refresh saved metadata" }),
  ).toBeNull();
});
it("refresh persists metadata while retaining unrelated unsaved names", async () => {
  api.refresh.mockResolvedValue({
    issuer: {
      ...savedIssuer,
      authorizationEndpoint: "https://saved.example/new-auth",
    },
    discoveryWarnings: ["Discovery warning"],
  });
  mount(savedIssuer);
  fireEvent.change(screen.getByLabelText("Display name"), {
    target: { value: "Unsaved name" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Refresh saved metadata" }),
  );
  expect(await screen.findByText("Discovery warning")).toBeTruthy();
  expect(api.refresh).toHaveBeenCalledWith({ id: "saved" });
  expect(
    (screen.getByLabelText("Display name") as HTMLInputElement).value,
  ).toBe("Unsaved name");
  expect(
    (screen.getByLabelText("Authorization endpoint") as HTMLInputElement).value,
  ).toBe("https://saved.example/new-auth");
});
it("holds saves during logo upload and sends explicit logo removal", async () => {
  let finish: (value: { asset: { id: string } }) => void = () => {};
  api.upload.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  api.update.mockResolvedValue(savedIssuer);
  mount(savedIssuer);
  fireEvent.change(screen.getByLabelText("Logo"), {
    target: { files: [new File(["image"], "logo.png", { type: "image/png" })] },
  });
  fireEvent.submit(screen.getByLabelText("Issuer URL").closest("form")!);
  expect(api.update).not.toHaveBeenCalled();
  finish({ asset: { id: "new-logo" } });
  expect(await screen.findByText("Logo new-logo")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Remove logo" }));
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(api.update).toHaveBeenCalledWith(
      expect.objectContaining({ logoAssetId: "" }),
    ),
  );
});
it("rejects logos over the baseline four MiB limit without uploading", async () => {
  mount(savedIssuer);
  fireEvent.change(screen.getByLabelText("Logo"), {
    target: {
      files: [
        new File([new Uint8Array(4 * 1024 * 1024 + 1)], "large.png", {
          type: "image/png",
        }),
      ],
    },
  });
  expect((await screen.findByRole("alert")).textContent).toContain("4 MiB");
  expect(api.upload).not.toHaveBeenCalled();
});

it("selectively resets saved endpoints without losing the unsaved name or doing discovery", () => {
  mount(savedIssuer);
  expect(screen.queryByRole("button", { name: "Discover" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Display name"), {
    target: { value: "Unsaved label" },
  });
  fireEvent.change(screen.getByLabelText("Authorization endpoint"), {
    target: { value: "https://manual.example/auth" },
  });
  fireEvent.change(screen.getByLabelText("Registration endpoint"), {
    target: { value: "https://manual.example/register" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Reset endpoints" }));
  expect(
    (screen.getByLabelText("Authorization endpoint") as HTMLInputElement).value,
  ).toBe(savedIssuer.authorizationEndpoint);
  expect(
    (screen.getByLabelText("Registration endpoint") as HTMLInputElement).value,
  ).toBe("https://manual.example/register");
  expect(
    (screen.getByLabelText("Display name") as HTMLInputElement).value,
  ).toBe("Unsaved label");
  expect(api.discover).not.toHaveBeenCalled();
  expect(api.refresh).not.toHaveBeenCalled();
});
it("checks valid URLs on blur and links matching IDs while excluding itself", async () => {
  api.duplicates.mockResolvedValue({
    matches: [
      {
        id: "saved",
        tier: "global",
        name: "Self",
        issuer: "https://saved.example",
        slug: "saved",
        projectName: "",
      },
      {
        id: "existing",
        tier: "global",
        name: "Other provider",
        issuer: "https://other.example",
        slug: "other",
        projectName: "",
      },
    ],
  });
  mount(savedIssuer);
  expect(
    await screen.findByRole("link", { name: "View existing provider" }),
  ).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: "View existing provider" })
      .getAttribute("href"),
  ).toBe("/remote-session-issuers/existing");
  api.duplicates.mockClear();
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "not-a-url" },
  });
  fireEvent.blur(screen.getByLabelText("Issuer URL"));
  expect(api.duplicates).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://settled.example" },
  });
  expect(api.duplicates).not.toHaveBeenCalled();
  fireEvent.blur(screen.getByLabelText("Issuer URL"));
  await waitFor(() =>
    expect(api.duplicates).toHaveBeenCalledWith({
      issuer: "https://settled.example",
    }),
  );
});

it("restores all saved endpoints when returning to the saved URL", async () => {
  const issuer = {
    ...savedIssuer,
    registrationEndpoint: "https://saved.example/register",
    jwksUri: "https://saved.example/jwks",
  };
  api.update.mockResolvedValue({ issuer });
  mount(issuer);
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://changed.example" },
  });
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: issuer.issuer },
  });
  for (const [label, value] of [
    ["Authorization endpoint", issuer.authorizationEndpoint],
    ["Token endpoint", issuer.tokenEndpoint],
    ["Registration endpoint", issuer.registrationEndpoint],
    ["JWKS URI", issuer.jwksUri],
  ] as const) {
    expect((screen.getByLabelText(label) as HTMLInputElement).value).toBe(
      value,
    );
  }
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(api.update).toHaveBeenCalledWith(
      expect.objectContaining({
        id: issuer.id,
        authorizationEndpoint: issuer.authorizationEndpoint,
        tokenEndpoint: issuer.tokenEndpoint,
        registrationEndpoint: issuer.registrationEndpoint,
        jwksUri: issuer.jwksUri,
      }),
    ),
  );
});

const nullableCapabilities = [
  "codeChallengeMethodsSupported",
  "introspectionEndpointAuthMethodsSupported",
  "idTokenSigningAlgValuesSupported",
  "claimsSupported",
] as const;
it.each([undefined, null])(
  "clears saved capabilities omitted by fresh discovery (%s)",
  async (absent) => {
    mount({
      ...savedIssuer,
      ...Object.fromEntries(
        nullableCapabilities.map((key) => [key, ["stale"]]),
      ),
    });
    api.discover.mockResolvedValue({
      issuer: "https://changed.example",
      discoveryWarnings: [],
      ...Object.fromEntries(nullableCapabilities.map((key) => [key, absent])),
    });
    api.update.mockResolvedValue(savedIssuer);
    fireEvent.change(screen.getByLabelText("Issuer URL"), {
      target: { value: "https://changed.example" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Discover" }));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Discover" })).toBeNull(),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(api.update).toHaveBeenCalledWith(
        expect.objectContaining(
          Object.fromEntries(nullableCapabilities.map((key) => [key, []])),
        ),
      ),
    );
  },
);
it("keeps uncaptured saved capabilities omitted without fresh discovery", async () => {
  mount({
    ...savedIssuer,
    ...Object.fromEntries(nullableCapabilities.map((key) => [key, null])),
  });
  api.update.mockResolvedValue(savedIssuer);
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() =>
    expect(api.update).toHaveBeenCalledWith(
      expect.objectContaining(
        Object.fromEntries(nullableCapabilities.map((key) => [key, undefined])),
      ),
    ),
  );
});
