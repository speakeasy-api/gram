# WebGL ASCII Shader - Quick Start Guide

## 1. Install Dependencies

```bash
cd /Users/farazkhan/Code/gram/client/dashboard
npm install
```

This will install:
- `@react-three/fiber@^8.18.7`
- `@react-three/drei@^9.117.3`
- `three@^0.171.0`

## 2. Copy Video Asset

```bash
cp /Users/farazkhan/Code/marketing-site/public/webgl/stars.mp4 \
   /Users/farazkhan/Code/gram/client/dashboard/public/webgl/stars.mp4
```

## 3. Import and Use

```tsx
import { AsciiVideo } from "@/components/webgl";

function MyComponent() {
  return (
    <AsciiVideo
      videoSrc="/webgl/stars.mp4"
      className="w-full h-full"
    />
  );
}
```

## For Onboarding Wizard

```tsx
import { OnboardingAsciiBackground } from "@/components/webgl";

function OnboardingWizard() {
  return (
    <div className="relative min-h-screen">
      <OnboardingAsciiBackground />
      <div className="relative z-10">
        {/* Your wizard content */}
      </div>
    </div>
  );
}
```

## Customization Options

```tsx
<AsciiVideo
  videoSrc="/webgl/stars.mp4"
  className="w-full h-96"
  cellSize={6}      // Smaller = more dense
  fontSize={8}      // Character size
  color="#00ffff"   // Cyan color
  invert={true}     // Invert brightness
/>
```

## Files Location

All components are in:
```
/Users/farazkhan/Code/gram/client/dashboard/src/components/webgl/
```

See `README.md` in that directory for full documentation.
