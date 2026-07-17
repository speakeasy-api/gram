import { cn } from "@/lib/utils";
import { forwardRef, type InputHTMLAttributes } from "react";

interface SliderProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "onChange"
> {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  // Optional values at which to render tick marks on the track, as a discrete
  // affordance. Omit for a continuous slider (default).
  ticks?: number[];
}

const Slider = forwardRef<HTMLInputElement, SliderProps>(
  (
    {
      className,
      value,
      onChange,
      min = 0,
      max = 100,
      step = 1,
      ticks,
      ...props
    },
    ref,
  ) => {
    const hasTicks = !!ticks && ticks.length > 0;
    const input = (
      <input
        type="range"
        ref={ref}
        value={value}
        onChange={(e) => onChange(parseFloat(e.target.value))}
        min={min}
        max={max}
        step={step}
        className={cn(
          "relative z-10 h-2 w-full cursor-pointer appearance-none rounded-lg",
          // The ticks variant draws its own track behind the input, so the
          // input's own track must stay transparent; otherwise show a solid
          // track so the thumb sits on a visible line.
          hasTicks ? "bg-transparent" : "bg-muted",
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
          "disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
        {...props}
      />
    );

    if (!ticks || ticks.length === 0) {
      return input;
    }

    return (
      <div className="relative flex w-full items-center">
        {/* Track + tick marks sit behind the (transparent-track) input. Ticks
            are inset by half the 16px thumb so their centers line up with the
            thumb center across the full range. */}
        <div className="bg-muted pointer-events-none absolute inset-x-0 top-1/2 h-2 -translate-y-1/2 rounded-lg">
          {ticks.map((t) => {
            const frac = max === min ? 0 : (t - min) / (max - min);
            const active = value >= t;
            return (
              <span
                key={t}
                className={cn(
                  "absolute top-1/2 h-2 w-0.5 -translate-x-1/2 -translate-y-1/2 rounded-full",
                  active ? "bg-foreground/50" : "bg-foreground/20",
                )}
                style={{ left: `calc(${frac} * (100% - 16px) + 8px)` }}
              />
            );
          })}
        </div>
        {input}
      </div>
    );
  },
);

Slider.displayName = "Slider";

export { Slider };
