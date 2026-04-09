# Playground Credentials via Server-Side Environments — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace localStorage-based playground credential storage with encrypted server-side environments, scoped per-user per-toolset.

**Architecture:** A new `usePlaygroundEnvironment` hook manages a lazily-created environment (slug `playground-<userID>-<toolsetSlug>`) using existing `createEnvironment`/`updateEnvironment` SDK mutations. `PlaygroundAuth` uses this hook instead of `useLocalStorageState`. `PlaygroundElements` passes the environment slug via `Gram-Environment` header instead of `MCP-*` headers, letting the server resolve credentials.

**Tech Stack:** React hooks, `@gram/client` SDK mutations, existing Gram environment APIs

**Skills:** `frontend`

---

### Task 1: Create `usePlaygroundEnvironment` hook

**Files:**

- Create: `client/dashboard/src/pages/playground/usePlaygroundEnvironment.ts`

- [ ] **Step 1: Create the hook file**

```typescript
import { useCallback, useMemo, useRef } from "react";
import { useOrganization, useUser } from "@/contexts/Auth";
import { Toolset } from "@/lib/toolTypes";
import {
  useCreateEnvironmentMutation,
  useListEnvironments,
  useUpdateEnvironmentMutation,
  invalidateAllListEnvironments,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";

interface PlaygroundEnvironmentEntry {
  name: string;
  hasStoredValue: boolean;
}

interface UsePlaygroundEnvironmentReturn {
  /** The environment slug (always computed, even if env doesn't exist yet) */
  slug: string;
  /** Whether the environment already exists on the server */
  exists: boolean;
  /** Which entry keys have stored values */
  storedEntries: PlaygroundEnvironmentEntry[];
  /** Save entries to the environment (creates if needed, debounced) */
  save: (
    entriesToUpdate: { name: string; value: string }[],
    entriesToRemove: string[],
  ) => void;
  /** Whether a save is currently in progress */
  isSaving: boolean;
}

export function usePlaygroundEnvironment(
  toolset: Toolset,
): UsePlaygroundEnvironmentReturn {
  const user = useUser();
  const organization = useOrganization();
  const queryClient = useQueryClient();

  const slug = useMemo(
    () => `playground-${user.id}-${toolset.slug}`,
    [user.id, toolset.slug],
  );

  const displayName = user.displayName || user.email || "User";
  const envName = `Playground - ${displayName}`;

  const { data: environmentsData } = useListEnvironments();
  const existingEnv = useMemo(
    () => environmentsData?.environments?.find((env) => env.slug === slug),
    [environmentsData, slug],
  );

  const storedEntries = useMemo<PlaygroundEnvironmentEntry[]>(() => {
    if (!existingEnv) return [];
    return existingEnv.entries.map((entry) => ({
      name: entry.name,
      hasStoredValue: !!entry.value && entry.value.trim() !== "",
    }));
  }, [existingEnv]);

  const createMutation = useCreateEnvironmentMutation();
  const updateMutation = useUpdateEnvironmentMutation();

  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const save = useCallback(
    (
      entriesToUpdate: { name: string; value: string }[],
      entriesToRemove: string[],
    ) => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }

      debounceTimerRef.current = setTimeout(() => {
        const onSuccess = () => {
          invalidateAllListEnvironments(queryClient);
        };

        if (!existingEnv) {
          // Create new environment with initial entries
          createMutation.mutate(
            {
              request: {
                createEnvironmentForm: {
                  name: envName,
                  organizationId: organization.id,
                  entries: entriesToUpdate,
                },
              },
            },
            { onSuccess },
          );
        } else {
          // Update existing environment
          updateMutation.mutate(
            {
              request: {
                slug,
                updateEnvironmentRequestBody: {
                  entriesToUpdate,
                  entriesToRemove,
                },
              },
            },
            { onSuccess },
          );
        }
      }, 1000);
    },
    [
      existingEnv,
      slug,
      envName,
      organization.id,
      createMutation,
      updateMutation,
      queryClient,
    ],
  );

  return {
    slug,
    exists: !!existingEnv,
    storedEntries,
    save,
    isSaving: createMutation.isPending || updateMutation.isPending,
  };
}
```

- [ ] **Step 2: Verify types compile**

Run: `cd client/dashboard && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add client/dashboard/src/pages/playground/usePlaygroundEnvironment.ts
git commit -m "feat: add usePlaygroundEnvironment hook for server-side credential storage"
```

---

### Task 2: Update `PlaygroundAuth` to use server-side environment

**Files:**

- Modify: `client/dashboard/src/pages/playground/PlaygroundAuth.tsx`

- [ ] **Step 1: Replace localStorage with `usePlaygroundEnvironment` and add save button**

Key changes to `PlaygroundAuth.tsx`:

1. Remove `useLocalStorageState` import
2. Add `useState` back to the react import
3. Import `usePlaygroundEnvironment` from `./usePlaygroundEnvironment`
4. Remove `onUserProvidedHeadersChange` from props interface and component
5. Add `onPlaygroundEnvironmentSlug` callback prop to notify parent of the env slug
6. Replace `useLocalStorageState` call with `useState<Record<string, string>>({})`
7. Use `usePlaygroundEnvironment` to detect stored entries and save values
8. Show `PASSWORD_MASK` for entries that have stored values but haven't been edited
9. Remove the `useEffect` that built `MCP-*` headers
10. Add a "Save" button that triggers the save
11. Track which masked fields have been edited to avoid re-saving unchanged values

Updated props interface:

```typescript
interface PlaygroundAuthProps {
  toolset: Toolset;
  onPlaygroundEnvironmentSlug?: (slug: string | undefined) => void;
}
```

Updated state and hook usage (inside `PlaygroundAuth`):

```typescript
// Replace the useLocalStorageState line with:
const [userProvidedValues, setUserProvidedValues] = useState<
  Record<string, string>
>({});
const [editedKeys, setEditedKeys] = useState<Set<string>>(new Set());

const playgroundEnv = usePlaygroundEnvironment(toolset);

// Notify parent of playground environment slug when it exists
useEffect(() => {
  onPlaygroundEnvironmentSlug?.(
    playgroundEnv.exists ? playgroundEnv.slug : undefined,
  );
}, [playgroundEnv.exists, playgroundEnv.slug, onPlaygroundEnvironmentSlug]);

// Save handler
const handleSave = () => {
  const entriesToUpdate = Object.entries(userProvidedValues)
    .filter(([key, value]) => value.trim() && editedKeys.has(key))
    .map(([name, value]) => ({ name, value }));

  const entriesToRemove = Array.from(editedKeys).filter(
    (key) => !userProvidedValues[key]?.trim(),
  );

  if (entriesToUpdate.length > 0 || entriesToRemove.length > 0) {
    playgroundEnv.save(entriesToUpdate, entriesToRemove);
    setEditedKeys(new Set());
  }
};
```

Updated display logic for user-provided env vars:

```typescript
if (envVar.state === "user-provided") {
  const storedEntry = playgroundEnv.storedEntries.find(
    (e) => e.name === envVar.key,
  );
  const hasBeenEdited = editedKeys.has(envVar.key);

  if (hasBeenEdited) {
    displayValue = userProvidedValues[envVar.key] || "";
  } else if (storedEntry?.hasStoredValue) {
    displayValue = PASSWORD_MASK;
  } else {
    displayValue = "";
  }
  placeholder = "Enter value here";
  isEditable = true;
}
```

Updated onChange handler:

```typescript
onChange={(newValue) => {
  if (isEditable) {
    setUserProvidedValues((prev) => ({
      ...prev,
      [envVar.key]: newValue,
    }));
    setEditedKeys((prev) => new Set(prev).add(envVar.key));
  }
}}
```

Add save button after the env vars list (before the "Configure auth" link):

```tsx
{
  envVars.some((v) => v.state === "user-provided") && (
    <Button
      size="sm"
      variant="default"
      className="w-full"
      onClick={handleSave}
      disabled={editedKeys.size === 0 || playgroundEnv.isSaving}
    >
      {playgroundEnv.isSaving ? (
        <Loader2 className="size-3 mr-2 animate-spin" />
      ) : null}
      Save
    </Button>
  );
}
```

Also remove the old `useEffect` that built `MCP-*` headers (lines 384-398 in current file).

- [ ] **Step 2: Verify types compile**

Run: `cd client/dashboard && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add client/dashboard/src/pages/playground/PlaygroundAuth.tsx
git commit -m "feat: use server-side environment for playground credentials"
```

---

### Task 3: Update `Playground.tsx` to remove `userProvidedHeaders` and thread environment slug

**Files:**

- Modify: `client/dashboard/src/pages/playground/Playground.tsx`

- [ ] **Step 1: Replace `userProvidedHeaders` with `playgroundEnvironmentSlug`**

In `PlaygroundInner` (lines 52-69):

- Remove: `const [userProvidedHeaders, setUserProvidedHeaders] = useState<Record<string, string>>({});`
- Add: `const [playgroundEnvironmentSlug, setPlaygroundEnvironmentSlug] = useState<string | undefined>(undefined);`

In `ToolsetPanel` call (lines 129-141):

- Remove: `onUserProvidedHeadersChange={setUserProvidedHeaders}`
- Add: `onPlaygroundEnvironmentSlug={setPlaygroundEnvironmentSlug}`

In `PlaygroundElements` call (lines 144-156):

- Remove: `userProvidedHeaders={userProvidedHeaders}`
- Add: `playgroundEnvironmentSlug={playgroundEnvironmentSlug}`

In `ToolsetPanel` component props (lines 173-194):

- Remove: `onUserProvidedHeadersChange?: (headers: Record<string, string>) => void;`
- Add: `onPlaygroundEnvironmentSlug?: (slug: string | undefined) => void;`

In `ToolsetPanel` render of `PlaygroundAuth` (lines 418-425):

- Remove: `onUserProvidedHeadersChange={onUserProvidedHeadersChange}`
- Add: `onPlaygroundEnvironmentSlug={onPlaygroundEnvironmentSlug}`

- [ ] **Step 2: Verify types compile**

Run: `cd client/dashboard && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add client/dashboard/src/pages/playground/Playground.tsx
git commit -m "refactor: replace userProvidedHeaders with playgroundEnvironmentSlug"
```

---

### Task 4: Update `PlaygroundElements.tsx` to use environment slug instead of headers

**Files:**

- Modify: `client/dashboard/src/pages/playground/PlaygroundElements.tsx`

- [ ] **Step 1: Replace `userProvidedHeaders` prop with `playgroundEnvironmentSlug`**

Update props interface:

```typescript
// Remove: userProvidedHeaders: Record<string, string>;
// Add: playgroundEnvironmentSlug?: string;
```

Update `GramElementsProvider` config:

- Remove `...userProvidedHeaders` from `api.headers` (keep `"X-Gram-Source": "playground"`)
- Remove `...userProvidedHeaders` from `environment` config
- Update `gramEnvironment`: use `playgroundEnvironmentSlug ?? environmentSlug ?? undefined`

Also remove `userProvidedHeaders` from `PlaygroundMcpAppsProvider` headers if present.

- [ ] **Step 2: Verify types compile**

Run: `cd client/dashboard && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add client/dashboard/src/pages/playground/PlaygroundElements.tsx
git commit -m "feat: pass playground environment slug to GramElementsProvider"
```

---

### Task 5: Clean up unused imports and verify end-to-end

**Files:**

- Modify: `client/dashboard/src/pages/playground/PlaygroundAuth.tsx` (cleanup)

- [ ] **Step 1: Remove `useLocalStorageState` import if still present**

Verify no unused imports remain across all modified files.

- [ ] **Step 2: Run full type check and lint**

Run: `cd client/dashboard && npx tsc --noEmit`
Expected: No errors

Run: `cd client/dashboard && pnpm build`
Expected: Build succeeds

- [ ] **Step 3: Commit any cleanup**

```bash
git add -u client/dashboard/src/pages/playground/
git commit -m "chore: clean up unused imports from playground auth refactor"
```

---

### Task 6: Update changeset and push

**Files:**

- Modify: `.changeset/persist-playground-auth.md`

- [ ] **Step 1: Update changeset description**

```markdown
---
"dashboard": patch
---

Store user-provided playground credentials in encrypted server-side environments instead of localStorage. Credentials are scoped per-user per-toolset and resolved server-side when proxying to MCP servers.
```

- [ ] **Step 2: Commit and push**

```bash
git add .changeset/persist-playground-auth.md
git commit -m "chore: update changeset for server-side credential storage"
git push
```
