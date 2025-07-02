"use client";

import React from "react";
import { FileCode2, Settings2, Share2 } from "lucide-react";

const HowItWorksSection = () => {
  return (
    <section className="w-full py-24 sm:py-32 lg:py-40 relative">
      <div className="container mx-auto px-8 lg:px-[150px]">
        {/* Main Heading */}
        <div className="text-center mb-16 sm:mb-20 lg:mb-[68px]">
          <h2 className="font-display font-thin text-[51px] leading-[66px] tracking-[-2.04px] text-black">
            How it works
          </h2>
          <p className="text-foreground/70 text-base sm:text-lg lg:text-xl leading-relaxed mt-6">
            Get your API integrated with AI clients in three simple steps
          </p>
        </div>

        {/* Three Column Grid */}
        <div className="relative">
          {/* Grid Container */}
          <div className="grid grid-cols-1 md:grid-cols-3 relative">
            {/* Column 1 - Connect Your API */}
            <div className="relative border-l border-t border-b border-[#dcdcdc]">
              {/* Top content section */}
              <div className="border-b border-[#dcdcdc]">
                <div className="flex flex-col items-center justify-center px-6 py-6 gap-6">
                  <FileCode2 className="w-6 h-6 text-black" />
                  <h3 className="font-display font-thin text-[28px] leading-[39px] tracking-[-1.12px] text-black text-center">
                    Connect Your API
                  </h3>
                </div>
              </div>
              
              {/* Bottom description section */}
              <div className="flex items-center justify-center px-6 pb-6 pt-6 min-h-[88px]">
                <p className="text-[16px] leading-[28px] tracking-[0.04px] text-[#979797] text-center">
                  Bring your OpenAPI spec to create a tool for each operation
                </p>
              </div>
            </div>

            {/* Column 2 - Configure & Test */}
            <div className="relative border-l border-t border-b border-[#dcdcdc]">
              {/* Top content section */}
              <div className="border-b border-[#dcdcdc]">
                <div className="flex flex-col items-center justify-center px-6 py-6 gap-6">
                  <Settings2 className="w-6 h-6 text-black" />
                  <h3 className="font-display font-thin text-[28px] leading-[39px] tracking-[-1.12px] text-black text-center">
                    Configure & Test
                  </h3>
                </div>
              </div>
              
              {/* Bottom description section */}
              <div className="flex items-center justify-center px-6 pb-6 pt-6 min-h-[88px]">
                <p className="text-[16px] leading-[28px] tracking-[0.04px] text-[#979797] text-center">
                  Curate a toolset choosing tools from different APIs, custom tools and prompts
                </p>
              </div>
            </div>

            {/* Column 3 - Deploy & Share */}
            <div className="relative border border-[#dcdcdc]">
              {/* Top content section */}
              <div className="border-b border-[#dcdcdc]">
                <div className="flex flex-col items-center justify-center px-6 py-6 gap-6">
                  <Share2 className="w-6 h-6 text-black" />
                  <h3 className="font-display font-thin text-[28px] leading-[39px] tracking-[-1.12px] text-black text-center">
                    Deploy & Share
                  </h3>
                </div>
              </div>
              
              {/* Bottom description section */}
              <div className="flex items-center justify-center px-6 pb-6 pt-6 min-h-[88px]">
                <p className="text-[16px] leading-[28px] tracking-[0.04px] text-[#979797] text-center">
                  Host and share a high quality MCP server. Works everywhere and updates instantly.
                </p>
              </div>
            </div>
            
            {/* Decoration squares at grid intersections */}
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
    </section>
  );
};

export default HowItWorksSection;