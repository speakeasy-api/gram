# MCP public-visibility warning for system variables — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Block the MCP "make public" action behind an on-brand warning modal that lists every system-provided environment variable that will be shared with public callers, with a one-click link into the attached environment.

**Architecture:** A small pure helper derives "system-provided" var names from the existing `useEnvironmentVariables` output. A new `PublicMcpWarningDialog` component renders the warning using moonshine + licensed brand fonts. `MCPStatusDropdown` is widened to fetch env + MCP metadata and to route public transitions through the new dialog, composing with the existing `ServerEnableDialog` on disabled→public.

**Tech Stack:** React 18 / TypeScript / Vitest + happy-dom / Tailwind / moonshine `Button` + `@/components/ui/dialog` / react-query hooks from `@gram/client/react-query`.

**Spec:** `docs/superpowers/specs/2026-04-19-mcp-public-system-vars-warning-design.md`

---

## File Structure

**Modify:**

- `client/dashboard/src/pages/mcp/environmentVariableUtils.ts` — add `getSystemProvidedVariables()` helper (≤10 LOC).
- `client/dashboard/src/pages/mcp/MCPDetails.tsx` — widen `MCPStatusDropdown` to fetch env + metadata and gate public transitions on the new dialog; extend telemetry with `system_vars_warned`.

**Create:**

- `client/dashboard/src/pages/mcp/environmentVariableUtils.test.ts` — unit tests for the helper.
- `client/dashboard/src/components/public-mcp-warning-dialog.tsx` — the dialog component.
- `client/dashboard/src/components/public-mcp-warning-dialog.test.tsx` — component-level tests.

---

### Task 1: Pure helper — `getSystemProvidedVariables`

**Files:**

- Modify: `client/dashboard/src/pages/mcp/environmentVariableUtils.ts`
- Create: `client/dashboard/src/pages/mcp/environmentVariableUtils.test.ts`

- [ ] **Step 1: Write the failing test file**

Create `client/dashboard/src/pages/mcp/environmentVariableUtils.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import type { EnvironmentVariable } from "./environmentVariableUtils";
import { getSystemProvidedVariables } from "./environmentVariableUtils";

const mkVar = (
  overrides: Partial<EnvironmentVariable> &
    Pick<EnvironmentVariable, "key" | "state">,
): EnvironmentVariable => ({
  id: overrides.id ?? `id-${overrides.key}`,
  key: overrides.key,
  state: overrides.state,
  isRequired: overrides.isRequired ?? true,
  valueGroups: overrides.valueGroups ?? [],
  description: overrides.description,
});

describe("getSystemProvidedVariables", () => {
  it("returns empty array when no vars are in system state", () => {
    const vars: EnvironmentVariable[] = [
      mkVar({ key: "A", state: "user-provided" }),
      mkVar({ key: "B", state: "omitted" }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual([]);
  });

  it("returns keys of system vars that have a value in the attached env", () => {
    const vars: EnvironmentVariable[] = [
      mkVar({
        key: "STRIPE_API_KEY",
        state: "system",
        valueGroups: [
          { valueHash: "h1", value: "***", environments: ["prod"] },
        ],
      }),
      mkVar({
        key: "DATABASE_URL",
        state: "system",
        valueGroups: [
          { valueHash: "h2", value: "***", environments: ["prod"] },
        ],
      }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual([
      "STRIPE_API_KEY",
      "DATABASE_URL",
    ]);
  });

  it("excludes system vars with no value in the attached env", () => {
    const vars: EnvironmentVariable[] = [
      mkVar({
        key: "ONLY_IN_STAGING",
        state: "system",
        valueGroups: [
          { valueHash: "h1", value: "***", environments: ["staging"] },
        ],
      }),
      mkVar({
        key: "IN_PROD",
        state: "system",
        valueGroups: [
          { valueHash: "h2", value: "***", environments: ["prod"] },
        ],
      }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual(["IN_PROD"]);
  });

  it("handles custom (non-required) system vars the same as required", () => {
    const vars: EnvironmentVariable[] = [
      mkVar({
        key: "CUSTOM_SECRET",
        state: "system",
        isRequired: false,
        valueGroups: [
          { valueHash: "h1", value: "***", environments: ["prod"] },
        ],
      }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual(["CUSTOM_SECRET"]);
  });
});
```

- [ ] **Step 2: Run the test and watch it fail**

Run: `cd client/dashboard && pnpm test src/pages/mcp/environmentVariableUtils.test.ts`
Expected: FAIL — `getSystemProvidedVariables is not a function`.

- [ ] **Step 3: Add the helper to `environmentVariableUtils.ts`**

Append to the bottom of `client/dashboard/src/pages/mcp/environmentVariableUtils.ts`:

```ts
// Returns keys of variables that will be server-injected for every caller of the
// MCP server — i.e. variables in `state: "system"` with a value present in the
// attached environment. Used to warn users before flipping an MCP to public.
export const getSystemProvidedVariables = (
  envVars: EnvironmentVariable[],
  attachedEnvironmentSlug: string,
): string[] =>
  envVars
    .filter((v) => v.state === "system")
    .filter((v) => environmentHasValue(v, attachedEnvironmentSlug))
    .map((v) => v.key);
```

- [ ] **Step 4: Run the test and watch it pass**

Run: `cd client/dashboard && pnpm test src/pages/mcp/environmentVariableUtils.test.ts`
Expected: PASS — all 4 cases green.

- [ ] **Step 5: Type-check**

Run: `cd client/dashboard && pnpm tsc -p tsconfig.app.json --noEmit`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add client/dashboard/src/pages/mcp/environmentVariableUtils.ts \
        client/dashboard/src/pages/mcp/environmentVariableUtils.test.ts
git commit -m "feat(dashboard): add getSystemProvidedVariables helper"
```

---

### Task 2: `PublicMcpWarningDialog` component

**Files:**

- Create: `client/dashboard/src/components/public-mcp-warning-dialog.tsx`
- Create: `client/dashboard/src/components/public-mcp-warning-dialog.test.tsx`

- [ ] **Step 1: Write the failing component test**

Create `client/dashboard/src/components/public-mcp-warning-dialog.test.tsx`:

```tsx
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { PublicMcpWarningDialog } from "./public-mcp-warning-dialog";

const defaultProps = {
  isOpen: true,
  onClose: vi.fn(),
  onConfirm: vi.fn(),
  environmentName: "Production",
  environmentSlug: "production",
  variableNames: ["STRIPE_API_KEY", "DATABASE_URL"],
};

describe("PublicMcpWarningDialog", () => {
  it("renders title, body, variable names, and the environment link", () => {
    render(<PublicMcpWarningDialog {...defaultProps} />);

    expect(
      screen.getByText("Share system secrets with public callers."),
    ).toBeTruthy();
    // "Production" appears both in the body and in the link label; use getAllByText.
    expect(screen.getAllByText(/Production/).length).toBeGreaterThan(0);
    expect(screen.getByText("STRIPE_API_KEY")).toBeTruthy();
    expect(screen.getByText("DATABASE_URL")).toBeTruthy();

    const link = screen.getByRole("link", { name: /Review in Production/ });
    expect(link.getAttribute("href")).toBe("/environments/production");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("fires onConfirm when the destructive action is clicked", () => {
    const onConfirm = vi.fn();
    render(<PublicMcpWarningDialog {...defaultProps} onConfirm={onConfirm} />);
    fireEvent.click(screen.getByRole("button", { name: /Make public anyway/ }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("fires onClose when Cancel is clicked", () => {
    const onClose = vi.fn();
    render(<PublicMcpWarningDialog {...defaultProps} onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
```

Note: the dashboard does not configure `@testing-library/jest-dom`, so assertions stick to plain `.toBeTruthy()` / attribute reads rather than `.toBeInTheDocument()` / `.toHaveAttribute()`. `@testing-library/react` and `@testing-library/dom` are already in `package.json`, so no install is needed.

- [ ] **Step 2: Run the test and watch it fail**

Run: `cd client/dashboard && pnpm test src/components/public-mcp-warning-dialog.test.tsx`
Expected: FAIL — `Cannot find module './public-mcp-warning-dialog'`.

- [ ] **Step 3: Create the component**

Create `client/dashboard/src/components/public-mcp-warning-dialog.tsx`:

```tsx
import { Dialog } from "@/components/ui/dialog";
import { Button } from "@speakeasy-api/moonshine";
import { ExternalLink, ShieldAlert } from "lucide-react";

interface PublicMcpWarningDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
  environmentName: string;
  environmentSlug: string;
  variableNames: string[];
}

export function PublicMcpWarningDialog({
  isOpen,
  onClose,
  onConfirm,
  isLoading = false,
  environmentName,
  environmentSlug,
  variableNames,
}: PublicMcpWarningDialogProps) {
  const handleConfirm = () => {
    onConfirm();
    onClose();
  };

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content
        className="max-w-md overflow-hidden p-0"
        style={{
          borderTop: "2px solid #C83228",
        }}
      >
        <div className="p-6">
          <Dialog.Header>
            <Dialog.Title
              className="flex items-center gap-2 text-2xl"
              style={{
                fontFamily: 'Tobias, "Taviraj", Georgia, serif',
                fontWeight: 100,
                letterSpacing: "-0.04em",
                lineHeight: 1.1,
              }}
            >
              <ShieldAlert
                className="h-5 w-5 shrink-0"
                style={{ color: "#C83228" }}
              />
              Share system secrets with public callers.
            </Dialog.Title>
          </Dialog.Header>

          <div className="mt-4 space-y-4 text-sm">
            <p className="text-muted-foreground">
              Anyone with this URL will call with values from{" "}
              <strong className="text-foreground">{environmentName}</strong>.
              System values are shared. Treat them as team credentials, not user
              credentials.
            </p>

            <div className="space-y-2">
              <p
                className="text-[11px] tracking-wider uppercase text-[#8B8684]"
                style={{ fontFamily: '"Diatype Mono", monospace' }}
              >
                Used by every public caller
              </p>
              <ul
                className="max-h-40 space-y-1 overflow-y-auto rounded border border-border bg-muted/30 p-3"
                style={{ fontFamily: '"Diatype Mono", monospace' }}
              >
                {variableNames.map((name) => (
                  <li key={name} className="font-light text-sm">
                    {name}
                  </li>
                ))}
              </ul>
            </div>

            <a
              href={`/environments/${environmentSlug}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-sm text-foreground underline-offset-4 hover:underline"
            >
              Review in {environmentName}
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          </div>

          <Dialog.Footer className="mt-6 gap-2">
            <Button variant="tertiary" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              onClick={handleConfirm}
              disabled={isLoading}
            >
              {isLoading ? "Publishing..." : "Make public anyway."}
            </Button>
          </Dialog.Footer>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
```

- [ ] **Step 4: Run the test and watch it pass**

Run: `cd client/dashboard && pnpm test src/components/public-mcp-warning-dialog.test.tsx`
Expected: PASS — all 3 cases green.

- [ ] **Step 5: Type-check + lint**

Run: `cd client/dashboard && pnpm tsc -p tsconfig.app.json --noEmit`
Run: `cd client/dashboard && pnpm lint`
Expected: both clean.

- [ ] **Step 6: Commit**

```bash
git add client/dashboard/src/components/public-mcp-warning-dialog.tsx \
        client/dashboard/src/components/public-mcp-warning-dialog.test.tsx
git commit -m "feat(dashboard): add PublicMcpWarningDialog component"
```

---

### Task 3: Wire the dialog into `MCPStatusDropdown`

**Files:**

- Modify: `client/dashboard/src/pages/mcp/MCPDetails.tsx:404-549` (the `MCPStatusDropdown` component)

- [ ] **Step 1: Add the new imports at the top of `MCPDetails.tsx`**

Find the existing `@gram/client/react-query` import block (lines 51–63). The hooks `useGetMcpMetadata` and `useListEnvironments` are already imported. Add the remaining imports near the existing moonshine / local imports:

```tsx
// Existing:
import { ServerEnableDialog } from "@/components/server-enable-dialog";
// Add:
import { PublicMcpWarningDialog } from "@/components/public-mcp-warning-dialog";
```

And add the helper import near the top of the file alongside other local `./` imports — search for where `./` imports live inside `MCPDetails.tsx` (e.g. `import { ... } from "./mcp-details-utils"` if present; otherwise add before the `MCPStatusDropdown` declaration):

```tsx
import { getSystemProvidedVariables } from "./environmentVariableUtils";
import { useEnvironmentVariables } from "./useEnvironmentVariables";
```

- [ ] **Step 2: Extend `MCPStatusDropdown` state + data fetching**

In `client/dashboard/src/pages/mcp/MCPDetails.tsx`, replace the beginning of `MCPStatusDropdown` (the block from line 404 through line 416) with:

```tsx
export function MCPStatusDropdown({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [pendingStatus, setPendingStatus] = useState<ServerStatus | null>(null);
  const [publicWarningPending, setPublicWarningPending] =
    useState<ServerStatus | null>(null);
  const [isMaxServersModalOpen, setIsMaxServersModalOpen] = useState(false);
  const updateToolsetMutation = useUpdateToolsetMutation();
  const telemetry = useTelemetry();

  // Fetch data needed to detect system-provided vars on the attached env.
  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];
  const { data: mcpMetadataData } = useGetMcpMetadata(
    { toolsetSlug: toolset.slug },
    undefined,
    { throwOnError: false, retry: false },
  );
  const mcpMetadata = mcpMetadataData?.metadata;

  const attachedEnvironment = mcpMetadata?.defaultEnvironmentId
    ? (environments.find((e) => e.id === mcpMetadata.defaultEnvironmentId) ??
      null)
    : null;

  const envVars = useEnvironmentVariables(toolset, environments, mcpMetadata);
  const systemVarNames = useMemo(
    () =>
      attachedEnvironment
        ? getSystemProvidedVariables(envVars, attachedEnvironment.slug)
        : [],
    [envVars, attachedEnvironment],
  );

  const currentStatus: ServerStatus = !toolset.mcpEnabled
    ? "disabled"
    : toolset.mcpIsPublic
      ? "public"
      : "private";
```

If `useMemo` is not yet imported at the top of `MCPDetails.tsx`, add it to the `react` import line (it likely already is, but verify).

- [ ] **Step 3: Update `applyStatus` telemetry to record whether the warning fired**

Replace the `telemetry.capture(...)` call inside `onSuccess` (roughly lines 435–443) with:

```tsx
telemetry.capture("mcp_event", {
  action:
    status === "disabled"
      ? "mcp_disabled"
      : status === "public"
        ? "mcp_made_public"
        : "mcp_made_private",
  slug: toolset.slug,
  system_vars_warned:
    status === "public" ? systemVarNames.length > 0 : undefined,
});
```

- [ ] **Step 4: Route public transitions through the warning dialog**

Replace the `handleSelect` function (lines 470–480) with:

```tsx
const handleSelect = (status: ServerStatus) => {
  if (status === currentStatus) return;
  setDropdownOpen(false);

  const goingPublic = status === "public";
  const needsEnableDialog =
    status === "disabled" || currentStatus === "disabled";
  const needsPublicWarning = goingPublic && systemVarNames.length > 0;

  // Defer state changes until after the dropdown has fully closed to avoid
  // Radix focus-trap conflicts (same pattern as before).
  setTimeout(() => {
    if (needsPublicWarning) {
      // Show the system-vars warning first. If the user confirms, we chain to
      // ServerEnableDialog when the transition also requires enablement.
      setPublicWarningPending(status);
    } else if (needsEnableDialog) {
      setPendingStatus(status);
    } else {
      applyStatus(status);
    }
  }, 0);
};

const handlePublicWarningConfirm = () => {
  const target = publicWarningPending;
  setPublicWarningPending(null);
  if (!target) return;
  // If we also need the enablement dialog (disabled → public), chain it now.
  if (currentStatus === "disabled") {
    setPendingStatus(target);
  } else {
    applyStatus(target);
  }
};
```

- [ ] **Step 5: Render the new dialog inside `MCPStatusDropdown`'s returned JSX**

Find the closing `</DropdownMenu>` at roughly line 527, followed by `<ServerEnableDialog ... />` at line 528. Insert the new dialog _between_ them so both dialogs are rendered:

```tsx
      </DropdownMenu>
      <PublicMcpWarningDialog
        isOpen={publicWarningPending !== null}
        onClose={() => setPublicWarningPending(null)}
        onConfirm={handlePublicWarningConfirm}
        isLoading={updateToolsetMutation.isPending}
        environmentName={attachedEnvironment?.name ?? ""}
        environmentSlug={attachedEnvironment?.slug ?? ""}
        variableNames={systemVarNames}
      />
      <ServerEnableDialog
        isOpen={pendingStatus !== null}
        ...
```

Leave the rest of the existing `ServerEnableDialog` and `FeatureRequestModal` JSX unchanged.

- [ ] **Step 6: Type-check**

Run: `cd client/dashboard && pnpm tsc -p tsconfig.app.json --noEmit`
Expected: clean.

- [ ] **Step 7: Lint**

Run: `cd client/dashboard && pnpm lint`
Expected: clean.

- [ ] **Step 8: Run the full dashboard test suite**

Run: `cd client/dashboard && pnpm test`
Expected: all tests pass, including the two new test files.

- [ ] **Step 9: Commit**

```bash
git add client/dashboard/src/pages/mcp/MCPDetails.tsx
git commit -m "feat(dashboard): warn on public MCP toggle when system vars exist"
```

---

### Task 4: Manual verification

No code changes — these are the runtime checks a reviewer should perform before approving the PR. Check the box once each scenario has been exercised.

- [ ] **Step 1: Start the dev stack**

Run (in two panes):

```bash
mise start:server --dev-single-process
cd client/dashboard && pnpm dev
```

- [ ] **Step 2: Scenario A — private → public with system vars**

1. Open a toolset that has an attached environment with at least one required variable whose value is populated (so it's `state: "system"`).
2. Ensure the MCP server is `Private` (enabled).
3. Click the status dropdown → select **Public**.
4. Verify: the `PublicMcpWarningDialog` appears, lists the variable names, the link points at `/environments/<slug>`, and clicking "Cancel" dismisses without changing status.
5. Re-open, click "Make public anyway." → verify status flips to Public, toast reads "MCP server set to public".

- [ ] **Step 3: Scenario B — private → public with NO system vars**

1. Detach the environment (or switch to an env with no populated system vars) so `systemVarNames` is empty.
2. Flip status to Public → verify **no dialog** appears and the server goes public immediately (preserving current behavior).

- [ ] **Step 4: Scenario C — disabled → public with system vars**

1. Disable the MCP server.
2. Click the status dropdown → select **Public**.
3. Verify: the `PublicMcpWarningDialog` appears **first**. After clicking "Make public anyway.", the existing `ServerEnableDialog` is chained (billing-gate copy still works). After confirming that, the server is enabled and public.

- [ ] **Step 5: Scenario D — disabled → public with NO system vars**

1. Disable, detach env (or ensure no system vars), then flip to Public.
2. Verify: **only** the existing `ServerEnableDialog` appears (no system-vars warning).

- [ ] **Step 6: Scenario E — public → private**

Flip a public server back to private → verify **no warning dialog** appears (warning only fires on transitions _to_ public).

- [ ] **Step 7: Telemetry spot-check**

In the browser devtools network tab, filter for `capture` / PostHog requests. On Scenario A's confirmation, verify the `mcp_made_public` event payload includes `system_vars_warned: true`. On Scenario B's success, verify `system_vars_warned: false`.

---

## Self-review notes

- **Spec coverage**: Tasks 1–3 cover the helper, the dialog, and the wiring. The brand adaptation (Tobias title, Swift red accent, Diatype Mono list, period-terminated copy) is realised in Task 2 Step 4. The "chain into ServerEnableDialog on disabled→public" composition is in Task 3 Step 4. Telemetry is in Task 3 Step 3. Manual scenarios in Task 4 map 1:1 to the spec's trigger matrix.
- **Placeholder scan**: no "TBD"/"TODO" markers; every step has the literal code it expects.
- **Type consistency**: the helper signature `(envVars: EnvironmentVariable[], slug: string) => string[]` is used identically in both Task 1 and Task 3. Prop names on `PublicMcpWarningDialog` match between Task 2 definition and Task 3 call site.
- **Note on test style**: the dashboard does not configure `@testing-library/jest-dom`, so the component tests use `.toBeTruthy()` + `getAttribute()` rather than `.toBeInTheDocument()` / `.toHaveAttribute()`. `@testing-library/react` is already in `package.json`, so no new dev-dependency install is needed.
