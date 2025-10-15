# WebGL Assets

## Required Video Assets

### stars.mp4

This directory should contain the `stars.mp4` video file for the ASCII video effect.

To copy the file from the marketing site:

```bash
cp /Users/farazkhan/Code/marketing-site/public/webgl/stars.mp4 /Users/farazkhan/Code/gram/client/dashboard/public/webgl/stars.mp4
```

The video should be:
- Format: MP4
- Looping: Yes
- Audio: Not required (will be muted)
- Aspect ratio: Any (will be scaled to fit)

## Usage

Once the video asset is in place, you can use the AsciiVideo component:

```tsx
import { AsciiVideo } from "@/components/webgl";

<AsciiVideo
  videoSrc="/webgl/stars.mp4"
  className="w-full h-full"
  cellSize={8}
  fontSize={10}
  color="#00ff00"
  invert={false}
/>
```
