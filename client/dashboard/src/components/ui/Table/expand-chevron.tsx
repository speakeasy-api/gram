import { ChevronUp } from "lucide-react";
import { motion } from "motion/react";

const chevronVariants = {
  up: { rotate: 0 },
  down: { rotate: 180 },
};

export const ExpandChevron = ({
  isCollapsed,
}: {
  isCollapsed: boolean | undefined;
}): React.JSX.Element => {
  return (
    <motion.div
      initial={isCollapsed ? "down" : "up"}
      animate={isCollapsed ? "down" : "up"}
      variants={chevronVariants}
      transition={{
        duration: 0.2,
        ease: [0.215, 0.61, 0.355, 1],
      }}
    >
      <ChevronUp className="h-4 w-4 text-default" />
    </motion.div>
  );
};
