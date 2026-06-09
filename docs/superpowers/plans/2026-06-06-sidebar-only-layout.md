# Sidebar-only app layout — PR 1 (shell) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the dashboard's full-width top bar with a single vertical sidebar/main split, relocating all top-bar functionality into the sidebar header + footer and repurposing the brand gradient as a left-rail accent.

**Architecture:** Remove `TopHeader` + `BrandGradientLine` from `AppLayout`/`OrgLayout`. Add a `SidebarHeader` (logo + combined org/project `WorkspaceSwitcher`) and a richer `SidebarFooter` (`SidebarUserMenu`: avatar + theme + ⋯ menu). Add a left-rail gradient accent to the active nav item and the switcher. Add an empty `Page.Header.Actions` slot to the main toolbar (filled by a later migration PR).

**Tech Stack:** React 19, react-router, Tailwind v4, `@speakeasy-api/moonshine`, Radix-based `components/ui/*`, vitest + @testing-library/react.

**Spec:** `docs/superpowers/specs/2026-06-06-sidebar-only-layout-design.md`

**Conventions (from the `frontend` skill — follow exactly):**

- Verify with `pnpm -F dashboard <script>`: `type-check`, `test`, `lint`, `build`. Never bare `tsc`/`vitest`/`eslint`.
- Internal links via route helpers (`routes.x.href()` / `.Link`); external links via plain `<a target="_blank" rel="noopener noreferrer">`.
- Moonshine utilities only; no hardcoded Tailwind colors. No nested `TooltipProvider`.
- All commits in this plan use `git commit --no-verify` — the worktree's oxfmt pre-commit hook is known to fail (see project memory). Run `pnpm -F dashboard lint:format` manually before the final commit instead.

---

## File Structure

**Create:**

- `client/dashboard/src/components/brand-gradient-rail.tsx` — vertical gradient accent bar (shared by switcher + active nav).
- `client/dashboard/src/components/brand-gradient-rail.test.tsx`
- `client/dashboard/src/components/workspace-switcher.tsx` — combined org/project switcher for the sidebar header.
- `client/dashboard/src/components/workspace-switcher.test.tsx`
- `client/dashboard/src/components/sidebar-user-menu.tsx` — footer avatar + theme + ⋯ menu.
- `client/dashboard/src/components/sidebar-user-menu.test.tsx`

**Modify:**

- `client/dashboard/src/components/ui/sidebar.tsx` — add `SidebarHeader` primitive; change fixed offset default.
- `client/dashboard/src/components/nav-menu.tsx` — active-item gradient rail.
- `client/dashboard/src/components/nav-menu.test.tsx` — rail tests.
- `client/dashboard/src/components/app-sidebar.tsx` — header + footer wiring; pass group `active`.
- `client/dashboard/src/components/org-sidebar.tsx` — header + footer wiring.
- `client/dashboard/src/components/page-header.tsx` — `PageHeader.Actions` slot.
- `client/dashboard/src/components/page-header.test.tsx` — slot test (new file if absent).
- `client/dashboard/src/components/app-layout.tsx` — remove top bar; restructure; CSS vars.
- `client/dashboard/src/components/page-layout.tsx` — height recompute.

**Delete:**

- `client/dashboard/src/components/top-header.tsx` — after logic ported.

---

## Task 1: Brand gradient rail primitive

**Files:**

- Create: `client/dashboard/src/components/brand-gradient-rail.tsx`
- Test: `client/dashboard/src/components/brand-gradient-rail.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
// brand-gradient-rail.test.tsx
import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { BrandGradientRail } from "./brand-gradient-rail";

afterEach(cleanup);

describe("BrandGradientRail", () => {
  it("renders an aria-hidden bar with the brand gradient", () => {
    const { container } = render(<BrandGradientRail />);
    const el = container.firstChild as HTMLElement;
    expect(el.getAttribute("aria-hidden")).toBe("true");
    expect(el.style.background).toContain("gradient-brand-primary-colors");
  });

  it("merges a passed className", () => {
    const { container } = render(<BrandGradientRail className="left-0" />);
    expect((container.firstChild as HTMLElement).className).toContain("left-0");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm -F dashboard test src/components/brand-gradient-rail.test.tsx`
Expected: FAIL — cannot resolve `./brand-gradient-rail`.

- [ ] **Step 3: Write minimal implementation**

```tsx
// brand-gradient-rail.tsx
import { cn } from "@/lib/utils";

type BrandGradientRailProps = {
  className?: string;
};

/**
 * Vertical sibling of BrandGradientLine: the Speakeasy brand spectrum as a thin
 * vertical accent bar. Used as a left rail on the active nav item and the
 * workspace switcher now that the horizontal brand line is gone from the top bar.
 * Position it with className (e.g. absolute left-0). Pulls the gradient from
 * Moonshine so it stays in sync with brand updates.
 */
export function BrandGradientRail({ className }: BrandGradientRailProps) {
  return (
    <div
      aria-hidden
      className={cn("w-[3px] rounded-full", className)}
      style={{
        background:
          "linear-gradient(180deg, var(--gradient-brand-primary-colors))",
      }}
    />
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm -F dashboard test src/components/brand-gradient-rail.test.tsx`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/components/brand-gradient-rail.tsx client/dashboard/src/components/brand-gradient-rail.test.tsx
git commit --no-verify -m "feat(dashboard): add BrandGradientRail accent primitive"
```

---

## Task 2: Active-nav gradient rail

**Files:**

- Modify: `client/dashboard/src/components/nav-menu.tsx` (`NavButton`, `CollapsibleNavGroup`, `CollapsibleNavItem`)
- Test: `client/dashboard/src/components/nav-menu.test.tsx`

The rail marks the active route. Expanded mode: rail on the active leaf (`NavButton` when `active`, `CollapsibleNavItem` when `item.active`). Collapsed mode: sub-items are hidden, so the active group icon shows the rail — driven by a new `active` prop on `CollapsibleNavGroup` plus `hidden group-data-[collapsible=icon]:block`.

- [ ] **Step 1: Write the failing tests**

Add to `nav-menu.test.tsx`. First extend the existing mocks so the new imports resolve — add this mock near the others at the top of the file:

```tsx
vi.mock("./brand-gradient-rail", () => ({
  BrandGradientRail: ({ className }: { className?: string }) => (
    <div data-testid="nav-rail" className={className} />
  ),
}));
```

Then append this describe block:

```tsx
describe("NavButton active rail", () => {
  afterEach(cleanup);

  it("renders the gradient rail when active", () => {
    render(<NavButton title="Home" Icon={TestIcon} active />);
    expect(screen.getByTestId("nav-rail")).toBeTruthy();
  });

  it("does not render the rail when inactive", () => {
    render(<NavButton title="Home" Icon={TestIcon} />);
    expect(screen.queryByTestId("nav-rail")).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm -F dashboard test src/components/nav-menu.test.tsx`
Expected: FAIL — `getByTestId("nav-rail")` not found (rail not rendered yet).

- [ ] **Step 3: Implement the rail in `NavButton`**

In `nav-menu.tsx`, add the import:

```tsx
import { BrandGradientRail } from "./brand-gradient-rail";
```

In `NavButton`, the `<Link>` is already `relative z-1`. Insert the rail as its first child, shown only when `active`:

```tsx
      <Link
        to={href ?? "#"}
        target={target}
        onClick={handleClick}
        className={cn(
          "relative z-1 flex w-full items-center gap-2 rounded-lg px-2 py-2 text-sm transition-colors hover:no-underline",
          "group-data-[collapsible=icon]:min-w-8 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:gap-0 group-data-[collapsible=icon]:p-2!",
          active
            ? "text-foreground font-semibold"
            : "text-muted-foreground hover:text-foreground font-medium",
        )}
      >
        {active && (
          <BrandGradientRail className="absolute top-1.5 bottom-1.5 left-0" />
        )}
        {Icon && (
```

(leave the rest of `NavButton` unchanged.)

- [ ] **Step 4: Implement the rail in `CollapsibleNavItem`**

The sub-item `<Link>` is already `relative z-1`. Insert the rail as its first child, shown when `item.active`:

```tsx
        <Link
          to={item.href()}
          onClick={handleClick}
          className={cn(
            "relative z-1 flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:no-underline",
            item.active
              ? "text-foreground font-semibold"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {item.active && (
            <BrandGradientRail className="absolute top-1 bottom-1 -left-2" />
          )}
          <span className={cn("truncate", isLoading && "nav-shimmer")}>
```

> The `-left-2` lands the rail on the group's `border-l` gutter (the sub-list is wrapped in `ml-4 border-l pl-2`), so it reads as "this leaf within the group."

- [ ] **Step 5: Implement the collapsed-mode group rail in `CollapsibleNavGroup`**

Add an `active` prop to the component signature:

```tsx
export function CollapsibleNavGroup({
  label,
  Icon,
  defaultHref,
  stage,
  active,
  children,
}: {
  label: string;
  Icon: React.ComponentType<{ className?: string }>;
  defaultHref?: string;
  stage?: ReleaseStage;
  active?: boolean;
  children: React.ReactNode;
}) {
```

The group's `<Link>` is `relative z-1`. Insert the rail as its first child — visible only in collapsed (icon) mode:

```tsx
          <Link
            to={defaultHref ?? "#"}
            onClick={handleClick}
            className={cn(
              "relative z-1 flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors hover:no-underline",
              "group-data-[collapsible=icon]:min-w-8 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:gap-0 group-data-[collapsible=icon]:p-2!",
              "cursor-pointer outline-hidden",
              isOpen
                ? "text-foreground font-semibold"
                : "text-muted-foreground hover:text-foreground font-medium",
            )}
          >
            {active && (
              <BrandGradientRail className="absolute top-1.5 bottom-1.5 left-0 hidden group-data-[collapsible=icon]:block" />
            )}
            <Icon
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `pnpm -F dashboard test src/components/nav-menu.test.tsx`
Expected: PASS (all existing tests + the 2 new rail tests).

- [ ] **Step 7: Commit**

```bash
git add client/dashboard/src/components/nav-menu.tsx client/dashboard/src/components/nav-menu.test.tsx
git commit --no-verify -m "feat(dashboard): gradient left rail on active nav item"
```

---

## Task 3: `SidebarHeader` primitive

**Files:**

- Modify: `client/dashboard/src/components/ui/sidebar.tsx` (add component + export)

- [ ] **Step 1: Add the component**

Mirror the existing `SidebarFooter` (around `ui/sidebar.tsx:270`). Add directly after it:

```tsx
function SidebarHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="sidebar-header"
      data-sidebar="header"
      className={cn("flex flex-col gap-2 p-2", className)}
      {...props}
    />
  );
}
```

- [ ] **Step 2: Export it**

In the `export { … }` block (`ui/sidebar.tsx:317`), add `SidebarHeader` to the exported names (alphabetical/with the other `Sidebar*` names).

- [ ] **Step 3: Verify it type-checks**

Run: `pnpm -F dashboard type-check`
Expected: PASS (no usages yet; just confirms the new symbol compiles).

- [ ] **Step 4: Commit**

```bash
git add client/dashboard/src/components/ui/sidebar.tsx
git commit --no-verify -m "feat(dashboard): add SidebarHeader primitive"
```

---

## Task 4: `WorkspaceSwitcher` component

**Files:**

- Create: `client/dashboard/src/components/workspace-switcher.tsx`
- Test: `client/dashboard/src/components/workspace-switcher.test.tsx`

Ports the project-switcher Popover+Command and "Create Project" flow from `top-header.tsx:141–205`. Combined `org / project` label with the gradient left rail. On org-level pages (no `projectSlug`) it renders org context only.

- [ ] **Step 1: Write the failing test**

```tsx
// workspace-switcher.test.tsx
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const switchProject = vi.fn();

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_1",
    slug: "acme",
    name: "Acme",
    projects: [{ id: "p1", slug: "proj", name: "Proj" }],
  }),
  useProject: () => ({ id: "p1", slug: "proj", name: "Proj", switchProject }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "proj" }),
  useSdkClient: () => ({ projects: { create: vi.fn() } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
}));
vi.mock("./project-menu", () => ({
  ProjectAvatar: () => <span data-testid="project-avatar" />,
}));
vi.mock("./brand-gradient-rail", () => ({
  BrandGradientRail: () => <div data-testid="ws-rail" />,
}));

import { WorkspaceSwitcher } from "./workspace-switcher";

afterEach(cleanup);

describe("WorkspaceSwitcher", () => {
  it("shows the project slug and the gradient rail", () => {
    render(<WorkspaceSwitcher />);
    expect(screen.getByText("proj")).toBeTruthy();
    expect(screen.getByTestId("ws-rail")).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm -F dashboard test src/components/workspace-switcher.test.tsx`
Expected: FAIL — cannot resolve `./workspace-switcher`.

- [ ] **Step 3: Write the implementation**

```tsx
// workspace-switcher.tsx
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { ChevronsUpDown, PlusIcon } from "lucide-react";
import { useState } from "react";
import { BrandGradientRail } from "./brand-gradient-rail";
import { InputDialog } from "./input-dialog";
import { ProjectAvatar } from "./project-menu";
import { Button } from "./ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "./ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { CheckIcon } from "lucide-react";

export function WorkspaceSwitcher() {
  const organization = useOrganization();
  const project = useProject();
  const { projectSlug } = useSlugs();
  const client = useSdkClient();
  const [open, setOpen] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");

  const handleProjectSelect = (slug: string) => {
    if (slug === "new-project") {
      setCreateDialogOpen(true);
    } else {
      project.switchProject(slug);
    }
    setOpen(false);
  };

  const createProject = async (name: string) => {
    const result = await client.projects.create({
      createProjectRequestBody: { name, organizationId: organization.id },
    });
    setCreateDialogOpen(false);
    setNewProjectName("");
    project.switchProject(result.project.slug);
  };

  // Org-level pages have no project — render org context only.
  if (!projectSlug) {
    return (
      <div className="relative flex items-center gap-2 overflow-hidden rounded-md border px-2 py-1.5 text-sm font-medium">
        <BrandGradientRail className="absolute top-0 bottom-0 left-0 rounded-none" />
        <span className="truncate">
          {organization.name || organization.slug}
        </span>
      </div>
    );
  }

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="ghost"
            className="relative h-auto w-full justify-start gap-2 overflow-hidden rounded-md border px-2 py-1.5"
          >
            <BrandGradientRail className="absolute top-0 bottom-0 left-0 rounded-none" />
            <ProjectAvatar
              project={project}
              className="h-5 w-5 shrink-0 rounded"
            />
            <span className="truncate text-sm font-medium">
              <span className="text-muted-foreground">
                {organization.slug} /{" "}
              </span>
              {project?.slug || projectSlug}
            </span>
            <ChevronsUpDown className="text-muted-foreground ml-auto h-4 w-4 shrink-0" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[240px] p-0" align="start">
          <Command className="border-none">
            <div className="border-b">
              <CommandInput placeholder="Find Project..." className="h-10" />
            </div>
            <CommandList className="max-h-[250px] !p-1">
              <CommandEmpty>No projects found.</CommandEmpty>
              <CommandGroup heading="Projects">
                {[...organization.projects]
                  .sort((a, b) => a.slug.localeCompare(b.slug))
                  .map((p) => (
                    <CommandItem
                      key={p.id}
                      value={p.slug}
                      onSelect={() => handleProjectSelect(p.slug)}
                      className="flex cursor-pointer items-center gap-2"
                    >
                      <ProjectAvatar
                        project={p}
                        className="h-5 w-5 shrink-0 rounded"
                      />
                      <span className="flex-1 truncate">{p.slug}</span>
                      {p.id === project.id && (
                        <CheckIcon className="h-4 w-4 shrink-0" />
                      )}
                    </CommandItem>
                  ))}
              </CommandGroup>
            </CommandList>
          </Command>
          <button
            onClick={() => handleProjectSelect("new-project")}
            className="hover:bg-accent flex w-full cursor-pointer items-center gap-2 border-t px-3 py-2 text-sm"
          >
            <PlusIcon className="text-muted-foreground h-5 w-5 shrink-0" />
            <span>Create Project</span>
          </button>
        </PopoverContent>
      </Popover>
      {createDialogOpen && (
        <InputDialog
          open={createDialogOpen}
          onOpenChange={() => {
            setCreateDialogOpen(false);
            setNewProjectName("");
          }}
          title="Create New Project"
          description="Create a new project to get started"
          onSubmit={() => createProject(newProjectName)}
          inputs={[
            {
              label: "Name",
              value: newProjectName,
              onChange: setNewProjectName,
              placeholder: "New Project",
            },
          ]}
        />
      )}
    </>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm -F dashboard test src/components/workspace-switcher.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/components/workspace-switcher.tsx client/dashboard/src/components/workspace-switcher.test.tsx
git commit --no-verify -m "feat(dashboard): combined org/project WorkspaceSwitcher for sidebar header"
```

---

## Task 5: `SidebarUserMenu` component

**Files:**

- Create: `client/dashboard/src/components/sidebar-user-menu.tsx`
- Test: `client/dashboard/src/components/sidebar-user-menu.test.tsx`

Ports the avatar `DropdownMenu` from `top-header.tsx:241–334`, opens upward (`side="top"`), adds an inline `ThemeSwitcher` in the footer row, and **replaces "Bug or Feature Request" with "Roadmap" → https://roadmap.speakeasy.com**. Help links (Docs, Changelog, Get Support) live in the menu.

- [ ] **Step 1: Write the failing test**

```tsx
// sidebar-user-menu.test.tsx
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({ displayName: "Sagar", email: "s@x.dev", photoUrl: "" }),
  useIsAdmin: () => false,
  useSession: () => ({ organizations: [{ id: "o1" }] }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ projectSlug: "proj" }),
  useSdkClient: () => ({ auth: { logout: vi.fn() } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ settings: { goTo: vi.fn() } }),
  useOrgRoutes: () => ({
    billing: { goTo: vi.fn() },
    adminSettings: { goTo: vi.fn() },
  }),
}));
vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
}));
vi.mock("@speakeasy-api/moonshine", async (orig) => ({
  ...(await orig<typeof import("@speakeasy-api/moonshine")>()),
  ThemeSwitcher: () => <div data-testid="theme-switcher" />,
}));

import { SidebarUserMenu } from "./sidebar-user-menu";

afterEach(cleanup);

describe("SidebarUserMenu", () => {
  it("renders the inline theme switcher and the user name", () => {
    render(<SidebarUserMenu />);
    expect(screen.getByTestId("theme-switcher")).toBeTruthy();
    expect(screen.getByText("Sagar")).toBeTruthy();
  });

  it("links Roadmap to roadmap.speakeasy.com and has no GitHub issues link", () => {
    render(<SidebarUserMenu />);
    fireEvent.click(screen.getByTestId("user-menu-trigger"));
    const roadmap = screen.getByText("Roadmap").closest("a");
    expect(roadmap?.getAttribute("href")).toBe("https://roadmap.speakeasy.com");
    expect(screen.queryByText(/Bug or Feature Request/)).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm -F dashboard test src/components/sidebar-user-menu.test.tsx`
Expected: FAIL — cannot resolve `./sidebar-user-menu`.

- [ ] **Step 3: Write the implementation**

```tsx
// sidebar-user-menu.tsx
import { useIsAdmin, useSession, useUser } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  ThemeSwitcher,
} from "@speakeasy-api/moonshine";
import {
  ArrowRightLeftIcon,
  BookOpenIcon,
  BuildingIcon,
  CreditCardIcon,
  LifeBuoyIcon,
  LogOutIcon,
  MailIcon,
  MapIcon,
  MoreHorizontal,
  PencilIcon,
  SettingsIcon,
} from "lucide-react";
import { useCallback, useState } from "react";
import { useNavigate } from "react-router";
import { Avatar, AvatarFallback, AvatarImage } from "./ui/avatar";
import { Button } from "./ui/button";

export function SidebarUserMenu() {
  const user = useUser();
  const session = useSession();
  const isAdmin = useIsAdmin();
  const navigate = useNavigate();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const client = useSdkClient();
  const { projectSlug } = useSlugs();
  const { hasAnyScope } = useRBAC();

  const canAccessOrgRoutes = hasAnyScope(["org:read", "org:admin"]);
  const isMultiOrg = session.organizations.length > 1;

  const [pylonOpen, setPylonOpen] = useState(false);
  const togglePylon = useCallback(() => {
    if (pylonOpen) window.Pylon?.("hide");
    else window.Pylon?.("show");
    setPylonOpen((prev) => !prev);
  }, [pylonOpen]);

  const userInitials =
    user.displayName
      ?.split(" ")
      .map((n) => n[0])
      .join("")
      .toUpperCase()
      .slice(0, 2) ||
    user.email?.slice(0, 2).toUpperCase() ||
    "?";

  return (
    <div className="flex items-center gap-1 px-1 py-1">
      <Avatar className="size-7 shrink-0">
        <AvatarImage src={user.photoUrl} alt={user.displayName || user.email} />
        <AvatarFallback className="text-xs">{userInitials}</AvatarFallback>
      </Avatar>
      <span className="truncate text-sm font-medium group-data-[collapsible=icon]:hidden">
        {user.displayName || "User"}
      </span>
      <div className="ml-auto flex items-center gap-0.5 group-data-[collapsible=icon]:hidden">
        <ThemeSwitcher />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              data-testid="user-menu-trigger"
              variant="ghost"
              size="icon"
              className="size-7"
            >
              <MoreHorizontal className="h-4 w-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" align="end" className="w-56">
            <DropdownMenuLabel className="font-normal">
              <div className="flex items-start justify-between gap-2">
                <div className="flex flex-col space-y-1">
                  <p className="text-sm leading-none font-medium">
                    {user.displayName || "User"}
                  </p>
                  <p className="text-muted-foreground text-xs leading-none">
                    {user.email}
                  </p>
                </div>
                {projectSlug && (
                  <button
                    type="button"
                    aria-label="Project Settings"
                    onClick={() => routes.settings.goTo()}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <SettingsIcon className="h-4 w-4" />
                  </button>
                )}
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              {projectSlug && (
                <DropdownMenuItem onClick={() => routes.settings.goTo()}>
                  <SettingsIcon className="mr-2 h-4 w-4" />
                  Project Settings
                </DropdownMenuItem>
              )}
              {canAccessOrgRoutes && (
                <DropdownMenuItem onClick={() => orgRoutes.billing.goTo()}>
                  <CreditCardIcon className="mr-2 h-4 w-4" />
                  Billing
                </DropdownMenuItem>
              )}
              {isAdmin && (
                <DropdownMenuItem
                  onClick={() => orgRoutes.adminSettings.goTo()}
                >
                  <ArrowRightLeftIcon className="mr-2 h-4 w-4" />
                  Organization Override
                </DropdownMenuItem>
              )}
              {isMultiOrg && (
                <DropdownMenuItem onClick={() => navigate("/switch-org")}>
                  <BuildingIcon className="mr-2 h-4 w-4" />
                  Switch Organization
                </DropdownMenuItem>
              )}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem asChild>
                <a
                  href="https://www.speakeasy.com/docs/mcp"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <BookOpenIcon className="mr-2 h-4 w-4" />
                  Docs
                </a>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <a
                  href="https://www.speakeasy.com/changelog?product=mcp-platform"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <PencilIcon className="mr-2 h-4 w-4" />
                  Changelog
                </a>
              </DropdownMenuItem>
              {"Pylon" in window && (
                <DropdownMenuItem onClick={togglePylon}>
                  <LifeBuoyIcon className="mr-2 h-4 w-4" />
                  {pylonOpen ? "Close Support" : "Get Support"}
                </DropdownMenuItem>
              )}
              <DropdownMenuItem asChild>
                <a href="mailto:gram@speakeasy.com">
                  <MailIcon className="mr-2 h-4 w-4" />
                  Email Team
                </a>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <a
                  href="https://roadmap.speakeasy.com"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <MapIcon className="mr-2 h-4 w-4" />
                  Roadmap
                </a>
              </DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={async () => {
                await client.auth.logout();
                window.location.href = "/login";
              }}
            >
              <LogOutIcon className="mr-2 h-4 w-4" />
              Log out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm -F dashboard test src/components/sidebar-user-menu.test.tsx`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/components/sidebar-user-menu.tsx client/dashboard/src/components/sidebar-user-menu.test.tsx
git commit --no-verify -m "feat(dashboard): sidebar footer user menu (Roadmap replaces bug link)"
```

---

## Task 6: Wire header + footer into `AppSidebar`

**Files:**

- Modify: `client/dashboard/src/components/app-sidebar.tsx`

- [ ] **Step 1: Update imports**

Add to the `@/components/ui/sidebar` import (currently `app-sidebar.tsx:7-13`): `SidebarHeader`. Add new imports:

```tsx
import { GramLogo } from "./gram-logo";
import { WorkspaceSwitcher } from "./workspace-switcher";
import { SidebarUserMenu } from "./sidebar-user-menu";
```

- [ ] **Step 2: Add the `SidebarHeader` block**

Immediately inside `<Sidebar collapsible="icon" {...props}>` (before `<SidebarContent className="pt-5">`, `app-sidebar.tsx:146-147`):

```tsx
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <Link
          to={routes.home.href()}
          className="flex items-center px-1 hover:no-underline group-data-[collapsible=icon]:justify-center"
        >
          <GramLogo className="w-24 group-data-[collapsible=icon]:hidden" />
        </Link>
        <WorkspaceSwitcher />
      </SidebarHeader>
      <SidebarContent className="pt-2">
```

> Note: change `pt-5` → `pt-2` since the header now occupies the top. `Link` is already imported (`app-sidebar.tsx:25`).

- [ ] **Step 3: Pass `active` to each `CollapsibleNavGroup`**

The component already computes `connectActive`, `buildActive`, `observeActive`, `securityActive` (`app-sidebar.tsx:79-101`). Pass them:

```tsx
            <CollapsibleNavGroup label="Connect" Icon={(p) => <Icon {...p} name="plug" />} defaultHref={routes.sources.href()} active={connectActive}>
```

```tsx
            <CollapsibleNavGroup label="Build" Icon={(p) => <Icon {...p} name="hammer" />} defaultHref={routes.mcp.href()} active={buildActive}>
```

```tsx
            <CollapsibleNavGroup label="Observe" Icon={(p) => <Icon {...p} name="eye" />} defaultHref={routes.insights.href()} active={observeActive}>
```

```tsx
            <CollapsibleNavGroup label="Secure" Icon={(p) => <Icon {...p} name="shield" />} defaultHref={routes.riskOverview.href()} stage="beta" active={securityActive}>
```

(keep each group's existing children unchanged.)

- [ ] **Step 4: Add the user menu to the footer**

Replace the `SidebarFooter` block (`app-sidebar.tsx:268-270`) so it contains the user menu above the free-tier notification:

```tsx
<SidebarFooter className="border-t">
  <FreeTierExceededNotification />
  <SidebarUserMenu />
</SidebarFooter>
```

> Remove the `group-data-[collapsible=icon]:hidden` on `SidebarFooter` — the avatar should remain visible when collapsed (the user menu hides its own labels via `group-data-[collapsible=icon]:hidden` internally).

- [ ] **Step 5: Verify type-check + tests**

Run: `pnpm -F dashboard type-check`
Expected: PASS.
Run: `pnpm -F dashboard test src/components/nav-menu.test.tsx`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add client/dashboard/src/components/app-sidebar.tsx
git commit --no-verify -m "feat(dashboard): mount header + user menu in AppSidebar"
```

---

## Task 7: Wire header + footer into `OrgSidebar`

**Files:**

- Modify: `client/dashboard/src/components/org-sidebar.tsx`

- [ ] **Step 1: Read `org-sidebar.tsx`**

Run: confirm its structure mirrors `app-sidebar.tsx` (a `<Sidebar collapsible="icon">` with `SidebarContent` of org nav groups and a footer).

- [ ] **Step 2: Apply the same header + footer**

Add the same imports (`SidebarHeader`, `GramLogo`, `WorkspaceSwitcher`, `SidebarUserMenu`) and insert, immediately inside `<Sidebar …>`:

```tsx
<SidebarHeader>
  <Link
    to={`/${orgSlug}`}
    className="flex items-center px-1 hover:no-underline group-data-[collapsible=icon]:justify-center"
  >
    <GramLogo className="w-24 group-data-[collapsible=icon]:hidden" />
  </Link>
  <WorkspaceSwitcher />
</SidebarHeader>
```

> On org pages `WorkspaceSwitcher` auto-renders org-only (its `!projectSlug` branch). Use the org-sidebar's existing slug source for the logo link (`orgSlug` from `useSlugs()`; add the hook if not already present).

Add a footer with the user menu (create one if `OrgSidebar` lacks a `SidebarFooter`):

```tsx
<SidebarFooter className="border-t">
  <SidebarUserMenu />
</SidebarFooter>
```

Pass `active={…}` to each org `CollapsibleNavGroup` using the org-sidebar's existing active-group computation (mirror Task 6, Step 3, for whatever groups org-sidebar defines).

- [ ] **Step 3: Verify type-check**

Run: `pnpm -F dashboard type-check`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add client/dashboard/src/components/org-sidebar.tsx
git commit --no-verify -m "feat(dashboard): mount header + user menu in OrgSidebar"
```

---

## Task 8: `Page.Header.Actions` slot

**Files:**

- Modify: `client/dashboard/src/components/page-header.tsx`
- Test: `client/dashboard/src/components/page-header.test.tsx` (create)

- [ ] **Step 1: Write the failing test**

```tsx
// page-header.test.tsx
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/insights-sidebar", () => ({
  InsightsTrigger: () => <div data-testid="insights-trigger" />,
}));
vi.mock("@/components/ui/sidebar", () => ({
  SidebarTrigger: () => <button data-testid="sidebar-trigger" />,
}));

import { PageHeader } from "./page-header";

afterEach(cleanup);

describe("PageHeader.Actions", () => {
  it("renders action children in the toolbar", () => {
    render(
      <PageHeader>
        <PageHeader.Actions>
          <button data-testid="page-action">New</button>
        </PageHeader.Actions>
      </PageHeader>,
    );
    expect(screen.getByTestId("page-action")).toBeTruthy();
  });
});
```

> If `page-header.tsx` imports anything else that breaks in jsdom (e.g. router hooks via `Breadcrumbs`), the `PageHeaderComponent` itself only uses `SidebarTrigger`, `Separator`, and `InsightsTrigger` — all either trivial or mocked above. Add a `vi.mock` for `@/components/ui/separator` returning a passthrough if needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm -F dashboard test src/components/page-header.test.tsx`
Expected: FAIL — `PageHeader.Actions` is undefined.

- [ ] **Step 3: Add the `Actions` sub-component**

In `page-header.tsx`, add the component:

```tsx
function PageHeaderActions({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={cn("ml-auto flex shrink-0 items-center gap-2", className)}>
      {children}
    </div>
  );
}
```

Wire it into the toolbar so actions sit just left of the Insights trigger. Update `PageHeaderComponent`'s inner row — the Insights trigger keeps `ml-auto`, and `PageHeaderActions` provides its own `ml-auto`, so whichever appears pushes to the right; render `{children}` (breadcrumbs + any `<Page.Header.Actions>`) then the trigger:

```tsx
<div className="flex w-full items-center gap-3 px-3">
  <SidebarTrigger className="mx-0 -ml-1 px-0" />
  <Separator
    orientation="vertical"
    className="data-[orientation=vertical]:h-4"
  />
  {children}
  <InsightsTrigger className="ml-auto shrink-0" />
</div>
```

> No structural change is needed beyond adding the component — pages render `<Page.Header.Actions>` as a sibling of `<Page.Header.Breadcrumbs>` inside `<Page.Header>`. The `ml-auto` on `PageHeaderActions` right-aligns it; `InsightsTrigger` stays at the far right.

Extend the export:

```tsx
export const PageHeader = Object.assign(PageHeaderComponent, {
  Title: PageHeaderTitle,
  Breadcrumbs: PageHeaderBreadcrumbs,
  Actions: PageHeaderActions,
});
```

- [ ] **Step 4: Surface the slot through `Page` in `page-layout.tsx`**

`page-layout.tsx` composes the `Page` object that pages use. Find where `Page.Header` is assigned `PageHeader` (it re-exports `PageHeader` with `Breadcrumbs`). Ensure `Page.Header.Actions` resolves — since `Page.Header` IS `PageHeader`, `Actions` is already attached by the `Object.assign` above. No change needed unless `page-layout.tsx` rebuilds a narrower header object; if it does, add `Actions: PageHeader.Actions` there.

- [ ] **Step 5: Run test to verify it passes**

Run: `pnpm -F dashboard test src/components/page-header.test.tsx`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add client/dashboard/src/components/page-header.tsx client/dashboard/src/components/page-header.test.tsx
git commit --no-verify -m "feat(dashboard): add Page.Header.Actions toolbar slot"
```

---

## Task 9: Remove the top bar from the layout shell

**Files:**

- Modify: `client/dashboard/src/components/app-layout.tsx`

- [ ] **Step 1: Restructure `AppLayoutContent`**

Replace the body of `AppLayoutContent` (`app-layout.tsx:99-121`) — drop `<TopHeader/>` and `<BrandGradientLine/>`, make the row the whole app (banner stays above it):

```tsx
const AppLayoutContent = ({
  isImpersonating,
}: {
  isImpersonating: boolean;
}) => {
  return (
    <div className="flex h-screen w-full flex-col">
      {isImpersonating && <ImpersonationBanner />}
      <div className="flex w-full flex-1 overflow-hidden">
        <AppSidebar variant="inset" />
        <SidebarInset>
          <GlobalInsightsWrapper>
            <MembershipSyncGuard>
              <Outlet />
            </MembershipSyncGuard>
            <Modal
              closable
              className="h-full max-h-[450px] min-h-auto w-9/12 max-w-[1100px] min-w-auto rounded-sm p-0 2xl:w-2/3 2xl:max-w-[1000px]"
              layout="custom"
            />
          </GlobalInsightsWrapper>
        </SidebarInset>
      </div>
    </div>
  );
};
```

- [ ] **Step 2: Fix the CSS vars in `AppLayout`**

`--header-offset` now represents only the banner height. Update `AppLayout` (`app-layout.tsx:40-52`):

```tsx
    <SidebarProvider
      style={
        {
          "--sidebar-width": "14rem",
          "--header-offset": isImpersonating ? "2.25rem" : "0px",
          ...(isImpersonating ? { "--banner-offset": "2.25rem" } : undefined),
        } as React.CSSProperties
      }
    >
```

Apply the identical change to `OrgLayout`'s `SidebarProvider` (`app-layout.tsx:207-218`) and remove `<TopHeader/>` + `<BrandGradientLine/>` and the `pt-2` from its inline JSX (`app-layout.tsx:221-233`), matching Step 1's structure (banner above the row).

- [ ] **Step 3: Remove now-unused imports**

Delete the `TopHeader` and `BrandGradientLine` imports (`app-layout.tsx:10,13`).

- [ ] **Step 4: Update the default offset in `ui/sidebar.tsx`**

Change the default `--header-offset` from `3.5rem` to `0px` (`ui/sidebar.tsx:107`) so the fixed sidebar (`top-(--header-offset)`, line 201) starts at the viewport top when not impersonating:

```tsx
            "--header-offset": "0px",
```

- [ ] **Step 5: Verify type-check + build**

Run: `pnpm -F dashboard type-check`
Expected: PASS (no references to removed imports).
Run: `pnpm -F dashboard build`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add client/dashboard/src/components/app-layout.tsx client/dashboard/src/components/ui/sidebar.tsx
git commit --no-verify -m "feat(dashboard): remove top bar; sidebar-only vertical split"
```

---

## Task 10: Recompute page height & delete `top-header.tsx`

**Files:**

- Modify: `client/dashboard/src/components/page-layout.tsx:24`
- Delete: `client/dashboard/src/components/top-header.tsx`

- [ ] **Step 1: Recompute the `PageLayout` height**

The old `calc(100vh-5rem-…)` subtracted TopHeader (3.5rem) + content `pt-2` (0.5rem) + inset gutter (1rem). With the top bar and `pt-2` gone, only the SidebarInset gutter remains (~1rem). Update `page-layout.tsx:24` and its comment:

```tsx
    // Height accounts for the SidebarInset visual gutter (m-2 top+bottom = 1rem)
    // and the impersonation banner via --banner-offset. The top bar is gone, so
    // there's no header/pt-2 term to subtract.
    <div className="flex h-[calc(100vh-1rem-var(--banner-offset,0px))] flex-col overflow-hidden">
```

- [ ] **Step 2: Confirm no remaining importers of `TopHeader`**

Run: `grep -rn "top-header\|TopHeader" client/dashboard/src`
Expected: no matches (Task 9 removed the import). If any remain, resolve them before deleting.

- [ ] **Step 3: Delete the file**

Run: `git rm client/dashboard/src/components/top-header.tsx`

- [ ] **Step 4: Verify type-check + build**

Run: `pnpm -F dashboard type-check && pnpm -F dashboard build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A client/dashboard/src/components/page-layout.tsx
git commit --no-verify -m "refactor(dashboard): recompute page height; delete dead TopHeader"
```

---

## Task 11: Full verification & manual QA

**Files:** none (verification only)

- [ ] **Step 1: Run the full dashboard gate**

Run: `pnpm -F dashboard lint:format` (then commit any formatting changes with `--no-verify`)
Run: `pnpm -F dashboard type-check`
Run: `pnpm -F dashboard test`
Run: `pnpm -F dashboard build`
Expected: all PASS.

- [ ] **Step 2: Manual QA in dev**

Run: `pnpm -F dashboard dev` and verify:

- No top bar; single vertical split.
- Sidebar header: logo links home; switcher shows `org / project` with the gradient rail; switching project works; "Create Project" opens the dialog; org-level pages show org-only switcher.
- Active nav: gradient left rail on the active leaf (expanded) and on the active group icon (collapsed via Cmd+B); hover pill stays neutral; no double rails.
- Footer: avatar + name; inline theme toggle flips theme; ⋯ menu opens upward with Project Settings / Billing / Org Override / Switch Org (per RBAC) / Docs / Changelog / Get Support / Email / **Roadmap → roadmap.speakeasy.com** / Log Out; no "Bug or Feature Request" item.
- Collapsed sidebar: header → avatar/project-avatar only; footer → avatar only; menu still opens.
- Org-level pages (Team/Billing/Audit Logs): correct sidebar, header, footer.
- Impersonation banner (if reproducible): full-width above the split; sidebar starts below it.
- A `fullHeight` page (Playground chat) does not clip below the fold; the composer is visible.
- Mobile width: sidebar collapses to the sheet; trigger in the toolbar opens it with header + footer inside.

- [ ] **Step 3: Final commit (if formatting changed anything)**

```bash
git add -A client/dashboard
git commit --no-verify -m "chore(dashboard): formatting for sidebar-only layout"
```

---

## Self-Review

**Spec coverage:**

- Shell restructure (remove top bar) → Task 9. ✓
- SidebarHeader + combined switcher + gradient rail → Tasks 3, 4, 6, 7. ✓
- Active-nav gradient rail (expanded + collapsed) → Task 2. ✓
- Footer user menu (theme inline, help links folded, Roadmap replaces bug link) → Tasks 5, 6, 7. ✓
- `Page.Header.Actions` slot → Task 8. ✓
- CSS var / height recompute → Tasks 9, 10. ✓
- Delete `top-header.tsx` → Task 10. ✓
- Edge cases (collapsed, mobile, impersonation, fullHeight) → Task 11 manual QA. ✓
- Out of scope (page-action migration, sidebar search) → not in this plan, by design. ✓

**Type consistency:** `BrandGradientRail` (Task 1) is consumed with the same prop (`className`) in Tasks 2 & 4. `WorkspaceSwitcher` / `SidebarUserMenu` (Tasks 4, 5) are imported with matching names in Tasks 6, 7. `CollapsibleNavGroup`'s new `active?: boolean` prop (Task 2) is supplied in Tasks 6, 7. `PageHeader.Actions` (Task 8) matches its test usage.

**Placeholder scan:** No TBD/TODO; every code step shows complete code; commands have expected output. Task 7 references the org-sidebar's existing active-group computation rather than reproducing unseen code — the implementer reads `org-sidebar.tsx` in Step 1 and mirrors Task 6's pattern, which is fully specified.
