---
name: admin-shadcn
description: 'Use when adding, changing, or styling UI in client/admin (the Gram admin dashboard) that touches shadcn/ui — a button, dialog, table, sidebar, badge, select, tabs, tooltip, card, sheet, or any file under client/admin/src/components/ui/. Triggers include "add a shadcn component", "install shadcn X", "npx shadcn add", "restyle this button", "change the dialog", "that variant does not exist", any edit to client/admin/src/components/ui/, and the errors "No components.json was found" or "sh: 1: oxfmt: not found".'
metadata:
  relevant_files:
    - "client/admin/**"
---

# shadcn in client/admin

`client/admin/src/components/ui/**` is **vendored**. The shadcn CLI owns every file in it. Two rules follow:

1. **Get components from the CLI.** Never hand-write a `ui/` file. Never paste one from ui.shadcn.com.
2. **Never edit a `ui/` file.** Customize by composition. If a change looks impossible without editing `ui/`, stop and ask the user.

`client/dashboard` does not obey rule 2 today. Its `ui/` directory holds custom files. Do not copy that habit into `client/admin`.

## Commands

Run every shadcn command from `client/admin`. The CLI reads the nearest `components.json`. From the repo root it resolves the wrong package.

| Goal                    | Command                                       |
| ----------------------- | --------------------------------------------- |
| Add a component         | `aube dlx shadcn@latest add <name>`           |
| Preview before you add  | `aube dlx shadcn@latest add <name> --dry-run` |
| Show the project config | `aube dlx shadcn@latest info`                 |
| Read the component docs | `aube dlx shadcn@latest docs <name>`          |
| Compare with upstream   | `aube dlx shadcn@latest add <name> --diff`    |

Do not use `npx`, `npm`, `yarn`, or `pnpm dlx`. This repo uses `aube`.

Read `docs <name>` before you decide that a variant or a sub-component is missing.

### Components that need a new dependency

`add <name>` cannot install one. It shells out to a bare `pnpm add`, and this
tree's `node_modules` is aube-shaped, so it dies with
`ERR_PNPM_HOIST_PATTERN_DIFF` before writing any file — including the component.
It leaves `node_modules` intact.

Install the dependency first, then re-run `add <name>`:

```sh
mise run install:lock -F admin react-day-picker
aube dlx shadcn@latest add calendar
```

`add <name> --view` prints a component's source without installing anything, if
you only need to read it.

## After you add a component

Run these three commands, in this order:

```
hk fix
aube run -F admin type-check
aube run -F admin lint:oxlint
```

`hk fix` is not optional. Every file the shadcn CLI writes fails `oxfmt`. `hk fix` reformats it.

Do not run `aube run -F admin lint` or `aube run -F admin lint:format`. Both fail before they lint. `oxfmt` is installed only in the root `node_modules/.bin`, so the package script stops at `sh: 1: oxfmt: not found`.

`--dry-run` lists the dependencies a component pulls in. When the CLI adds a dependency to `client/admin/package.json`, check `pnpm-workspace.yaml` for a `catalog:` entry. Use `catalog:` when one exists.

## Customize by composition

Use the first option that works:

1. **Props and `className`.** Layout and spacing only.
2. **Built-in variants.** `<Button variant="ghost" size="sm">`.
3. **A wrapper component** in `client/admin/src/components/`, never in `ui/`. `ConfirmDialog.tsx` wraps `Dialog`. `data-table.tsx` wraps the table primitives. Follow that pattern and name the wrapper for what it is.
4. **A `cva` variant object** in your own file, applied through `cn()`.

Never override a component's colors with `className`. Use the semantic tokens: `bg-primary`, `text-muted-foreground`, `border-border`.

## Read the --diff output correctly

`--diff` compares the local file with upstream. Two differences are expected noise:

- **"Formatting-only changes (spacing, quotes, semicolons)"** — caused by `hk fix`. Every admin component reports this. It is not drift.
- **A `"use client"` line added or removed** — `components.json` sets `"rsc": false` and admin is a Vite SPA.

Anything else is real drift. Report it to the user. Do not overwrite it silently.

`--diff` prints at most 5 files per run. Pass one component at a time when you need the full list.

## Common mistakes

- Hand-writing `ui/card.tsx` because it is short. Run the CLI.
- Editing `ui/button.tsx` to add a variant. Wrap it, or ask the user.
- Running the CLI from the repo root, then finding the file in `client/dashboard`.
- Passing `--overwrite` to clear a conflict. It discards local state. Run `--diff` first and read it.
- Skipping `hk fix`, then failing CI on formatting.
