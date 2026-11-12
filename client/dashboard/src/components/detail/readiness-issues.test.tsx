import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import {
  ReadinessIssueNotice,
  ReadinessIssuesProvider,
} from "./readiness-issues";

const checks = [
  {
    key: "sign-in",
    label: "Set up sign-in for Linear",
    description: "Add its authorization server here.",
    ready: false,
    broken: true,
    href: "/gateway/settings#authentication",
  },
  {
    key: "url",
    label: "Gateway URL",
    description: "Live.",
    ready: true,
    href: "/gateway/settings#server-url",
  },
];

it("shows the header issues that link to this section", () => {
  render(
    <ReadinessIssuesProvider checks={checks}>
      <ReadinessIssueNotice anchorId="authentication" />
    </ReadinessIssuesProvider>,
  );
  expect(screen.getByText("Set up sign-in for Linear")).toBeTruthy();
});

it("shows nothing for a section with no open issue", () => {
  const { container } = render(
    <ReadinessIssuesProvider checks={checks}>
      <ReadinessIssueNotice anchorId="server-url" />
    </ReadinessIssuesProvider>,
  );
  expect(container.innerHTML).toBe("");
});
