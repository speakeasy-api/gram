# Overlays and links

## Tooltip usage

`App.tsx` wraps the entire app in a global `TooltipProvider`. **Never add another `TooltipProvider` inside a component** — doing so creates a redundant Radix context per instance and contributes to `ResizeObserver loop completed with undelivered notifications` errors in the browser.

Use `<Tooltip>`, `<TooltipTrigger>`, and `<TooltipContent>` directly — they inherit the global provider automatically. For simple cases use the existing `<SimpleTooltip tooltip="...">` wrapper from `@/components/ui/Tooltip`.

```tsx
// ✅ correct
<Tooltip>
  <TooltipTrigger asChild>{button}</TooltipTrigger>
  <TooltipContent>Hello</TooltipContent>
</Tooltip>

// ❌ wrong — TooltipProvider already exists at the app root
<TooltipProvider>
  <Tooltip>
    <TooltipTrigger asChild>{button}</TooltipTrigger>
    <TooltipContent>Hello</TooltipContent>
  </Tooltip>
</TooltipProvider>
```

## Navigation and links

Use the right primitive for the link type — mixing them causes full-page reloads, broken multi-tenancy, or missing security headers.

**Internal navigation (any URL inside the dashboard):** Get the route helpers from `const routes = useRoutes()` on project pages or `const routes = useOrgRoutes()` on org pages (both from `@/routes`). Use the route helpers from `client/dashboard/src/routes.tsx`. Top-level routes _and_ subpages expose `.Link`, `.href()`, and `.goTo()`:

```tsx
// Wrap a child node with .Link
<routes.plugins.Link>
  <Button>Set up a plugin</Button>
</routes.plugins.Link>

// Subpages get .Link too — use it instead of building strings
<routes.mcp.deployments.Link>
  <Button>View deployments</Button>
</routes.mcp.deployments.Link>

// Plain react-router Link with .href() when you need a className or are inside <p>
<Link to={routes.plugins.href()} className="underline underline-offset-2">
  Observability plugin
</Link>
```

Never hardcode org/project slugs in an `href` (e.g. `https://app.getgram.ai/speakeasy-team/projects/default/plugins`). The route helpers resolve the current `:orgSlug` / `:projectSlug` from the URL, so the same call works for every tenant.

**External links (anywhere outside the dashboard):** Use a plain `<a>` with `target="_blank"` and `rel="noopener noreferrer"`. This matches the existing pattern (`AddServerDialog.tsx`, `CatalogDetail.tsx`) and the security attributes are mandatory — `noopener` blocks `window.opener` access; `noreferrer` strips the Referer header.

```tsx
<a
  href="https://www.speakeasy.com/product/mcp-gateway/catalog"
  target="_blank"
  rel="noopener noreferrer"
  className="underline underline-offset-2 hover:text-foreground"
>
  MCP Registry
</a>
```

The `Link` from `@/components/ui/Link` is a styled `<a>` with optional `iconPrefixName` / `iconSuffixName`. It does not set `target` or `rel`, so pass `target="_blank" rel="noopener noreferrer"` yourself when you use it for an external URL. Reach for the plain `<a>` for inline external links inside subtext.

## Sheet (side panel) usage

Side panels ("drawers") use `Sheet` from `@/components/ui/Sheet`. `SheetContent` requires `SheetTitle` (and optionally `SheetDescription`) inside it. Omitting them generates a console error and breaks screen reader accessibility. `SheetHeader` and `SheetFooter` are optional layout wrappers.

```tsx
<Sheet open={open} onOpenChange={setOpen}>
  <SheetContent>
    <SheetHeader>
      <SheetTitle>Session Details</SheetTitle>
      <SheetDescription>Viewing trace for this chat.</SheetDescription>
    </SheetHeader>
    {/* content */}
  </SheetContent>
</Sheet>
```

If the title is visually redundant, hide it from sighted users while keeping it for screen readers:

```tsx
<SheetTitle className="sr-only">Details</SheetTitle>
```

## Dialog component usage

`Dialog` from `@/components/ui/Dialog` is a compound component. `Dialog.Content` requires `Dialog.Title` (and optionally `Dialog.Description`) inside it. Omitting them generates a console error and breaks screen reader accessibility. `Dialog.Header` and `Dialog.Footer` are optional layout wrappers.

```tsx
<Dialog>
  <Dialog.Trigger>Open</Dialog.Trigger>
  <Dialog.Content>
    <Dialog.Header>
      <Dialog.Title>Rename server</Dialog.Title>
      <Dialog.Description>The new name shows everywhere.</Dialog.Description>
    </Dialog.Header>
    {/* content */}
  </Dialog.Content>
</Dialog>
```

If the title is visually redundant, hide it from sighted users while keeping it for screen readers:

```tsx
<Dialog.Title className="sr-only">Details</Dialog.Title>
```

### Confirm before acting: use `ConfirmDialog`

To ask "are you sure?" before an action, use `ConfirmDialog` from `@/components/ui/ConfirmDialog`. Never hand-roll a confirm from raw `Dialog` parts. It handles:

- `isPending` — locks the dialog while the action runs: it cannot close, Cancel is disabled, and the button reads "Working…".
- `error` — an inline `Alert` above the footer, for a refusal the user must read. A toast is wrong here: it vanishes while the dialog stays open.
- `impact` — a server-sourced summary (`summary`, optional `mcpServerNames` + `namesLabel`, `isLoading`) of what the action affects. Never confirm a destructive action against a client-side estimate.
- `confirmVariant` — `"destructive-primary"` (default) or `"primary"` for a safe step that still needs a pause.
- `confirmDisabled` — blocks the action when a preflight predicts a refusal.

```tsx
<ConfirmDialog
  open={open}
  onOpenChange={setOpen}
  title="Delete key"
  description="Clients using this key stop working at once."
  confirmLabel="Delete key"
  onConfirm={() => deleteKey.mutate()}
  isPending={deleteKey.isPending}
  error={deleteError}
/>
```
