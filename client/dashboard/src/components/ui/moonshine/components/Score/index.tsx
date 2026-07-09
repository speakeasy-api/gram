import { Size } from "@/components/ui/moonshine/types";
import type { Range } from "@/components/ui/moonshine/lib/typeUtils";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import styles from "./index.module.css";
import { cn } from "@/components/ui/moonshine/lib/utils";

/** @public — kept for upcoming design-system adoption (Kbd/stories) */
export type ScoreValue = Range<100>;

interface ThresholdStyles {
  /**
   * The color of the track when the score is greater than the threshold.
   */
  [key: number]: string;
}

export interface ScoreProps {
  /**
   * The score to display. Must be between 0 and 100.
   */
  score: ScoreValue;
  /**
   * The size of the score component e.g small, medium, large, xl, 2xl
   */
  size?: Size;
  /**
   * Whether to show the label.
   */
  showLabel?: boolean;
  /**
   * The color of the track.
   */
  trackColor?: string;
  /**
   * Custom definition of thresholds for color changes.
   * e.g [50, 75] will change color to green at 75%, orange at 50%. Otherwise red.
   */
  thresholds?: ThresholdStyles;

  animate?: boolean;

  animationDuration?: number;
  className?: string;
}

const transition = {
  length: 1000, // ms
  step: 200, // ms
  delay: 0, // ms
};
const gapPercent = 5;
const strokeWidth = 10;
const circleSize = 100;
const radius = circleSize / 2 - strokeWidth / 2;
const circumference = 2 * Math.PI * radius;
const percentToDegree = 360 / 100; // deg
const percentToPx = circumference / 100; // px
const offsetFactor = 0;
const offsetFactorSecondary = 1 - offsetFactor;

const sizeMap: Record<Size, number> = {
  small: 40,
  medium: 50,
  large: 60,
  xl: 70,
  "2xl": 80,
};

const defaultThresholds: ThresholdStyles = {
  0: "var(--score-low)",
  40: "var(--score-mid)",
  60: "var(--score-high)",
};

const defaultTrackColor = "var(--score-track)";

export function Score({
  score,
  size = "large",
  showLabel = true,
  trackColor = defaultTrackColor,
  thresholds = defaultThresholds,
  animate = false,
  animationDuration = 40,
  className,
}: ScoreProps): React.JSX.Element {
  const [scoreInternal, setScoreInternal] = useState<ScoreValue>(
    animate ? 0 : score,
  );
  const strokePercent = scoreInternal; // %

  // Only the values present on the initial render should drive the
  // reveal animation below, so the latest values are tracked in a ref
  // (refs don't need to be listed as effect dependencies) rather than
  // added to the effect's dependency array, which intentionally runs once.
  const animationProps = useRef({ animate, score, animationDuration });
  animationProps.current = { animate, score, animationDuration };

  useEffect(() => {
    const { animate, score, animationDuration } = animationProps.current;
    if (animate) {
      const stepSize = Math.max(1, Math.floor(score / 20)); // Increase by 5% of total at minimum
      const interval = setInterval(() => {
        setScoreInternal((prevScore) => {
          if (prevScore >= score) {
            clearInterval(interval);
            return score;
          }
          return Math.min(prevScore + stepSize, score) as ScoreValue;
        });
      }, animationDuration); // Reduced interval time for smoother animation
      return () => clearInterval(interval);
    }
  }, []);

  const progressColour = useMemo(() => {
    for (const [threshold, color] of Object.entries(thresholds).reverse()) {
      if (scoreInternal >= Number(threshold)) return color;
    }
  }, [scoreInternal, thresholds]);

  const primaryStrokeDasharray = () => {
    if (
      offsetFactor > 0 &&
      strokePercent > 100 - gapPercent * 2 * offsetFactor
    ) {
      // calculation to gradually shift back to 0 offset as progress nears 100% when offsetFactor > 0
      const subtract = -strokePercent + 100;

      return `${Math.max(strokePercent * percentToPx - subtract * percentToPx, 0)} ${circumference}`;
    } else {
      const subtract = gapPercent * 2 * offsetFactor;

      return `${Math.max(strokePercent * percentToPx - subtract * percentToPx, 0)} ${circumference}`;
    }
  };

  const secondaryStrokeDasharray = () => {
    if (
      offsetFactorSecondary < 1 &&
      strokePercent < gapPercent * 2 * offsetFactorSecondary
    ) {
      // calculation to gradually shift back to 1 secondary offset as progress nears 100% when offsetFactorSecondary < 1
      const subtract = strokePercent;

      return `${Math.max((100 - strokePercent) * percentToPx - subtract * percentToPx, 0)} ${circumference}`;
    } else {
      const subtract = gapPercent * 2 * offsetFactorSecondary;

      return `${Math.max((100 - strokePercent) * percentToPx - subtract * percentToPx, 0)} ${circumference}`;
    }
  };

  const primaryTransform = () => {
    if (
      offsetFactor > 0 &&
      strokePercent > 100 - gapPercent * 2 * offsetFactor
    ) {
      // calculation to gradually shift back to 0 offset as progress nears 100% when offsetFactor > 0
      const add = 0.5 * (-strokePercent + 100);

      return `rotate(${-90 + add * percentToDegree}deg)`;
    } else {
      const add = gapPercent * offsetFactor;

      return `rotate(${-90 + add * percentToDegree}deg)`;
    }
  };

  const secondaryTransform = () => {
    if (
      offsetFactorSecondary < 1 &&
      strokePercent < gapPercent * 2 * offsetFactorSecondary
    ) {
      // calculation to gradually shift back to 1 secondary offset as progress nears 100% when offsetFactorSecondary < 1
      const subtract = 0.5 * strokePercent;

      return `rotate(${360 - 90 - subtract * percentToDegree}deg) scaleY(-1)`;
    } else {
      const subtract = gapPercent * offsetFactorSecondary;

      return `rotate(${360 - 90 - subtract * percentToDegree}deg) scaleY(-1)`;
    }
  };

  const primaryOpacity = () => {
    if (
      offsetFactor > 0 &&
      strokePercent < gapPercent * 2 * offsetFactor &&
      strokePercent < gapPercent * 2 * offsetFactorSecondary
    ) {
      return 0;
    } else return 1;
  };

  const secondaryOpacity = () => {
    if (
      (offsetFactor === 0 && strokePercent > 100 - gapPercent * 2) ||
      (offsetFactor > 0 &&
        strokePercent > 100 - gapPercent * 2 * offsetFactor &&
        strokePercent > 100 - gapPercent * 2 * offsetFactorSecondary)
    ) {
      return 0;
    } else return 1;
  };

  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox={`0 0 ${circleSize} ${circleSize}`}
      shapeRendering="crispEdges"
      width={sizeMap[size]}
      height={sizeMap[size]}
      className={cn(styles.svg, className)}
      style={
        {
          "--stroke-width": strokeWidth,
          "--transition-length": `${transition?.length}ms`,
          "--delay": `${transition?.delay}ms`,
        } as CSSProperties
      }
      fill="none"
    >
      <circle
        cx={circleSize / 2}
        cy={circleSize / 2}
        r={radius}
        style={
          {
            "--track-colour": trackColor,
            "--track-stroke-dasharray": secondaryStrokeDasharray(),
            transform: secondaryTransform(),
            opacity: secondaryOpacity(),
          } as CSSProperties
        }
        className={styles.track}
      />

      <circle
        cx={circleSize / 2}
        cy={circleSize / 2}
        r={radius}
        style={
          {
            "--progress-colour": progressColour,
            "--progress-stroke-dasharray": primaryStrokeDasharray(),
            "--transition-length": `${transition?.length}ms`,
            "--delay": `${transition?.delay}ms`,
            transform: primaryTransform(),
            opacity: primaryOpacity(),
          } as CSSProperties
        }
        className={styles.progress}
      />

      {showLabel && (
        <text
          x="50%"
          y="50%"
          textAnchor="middle"
          dominantBaseline="middle"
          alignmentBaseline="central"
          className={styles.text}
          style={
            {
              "--progress-colour": progressColour,
              "--transition-length": `${transition?.length}ms`,
              "--delay": `${transition?.delay}ms`,
            } as CSSProperties
          }
        >
          {Math.round(strokePercent)}
        </text>
      )}
    </svg>
  );
}
