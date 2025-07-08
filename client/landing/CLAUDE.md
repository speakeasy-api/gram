# Landing Page Development Guide

This is the Gram landing page built with Next.js, TypeScript, and Tailwind CSS. The codebase uses a composable component architecture designed for rapid page creation and consistent design.

## Quick Commands

- `npm run dev` - Start development server
- `npm run build` - Build for production
- `npm run lint` - Run ESLint
- `pnpm add <package>` - Add new dependencies (use pnpm, not npm)

## Composable Component System

We've built a flexible component system using **Radix Slot** for maximum composability. Use these instead of creating custom components:

### Core Layout Components

```tsx
import { Section, Container, Grid, Flex } from "./components/sections";

// Responsive section wrapper with consistent spacing
<Section size="md" background="white">
  <Container>
    {/* Content */}
  </Container>
</Section>

// Grid layouts for complex arrangements
<Grid cols="hero" gap={12} align="center">
  <div>Left content</div>
  <div>Right content</div>
</Grid>

// Flexible box layouts
<Flex direction="col" align="center" gap={8}>
  <h1>Title</h1>
  <p>Description</p>
</Flex>
```

### Typography Components

```tsx
import { Heading, Text } from "./components/sections";

// Consistent headings with design system scales
<Heading size="hero" weight="light" color="white">
  Page Title
</Heading>

// Body text with proper sizing and colors
<Text size="description" color="muted" leading="relaxed">
  Description text
</Text>
```

### Interactive Components

```tsx
import { Badge, ButtonGroup, CommunityBadge } from "./components/sections";

// Gradient or simple badges
<Badge variant="gradient">New Feature</Badge>

// Button groups with consistent styling
<ButtonGroup
  buttons={[
    { text: "Primary Action", href: "/signup", variant: "rainbow-light" },
    { text: "Secondary Action", href: "/docs", variant: "outline" }
  ]}
/>

// Community avatars + text
<CommunityBadge text="Join 5000+ developers" />
```

## Component Variants Reference

### Section Sizes

- `none` - No padding (0px) - **Use for custom layouts**
- `xs` - Minimal padding (4-8px)
- `sm` - Small padding (6-10px) - **Use this for most sections**
- `md` - Medium padding (8-12px) - **Use for sections needing more breathing room**
- `lg` - Large padding (10-16px)
- `xl` - Extra large (12-20px)
- `hero` - Full height hero sections (no vertical padding)

### Section Backgrounds

- `white` - Default white background
- `black` - Dark background (use `color="white"` for text)
- `neutral` - Light gray background
- `transparent` - No background

### Heading Sizes

- `hero` - Largest text for hero sections (4xl-7xl responsive)
- `display` - Section headers (display-sm to display-lg)
- `h1`, `h2`, `h3`, `h4` - Standard heading sizes

### Text Sizes

- `hero` - Large descriptive text for heroes (lg-2xl)
- `description` - Section descriptions (base-lg)
- `base` - Standard body text
- `sm` - Small text for labels/captions

### Grid Patterns

- `hero` - Two-column hero layout (1 col mobile, 2 col desktop)
- `responsive` - Auto-responsive grid (1-2-3 cols based on screen)
- `auto` - Auto-fit with min 300px width
- `1`, `2`, `3`, `4` - Fixed column counts

## Common Page Patterns

### Page Structure with Proper Spacing

```tsx
export default function Page() {
  return (
    <Flex direction="col" gap={0} className="min-h-screen">
      {/* Header */}
      <header className="header-base">...</header>
      
      {/* Main content with consistent section spacing */}
      <Flex direction="col" gap={0} className="flex-1 space-y-16 sm:space-y-20 lg:space-y-24">
        <Section size="hero">...</Section>
        <Section size="sm">...</Section>
        <Section size="sm">...</Section>
        <Section size="md" background="black">...</Section>
        <Section size="none" asChild>
          <footer>...</footer>
        </Section>
      </Flex>
    </Flex>
  );
}
```

### Hero Section

```tsx
<Section size="hero">
  <Container>
    <Grid cols="hero" gap={12} align="center" className="lg:gap-16">
      <Flex direction="col" gap={6} className="lg:gap-8 py-8 lg:py-0">
        <Badge variant="gradient">New Feature</Badge>
        <Heading size="hero" weight="light">Your Amazing Product</Heading>
        <Text size="hero" color="muted">Product description</Text>
        <ButtonGroup buttons={[...]} />
      </Flex>
      <div className="flex items-center justify-center">
        <YourVisualComponent />
      </div>
    </Grid>
  </Container>
</Section>
```

### Feature Section

```tsx
<Section>
  <Container>
    <Flex direction="col" align="center" className="text-center mb-16">
      <Heading size="display">Section Title</Heading>
      <Text size="description" color="muted" className="max-w-2xl mx-auto">
        Section description
      </Text>
    </Flex>
    {/* Feature content */}
  </Container>
</Section>
```

### CTA Section

```tsx
<Section background="black" size="lg">
  <Container>
    <Flex direction="col" align="center" className="text-center">
      <Heading size="display" color="white" className="mb-12 max-w-4xl mx-auto">
        Call to Action Title
      </Heading>
      <ButtonGroup buttons={[...]} />
    </Flex>
  </Container>
</Section>
```

### Community Footer

```tsx
<Section asChild>
  <footer className="relative bg-neutral-100 border-t border-neutral-200 min-h-[600px] flex flex-col justify-center items-center">
    <Container size="2xl" className="relative z-20">
      <Flex direction="col" align="center" className="py-40">
        <CommunityBadge className="mb-12" />
        <Heading size="display" weight="light" align="center" className="mb-12">
          Join the Community
        </Heading>
        <ButtonGroup buttons={[...]} />
      </Flex>
    </Container>
  </footer>
</Section>
```

## Design System Guidelines

### Colors

- Use semantic color props: `default`, `muted`, `white` instead of specific Tailwind classes
- For custom colors, use the neutral palette: `text-neutral-900`, `text-neutral-600`, etc.

### Spacing

- **Page-level spacing**: Wrap sections in a Flex container with `space-y-16 sm:space-y-20 lg:space-y-24` for consistent gaps between sections
- **Component spacing**: Use gap props: `gap={4}`, `gap={8}`, `gap={12}` for internal spacing
- **Section padding**: Use minimal padding (`size="sm"` or `size="md"`) for internal content spacing
- For custom spacing, use Tailwind's 4px scale: `mb-4`, `py-8`, `gap-12`

### Responsive Design

- Components are mobile-first by default
- Use responsive props when available: `cols="hero"` automatically adapts
- For custom responsive: `className="text-base lg:text-xl"`

### Using `asChild` Prop

The `asChild` prop lets you change the underlying HTML element while keeping component styles:

```tsx
// Renders as <main> instead of <section>
<Section asChild>
  <main className="custom-main">
    <Heading asChild>
      <h1>Custom H1 with design system styles</h1>
    </Heading>
  </main>
</Section>
```

## File Structure

```
app/components/sections/
├── Section.tsx          # Main section wrapper
├── Container.tsx        # Content containers
├── Typography.tsx       # Heading & Text components
├── Layout.tsx          # Grid & Flex components
├── Primitives.tsx      # Badge, ButtonGroup, etc.
└── index.ts           # Exports all components
```

## Best Practices

### DO ✅

- Use the composable components for consistency
- Wrap content in `<Container>` for proper max-width and padding
- Use semantic color and size props instead of Tailwind classes
- Center text sections with `mx-auto` on text elements
- Use `Section` for all page sections to maintain consistent spacing

### DON'T ❌

- Create custom layout components - use the existing Grid/Flex system
- Use arbitrary Tailwind spacing - stick to the component gap props
- Mix component props with conflicting Tailwind classes
- Forget to wrap sections in `<Container>` - content will stretch full width

### Performance

- All components use CVA (Class Variance Authority) for optimized CSS output
- Radix Slot enables composition without wrapper div overhead
- Components are tree-shakeable - only import what you use
