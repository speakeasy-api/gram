# WebGL ASCII Shader Implementation

## Summary

Successfully ported a simplified version of the WebGL ASCII shader from the marketing site to the Gram dashboard for use in the onboarding wizard.

## What Was Done

### 1. Dependencies Added ✅

Added to `/Users/farazkhan/Code/gram/client/dashboard/package.json`:
- `@react-three/fiber@^8.18.7` - React renderer for Three.js
- `@react-three/drei@^9.117.3` - Helper utilities for React Three Fiber
- `three@^0.171.0` - Three.js WebGL library

### 2. Directory Structure Created ✅

```
/Users/farazkhan/Code/gram/client/dashboard/
├── src/components/webgl/
│   ├── ascii-effect.tsx        # Core ASCII shader component
│   ├── ascii-video.tsx         # Main wrapper component
│   ├── video.tsx               # Video texture hook
│   ├── example-usage.tsx       # Usage examples
│   ├── index.tsx               # Exports
│   └── README.md               # Documentation
└── public/webgl/
    └── README.md               # Video asset instructions
```

### 3. Core Components Created ✅

#### `ascii-effect.tsx`
- Custom GLSL shaders (vertex + fragment)
- Converts video frames to ASCII characters based on brightness
- Character mapping: `@` (brightest) → `#` → `$` → `&` → `+` → `=` → `-` → ` ` (darkest)
- Configurable colors, cell size, and invert mode

#### `video.tsx`
- `useVideoTexture` hook for loading video files
- Handles video element lifecycle
- Converts HTML5 video to Three.js VideoTexture
- Auto-plays, loops, and mutes video

#### `ascii-video.tsx`
- Simplified wrapper component
- Sets up Three.js Canvas
- Manages camera and GL settings
- Provides clean API for consumers

#### `example-usage.tsx`
- `OnboardingAsciiBackground` - Full-screen background example
- `CustomAsciiEffect` - Contained effect with custom styling
- Demonstrates different configurations

### 4. Simplifications Made ✅

Removed complexity from marketing site version:
- No fluid simulation
- No scroll synchronization
- No complex store management (zustand not needed for this)
- No debug tools
- Pure prop-based configuration

### 5. Documentation Created ✅

- Component README with usage examples
- Public asset README with copy instructions
- Implementation summary (this file)
- Inline code comments

## Next Steps

### 1. Install Dependencies

```bash
cd /Users/farazkhan/Code/gram/client/dashboard
npm install
```

### 2. Copy Video Asset

```bash
cp /Users/farazkhan/Code/marketing-site/public/webgl/stars.mp4 \
   /Users/farazkhan/Code/gram/client/dashboard/public/webgl/stars.mp4
```

### 3. Use in Onboarding Wizard

```tsx
import { OnboardingAsciiBackground } from "@/components/webgl";

function OnboardingPage() {
  return (
    <div className="relative min-h-screen">
      <OnboardingAsciiBackground />
      <div className="relative z-10">
        {/* Your onboarding wizard content */}
      </div>
    </div>
  );
}
```

## Usage Examples

### Basic Usage

```tsx
import { AsciiVideo } from "@/components/webgl";

<AsciiVideo
  videoSrc="/webgl/stars.mp4"
  className="w-full h-full"
/>
```

### Custom Styling

```tsx
<AsciiVideo
  videoSrc="/webgl/stars.mp4"
  className="w-full h-96"
  cellSize={6}
  fontSize={8}
  color="#00ffff"
  invert={true}
/>
```

### As Background

```tsx
<div className="fixed inset-0 -z-10">
  <AsciiVideo
    videoSrc="/webgl/stars.mp4"
    className="w-full h-full"
    color="#00ff00"
  />
</div>
```

## Component API

### AsciiVideo Props

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `videoSrc` | `string` | required | Path to video file (relative to public/) |
| `className` | `string` | `""` | CSS classes for container |
| `fontSize` | `number` | `10` | Font size for ASCII characters |
| `cellSize` | `number` | `8` | Size of each ASCII character cell |
| `color` | `string` | `"#00ff00"` | Color of ASCII characters |
| `invert` | `boolean` | `false` | Invert brightness mapping |

## Files Created

1. `/Users/farazkhan/Code/gram/client/dashboard/package.json` (modified)
2. `/Users/farazkhan/Code/gram/client/dashboard/src/components/webgl/ascii-effect.tsx`
3. `/Users/farazkhan/Code/gram/client/dashboard/src/components/webgl/ascii-video.tsx`
4. `/Users/farazkhan/Code/gram/client/dashboard/src/components/webgl/video.tsx`
5. `/Users/farazkhan/Code/gram/client/dashboard/src/components/webgl/example-usage.tsx`
6. `/Users/farazkhan/Code/gram/client/dashboard/src/components/webgl/index.tsx`
7. `/Users/farazkhan/Code/gram/client/dashboard/src/components/webgl/README.md`
8. `/Users/farazkhan/Code/gram/client/dashboard/public/webgl/README.md`

## Technical Details

### Shader Implementation

The ASCII effect uses a custom GLSL fragment shader that:
1. Samples the video texture at each pixel
2. Converts RGB to grayscale using standard luminance weights (0.3R + 0.59G + 0.11B)
3. Maps grayscale values to ASCII character bitmaps
4. Renders character shapes as colored pixels

### Performance

- Video decoding: GPU-accelerated
- ASCII mapping: Per-fragment shader operation
- Canvas settings: Antialiasing disabled for performance
- Video playback: Muted and inline for mobile compatibility

### Browser Compatibility

Works on all modern browsers that support:
- WebGL 1.0+
- HTML5 Video
- ES6+ JavaScript

## Troubleshooting

### Video Not Loading
- Check file exists at `/public/webgl/stars.mp4`
- Verify video format (H.264 MP4 recommended)
- Check browser console for errors

### Performance Issues
- Reduce `cellSize` (fewer characters = better performance)
- Use lower resolution video
- Ensure hardware acceleration enabled

### TypeScript Errors
- Run `npm install` to install all dependencies
- Check that `@types/three` is available via drei

## Testing

After implementation, test:
1. Video loads and plays automatically
2. ASCII effect renders correctly
3. Component responds to prop changes
4. No memory leaks on mount/unmount
5. Works on mobile devices
