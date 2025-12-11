import React, { forwardRef, useEffect, useMemo } from "react";
import { useAsciiStore } from "../../hooks/use-ascii-store";
import { glsl } from "@/lib/webgl/utils";
import { useTheme } from "next-themes";
import { BlendFunction, Effect } from "postprocessing";
import * as THREE from "three";
import { useFrame } from "@react-three/fiber";
import { useWebGLStore } from "../../store";

// https://github.com/pmndrs/postprocessing/wiki/Custom-Effects
// https://docs.pmnd.rs/react-postprocessing/effects/custom-effects

// Create a simple empty texture for fluid density (stub)
const createEmptyTexture = () => {
  const canvas = document.createElement("canvas");
  canvas.width = 1;
  canvas.height = 1;
  const ctx = canvas.getContext("2d")!;
  ctx.fillStyle = "black";
  ctx.fillRect(0, 0, 1, 1);
  const texture = new THREE.CanvasTexture(canvas);
  return texture;
};

const emptyFluidTexture = createEmptyTexture();

const fragmentShader = glsl`
  precision mediump float;

  // Uniform declarations
  uniform float uCharLength;
  uniform float uCharSize;
  uniform sampler2D uFont;
  uniform bool uOverwriteColor;
  uniform vec3 uColor;
  uniform bool uPixels;
  uniform bool uGreyscale;
  uniform bool uMatrix;
  uniform float uDevicePixelRatio;
  uniform bool uDarkTheme;
  uniform vec2 uScrollOffset;
  uniform vec2 uResolution;
  uniform sampler2D uFluidDensity;
  uniform sampler2D uColorWheel;
  uniform float uTime;

  const vec2 SIZE = vec2(16.0);
  const float SCREEN_WIDTH_BASE = 1720.0;

  float charSizeToVw(float value, float screenWidth) {
    return clamp(value * screenWidth / SCREEN_WIDTH_BASE, 6.0, uCharSize);
  }

  // Utility functions
  float grayscale(vec3 c) {
    // Standard luminance weights for grayscale conversion
    return dot(c, vec3(0.299, 0.587, 0.114));
  }

  float random(float x) {
    return fract(sin(x) * 1e4);
  }

  float valueRemap(
    float value,
    float minIn,
    float maxIn,
    float minOut,
    float maxOut
  ) {
    return minOut + (value - minIn) * (maxOut - minOut) / (maxIn - minIn);
  }

  vec3 hsv2rgb(vec3 c) {
    vec4 K = vec4(1.0, 2.0 / 3.0, 1.0 / 3.0, 3.0);
    vec3 p = abs(fract(c.xxx + K.xyz) * 6.0 - K.www);
    return c.z * mix(K.xxx, clamp(p - K.xxx, 0.0, 1.0), c.y);
  }

  vec3 rgb2hsv(vec3 c) {
    vec4 K = vec4(0.0, -1.0 / 3.0, 2.0 / 3.0, -1.0);
    vec4 p = mix(vec4(c.bg, K.wz), vec4(c.gb, K.xy), step(c.b, c.g));
    vec4 q = mix(vec4(p.xyw, c.r), vec4(c.r, p.yzx), step(p.x, c.r));

    float d = q.x - min(q.w, q.y);
    float e = 1.0e-10;
    return vec3(abs(q.z + (q.w - q.y) / (6.0 * d + e)), d / (q.x + e), q.x);
  }

  vec3 saturateColor(vec3 color, float saturation) {
    // Convert to HSV
    vec3 hsv = rgb2hsv(color);
    // Increase saturation
    hsv.y = hsv.y * saturation;
    // Convert back to RGB
    return hsv2rgb(hsv);
  }

  vec3 acesFilm(vec3 color) {
    float a = 2.51;
    float b = 0.03;
    float c = 2.43;
    float d = 0.59;
    float e = 0.14;
    return clamp(
      color * (a * color + b) / (color * (c * color + d) + e),
      0.0,
      1.0
    );
  }

  void mainImage(const vec4 inputColor, const vec2 uv, out vec4 outputColor) {
    // Constants for ASCII character grid
    float cLength = SIZE.x * SIZE.y;

    // Calculate pixelization grid
    vec2 cell =
      resolution / charSizeToVw(uCharSize, uResolution.x) / uDevicePixelRatio;
    vec2 grid = 1.0 / cell;

    // fix uv grid to screen
    float adjustmentFactor = uScrollOffset.y;
    adjustmentFactor /= resolution.y;
    adjustmentFactor = mod(adjustmentFactor, grid.y / uDevicePixelRatio);
    adjustmentFactor *= uDevicePixelRatio;
    vec2 adjustedUv = uv;
    adjustedUv.y -= adjustmentFactor;

    vec2 pixelizationUv = grid * (floor(adjustedUv / grid) + 0.5);

    // Apply matrix effect if enabled
    if (uMatrix) {
      float noise = random(pixelizationUv.x);
      pixelizationUv = mod(
        pixelizationUv + vec2(0.0, time * abs(noise) * 0.1),
        2.0
      );
    }

    // Sample color from input buffer
    vec2 sampleUv = pixelizationUv;
    sampleUv.y += adjustmentFactor;
    vec4 color = texture2D(inputBuffer, sampleUv);

    // Sample fluid density for red tinting (simplified - mostly zero)
    vec4 densitySample = texture2D(uFluidDensity, sampleUv);
    float fluidDensity = length(densitySample.rgb);

    float densityMin = 10.0;
    float fluidActiveFactor = valueRemap(
      fluidDensity,
      densityMin,
      densityMin * 2.0,
      0.0,
      1.0
    );
    fluidActiveFactor = clamp(fluidActiveFactor, 0.0, 1.0);

    float fluidHue = valueRemap(fluidDensity, densityMin, 200.0, 0.0, 1.0);
    fluidHue = clamp(fluidHue, 0.0, 0.5);
    fluidHue -= uTime * 0.1;
    vec3 fluidColor = texture2D(uColorWheel, vec2(fluidHue, 0.5)).rgb;

    float fluidMultiplier = valueRemap(
      fluidDensity,
      densityMin,
      200.0,
      1.0,
      1.5
    );
    fluidMultiplier = clamp(fluidMultiplier, 1.0, 1.5);

    // Apply multiplier to lighten the color
    fluidColor = fluidColor * fluidMultiplier;

    // Apply simple tonemap to prevent overexposure and make colors prettier
    fluidColor = clamp(fluidColor, 0.0, 1.0);

    float gray = grayscale(color.rgb);

    // Calculate ASCII character index based on grayscale value
    float charIndex = floor(gray * (uCharLength - 0.01));
    float charIndexX = mod(charIndex, SIZE.x);
    float charIndexY = floor(charIndex / SIZE.y);

    // Calculate texture coordinates for ASCII character
    vec2 offset = vec2(charIndexX, charIndexY) / SIZE;
    vec2 asciiUv =
      mod(adjustedUv * (cell / SIZE), 1.0 / SIZE) -
      vec2(0.0, 1.0 / SIZE.y) -
      offset;

    float asciiChar = texture2D(uFont, asciiUv).r;

    // Handle transparency
    if (color.a == 0.0) {
      outputColor = vec4(0.0);
      return;
    }

    // Apply ASCII effect based on mode
    if (uPixels) {
      color.rgb = asciiChar > 0.0 ? vec3(1.0) : color.rgb;
      color.a = gray < 0.01 ? 0.0 : color.a;
    } else {
      vec3 invertedColor = uDarkTheme ? color.rgb : 1.0 - color.rgb;
      color.rgb = mix(vec3(0.0), invertedColor, asciiChar);
      color.a = asciiChar > 0.0 ? color.a : 0.0;
    }

    // Mix base color with fluid-based color when there's fluid activity
    vec3 charColor = mix(uColor, fluidColor, fluidActiveFactor);

    // Apply color overwrite if enabled
    if (uOverwriteColor && color.a > 0.0) {
      color.rgb = mix(vec3(0.0), charColor, asciiChar);
    }

    // Apply greyscale if enabled
    if (uGreyscale) {
      outputColor = vec4(vec3(gray), color.a);
    } else {
      outputColor = color;
    }
  }
`;

interface ASCIIEffectProps {
  colorWheelTexture: THREE.Texture;
  fontTexture: THREE.Texture;
  charSize: number;
  charLength: number;
  pixels: boolean;
  overwriteColor: boolean;
  color: THREE.Color;
  greyscale: boolean;
  matrix: boolean;
  devicePixelRatio: number;
  darkTheme: boolean;
  resolution: [number, number];
  scrollOffset: THREE.Vector2;
}

let uFont: THREE.Texture,
  uCharSize: number,
  uCharLength: number,
  uPixels: boolean,
  uOverwriteColor: boolean,
  uColor: THREE.Color,
  uGreyscale: boolean,
  uMatrix: boolean,
  uDevicePixelRatio: number,
  uDarkTheme: boolean,
  uResolution: [number, number],
  uScrollOffset: THREE.Vector2,
  uTime: number;

// Effect implementation
class ASCIIEffectImpl extends Effect {
  constructor({
    colorWheelTexture,
    fontTexture,
    charSize,
    charLength,
    pixels,
    overwriteColor,
    color,
    greyscale,
    matrix,
    devicePixelRatio,
    darkTheme,
    resolution,
    scrollOffset,
  }: ASCIIEffectProps) {
    super("ASCIIEffect", fragmentShader, {
      blendFunction: BlendFunction.SET,
      uniforms: new Map<string, THREE.Uniform<unknown>>([
        ["uFont", new THREE.Uniform(fontTexture)],
        ["uCharSize", new THREE.Uniform(charSize)],
        ["uPixels", new THREE.Uniform(pixels)],
        ["uCharLength", new THREE.Uniform(charLength)],
        ["uOverwriteColor", new THREE.Uniform(overwriteColor)],
        ["uColor", new THREE.Uniform(color)],
        ["uGreyscale", new THREE.Uniform(greyscale)],
        ["uMatrix", new THREE.Uniform(matrix)],
        ["uDevicePixelRatio", new THREE.Uniform(devicePixelRatio)],
        ["uDarkTheme", new THREE.Uniform(darkTheme)],
        ["uResolution", new THREE.Uniform(resolution)],
        ["uScrollOffset", new THREE.Uniform(scrollOffset)],
        ["uFluidDensity", new THREE.Uniform(emptyFluidTexture)],
        ["uColorWheel", new THREE.Uniform(colorWheelTexture)],
        ["uTime", new THREE.Uniform(0)],
      ]),
    });

    uFont = fontTexture;
    uCharSize = charSize;
    uCharLength = charLength;
    uPixels = pixels;
    uOverwriteColor = overwriteColor;
    uColor = color;
    uGreyscale = greyscale;
    uMatrix = matrix;
    uDevicePixelRatio = devicePixelRatio;
    uDarkTheme = darkTheme;
    uResolution = resolution;
    uScrollOffset = scrollOffset;
    uTime = 0;
  }

  update() {
    if (!this.uniforms) return;
    this.uniforms.get("uFont")!.value = uFont;
    this.uniforms.get("uCharSize")!.value = uCharSize;
    this.uniforms.get("uCharLength")!.value = uCharLength;
    this.uniforms.get("uPixels")!.value = uPixels;
    this.uniforms.get("uOverwriteColor")!.value = uOverwriteColor;
    this.uniforms.get("uColor")!.value = uColor;
    this.uniforms.get("uGreyscale")!.value = uGreyscale;
    this.uniforms.get("uMatrix")!.value = uMatrix;
    this.uniforms.get("uDevicePixelRatio")!.value = uDevicePixelRatio;
    this.uniforms.get("uDarkTheme")!.value = uDarkTheme;
    this.uniforms.get("uResolution")!.value = uResolution;
    this.uniforms.get("uScrollOffset")!.value = uScrollOffset;
    this.uniforms.get("uTime")!.value = uTime;
  }
}

const charSize = 9;
const charLength = 10;
const pixels = false;
const greyscale = false;
const overwriteColor = true;
const color = "#808080";
const matrix = false;

// Effect component
export const ASCIIEffect = forwardRef((_, ref) => {
  const { resolvedTheme } = useTheme();
  const fontTexture = useAsciiStore((state) => state.fontTexture);
  const setLength = useAsciiStore((state) => state.setLength);
  const darkTheme = useMemo(() => resolvedTheme === "dark", [resolvedTheme]);
  const screenWidth = useWebGLStore((state) => state.screenWidth);
  const screenHeight = useWebGLStore((state) => state.screenHeight);
  const devicePixelRatio = useWebGLStore((state) => state.dpr);

  useEffect(() => {
    if (!fontTexture) return;
    fontTexture.minFilter = fontTexture.magFilter = THREE.LinearFilter;
    fontTexture.wrapS = fontTexture.wrapT = THREE.RepeatWrapping;
    fontTexture.needsUpdate = true;
  }, [fontTexture]);

  const scrollOffset = useWebGLStore((state) => state.scrollOffset);

  useEffect(() => {
    setLength(charLength);
  }, [setLength]);

  // Create a simple 1x1 texture as a stub for the color wheel (unused in dashboard)
  const colorWheelTexture = useMemo(() => createEmptyTexture(), []);

  useFrame((state) => {
    uTime = state.clock.elapsedTime;
  });

  const effect = useMemo(() => {
    if (!fontTexture) {
      return null;
    }

    return new ASCIIEffectImpl({
      colorWheelTexture,
      fontTexture,
      charSize,
      charLength,
      pixels,
      overwriteColor,
      color: new THREE.Color(color),
      greyscale,
      matrix,
      devicePixelRatio,
      darkTheme,
      resolution: [screenWidth, screenHeight],
      scrollOffset,
    });
  }, [
    colorWheelTexture,
    fontTexture,
    devicePixelRatio,
    darkTheme,
    screenWidth,
    screenHeight,
    scrollOffset,
  ]);

  if (!effect) {
    return null;
  }

  return <primitive ref={ref} object={effect} dispose={null} />;
});

ASCIIEffect.displayName = "ASCIIEffect";
