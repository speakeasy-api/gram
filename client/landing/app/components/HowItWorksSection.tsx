"use client";

import React from "react";
import { FileCode2, Settings2, Share2 } from "lucide-react";

const HowItWorksSection = () => {
  return (
    <section className="w-full py-24 sm:py-32 lg:py-40 relative">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        {/* Main Heading */}
        <div className="text-center mb-16 sm:mb-20 lg:mb-[68px]">
          <h2 className="font-display font-thin text-3xl sm:text-4xl lg:text-[51px] leading-tight lg:leading-[66px] tracking-tight lg:tracking-[-2.04px] text-black">
            How it works
          </h2>
          <p className="text-foreground/70 text-base sm:text-lg lg:text-xl leading-relaxed mt-4 sm:mt-6 max-w-2xl mx-auto">
            Get your API integrated with AI clients in three simple steps
          </p>
        </div>

        {/* Three Column Grid */}
        <div className="relative">
          {/* Grid Container */}
          <div className="grid grid-cols-1 md:grid-cols-3 relative">
            {/* Column 1 - Connect Your API */}
            <div className="relative border border-[#dcdcdc] md:border-l md:border-t md:border-b md:border-r-0">
              {/* Top content section */}
              <div className="border-b border-[#dcdcdc]">
                <div className="flex flex-col items-center justify-center px-4 sm:px-6 py-6 gap-4 sm:gap-6">
                  <FileCode2 className="w-5 h-5 sm:w-6 sm:h-6 text-black" />
                  <h3 className="font-display font-thin text-xl sm:text-2xl lg:text-[28px] leading-tight lg:leading-[39px] tracking-tight lg:tracking-[-1.12px] text-black text-center">
                    Connect Your API
                  </h3>
                </div>
              </div>

              {/* Bottom description section */}
              <div className="flex items-center justify-center px-4 sm:px-6 pb-6 pt-6 min-h-[88px]">
                <p className="text-sm sm:text-base lg:text-[16px] leading-relaxed lg:leading-[28px] lg:tracking-[0.04px] text-[#979797] text-center">
                  Bring your OpenAPI spec to create a tool for each operation
                </p>
              </div>
            </div>

            {/* Column 2 - Configure & Test */}
            <div className="relative border-t-0 md:border-t border-l border-r border-b border-[#dcdcdc] md:border-r-0">
              {/* Top content section */}
              <div className="border-b border-[#dcdcdc]">
                <div className="flex flex-col items-center justify-center px-4 sm:px-6 py-6 gap-4 sm:gap-6">
                  <Settings2 className="w-5 h-5 sm:w-6 sm:h-6 text-black" />
                  <h3 className="font-display font-thin text-xl sm:text-2xl lg:text-[28px] leading-tight lg:leading-[39px] tracking-tight lg:tracking-[-1.12px] text-black text-center">
                    Configure & Test
                  </h3>
                </div>
              </div>

              {/* Bottom description section */}
              <div className="flex items-center justify-center px-4 sm:px-6 pb-6 pt-6 min-h-[88px]">
                <p className="text-sm sm:text-base lg:text-[16px] leading-relaxed lg:leading-[28px] lg:tracking-[0.04px] text-[#979797] text-center">
                  Curate a toolset choosing tools from different APIs, custom
                  tools and prompts
                </p>
              </div>
            </div>

            {/* Column 3 - Deploy & Share */}
            <div className="relative border-t-0 md:border border-l border-r border-b border-[#dcdcdc]">
              {/* Top content section */}
              <div className="border-b border-[#dcdcdc]">
                <div className="flex flex-col items-center justify-center px-4 sm:px-6 py-6 gap-4 sm:gap-6">
                  <Share2 className="w-5 h-5 sm:w-6 sm:h-6 text-black" />
                  <h3 className="font-display font-thin text-xl sm:text-2xl lg:text-[28px] leading-tight lg:leading-[39px] tracking-tight lg:tracking-[-1.12px] text-black text-center">
                    Deploy & Share
                  </h3>
                </div>
              </div>

              {/* Bottom description section */}
              <div className="flex items-center justify-center px-4 sm:px-6 pb-6 pt-6 min-h-[88px]">
                <p className="text-sm sm:text-base lg:text-[16px] leading-relaxed lg:leading-[28px] lg:tracking-[0.04px] text-[#979797] text-center">
                  Host and share a high quality MCP server. Works everywhere and
                  updates instantly.
                </p>
              </div>
            </div>

            {/* Decoration squares at grid intersections - Hidden on mobile, visible on md+ */}
            <div className="hidden md:block">
              {/* Top row - at column intersections */}
              <div className="absolute left-0 -top-[3px] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2" />
              <div className="absolute left-1/3 -top-[3px] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2" />
              <div className="absolute left-2/3 -top-[3px] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2" />
              <div className="absolute right-0 -top-[3px] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] translate-x-1/2" />

              {/* Middle row - at horizontal divider line */}
              <div className="absolute left-0 top-[50%] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2 -translate-y-1/2" />
              <div className="absolute left-1/3 top-[50%] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2 -translate-y-1/2" />
              <div className="absolute left-2/3 top-[50%] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2 -translate-y-1/2" />
              <div className="absolute right-0 top-[50%] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] translate-x-1/2 -translate-y-1/2" />

              {/* Bottom row - at column intersections */}
              <div className="absolute left-0 -bottom-[3px] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2" />
              <div className="absolute left-1/3 -bottom-[3px] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2" />
              <div className="absolute left-2/3 -bottom-[3px] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] -translate-x-1/2" />
              <div className="absolute right-0 -bottom-[3px] w-[6px] h-[6px] bg-[#f9f9f9] border border-[#dcdcdc] translate-x-1/2" />
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};

export default HowItWorksSection;
