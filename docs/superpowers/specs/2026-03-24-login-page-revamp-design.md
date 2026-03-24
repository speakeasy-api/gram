# Login Page Revamp Design

## Overview

Revamp the Gram login page (`client/dashboard/src/pages/login/`) to better communicate what the platform does, align with Speakeasy brand guidelines, and replace the static left-pane diagram with a continuously animated "pulse flow" visualization.

**What stays**: Two-pane layout, Gram logo (vertical), X + GitHub social links, existing auth logic, error handling, register flow.

**What changes**: Right-pane copy/badges/button, left-pane animation.

---

## Right Pane (`login-section.tsx`)

### Copy

Use the subtext from `main` at implementation time (currently: "Embed intelligent, performant chat and agents directly into your product. Give users AI that understands your data and takes action. Powered by MCP."). Update to whatever latest copy is on `main` when implementing.

### Feature Badges

Add a row of pill-shaped badges below the subtext, above the login button. These are non-interactive, purely visual.

**Layout:**
- Horizontal flex row, centered, wrapping on mobile
- Gap: `8px`
- Placed between subtext `<p>` and login button

**Badge style:**
- `border: 1px solid #D3D3D3` (brand supporting grey)
- `border-radius: 9999px` (pill)
- `padding: 4px 12px`
- `background: transparent`
- `font-family: font-mono` (Tailwind class — falls back to system monospace; ABC Diatype Mono is the brand ideal but is not loaded in the dashboard app)
- `font-size: 12px`
- `text-transform: uppercase`
- `letter-spacing: 0.01em`
- `color: #8B8684` (brand muted text)

**Badge labels** (map to Gram platform features):
- `MCP Servers`
- `Observability`
- `Auth & OAuth`
- `Tool Curation`

Labels are subject to change — the component should accept an array of strings.

### Gradient-Outline Login Button

Replace the Moonshine `Button variant="brand"` import (from `@speakeasy-api/moonshine`) in `LoginSection` with a custom gradient-outlined button. **Note:** `RegisterSection` in the same file also uses Moonshine's `Button` — keep the Moonshine import for `RegisterSection`; only the login button changes.

**Default state:**
- `background: white`
- `border: 2px solid transparent` with gradient border using the Gram brand gradient: `linear-gradient(135deg, #5A8250, #2873D7, #FB873F)`
- `border-radius: 9999px` (pill)
- `color: #000000`
- `font-family: font-mono` (Tailwind class, matching the local `button.tsx` style)
- `font-size: 15px`, `text-transform: uppercase`, `tracking-[0.01em]`
- `padding: 8px 32px`
- `width: 100%` (full width within the `max-w-xs` container, matching visual weight of the page)

**Hover state:**
- Background fills with the gradient
- Text becomes white
- Smooth transition (~200ms)

**Disabled state** (for reusability):
- `opacity: 0.5`, `pointer-events: none`, no hover effect

**Implementation note:** CSS `border-image` doesn't work with `border-radius`. Use the pseudo-element approach: outer container with gradient background, inner element with white background and border-radius, hover removes inner background.

### Structure (top to bottom, centered, max-w-xs)

1. `GramLogo variant="vertical"` (unchanged)
2. Subtext `<p>` (copy from main)
3. Badge row (new)
4. Error message (unchanged, conditional — keeps current position before button)
5. Gradient-outline login button (new)

---

## Left Pane (`journey-demo.tsx` + `platform-diagram.tsx`)

### Pulse Flow Animation

Enhance the existing `PlatformDiagram` with continuously animated particles flowing upward through the connection lines.

#### Connection Line Pulses

Each of the 3 vertical connection lines (`w-px h-6`) between diagram sections gets replaced with a pulse connector component.

**Pulse connector spec:**
- Container: `width: 1px, height: 24px` (same as current)
- Contains 3-4 small dots (`width: 4px, height: 4px, border-radius: 50%`)
- Dots cycle through brand colors: `#5A8250` (green), `#2873D7` (blue), `#FB873F` (orange)
- Animation: dots translate from bottom to top of the connector, `repeat: Infinity`
- Each dot fades in at the bottom edge, fades out at the top edge (`opacity: 0 → 1 → 0`)
- Duration: ~2.5 seconds per cycle
- Stagger: Each connector starts 0.3s after the one below it, creating a cascade effect (data flows upward from sources → through Gram → into the app)
- Stagger order: Connector 3 (above Data Sources) → Connector 2 (above Tool Management) → Connector 1 (above Chat Backend) — bottom-up cascade
- Implementation: Use `motion` from `"motion/react"` (existing import in the file). `animate` with `transition: { repeat: Infinity, duration: 2.5, ease: "easeInOut" }`
- **Accessibility:** Wrap animations in a `prefers-reduced-motion` check. When reduced motion is preferred, show static dots (no animation) or a simple static line.

#### Gradient Border Pulse

The "Tool Management" card's gradient border (already `linear-gradient(135deg, #5A8250, #2873D7, #FB873F)`) gets a subtle breathing animation.

- `opacity` oscillates `0.7 → 1 → 0.7` on a 3-second infinite loop
- Uses Framer Motion `animate={{ opacity: [0.7, 1, 0.7] }}` with `transition: { repeat: Infinity, duration: 3, ease: "easeInOut" }`

#### Everything Else Unchanged

- `DottedBackground` SVG pattern
- Gradient overlays (blue/emerald)
- Top/bottom gradient fades
- Social links (X, GitHub) at bottom
- `ProductWithChat` mockup at top
- `FeatureBar` items within cards
- Initial staggered fade-in animations on mount

---

## Brand Alignment (Speakeasy Brand Skill)

Per the Speakeasy brand guidelines (`speakeasy-brand-skill/.claude/skills/speakeasy-brand.md`):

| Element | Brand Rule | Our Application |
|---|---|---|
| Badge typography | Captions use ABC Diatype Mono, uppercase | Badges use monospace uppercase, 12px |
| Colors | Language colors for accents (15% of palette) | Gradient button uses Terraform/Java/Ruby subset |
| Muted text | `#8B8684` for secondary labels | Badge text color |
| Supporting grey | `#D3D3D3` for medium grey | Badge border color |
| Design philosophy | "Precise, warm, confident, developer-native" | Terse copy, clean layout, animated but restrained |
| Voice | Short declarative statements | Developer-direct copy tone |

**Not applicable to login page** (product UI, not marketing asset): RGB gradient bar at bottom, pattern system, Tobias headlines, logo placement rules. The login page uses the Gram logo and Moonshine design system, not Speakeasy marketing templates.

---

## Files to Modify

| File | Change |
|---|---|
| `client/dashboard/src/pages/login/components/login-section.tsx` | New badges, gradient button in `LoginSection`. Keep Moonshine `Button` import for `RegisterSection`. |
| `client/dashboard/src/pages/login/components/platform-diagram.tsx` | New `PulseConnector` component, gradient border breathing animation |

---

## Out of Scope

- Register flow UI changes
- Auth backend changes
- Mobile-specific layout changes (existing responsive classes stay)
- PromptsSection (marquee cards) — remains unused
- Copy finalization (deferred to latest main)
