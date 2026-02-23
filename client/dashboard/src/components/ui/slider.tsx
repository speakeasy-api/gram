import { cn } from "@/lib/utils";
import { forwardRef, type InputHTMLAttributes } from "react";

export interface SliderProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "onChange"
> {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
}

const Slider = forwardRef<HTMLInputElement, SliderProps>(
  (
    { className, value, onChange, min = 0, max = 100, step = 1, ...props },
    ref,
  ) => {
    return (
      <input
        type="range"
        ref={ref}
        value={value}
        onChange={(e) => onChange(parseFloat(e.target.value))}
        min={min}
        max={max}
        step={step}
        className={cn(
          "w-full h-2 bg-muted rounded-lg appearance-none cursor-pointer",
          "[&::-webkit-slider-thumb]:appearance-none",
          "[&::-webkit-slider-thumb]:w-4",
          "[&::-webkit-slider-thumb]:h-4",
          "[&::-webkit-slider-thumb]:rounded-full",
          "[&::-webkit-slider-thumb]:bg-foreground",
          "[&::-webkit-slider-thumb]:cursor-pointer",
          "[&::-webkit-slider-thumb]:hover:bg-foreground/80",
          "[&::-webkit-slider-thumb]:transition-colors",
          "[&::-moz-range-thumb]:w-4",
          "[&::-moz-range-thumb]:h-4",
          "[&::-moz-range-thumb]:rounded-full",
          "[&::-moz-range-thumb]:bg-foreground",
          "[&::-moz-range-thumb]:border-0",
          "[&::-moz-range-thumb]:cursor-pointer",
          "[&::-moz-range-thumb]:hover:bg-foreground/80",
          "[&::-moz-range-thumb]:transition-colors",
          "disabled:opacity-50 disabled:cursor-not-allowed",
          className,
        )}
        {...props}
      />
    );
  },
);

Slider.displayName = "Slider";

export { Slider };
