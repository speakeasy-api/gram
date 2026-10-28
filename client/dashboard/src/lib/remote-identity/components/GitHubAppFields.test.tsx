import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { GitHubAppFields } from "./GitHubAppFields";
import { gitHubAppReady, newGitHubAppDraft } from "../model/githubApp";

describe("GitHub App configuration", () => {
  it("defaults to GitHub.com and requires all credentials", () => {
    const draft = newGitHubAppDraft();
    expect(draft.instanceUrl).toBe("https://github.com");
    expect(gitHubAppReady(draft)).toBe(false);
    expect(
      gitHubAppReady({
        instanceUrl: "https://git.example.com",
        appId: "123",
        installationId: "456",
        privateKey: "-----BEGIN RSA PRIVATE KEY-----\nTEST_ONLY",
      }),
    ).toBe(true);
  });
  it.each(["instanceUrl", "appId", "installationId", "privateKey"] as const)(
    "requires %s",
    (field) => {
      const complete = {
        instanceUrl: "https://github.com",
        appId: "123",
        installationId: "456",
        privateKey: "-----BEGIN PRIVATE KEY-----",
      };
      expect(gitHubAppReady({ ...complete, [field]: "" })).toBe(false);
    },
  );
  it("has accessible enterprise and private key fields", () => {
    const onChange =
      vi.fn<(value: import("../model/githubApp").GitHubAppDraft) => void>();
    render(<GitHubAppFields value={newGitHubAppDraft()} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText("GitHub instance URL"), {
      target: { value: "https://git.example.com" },
    });
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ instanceUrl: "https://git.example.com" }),
    );
    expect(screen.getByLabelText("Private key (PEM)")).toBeDefined();
    expect(screen.queryByLabelText("Client secret")).toBeNull();
    expect(screen.getByText(/encrypted and never returned/)).toBeDefined();
  });
});
