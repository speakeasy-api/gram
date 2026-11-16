import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ClaudeOrgInstructionsStep } from "./claude-org-instructions-step";
import { ClaudePluginRegistrationStep } from "./claude-plugin-registration-step";

const publishStatus = vi.hoisted(() => ({
  current: {
    data: { connected: false } as Record<string, unknown>,
    isLoading: false,
  },
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => publishStatus.current,
}));
vi.mock("../marketplace-section", () => ({
  MarketplaceSection: ({
    requiresCollaborators,
  }: {
    requiresCollaborators?: boolean;
  }) => (
    <div>
      Marketplace section
      {requiresCollaborators ? " (needs a collaborator)" : ""}
    </div>
  ),
}));
vi.mock("../platform-step-list", () => ({
  PlatformStepList: ({
    platformId,
    steps,
    heldBack,
  }: {
    platformId: string;
    steps: Array<{ title: string; code?: string }>;
    heldBack?: string;
  }) =>
    heldBack ? (
      <p>{heldBack}</p>
    ) : (
      <ol aria-label={`${platformId} steps`}>
        {steps.map(({ title, code }) => (
          <li key={title}>
            {title}
            {code ? <pre>{code}</pre> : null}
          </li>
        ))}
      </ol>
    ),
}));

afterEach(cleanup);

beforeEach(() => {
  publishStatus.current = { data: { connected: false }, isLoading: false };
});

const stepTitles = () =>
  screen
    .getAllByRole("listitem")
    .map((item) => item.firstChild?.textContent ?? "");

describe("ClaudePluginRegistrationStep", () => {
  it("walks the registration steps once the marketplace is published", () => {
    publishStatus.current = {
      data: {
        connected: true,
        marketplaceUrl: "https://app.example.com/marketplace/tok.git",
      },
      isLoading: false,
    };

    render(<ClaudePluginRegistrationStep onComplete={() => {}} />);

    expect(
      screen.getByText("Register Speakeasy's plugins in Claude"),
    ).toBeTruthy();
    expect(screen.getByText(/A copy the device agent installs/)).toBeTruthy();
    expect(
      screen.getByText("Marketplace section (needs a collaborator)"),
    ).toBeTruthy();
    expect(stepTitles()).toEqual([
      "Open Organization settings on Claude.ai",
      "Click Add plugins → Sync from GitHub",
      "Install Claude's GitHub App, then select your repo",
      "Mark the observability plugin as Required",
      "Set access for your MCP plugins",
      "Allow Speakeasy in Claude's network egress settings",
    ]);
  });

  it("holds the steps until the marketplace is published", () => {
    render(<ClaudePluginRegistrationStep onComplete={() => {}} />);

    expect(
      screen.getByText(/Publish the marketplace above first/),
    ).toBeTruthy();
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
  });

  it("holds the steps until the repo has a collaborator", () => {
    publishStatus.current = {
      data: {
        connected: true,
        marketplaceUrl: "https://app.example.com/marketplace/tok.git",
        hasCollaborators: false,
      },
      isLoading: false,
    };

    render(<ClaudePluginRegistrationStep onComplete={() => {}} />);

    expect(screen.getByText(/Add a collaborator/)).toBeTruthy();
  });
});

describe("ClaudeOrgInstructionsStep", () => {
  it("gives the instruction to paste, how to check it, and what Cowork capture leaves out", () => {
    render(<ClaudeOrgInstructionsStep onComplete={() => {}} />);

    expect(
      screen.getByText("Add Claude organization instructions"),
    ).toBeTruthy();
    expect(screen.getByText(/every conversation starts in Chat/)).toBeTruthy();
    expect(stepTitles()).toEqual([
      "Add the organization instruction",
      "Check it in a new conversation",
    ]);
    expect(screen.getByText(/echo "Speakeasy session check-in"/)).toBeTruthy();
    expect(
      screen.getByText(/completes on its own once Speakeasy receives/),
    ).toBeTruthy();
    expect(screen.getByText(/Policies aren't enforced in Cowork/)).toBeTruthy();
  });
});
