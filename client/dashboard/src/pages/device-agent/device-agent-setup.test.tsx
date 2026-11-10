import { TooltipProvider } from "@/components/ui/Tooltip";
import type { UseAgentToken } from "@/hooks/useAgentToken";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DeviceAgentSetup, ManagedProfileExample } from "./device-agent-setup";

// buildCopyText is what useAgentToken writes to the clipboard after minting,
// captured so tests can check the copied managed.json.
const agentToken = vi.hoisted(() => ({
  current: {} as UseAgentToken,
  buildCopyText: (_token: string): string => "",
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ slug: "example-corp", name: "Example Corp" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "example-corp", projectSlug: "default" }),
}));
// Install commands use the server's stable links only over HTTPS. Tests set
// serverURL.current to cover the other forms.
const serverURL = vi.hoisted(() => ({ current: "" }));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => serverURL.current,
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));
vi.mock("@/hooks/useAgentToken", () => ({
  useAgentToken: (opts: { buildCopyText: (token: string) => string }) => {
    agentToken.buildCopyText = opts.buildCopyText;
    return agentToken.current;
  },
}));

function renderSetup(
  token: Partial<UseAgentToken>,
  ui: React.ReactElement = <DeviceAgentSetup />,
) {
  agentToken.current = {
    generatedToken: null,
    autoCopied: false,
    isPending: false,
    isError: false,
    canGenerate: true,
    hasExistingAgentKey: false,
    generate: vi.fn<() => void>(),
    ...token,
  };
  // The release lookup retries once; no delay keeps a failed lookup fast.
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retryDelay: 0 } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <MemoryRouter>{ui}</MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return agentToken.current;
}

// The release manifest the setup sheet fetches, stubbed so tests stay off the
// network and always resolve the same version.
const RELEASE_VERSION = "1.2.3";

beforeEach(() => {
  serverURL.current = "https://app.example.com";
  vi.stubGlobal("fetch", async () =>
    Response.json({
      latest: { speakeasyd: { version: RELEASE_VERSION, artifacts: [] } },
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("DeviceAgentSetup organization values", () => {
  it("shows org_slug with a copy button outside the platform walkthroughs", () => {
    renderSetup({});

    expect(screen.getByText("example-corp")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy org_slug" })).toBeTruthy();
  });

  it("mints a token directly when none exists", () => {
    const token = renderSetup({});

    fireEvent.click(screen.getByRole("button", { name: /Generate token/ }));

    expect(token.generate).toHaveBeenCalledOnce();
  });

  it("re-generates without a confirmation when a token already exists", () => {
    const token = renderSetup({ hasExistingAgentKey: true });

    fireEvent.click(screen.getByRole("button", { name: /Re-generate token/ }));

    expect(screen.queryByRole("dialog")).toBeNull();
    expect(token.generate).toHaveBeenCalledOnce();
  });

  it("shows a minted token once with a copy button", () => {
    renderSetup({ generatedToken: "spk_org_test" });

    expect(screen.getByText("spk_org_test")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy org_token" })).toBeTruthy();
    expect(screen.getByText(/shown only once/)).toBeTruthy();
  });

  it("disables minting without org:admin", () => {
    renderSetup({ canGenerate: false });

    const button = screen.getByRole("button", { name: /Generate token/ });
    expect((button as HTMLButtonElement).disabled).toBe(true);
  });
});

describe("ManagedProfileExample auto_update", () => {
  it.each([
    ["macos", "automatic"],
    ["linux", "automatic"],
    ["windows", "notify"],
  ] as const)("ships %s fleets with %s", (os, mode) => {
    renderSetup({}, <ManagedProfileExample os={os} />);

    expect(document.body.textContent).toContain(`"auto_update": "${mode}"`);
    expect(screen.queryByText(/On Windows, set/)).toBeNull();
  });

  it.each([
    ["macos", "automatic"],
    ["linux", "automatic"],
    ["windows", "notify"],
  ] as const)(
    "shows and copies a minted token with %s's %s mode",
    (os, mode) => {
      renderSetup(
        { generatedToken: "spk_org_test" },
        <ManagedProfileExample os={os} />,
      );

      expect(document.body.textContent).toContain(
        `"org_token": "spk_org_test"`,
      );
      expect(document.body.textContent).toContain(`"auto_update": "${mode}"`);
      expect(
        JSON.parse(agentToken.buildCopyText("spk_org_test")),
      ).toMatchObject({ org_token: "spk_org_test", auto_update: mode });
    },
  );

  it("uses automatic and calls out Windows when no OS is given", () => {
    renderSetup({}, <ManagedProfileExample />);

    expect(document.body.textContent).toContain(`"auto_update": "automatic"`);
    expect(screen.getByText(/On Windows, set/)).toBeTruthy();
  });
});

describe("macOS install command", () => {
  it.each(["https://app.example.com", "https://app.example.com/"])(
    "downloads the stable pkg link of %s, even with a resolved version",
    async (url) => {
      serverURL.current = url;
      renderSetup({});

      fireEvent.click(screen.getByRole("img", { name: "macOS logo" }));

      await screen.findByText(`v${RELEASE_VERSION}`);
      expect(document.body.textContent).toContain(
        "curl -fSL -o speakeasy-agent.pkg https://app.example.com/rpc/install/device-agent-macos.pkg",
      );
    },
  );

  it("downloads the versioned bucket pkg without an HTTPS server URL", async () => {
    serverURL.current = "http://localhost:8080";
    renderSetup({});

    fireEvent.click(screen.getByRole("img", { name: "macOS logo" }));

    await screen.findByText(`v${RELEASE_VERSION}`);
    expect(document.body.textContent).toContain(
      `curl -fSL -o speakeasy-agent.pkg https://storage.googleapis.com/speakeasy-device-agent-releases-prod/v${RELEASE_VERSION}/speakeasy-agent_${RELEASE_VERSION}.pkg`,
    );
  });

  it("offers the stable pkg link when the version lookup fails", async () => {
    vi.stubGlobal("fetch", async () => new Response(null, { status: 503 }));
    renderSetup({});

    fireEvent.click(screen.getByRole("img", { name: "macOS logo" }));

    const link = await screen.findByRole("link", {
      name: "stable installer link",
    });
    expect(link.getAttribute("href")).toBe(
      "https://app.example.com/rpc/install/device-agent-macos.pkg",
    );
    // The stable snippet has no shell variables, so it runs in fish too.
    expect(screen.queryByText(/Uses bash syntax/)).toBeNull();
  });
});
