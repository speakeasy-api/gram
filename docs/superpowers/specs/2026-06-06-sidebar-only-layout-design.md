# Sidebar-only app layout — design spec

**Date:** 2026-06-06
**Status:** Approved direction; spec under review
**Scope:** Dashboard frontend (`client/dashboard`) only. No backend changes.

## Summary

Replace the dashboard's two-part chrome (full-width **top bar** + left **sidebar**) with a
single **vertical split**: a left sidebar and a main content area, no top bar. All
functionality currently in the top bar is preserved, relocated into the sidebar
(header + footer) and the main-section toolbar. The Speakeasy brand gradient — currently a
horizontal line under the top bar — is repurposed as a product accent on the workspace
switcher and the active nav item.

## Sequencing (two PRs)

The end state (chosen by the user) includes a top-of-main **actions slot** that pages
migrate their primary buttons into. To keep the structural change reviewable, this ships
in two PRs:

- **PR 1 — Shell redesign (THIS spec).** Remove the top bar; rebuild the sidebar
  (header + footer); add gradient accents; add the _empty_ `Page.Header.Actions` slot to
  the main toolbar. No per-page content edits.
- **PR 2 — Action migration (separate spec + plan, built on PR 1).** Move each page's
  primary action from the inline `Page.Section.CTA` into `Page.Header.Actions`,
  page by page. Mechanical; ~30 page files.

This matches the repo convention of isolating large mechanical migrations from feature
changes.

## Decisions captured during brainstorming

| Topic                      | Decision                                                                                                                                                                                   |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Overall shell              | Sidebar-only vertical split; no top bar.                                                                                                                                                   |
| Top-bar chrome destination | Workspace switcher + logo → sidebar **header**; user menu + theme + help links → sidebar **footer**.                                                                                       |
| Workspace switcher         | **Combined** "org / project" control (Vercel-style) with a grouped popover. Collapses to org-only on org-level pages.                                                                      |
| Brand gradient             | Repurposed as **Option A — left rail**: a 3px vertical gradient bar on the left edge of (a) the workspace switcher box and (b) the active nav item. Hover keeps the existing neutral pill. |
| Footer user menu           | Vercel-style: footer row = avatar + name + inline theme toggle + ⋯ button. ⋯ opens an upward menu carrying all of today's avatar-dropdown items + Docs/Changelog/Support.                  |
| Help links                 | Folded into the user menu (Docs, Changelog, Get Support).                                                                                                                                  |
| Roadmap                    | Replace today's "Bug or Feature Request" (→ GitHub issues) with **"Roadmap" → https://roadmap.speakeasy.com**.                                                                             |
| Theme toggle               | Inline in the footer (visible), next to the user row.                                                                                                                                      |
| Sidebar search             | Out of scope. Existing Cmd+K command palette unchanged.                                                                                                                                    |

## Current architecture (before)

`AppLayoutContent` (`components/app-layout.tsx:94`) is a vertical column:

```
column[
  {impersonation banner?}        // full-width
  <TopHeader/>                    // h-14, full-width: logo, org/proj switchers, Docs/Changelog/Support, theme, avatar menu
  <BrandGradientLine/>            // 3px horizontal gradient
  row[ <AppSidebar variant=inset/> , <SidebarInset> … <Outlet/> … </SidebarInset> ]   // pt-2
]
```

Relevant load-bearing details:

- `Sidebar` is `position: fixed; top: var(--header-offset)` (`ui/sidebar.tsx:201`).
  `--header-offset` = `3.5rem` default (`ui/sidebar.tsx:107`), overridden to `5.75rem` when
  impersonating (`app-layout.tsx:46,213`). It exists solely to clear the top header.
- `page-layout.tsx:24` sizes the page as `h-[calc(100vh-5rem-var(--banner-offset,0px))]`,
  where `5rem` = TopHeader (3.5) + content-wrapper `pt-2` (0.5) + SidebarInset gutter `m-2` (1).
- Active nav highlight is a sliding `motion.div` pill (`bg-card` + `ring-border/50`) that
  follows `hoveredItem ?? activeItem` (`nav-menu.tsx:124,209`).
- The sidebar has no header today; it has a `mt-auto` "Back to org" link
  (`app-sidebar.tsx:255`) and a `SidebarFooter` with `FreeTierExceededNotification`.
- `TopHeader` (`top-header.tsx`) holds the project switcher Popover+Command
  (lines 146–203) and the avatar `DropdownMenu` (lines 241–334) — both reused below.

## Target architecture (after, PR 1)

```
column[
  {impersonation banner?}        // full-width, the one exception above the split
  row[ <AppSidebar/> , <SidebarInset> … <Outlet/> … </SidebarInset> ]
]
```

`AppSidebar` (and `OrgSidebar`) gain a header and a richer footer:

```
<Sidebar collapsible="icon">
  <SidebarHeader>           // NEW
    <GramLogo/>
    <WorkspaceSwitcher/>    // combined org/project, gradient left rail
  </SidebarHeader>
  <SidebarContent>
    …nav (active item gets gradient left rail)…
    "Back to org"           // project sidebar only; kept
  </SidebarContent>
  <SidebarFooter>           // REVISED
    <FreeTierExceededNotification/>
    <SidebarUserMenu/>      // avatar + name + theme toggle + ⋯ menu
  </SidebarFooter>
</Sidebar>
```

## Components

### 1. Layout shell — `components/app-layout.tsx`

- Delete `<TopHeader/>` and `<BrandGradientLine/>` from `AppLayoutContent` and `OrgLayout`.
- Restructure to `column[ banner?, row[sidebar, inset] ]`; the row is `flex-1 overflow-hidden`
  (drop the `pt-2`).
- CSS vars: `--header-offset` now represents only the banner height.
  - Non-impersonating: `0px`.
  - Impersonating: `2.25rem` (banner). Keep `--banner-offset: 2.25rem` for `page-layout`.
- `GlobalInsightsWrapper`, `MembershipSyncGuard`, `ModalProvider`, impersonation logic:
  unchanged.

### 2. Sidebar primitive — `components/ui/sidebar.tsx`

- Add a `SidebarHeader` component mirroring `SidebarFooter` (`ui/sidebar.tsx:270`); export it.
- Change the fixed sidebar offset default from `3.5rem` to `0px` (line 107) so the sidebar
  starts at the top of the viewport (or below the banner when impersonating, via
  `--header-offset`). Verify `top-(--header-offset)` (line 201) resolves correctly in both
  states.

### 3. Workspace switcher — `components/workspace-switcher.tsx` (NEW)

- Combined control rendering `{orgSlug} / {projectSlug}` with the project avatar and a
  chevron; clicking opens the existing Popover+Command project list (ported from
  `top-header.tsx:146–203`, including "Create Project" via `InputDialog`).
- Org switching: if `isMultiOrg`, the popover includes a "Switch Organization" affordance
  (→ `/switch-org`); org slug segment links to `/${orgSlug}` when the user
  `hasAnyScope(["org:read","org:admin"])` (same gate as today, `top-header.tsx:127`).
- **Gradient left rail** (Option A): a 3px vertical bar
  (`linear-gradient(180deg, var(--gradient-brand-primary-colors))`) on the box's left edge.
- Org-level usage (`OrgSidebar`): render org context only (no project segment / avatar).
- Collapsed sidebar: render just the project avatar + rail; popover still opens.

### 4. Active-nav gradient rail — `components/nav-menu.tsx`

- Add a 3px vertical gradient bar to the **active** element, attached directly via the
  existing active flags (`NavButton.active`, `CollapsibleNavGroup.isOpen`,
  `CollapsibleNavItem.item.active`) as an absolutely-positioned child on the left edge —
  **not** via the sliding `motion.div` (which continues to track hover only).
- This preserves the collapsed-mode fallback (group icon shows active when sub-items hide),
  because it keys off the same flags the component already computes.
- Extract the gradient value into a shared helper/variant so the switcher and the rail use
  one source (e.g. a `BrandGradientRail` component or a shared className in
  `brand-gradient-line.tsx`). Keep `BrandGradientLine` itself (still used by
  `pages/org/OrgHome.tsx:1017`).

### 5. Sidebar footer user menu — `components/sidebar-user-menu.tsx` (NEW)

- Footer row: avatar + display name + inline `ThemeSwitcher` + a `⋯` trigger.
- `⋯` opens a `DropdownMenu` with `side="top"` carrying every item from today's avatar
  dropdown (`top-header.tsx:258–333`), with `Roadmap` replacing `Bug or Feature Request`:
  - Header: name + email + a gear → Project Settings.
  - Project Settings (project pages), Billing (`org:read`/`org:admin`), Organization
    Override (admin), Switch Organization (multi-org).
  - Docs (`speakeasy.com/docs/mcp`), Changelog
    (`speakeasy.com/changelog?product=mcp-platform`), Get Support (Pylon toggle), Email Team
    (`mailto:gram@speakeasy.com`), **Roadmap (`https://roadmap.speakeasy.com`, new tab)**.
  - Log Out (`client.auth.logout()` → `/login`).
- All RBAC/feature gates identical to current behavior.
- Collapsed sidebar: render avatar only; same menu on click.

### 6. App / org sidebars — `components/app-sidebar.tsx`, `components/org-sidebar.tsx`

- Add `<SidebarHeader>` with `GramLogo` + `<WorkspaceSwitcher/>`.
- Replace the footer contents with `<SidebarUserMenu/>` (keep `FreeTierExceededNotification`).
- `OrgSidebar`: same header/footer, org-context switcher, org nav unchanged.
- Keep the project sidebar's "Back to org" link.

### 7. Main toolbar actions slot — `components/page-header.tsx`

- Add `PageHeader.Actions`: a right-aligned slot rendered in `PageHeaderComponent` between
  the breadcrumbs (`{children}`) and the `InsightsTrigger`. Final order:
  `SidebarTrigger | Separator | breadcrumbs … [Actions] [Insights]`.
- Empty in PR 1; pages adopt `<Page.Header.Actions>` in PR 2.
- `SidebarTrigger` (collapse toggle, Cmd+B) stays in the toolbar — it is now the only
  collapse affordance.

### 8. Delete `components/top-header.tsx`

- Removed after its switcher/avatar logic is ported into the components above. Confirm no
  other importers (only `app-layout.tsx` today).

## Edge cases

- **Impersonation banner:** stays full-width above the split; sidebar offset driven by
  `--header-offset` = `2.25rem` while impersonating.
- **Collapsed sidebar (Cmd+B / `collapsible=icon`):** header → project avatar + rail;
  footer → avatar only; active group icon keeps the gradient rail.
- **Mobile:** the sidebar's existing mobile sheet (offcanvas) now contains the header and
  footer; `SidebarTrigger` in the toolbar opens it.
- **`page-layout.tsx` height calc:** recompute `calc(100vh - 5rem - …)` → remove the
  3.5rem + 0.5rem top-header terms (becomes ~`calc(100vh - 1.5rem - var(--banner-offset,0px))`,
  i.e. just the SidebarInset gutter). **Verify in-browser** that `fullHeight` pages (e.g.
  Playground chat) don't overflow or clip.
- **`OrgHome.tsx` BrandGradientLine:** untouched — that's a separate decorative usage.

## Testing

- `pnpm -F dashboard type-check` and `pnpm -F dashboard build` pass.
- Manual pass in `pnpm -F dashboard dev`:
  - Sidebar header switcher: switch project, create project, org/switch-org, gradient rail.
  - Active nav rail correct on every group + top-level item; hover pill still neutral.
  - Footer: theme toggle, ⋯ menu items + RBAC gating, Roadmap link, Docs/Changelog/Support,
    Log Out.
  - Collapse (Cmd+B), org-level pages, impersonation banner, mobile sheet.
  - `fullHeight` page (Playground) does not clip below the fold.
- No backend, no generated code, no migrations.

## Out of scope

- PR 2 page-action migration into `Page.Header.Actions`.
- Sidebar search UI.
- Any change to nav information architecture (groups/items unchanged).
- "Platform status" strip (Vercel reference only; we have no equivalent data source).
