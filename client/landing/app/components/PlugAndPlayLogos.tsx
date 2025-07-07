"use client";

import { motion } from "framer-motion";
import { useBentoItemState } from "./BentoGrid";

export default function PlugAndPlayLogos() {
  const { isHovered } = useBentoItemState();

  const logos = [
    {
      id: "claude",
      icon: (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          preserveAspectRatio="xMidYMid"
          viewBox="0 0 256 257"
        >
          <path
            fill="#D97757"
            d="m50.228 170.321 50.357-28.257.843-2.463-.843-1.361h-2.462l-8.426-.518-28.775-.778-24.952-1.037-24.175-1.296-6.092-1.297L0 125.796l.583-3.759 5.12-3.434 7.324.648 16.202 1.101 24.304 1.685 17.629 1.037 26.118 2.722h4.148l.583-1.685-1.426-1.037-1.101-1.037-25.147-17.045-27.22-18.017-14.258-10.37-7.713-5.25-3.888-4.925-1.685-10.758 7-7.713 9.397.649 2.398.648 9.527 7.323 20.35 15.75L94.817 91.9l3.889 3.24 1.555-1.102.195-.777-1.75-2.917-14.453-26.118-15.425-26.572-6.87-11.018-1.814-6.61c-.648-2.723-1.102-4.991-1.102-7.778l7.972-10.823L71.42 0 82.05 1.426l4.472 3.888 6.61 15.101 10.694 23.786 16.591 32.34 4.861 9.592 2.592 8.879.973 2.722h1.685v-1.556l1.36-18.211 2.528-22.36 2.463-28.776.843-8.1 4.018-9.722 7.971-5.25 6.222 2.981 5.12 7.324-.713 4.73-3.046 19.768-5.962 30.98-3.889 20.739h2.268l2.593-2.593 10.499-13.934 17.628-22.036 7.778-8.749 9.073-9.657 5.833-4.601h11.018l8.1 12.055-3.628 12.443-11.342 14.388-9.398 12.184-13.48 18.147-8.426 14.518.778 1.166 2.01-.194 30.46-6.481 16.462-2.982 19.637-3.37 8.88 4.148.971 4.213-3.5 8.62-20.998 5.184-24.628 4.926-36.682 8.685-.454.324.519.648 16.526 1.555 7.065.389h17.304l32.21 2.398 8.426 5.574 5.055 6.805-.843 5.184-12.962 6.611-17.498-4.148-40.83-9.721-14-3.5h-1.944v1.167l11.666 11.406 21.387 19.314 26.767 24.887 1.36 6.157-3.434 4.86-3.63-.518-23.526-17.693-9.073-7.972-20.545-17.304h-1.36v1.814l4.73 6.935 25.017 37.59 1.296 11.536-1.814 3.76-6.481 2.268-7.13-1.297-14.647-20.544-15.1-23.138-12.185-20.739-1.49.843-7.194 77.448-3.37 3.953-7.778 2.981-6.48-4.925-3.436-7.972 3.435-15.749 4.148-20.544 3.37-16.333 3.046-20.285 1.815-6.74-.13-.454-1.49.194-15.295 20.999-23.267 31.433-18.406 19.702-4.407 1.75-7.648-3.954.713-7.064 4.277-6.286 25.47-32.405 15.36-20.092 9.917-11.6-.065-1.686h-.583L44.07 198.125l-12.055 1.555-5.185-4.86.648-7.972 2.463-2.593 20.35-13.999-.064.065Z"
          />
        </svg>
      ),
    },
    {
      id: "cursor",
      icon: (
        <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
          <title>Cursor</title>
          <path
            d="M11.925 24l10.425-6-10.425-6L1.5 18l10.425 6z"
            fill="url(#lobe-icons-cursor-fill-0)"
          />
          <path
            d="M22.35 18V6L11.925 0v12l10.425 6z"
            fill="url(#lobe-icons-cursor-fill-1)"
          />
          <path
            d="M11.925 0L1.5 6v12l10.425-6V0z"
            fill="url(#lobe-icons-cursor-fill-2)"
          />
          <path d="M22.35 6L11.925 24V12L22.35 6z" fill="#555" />
          <path d="M22.35 6l-10.425 6L1.5 6h20.85z" fill="#000" />
          <defs>
            <linearGradient
              gradientUnits="userSpaceOnUse"
              id="lobe-icons-cursor-fill-0"
              x1="11.925"
              x2="11.925"
              y1="12"
              y2="24"
            >
              <stop offset=".16" stopColor="#000" stopOpacity=".39" />
              <stop offset=".658" stopColor="#000" stopOpacity=".8" />
            </linearGradient>
            <linearGradient
              gradientUnits="userSpaceOnUse"
              id="lobe-icons-cursor-fill-1"
              x1="22.35"
              x2="11.925"
              y1="6.037"
              y2="12.15"
            >
              <stop offset=".182" stopColor="#000" stopOpacity=".31" />
              <stop offset=".715" stopColor="#000" stopOpacity="0" />
            </linearGradient>
            <linearGradient
              gradientUnits="userSpaceOnUse"
              id="lobe-icons-cursor-fill-2"
              x1="11.925"
              x2="1.5"
              y1="0"
              y2="18"
            >
              <stop stopColor="#000" stopOpacity=".6" />
              <stop offset=".667" stopColor="#000" stopOpacity=".22" />
            </linearGradient>
          </defs>
        </svg>
      ),
    },
    {
      id: "windsurf",
      icon: (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          fill="none"
          viewBox="0 0 166 263"
        >
          <g filter="url(#windsurf-a)">
            <path
              fill="#58E5BB"
              d="M42.086 128.474 28.427 90.089c-2.383-6.696 2.436-13.802 9.291-13.48 76.537 3.589 115.804 31.534 112.144 112.12-13.311-56.28-77.153-60.255-107.776-60.255Z"
            />
          </g>
          <g filter="url(#windsurf-b)">
            <path
              fill="#58E5BB"
              d="M21.453 57.833 7.236 20.639C4.662 13.908 9.478 6.6 16.44 6.683c78.163.938 132.738 6.243 132.738 110.722-13.311-56.28-97.101-59.572-127.725-59.572Z"
            />
          </g>
          <g filter="url(#windsurf-c)">
            <path
              fill="#58E5BB"
              d="m63.245 201.377-14.653-41.075c-2.376-6.661 2.376-13.751 9.196-13.388 62.444 3.327 93.677 30.587 90.06 110.239-13.311-56.28-53.76-55.776-84.604-55.776Z"
            />
          </g>
          <defs>
            <filter
              id="windsurf-a"
              width="122.286"
              height="116.131"
              x="27.81"
              y="76.598"
              colorInterpolationFilters="sRGB"
              filterUnits="userSpaceOnUse"
            >
              <feFlood floodOpacity="0" result="BackgroundImageFix" />
              <feBlend
                in="SourceGraphic"
                in2="BackgroundImageFix"
                result="shape"
              />
              <feColorMatrix
                in="SourceAlpha"
                result="hardAlpha"
                values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
              />
              <feOffset dy="4" />
              <feGaussianBlur stdDeviation="2" />
              <feComposite
                in2="hardAlpha"
                k2="-1"
                k3="1"
                operator="arithmetic"
              />
              <feColorMatrix values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.25 0" />
              <feBlend in2="shape" result="effect1_innerShadow_15473_4390" />
            </filter>
            <filter
              id="windsurf-b"
              width="142.645"
              height="114.724"
              x="6.533"
              y="6.682"
              colorInterpolationFilters="sRGB"
              filterUnits="userSpaceOnUse"
            >
              <feFlood floodOpacity="0" result="BackgroundImageFix" />
              <feBlend
                in="SourceGraphic"
                in2="BackgroundImageFix"
                result="shape"
              />
              <feColorMatrix
                in="SourceAlpha"
                result="hardAlpha"
                values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
              />
              <feOffset dy="4" />
              <feGaussianBlur stdDeviation="2" />
              <feComposite
                in2="hardAlpha"
                k2="-1"
                k3="1"
                operator="arithmetic"
              />
              <feColorMatrix values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.25 0" />
              <feBlend in2="shape" result="effect1_innerShadow_15473_4390" />
            </filter>
            <filter
              id="windsurf-c"
              width="100.158"
              height="114.252"
              x="47.972"
              y="146.9"
              colorInterpolationFilters="sRGB"
              filterUnits="userSpaceOnUse"
            >
              <feFlood floodOpacity="0" result="BackgroundImageFix" />
              <feBlend
                in="SourceGraphic"
                in2="BackgroundImageFix"
                result="shape"
              />
              <feColorMatrix
                in="SourceAlpha"
                result="hardAlpha"
                values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"
              />
              <feOffset dy="4" />
              <feGaussianBlur stdDeviation="2" />
              <feComposite
                in2="hardAlpha"
                k2="-1"
                k3="1"
                operator="arithmetic"
              />
              <feColorMatrix values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0.25 0" />
              <feBlend in2="shape" result="effect1_innerShadow_15473_4390" />
            </filter>
          </defs>
        </svg>
      ),
    },
    {
      id: "openai",
      icon: (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          preserveAspectRatio="xMidYMid"
          viewBox="0 0 256 260"
        >
          <path d="M239.184 106.203a64.716 64.716 0 0 0-5.576-53.103C219.452 28.459 191 15.784 163.213 21.74A65.586 65.586 0 0 0 52.096 45.22a64.716 64.716 0 0 0-43.23 31.36c-14.31 24.602-11.061 55.634 8.033 76.74a64.665 64.665 0 0 0 5.525 53.102c14.174 24.65 42.644 37.324 70.446 31.36a64.72 64.72 0 0 0 48.754 21.744c28.481.025 53.714-18.361 62.414-45.481a64.767 64.767 0 0 0 43.229-31.36c14.137-24.558 10.875-55.423-8.083-76.483Zm-97.56 136.338a48.397 48.397 0 0 1-31.105-11.255l1.535-.87 51.67-29.825a8.595 8.595 0 0 0 4.247-7.367v-72.85l21.845 12.636c.218.111.37.32.409.563v60.367c-.056 26.818-21.783 48.545-48.601 48.601Zm-104.466-44.61a48.345 48.345 0 0 1-5.781-32.589l1.534.921 51.722 29.826a8.339 8.339 0 0 0 8.441 0l63.181-36.425v25.221a.87.87 0 0 1-.358.665l-52.335 30.184c-23.257 13.398-52.97 5.431-66.404-17.803ZM23.549 85.38a48.499 48.499 0 0 1 25.58-21.333v61.39a8.288 8.288 0 0 0 4.195 7.316l62.874 36.272-21.845 12.636a.819.819 0 0 1-.767 0L41.353 151.53c-23.211-13.454-31.171-43.144-17.804-66.405v.256Zm179.466 41.695-63.08-36.63L161.73 77.86a.819.819 0 0 1 .768 0l52.233 30.184a48.6 48.6 0 0 1-7.316 87.635v-61.391a8.544 8.544 0 0 0-4.4-7.213Zm21.742-32.69-1.535-.922-51.619-30.081a8.39 8.39 0 0 0-8.492 0L99.98 99.808V74.587a.716.716 0 0 1 .307-.665l52.233-30.133a48.652 48.652 0 0 1 72.236 50.391v.205ZM88.061 139.097l-21.845-12.585a.87.87 0 0 1-.41-.614V65.685a48.652 48.652 0 0 1 79.757-37.346l-1.535.87-51.67 29.825a8.595 8.595 0 0 0-4.246 7.367l-.051 72.697Zm11.868-25.58 28.138-16.217 28.188 16.218v32.434l-28.086 16.218-28.188-16.218-.052-32.434Z" />
        </svg>
      ),
    },
    {
      id: "mistral",
      icon: (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          preserveAspectRatio="xMidYMid"
          viewBox="0 0 256 233"
        >
          <path d="M186.18182 0h46.54545v46.54545h-46.54545z" />
          <path fill="#F7D046" d="M209.45454 0h46.54545v46.54545h-46.54545z" />
          <path d="M0 0h46.54545v46.54545H0zM0 46.54545h46.54545V93.0909H0zM0 93.09091h46.54545v46.54545H0zM0 139.63636h46.54545v46.54545H0zM0 186.18182h46.54545v46.54545H0z" />
          <path fill="#F7D046" d="M23.27273 0h46.54545v46.54545H23.27273z" />
          <path
            fill="#F2A73B"
            d="M209.45454 46.54545h46.54545V93.0909h-46.54545zM23.27273 46.54545h46.54545V93.0909H23.27273z"
          />
          <path d="M139.63636 46.54545h46.54545V93.0909h-46.54545z" />
          <path
            fill="#F2A73B"
            d="M162.90909 46.54545h46.54545V93.0909h-46.54545zM69.81818 46.54545h46.54545V93.0909H69.81818z"
          />
          <path
            fill="#EE792F"
            d="M116.36364 93.09091h46.54545v46.54545h-46.54545zM162.90909 93.09091h46.54545v46.54545h-46.54545zM69.81818 93.09091h46.54545v46.54545H69.81818z"
          />
          <path d="M93.09091 139.63636h46.54545v46.54545H93.09091z" />
          <path
            fill="#EB5829"
            d="M116.36364 139.63636h46.54545v46.54545h-46.54545z"
          />
          <path
            fill="#EE792F"
            d="M209.45454 93.09091h46.54545v46.54545h-46.54545zM23.27273 93.09091h46.54545v46.54545H23.27273z"
          />
          <path d="M186.18182 139.63636h46.54545v46.54545h-46.54545z" />
          <path
            fill="#EB5829"
            d="M209.45454 139.63636h46.54545v46.54545h-46.54545z"
          />
          <path d="M186.18182 186.18182h46.54545v46.54545h-46.54545z" />
          <path
            fill="#EB5829"
            d="M23.27273 139.63636h46.54545v46.54545H23.27273z"
          />
          <path
            fill="#EA3326"
            d="M209.45454 186.18182h46.54545v46.54545h-46.54545zM23.27273 186.18182h46.54545v46.54545H23.27273z"
          />
        </svg>
      ),
    },
  ];

  return (
    <div className="w-full max-w-md">
      <motion.div
        className="flex flex-wrap justify-center items-center gap-8"
        initial={{ opacity: 1 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.3 }}
      >
        {logos.map((logo, index) => (
          <motion.div
            key={logo.id}
            className="w-14 h-14 flex items-center justify-center group cursor-pointer"
            initial={{ opacity: 1, scale: 1, rotate: 0 }}
            animate={{
              opacity: 1,
              scale: isHovered ? 1.05 : 1,
              rotate: 0,
            }}
            transition={{
              duration: 0.4,
              delay: isHovered ? index * 0.08 : 0,
              ease: [0.21, 0.47, 0.32, 0.98],
            }}
            whileHover={{
              scale: 1.15,
              rotate: 5,
              y: -4,
            }}
            whileTap={{ scale: 0.95 }}
          >
            <motion.div
              className="w-full h-full [&>svg]:w-full [&>svg]:h-full [&>svg]:object-contain p-2 rounded-lg"
              whileHover={{
                backgroundColor: "rgba(0,0,0,0.05)",
              }}
              transition={{ duration: 0.2 }}
            >
              {logo.icon}
            </motion.div>
          </motion.div>
        ))}
      </motion.div>

    </div>
  );
}
