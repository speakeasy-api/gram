import type { ComponentProps, ReactNode } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AssigneeAvatar } from "./assignee-avatar";

vi.mock("@/components/gradient-colors", () => ({
  useIdentityTint: () => ({}),
}));
vi.mock("@/components/ui/Avatar", () => ({
  Avatar: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AvatarFallback: ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  ),
  AvatarImage: ({ src, alt }: ComponentProps<"img">) => (
    <img src={src} alt={alt} />
  ),
}));
afterEach(cleanup);

function renderAvatar(photoUrl?: string) {
  render(
    <AssigneeAvatar
      assignee={{
        kind: "user",
        userId: "user-test",
        name: "Test Owner",
        email: "owner@example.test",
        photoUrl,
      }}
    />,
  );
}

it("renders a parsed HTTPS avatar URL", () => {
  renderAvatar("HTTPS://example.test/avatar.png");
  expect(screen.getByRole("img").getAttribute("src")).toBe(
    "https://example.test/avatar.png",
  );
});

it.each([
  undefined,
  "",
  "http://example.test/avatar.png",
  "//example.test/avatar.png",
  "/avatar.png",
  "javascript:alert(1)",
  "data:image/png;base64,abc",
  "blob:https://example.test/avatar",
  "ftp://example.test/avatar.png",
  "https://",
  "https://[invalid",
  "not a URL",
])("uses initials without requesting an unsafe or malformed URL: %s", (url) => {
  renderAvatar(url);
  expect(screen.queryByRole("img")).toBeNull();
  expect(screen.getByText("TO")).toBeTruthy();
});
