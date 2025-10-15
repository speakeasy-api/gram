# WebGL ASCII Video Effect

A simplified port of the WebGL ASCII shader from the marketing site, designed for use in the Gram dashboard onboarding wizard.

## Overview

This implementation provides a clean, simplified WebGL ASCII effect that renders video through an ASCII shader. It uses Three.js, React Three Fiber, and custom GLSL shaders to create a retro terminal-style visual effect.

## Components

### AsciiVideo

The main wrapper component that sets up the Three.js canvas and renders the ASCII effect.

**Props:**
- `videoSrc: string` - Path to video file (relative to public/)
- `className?: string` - CSS classes for the container
- `fontSize?: number` - Font size for ASCII characters (default: 10)
- `cellSize?: number` - Size of each ASCII character cell (default: 8)
- `color?: string` - Color of ASCII characters (default: "#00ff00")
- `invert?: boolean` - Invert brightness mapping (default: false)

### AsciiEffect

The core component that applies the ASCII shader to a texture.

### useVideoTexture

A custom hook that loads and manages video textures.

## Installation

The required dependencies have been added to `package.json`:

```bash
npm install
```

Dependencies added:
- `@react-three/fiber@^8.18.7` - React renderer for Three.js
- `@react-three/drei@^9.117.3` - Useful helpers for React Three Fiber
- `three@^0.171.0` - Three.js core library

## Setup

1. **Copy the video asset:**
   ```bash
   cp /Users/farazkhan/Code/marketing-site/public/webgl/stars.mp4 \
      /Users/farazkhan/Code/gram/client/dashboard/public/webgl/stars.mp4
   ```

2. **Import and use the component:**
   ```tsx
   import { AsciiVideo } from "@/components/webgl";

   function MyComponent() {
     return (
       <AsciiVideo
         videoSrc="/webgl/stars.mp4"
         className="w-full h-full"
         cellSize={8}
         fontSize={10}
         color="#00ff00"
       />
     );
   }
   ```

## Usage Examples

### As Onboarding Background

```tsx
import { OnboardingAsciiBackground } from "@/components/webgl";

function OnboardingWizard() {
  return (
    <div className="relative min-h-screen">
      <OnboardingAsciiBackground />
      <div className="relative z-10">
        {/* Your onboarding content */}
      </div>
    </div>
  );
}
```

### Custom Styled Effect

```tsx
import { AsciiVideo } from "@/components/webgl";

function CustomEffect() {
  return (
    <div className="w-full h-96 rounded-lg overflow-hidden">
      <AsciiVideo
        videoSrc="/webgl/stars.mp4"
        className="w-full h-full"
        cellSize={6}
        fontSize={8}
        color="#00ffff"
        invert={true}
      />
    </div>
  );
}
```

## How It Works

1. **Video Loading**: The `useVideoTexture` hook creates an HTML5 video element, loads the video file, and converts it to a Three.js VideoTexture.

2. **ASCII Shader**: The fragment shader samples the video texture, converts each pixel to grayscale, and maps brightness levels to ASCII characters:
   - Very bright: `@`
   - Bright: `#`
   - Medium-bright: `$`
   - Medium: `&`
   - Medium-dark: `+`
   - Dark: `=`
   - Very dark: `-`
   - Black: ` ` (space)

3. **Rendering**: React Three Fiber sets up a WebGL context and renders the shader on a full-screen plane geometry.

## Simplifications from Marketing Site

This implementation is simplified compared to the marketing site version:

- **No fluid simulation** - Just video + ASCII shader
- **No scroll synchronization** - Plays independently
- **No complex store management** - Simple prop-based configuration
- **No debug tools** - Lightweight production-ready code
- **No external dependencies** on marketing site utilities

## Performance Considerations

- Video decoding happens on the GPU
- ASCII character mapping is done in the fragment shader for performance
- The canvas is set to `antialias: false` for better performance
- Video is muted and plays inline to avoid mobile restrictions

## Customization

### Changing Colors

```tsx
<AsciiVideo color="#ff00ff" /> // Magenta
<AsciiVideo color="#00ffff" /> // Cyan
<AsciiVideo color="#ffffff" /> // White
```

### Adjusting Character Density

```tsx
// More dense (smaller cells)
<AsciiVideo cellSize={4} fontSize={6} />

// Less dense (larger cells)
<AsciiVideo cellSize={12} fontSize={14} />
```

### Inverting Brightness

```tsx
// Invert bright/dark mapping
<AsciiVideo invert={true} />
```

## Troubleshooting

### Video not loading

1. Verify the video file exists at `/public/webgl/stars.mp4`
2. Check browser console for video loading errors
3. Ensure video format is compatible (H.264 MP4 recommended)

### Performance issues

1. Reduce `cellSize` to render fewer ASCII characters
2. Check video resolution (lower resolution = better performance)
3. Ensure hardware acceleration is enabled in browser

### Blank screen

1. Verify all npm dependencies are installed
2. Check for JavaScript errors in console
3. Ensure the container has explicit width/height
