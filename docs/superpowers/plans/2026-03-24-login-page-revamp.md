# Login Page Revamp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Revamp the Gram login page with feature badges, a gradient-outline button, and pulse flow animations on the platform diagram.

**Architecture:** Two independent workstreams — right pane (badges + button in `login-section.tsx`) and left pane (pulse animation in `platform-diagram.tsx`). Both modify only their respective component files. No backend changes.

**Tech Stack:** React, Tailwind CSS, Framer Motion (`motion/react`), Moonshine design system (`@speakeasy-api/moonshine`).

**Spec:** `docs/superpowers/specs/2026-03-24-login-page-revamp-design.md`

**Frontend skill:** `@frontend` — Use Moonshine utilities where possible. Use `pnpm`. Check `components/` for reuse. No hardcoded Tailwind colors except for brand-specific values not in the design system.

**Brand skill:** `speakeasy-brand-skill/.claude/skills/speakeasy-brand.md` — Language colors for gradients, `#8B8684` muted text, `#D3D3D3` supporting grey, monospace uppercase for captions.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `client/dashboard/src/pages/login/components/login-section.tsx` | Modify | Add `FeatureBadges` component, `GradientButton` component, update `LoginSection` layout |
| `client/dashboard/src/pages/login/components/platform-diagram.tsx` | Modify | Add `PulseConnector` component, add breathing animation to gradient border |

---

## Task 1: Feature Badges (Right Pane)

**Files:**
- Modify: `client/dashboard/src/pages/login/components/login-section.tsx`

- [ ] **Step 1: Add the FeatureBadges component**

Add above the `AuthLayout` function in `login-section.tsx`:

```tsx
const FEATURE_BADGES = [
  "MCP Servers",
  "Observability",
  "Auth & OAuth",
  "Tool Curation",
];

function FeatureBadges({ labels = FEATURE_BADGES }: { labels?: string[] }) {
  return (
    <div className="flex flex-wrap justify-center gap-2">
      {labels.map((label) => (
        <span
          key={label}
          className="rounded-full border border-[#D3D3D3] px-3 py-1 font-mono text-xs uppercase tracking-[0.01em] text-[#8B8684]"
        >
          {label}
        </span>
      ))}
    </div>
  );
}
```

Note: We use hardcoded brand hex values (`#D3D3D3`, `#8B8684`) here because these are Speakeasy brand-specific colors not available in Moonshine's design tokens. The existing `Badge` component (`components/ui/badge.tsx`) uses `rounded-md` and background-fill variants — it doesn't match the pill-shaped, border-only, monospace-uppercase style we need, so a local component is appropriate.

- [ ] **Step 2: Render badges in AuthLayout**

In the `AuthLayout` component, add `<FeatureBadges />` after the subtext `<p>` and before `{children}`:

```tsx
function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen p-8 md:p-16 bg-white relative">
      <div className="w-full flex flex-col items-center gap-8 max-w-xs">
        <div className="flex flex-col items-center gap-4">
          <GramLogo
            className="w-[200px] mb-2 dark:!invert-0"
            variant="vertical"
          />
          <p className="text-body-lg text-center dark:text-black">
            Embed intelligent, performant chat and agents directly into your
            product. Give users AI that understands your data and takes action.
            Powered by MCP.
          </p>
          <FeatureBadges />
        </div>

        {children}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Verify badges render**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: No type errors.

Visually verify by running the dev server (`mise start:server --dev-single-process`) and navigating to the login page. Badges should appear as a horizontal row of grey-bordered pills below the subtext.

- [ ] **Step 4: Commit**

```bash
git add client/dashboard/src/pages/login/components/login-section.tsx
git commit -m "feat(login): add feature badges to login page"
```

---

## Task 2: Gradient-Outline Login Button (Right Pane)

**Files:**
- Modify: `client/dashboard/src/pages/login/components/login-section.tsx`

- [ ] **Step 1: Add the GradientButton component**

Add below `FeatureBadges` in `login-section.tsx`. First, update the imports to add `cn`:

```tsx
// Change this line:
import { getServerURL } from "@/lib/utils";
// To:
import { cn, getServerURL } from "@/lib/utils";
```

Then add the component. Uses the pseudo-element approach for gradient border with border-radius:

```tsx
function GradientButton({
  children,
  onClick,
  disabled,
  className,
}: {
  children: React.ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "group relative inline-flex w-full cursor-pointer items-center justify-center rounded-full p-[2px] transition-all duration-200",
        "bg-gradient-to-br from-[#5A8250] via-[#2873D7] to-[#FB873F]",
        disabled && "pointer-events-none opacity-50",
        className,
      )}
    >
      <span
        className={cn(
          "flex w-full items-center justify-center rounded-full bg-white px-8 py-2 font-mono text-[15px] uppercase leading-[1.6] tracking-[0.01em] text-black transition-all duration-200",
          "group-hover:bg-transparent group-hover:text-white",
        )}
      >
        {children}
      </span>
    </button>
  );
}
```

Note: The gradient uses Speakeasy brand language colors (Terraform `#5A8250`, Java `#2873D7`, Ruby `#FB873F`).

- [ ] **Step 2: Replace the Moonshine Button in LoginSection**

In `LoginSection`, replace:
```tsx
<div className="relative z-10">
  <Button variant="brand" onClick={handleLogin}>
    Login
  </Button>
</div>
```

With:
```tsx
<GradientButton onClick={handleLogin}>
  Login
</GradientButton>
```

Keep the Moonshine `Button` import at the top — it's still used by `RegisterSection` in the same file.

- [ ] **Step 3: Verify button renders and type-checks**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: No type errors.

Visually verify: Login button should show as a pill with a gradient border (green→blue→orange), white interior, dark text. On hover, the interior fills with the gradient and text turns white.

- [ ] **Step 4: Commit**

```bash
git add client/dashboard/src/pages/login/components/login-section.tsx
git commit -m "feat(login): replace login button with gradient-outline style"
```

---

## Task 3: Pulse Connector Animation (Left Pane)

**Files:**
- Modify: `client/dashboard/src/pages/login/components/platform-diagram.tsx`

- [ ] **Step 1: Add the PulseConnector component**

Add below the `FeatureBar` component in `platform-diagram.tsx`. The file already imports `motion` from `"motion/react"`.

```tsx
const PULSE_COLORS = [
  BRAND_COLORS.green,  // #5A8250
  BRAND_COLORS.blue,   // #2873D7
  BRAND_COLORS.orange, // #FB873F
];

function PulseConnector({ delay = 0 }: { delay?: number }) {
  return (
    <div className="relative flex h-6 w-2 items-center justify-center overflow-hidden">
      {PULSE_COLORS.map((color, i) => (
        <motion.div
          key={i}
          className="absolute h-1 w-1 rounded-full"
          style={{ backgroundColor: color }}
          initial={{ y: 12, opacity: 0 }}
          animate={{ y: -12, opacity: [0, 1, 1, 0] }}
          transition={{
            duration: 2.5,
            delay: delay + i * 0.6,
            repeat: Infinity,
            ease: "easeInOut",
          }}
        />
      ))}
    </div>
  );
}
```

Note: `BRAND_COLORS` already exists in this file but only has `green`, `blue`, and `orange`. Verify the object has all three keys — if `orange` is missing, add it: `orange: "#FB873F"`.

- [ ] **Step 2: Update BRAND_COLORS if needed**

The current `BRAND_COLORS` in `platform-diagram.tsx` has `green: "#5A8250"`, `blue: "#2873D7"`, `orange: "#FB873F"`. Verify all three exist. If only a subset exists, add the missing ones.

- [ ] **Step 3: Replace static connection lines with PulseConnector**

There are 3 `motion.div` elements that serve as connection lines (each has `className="w-px h-6 bg-slate-300 origin-top"`). Replace each with `PulseConnector`, using staggered delays — bottom-up cascade:

**Connector 1** (between ProductWithChat and Chat Backend, ~line 246-251):
```tsx
<PulseConnector delay={1.4} />
```

**Connector 2** (between Chat Backend and Tool Management, ~line 319-323):
```tsx
<PulseConnector delay={1.1} />
```

**Connector 3** (between Tool Management and Data Sources, ~line 399-404):
```tsx
<PulseConnector delay={0.8} />
```

The delays create bottom-up flow: Connector 3 starts first (0.8s), then 2 (1.1s), then 1 (1.4s). These delays are after the initial fade-in animations complete.

- [ ] **Step 4: Verify animation renders**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: No type errors.

Visually verify: Small colored dots should flow upward through each connection line in the platform diagram, with a bottom-to-top cascade timing.

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/pages/login/components/platform-diagram.tsx
git commit -m "feat(login): add pulse flow animation to platform diagram connectors"
```

---

## Task 4: Gradient Border Breathing Animation (Left Pane)

**Files:**
- Modify: `client/dashboard/src/pages/login/components/platform-diagram.tsx`

- [ ] **Step 1: Add breathing animation to the gradient border div**

In the `PlatformDiagram` component, find the "Tool Management" card's gradient border element (~line 335):

```tsx
<div
  className="absolute -inset-[1.5px] rounded-lg"
  style={{
    background: `linear-gradient(135deg, ${BRAND_COLORS.green}, ${BRAND_COLORS.blue}, ${BRAND_COLORS.orange})`,
  }}
/>
```

Replace it with a `motion.div` to add the breathing animation:

```tsx
<motion.div
  className="absolute -inset-[1.5px] rounded-lg"
  style={{
    background: `linear-gradient(135deg, ${BRAND_COLORS.green}, ${BRAND_COLORS.blue}, ${BRAND_COLORS.orange})`,
  }}
  animate={{ opacity: [0.7, 1, 0.7] }}
  transition={{
    duration: 3,
    repeat: Infinity,
    ease: "easeInOut",
  }}
/>
```

- [ ] **Step 2: Add prefers-reduced-motion support**

Add a `useReducedMotion` hook usage at the top of `PlatformDiagram`:

```tsx
import { useReducedMotion } from "motion/react";

// Inside PlatformDiagram:
const prefersReducedMotion = useReducedMotion();
```

Then conditionally disable animations:
- For `PulseConnector`, pass a `disabled` prop and render a static grey line when `true`
- For the gradient border breathing, skip the `animate` prop when reduced motion is preferred

Update `PulseConnector` signature:
```tsx
function PulseConnector({ delay = 0, disabled = false }: { delay?: number; disabled?: boolean }) {
  if (disabled) {
    return <div className="h-6 w-px bg-slate-300" />;
  }
  // ... existing animation code
}
```

Pass `disabled={prefersReducedMotion ?? false}` to each `PulseConnector`.

For the gradient border:
```tsx
<motion.div
  className="absolute -inset-[1.5px] rounded-lg"
  style={{
    background: `linear-gradient(135deg, ${BRAND_COLORS.green}, ${BRAND_COLORS.blue}, ${BRAND_COLORS.orange})`,
  }}
  animate={prefersReducedMotion ? undefined : { opacity: [0.7, 1, 0.7] }}
  transition={{
    duration: 3,
    repeat: Infinity,
    ease: "easeInOut",
  }}
/>
```

- [ ] **Step 3: Verify type-check and visual**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: No type errors.

Visually verify: The gradient border around "Tool Management" gently pulses between 70% and 100% opacity on a 3-second loop.

- [ ] **Step 4: Commit**

```bash
git add client/dashboard/src/pages/login/components/platform-diagram.tsx
git commit -m "feat(login): add gradient border breathing animation with reduced-motion support"
```

---

## Task 5: Final Verification

- [ ] **Step 1: Run type check**

Run: `cd client/dashboard && pnpm tsc --noEmit`
Expected: PASS, no errors.

- [ ] **Step 2: Run lint**

Run: `cd client/dashboard && pnpm lint`
Expected: PASS (or only pre-existing warnings).

- [ ] **Step 3: Visual QA on login page**

Check all elements on the login page:
- Gram logo renders correctly
- Subtext displays
- Feature badges appear as grey-bordered pills in a row
- Login button has gradient outline, hover fills with gradient
- Left pane platform diagram shows animated pulse dots flowing upward
- Gradient border on Tool Management card breathes
- Social links (X, GitHub) still visible at bottom of left pane
- No layout shifts or overflow issues

- [ ] **Step 4: Check mobile responsiveness**

Resize browser to mobile width. Verify:
- Left pane is hidden (existing `hidden md:flex` class)
- Right pane badges wrap correctly
- Button is full width and usable

- [ ] **Step 5: Final commit if any fixes needed**

If any adjustments were made during QA, commit them:
```bash
git add -A
git commit -m "fix(login): polish login page revamp"
```
