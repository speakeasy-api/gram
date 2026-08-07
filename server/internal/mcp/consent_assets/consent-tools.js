(function () {
  var e = document.createElement(`style`);
  ((e.textContent = `/*! tailwindcss v4.3.3 | MIT License | https://tailwindcss.com */
@layer properties{@supports (((-webkit-hyphens:none)) and (not (margin-trim:inline))) or ((-moz-orient:inline) and (not (color:rgb(from red r g b)))){*,:before,:after,::backdrop{--tw-space-y-reverse:0;--tw-border-style:solid;--tw-leading:initial;--tw-font-weight:initial;--tw-tracking:initial;--tw-shadow:0 0 #0000;--tw-shadow-color:initial;--tw-shadow-alpha:100%;--tw-inset-shadow:0 0 #0000;--tw-inset-shadow-color:initial;--tw-inset-shadow-alpha:100%;--tw-ring-color:initial;--tw-ring-shadow:0 0 #0000;--tw-inset-ring-color:initial;--tw-inset-ring-shadow:0 0 #0000;--tw-ring-inset:initial;--tw-ring-offset-width:0px;--tw-ring-offset-color:#fff;--tw-ring-offset-shadow:0 0 #0000}}}@layer theme{:root,:host{--font-mono:ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;--color-yellow-200:oklch(94.5% .129 101.54);--color-yellow-800:oklch(47.6% .114 61.907);--color-neutral-100:oklch(97% 0 none);--color-neutral-200:oklch(92.2% 0 none);--color-neutral-300:oklch(87% 0 none);--color-neutral-400:oklch(70.8% 0 none);--color-neutral-500:oklch(55.6% 0 none);--color-neutral-600:oklch(43.9% 0 none);--color-neutral-700:oklch(37.1% 0 none);--color-neutral-800:oklch(26.9% 0 none);--color-neutral-900:oklch(20.5% 0 none);--spacing:.25rem;--container-xs:20rem;--container-sm:24rem;--text-xs:.75rem;--text-xs--line-height:calc(1 / .75);--text-sm:.875rem;--text-sm--line-height:calc(1.25 / .875);--font-weight-medium:500;--tracking-wider:.05em;--leading-snug:1.375;--radius-md:.375rem;--default-transition-duration:.15s;--default-transition-timing-function:cubic-bezier(.4, 0, .2, 1);--color-background:var(--background);--ease-in-out-quad:var(--ease-in-out-quad);--ease-in-out-cubic:var(--ease-in-out-cubic);--ease-in-out-quart:var(--ease-in-out-quart);--ease-in-out-quint:var(--ease-in-out-quint);--ease-in-out-expo:var(--ease-in-out-expo);--ease-in-out-circ:var(--ease-in-out-circ);--ease-in-quad:var(--ease-in-quad);--ease-in-cubic:var(--ease-in-cubic);--ease-in-quart:var(--ease-in-quart);--ease-in-quint:var(--ease-in-quint);--ease-in-expo:var(--ease-in-expo);--ease-in-circ:var(--ease-in-circ);--ease-out-quad:var(--ease-out-quad);--ease-out-cubic:var(--ease-out-cubic);--ease-out-quart:var(--ease-out-quart);--ease-out-quint:var(--ease-out-quint);--ease-out-expo:var(--ease-out-expo);--ease-out-circ:var(--ease-out-circ)}}@layer base{*,:after,:before,::backdrop{border-color:var(--border-neutral-softest)}::file-selector-button{border-color:var(--border-neutral-softest)}button:not(:disabled),[role=button]:not(:disabled){cursor:pointer}body{background-color:var(--bg-surface-primary-default);color:var(--text-default)}#consent-tools-root,#consent-tools-root *,#consent-tools-root :before,#consent-tools-root :after{box-sizing:border-box;border-style:solid;border-width:0;border-color:var(--border)}#consent-tools-root{color:var(--foreground);font-size:.875rem;line-height:1.4}#consent-tools-root p,#consent-tools-root h1,#consent-tools-root h2,#consent-tools-root h3,#consent-tools-root ul{margin:0;padding:0}#consent-tools-root button{color:inherit;font:inherit;background:0 0;border:0;padding:0}#consent-tools-root input[type=text],#consent-tools-root input[type=search]{font:inherit;color:inherit}@media (prefers-reduced-motion:reduce){#consent-tools-root *{transition-duration:.01ms!important;animation-duration:.01ms!important}}}@layer components;@layer utilities{.pointer-events-auto{pointer-events:auto}.pointer-events-none{pointer-events:none}.invisible{visibility:hidden}.absolute{position:absolute}.relative{position:relative}.bottom-full{bottom:100%}.left-0{left:0}.z-10{z-index:10}.z-50{z-index:50}.m-0{margin:0}.mt-1\\.5{margin-top:calc(var(--spacing) * 1.5)}.mb-1{margin-bottom:var(--spacing)}.ml-0\\.5{margin-left:calc(var(--spacing) * .5)}.flex{display:flex}.grid{display:grid}.hidden{display:none}.inline{display:inline}.inline-flex{display:inline-flex}.table{display:table}.size-3\\.5{width:calc(var(--spacing) * 3.5);height:calc(var(--spacing) * 3.5)}.size-4{width:calc(var(--spacing) * 4);height:calc(var(--spacing) * 4)}.h-2\\.5{height:calc(var(--spacing) * 2.5)}.h-3{height:calc(var(--spacing) * 3)}.h-3\\.5{height:calc(var(--spacing) * 3.5)}.h-8{height:calc(var(--spacing) * 8)}.h-px{height:1px}.max-h-44{max-height:calc(var(--spacing) * 44)}.max-h-\\[300px\\]{max-height:300px}.min-h-0{min-height:0}.w-2\\.5{width:calc(var(--spacing) * 2.5)}.w-3{width:calc(var(--spacing) * 3)}.w-3\\.5{width:calc(var(--spacing) * 3.5)}.w-fit{width:fit-content}.w-full{width:100%}.w-max{width:max-content}.max-w-72{max-width:calc(var(--spacing) * 72)}.max-w-sm{max-width:var(--container-sm)}.max-w-xs{max-width:var(--container-xs)}.min-w-0{min-width:0}.flex-1{flex:1}.shrink-0{flex-shrink:0}.rotate-90{rotate:90deg}.cursor-default{cursor:default}.cursor-help{cursor:help}.cursor-not-allowed{cursor:not-allowed}.cursor-pointer{cursor:pointer}.list-none{list-style-type:none}.grid-cols-\\[9\\.5rem_1fr\\]{grid-template-columns:9.5rem 1fr}.flex-col{flex-direction:column}.flex-wrap{flex-wrap:wrap}.items-center{align-items:center}.justify-between{justify-content:space-between}.justify-center{justify-content:center}.gap-1{gap:var(--spacing)}.gap-1\\.5{gap:calc(var(--spacing) * 1.5)}.gap-2{gap:calc(var(--spacing) * 2)}.gap-3{gap:calc(var(--spacing) * 3)}.gap-8{gap:calc(var(--spacing) * 8)}:where(.space-y-0\\.5>:not(:last-child)){--tw-space-y-reverse:0;margin-block-start:calc(calc(var(--spacing) * .5) * var(--tw-space-y-reverse));margin-block-end:calc(calc(var(--spacing) * .5) * calc(1 - var(--tw-space-y-reverse)))}:where(.space-y-1>:not(:last-child)){--tw-space-y-reverse:0;margin-block-start:calc(var(--spacing) * var(--tw-space-y-reverse));margin-block-end:calc(var(--spacing) * calc(1 - var(--tw-space-y-reverse)))}.gap-x-3{column-gap:calc(var(--spacing) * 3)}.gap-y-0\\.5{row-gap:calc(var(--spacing) * .5)}.truncate{text-overflow:ellipsis;white-space:nowrap;overflow:hidden}.overflow-y-auto{overflow-y:auto}.rounded-\\[3px\\]{border-radius:3px}.rounded-md{border-radius:var(--radius-md)}.border{border-style:var(--tw-border-style);border-width:1px}.border-t{border-top-style:var(--tw-border-style);border-top-width:1px}.border-r{border-right-style:var(--tw-border-style);border-right-width:1px}.border-b{border-bottom-style:var(--tw-border-style);border-bottom-width:1px}.border-border{border-color:var(--border)}.border-foreground{border-color:var(--foreground)}.border-input{border-color:var(--input)}.border-primary{border-color:var(--primary)}.bg-accent{background-color:var(--accent)}.bg-background{background-color:var(--background)}.bg-border{background-color:var(--border)}.bg-foreground{background-color:var(--foreground)}.bg-muted\\/30{background-color:var(--muted)}@supports (color:color-mix(in lab, red, red)){.bg-muted\\/30{background-color:color-mix(in oklab, var(--muted) 30%, transparent)}}.bg-popover{background-color:var(--popover)}.bg-primary\\/5{background-color:var(--primary)}@supports (color:color-mix(in lab, red, red)){.bg-primary\\/5{background-color:color-mix(in oklab, var(--primary) 5%, transparent)}}.bg-transparent{background-color:#0000}.bg-yellow-200{background-color:var(--color-yellow-200)}.fill-popover{fill:var(--popover)}.p-0{padding:0}.p-16{padding:calc(var(--spacing) * 16)}.px-2{padding-inline:calc(var(--spacing) * 2)}.px-2\\.5{padding-inline:calc(var(--spacing) * 2.5)}.px-3{padding-inline:calc(var(--spacing) * 3)}.px-8{padding-inline:calc(var(--spacing) * 8)}.py-1{padding-block:var(--spacing)}.py-1\\.5{padding-block:calc(var(--spacing) * 1.5)}.py-2{padding-block:calc(var(--spacing) * 2)}.py-2\\.5{padding-block:calc(var(--spacing) * 2.5)}.py-3{padding-block:calc(var(--spacing) * 3)}.pt-1{padding-top:var(--spacing)}.pt-1\\.5{padding-top:calc(var(--spacing) * 1.5)}.pt-5{padding-top:calc(var(--spacing) * 5)}.pr-3{padding-right:calc(var(--spacing) * 3)}.pb-3{padding-bottom:calc(var(--spacing) * 3)}.pb-4{padding-bottom:calc(var(--spacing) * 4)}.pl-8{padding-left:calc(var(--spacing) * 8)}.text-left{text-align:left}.font-mono{font-family:var(--font-mono)}.text-sm{font-size:var(--text-sm);line-height:var(--tw-leading,var(--text-sm--line-height))}.text-xs{font-size:var(--text-xs);line-height:var(--tw-leading,var(--text-xs--line-height))}.text-\\[10px\\]{font-size:10px}.text-\\[11px\\]{font-size:11px}.leading-snug{--tw-leading:var(--leading-snug);line-height:var(--leading-snug)}.font-medium{--tw-font-weight:var(--font-weight-medium);font-weight:var(--font-weight-medium)}.tracking-wider{--tw-tracking:var(--tracking-wider);letter-spacing:var(--tracking-wider)}.text-pretty{text-wrap:pretty}.text-background{color:var(--background)}.text-current{color:currentColor}.text-foreground{color:var(--foreground)}.text-muted-foreground,.text-muted-foreground\\/60{color:var(--muted-foreground)}@supports (color:color-mix(in lab, red, red)){.text-muted-foreground\\/60{color:color-mix(in oklab, var(--muted-foreground) 60%, transparent)}}.text-muted-foreground\\/70{color:var(--muted-foreground)}@supports (color:color-mix(in lab, red, red)){.text-muted-foreground\\/70{color:color-mix(in oklab, var(--muted-foreground) 70%, transparent)}}.text-popover-foreground{color:var(--popover-foreground)}.text-primary{color:var(--primary)}.text-success{color:var(--success)}.uppercase{text-transform:uppercase}.underline{text-decoration-line:underline}.decoration-dotted{text-decoration-style:dotted}.underline-offset-2{text-underline-offset:2px}.opacity-50{opacity:.5}.opacity-60{opacity:.6}.shadow-md{--tw-shadow:0 4px 6px -1px var(--tw-shadow-color,#0000001a), 0 2px 4px -2px var(--tw-shadow-color,#0000001a);box-shadow:var(--tw-inset-shadow), var(--tw-inset-ring-shadow), var(--tw-ring-offset-shadow), var(--tw-ring-shadow), var(--tw-shadow)}.transition-colors{transition-property:color,background-color,border-color,outline-color,text-decoration-color,fill,stroke,--tw-gradient-from,--tw-gradient-via,--tw-gradient-to;transition-timing-function:var(--tw-ease,var(--default-transition-timing-function));transition-duration:var(--tw-duration,var(--default-transition-duration))}.transition-shadow{transition-property:box-shadow;transition-timing-function:var(--tw-ease,var(--default-transition-timing-function));transition-duration:var(--tw-duration,var(--default-transition-duration))}.transition-transform{transition-property:transform,translate,scale,rotate;transition-timing-function:var(--tw-ease,var(--default-transition-timing-function));transition-duration:var(--tw-duration,var(--default-transition-duration))}.transition-none{transition-property:none}.outline-none{--tw-outline-style:none;outline-style:none}.group-focus-within\\:block:is(:where(.group):focus-within *){display:block}@media (hover:hover){.group-hover\\:block:is(:where(.group):hover *){display:block}}.placeholder\\:text-muted-foreground::placeholder{color:var(--muted-foreground)}.last\\:border-b-0:last-child{border-bottom-style:var(--tw-border-style);border-bottom-width:0}@media (hover:hover){.hover\\:bg-accent:hover{background-color:var(--accent)}.hover\\:bg-muted\\/50:hover{background-color:var(--muted)}@supports (color:color-mix(in lab, red, red)){.hover\\:bg-muted\\/50:hover{background-color:color-mix(in oklab, var(--muted) 50%, transparent)}}.hover\\:text-foreground:hover{color:var(--foreground)}.hover\\:underline:hover{text-decoration-line:underline}}.focus-visible\\:border-input:focus-visible{border-color:var(--input)}.focus-visible\\:border-ring:focus-visible{border-color:var(--ring)}.focus-visible\\:ring-0:focus-visible{--tw-ring-shadow:var(--tw-ring-inset,) 0 0 0 calc(0px + var(--tw-ring-offset-width)) var(--tw-ring-color,currentcolor);box-shadow:var(--tw-inset-shadow), var(--tw-inset-ring-shadow), var(--tw-ring-offset-shadow), var(--tw-ring-shadow), var(--tw-shadow)}.focus-visible\\:ring-1:focus-visible{--tw-ring-shadow:var(--tw-ring-inset,) 0 0 0 calc(1px + var(--tw-ring-offset-width)) var(--tw-ring-color,currentcolor);box-shadow:var(--tw-inset-shadow), var(--tw-inset-ring-shadow), var(--tw-ring-offset-shadow), var(--tw-ring-shadow), var(--tw-shadow)}.focus-visible\\:ring-\\[3px\\]:focus-visible{--tw-ring-shadow:var(--tw-ring-inset,) 0 0 0 calc(3px + var(--tw-ring-offset-width)) var(--tw-ring-color,currentcolor);box-shadow:var(--tw-inset-shadow), var(--tw-inset-ring-shadow), var(--tw-ring-offset-shadow), var(--tw-ring-shadow), var(--tw-shadow)}.focus-visible\\:ring-ring:focus-visible,.focus-visible\\:ring-ring\\/50:focus-visible{--tw-ring-color:var(--ring)}@supports (color:color-mix(in lab, red, red)){.focus-visible\\:ring-ring\\/50:focus-visible{--tw-ring-color:color-mix(in oklab, var(--ring) 50%, transparent)}}.focus-visible\\:outline-none:focus-visible{--tw-outline-style:none;outline-style:none}.disabled\\:cursor-not-allowed:disabled{cursor:not-allowed}.disabled\\:opacity-50:disabled{opacity:.5}.aria-invalid\\:border-destructive[aria-invalid=true]{border-color:var(--destructive)}.aria-invalid\\:ring-destructive\\/20[aria-invalid=true]{--tw-ring-color:var(--destructive)}@supports (color:color-mix(in lab, red, red)){.aria-invalid\\:ring-destructive\\/20[aria-invalid=true]{--tw-ring-color:color-mix(in oklab, var(--destructive) 20%, transparent)}}.data-\\[state\\=checked\\]\\:border-primary[data-state=checked]{border-color:var(--primary)}.data-\\[state\\=checked\\]\\:bg-primary[data-state=checked]{background-color:var(--primary)}.data-\\[state\\=checked\\]\\:text-primary-foreground[data-state=checked]{color:var(--primary-foreground)}.data-\\[state\\=indeterminate\\]\\:border-primary[data-state=indeterminate]{border-color:var(--primary)}.data-\\[state\\=indeterminate\\]\\:bg-primary[data-state=indeterminate]{background-color:var(--primary)}.data-\\[state\\=indeterminate\\]\\:text-primary-foreground[data-state=indeterminate]{color:var(--primary-foreground)}.dark\\:border-1:where(.dark,.dark *){border-style:var(--tw-border-style);border-width:1px}.dark\\:bg-input\\/30:where(.dark,.dark *){background-color:var(--input)}@supports (color:color-mix(in lab, red, red)){.dark\\:bg-input\\/30:where(.dark,.dark *){background-color:color-mix(in oklab, var(--input) 30%, transparent)}}.dark\\:bg-yellow-800\\/60:where(.dark,.dark *){background-color:#874b0099}@supports (color:color-mix(in lab, red, red)){.dark\\:bg-yellow-800\\/60:where(.dark,.dark *){background-color:color-mix(in oklab, var(--color-yellow-800) 60%, transparent)}}.dark\\:aria-invalid\\:ring-destructive\\/40:where(.dark,.dark *)[aria-invalid=true]{--tw-ring-color:var(--destructive)}@supports (color:color-mix(in lab, red, red)){.dark\\:aria-invalid\\:ring-destructive\\/40:where(.dark,.dark *)[aria-invalid=true]{--tw-ring-color:color-mix(in oklab, var(--destructive) 40%, transparent)}}.dark\\:data-\\[state\\=checked\\]\\:bg-primary:where(.dark,.dark *)[data-state=checked]{background-color:var(--primary)}.\\[\\&_svg\\]\\:fill-background svg{fill:var(--background)}.devicon-devicon-plain{max-width:2em}.devicon-devicon-plain path{fill:var(--primary)}@keyframes slide-fade-in{0%{opacity:0;transform:translate(100%)}to{opacity:1;transform:translate(0)}}@keyframes slide-fade-out{0%{opacity:1;transform:translate(0)}to{opacity:0;transform:translate(-100%)}}@keyframes spin{0%{transform:rotate(0)}to{transform:rotate(360deg)}}}:root,:root.light{--font-diatype:"Diatype", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;--font-diatype-mono:"Diatype Mono", SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;--font-tobias:"Tobias", "Cormorant", Georgia, Cambria, "Times New Roman", Times, serif;--color-base-white:#fff;--color-base-black:#000;--color-neutral-100:#fafafa;--color-neutral-200:#ebebeb;--color-neutral-300:#dbdbdb;--color-neutral-400:#bababa;--color-neutral-500:#969696;--color-neutral-600:#757575;--color-neutral-700:#545454;--color-neutral-800:#333;--color-neutral-900:#121212;--color-neutral-100-40:hsl(from var(--color-neutral-100) h s l / 40%);--color-neutral-100-56:hsl(from var(--color-neutral-100) h s l / 56%);--color-neutral-100-64:hsl(from var(--color-neutral-100) h s l / 64%);--color-neutral-200-56:hsl(from var(--color-neutral-200) h s l / 56%);--color-neutral-400-40:hsl(from var(--color-neutral-400) h s l / 40%);--color-neutral-600-40:hsl(from var(--color-neutral-600) h s l / 40%);--color-neutral-800-56:hsl(from var(--color-neutral-800) h s l / 56%);--color-neutral-900-40:hsl(from var(--color-neutral-900) h s l / 40%);--color-neutral-900-56:hsl(from var(--color-neutral-900) h s l / 56%);--color-neutral-900-64:hsl(from var(--color-neutral-900) h s l / 64%);--color-brand-red-100:#fb8841;--color-brand-red-200:#ee733a;--color-brand-red-300:#e15b33;--color-brand-red-400:#d6472e;--color-brand-red-500:#c83228;--color-brand-red-600:#a12826;--color-brand-red-700:#7d2123;--color-brand-red-800:#581821;--color-brand-red-900:#330f1f;--gradient-brand-red:radial-gradient(138.34% 138.34% at 0% 4.4%, var(--color-brand-red-900) 0%, var(--color-brand-red-800) 12.5%, var(--color-brand-red-700) 25%, var(--color-brand-red-600) 37.5%, var(--color-brand-red-500) 50%, var(--color-brand-red-400) 62.5%, var(--color-brand-red-300) 75%, var(--color-brand-red-200) 87.5%, var(--color-brand-red-100) 100%);--color-brand-green-100:#d3dd92;--color-brand-green-200:#b4c581;--color-brand-green-300:#95ae6f;--color-brand-green-400:#789a60;--color-brand-green-500:#59824f;--color-brand-green-600:#456c42;--color-brand-green-700:#2d5332;--color-brand-green-800:#173b23;--color-brand-green-900:#002414;--gradient-brand-green:radial-gradient(137.26% 137.26% at 0% 0%, var(--color-brand-green-900) 0%, var(--color-brand-green-800) 12.5%, var(--color-brand-green-700) 25%, var(--color-brand-green-600) 37.5%, var(--color-brand-green-500) 50%, var(--color-brand-green-400) 62.5%, var(--color-brand-green-300) 75%, var(--color-brand-green-200) 87.5%, var(--color-brand-green-100) 100%);--color-brand-blue-100:#fff;--color-brand-blue-200:#7fb0f5;--color-brand-blue-300:#619aea;--color-brand-blue-400:#4787e1;--color-brand-blue-500:#2874d7;--color-brand-blue-600:#1e5aae;--color-brand-blue-700:#14458a;--color-brand-blue-800:#0a2b61;--color-brand-blue-900:#00143d;--gradient-brand-blue:radial-gradient(141.42% 141.42% at 0% 0%, var(--color-brand-blue-900) 0%, var(--color-brand-blue-800) 12.5%, var(--color-brand-blue-700) 25%, var(--color-brand-blue-600) 37.5%, var(--color-brand-blue-500) 50%, var(--color-brand-blue-400) 62.5%, var(--color-brand-blue-300) 75%, var(--color-brand-blue-200) 87.5%, var(--color-brand-blue-100) 100%);--color-brand-typescript:#000;--color-brand-go:#00143d;--color-brand-java:#2874d7;--color-brand-python:#99c2ff;--color-brand-c:#002414;--color-brand-terraform:#59824f;--color-brand-unity:#d3dd92;--color-brand-php:#330f1f;--color-brand-swift:#c83228;--color-brand-ruby:#fb8841;--color-brand-postman:#d4d4d4;--gradient-brand-primary-colors:var(--color-brand-php) 0%, var(--color-brand-swift) 12.56%, var(--color-brand-ruby) 25.06%, var(--color-brand-unity) 37.56%, var(--color-brand-terraform) 50.06%, var(--color-brand-c) 62.06%, var(--color-brand-go) 74.06%, var(--color-brand-java) 86.06%, var(--color-brand-python) 97.06%;--gradient-brand-primary:linear-gradient(90deg, var(--gradient-brand-primary-colors));--gradient-brand-primary-y:linear-gradient(180deg, var(--gradient-brand-primary-colors));--color-feedback-red-100:#fff0f0;--color-feedback-red-200:#f9bcb9;--color-feedback-red-300:#ef817b;--color-feedback-red-400:#e34a45;--color-feedback-red-500:#c83228;--color-feedback-red-600:#a42723;--color-feedback-red-700:#861f1d;--color-feedback-red-800:#641817;--color-feedback-red-900:#461010;--color-feedback-red-400-56:hsl(from var(--color-feedback-red-400) h s l / 56%);--color-feedback-red-500-56:hsl(from var(--color-feedback-red-500) h s l / 56%);--color-feedback-red-600-56:hsl(from var(--color-feedback-red-600) h s l / 56%);--color-feedback-orange-100:#fff2e6;--color-feedback-orange-200:#ffd9b3;--color-feedback-orange-300:#ffb87a;--color-feedback-orange-400:#ffa052;--color-feedback-orange-500:#fb8841;--color-feedback-orange-600:#db7133;--color-feedback-orange-700:#b45927;--color-feedback-orange-800:#8c401d;--color-feedback-orange-900:#672c13;--color-feedback-orange-400-56:hsl(from var(--color-feedback-orange-400) h s l / 56%);--color-feedback-orange-500-56:hsl(from var(--color-feedback-orange-500) h s l / 56%);--color-feedback-green-100:#ebf3e7;--color-feedback-green-200:#cfe2c5;--color-feedback-green-300:#b4cfa5;--color-feedback-green-400:#94b983;--color-feedback-green-500:#59824f;--color-feedback-green-600:#4b6c42;--color-feedback-green-700:#3d5534;--color-feedback-green-800:#2d3f27;--color-feedback-green-900:#1d2919;--color-feedback-green-400-56:hsl(from var(--color-feedback-green-400) h s l / 56%);--color-feedback-green-500-56:hsl(from var(--color-feedback-green-500) h s l / 56%);--color-feedback-blue-100:#e6f1ff;--color-feedback-blue-200:#b8d7ff;--color-feedback-blue-300:#8abdff;--color-feedback-blue-400:#60a1f6;--color-feedback-blue-500:#2874d7;--color-feedback-blue-600:#1f5cb2;--color-feedback-blue-700:#18488c;--color-feedback-blue-800:#103165;--color-feedback-blue-900:#0a1f42;--color-feedback-blue-400-56:hsl(from var(--color-feedback-blue-400) h s l / 56%);--color-feedback-blue-500-56:hsl(from var(--color-feedback-blue-500) h s l / 56%);--color-feedback-violet-300:#9a61ea;--color-feedback-violet-600:#4b148a;--color-link-default:var(--color-brand-blue-600);--color-link-visited-primary:var(--color-feedback-violet-600);--color-link-secondary:var(--color-base-black);--color-link-visited-secondary:var(--color-feedback-violet-600);--text-heading-xl:var(--color-neutral-900);--text-heading-lg:var(--color-neutral-900);--text-heading-md:var(--color-neutral-900);--text-heading-sm:var(--color-neutral-800);--text-heading-xs:var(--color-neutral-800);--text-display:var(--color-base-black);--text-highlight:var(--color-neutral-900);--text-default:var(--color-neutral-700);--text-muted:var(--color-neutral-900-64);--text-placeholder:var(--color-neutral-900-56);--text-disabled:var(--color-neutral-900-40);--text-highlight-fixed-dark:var(--color-base-black);--text-default-fixed-dark:var(--color-neutral-900);--text-muted-fixed-dark:var(--color-neutral-900-64);--text-highlight-fixed-light:var(--color-base-white);--text-default-fixed-light:var(--color-neutral-100);--text-muted-fixed-light:var(--color-neutral-100-64);--text-highlight-inverse:var(--color-base-white);--text-default-inverse:var(--color-neutral-100);--text-muted-inverse:var(--color-neutral-100-64);--text-link-primary:var(--color-brand-blue-600);--text-link-secondary:var(--color-base-black);--text-link-visited:var(--color-feedback-violet-600);--text-default-destructive:var(--color-feedback-red-700);--text-link-destructive:var(--color-feedback-red-900);--text-default-information:var(--color-feedback-blue-700);--text-link-information:var(--color-feedback-blue-900);--text-default-success:var(--color-feedback-green-700);--text-link-success:var(--color-feedback-green-900);--text-default-warning:var(--color-feedback-orange-700);--text-link-warning:var(--color-feedback-orange-900);--text-body:var(--color-neutral-900);--text-body-muted:var(--color-neutral-400);--bg-warning:var(--color-feedback-orange-100);--text-warning:var(--color-feedback-orange-700);--border-warning:var(--color-feedback-orange-300);--underline-link-primary:var(--color-brand-blue-600);--underline-link-secondary:var(--color-base-black);--underline-link-visited:var(--color-feedback-violet-600);--border-neutral-active:var(--color-neutral-600);--border-neutral-hover:var(--color-neutral-500);--border-neutral-default:var(--color-neutral-400);--border-neutral-disabled:var(--color-neutral-400-40);--border-neutral-softest:var(--color-neutral-200);--border-neutral-inset:var(--color-base-white);--border-neutral-alpha:#0003;--border-destructive-highlight:var(--color-feedback-red-600);--border-destructive-default:var(--color-feedback-red-500);--border-destructive-muted:var(--color-feedback-red-500-56);--border-destructive-softest:var(--color-feedback-red-300);--border-information-highlight:var(--color-feedback-blue-600);--border-information-default:var(--color-feedback-blue-500);--border-information-muted:var(--color-feedback-blue-500-56);--border-information-softest:var(--color-feedback-blue-300);--border-success-highlight:var(--color-feedback-green-600);--border-success-default:var(--color-feedback-green-500);--border-success-muted:var(--color-feedback-green-500-56);--border-success-softest:var(--color-feedback-green-300);--border-warning-highlight:var(--color-feedback-orange-600);--border-warning-default:var(--color-feedback-orange-500);--border-warning-muted:var(--color-feedback-orange-500-56);--border-warning-softest:var(--color-feedback-orange-300);--border-focus:var(--color-brand-blue-600);--fill-neutral-highlight:var(--color-base-black);--fill-neutral-active:var(--color-neutral-900);--fill-neutral-default:var(--color-neutral-800);--fill-neutral-muted:var(--color-neutral-800-56);--fill-neutral-highlight-fixed-dark:var(--color-base-black);--fill-neutral-default-fixed-dark:var(--color-neutral-800);--fill-neutral-muted-fixed-dark:var(--color-neutral-900-56);--fill-neutral-highlight-fixed-light:var(--color-base-white);--fill-neutral-default-fixed-light:var(--color-neutral-100);--fill-neutral-muted-fixed-light:var(--color-neutral-100-56);--fill-neutral-highlight-inverse:var(--color-base-white);--fill-neutral-default-inverse:var(--color-neutral-100);--fill-neutral-muted-inverse:var(--color-neutral-100-56);--fill-link-primary:var(--color-brand-blue-600);--fill-link-secondary:var(--color-base-black);--fill-link-visited:var(--color-feedback-violet-600);--fill-destructive-highlight:var(--color-feedback-red-600);--fill-destructive-default:var(--color-feedback-red-500);--fill-destructive-muted:var(--color-feedback-red-600-56);--fill-information-highlight:var(--color-feedback-blue-600);--fill-information-default:var(--color-feedback-blue-500);--fill-information-muted:var(--color-feedback-blue-500-56);--fill-success-highlight:var(--color-feedback-green-600);--fill-success-default:var(--color-feedback-green-500);--fill-success-muted:var(--color-feedback-green-500-56);--fill-warning-highlight:var(--color-feedback-orange-600);--fill-warning-default:var(--color-feedback-orange-500);--fill-warning-muted:var(--color-feedback-orange-500-56);--stroke-neutral-highlight:var(--color-base-black);--stroke-neutral-active:var(--color-neutral-900);--stroke-neutral-default:var(--color-neutral-800);--stroke-neutral-muted:var(--color-neutral-800-56);--stroke-neutral-highlight-fixed-dark:var(--color-base-black);--stroke-neutral-default-fixed-dark:var(--color-neutral-800);--stroke-neutral-muted-fixed-dark:var(--color-neutral-900-56);--stroke-neutral-highlight-fixed-light:var(--color-base-white);--stroke-neutral-default-fixed-light:var(--color-neutral-100);--stroke-neutral-muted-fixed-light:var(--color-neutral-100-56);--stroke-neutral-highlight-inverse:var(--color-base-white);--stroke-neutral-default-inverse:var(--color-neutral-100);--stroke-neutral-muted-inverse:var(--color-neutral-100-56);--stroke-link-primary:var(--color-brand-blue-600);--stroke-link-secondary:var(--color-base-black);--stroke-link-visited:var(--color-feedback-violet-600);--stroke-destructive-highlight:var(--color-feedback-red-600);--stroke-destructive-default:var(--color-feedback-red-500);--stroke-destructive-muted:var(--color-feedback-red-600-56);--stroke-information-highlight:var(--color-feedback-blue-600);--stroke-information-default:var(--color-feedback-blue-500);--stroke-information-muted:var(--color-feedback-blue-500-56);--stroke-success-highlight:var(--color-feedback-green-600);--stroke-success-default:var(--color-feedback-green-500);--stroke-success-muted:var(--color-feedback-green-500-56);--stroke-warning-highlight:var(--color-feedback-orange-600);--stroke-warning-default:var(--color-feedback-orange-500);--stroke-warning-muted:var(--color-feedback-orange-500-56);--bg-surface-primary-default:var(--color-base-white);--bg-surface-secondary-default:var(--color-neutral-100);--bg-surface-tertiary-default:var(--color-neutral-200);--bg-surface-primary-inverse:var(--color-base-black);--bg-surface-secondary-inverse:var(--color-neutral-900);--bg-surface-tertiary-inverse:var(--color-neutral-800);--bg-highlight:var(--color-neutral-300);--bg-active:var(--color-neutral-200);--bg-default:var(--color-neutral-100);--bg-muted:var(--color-neutral-100-56);--bg-inset:var(--color-base-white);--bg-surface-primary-fixed-light:var(--color-base-white);--bg-surface-secondary-fixed-light:var(--color-neutral-100);--bg-surface-tertiary-fixed-light:var(--color-neutral-200);--bg-surface-primary-fixed-dark:var(--color-base-black);--bg-surface-secondary-fixed-dark:var(--color-neutral-900);--bg-surface-tertiary-fixed-dark:var(--color-neutral-800);--bg-destructive-highlight:var(--color-feedback-red-600);--bg-destructive-default:var(--color-feedback-red-500);--bg-destructive-muted:var(--color-feedback-red-500-56);--bg-destructive-softest:var(--color-feedback-red-100);--bg-information-highlight:var(--color-feedback-blue-600);--bg-information-default:var(--color-feedback-blue-500);--bg-information-muted:var(--color-feedback-blue-500-56);--bg-information-softest:var(--color-feedback-blue-100);--bg-success-highlight:var(--color-feedback-green-600);--bg-success-default:var(--color-feedback-green-500);--bg-success-muted:var(--color-feedback-green-500-56);--bg-success-softest:var(--color-feedback-green-100);--bg-warning-highlight:var(--color-feedback-orange-600);--bg-warning-default:var(--color-feedback-orange-500);--bg-warning-muted:var(--color-feedback-orange-500-56);--bg-warning-softest:var(--color-feedback-orange-100);--radius:0rem;--background-pure:var(--color-base-white);--background:var(--color-neutral-100);--foreground:var(--color-neutral-900);--card:var(--color-base-white);--card-foreground:var(--color-neutral-900);--popover:var(--color-base-white);--popover-foreground:var(--color-neutral-900);--primary:var(--color-base-black);--primary-foreground:var(--color-base-white);--secondary:var(--color-neutral-100);--secondary-foreground:var(--color-neutral-900);--muted:var(--color-neutral-200);--muted-foreground:var(--color-neutral-900-64);--accent:var(--color-neutral-200);--accent-foreground:var(--color-neutral-900);--destructive:var(--color-feedback-red-700);--border:var(--color-neutral-300);--input:var(--color-neutral-400);--ring:var(--color-brand-blue-600);--chart-1:var(--color-brand-red-600);--chart-2:var(--color-brand-green-600);--chart-3:var(--color-brand-red-600);--chart-4:var(--color-brand-blue-600);--chart-5:var(--color-brand-green-600);--sidebar:var(--color-neutral-100);--sidebar-foreground:var(--color-neutral-900);--sidebar-primary:var(--color-base-black);--sidebar-primary-foreground:var(--color-neutral-300);--sidebar-accent:var(--color-neutral-300);--sidebar-accent-foreground:var(--color-neutral-900);--sidebar-border:var(--color-neutral-300);--sidebar-ring:var(--color-brand-blue-600);--success:var(--color-feedback-green-100);--success-foreground:var(--color-feedback-green-700);--warning:var(--color-feedback-orange-100);--warning-foreground:var(--color-feedback-orange-700);--info:var(--color-info-100);--info-foreground:var(--color-info-700);--destructive-foreground:var(--color-feedback-red-100);--feature:var(--color-info-100);--feature-foreground:var(--color-info-700);--shadow:gray;--sb-size:.5rem;--sb-track-color:var(--color-background);--sb-thumb-color:var(--color-background);--sb-track-border:var(--color-neutral-300);--score-low:var(--color-feedback-red-500);--score-mid:var(--color-feedback-orange-500);--score-high:var(--color-feedback-green-500);--score-track:var(--color-neutral-700);--ease-in-quad:cubic-bezier(.55, .085, .68, .53);--ease-in-cubic:cubic-bezier(.55, .055, .675, .19);--ease-in-quart:cubic-bezier(.895, .03, .685, .22);--ease-in-quint:cubic-bezier(.755, .05, .855, .06);--ease-in-expo:cubic-bezier(.95, .05, .795, .035);--ease-in-circ:cubic-bezier(.6, .04, .98, .335);--ease-out-quad:cubic-bezier(.25, .46, .45, .94);--ease-out-cubic:cubic-bezier(.215, .61, .355, 1);--ease-out-quart:cubic-bezier(.165, .84, .44, 1);--ease-out-quint:cubic-bezier(.23, 1, .32, 1);--ease-out-expo:cubic-bezier(.19, 1, .22, 1);--ease-out-circ:cubic-bezier(.075, .82, .165, 1);--ease-in-out-quad:cubic-bezier(.455, .03, .515, .955);--ease-in-out-cubic:cubic-bezier(.645, .045, .355, 1);--ease-in-out-quart:cubic-bezier(.77, 0, .175, 1);--ease-in-out-quint:cubic-bezier(.86, 0, .07, 1);--ease-in-out-expo:cubic-bezier(1, 0, 0, 1);--ease-in-out-circ:cubic-bezier(.785, .135, .15, .86)}:root.dark{--color-link-default:var(--color-brand-blue-300);--color-link-visited-primary:var(--color-feedback-violet-300);--color-link-secondary:var(--color-base-white);--color-link-visited-secondary:var(--color-feedback-violet-300);--text-heading-xl:var(--color-neutral-100);--text-heading-lg:var(--color-neutral-100);--text-heading-md:var(--color-neutral-100);--text-heading-sm:var(--color-neutral-200);--text-heading-xs:var(--color-neutral-200);--text-display:var(--color-base-white);--text-highlight:var(--color-neutral-100);--text-default:var(--color-neutral-300);--text-muted:var(--color-neutral-100-64);--text-placeholder:var(--color-neutral-100-56);--text-disabled:var(--color-neutral-100-40);--text-highlight-inverse:var(--color-base-black);--text-default-inverse:var(--color-neutral-900);--text-muted-inverse:var(--color-neutral-900-64);--text-link-primary:var(--color-brand-blue-300);--text-link-secondary:var(--color-base-white);--text-link-visited:var(--color-feedback-violet-300);--text-default-destructive:var(--color-feedback-red-300);--text-link-destructive:var(--color-feedback-red-100);--text-default-information:var(--color-feedback-blue-300);--text-link-information:var(--color-feedback-blue-100);--text-default-success:var(--color-feedback-green-300);--text-link-success:var(--color-feedback-green-100);--text-default-warning:var(--color-feedback-orange-300);--text-link-warning:var(--color-feedback-orange-100);--text-body:var(--color-neutral-200);--text-body-muted:var(--color-neutral-600);--bg-warning:var(--color-feedback-orange-900);--text-warning:var(--color-feedback-orange-300);--border-warning:var(--color-feedback-orange-700);--underline-link-primary:var(--color-brand-blue-300);--underline-link-secondary:var(--color-base-white);--underline-link-visited:var(--color-feedback-violet-300);--border-neutral-active:var(--color-neutral-400);--border-neutral-hover:var(--color-neutral-500);--border-neutral-default:var(--color-neutral-600);--border-neutral-disabled:var(--color-neutral-600-40);--border-neutral-softest:var(--color-neutral-800-56);--border-neutral-inset:var(--color-base-black);--border-neutral-alpha:#fff3;--border-destructive-highlight:var(--color-feedback-red-400);--border-destructive-default:var(--color-feedback-red-500);--border-destructive-muted:var(--color-feedback-red-500-56);--border-destructive-softest:var(--color-feedback-red-700);--border-information-highlight:var(--color-feedback-blue-400);--border-information-default:var(--color-feedback-blue-500);--border-information-muted:var(--color-feedback-blue-500-56);--border-information-softest:var(--color-feedback-blue-700);--border-success-highlight:var(--color-feedback-green-400);--border-success-default:var(--color-feedback-green-500);--border-success-muted:var(--color-feedback-green-500-56);--border-success-softest:var(--color-feedback-green-700);--border-warning-highlight:var(--color-feedback-orange-400);--border-warning-default:var(--color-feedback-orange-500);--border-warning-muted:var(--color-feedback-orange-500-56);--border-warning-softest:var(--color-feedback-orange-700);--border-focus:var(--color-brand-blue-300);--fill-neutral-highlight:var(--color-base-white);--fill-neutral-active:var(--color-neutral-100);--fill-neutral-default:var(--color-neutral-200);--fill-neutral-muted:var(--color-neutral-200-56);--fill-neutral-highlight-inverse:var(--color-base-black);--fill-neutral-default-inverse:var(--color-neutral-900);--fill-neutral-muted-inverse:var(--color-neutral-900-56);--fill-link-primary:var(--color-brand-blue-300);--fill-link-secondary:var(--color-base-white);--fill-link-visited:var(--color-feedback-violet-300);--fill-destructive-highlight:var(--color-feedback-red-300);--fill-destructive-default:var(--color-feedback-red-400);--fill-destructive-muted:var(--color-feedback-red-400-56);--fill-information-highlight:var(--color-feedback-blue-300);--fill-information-default:var(--color-feedback-blue-400);--fill-information-muted:var(--color-feedback-blue-400-56);--fill-success-highlight:var(--color-feedback-green-300);--fill-success-default:var(--color-feedback-green-400);--fill-success-muted:var(--color-feedback-green-400-56);--fill-warning-highlight:var(--color-feedback-orange-300);--fill-warning-default:var(--color-feedback-orange-400);--fill-warning-muted:var(--color-feedback-orange-400-56);--stroke-neutral-highlight:var(--color-base-white);--stroke-neutral-active:var(--color-neutral-100);--stroke-neutral-default:var(--color-neutral-200);--stroke-neutral-muted:var(--color-neutral-200-56);--stroke-neutral-highlight-inverse:var(--color-base-black);--stroke-neutral-default-inverse:var(--color-neutral-900);--stroke-neutral-muted-inverse:var(--color-neutral-900-56);--stroke-link-primary:var(--color-brand-blue-300);--stroke-link-secondary:var(--color-base-white);--stroke-link-visited:var(--color-feedback-violet-300);--stroke-destructive-highlight:var(--color-feedback-red-300);--stroke-destructive-default:var(--color-feedback-red-400);--stroke-destructive-muted:var(--color-feedback-red-400-56);--stroke-information-highlight:var(--color-feedback-blue-300);--stroke-information-default:var(--color-feedback-blue-400);--stroke-information-muted:var(--color-feedback-blue-400-56);--stroke-success-highlight:var(--color-feedback-green-300);--stroke-success-default:var(--color-feedback-green-400);--stroke-success-muted:var(--color-feedback-green-400-56);--stroke-warning-highlight:var(--color-feedback-orange-300);--stroke-warning-default:var(--color-feedback-orange-400);--stroke-warning-muted:var(--color-feedback-orange-400-56);--bg-surface-primary-default:var(--color-base-black);--bg-surface-secondary-default:var(--color-neutral-900);--bg-surface-tertiary-default:var(--color-neutral-800);--bg-surface-primary-inverse:var(--color-base-white);--bg-surface-secondary-inverse:var(--color-neutral-100);--bg-surface-tertiary-inverse:var(--color-neutral-200);--bg-highlight:var(--color-neutral-700);--bg-active:var(--color-neutral-800);--bg-default:var(--color-neutral-900);--bg-muted:var(--color-neutral-900-56);--bg-inset:var(--color-base-black);--bg-destructive-highlight:var(--color-feedback-red-400);--bg-destructive-default:var(--color-feedback-red-500);--bg-destructive-muted:var(--color-feedback-red-500-56);--bg-destructive-softest:var(--color-feedback-red-900);--bg-information-highlight:var(--color-feedback-blue-400);--bg-information-default:var(--color-feedback-blue-500);--bg-information-muted:var(--color-feedback-blue-500-56);--bg-information-softest:var(--color-feedback-blue-900);--bg-success-highlight:var(--color-feedback-green-400);--bg-success-default:var(--color-feedback-green-500);--bg-success-muted:var(--color-feedback-green-500-56);--bg-success-softest:var(--color-feedback-green-900);--bg-warning-highlight:var(--color-feedback-orange-400);--bg-warning-default:var(--color-feedback-orange-500);--bg-warning-muted:var(--color-feedback-orange-500-56);--bg-warning-softest:var(--color-feedback-orange-900);--success:var(--color-feedback-green-500);--success-foreground:var(--color-feedback-green-100);--warning:var(--color-feedback-orange-500);--warning-foreground:var(--color-feedback-orange-100);--info:var(--color-info-500);--info-foreground:var(--color-info-100);--destructive:var(--color-feedback-red-500);--destructive-foreground:var(--color-feedback-red-100);--feature:var(--color-info-500);--feature-foreground:var(--color-info-100);--score-low:#f43e5c;--score-mid:#f59f0a;--score-high:#10b77f;--score-track:#4a4945;--sb-track-color:var(--color-neutral-800);--sb-thumb-color:var(--color-neutral-900);--sb-track-border:var(--color-neutral-800);--header-border:0 0% 14.9%;--background-pure:var(--color-base-black);--background:var(--color-base-black);--foreground:var(--color-neutral-300);--card:var(--color-neutral-900);--card-foreground:var(--color-neutral-300);--popover:var(--color-base-black);--popover-foreground:var(--color-neutral-100);--primary:var(--color-base-white);--primary-foreground:var(--color-base-black);--secondary:var(--color-neutral-900);--secondary-foreground:var(--color-neutral-300);--muted:var(--color-neutral-900);--muted-foreground:var(--color-neutral-100-64);--accent:var(--color-neutral-800);--accent-foreground:var(--color-base-white);--border:var(--color-neutral-700);--input:var(--color-neutral-700);--ring:var(--color-brand-blue-600);--chart-1:var(--color-brand-red-600);--chart-2:var(--color-brand-green-600);--chart-3:var(--color-brand-red-600);--chart-4:var(--color-brand-blue-600);--chart-5:var(--color-brand-green-600);--sidebar:var(--color-neutral-900);--sidebar-foreground:var(--color-base-white);--sidebar-primary:var(--color-brand-red-600);--sidebar-primary-foreground:var(--color-neutral-300);--sidebar-accent:var(--color-neutral-700);--sidebar-accent-foreground:var(--color-base-white);--sidebar-border:var(--color-neutral-700);--sidebar-ring:var(--color-brand-blue-600)}@property --tw-space-y-reverse{syntax:"*";inherits:false;initial-value:0}@property --tw-border-style{syntax:"*";inherits:false;initial-value:solid}@property --tw-leading{syntax:"*";inherits:false}@property --tw-font-weight{syntax:"*";inherits:false}@property --tw-tracking{syntax:"*";inherits:false}@property --tw-shadow{syntax:"*";inherits:false;initial-value:0 0 #0000}@property --tw-shadow-color{syntax:"*";inherits:false}@property --tw-shadow-alpha{syntax:"<percentage>";inherits:false;initial-value:100%}@property --tw-inset-shadow{syntax:"*";inherits:false;initial-value:0 0 #0000}@property --tw-inset-shadow-color{syntax:"*";inherits:false}@property --tw-inset-shadow-alpha{syntax:"<percentage>";inherits:false;initial-value:100%}@property --tw-ring-color{syntax:"*";inherits:false}@property --tw-ring-shadow{syntax:"*";inherits:false;initial-value:0 0 #0000}@property --tw-inset-ring-color{syntax:"*";inherits:false}@property --tw-inset-ring-shadow{syntax:"*";inherits:false;initial-value:0 0 #0000}@property --tw-ring-inset{syntax:"*";inherits:false}@property --tw-ring-offset-width{syntax:"<length>";inherits:false;initial-value:0}@property --tw-ring-offset-color{syntax:"*";inherits:false;initial-value:#fff}@property --tw-ring-offset-shadow{syntax:"*";inherits:false;initial-value:0 0 #0000}
/*$vite$:1*/`),
    document.head.appendChild(e));
  var t = Object.create,
    n = Object.defineProperty,
    r = Object.getOwnPropertyDescriptor,
    i = Object.getOwnPropertyNames,
    a = Object.getPrototypeOf,
    o = Object.prototype.hasOwnProperty,
    s = (e, t, n) => () => {
      if (n) throw n[0];
      try {
        return (e && (t = e((e = 0))), t);
      } catch (e) {
        throw ((n = [e]), e);
      }
    },
    c = (e, t) => () => (
      t || (e((t = { exports: {} }).exports, t), (e = null)),
      t.exports
    ),
    l = (e, t) => {
      let r = {};
      for (var i in e) n(r, i, { get: e[i], enumerable: !0 });
      return (t || n(r, Symbol.toStringTag, { value: `Module` }), r);
    },
    u = (e, t, a, s) => {
      if ((t && typeof t == `object`) || typeof t == `function`)
        for (var c = i(t), l = 0, u = c.length, d; l < u; l++)
          ((d = c[l]),
            !o.call(e, d) &&
              d !== a &&
              n(e, d, {
                get: ((e) => t[e]).bind(null, d),
                enumerable: !(s = r(t, d)) || s.enumerable,
              }));
      return e;
    },
    d = (e, r, i) => (
      (i = e == null ? {} : t(a(e))),
      u(
        r || !e || !e.__esModule
          ? n(i, `default`, { value: e, enumerable: !0 })
          : i,
        e,
      )
    ),
    f = (e) =>
      o.call(e, `module.exports`)
        ? e[`module.exports`]
        : u(n({}, `__esModule`, { value: !0 }), e),
    p = c((e) => {
      function t(e, t) {
        var n = e.length;
        e.push(t);
        a: for (; 0 < n; ) {
          var r = (n - 1) >>> 1,
            a = e[r];
          if (0 < i(a, t)) ((e[r] = t), (e[n] = a), (n = r));
          else break a;
        }
      }
      function n(e) {
        return e.length === 0 ? null : e[0];
      }
      function r(e) {
        if (e.length === 0) return null;
        var t = e[0],
          n = e.pop();
        if (n !== t) {
          e[0] = n;
          a: for (var r = 0, a = e.length, o = a >>> 1; r < o; ) {
            var s = 2 * (r + 1) - 1,
              c = e[s],
              l = s + 1,
              u = e[l];
            if (0 > i(c, n))
              l < a && 0 > i(u, c)
                ? ((e[r] = u), (e[l] = n), (r = l))
                : ((e[r] = c), (e[s] = n), (r = s));
            else if (l < a && 0 > i(u, n)) ((e[r] = u), (e[l] = n), (r = l));
            else break a;
          }
        }
        return t;
      }
      function i(e, t) {
        var n = e.sortIndex - t.sortIndex;
        return n === 0 ? e.id - t.id : n;
      }
      if (
        ((e.unstable_now = void 0),
        typeof performance == `object` && typeof performance.now == `function`)
      ) {
        var a = performance;
        e.unstable_now = function () {
          return a.now();
        };
      } else {
        var o = Date,
          s = o.now();
        e.unstable_now = function () {
          return o.now() - s;
        };
      }
      var c = [],
        l = [],
        u = 1,
        d = null,
        f = 3,
        p = !1,
        m = !1,
        h = !1,
        g = !1,
        _ = typeof setTimeout == `function` ? setTimeout : null,
        v = typeof clearTimeout == `function` ? clearTimeout : null,
        y = typeof setImmediate < `u` ? setImmediate : null;
      function b(e) {
        for (var i = n(l); i !== null; ) {
          if (i.callback === null) r(l);
          else if (i.startTime <= e)
            (r(l), (i.sortIndex = i.expirationTime), t(c, i));
          else break;
          i = n(l);
        }
      }
      function x(e) {
        if (((h = !1), b(e), !m))
          if (n(c) !== null) ((m = !0), ee || ((ee = !0), re()));
          else {
            var t = n(l);
            t !== null && oe(x, t.startTime - e);
          }
      }
      var ee = !1,
        S = -1,
        C = 5,
        w = -1;
      function te() {
        return g ? !0 : !(e.unstable_now() - w < C);
      }
      function ne() {
        if (((g = !1), ee)) {
          var t = e.unstable_now();
          w = t;
          var i = !0;
          try {
            a: {
              ((m = !1), h && ((h = !1), v(S), (S = -1)), (p = !0));
              var a = f;
              try {
                b: {
                  for (
                    b(t), d = n(c);
                    d !== null && !(d.expirationTime > t && te());
                  ) {
                    var o = d.callback;
                    if (typeof o == `function`) {
                      ((d.callback = null), (f = d.priorityLevel));
                      var s = o(d.expirationTime <= t);
                      if (((t = e.unstable_now()), typeof s == `function`)) {
                        ((d.callback = s), b(t), (i = !0));
                        break b;
                      }
                      (d === n(c) && r(c), b(t));
                    } else r(c);
                    d = n(c);
                  }
                  if (d !== null) i = !0;
                  else {
                    var u = n(l);
                    (u !== null && oe(x, u.startTime - t), (i = !1));
                  }
                }
                break a;
              } finally {
                ((d = null), (f = a), (p = !1));
              }
              i = void 0;
            }
          } finally {
            i ? re() : (ee = !1);
          }
        }
      }
      var re;
      if (typeof y == `function`)
        re = function () {
          y(ne);
        };
      else if (typeof MessageChannel < `u`) {
        var ie = new MessageChannel(),
          ae = ie.port2;
        ((ie.port1.onmessage = ne),
          (re = function () {
            ae.postMessage(null);
          }));
      } else
        re = function () {
          _(ne, 0);
        };
      function oe(t, n) {
        S = _(function () {
          t(e.unstable_now());
        }, n);
      }
      ((e.unstable_IdlePriority = 5),
        (e.unstable_ImmediatePriority = 1),
        (e.unstable_LowPriority = 4),
        (e.unstable_NormalPriority = 3),
        (e.unstable_Profiling = null),
        (e.unstable_UserBlockingPriority = 2),
        (e.unstable_cancelCallback = function (e) {
          e.callback = null;
        }),
        (e.unstable_forceFrameRate = function (e) {
          0 > e || 125 < e
            ? console.error(
                `forceFrameRate takes a positive int between 0 and 125, forcing frame rates higher than 125 fps is not supported`,
              )
            : (C = 0 < e ? Math.floor(1e3 / e) : 5);
        }),
        (e.unstable_getCurrentPriorityLevel = function () {
          return f;
        }),
        (e.unstable_next = function (e) {
          switch (f) {
            case 1:
            case 2:
            case 3:
              var t = 3;
              break;
            default:
              t = f;
          }
          var n = f;
          f = t;
          try {
            return e();
          } finally {
            f = n;
          }
        }),
        (e.unstable_requestPaint = function () {
          g = !0;
        }),
        (e.unstable_runWithPriority = function (e, t) {
          switch (e) {
            case 1:
            case 2:
            case 3:
            case 4:
            case 5:
              break;
            default:
              e = 3;
          }
          var n = f;
          f = e;
          try {
            return t();
          } finally {
            f = n;
          }
        }),
        (e.unstable_scheduleCallback = function (r, i, a) {
          var o = e.unstable_now();
          switch (
            (typeof a == `object` && a
              ? ((a = a.delay), (a = typeof a == `number` && 0 < a ? o + a : o))
              : (a = o),
            r)
          ) {
            case 1:
              var s = -1;
              break;
            case 2:
              s = 250;
              break;
            case 5:
              s = 1073741823;
              break;
            case 4:
              s = 1e4;
              break;
            default:
              s = 5e3;
          }
          return (
            (s = a + s),
            (r = {
              id: u++,
              callback: i,
              priorityLevel: r,
              startTime: a,
              expirationTime: s,
              sortIndex: -1,
            }),
            a > o
              ? ((r.sortIndex = a),
                t(l, r),
                n(c) === null &&
                  r === n(l) &&
                  (h ? (v(S), (S = -1)) : (h = !0), oe(x, a - o)))
              : ((r.sortIndex = s),
                t(c, r),
                m || p || ((m = !0), ee || ((ee = !0), re()))),
            r
          );
        }),
        (e.unstable_shouldYield = te),
        (e.unstable_wrapCallback = function (e) {
          var t = f;
          return function () {
            var n = f;
            f = t;
            try {
              return e.apply(this, arguments);
            } finally {
              f = n;
            }
          };
        }));
    }),
    m = c((e, t) => {
      t.exports = p();
    }),
    h = c((e) => {
      var t = Symbol.for(`react.transitional.element`),
        n = Symbol.for(`react.portal`),
        r = Symbol.for(`react.fragment`),
        i = Symbol.for(`react.strict_mode`),
        a = Symbol.for(`react.profiler`),
        o = Symbol.for(`react.consumer`),
        s = Symbol.for(`react.context`),
        c = Symbol.for(`react.forward_ref`),
        l = Symbol.for(`react.suspense`),
        u = Symbol.for(`react.memo`),
        d = Symbol.for(`react.lazy`),
        f = Symbol.for(`react.activity`),
        p = Symbol.iterator;
      function m(e) {
        return typeof e != `object` || !e
          ? null
          : ((e = (p && e[p]) || e[`@@iterator`]),
            typeof e == `function` ? e : null);
      }
      var h = {
          isMounted: function () {
            return !1;
          },
          enqueueForceUpdate: function () {},
          enqueueReplaceState: function () {},
          enqueueSetState: function () {},
        },
        g = Object.assign,
        _ = {};
      function v(e, t, n) {
        ((this.props = e),
          (this.context = t),
          (this.refs = _),
          (this.updater = n || h));
      }
      ((v.prototype.isReactComponent = {}),
        (v.prototype.setState = function (e, t) {
          if (typeof e != `object` && typeof e != `function` && e != null)
            throw Error(
              `takes an object of state variables to update or a function which returns an object of state variables.`,
            );
          this.updater.enqueueSetState(this, e, t, `setState`);
        }),
        (v.prototype.forceUpdate = function (e) {
          this.updater.enqueueForceUpdate(this, e, `forceUpdate`);
        }));
      function y() {}
      y.prototype = v.prototype;
      function b(e, t, n) {
        ((this.props = e),
          (this.context = t),
          (this.refs = _),
          (this.updater = n || h));
      }
      var x = (b.prototype = new y());
      ((x.constructor = b), g(x, v.prototype), (x.isPureReactComponent = !0));
      var ee = Array.isArray;
      function S() {}
      var C = { H: null, A: null, T: null, S: null },
        w = Object.prototype.hasOwnProperty;
      function te(e, n, r) {
        var i = r.ref;
        return {
          $$typeof: t,
          type: e,
          key: n,
          ref: i === void 0 ? null : i,
          props: r,
        };
      }
      function ne(e, t) {
        return te(e.type, t, e.props);
      }
      function re(e) {
        return typeof e == `object` && !!e && e.$$typeof === t;
      }
      function ie(e) {
        var t = { "=": `=0`, ":": `=2` };
        return (
          `$` +
          e.replace(/[=:]/g, function (e) {
            return t[e];
          })
        );
      }
      var ae = /\/+/g;
      function oe(e, t) {
        return typeof e == `object` && e && e.key != null
          ? ie(`` + e.key)
          : t.toString(36);
      }
      function se(e) {
        switch (e.status) {
          case `fulfilled`:
            return e.value;
          case `rejected`:
            throw e.reason;
          default:
            switch (
              (typeof e.status == `string`
                ? e.then(S, S)
                : ((e.status = `pending`),
                  e.then(
                    function (t) {
                      e.status === `pending` &&
                        ((e.status = `fulfilled`), (e.value = t));
                    },
                    function (t) {
                      e.status === `pending` &&
                        ((e.status = `rejected`), (e.reason = t));
                    },
                  )),
              e.status)
            ) {
              case `fulfilled`:
                return e.value;
              case `rejected`:
                throw e.reason;
            }
        }
        throw e;
      }
      function ce(e, r, i, a, o) {
        var s = typeof e;
        (s === `undefined` || s === `boolean`) && (e = null);
        var c = !1;
        if (e === null) c = !0;
        else
          switch (s) {
            case `bigint`:
            case `string`:
            case `number`:
              c = !0;
              break;
            case `object`:
              switch (e.$$typeof) {
                case t:
                case n:
                  c = !0;
                  break;
                case d:
                  return ((c = e._init), ce(c(e._payload), r, i, a, o));
              }
          }
        if (c)
          return (
            (o = o(e)),
            (c = a === `` ? `.` + oe(e, 0) : a),
            ee(o)
              ? ((i = ``),
                c != null && (i = c.replace(ae, `$&/`) + `/`),
                ce(o, r, i, ``, function (e) {
                  return e;
                }))
              : o != null &&
                (re(o) &&
                  (o = ne(
                    o,
                    i +
                      (o.key == null || (e && e.key === o.key)
                        ? ``
                        : (`` + o.key).replace(ae, `$&/`) + `/`) +
                      c,
                  )),
                r.push(o)),
            1
          );
        c = 0;
        var l = a === `` ? `.` : a + `:`;
        if (ee(e))
          for (var u = 0; u < e.length; u++)
            ((a = e[u]), (s = l + oe(a, u)), (c += ce(a, r, i, s, o)));
        else if (((u = m(e)), typeof u == `function`))
          for (e = u.call(e), u = 0; !(a = e.next()).done; )
            ((a = a.value), (s = l + oe(a, u++)), (c += ce(a, r, i, s, o)));
        else if (s === `object`) {
          if (typeof e.then == `function`) return ce(se(e), r, i, a, o);
          throw (
            (r = String(e)),
            Error(
              `Objects are not valid as a React child (found: ` +
                (r === `[object Object]`
                  ? `object with keys {` + Object.keys(e).join(`, `) + `}`
                  : r) +
                `). If you meant to render a collection of children, use an array instead.`,
            )
          );
        }
        return c;
      }
      function le(e, t, n) {
        if (e == null) return e;
        var r = [],
          i = 0;
        return (
          ce(e, r, ``, ``, function (e) {
            return t.call(n, e, i++);
          }),
          r
        );
      }
      function T(e) {
        if (e._status === -1) {
          var t = e._result;
          ((t = t()),
            t.then(
              function (t) {
                (e._status === 0 || e._status === -1) &&
                  ((e._status = 1), (e._result = t));
              },
              function (t) {
                (e._status === 0 || e._status === -1) &&
                  ((e._status = 2), (e._result = t));
              },
            ),
            e._status === -1 && ((e._status = 0), (e._result = t)));
        }
        if (e._status === 1) return e._result.default;
        throw e._result;
      }
      var E =
          typeof reportError == `function`
            ? reportError
            : function (e) {
                if (
                  typeof window == `object` &&
                  typeof window.ErrorEvent == `function`
                ) {
                  var t = new window.ErrorEvent(`error`, {
                    bubbles: !0,
                    cancelable: !0,
                    message:
                      typeof e == `object` && e && typeof e.message == `string`
                        ? String(e.message)
                        : String(e),
                    error: e,
                  });
                  if (!window.dispatchEvent(t)) return;
                } else if (
                  typeof process == `object` &&
                  typeof process.emit == `function`
                ) {
                  process.emit(`uncaughtException`, e);
                  return;
                }
                console.error(e);
              },
        D = {
          map: le,
          forEach: function (e, t, n) {
            le(
              e,
              function () {
                t.apply(this, arguments);
              },
              n,
            );
          },
          count: function (e) {
            var t = 0;
            return (
              le(e, function () {
                t++;
              }),
              t
            );
          },
          toArray: function (e) {
            return (
              le(e, function (e) {
                return e;
              }) || []
            );
          },
          only: function (e) {
            if (!re(e))
              throw Error(
                `React.Children.only expected to receive a single React element child.`,
              );
            return e;
          },
        };
      ((e.Activity = f),
        (e.Children = D),
        (e.Component = v),
        (e.Fragment = r),
        (e.Profiler = a),
        (e.PureComponent = b),
        (e.StrictMode = i),
        (e.Suspense = l),
        (e.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = C),
        (e.__COMPILER_RUNTIME = {
          __proto__: null,
          c: function (e) {
            return C.H.useMemoCache(e);
          },
        }),
        (e.cache = function (e) {
          return function () {
            return e.apply(null, arguments);
          };
        }),
        (e.cacheSignal = function () {
          return null;
        }),
        (e.cloneElement = function (e, t, n) {
          if (e == null)
            throw Error(
              `The argument must be a React element, but you passed ` + e + `.`,
            );
          var r = g({}, e.props),
            i = e.key;
          if (t != null)
            for (a in (t.key !== void 0 && (i = `` + t.key), t))
              !w.call(t, a) ||
                a === `key` ||
                a === `__self` ||
                a === `__source` ||
                (a === `ref` && t.ref === void 0) ||
                (r[a] = t[a]);
          var a = arguments.length - 2;
          if (a === 1) r.children = n;
          else if (1 < a) {
            for (var o = Array(a), s = 0; s < a; s++) o[s] = arguments[s + 2];
            r.children = o;
          }
          return te(e.type, i, r);
        }),
        (e.createContext = function (e) {
          return (
            (e = {
              $$typeof: s,
              _currentValue: e,
              _currentValue2: e,
              _threadCount: 0,
              Provider: null,
              Consumer: null,
            }),
            (e.Provider = e),
            (e.Consumer = { $$typeof: o, _context: e }),
            e
          );
        }),
        (e.createElement = function (e, t, n) {
          var r,
            i = {},
            a = null;
          if (t != null)
            for (r in (t.key !== void 0 && (a = `` + t.key), t))
              w.call(t, r) &&
                r !== `key` &&
                r !== `__self` &&
                r !== `__source` &&
                (i[r] = t[r]);
          var o = arguments.length - 2;
          if (o === 1) i.children = n;
          else if (1 < o) {
            for (var s = Array(o), c = 0; c < o; c++) s[c] = arguments[c + 2];
            i.children = s;
          }
          if (e && e.defaultProps)
            for (r in ((o = e.defaultProps), o))
              i[r] === void 0 && (i[r] = o[r]);
          return te(e, a, i);
        }),
        (e.createRef = function () {
          return { current: null };
        }),
        (e.forwardRef = function (e) {
          return { $$typeof: c, render: e };
        }),
        (e.isValidElement = re),
        (e.lazy = function (e) {
          return {
            $$typeof: d,
            _payload: { _status: -1, _result: e },
            _init: T,
          };
        }),
        (e.memo = function (e, t) {
          return { $$typeof: u, type: e, compare: t === void 0 ? null : t };
        }),
        (e.startTransition = function (e) {
          var t = C.T,
            n = {};
          C.T = n;
          try {
            var r = e(),
              i = C.S;
            (i !== null && i(n, r),
              typeof r == `object` &&
                r &&
                typeof r.then == `function` &&
                r.then(S, E));
          } catch (e) {
            E(e);
          } finally {
            (t !== null && n.types !== null && (t.types = n.types), (C.T = t));
          }
        }),
        (e.unstable_useCacheRefresh = function () {
          return C.H.useCacheRefresh();
        }),
        (e.use = function (e) {
          return C.H.use(e);
        }),
        (e.useActionState = function (e, t, n) {
          return C.H.useActionState(e, t, n);
        }),
        (e.useCallback = function (e, t) {
          return C.H.useCallback(e, t);
        }),
        (e.useContext = function (e) {
          return C.H.useContext(e);
        }),
        (e.useDebugValue = function () {}),
        (e.useDeferredValue = function (e, t) {
          return C.H.useDeferredValue(e, t);
        }),
        (e.useEffect = function (e, t) {
          return C.H.useEffect(e, t);
        }),
        (e.useEffectEvent = function (e) {
          return C.H.useEffectEvent(e);
        }),
        (e.useId = function () {
          return C.H.useId();
        }),
        (e.useImperativeHandle = function (e, t, n) {
          return C.H.useImperativeHandle(e, t, n);
        }),
        (e.useInsertionEffect = function (e, t) {
          return C.H.useInsertionEffect(e, t);
        }),
        (e.useLayoutEffect = function (e, t) {
          return C.H.useLayoutEffect(e, t);
        }),
        (e.useMemo = function (e, t) {
          return C.H.useMemo(e, t);
        }),
        (e.useOptimistic = function (e, t) {
          return C.H.useOptimistic(e, t);
        }),
        (e.useReducer = function (e, t, n) {
          return C.H.useReducer(e, t, n);
        }),
        (e.useRef = function (e) {
          return C.H.useRef(e);
        }),
        (e.useState = function (e) {
          return C.H.useState(e);
        }),
        (e.useSyncExternalStore = function (e, t, n) {
          return C.H.useSyncExternalStore(e, t, n);
        }),
        (e.useTransition = function () {
          return C.H.useTransition();
        }),
        (e.version = `19.2.8`));
    }),
    g = c((e, t) => {
      t.exports = h();
    }),
    _ = c((e) => {
      var t = g();
      function n(e) {
        var t = `https://react.dev/errors/` + e;
        if (1 < arguments.length) {
          t += `?args[]=` + encodeURIComponent(arguments[1]);
          for (var n = 2; n < arguments.length; n++)
            t += `&args[]=` + encodeURIComponent(arguments[n]);
        }
        return (
          `Minified React error #` +
          e +
          `; visit ` +
          t +
          ` for the full message or use the non-minified dev environment for full errors and additional helpful warnings.`
        );
      }
      function r() {}
      var i = {
          d: {
            f: r,
            r: function () {
              throw Error(n(522));
            },
            D: r,
            C: r,
            L: r,
            m: r,
            X: r,
            S: r,
            M: r,
          },
          p: 0,
          findDOMNode: null,
        },
        a = Symbol.for(`react.portal`);
      function o(e, t, n) {
        var r =
          3 < arguments.length && arguments[3] !== void 0 ? arguments[3] : null;
        return {
          $$typeof: a,
          key: r == null ? null : `` + r,
          children: e,
          containerInfo: t,
          implementation: n,
        };
      }
      var s = t.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE;
      function c(e, t) {
        if (e === `font`) return ``;
        if (typeof t == `string`) return t === `use-credentials` ? t : ``;
      }
      ((e.__DOM_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = i),
        (e.createPortal = function (e, t) {
          var r =
            2 < arguments.length && arguments[2] !== void 0
              ? arguments[2]
              : null;
          if (!t || (t.nodeType !== 1 && t.nodeType !== 9 && t.nodeType !== 11))
            throw Error(n(299));
          return o(e, t, null, r);
        }),
        (e.flushSync = function (e) {
          var t = s.T,
            n = i.p;
          try {
            if (((s.T = null), (i.p = 2), e)) return e();
          } finally {
            ((s.T = t), (i.p = n), i.d.f());
          }
        }),
        (e.preconnect = function (e, t) {
          typeof e == `string` &&
            (t
              ? ((t = t.crossOrigin),
                (t =
                  typeof t == `string`
                    ? t === `use-credentials`
                      ? t
                      : ``
                    : void 0))
              : (t = null),
            i.d.C(e, t));
        }),
        (e.prefetchDNS = function (e) {
          typeof e == `string` && i.d.D(e);
        }),
        (e.preinit = function (e, t) {
          if (typeof e == `string` && t && typeof t.as == `string`) {
            var n = t.as,
              r = c(n, t.crossOrigin),
              a = typeof t.integrity == `string` ? t.integrity : void 0,
              o = typeof t.fetchPriority == `string` ? t.fetchPriority : void 0;
            n === `style`
              ? i.d.S(
                  e,
                  typeof t.precedence == `string` ? t.precedence : void 0,
                  { crossOrigin: r, integrity: a, fetchPriority: o },
                )
              : n === `script` &&
                i.d.X(e, {
                  crossOrigin: r,
                  integrity: a,
                  fetchPriority: o,
                  nonce: typeof t.nonce == `string` ? t.nonce : void 0,
                });
          }
        }),
        (e.preinitModule = function (e, t) {
          if (typeof e == `string`)
            if (typeof t == `object` && t) {
              if (t.as == null || t.as === `script`) {
                var n = c(t.as, t.crossOrigin);
                i.d.M(e, {
                  crossOrigin: n,
                  integrity:
                    typeof t.integrity == `string` ? t.integrity : void 0,
                  nonce: typeof t.nonce == `string` ? t.nonce : void 0,
                });
              }
            } else t ?? i.d.M(e);
        }),
        (e.preload = function (e, t) {
          if (
            typeof e == `string` &&
            typeof t == `object` &&
            t &&
            typeof t.as == `string`
          ) {
            var n = t.as,
              r = c(n, t.crossOrigin);
            i.d.L(e, n, {
              crossOrigin: r,
              integrity: typeof t.integrity == `string` ? t.integrity : void 0,
              nonce: typeof t.nonce == `string` ? t.nonce : void 0,
              type: typeof t.type == `string` ? t.type : void 0,
              fetchPriority:
                typeof t.fetchPriority == `string` ? t.fetchPriority : void 0,
              referrerPolicy:
                typeof t.referrerPolicy == `string` ? t.referrerPolicy : void 0,
              imageSrcSet:
                typeof t.imageSrcSet == `string` ? t.imageSrcSet : void 0,
              imageSizes:
                typeof t.imageSizes == `string` ? t.imageSizes : void 0,
              media: typeof t.media == `string` ? t.media : void 0,
            });
          }
        }),
        (e.preloadModule = function (e, t) {
          if (typeof e == `string`)
            if (t) {
              var n = c(t.as, t.crossOrigin);
              i.d.m(e, {
                as:
                  typeof t.as == `string` && t.as !== `script` ? t.as : void 0,
                crossOrigin: n,
                integrity:
                  typeof t.integrity == `string` ? t.integrity : void 0,
              });
            } else i.d.m(e);
        }),
        (e.requestFormReset = function (e) {
          i.d.r(e);
        }),
        (e.unstable_batchedUpdates = function (e, t) {
          return e(t);
        }),
        (e.useFormState = function (e, t, n) {
          return s.H.useFormState(e, t, n);
        }),
        (e.useFormStatus = function () {
          return s.H.useHostTransitionStatus();
        }),
        (e.version = `19.2.8`));
    }),
    v = c((e, t) => {
      function n() {
        if (
          !(
            typeof __REACT_DEVTOOLS_GLOBAL_HOOK__ > `u` ||
            typeof __REACT_DEVTOOLS_GLOBAL_HOOK__.checkDCE != `function`
          )
        )
          try {
            __REACT_DEVTOOLS_GLOBAL_HOOK__.checkDCE(n);
          } catch (e) {
            console.error(e);
          }
      }
      (n(), (t.exports = _()));
    }),
    y = c((e) => {
      var t = m(),
        n = g(),
        r = v();
      function i(e) {
        var t = `https://react.dev/errors/` + e;
        if (1 < arguments.length) {
          t += `?args[]=` + encodeURIComponent(arguments[1]);
          for (var n = 2; n < arguments.length; n++)
            t += `&args[]=` + encodeURIComponent(arguments[n]);
        }
        return (
          `Minified React error #` +
          e +
          `; visit ` +
          t +
          ` for the full message or use the non-minified dev environment for full errors and additional helpful warnings.`
        );
      }
      function a(e) {
        return !(
          !e ||
          (e.nodeType !== 1 && e.nodeType !== 9 && e.nodeType !== 11)
        );
      }
      function o(e) {
        var t = e,
          n = e;
        if (e.alternate) for (; t.return; ) t = t.return;
        else {
          e = t;
          do ((t = e), t.flags & 4098 && (n = t.return), (e = t.return));
          while (e);
        }
        return t.tag === 3 ? n : null;
      }
      function s(e) {
        if (e.tag === 13) {
          var t = e.memoizedState;
          if (
            (t === null &&
              ((e = e.alternate), e !== null && (t = e.memoizedState)),
            t !== null)
          )
            return t.dehydrated;
        }
        return null;
      }
      function c(e) {
        if (e.tag === 31) {
          var t = e.memoizedState;
          if (
            (t === null &&
              ((e = e.alternate), e !== null && (t = e.memoizedState)),
            t !== null)
          )
            return t.dehydrated;
        }
        return null;
      }
      function l(e) {
        if (o(e) !== e) throw Error(i(188));
      }
      function u(e) {
        var t = e.alternate;
        if (!t) {
          if (((t = o(e)), t === null)) throw Error(i(188));
          return t === e ? e : null;
        }
        for (var n = e, r = t; ; ) {
          var a = n.return;
          if (a === null) break;
          var s = a.alternate;
          if (s === null) {
            if (((r = a.return), r !== null)) {
              n = r;
              continue;
            }
            break;
          }
          if (a.child === s.child) {
            for (s = a.child; s; ) {
              if (s === n) return (l(a), e);
              if (s === r) return (l(a), t);
              s = s.sibling;
            }
            throw Error(i(188));
          }
          if (n.return !== r.return) ((n = a), (r = s));
          else {
            for (var c = !1, u = a.child; u; ) {
              if (u === n) {
                ((c = !0), (n = a), (r = s));
                break;
              }
              if (u === r) {
                ((c = !0), (r = a), (n = s));
                break;
              }
              u = u.sibling;
            }
            if (!c) {
              for (u = s.child; u; ) {
                if (u === n) {
                  ((c = !0), (n = s), (r = a));
                  break;
                }
                if (u === r) {
                  ((c = !0), (r = s), (n = a));
                  break;
                }
                u = u.sibling;
              }
              if (!c) throw Error(i(189));
            }
          }
          if (n.alternate !== r) throw Error(i(190));
        }
        if (n.tag !== 3) throw Error(i(188));
        return n.stateNode.current === n ? e : t;
      }
      function d(e) {
        var t = e.tag;
        if (t === 5 || t === 26 || t === 27 || t === 6) return e;
        for (e = e.child; e !== null; ) {
          if (((t = d(e)), t !== null)) return t;
          e = e.sibling;
        }
        return null;
      }
      var f = Object.assign,
        p = Symbol.for(`react.element`),
        h = Symbol.for(`react.transitional.element`),
        _ = Symbol.for(`react.portal`),
        y = Symbol.for(`react.fragment`),
        b = Symbol.for(`react.strict_mode`),
        x = Symbol.for(`react.profiler`),
        ee = Symbol.for(`react.consumer`),
        S = Symbol.for(`react.context`),
        C = Symbol.for(`react.forward_ref`),
        w = Symbol.for(`react.suspense`),
        te = Symbol.for(`react.suspense_list`),
        ne = Symbol.for(`react.memo`),
        re = Symbol.for(`react.lazy`),
        ie = Symbol.for(`react.activity`),
        ae = Symbol.for(`react.memo_cache_sentinel`),
        oe = Symbol.iterator;
      function se(e) {
        return typeof e != `object` || !e
          ? null
          : ((e = (oe && e[oe]) || e[`@@iterator`]),
            typeof e == `function` ? e : null);
      }
      var ce = Symbol.for(`react.client.reference`);
      function le(e) {
        if (e == null) return null;
        if (typeof e == `function`)
          return e.$$typeof === ce ? null : e.displayName || e.name || null;
        if (typeof e == `string`) return e;
        switch (e) {
          case y:
            return `Fragment`;
          case x:
            return `Profiler`;
          case b:
            return `StrictMode`;
          case w:
            return `Suspense`;
          case te:
            return `SuspenseList`;
          case ie:
            return `Activity`;
        }
        if (typeof e == `object`)
          switch (e.$$typeof) {
            case _:
              return `Portal`;
            case S:
              return e.displayName || `Context`;
            case ee:
              return (e._context.displayName || `Context`) + `.Consumer`;
            case C:
              var t = e.render;
              return (
                (e = e.displayName),
                (e ||=
                  ((e = t.displayName || t.name || ``),
                  e === `` ? `ForwardRef` : `ForwardRef(` + e + `)`)),
                e
              );
            case ne:
              return (
                (t = e.displayName || null),
                t === null ? le(e.type) || `Memo` : t
              );
            case re:
              ((t = e._payload), (e = e._init));
              try {
                return le(e(t));
              } catch {}
          }
        return null;
      }
      var T = Array.isArray,
        E = n.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE,
        D = r.__DOM_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE,
        ue = { pending: !1, data: null, method: null, action: null },
        de = [],
        fe = -1;
      function pe(e) {
        return { current: e };
      }
      function O(e) {
        0 > fe || ((e.current = de[fe]), (de[fe] = null), fe--);
      }
      function k(e, t) {
        (fe++, (de[fe] = e.current), (e.current = t));
      }
      var me = pe(null),
        he = pe(null),
        ge = pe(null),
        _e = pe(null);
      function A(e, t) {
        switch ((k(ge, t), k(he, e), k(me, null), t.nodeType)) {
          case 9:
          case 11:
            e = (e = t.documentElement) && (e = e.namespaceURI) ? Gd(e) : 0;
            break;
          default:
            if (((e = t.tagName), (t = t.namespaceURI)))
              ((t = Gd(t)), (e = Kd(t, e)));
            else
              switch (e) {
                case `svg`:
                  e = 1;
                  break;
                case `math`:
                  e = 2;
                  break;
                default:
                  e = 0;
              }
        }
        (O(me), k(me, e));
      }
      function ve() {
        (O(me), O(he), O(ge));
      }
      function ye(e) {
        e.memoizedState !== null && k(_e, e);
        var t = me.current,
          n = Kd(t, e.type);
        t !== n && (k(he, e), k(me, n));
      }
      function be(e) {
        (he.current === e && (O(me), O(he)),
          _e.current === e && (O(_e), (np._currentValue = ue)));
      }
      var xe, Se;
      function Ce(e) {
        if (xe === void 0)
          try {
            throw Error();
          } catch (e) {
            var t = e.stack.trim().match(/\n( *(at )?)/);
            ((xe = (t && t[1]) || ``),
              (Se =
                -1 <
                e.stack.indexOf(`
    at`)
                  ? ` (<anonymous>)`
                  : -1 < e.stack.indexOf(`@`)
                    ? `@unknown:0:0`
                    : ``));
          }
        return (
          `
` +
          xe +
          e +
          Se
        );
      }
      var we = !1;
      function Te(e, t) {
        if (!e || we) return ``;
        we = !0;
        var n = Error.prepareStackTrace;
        Error.prepareStackTrace = void 0;
        try {
          var r = {
            DetermineComponentFrameRoot: function () {
              try {
                if (t) {
                  var n = function () {
                    throw Error();
                  };
                  if (
                    (Object.defineProperty(n.prototype, "props", {
                      set: function () {
                        throw Error();
                      },
                    }),
                    typeof Reflect == `object` && Reflect.construct)
                  ) {
                    try {
                      Reflect.construct(n, []);
                    } catch (e) {
                      var r = e;
                    }
                    Reflect.construct(e, [], n);
                  } else {
                    try {
                      n.call();
                    } catch (e) {
                      r = e;
                    }
                    e.call(n.prototype);
                  }
                } else {
                  try {
                    throw Error();
                  } catch (e) {
                    r = e;
                  }
                  (n = e()) &&
                    typeof n.catch == `function` &&
                    n.catch(function () {});
                }
              } catch (e) {
                if (e && r && typeof e.stack == `string`)
                  return [e.stack, r.stack];
              }
              return [null, null];
            },
          };
          r.DetermineComponentFrameRoot.displayName = `DetermineComponentFrameRoot`;
          var i = Object.getOwnPropertyDescriptor(
            r.DetermineComponentFrameRoot,
            `name`,
          );
          i &&
            i.configurable &&
            Object.defineProperty(r.DetermineComponentFrameRoot, "name", {
              value: `DetermineComponentFrameRoot`,
            });
          var a = r.DetermineComponentFrameRoot(),
            o = a[0],
            s = a[1];
          if (o && s) {
            var c = o.split(`
`),
              l = s.split(`
`);
            for (
              i = r = 0;
              r < c.length && !c[r].includes(`DetermineComponentFrameRoot`);
            )
              r++;
            for (
              ;
              i < l.length && !l[i].includes(`DetermineComponentFrameRoot`);
            )
              i++;
            if (r === c.length || i === l.length)
              for (
                r = c.length - 1, i = l.length - 1;
                1 <= r && 0 <= i && c[r] !== l[i];
              )
                i--;
            for (; 1 <= r && 0 <= i; r--, i--)
              if (c[r] !== l[i]) {
                if (r !== 1 || i !== 1)
                  do
                    if ((r--, i--, 0 > i || c[r] !== l[i])) {
                      var u =
                        `
` + c[r].replace(` at new `, ` at `);
                      return (
                        e.displayName &&
                          u.includes(`<anonymous>`) &&
                          (u = u.replace(`<anonymous>`, e.displayName)),
                        u
                      );
                    }
                  while (1 <= r && 0 <= i);
                break;
              }
          }
        } finally {
          ((we = !1), (Error.prepareStackTrace = n));
        }
        return (n = e ? e.displayName || e.name : ``) ? Ce(n) : ``;
      }
      function Ee(e, t) {
        switch (e.tag) {
          case 26:
          case 27:
          case 5:
            return Ce(e.type);
          case 16:
            return Ce(`Lazy`);
          case 13:
            return e.child !== t && t !== null
              ? Ce(`Suspense Fallback`)
              : Ce(`Suspense`);
          case 19:
            return Ce(`SuspenseList`);
          case 0:
          case 15:
            return Te(e.type, !1);
          case 11:
            return Te(e.type.render, !1);
          case 1:
            return Te(e.type, !0);
          case 31:
            return Ce(`Activity`);
          default:
            return ``;
        }
      }
      function De(e) {
        try {
          var t = ``,
            n = null;
          do ((t += Ee(e, n)), (n = e), (e = e.return));
          while (e);
          return t;
        } catch (e) {
          return (
            `
Error generating stack: ` +
            e.message +
            `
` +
            e.stack
          );
        }
      }
      var Oe = Object.prototype.hasOwnProperty,
        ke = t.unstable_scheduleCallback,
        Ae = t.unstable_cancelCallback,
        je = t.unstable_shouldYield,
        Me = t.unstable_requestPaint,
        Ne = t.unstable_now,
        Pe = t.unstable_getCurrentPriorityLevel,
        Fe = t.unstable_ImmediatePriority,
        Ie = t.unstable_UserBlockingPriority,
        Le = t.unstable_NormalPriority,
        Re = t.unstable_LowPriority,
        ze = t.unstable_IdlePriority,
        Be = t.log,
        Ve = t.unstable_setDisableYieldValue,
        He = null,
        Ue = null;
      function We(e) {
        if (
          (typeof Be == `function` && Ve(e),
          Ue && typeof Ue.setStrictMode == `function`)
        )
          try {
            Ue.setStrictMode(He, e);
          } catch {}
      }
      var Ge = Math.clz32 ? Math.clz32 : Je,
        Ke = Math.log,
        qe = Math.LN2;
      function Je(e) {
        return ((e >>>= 0), e === 0 ? 32 : (31 - ((Ke(e) / qe) | 0)) | 0);
      }
      var Ye = 256,
        Xe = 262144,
        Ze = 4194304;
      function Qe(e) {
        var t = e & 42;
        if (t !== 0) return t;
        switch (e & -e) {
          case 1:
            return 1;
          case 2:
            return 2;
          case 4:
            return 4;
          case 8:
            return 8;
          case 16:
            return 16;
          case 32:
            return 32;
          case 64:
            return 64;
          case 128:
            return 128;
          case 256:
          case 512:
          case 1024:
          case 2048:
          case 4096:
          case 8192:
          case 16384:
          case 32768:
          case 65536:
          case 131072:
            return e & 261888;
          case 262144:
          case 524288:
          case 1048576:
          case 2097152:
            return e & 3932160;
          case 4194304:
          case 8388608:
          case 16777216:
          case 33554432:
            return e & 62914560;
          case 67108864:
            return 67108864;
          case 134217728:
            return 134217728;
          case 268435456:
            return 268435456;
          case 536870912:
            return 536870912;
          case 1073741824:
            return 0;
          default:
            return e;
        }
      }
      function $e(e, t, n) {
        var r = e.pendingLanes;
        if (r === 0) return 0;
        var i = 0,
          a = e.suspendedLanes,
          o = e.pingedLanes;
        e = e.warmLanes;
        var s = r & 134217727;
        return (
          s === 0
            ? ((s = r & ~a),
              s === 0
                ? o === 0
                  ? n || ((n = r & ~e), n !== 0 && (i = Qe(n)))
                  : (i = Qe(o))
                : (i = Qe(s)))
            : ((r = s & ~a),
              r === 0
                ? ((o &= s),
                  o === 0
                    ? n || ((n = s & ~e), n !== 0 && (i = Qe(n)))
                    : (i = Qe(o)))
                : (i = Qe(r))),
          i === 0
            ? 0
            : t !== 0 &&
                t !== i &&
                (t & a) === 0 &&
                ((a = i & -i),
                (n = t & -t),
                a >= n || (a === 32 && n & 4194048))
              ? t
              : i
        );
      }
      function et(e, t) {
        return (
          (e.pendingLanes & ~(e.suspendedLanes & ~e.pingedLanes) & t) === 0
        );
      }
      function tt(e, t) {
        switch (e) {
          case 1:
          case 2:
          case 4:
          case 8:
          case 64:
            return t + 250;
          case 16:
          case 32:
          case 128:
          case 256:
          case 512:
          case 1024:
          case 2048:
          case 4096:
          case 8192:
          case 16384:
          case 32768:
          case 65536:
          case 131072:
          case 262144:
          case 524288:
          case 1048576:
          case 2097152:
            return t + 5e3;
          case 4194304:
          case 8388608:
          case 16777216:
          case 33554432:
            return -1;
          case 67108864:
          case 134217728:
          case 268435456:
          case 536870912:
          case 1073741824:
            return -1;
          default:
            return -1;
        }
      }
      function nt() {
        var e = Ze;
        return ((Ze <<= 1), !(Ze & 62914560) && (Ze = 4194304), e);
      }
      function rt(e) {
        for (var t = [], n = 0; 31 > n; n++) t.push(e);
        return t;
      }
      function it(e, t) {
        ((e.pendingLanes |= t),
          t !== 268435456 &&
            ((e.suspendedLanes = 0), (e.pingedLanes = 0), (e.warmLanes = 0)));
      }
      function at(e, t, n, r, i, a) {
        var o = e.pendingLanes;
        ((e.pendingLanes = n),
          (e.suspendedLanes = 0),
          (e.pingedLanes = 0),
          (e.warmLanes = 0),
          (e.expiredLanes &= n),
          (e.entangledLanes &= n),
          (e.errorRecoveryDisabledLanes &= n),
          (e.shellSuspendCounter = 0));
        var s = e.entanglements,
          c = e.expirationTimes,
          l = e.hiddenUpdates;
        for (n = o & ~n; 0 < n; ) {
          var u = 31 - Ge(n),
            d = 1 << u;
          ((s[u] = 0), (c[u] = -1));
          var f = l[u];
          if (f !== null)
            for (l[u] = null, u = 0; u < f.length; u++) {
              var p = f[u];
              p !== null && (p.lane &= -536870913);
            }
          n &= ~d;
        }
        (r !== 0 && ot(e, r, 0),
          a !== 0 &&
            i === 0 &&
            e.tag !== 0 &&
            (e.suspendedLanes |= a & ~(o & ~t)));
      }
      function ot(e, t, n) {
        ((e.pendingLanes |= t), (e.suspendedLanes &= ~t));
        var r = 31 - Ge(t);
        ((e.entangledLanes |= t),
          (e.entanglements[r] =
            e.entanglements[r] | 1073741824 | (n & 261930)));
      }
      function st(e, t) {
        var n = (e.entangledLanes |= t);
        for (e = e.entanglements; n; ) {
          var r = 31 - Ge(n),
            i = 1 << r;
          ((i & t) | (e[r] & t) && (e[r] |= t), (n &= ~i));
        }
      }
      function ct(e, t) {
        var n = t & -t;
        return (
          (n = n & 42 ? 1 : lt(n)), (n & (e.suspendedLanes | t)) === 0 ? n : 0
        );
      }
      function lt(e) {
        switch (e) {
          case 2:
            e = 1;
            break;
          case 8:
            e = 4;
            break;
          case 32:
            e = 16;
            break;
          case 256:
          case 512:
          case 1024:
          case 2048:
          case 4096:
          case 8192:
          case 16384:
          case 32768:
          case 65536:
          case 131072:
          case 262144:
          case 524288:
          case 1048576:
          case 2097152:
          case 4194304:
          case 8388608:
          case 16777216:
          case 33554432:
            e = 128;
            break;
          case 268435456:
            e = 134217728;
            break;
          default:
            e = 0;
        }
        return e;
      }
      function ut(e) {
        return (
          (e &= -e), 2 < e ? (8 < e ? (e & 134217727 ? 32 : 268435456) : 8) : 2
        );
      }
      function dt() {
        var e = D.p;
        return e === 0
          ? ((e = window.event), e === void 0 ? 32 : vp(e.type))
          : e;
      }
      function ft(e, t) {
        var n = D.p;
        try {
          return ((D.p = e), t());
        } finally {
          D.p = n;
        }
      }
      var pt = Math.random().toString(36).slice(2),
        mt = `__reactFiber$` + pt,
        ht = `__reactProps$` + pt,
        gt = `__reactContainer$` + pt,
        _t = `__reactEvents$` + pt,
        vt = `__reactListeners$` + pt,
        yt = `__reactHandles$` + pt,
        bt = `__reactResources$` + pt,
        xt = `__reactMarker$` + pt;
      function St(e) {
        (delete e[mt], delete e[ht], delete e[_t], delete e[vt], delete e[yt]);
      }
      function Ct(e) {
        var t = e[mt];
        if (t) return t;
        for (var n = e.parentNode; n; ) {
          if ((t = n[gt] || n[mt])) {
            if (
              ((n = t.alternate),
              t.child !== null || (n !== null && n.child !== null))
            )
              for (e = hf(e); e !== null; ) {
                if ((n = e[mt])) return n;
                e = hf(e);
              }
            return t;
          }
          ((e = n), (n = e.parentNode));
        }
        return null;
      }
      function wt(e) {
        if ((e = e[mt] || e[gt])) {
          var t = e.tag;
          if (
            t === 5 ||
            t === 6 ||
            t === 13 ||
            t === 31 ||
            t === 26 ||
            t === 27 ||
            t === 3
          )
            return e;
        }
        return null;
      }
      function Tt(e) {
        var t = e.tag;
        if (t === 5 || t === 26 || t === 27 || t === 6) return e.stateNode;
        throw Error(i(33));
      }
      function Et(e) {
        var t = e[bt];
        return (
          (t ||= e[bt] =
            { hoistableStyles: new Map(), hoistableScripts: new Map() }),
          t
        );
      }
      function Dt(e) {
        e[xt] = !0;
      }
      var Ot = new Set(),
        kt = {};
      function At(e, t) {
        (jt(e, t), jt(e + `Capture`, t));
      }
      function jt(e, t) {
        for (kt[e] = t, e = 0; e < t.length; e++) Ot.add(t[e]);
      }
      var Mt = RegExp(
          `^[:A-Z_a-z\\u00C0-\\u00D6\\u00D8-\\u00F6\\u00F8-\\u02FF\\u0370-\\u037D\\u037F-\\u1FFF\\u200C-\\u200D\\u2070-\\u218F\\u2C00-\\u2FEF\\u3001-\\uD7FF\\uF900-\\uFDCF\\uFDF0-\\uFFFD][:A-Z_a-z\\u00C0-\\u00D6\\u00D8-\\u00F6\\u00F8-\\u02FF\\u0370-\\u037D\\u037F-\\u1FFF\\u200C-\\u200D\\u2070-\\u218F\\u2C00-\\u2FEF\\u3001-\\uD7FF\\uF900-\\uFDCF\\uFDF0-\\uFFFD\\-.0-9\\u00B7\\u0300-\\u036F\\u203F-\\u2040]*$`,
        ),
        Nt = {},
        Pt = {};
      function Ft(e) {
        return Oe.call(Pt, e)
          ? !0
          : Oe.call(Nt, e)
            ? !1
            : Mt.test(e)
              ? (Pt[e] = !0)
              : ((Nt[e] = !0), !1);
      }
      function It(e, t, n) {
        if (Ft(t))
          if (n === null) e.removeAttribute(t);
          else {
            switch (typeof n) {
              case `undefined`:
              case `function`:
              case `symbol`:
                e.removeAttribute(t);
                return;
              case `boolean`:
                var r = t.toLowerCase().slice(0, 5);
                if (r !== `data-` && r !== `aria-`) {
                  e.removeAttribute(t);
                  return;
                }
            }
            e.setAttribute(t, `` + n);
          }
      }
      function Lt(e, t, n) {
        if (n === null) e.removeAttribute(t);
        else {
          switch (typeof n) {
            case `undefined`:
            case `function`:
            case `symbol`:
            case `boolean`:
              e.removeAttribute(t);
              return;
          }
          e.setAttribute(t, `` + n);
        }
      }
      function Rt(e, t, n, r) {
        if (r === null) e.removeAttribute(n);
        else {
          switch (typeof r) {
            case `undefined`:
            case `function`:
            case `symbol`:
            case `boolean`:
              e.removeAttribute(n);
              return;
          }
          e.setAttributeNS(t, n, `` + r);
        }
      }
      function zt(e) {
        switch (typeof e) {
          case `bigint`:
          case `boolean`:
          case `number`:
          case `string`:
          case `undefined`:
            return e;
          case `object`:
            return e;
          default:
            return ``;
        }
      }
      function Bt(e) {
        var t = e.type;
        return (
          (e = e.nodeName) &&
          e.toLowerCase() === `input` &&
          (t === `checkbox` || t === `radio`)
        );
      }
      function Vt(e, t, n) {
        var r = Object.getOwnPropertyDescriptor(e.constructor.prototype, t);
        if (
          !e.hasOwnProperty(t) &&
          r !== void 0 &&
          typeof r.get == `function` &&
          typeof r.set == `function`
        ) {
          var i = r.get,
            a = r.set;
          return (
            Object.defineProperty(e, t, {
              configurable: !0,
              get: function () {
                return i.call(this);
              },
              set: function (e) {
                ((n = `` + e), a.call(this, e));
              },
            }),
            Object.defineProperty(e, t, { enumerable: r.enumerable }),
            {
              getValue: function () {
                return n;
              },
              setValue: function (e) {
                n = `` + e;
              },
              stopTracking: function () {
                ((e._valueTracker = null), delete e[t]);
              },
            }
          );
        }
      }
      function Ht(e) {
        if (!e._valueTracker) {
          var t = Bt(e) ? `checked` : `value`;
          e._valueTracker = Vt(e, t, `` + e[t]);
        }
      }
      function Ut(e) {
        if (!e) return !1;
        var t = e._valueTracker;
        if (!t) return !0;
        var n = t.getValue(),
          r = ``;
        return (
          e && (r = Bt(e) ? (e.checked ? `true` : `false`) : e.value),
          (e = r),
          e === n ? !1 : (t.setValue(e), !0)
        );
      }
      function Wt(e) {
        if (((e ||= typeof document < `u` ? document : void 0), e === void 0))
          return null;
        try {
          return e.activeElement || e.body;
        } catch {
          return e.body;
        }
      }
      var Gt = /[\n"\\]/g;
      function Kt(e) {
        return e.replace(Gt, function (e) {
          return `\\` + e.charCodeAt(0).toString(16) + ` `;
        });
      }
      function qt(e, t, n, r, i, a, o, s) {
        ((e.name = ``),
          o != null &&
          typeof o != `function` &&
          typeof o != `symbol` &&
          typeof o != `boolean`
            ? (e.type = o)
            : e.removeAttribute(`type`),
          t == null
            ? (o !== `submit` && o !== `reset`) || e.removeAttribute(`value`)
            : o === `number`
              ? ((t === 0 && e.value === ``) || e.value != t) &&
                (e.value = `` + zt(t))
              : e.value !== `` + zt(t) && (e.value = `` + zt(t)),
          t == null
            ? n == null
              ? r != null && e.removeAttribute(`value`)
              : Yt(e, o, zt(n))
            : Yt(e, o, zt(t)),
          i == null && a != null && (e.defaultChecked = !!a),
          i != null &&
            (e.checked = i && typeof i != `function` && typeof i != `symbol`),
          s != null &&
          typeof s != `function` &&
          typeof s != `symbol` &&
          typeof s != `boolean`
            ? (e.name = `` + zt(s))
            : e.removeAttribute(`name`));
      }
      function Jt(e, t, n, r, i, a, o, s) {
        if (
          (a != null &&
            typeof a != `function` &&
            typeof a != `symbol` &&
            typeof a != `boolean` &&
            (e.type = a),
          t != null || n != null)
        ) {
          if (!((a !== `submit` && a !== `reset`) || t != null)) {
            Ht(e);
            return;
          }
          ((n = n == null ? `` : `` + zt(n)),
            (t = t == null ? n : `` + zt(t)),
            s || t === e.value || (e.value = t),
            (e.defaultValue = t));
        }
        ((r ??= i),
          (r = typeof r != `function` && typeof r != `symbol` && !!r),
          (e.checked = s ? e.checked : !!r),
          (e.defaultChecked = !!r),
          o != null &&
            typeof o != `function` &&
            typeof o != `symbol` &&
            typeof o != `boolean` &&
            (e.name = o),
          Ht(e));
      }
      function Yt(e, t, n) {
        (t === `number` && Wt(e.ownerDocument) === e) ||
          e.defaultValue === `` + n ||
          (e.defaultValue = `` + n);
      }
      function Xt(e, t, n, r) {
        if (((e = e.options), t)) {
          t = {};
          for (var i = 0; i < n.length; i++) t[`$` + n[i]] = !0;
          for (n = 0; n < e.length; n++)
            ((i = t.hasOwnProperty(`$` + e[n].value)),
              e[n].selected !== i && (e[n].selected = i),
              i && r && (e[n].defaultSelected = !0));
        } else {
          for (n = `` + zt(n), t = null, i = 0; i < e.length; i++) {
            if (e[i].value === n) {
              ((e[i].selected = !0), r && (e[i].defaultSelected = !0));
              return;
            }
            t !== null || e[i].disabled || (t = e[i]);
          }
          t !== null && (t.selected = !0);
        }
      }
      function Zt(e, t, n) {
        if (
          t != null &&
          ((t = `` + zt(t)), t !== e.value && (e.value = t), n == null)
        ) {
          e.defaultValue !== t && (e.defaultValue = t);
          return;
        }
        e.defaultValue = n == null ? `` : `` + zt(n);
      }
      function Qt(e, t, n, r) {
        if (t == null) {
          if (r != null) {
            if (n != null) throw Error(i(92));
            if (T(r)) {
              if (1 < r.length) throw Error(i(93));
              r = r[0];
            }
            n = r;
          }
          ((n ??= ``), (t = n));
        }
        ((n = zt(t)),
          (e.defaultValue = n),
          (r = e.textContent),
          r === n && r !== `` && r !== null && (e.value = r),
          Ht(e));
      }
      function $t(e, t) {
        if (t) {
          var n = e.firstChild;
          if (n && n === e.lastChild && n.nodeType === 3) {
            n.nodeValue = t;
            return;
          }
        }
        e.textContent = t;
      }
      var en = new Set(
        `animationIterationCount aspectRatio borderImageOutset borderImageSlice borderImageWidth boxFlex boxFlexGroup boxOrdinalGroup columnCount columns flex flexGrow flexPositive flexShrink flexNegative flexOrder gridArea gridRow gridRowEnd gridRowSpan gridRowStart gridColumn gridColumnEnd gridColumnSpan gridColumnStart fontWeight lineClamp lineHeight opacity order orphans scale tabSize widows zIndex zoom fillOpacity floodOpacity stopOpacity strokeDasharray strokeDashoffset strokeMiterlimit strokeOpacity strokeWidth MozAnimationIterationCount MozBoxFlex MozBoxFlexGroup MozLineClamp msAnimationIterationCount msFlex msZoom msFlexGrow msFlexNegative msFlexOrder msFlexPositive msFlexShrink msGridColumn msGridColumnSpan msGridRow msGridRowSpan WebkitAnimationIterationCount WebkitBoxFlex WebKitBoxFlexGroup WebkitBoxOrdinalGroup WebkitColumnCount WebkitColumns WebkitFlex WebkitFlexGrow WebkitFlexPositive WebkitFlexShrink WebkitLineClamp`.split(
          ` `,
        ),
      );
      function tn(e, t, n) {
        var r = t.indexOf(`--`) === 0;
        n == null || typeof n == `boolean` || n === ``
          ? r
            ? e.setProperty(t, ``)
            : t === `float`
              ? (e.cssFloat = ``)
              : (e[t] = ``)
          : r
            ? e.setProperty(t, n)
            : typeof n != `number` || n === 0 || en.has(t)
              ? t === `float`
                ? (e.cssFloat = n)
                : (e[t] = (`` + n).trim())
              : (e[t] = n + `px`);
      }
      function nn(e, t, n) {
        if (t != null && typeof t != `object`) throw Error(i(62));
        if (((e = e.style), n != null)) {
          for (var r in n)
            !n.hasOwnProperty(r) ||
              (t != null && t.hasOwnProperty(r)) ||
              (r.indexOf(`--`) === 0
                ? e.setProperty(r, ``)
                : r === `float`
                  ? (e.cssFloat = ``)
                  : (e[r] = ``));
          for (var a in t)
            ((r = t[a]), t.hasOwnProperty(a) && n[a] !== r && tn(e, a, r));
        } else for (var o in t) t.hasOwnProperty(o) && tn(e, o, t[o]);
      }
      function rn(e) {
        if (e.indexOf(`-`) === -1) return !1;
        switch (e) {
          case `annotation-xml`:
          case `color-profile`:
          case `font-face`:
          case `font-face-src`:
          case `font-face-uri`:
          case `font-face-format`:
          case `font-face-name`:
          case `missing-glyph`:
            return !1;
          default:
            return !0;
        }
      }
      var an = new Map([
          [`acceptCharset`, `accept-charset`],
          [`htmlFor`, `for`],
          [`httpEquiv`, `http-equiv`],
          [`crossOrigin`, `crossorigin`],
          [`accentHeight`, `accent-height`],
          [`alignmentBaseline`, `alignment-baseline`],
          [`arabicForm`, `arabic-form`],
          [`baselineShift`, `baseline-shift`],
          [`capHeight`, `cap-height`],
          [`clipPath`, `clip-path`],
          [`clipRule`, `clip-rule`],
          [`colorInterpolation`, `color-interpolation`],
          [`colorInterpolationFilters`, `color-interpolation-filters`],
          [`colorProfile`, `color-profile`],
          [`colorRendering`, `color-rendering`],
          [`dominantBaseline`, `dominant-baseline`],
          [`enableBackground`, `enable-background`],
          [`fillOpacity`, `fill-opacity`],
          [`fillRule`, `fill-rule`],
          [`floodColor`, `flood-color`],
          [`floodOpacity`, `flood-opacity`],
          [`fontFamily`, `font-family`],
          [`fontSize`, `font-size`],
          [`fontSizeAdjust`, `font-size-adjust`],
          [`fontStretch`, `font-stretch`],
          [`fontStyle`, `font-style`],
          [`fontVariant`, `font-variant`],
          [`fontWeight`, `font-weight`],
          [`glyphName`, `glyph-name`],
          [`glyphOrientationHorizontal`, `glyph-orientation-horizontal`],
          [`glyphOrientationVertical`, `glyph-orientation-vertical`],
          [`horizAdvX`, `horiz-adv-x`],
          [`horizOriginX`, `horiz-origin-x`],
          [`imageRendering`, `image-rendering`],
          [`letterSpacing`, `letter-spacing`],
          [`lightingColor`, `lighting-color`],
          [`markerEnd`, `marker-end`],
          [`markerMid`, `marker-mid`],
          [`markerStart`, `marker-start`],
          [`overlinePosition`, `overline-position`],
          [`overlineThickness`, `overline-thickness`],
          [`paintOrder`, `paint-order`],
          [`panose-1`, `panose-1`],
          [`pointerEvents`, `pointer-events`],
          [`renderingIntent`, `rendering-intent`],
          [`shapeRendering`, `shape-rendering`],
          [`stopColor`, `stop-color`],
          [`stopOpacity`, `stop-opacity`],
          [`strikethroughPosition`, `strikethrough-position`],
          [`strikethroughThickness`, `strikethrough-thickness`],
          [`strokeDasharray`, `stroke-dasharray`],
          [`strokeDashoffset`, `stroke-dashoffset`],
          [`strokeLinecap`, `stroke-linecap`],
          [`strokeLinejoin`, `stroke-linejoin`],
          [`strokeMiterlimit`, `stroke-miterlimit`],
          [`strokeOpacity`, `stroke-opacity`],
          [`strokeWidth`, `stroke-width`],
          [`textAnchor`, `text-anchor`],
          [`textDecoration`, `text-decoration`],
          [`textRendering`, `text-rendering`],
          [`transformOrigin`, `transform-origin`],
          [`underlinePosition`, `underline-position`],
          [`underlineThickness`, `underline-thickness`],
          [`unicodeBidi`, `unicode-bidi`],
          [`unicodeRange`, `unicode-range`],
          [`unitsPerEm`, `units-per-em`],
          [`vAlphabetic`, `v-alphabetic`],
          [`vHanging`, `v-hanging`],
          [`vIdeographic`, `v-ideographic`],
          [`vMathematical`, `v-mathematical`],
          [`vectorEffect`, `vector-effect`],
          [`vertAdvY`, `vert-adv-y`],
          [`vertOriginX`, `vert-origin-x`],
          [`vertOriginY`, `vert-origin-y`],
          [`wordSpacing`, `word-spacing`],
          [`writingMode`, `writing-mode`],
          [`xmlnsXlink`, `xmlns:xlink`],
          [`xHeight`, `x-height`],
        ]),
        on =
          /^[\u0000-\u001F ]*j[\r\n\t]*a[\r\n\t]*v[\r\n\t]*a[\r\n\t]*s[\r\n\t]*c[\r\n\t]*r[\r\n\t]*i[\r\n\t]*p[\r\n\t]*t[\r\n\t]*:/i;
      function sn(e) {
        return on.test(`` + e)
          ? `javascript:throw new Error('React has blocked a javascript: URL as a security precaution.')`
          : e;
      }
      function cn() {}
      var ln = null;
      function un(e) {
        return (
          (e = e.target || e.srcElement || window),
          e.correspondingUseElement && (e = e.correspondingUseElement),
          e.nodeType === 3 ? e.parentNode : e
        );
      }
      var dn = null,
        fn = null;
      function pn(e) {
        var t = wt(e);
        if (t && (e = t.stateNode)) {
          var n = e[ht] || null;
          a: switch (((e = t.stateNode), t.type)) {
            case `input`:
              if (
                (qt(
                  e,
                  n.value,
                  n.defaultValue,
                  n.defaultValue,
                  n.checked,
                  n.defaultChecked,
                  n.type,
                  n.name,
                ),
                (t = n.name),
                n.type === `radio` && t != null)
              ) {
                for (n = e; n.parentNode; ) n = n.parentNode;
                for (
                  n = n.querySelectorAll(
                    `input[name="` + Kt(`` + t) + `"][type="radio"]`,
                  ),
                    t = 0;
                  t < n.length;
                  t++
                ) {
                  var r = n[t];
                  if (r !== e && r.form === e.form) {
                    var a = r[ht] || null;
                    if (!a) throw Error(i(90));
                    qt(
                      r,
                      a.value,
                      a.defaultValue,
                      a.defaultValue,
                      a.checked,
                      a.defaultChecked,
                      a.type,
                      a.name,
                    );
                  }
                }
                for (t = 0; t < n.length; t++)
                  ((r = n[t]), r.form === e.form && Ut(r));
              }
              break a;
            case `textarea`:
              Zt(e, n.value, n.defaultValue);
              break a;
            case `select`:
              ((t = n.value), t != null && Xt(e, !!n.multiple, t, !1));
          }
        }
      }
      var mn = !1;
      function hn(e, t, n) {
        if (mn) return e(t, n);
        mn = !0;
        try {
          return e(t);
        } finally {
          if (
            ((mn = !1),
            (dn !== null || fn !== null) &&
              (Su(), dn && ((t = dn), (e = fn), (fn = dn = null), pn(t), e)))
          )
            for (t = 0; t < e.length; t++) pn(e[t]);
        }
      }
      function gn(e, t) {
        var n = e.stateNode;
        if (n === null) return null;
        var r = n[ht] || null;
        if (r === null) return null;
        n = r[t];
        a: switch (t) {
          case `onClick`:
          case `onClickCapture`:
          case `onDoubleClick`:
          case `onDoubleClickCapture`:
          case `onMouseDown`:
          case `onMouseDownCapture`:
          case `onMouseMove`:
          case `onMouseMoveCapture`:
          case `onMouseUp`:
          case `onMouseUpCapture`:
          case `onMouseEnter`:
            ((r = !r.disabled) ||
              ((e = e.type),
              (r = !(
                e === `button` ||
                e === `input` ||
                e === `select` ||
                e === `textarea`
              ))),
              (e = !r));
            break a;
          default:
            e = !1;
        }
        if (e) return null;
        if (n && typeof n != `function`) throw Error(i(231, t, typeof n));
        return n;
      }
      var _n = !(
          typeof window > `u` ||
          window.document === void 0 ||
          window.document.createElement === void 0
        ),
        vn = !1;
      if (_n)
        try {
          var yn = {};
          (Object.defineProperty(yn, "passive", {
            get: function () {
              vn = !0;
            },
          }),
            window.addEventListener(`test`, yn, yn),
            window.removeEventListener(`test`, yn, yn));
        } catch {
          vn = !1;
        }
      var bn = null,
        xn = null,
        Sn = null;
      function Cn() {
        if (Sn) return Sn;
        var e,
          t = xn,
          n = t.length,
          r,
          i = `value` in bn ? bn.value : bn.textContent,
          a = i.length;
        for (e = 0; e < n && t[e] === i[e]; e++);
        var o = n - e;
        for (r = 1; r <= o && t[n - r] === i[a - r]; r++);
        return (Sn = i.slice(e, 1 < r ? 1 - r : void 0));
      }
      function wn(e) {
        var t = e.keyCode;
        return (
          `charCode` in e
            ? ((e = e.charCode), e === 0 && t === 13 && (e = 13))
            : (e = t),
          e === 10 && (e = 13),
          32 <= e || e === 13 ? e : 0
        );
      }
      function Tn() {
        return !0;
      }
      function En() {
        return !1;
      }
      function Dn(e) {
        function t(t, n, r, i, a) {
          for (var o in ((this._reactName = t),
          (this._targetInst = r),
          (this.type = n),
          (this.nativeEvent = i),
          (this.target = a),
          (this.currentTarget = null),
          e))
            e.hasOwnProperty(o) && ((t = e[o]), (this[o] = t ? t(i) : i[o]));
          return (
            (this.isDefaultPrevented = (
              i.defaultPrevented == null
                ? !1 === i.returnValue
                : i.defaultPrevented
            )
              ? Tn
              : En),
            (this.isPropagationStopped = En),
            this
          );
        }
        return (
          f(t.prototype, {
            preventDefault: function () {
              this.defaultPrevented = !0;
              var e = this.nativeEvent;
              e &&
                (e.preventDefault
                  ? e.preventDefault()
                  : typeof e.returnValue != `unknown` && (e.returnValue = !1),
                (this.isDefaultPrevented = Tn));
            },
            stopPropagation: function () {
              var e = this.nativeEvent;
              e &&
                (e.stopPropagation
                  ? e.stopPropagation()
                  : typeof e.cancelBubble != `unknown` && (e.cancelBubble = !0),
                (this.isPropagationStopped = Tn));
            },
            persist: function () {},
            isPersistent: Tn,
          }),
          t
        );
      }
      var On = {
          eventPhase: 0,
          bubbles: 0,
          cancelable: 0,
          timeStamp: function (e) {
            return e.timeStamp || Date.now();
          },
          defaultPrevented: 0,
          isTrusted: 0,
        },
        kn = Dn(On),
        An = f({}, On, { view: 0, detail: 0 }),
        jn = Dn(An),
        Mn,
        Nn,
        Pn,
        Fn = f({}, An, {
          screenX: 0,
          screenY: 0,
          clientX: 0,
          clientY: 0,
          pageX: 0,
          pageY: 0,
          ctrlKey: 0,
          shiftKey: 0,
          altKey: 0,
          metaKey: 0,
          getModifierState: Kn,
          button: 0,
          buttons: 0,
          relatedTarget: function (e) {
            return e.relatedTarget === void 0
              ? e.fromElement === e.srcElement
                ? e.toElement
                : e.fromElement
              : e.relatedTarget;
          },
          movementX: function (e) {
            return `movementX` in e
              ? e.movementX
              : (e !== Pn &&
                  (Pn && e.type === `mousemove`
                    ? ((Mn = e.screenX - Pn.screenX),
                      (Nn = e.screenY - Pn.screenY))
                    : (Nn = Mn = 0),
                  (Pn = e)),
                Mn);
          },
          movementY: function (e) {
            return `movementY` in e ? e.movementY : Nn;
          },
        }),
        In = Dn(Fn),
        Ln = Dn(f({}, Fn, { dataTransfer: 0 })),
        Rn = Dn(f({}, An, { relatedTarget: 0 })),
        zn = Dn(
          f({}, On, { animationName: 0, elapsedTime: 0, pseudoElement: 0 }),
        ),
        Bn = Dn(
          f({}, On, {
            clipboardData: function (e) {
              return `clipboardData` in e
                ? e.clipboardData
                : window.clipboardData;
            },
          }),
        ),
        Vn = Dn(f({}, On, { data: 0 })),
        Hn = {
          Esc: `Escape`,
          Spacebar: ` `,
          Left: `ArrowLeft`,
          Up: `ArrowUp`,
          Right: `ArrowRight`,
          Down: `ArrowDown`,
          Del: `Delete`,
          Win: `OS`,
          Menu: `ContextMenu`,
          Apps: `ContextMenu`,
          Scroll: `ScrollLock`,
          MozPrintableKey: `Unidentified`,
        },
        Un = {
          8: `Backspace`,
          9: `Tab`,
          12: `Clear`,
          13: `Enter`,
          16: `Shift`,
          17: `Control`,
          18: `Alt`,
          19: `Pause`,
          20: `CapsLock`,
          27: `Escape`,
          32: ` `,
          33: `PageUp`,
          34: `PageDown`,
          35: `End`,
          36: `Home`,
          37: `ArrowLeft`,
          38: `ArrowUp`,
          39: `ArrowRight`,
          40: `ArrowDown`,
          45: `Insert`,
          46: `Delete`,
          112: `F1`,
          113: `F2`,
          114: `F3`,
          115: `F4`,
          116: `F5`,
          117: `F6`,
          118: `F7`,
          119: `F8`,
          120: `F9`,
          121: `F10`,
          122: `F11`,
          123: `F12`,
          144: `NumLock`,
          145: `ScrollLock`,
          224: `Meta`,
        },
        Wn = {
          Alt: `altKey`,
          Control: `ctrlKey`,
          Meta: `metaKey`,
          Shift: `shiftKey`,
        };
      function Gn(e) {
        var t = this.nativeEvent;
        return t.getModifierState
          ? t.getModifierState(e)
          : (e = Wn[e])
            ? !!t[e]
            : !1;
      }
      function Kn() {
        return Gn;
      }
      var qn = Dn(
          f({}, An, {
            key: function (e) {
              if (e.key) {
                var t = Hn[e.key] || e.key;
                if (t !== `Unidentified`) return t;
              }
              return e.type === `keypress`
                ? ((e = wn(e)), e === 13 ? `Enter` : String.fromCharCode(e))
                : e.type === `keydown` || e.type === `keyup`
                  ? Un[e.keyCode] || `Unidentified`
                  : ``;
            },
            code: 0,
            location: 0,
            ctrlKey: 0,
            shiftKey: 0,
            altKey: 0,
            metaKey: 0,
            repeat: 0,
            locale: 0,
            getModifierState: Kn,
            charCode: function (e) {
              return e.type === `keypress` ? wn(e) : 0;
            },
            keyCode: function (e) {
              return e.type === `keydown` || e.type === `keyup` ? e.keyCode : 0;
            },
            which: function (e) {
              return e.type === `keypress`
                ? wn(e)
                : e.type === `keydown` || e.type === `keyup`
                  ? e.keyCode
                  : 0;
            },
          }),
        ),
        Jn = Dn(
          f({}, Fn, {
            pointerId: 0,
            width: 0,
            height: 0,
            pressure: 0,
            tangentialPressure: 0,
            tiltX: 0,
            tiltY: 0,
            twist: 0,
            pointerType: 0,
            isPrimary: 0,
          }),
        ),
        Yn = Dn(
          f({}, An, {
            touches: 0,
            targetTouches: 0,
            changedTouches: 0,
            altKey: 0,
            metaKey: 0,
            ctrlKey: 0,
            shiftKey: 0,
            getModifierState: Kn,
          }),
        ),
        Xn = Dn(
          f({}, On, { propertyName: 0, elapsedTime: 0, pseudoElement: 0 }),
        ),
        Zn = Dn(
          f({}, Fn, {
            deltaX: function (e) {
              return `deltaX` in e
                ? e.deltaX
                : `wheelDeltaX` in e
                  ? -e.wheelDeltaX
                  : 0;
            },
            deltaY: function (e) {
              return `deltaY` in e
                ? e.deltaY
                : `wheelDeltaY` in e
                  ? -e.wheelDeltaY
                  : `wheelDelta` in e
                    ? -e.wheelDelta
                    : 0;
            },
            deltaZ: 0,
            deltaMode: 0,
          }),
        ),
        Qn = Dn(f({}, On, { newState: 0, oldState: 0 })),
        $n = [9, 13, 27, 32],
        er = _n && `CompositionEvent` in window,
        tr = null;
      _n && `documentMode` in document && (tr = document.documentMode);
      var nr = _n && `TextEvent` in window && !tr,
        rr = _n && (!er || (tr && 8 < tr && 11 >= tr)),
        ir = ` `,
        ar = !1;
      function or(e, t) {
        switch (e) {
          case `keyup`:
            return $n.indexOf(t.keyCode) !== -1;
          case `keydown`:
            return t.keyCode !== 229;
          case `keypress`:
          case `mousedown`:
          case `focusout`:
            return !0;
          default:
            return !1;
        }
      }
      function sr(e) {
        return (
          (e = e.detail), typeof e == `object` && `data` in e ? e.data : null
        );
      }
      var cr = !1;
      function lr(e, t) {
        switch (e) {
          case `compositionend`:
            return sr(t);
          case `keypress`:
            return t.which === 32 ? ((ar = !0), ir) : null;
          case `textInput`:
            return ((e = t.data), e === ir && ar ? null : e);
          default:
            return null;
        }
      }
      function ur(e, t) {
        if (cr)
          return e === `compositionend` || (!er && or(e, t))
            ? ((e = Cn()), (Sn = xn = bn = null), (cr = !1), e)
            : null;
        switch (e) {
          case `paste`:
            return null;
          case `keypress`:
            if (
              !(t.ctrlKey || t.altKey || t.metaKey) ||
              (t.ctrlKey && t.altKey)
            ) {
              if (t.char && 1 < t.char.length) return t.char;
              if (t.which) return String.fromCharCode(t.which);
            }
            return null;
          case `compositionend`:
            return rr && t.locale !== `ko` ? null : t.data;
          default:
            return null;
        }
      }
      var dr = {
        color: !0,
        date: !0,
        datetime: !0,
        "datetime-local": !0,
        email: !0,
        month: !0,
        number: !0,
        password: !0,
        range: !0,
        search: !0,
        tel: !0,
        text: !0,
        time: !0,
        url: !0,
        week: !0,
      };
      function fr(e) {
        var t = e && e.nodeName && e.nodeName.toLowerCase();
        return t === `input` ? !!dr[e.type] : t === `textarea`;
      }
      function pr(e, t, n, r) {
        (dn ? (fn ? fn.push(r) : (fn = [r])) : (dn = r),
          (t = kd(t, `onChange`)),
          0 < t.length &&
            ((n = new kn(`onChange`, `change`, null, n, r)),
            e.push({ event: n, listeners: t })));
      }
      var mr = null,
        hr = null;
      function gr(e) {
        Sd(e, 0);
      }
      function _r(e) {
        if (Ut(Tt(e))) return e;
      }
      function vr(e, t) {
        if (e === `change`) return t;
      }
      var yr = !1;
      if (_n) {
        var br;
        if (_n) {
          var xr = `oninput` in document;
          if (!xr) {
            var Sr = document.createElement(`div`);
            (Sr.setAttribute(`oninput`, `return;`),
              (xr = typeof Sr.oninput == `function`));
          }
          br = xr;
        } else br = !1;
        yr = br && (!document.documentMode || 9 < document.documentMode);
      }
      function Cr() {
        mr && (mr.detachEvent(`onpropertychange`, wr), (hr = mr = null));
      }
      function wr(e) {
        if (e.propertyName === `value` && _r(hr)) {
          var t = [];
          (pr(t, hr, e, un(e)), hn(gr, t));
        }
      }
      function Tr(e, t, n) {
        e === `focusin`
          ? (Cr(), (mr = t), (hr = n), mr.attachEvent(`onpropertychange`, wr))
          : e === `focusout` && Cr();
      }
      function Er(e) {
        if (e === `selectionchange` || e === `keyup` || e === `keydown`)
          return _r(hr);
      }
      function Dr(e, t) {
        if (e === `click`) return _r(t);
      }
      function Or(e, t) {
        if (e === `input` || e === `change`) return _r(t);
      }
      function kr(e, t) {
        return (e === t && (e !== 0 || 1 / e == 1 / t)) || (e !== e && t !== t);
      }
      var Ar = typeof Object.is == `function` ? Object.is : kr;
      function jr(e, t) {
        if (Ar(e, t)) return !0;
        if (typeof e != `object` || !e || typeof t != `object` || !t) return !1;
        var n = Object.keys(e),
          r = Object.keys(t);
        if (n.length !== r.length) return !1;
        for (r = 0; r < n.length; r++) {
          var i = n[r];
          if (!Oe.call(t, i) || !Ar(e[i], t[i])) return !1;
        }
        return !0;
      }
      function Mr(e) {
        for (; e && e.firstChild; ) e = e.firstChild;
        return e;
      }
      function Nr(e, t) {
        var n = Mr(e);
        e = 0;
        for (var r; n; ) {
          if (n.nodeType === 3) {
            if (((r = e + n.textContent.length), e <= t && r >= t))
              return { node: n, offset: t - e };
            e = r;
          }
          a: {
            for (; n; ) {
              if (n.nextSibling) {
                n = n.nextSibling;
                break a;
              }
              n = n.parentNode;
            }
            n = void 0;
          }
          n = Mr(n);
        }
      }
      function Pr(e, t) {
        return e && t
          ? e === t
            ? !0
            : e && e.nodeType === 3
              ? !1
              : t && t.nodeType === 3
                ? Pr(e, t.parentNode)
                : `contains` in e
                  ? e.contains(t)
                  : e.compareDocumentPosition
                    ? !!(e.compareDocumentPosition(t) & 16)
                    : !1
          : !1;
      }
      function Fr(e) {
        e =
          e != null &&
          e.ownerDocument != null &&
          e.ownerDocument.defaultView != null
            ? e.ownerDocument.defaultView
            : window;
        for (var t = Wt(e.document); t instanceof e.HTMLIFrameElement; ) {
          try {
            var n = typeof t.contentWindow.location.href == `string`;
          } catch {
            n = !1;
          }
          if (n) e = t.contentWindow;
          else break;
          t = Wt(e.document);
        }
        return t;
      }
      function Ir(e) {
        var t = e && e.nodeName && e.nodeName.toLowerCase();
        return (
          t &&
          ((t === `input` &&
            (e.type === `text` ||
              e.type === `search` ||
              e.type === `tel` ||
              e.type === `url` ||
              e.type === `password`)) ||
            t === `textarea` ||
            e.contentEditable === `true`)
        );
      }
      var Lr = _n && `documentMode` in document && 11 >= document.documentMode,
        Rr = null,
        zr = null,
        Br = null,
        Vr = !1;
      function Hr(e, t, n) {
        var r =
          n.window === n ? n.document : n.nodeType === 9 ? n : n.ownerDocument;
        Vr ||
          Rr == null ||
          Rr !== Wt(r) ||
          ((r = Rr),
          `selectionStart` in r && Ir(r)
            ? (r = { start: r.selectionStart, end: r.selectionEnd })
            : ((r = (
                (r.ownerDocument && r.ownerDocument.defaultView) ||
                window
              ).getSelection()),
              (r = {
                anchorNode: r.anchorNode,
                anchorOffset: r.anchorOffset,
                focusNode: r.focusNode,
                focusOffset: r.focusOffset,
              })),
          (Br && jr(Br, r)) ||
            ((Br = r),
            (r = kd(zr, `onSelect`)),
            0 < r.length &&
              ((t = new kn(`onSelect`, `select`, null, t, n)),
              e.push({ event: t, listeners: r }),
              (t.target = Rr))));
      }
      function Ur(e, t) {
        var n = {};
        return (
          (n[e.toLowerCase()] = t.toLowerCase()),
          (n[`Webkit` + e] = `webkit` + t),
          (n[`Moz` + e] = `moz` + t),
          n
        );
      }
      var Wr = {
          animationend: Ur(`Animation`, `AnimationEnd`),
          animationiteration: Ur(`Animation`, `AnimationIteration`),
          animationstart: Ur(`Animation`, `AnimationStart`),
          transitionrun: Ur(`Transition`, `TransitionRun`),
          transitionstart: Ur(`Transition`, `TransitionStart`),
          transitioncancel: Ur(`Transition`, `TransitionCancel`),
          transitionend: Ur(`Transition`, `TransitionEnd`),
        },
        Gr = {},
        Kr = {};
      _n &&
        ((Kr = document.createElement(`div`).style),
        `AnimationEvent` in window ||
          (delete Wr.animationend.animation,
          delete Wr.animationiteration.animation,
          delete Wr.animationstart.animation),
        `TransitionEvent` in window || delete Wr.transitionend.transition);
      function qr(e) {
        if (Gr[e]) return Gr[e];
        if (!Wr[e]) return e;
        var t = Wr[e],
          n;
        for (n in t) if (t.hasOwnProperty(n) && n in Kr) return (Gr[e] = t[n]);
        return e;
      }
      var Jr = qr(`animationend`),
        Yr = qr(`animationiteration`),
        Xr = qr(`animationstart`),
        Zr = qr(`transitionrun`),
        Qr = qr(`transitionstart`),
        $r = qr(`transitioncancel`),
        ei = qr(`transitionend`),
        ti = new Map(),
        ni =
          `abort auxClick beforeToggle cancel canPlay canPlayThrough click close contextMenu copy cut drag dragEnd dragEnter dragExit dragLeave dragOver dragStart drop durationChange emptied encrypted ended error gotPointerCapture input invalid keyDown keyPress keyUp load loadedData loadedMetadata loadStart lostPointerCapture mouseDown mouseMove mouseOut mouseOver mouseUp paste pause play playing pointerCancel pointerDown pointerMove pointerOut pointerOver pointerUp progress rateChange reset resize seeked seeking stalled submit suspend timeUpdate touchCancel touchEnd touchStart volumeChange scroll toggle touchMove waiting wheel`.split(
            ` `,
          );
      ni.push(`scrollEnd`);
      function ri(e, t) {
        (ti.set(e, t), At(t, [e]));
      }
      var ii =
          typeof reportError == `function`
            ? reportError
            : function (e) {
                if (
                  typeof window == `object` &&
                  typeof window.ErrorEvent == `function`
                ) {
                  var t = new window.ErrorEvent(`error`, {
                    bubbles: !0,
                    cancelable: !0,
                    message:
                      typeof e == `object` && e && typeof e.message == `string`
                        ? String(e.message)
                        : String(e),
                    error: e,
                  });
                  if (!window.dispatchEvent(t)) return;
                } else if (
                  typeof process == `object` &&
                  typeof process.emit == `function`
                ) {
                  process.emit(`uncaughtException`, e);
                  return;
                }
                console.error(e);
              },
        ai = [],
        oi = 0,
        si = 0;
      function ci() {
        for (var e = oi, t = (si = oi = 0); t < e; ) {
          var n = ai[t];
          ai[t++] = null;
          var r = ai[t];
          ai[t++] = null;
          var i = ai[t];
          ai[t++] = null;
          var a = ai[t];
          if (((ai[t++] = null), r !== null && i !== null)) {
            var o = r.pending;
            (o === null ? (i.next = i) : ((i.next = o.next), (o.next = i)),
              (r.pending = i));
          }
          a !== 0 && fi(n, i, a);
        }
      }
      function li(e, t, n, r) {
        ((ai[oi++] = e),
          (ai[oi++] = t),
          (ai[oi++] = n),
          (ai[oi++] = r),
          (si |= r),
          (e.lanes |= r),
          (e = e.alternate),
          e !== null && (e.lanes |= r));
      }
      function ui(e, t, n, r) {
        return (li(e, t, n, r), pi(e));
      }
      function di(e, t) {
        return (li(e, null, null, t), pi(e));
      }
      function fi(e, t, n) {
        e.lanes |= n;
        var r = e.alternate;
        r !== null && (r.lanes |= n);
        for (var i = !1, a = e.return; a !== null; )
          ((a.childLanes |= n),
            (r = a.alternate),
            r !== null && (r.childLanes |= n),
            a.tag === 22 &&
              ((e = a.stateNode), e === null || e._visibility & 1 || (i = !0)),
            (e = a),
            (a = a.return));
        return e.tag === 3
          ? ((a = e.stateNode),
            i &&
              t !== null &&
              ((i = 31 - Ge(n)),
              (e = a.hiddenUpdates),
              (r = e[i]),
              r === null ? (e[i] = [t]) : r.push(t),
              (t.lane = n | 536870912)),
            a)
          : null;
      }
      function pi(e) {
        if (50 < pu) throw ((pu = 0), (mu = null), Error(i(185)));
        for (var t = e.return; t !== null; ) ((e = t), (t = e.return));
        return e.tag === 3 ? e.stateNode : null;
      }
      var mi = {};
      function hi(e, t, n, r) {
        ((this.tag = e),
          (this.key = n),
          (this.sibling =
            this.child =
            this.return =
            this.stateNode =
            this.type =
            this.elementType =
              null),
          (this.index = 0),
          (this.refCleanup = this.ref = null),
          (this.pendingProps = t),
          (this.dependencies =
            this.memoizedState =
            this.updateQueue =
            this.memoizedProps =
              null),
          (this.mode = r),
          (this.subtreeFlags = this.flags = 0),
          (this.deletions = null),
          (this.childLanes = this.lanes = 0),
          (this.alternate = null));
      }
      function gi(e, t, n, r) {
        return new hi(e, t, n, r);
      }
      function _i(e) {
        return ((e = e.prototype), !(!e || !e.isReactComponent));
      }
      function vi(e, t) {
        var n = e.alternate;
        return (
          n === null
            ? ((n = gi(e.tag, t, e.key, e.mode)),
              (n.elementType = e.elementType),
              (n.type = e.type),
              (n.stateNode = e.stateNode),
              (n.alternate = e),
              (e.alternate = n))
            : ((n.pendingProps = t),
              (n.type = e.type),
              (n.flags = 0),
              (n.subtreeFlags = 0),
              (n.deletions = null)),
          (n.flags = e.flags & 65011712),
          (n.childLanes = e.childLanes),
          (n.lanes = e.lanes),
          (n.child = e.child),
          (n.memoizedProps = e.memoizedProps),
          (n.memoizedState = e.memoizedState),
          (n.updateQueue = e.updateQueue),
          (t = e.dependencies),
          (n.dependencies =
            t === null
              ? null
              : { lanes: t.lanes, firstContext: t.firstContext }),
          (n.sibling = e.sibling),
          (n.index = e.index),
          (n.ref = e.ref),
          (n.refCleanup = e.refCleanup),
          n
        );
      }
      function yi(e, t) {
        e.flags &= 65011714;
        var n = e.alternate;
        return (
          n === null
            ? ((e.childLanes = 0),
              (e.lanes = t),
              (e.child = null),
              (e.subtreeFlags = 0),
              (e.memoizedProps = null),
              (e.memoizedState = null),
              (e.updateQueue = null),
              (e.dependencies = null),
              (e.stateNode = null))
            : ((e.childLanes = n.childLanes),
              (e.lanes = n.lanes),
              (e.child = n.child),
              (e.subtreeFlags = 0),
              (e.deletions = null),
              (e.memoizedProps = n.memoizedProps),
              (e.memoizedState = n.memoizedState),
              (e.updateQueue = n.updateQueue),
              (e.type = n.type),
              (t = n.dependencies),
              (e.dependencies =
                t === null
                  ? null
                  : { lanes: t.lanes, firstContext: t.firstContext })),
          e
        );
      }
      function bi(e, t, n, r, a, o) {
        var s = 0;
        if (((r = e), typeof e == `function`)) _i(e) && (s = 1);
        else if (typeof e == `string`)
          s = qf(e, n, me.current)
            ? 26
            : e === `html` || e === `head` || e === `body`
              ? 27
              : 5;
        else
          a: switch (e) {
            case ie:
              return (
                (e = gi(31, n, t, a)), (e.elementType = ie), (e.lanes = o), e
              );
            case y:
              return xi(n.children, a, o, t);
            case b:
              ((s = 8), (a |= 24));
              break;
            case x:
              return (
                (e = gi(12, n, t, a | 2)), (e.elementType = x), (e.lanes = o), e
              );
            case w:
              return (
                (e = gi(13, n, t, a)), (e.elementType = w), (e.lanes = o), e
              );
            case te:
              return (
                (e = gi(19, n, t, a)), (e.elementType = te), (e.lanes = o), e
              );
            default:
              if (typeof e == `object` && e)
                switch (e.$$typeof) {
                  case S:
                    s = 10;
                    break a;
                  case ee:
                    s = 9;
                    break a;
                  case C:
                    s = 11;
                    break a;
                  case ne:
                    s = 14;
                    break a;
                  case re:
                    ((s = 16), (r = null));
                    break a;
                }
              ((s = 29),
                (n = Error(i(130, e === null ? `null` : typeof e, ``))),
                (r = null));
          }
        return (
          (t = gi(s, n, t, a)),
          (t.elementType = e),
          (t.type = r),
          (t.lanes = o),
          t
        );
      }
      function xi(e, t, n, r) {
        return ((e = gi(7, e, r, t)), (e.lanes = n), e);
      }
      function Si(e, t, n) {
        return ((e = gi(6, e, null, t)), (e.lanes = n), e);
      }
      function Ci(e) {
        var t = gi(18, null, null, 0);
        return ((t.stateNode = e), t);
      }
      function wi(e, t, n) {
        return (
          (t = gi(4, e.children === null ? [] : e.children, e.key, t)),
          (t.lanes = n),
          (t.stateNode = {
            containerInfo: e.containerInfo,
            pendingChildren: null,
            implementation: e.implementation,
          }),
          t
        );
      }
      var Ti = new WeakMap();
      function Ei(e, t) {
        if (typeof e == `object` && e) {
          var n = Ti.get(e);
          return n === void 0
            ? ((t = { value: e, source: t, stack: De(t) }), Ti.set(e, t), t)
            : n;
        }
        return { value: e, source: t, stack: De(t) };
      }
      var Di = [],
        Oi = 0,
        ki = null,
        Ai = 0,
        ji = [],
        Mi = 0,
        Ni = null,
        Pi = 1,
        Fi = ``;
      function Ii(e, t) {
        ((Di[Oi++] = Ai), (Di[Oi++] = ki), (ki = e), (Ai = t));
      }
      function Li(e, t, n) {
        ((ji[Mi++] = Pi), (ji[Mi++] = Fi), (ji[Mi++] = Ni), (Ni = e));
        var r = Pi;
        e = Fi;
        var i = 32 - Ge(r) - 1;
        ((r &= ~(1 << i)), (n += 1));
        var a = 32 - Ge(t) + i;
        if (30 < a) {
          var o = i - (i % 5);
          ((a = (r & ((1 << o) - 1)).toString(32)),
            (r >>= o),
            (i -= o),
            (Pi = (1 << (32 - Ge(t) + i)) | (n << i) | r),
            (Fi = a + e));
        } else ((Pi = (1 << a) | (n << i) | r), (Fi = e));
      }
      function Ri(e) {
        e.return !== null && (Ii(e, 1), Li(e, 1, 0));
      }
      function zi(e) {
        for (; e === ki; )
          ((ki = Di[--Oi]), (Di[Oi] = null), (Ai = Di[--Oi]), (Di[Oi] = null));
        for (; e === Ni; )
          ((Ni = ji[--Mi]),
            (ji[Mi] = null),
            (Fi = ji[--Mi]),
            (ji[Mi] = null),
            (Pi = ji[--Mi]),
            (ji[Mi] = null));
      }
      function Bi(e, t) {
        ((ji[Mi++] = Pi),
          (ji[Mi++] = Fi),
          (ji[Mi++] = Ni),
          (Pi = t.id),
          (Fi = t.overflow),
          (Ni = e));
      }
      var Vi = null,
        Hi = null,
        j = !1,
        Ui = null,
        Wi = !1,
        Gi = Error(i(519));
      function Ki(e) {
        throw (
          Qi(
            Ei(
              Error(
                i(
                  418,
                  1 < arguments.length &&
                    arguments[1] !== void 0 &&
                    arguments[1]
                    ? `text`
                    : `HTML`,
                  ``,
                ),
              ),
              e,
            ),
          ),
          Gi
        );
      }
      function qi(e) {
        var t = e.stateNode,
          n = e.type,
          r = e.memoizedProps;
        switch (((t[mt] = e), (t[ht] = r), n)) {
          case `dialog`:
            (Y(`cancel`, t), Y(`close`, t));
            break;
          case `iframe`:
          case `object`:
          case `embed`:
            Y(`load`, t);
            break;
          case `video`:
          case `audio`:
            for (n = 0; n < bd.length; n++) Y(bd[n], t);
            break;
          case `source`:
            Y(`error`, t);
            break;
          case `img`:
          case `image`:
          case `link`:
            (Y(`error`, t), Y(`load`, t));
            break;
          case `details`:
            Y(`toggle`, t);
            break;
          case `input`:
            (Y(`invalid`, t),
              Jt(
                t,
                r.value,
                r.defaultValue,
                r.checked,
                r.defaultChecked,
                r.type,
                r.name,
                !0,
              ));
            break;
          case `select`:
            Y(`invalid`, t);
            break;
          case `textarea`:
            (Y(`invalid`, t), Qt(t, r.value, r.defaultValue, r.children));
        }
        ((n = r.children),
          (typeof n != `string` &&
            typeof n != `number` &&
            typeof n != `bigint`) ||
          t.textContent === `` + n ||
          !0 === r.suppressHydrationWarning ||
          Fd(t.textContent, n)
            ? (r.popover != null && (Y(`beforetoggle`, t), Y(`toggle`, t)),
              r.onScroll != null && Y(`scroll`, t),
              r.onScrollEnd != null && Y(`scrollend`, t),
              r.onClick != null && (t.onclick = cn),
              (t = !0))
            : (t = !1),
          t || Ki(e, !0));
      }
      function Ji(e) {
        for (Vi = e.return; Vi; )
          switch (Vi.tag) {
            case 5:
            case 31:
            case 13:
              Wi = !1;
              return;
            case 27:
            case 3:
              Wi = !0;
              return;
            default:
              Vi = Vi.return;
          }
      }
      function Yi(e) {
        if (e !== Vi) return !1;
        if (!j) return (Ji(e), (j = !0), !1);
        var t = e.tag,
          n;
        if (
          ((n = t !== 3 && t !== 27) &&
            ((n = t === 5) &&
              ((n = e.type),
              (n =
                !(n !== `form` && n !== `button`) ||
                qd(e.type, e.memoizedProps))),
            (n = !n)),
          n && Hi && Ki(e),
          Ji(e),
          t === 13)
        ) {
          if (
            ((e = e.memoizedState), (e = e === null ? null : e.dehydrated), !e)
          )
            throw Error(i(317));
          Hi = mf(e);
        } else if (t === 31) {
          if (
            ((e = e.memoizedState), (e = e === null ? null : e.dehydrated), !e)
          )
            throw Error(i(317));
          Hi = mf(e);
        } else
          t === 27
            ? ((t = Hi),
              tf(e.type) ? ((e = pf), (pf = null), (Hi = e)) : (Hi = t))
            : (Hi = Vi ? ff(e.stateNode.nextSibling) : null);
        return !0;
      }
      function Xi() {
        ((Hi = Vi = null), (j = !1));
      }
      function Zi() {
        var e = Ui;
        return (
          e !== null &&
            ($l === null ? ($l = e) : $l.push.apply($l, e), (Ui = null)),
          e
        );
      }
      function Qi(e) {
        Ui === null ? (Ui = [e]) : Ui.push(e);
      }
      var $i = pe(null),
        ea = null,
        ta = null;
      function na(e, t, n) {
        (k($i, t._currentValue), (t._currentValue = n));
      }
      function ra(e) {
        ((e._currentValue = $i.current), O($i));
      }
      function ia(e, t, n) {
        for (; e !== null; ) {
          var r = e.alternate;
          if (
            ((e.childLanes & t) === t
              ? r !== null && (r.childLanes & t) !== t && (r.childLanes |= t)
              : ((e.childLanes |= t), r !== null && (r.childLanes |= t)),
            e === n)
          )
            break;
          e = e.return;
        }
      }
      function aa(e, t, n, r) {
        var a = e.child;
        for (a !== null && (a.return = e); a !== null; ) {
          var o = a.dependencies;
          if (o !== null) {
            var s = a.child;
            o = o.firstContext;
            a: for (; o !== null; ) {
              var c = o;
              o = a;
              for (var l = 0; l < t.length; l++)
                if (c.context === t[l]) {
                  ((o.lanes |= n),
                    (c = o.alternate),
                    c !== null && (c.lanes |= n),
                    ia(o.return, n, e),
                    r || (s = null));
                  break a;
                }
              o = c.next;
            }
          } else if (a.tag === 18) {
            if (((s = a.return), s === null)) throw Error(i(341));
            ((s.lanes |= n),
              (o = s.alternate),
              o !== null && (o.lanes |= n),
              ia(s, n, e),
              (s = null));
          } else s = a.child;
          if (s !== null) s.return = a;
          else
            for (s = a; s !== null; ) {
              if (s === e) {
                s = null;
                break;
              }
              if (((a = s.sibling), a !== null)) {
                ((a.return = s.return), (s = a));
                break;
              }
              s = s.return;
            }
          a = s;
        }
      }
      function oa(e, t, n, r) {
        e = null;
        for (var a = t, o = !1; a !== null; ) {
          if (!o) {
            if (a.flags & 524288) o = !0;
            else if (a.flags & 262144) break;
          }
          if (a.tag === 10) {
            var s = a.alternate;
            if (s === null) throw Error(i(387));
            if (((s = s.memoizedProps), s !== null)) {
              var c = a.type;
              Ar(a.pendingProps.value, s.value) ||
                (e === null ? (e = [c]) : e.push(c));
            }
          } else if (a === _e.current) {
            if (((s = a.alternate), s === null)) throw Error(i(387));
            s.memoizedState.memoizedState !== a.memoizedState.memoizedState &&
              (e === null ? (e = [np]) : e.push(np));
          }
          a = a.return;
        }
        (e !== null && aa(t, e, n, r), (t.flags |= 262144));
      }
      function sa(e) {
        for (e = e.firstContext; e !== null; ) {
          if (!Ar(e.context._currentValue, e.memoizedValue)) return !0;
          e = e.next;
        }
        return !1;
      }
      function ca(e) {
        ((ea = e),
          (ta = null),
          (e = e.dependencies),
          e !== null && (e.firstContext = null));
      }
      function la(e) {
        return da(ea, e);
      }
      function ua(e, t) {
        return (ea === null && ca(e), da(e, t));
      }
      function da(e, t) {
        var n = t._currentValue;
        if (((t = { context: t, memoizedValue: n, next: null }), ta === null)) {
          if (e === null) throw Error(i(308));
          ((ta = t),
            (e.dependencies = { lanes: 0, firstContext: t }),
            (e.flags |= 524288));
        } else ta = ta.next = t;
        return n;
      }
      var fa =
          typeof AbortController < `u`
            ? AbortController
            : function () {
                var e = [],
                  t = (this.signal = {
                    aborted: !1,
                    addEventListener: function (t, n) {
                      e.push(n);
                    },
                  });
                this.abort = function () {
                  ((t.aborted = !0),
                    e.forEach(function (e) {
                      return e();
                    }));
                };
              },
        pa = t.unstable_scheduleCallback,
        ma = t.unstable_NormalPriority,
        ha = {
          $$typeof: S,
          Consumer: null,
          Provider: null,
          _currentValue: null,
          _currentValue2: null,
          _threadCount: 0,
        };
      function ga() {
        return { controller: new fa(), data: new Map(), refCount: 0 };
      }
      function _a(e) {
        (e.refCount--,
          e.refCount === 0 &&
            pa(ma, function () {
              e.controller.abort();
            }));
      }
      var va = null,
        ya = 0,
        ba = 0,
        xa = null;
      function Sa(e, t) {
        if (va === null) {
          var n = (va = []);
          ((ya = 0),
            (ba = md()),
            (xa = {
              status: `pending`,
              value: void 0,
              then: function (e) {
                n.push(e);
              },
            }));
        }
        return (ya++, t.then(Ca, Ca), t);
      }
      function Ca() {
        if (--ya === 0 && va !== null) {
          xa !== null && (xa.status = `fulfilled`);
          var e = va;
          ((va = null), (ba = 0), (xa = null));
          for (var t = 0; t < e.length; t++) (0, e[t])();
        }
      }
      function wa(e, t) {
        var n = [],
          r = {
            status: `pending`,
            value: null,
            reason: null,
            then: function (e) {
              n.push(e);
            },
          };
        return (
          e.then(
            function () {
              ((r.status = `fulfilled`), (r.value = t));
              for (var e = 0; e < n.length; e++) (0, n[e])(t);
            },
            function (e) {
              for (
                r.status = `rejected`, r.reason = e, e = 0;
                e < n.length;
                e++
              )
                (0, n[e])(void 0);
            },
          ),
          r
        );
      }
      var Ta = E.S;
      E.S = function (e, t) {
        ((nu = Ne()),
          typeof t == `object` && t && typeof t.then == `function` && Sa(e, t),
          Ta !== null && Ta(e, t));
      };
      var M = pe(null);
      function Ea() {
        var e = M.current;
        return e === null ? zl.pooledCache : e;
      }
      function Da(e, t) {
        t === null ? k(M, M.current) : k(M, t.pool);
      }
      function N() {
        var e = Ea();
        return e === null ? null : { parent: ha._currentValue, pool: e };
      }
      var Oa = Error(i(460)),
        ka = Error(i(474)),
        Aa = Error(i(542)),
        ja = { then: function () {} };
      function Ma(e) {
        return ((e = e.status), e === `fulfilled` || e === `rejected`);
      }
      function Na(e, t, n) {
        switch (
          ((n = e[n]),
          n === void 0 ? e.push(t) : n !== t && (t.then(cn, cn), (t = n)),
          t.status)
        ) {
          case `fulfilled`:
            return t.value;
          case `rejected`:
            throw ((e = t.reason), La(e), e);
          default:
            if (typeof t.status == `string`) t.then(cn, cn);
            else {
              if (((e = zl), e !== null && 100 < e.shellSuspendCounter))
                throw Error(i(482));
              ((e = t),
                (e.status = `pending`),
                e.then(
                  function (e) {
                    if (t.status === `pending`) {
                      var n = t;
                      ((n.status = `fulfilled`), (n.value = e));
                    }
                  },
                  function (e) {
                    if (t.status === `pending`) {
                      var n = t;
                      ((n.status = `rejected`), (n.reason = e));
                    }
                  },
                ));
            }
            switch (t.status) {
              case `fulfilled`:
                return t.value;
              case `rejected`:
                throw ((e = t.reason), La(e), e);
            }
            throw ((Fa = t), Oa);
        }
      }
      function Pa(e) {
        try {
          var t = e._init;
          return t(e._payload);
        } catch (e) {
          throw typeof e == `object` && e && typeof e.then == `function`
            ? ((Fa = e), Oa)
            : e;
        }
      }
      var Fa = null;
      function Ia() {
        if (Fa === null) throw Error(i(459));
        var e = Fa;
        return ((Fa = null), e);
      }
      function La(e) {
        if (e === Oa || e === Aa) throw Error(i(483));
      }
      var Ra = null,
        za = 0;
      function Ba(e) {
        var t = za;
        return ((za += 1), Ra === null && (Ra = []), Na(Ra, e, t));
      }
      function Va(e, t) {
        ((t = t.props.ref), (e.ref = t === void 0 ? null : t));
      }
      function Ha(e, t) {
        throw t.$$typeof === p
          ? Error(i(525))
          : ((e = Object.prototype.toString.call(t)),
            Error(
              i(
                31,
                e === `[object Object]`
                  ? `object with keys {` + Object.keys(t).join(`, `) + `}`
                  : e,
              ),
            ));
      }
      function Ua(e) {
        function t(t, n) {
          if (e) {
            var r = t.deletions;
            r === null ? ((t.deletions = [n]), (t.flags |= 16)) : r.push(n);
          }
        }
        function n(n, r) {
          if (!e) return null;
          for (; r !== null; ) (t(n, r), (r = r.sibling));
          return null;
        }
        function r(e) {
          for (var t = new Map(); e !== null; )
            (e.key === null ? t.set(e.index, e) : t.set(e.key, e),
              (e = e.sibling));
          return t;
        }
        function a(e, t) {
          return ((e = vi(e, t)), (e.index = 0), (e.sibling = null), e);
        }
        function o(t, n, r) {
          return (
            (t.index = r),
            e
              ? ((r = t.alternate),
                r === null
                  ? ((t.flags |= 67108866), n)
                  : ((r = r.index), r < n ? ((t.flags |= 67108866), n) : r))
              : ((t.flags |= 1048576), n)
          );
        }
        function s(t) {
          return (e && t.alternate === null && (t.flags |= 67108866), t);
        }
        function c(e, t, n, r) {
          return t === null || t.tag !== 6
            ? ((t = Si(n, e.mode, r)), (t.return = e), t)
            : ((t = a(t, n)), (t.return = e), t);
        }
        function l(e, t, n, r) {
          var i = n.type;
          return i === y
            ? d(e, t, n.props.children, r, n.key)
            : t !== null &&
                (t.elementType === i ||
                  (typeof i == `object` &&
                    i &&
                    i.$$typeof === re &&
                    Pa(i) === t.type))
              ? ((t = a(t, n.props)), Va(t, n), (t.return = e), t)
              : ((t = bi(n.type, n.key, n.props, null, e.mode, r)),
                Va(t, n),
                (t.return = e),
                t);
        }
        function u(e, t, n, r) {
          return t === null ||
            t.tag !== 4 ||
            t.stateNode.containerInfo !== n.containerInfo ||
            t.stateNode.implementation !== n.implementation
            ? ((t = wi(n, e.mode, r)), (t.return = e), t)
            : ((t = a(t, n.children || [])), (t.return = e), t);
        }
        function d(e, t, n, r, i) {
          return t === null || t.tag !== 7
            ? ((t = xi(n, e.mode, r, i)), (t.return = e), t)
            : ((t = a(t, n)), (t.return = e), t);
        }
        function f(e, t, n) {
          if (
            (typeof t == `string` && t !== ``) ||
            typeof t == `number` ||
            typeof t == `bigint`
          )
            return ((t = Si(`` + t, e.mode, n)), (t.return = e), t);
          if (typeof t == `object` && t) {
            switch (t.$$typeof) {
              case h:
                return (
                  (n = bi(t.type, t.key, t.props, null, e.mode, n)),
                  Va(n, t),
                  (n.return = e),
                  n
                );
              case _:
                return ((t = wi(t, e.mode, n)), (t.return = e), t);
              case re:
                return ((t = Pa(t)), f(e, t, n));
            }
            if (T(t) || se(t))
              return ((t = xi(t, e.mode, n, null)), (t.return = e), t);
            if (typeof t.then == `function`) return f(e, Ba(t), n);
            if (t.$$typeof === S) return f(e, ua(e, t), n);
            Ha(e, t);
          }
          return null;
        }
        function p(e, t, n, r) {
          var i = t === null ? null : t.key;
          if (
            (typeof n == `string` && n !== ``) ||
            typeof n == `number` ||
            typeof n == `bigint`
          )
            return i === null ? c(e, t, `` + n, r) : null;
          if (typeof n == `object` && n) {
            switch (n.$$typeof) {
              case h:
                return n.key === i ? l(e, t, n, r) : null;
              case _:
                return n.key === i ? u(e, t, n, r) : null;
              case re:
                return ((n = Pa(n)), p(e, t, n, r));
            }
            if (T(n) || se(n)) return i === null ? d(e, t, n, r, null) : null;
            if (typeof n.then == `function`) return p(e, t, Ba(n), r);
            if (n.$$typeof === S) return p(e, t, ua(e, n), r);
            Ha(e, n);
          }
          return null;
        }
        function m(e, t, n, r, i) {
          if (
            (typeof r == `string` && r !== ``) ||
            typeof r == `number` ||
            typeof r == `bigint`
          )
            return ((e = e.get(n) || null), c(t, e, `` + r, i));
          if (typeof r == `object` && r) {
            switch (r.$$typeof) {
              case h:
                return (
                  (e = e.get(r.key === null ? n : r.key) || null), l(t, e, r, i)
                );
              case _:
                return (
                  (e = e.get(r.key === null ? n : r.key) || null), u(t, e, r, i)
                );
              case re:
                return ((r = Pa(r)), m(e, t, n, r, i));
            }
            if (T(r) || se(r))
              return ((e = e.get(n) || null), d(t, e, r, i, null));
            if (typeof r.then == `function`) return m(e, t, n, Ba(r), i);
            if (r.$$typeof === S) return m(e, t, n, ua(t, r), i);
            Ha(t, r);
          }
          return null;
        }
        function g(i, a, s, c) {
          for (
            var l = null, u = null, d = a, h = (a = 0), g = null;
            d !== null && h < s.length;
            h++
          ) {
            d.index > h ? ((g = d), (d = null)) : (g = d.sibling);
            var _ = p(i, d, s[h], c);
            if (_ === null) {
              d === null && (d = g);
              break;
            }
            (e && d && _.alternate === null && t(i, d),
              (a = o(_, a, h)),
              u === null ? (l = _) : (u.sibling = _),
              (u = _),
              (d = g));
          }
          if (h === s.length) return (n(i, d), j && Ii(i, h), l);
          if (d === null) {
            for (; h < s.length; h++)
              ((d = f(i, s[h], c)),
                d !== null &&
                  ((a = o(d, a, h)),
                  u === null ? (l = d) : (u.sibling = d),
                  (u = d)));
            return (j && Ii(i, h), l);
          }
          for (d = r(d); h < s.length; h++)
            ((g = m(d, i, h, s[h], c)),
              g !== null &&
                (e &&
                  g.alternate !== null &&
                  d.delete(g.key === null ? h : g.key),
                (a = o(g, a, h)),
                u === null ? (l = g) : (u.sibling = g),
                (u = g)));
          return (
            e &&
              d.forEach(function (e) {
                return t(i, e);
              }),
            j && Ii(i, h),
            l
          );
        }
        function v(a, s, c, l) {
          if (c == null) throw Error(i(151));
          for (
            var u = null, d = null, h = s, g = (s = 0), _ = null, v = c.next();
            h !== null && !v.done;
            g++, v = c.next()
          ) {
            h.index > g ? ((_ = h), (h = null)) : (_ = h.sibling);
            var y = p(a, h, v.value, l);
            if (y === null) {
              h === null && (h = _);
              break;
            }
            (e && h && y.alternate === null && t(a, h),
              (s = o(y, s, g)),
              d === null ? (u = y) : (d.sibling = y),
              (d = y),
              (h = _));
          }
          if (v.done) return (n(a, h), j && Ii(a, g), u);
          if (h === null) {
            for (; !v.done; g++, v = c.next())
              ((v = f(a, v.value, l)),
                v !== null &&
                  ((s = o(v, s, g)),
                  d === null ? (u = v) : (d.sibling = v),
                  (d = v)));
            return (j && Ii(a, g), u);
          }
          for (h = r(h); !v.done; g++, v = c.next())
            ((v = m(h, a, g, v.value, l)),
              v !== null &&
                (e &&
                  v.alternate !== null &&
                  h.delete(v.key === null ? g : v.key),
                (s = o(v, s, g)),
                d === null ? (u = v) : (d.sibling = v),
                (d = v)));
          return (
            e &&
              h.forEach(function (e) {
                return t(a, e);
              }),
            j && Ii(a, g),
            u
          );
        }
        function b(e, r, o, c) {
          if (
            (typeof o == `object` &&
              o &&
              o.type === y &&
              o.key === null &&
              (o = o.props.children),
            typeof o == `object` && o)
          ) {
            switch (o.$$typeof) {
              case h:
                a: {
                  for (var l = o.key; r !== null; ) {
                    if (r.key === l) {
                      if (((l = o.type), l === y)) {
                        if (r.tag === 7) {
                          (n(e, r.sibling),
                            (c = a(r, o.props.children)),
                            (c.return = e),
                            (e = c));
                          break a;
                        }
                      } else if (
                        r.elementType === l ||
                        (typeof l == `object` &&
                          l &&
                          l.$$typeof === re &&
                          Pa(l) === r.type)
                      ) {
                        (n(e, r.sibling),
                          (c = a(r, o.props)),
                          Va(c, o),
                          (c.return = e),
                          (e = c));
                        break a;
                      }
                      n(e, r);
                      break;
                    } else t(e, r);
                    r = r.sibling;
                  }
                  o.type === y
                    ? ((c = xi(o.props.children, e.mode, c, o.key)),
                      (c.return = e),
                      (e = c))
                    : ((c = bi(o.type, o.key, o.props, null, e.mode, c)),
                      Va(c, o),
                      (c.return = e),
                      (e = c));
                }
                return s(e);
              case _:
                a: {
                  for (l = o.key; r !== null; ) {
                    if (r.key === l)
                      if (
                        r.tag === 4 &&
                        r.stateNode.containerInfo === o.containerInfo &&
                        r.stateNode.implementation === o.implementation
                      ) {
                        (n(e, r.sibling),
                          (c = a(r, o.children || [])),
                          (c.return = e),
                          (e = c));
                        break a;
                      } else {
                        n(e, r);
                        break;
                      }
                    else t(e, r);
                    r = r.sibling;
                  }
                  ((c = wi(o, e.mode, c)), (c.return = e), (e = c));
                }
                return s(e);
              case re:
                return ((o = Pa(o)), b(e, r, o, c));
            }
            if (T(o)) return g(e, r, o, c);
            if (se(o)) {
              if (((l = se(o)), typeof l != `function`)) throw Error(i(150));
              return ((o = l.call(o)), v(e, r, o, c));
            }
            if (typeof o.then == `function`) return b(e, r, Ba(o), c);
            if (o.$$typeof === S) return b(e, r, ua(e, o), c);
            Ha(e, o);
          }
          return (typeof o == `string` && o !== ``) ||
            typeof o == `number` ||
            typeof o == `bigint`
            ? ((o = `` + o),
              r !== null && r.tag === 6
                ? (n(e, r.sibling), (c = a(r, o)), (c.return = e), (e = c))
                : (n(e, r), (c = Si(o, e.mode, c)), (c.return = e), (e = c)),
              s(e))
            : n(e, r);
        }
        return function (e, t, n, r) {
          try {
            za = 0;
            var i = b(e, t, n, r);
            return ((Ra = null), i);
          } catch (t) {
            if (t === Oa || t === Aa) throw t;
            var a = gi(29, t, null, e.mode);
            return ((a.lanes = r), (a.return = e), a);
          }
        };
      }
      var Wa = Ua(!0),
        Ga = Ua(!1),
        Ka = !1;
      function qa(e) {
        e.updateQueue = {
          baseState: e.memoizedState,
          firstBaseUpdate: null,
          lastBaseUpdate: null,
          shared: { pending: null, lanes: 0, hiddenCallbacks: null },
          callbacks: null,
        };
      }
      function Ja(e, t) {
        ((e = e.updateQueue),
          t.updateQueue === e &&
            (t.updateQueue = {
              baseState: e.baseState,
              firstBaseUpdate: e.firstBaseUpdate,
              lastBaseUpdate: e.lastBaseUpdate,
              shared: e.shared,
              callbacks: null,
            }));
      }
      function Ya(e) {
        return { lane: e, tag: 0, payload: null, callback: null, next: null };
      }
      function P(e, t, n) {
        var r = e.updateQueue;
        if (r === null) return null;
        if (((r = r.shared), K & 2)) {
          var i = r.pending;
          return (
            i === null ? (t.next = t) : ((t.next = i.next), (i.next = t)),
            (r.pending = t),
            (t = pi(e)),
            fi(e, null, n),
            t
          );
        }
        return (li(e, r, t, n), pi(e));
      }
      function Xa(e, t, n) {
        if (
          ((t = t.updateQueue), t !== null && ((t = t.shared), n & 4194048))
        ) {
          var r = t.lanes;
          ((r &= e.pendingLanes), (n |= r), (t.lanes = n), st(e, n));
        }
      }
      function Za(e, t) {
        var n = e.updateQueue,
          r = e.alternate;
        if (r !== null && ((r = r.updateQueue), n === r)) {
          var i = null,
            a = null;
          if (((n = n.firstBaseUpdate), n !== null)) {
            do {
              var o = {
                lane: n.lane,
                tag: n.tag,
                payload: n.payload,
                callback: null,
                next: null,
              };
              (a === null ? (i = a = o) : (a = a.next = o), (n = n.next));
            } while (n !== null);
            a === null ? (i = a = t) : (a = a.next = t);
          } else i = a = t;
          ((n = {
            baseState: r.baseState,
            firstBaseUpdate: i,
            lastBaseUpdate: a,
            shared: r.shared,
            callbacks: r.callbacks,
          }),
            (e.updateQueue = n));
          return;
        }
        ((e = n.lastBaseUpdate),
          e === null ? (n.firstBaseUpdate = t) : (e.next = t),
          (n.lastBaseUpdate = t));
      }
      var Qa = !1;
      function $a() {
        if (Qa) {
          var e = xa;
          if (e !== null) throw e;
        }
      }
      function eo(e, t, n, r) {
        Qa = !1;
        var i = e.updateQueue;
        Ka = !1;
        var a = i.firstBaseUpdate,
          o = i.lastBaseUpdate,
          s = i.shared.pending;
        if (s !== null) {
          i.shared.pending = null;
          var c = s,
            l = c.next;
          ((c.next = null), o === null ? (a = l) : (o.next = l), (o = c));
          var u = e.alternate;
          u !== null &&
            ((u = u.updateQueue),
            (s = u.lastBaseUpdate),
            s !== o &&
              (s === null ? (u.firstBaseUpdate = l) : (s.next = l),
              (u.lastBaseUpdate = c)));
        }
        if (a !== null) {
          var d = i.baseState;
          ((o = 0), (u = l = c = null), (s = a));
          do {
            var p = s.lane & -536870913,
              m = p !== s.lane;
            if (m ? (J & p) === p : (r & p) === p) {
              (p !== 0 && p === ba && (Qa = !0),
                u !== null &&
                  (u = u.next =
                    {
                      lane: 0,
                      tag: s.tag,
                      payload: s.payload,
                      callback: null,
                      next: null,
                    }));
              a: {
                var h = e,
                  g = s;
                p = t;
                var _ = n;
                switch (g.tag) {
                  case 1:
                    if (((h = g.payload), typeof h == `function`)) {
                      d = h.call(_, d, p);
                      break a;
                    }
                    d = h;
                    break a;
                  case 3:
                    h.flags = (h.flags & -65537) | 128;
                  case 0:
                    if (
                      ((h = g.payload),
                      (p = typeof h == `function` ? h.call(_, d, p) : h),
                      p == null)
                    )
                      break a;
                    d = f({}, d, p);
                    break a;
                  case 2:
                    Ka = !0;
                }
              }
              ((p = s.callback),
                p !== null &&
                  ((e.flags |= 64),
                  m && (e.flags |= 8192),
                  (m = i.callbacks),
                  m === null ? (i.callbacks = [p]) : m.push(p)));
            } else
              ((m = {
                lane: p,
                tag: s.tag,
                payload: s.payload,
                callback: s.callback,
                next: null,
              }),
                u === null ? ((l = u = m), (c = d)) : (u = u.next = m),
                (o |= p));
            if (((s = s.next), s === null)) {
              if (((s = i.shared.pending), s === null)) break;
              ((m = s),
                (s = m.next),
                (m.next = null),
                (i.lastBaseUpdate = m),
                (i.shared.pending = null));
            }
          } while (1);
          (u === null && (c = d),
            (i.baseState = c),
            (i.firstBaseUpdate = l),
            (i.lastBaseUpdate = u),
            a === null && (i.shared.lanes = 0),
            (ql |= o),
            (e.lanes = o),
            (e.memoizedState = d));
        }
      }
      function to(e, t) {
        if (typeof e != `function`) throw Error(i(191, e));
        e.call(t);
      }
      function no(e, t) {
        var n = e.callbacks;
        if (n !== null)
          for (e.callbacks = null, e = 0; e < n.length; e++) to(n[e], t);
      }
      var ro = pe(null),
        io = pe(0);
      function ao(e, t) {
        ((e = Gl), k(io, e), k(ro, t), (Gl = e | t.baseLanes));
      }
      function oo() {
        (k(io, Gl), k(ro, ro.current));
      }
      function so() {
        ((Gl = io.current), O(ro), O(io));
      }
      var co = pe(null),
        F = null;
      function lo(e) {
        var t = e.alternate;
        (k(po, po.current & 1),
          k(co, e),
          F === null &&
            (t === null || ro.current !== null || t.memoizedState !== null) &&
            (F = e));
      }
      function I(e) {
        (k(po, po.current), k(co, e), F === null && (F = e));
      }
      function uo(e) {
        e.tag === 22
          ? (k(po, po.current), k(co, e), F === null && (F = e))
          : fo(e);
      }
      function fo() {
        (k(po, po.current), k(co, co.current));
      }
      function L(e) {
        (O(co), F === e && (F = null), O(po));
      }
      var po = pe(0);
      function mo(e) {
        for (var t = e; t !== null; ) {
          if (t.tag === 13) {
            var n = t.memoizedState;
            if (
              n !== null &&
              ((n = n.dehydrated), n === null || lf(n) || uf(n))
            )
              return t;
          } else if (
            t.tag === 19 &&
            (t.memoizedProps.revealOrder === `forwards` ||
              t.memoizedProps.revealOrder === `backwards` ||
              t.memoizedProps.revealOrder === `unstable_legacy-backwards` ||
              t.memoizedProps.revealOrder === `together`)
          ) {
            if (t.flags & 128) return t;
          } else if (t.child !== null) {
            ((t.child.return = t), (t = t.child));
            continue;
          }
          if (t === e) break;
          for (; t.sibling === null; ) {
            if (t.return === null || t.return === e) return null;
            t = t.return;
          }
          ((t.sibling.return = t.return), (t = t.sibling));
        }
        return null;
      }
      var ho = 0,
        R = null,
        go = null,
        z = null,
        _o = !1,
        vo = !1,
        yo = !1,
        B = 0,
        bo = 0,
        xo = null,
        So = 0;
      function V() {
        throw Error(i(321));
      }
      function Co(e, t) {
        if (t === null) return !1;
        for (var n = 0; n < t.length && n < e.length; n++)
          if (!Ar(e[n], t[n])) return !1;
        return !0;
      }
      function wo(e, t, n, r, i, a) {
        return (
          (ho = a),
          (R = t),
          (t.memoizedState = null),
          (t.updateQueue = null),
          (t.lanes = 0),
          (E.H = e === null || e.memoizedState === null ? Bs : Vs),
          (yo = !1),
          (a = n(r, i)),
          (yo = !1),
          vo && (a = Eo(t, n, r, i)),
          To(e),
          a
        );
      }
      function To(e) {
        E.H = zs;
        var t = go !== null && go.next !== null;
        if (
          ((ho = 0), (z = go = R = null), (_o = !1), (bo = 0), (xo = null), t)
        )
          throw Error(i(300));
        e === null ||
          ic ||
          ((e = e.dependencies), e !== null && sa(e) && (ic = !0));
      }
      function Eo(e, t, n, r) {
        R = e;
        var a = 0;
        do {
          if ((vo && (xo = null), (bo = 0), (vo = !1), 25 <= a))
            throw Error(i(301));
          if (((a += 1), (z = go = null), e.updateQueue != null)) {
            var o = e.updateQueue;
            ((o.lastEffect = null),
              (o.events = null),
              (o.stores = null),
              o.memoCache != null && (o.memoCache.index = 0));
          }
          ((E.H = Hs), (o = t(n, r)));
        } while (vo);
        return o;
      }
      function Do() {
        var e = E.H,
          t = e.useState()[0];
        return (
          (t = typeof t.then == `function` ? Po(t) : t),
          (e = e.useState()[0]),
          (go === null ? null : go.memoizedState) !== e && (R.flags |= 1024),
          t
        );
      }
      function Oo() {
        var e = B !== 0;
        return ((B = 0), e);
      }
      function ko(e, t, n) {
        ((t.updateQueue = e.updateQueue), (t.flags &= -2053), (e.lanes &= ~n));
      }
      function Ao(e) {
        if (_o) {
          for (e = e.memoizedState; e !== null; ) {
            var t = e.queue;
            (t !== null && (t.pending = null), (e = e.next));
          }
          _o = !1;
        }
        ((ho = 0), (z = go = R = null), (vo = !1), (bo = B = 0), (xo = null));
      }
      function jo() {
        var e = {
          memoizedState: null,
          baseState: null,
          baseQueue: null,
          queue: null,
          next: null,
        };
        return (z === null ? (R.memoizedState = z = e) : (z = z.next = e), z);
      }
      function Mo() {
        if (go === null) {
          var e = R.alternate;
          e = e === null ? null : e.memoizedState;
        } else e = go.next;
        var t = z === null ? R.memoizedState : z.next;
        if (t !== null) ((z = t), (go = e));
        else {
          if (e === null)
            throw R.alternate === null ? Error(i(467)) : Error(i(310));
          ((go = e),
            (e = {
              memoizedState: go.memoizedState,
              baseState: go.baseState,
              baseQueue: go.baseQueue,
              queue: go.queue,
              next: null,
            }),
            z === null ? (R.memoizedState = z = e) : (z = z.next = e));
        }
        return z;
      }
      function No() {
        return {
          lastEffect: null,
          events: null,
          stores: null,
          memoCache: null,
        };
      }
      function Po(e) {
        var t = bo;
        return (
          (bo += 1),
          xo === null && (xo = []),
          (e = Na(xo, e, t)),
          (t = R),
          (z === null ? t.memoizedState : z.next) === null &&
            ((t = t.alternate),
            (E.H = t === null || t.memoizedState === null ? Bs : Vs)),
          e
        );
      }
      function Fo(e) {
        if (typeof e == `object` && e) {
          if (typeof e.then == `function`) return Po(e);
          if (e.$$typeof === S) return la(e);
        }
        throw Error(i(438, String(e)));
      }
      function Io(e) {
        var t = null,
          n = R.updateQueue;
        if ((n !== null && (t = n.memoCache), t == null)) {
          var r = R.alternate;
          r !== null &&
            ((r = r.updateQueue),
            r !== null &&
              ((r = r.memoCache),
              r != null &&
                (t = {
                  data: r.data.map(function (e) {
                    return e.slice();
                  }),
                  index: 0,
                })));
        }
        if (
          ((t ??= { data: [], index: 0 }),
          n === null && ((n = No()), (R.updateQueue = n)),
          (n.memoCache = t),
          (n = t.data[t.index]),
          n === void 0)
        )
          for (n = t.data[t.index] = Array(e), r = 0; r < e; r++) n[r] = ae;
        return (t.index++, n);
      }
      function Lo(e, t) {
        return typeof t == `function` ? t(e) : t;
      }
      function Ro(e) {
        return zo(Mo(), go, e);
      }
      function zo(e, t, n) {
        var r = e.queue;
        if (r === null) throw Error(i(311));
        r.lastRenderedReducer = n;
        var a = e.baseQueue,
          o = r.pending;
        if (o !== null) {
          if (a !== null) {
            var s = a.next;
            ((a.next = o.next), (o.next = s));
          }
          ((t.baseQueue = a = o), (r.pending = null));
        }
        if (((o = e.baseState), a === null)) e.memoizedState = o;
        else {
          t = a.next;
          var c = (s = null),
            l = null,
            u = t,
            d = !1;
          do {
            var f = u.lane & -536870913;
            if (f === u.lane ? (ho & f) === f : (J & f) === f) {
              var p = u.revertLane;
              if (p === 0)
                (l !== null &&
                  (l = l.next =
                    {
                      lane: 0,
                      revertLane: 0,
                      gesture: null,
                      action: u.action,
                      hasEagerState: u.hasEagerState,
                      eagerState: u.eagerState,
                      next: null,
                    }),
                  f === ba && (d = !0));
              else if ((ho & p) === p) {
                ((u = u.next), p === ba && (d = !0));
                continue;
              } else
                ((f = {
                  lane: 0,
                  revertLane: u.revertLane,
                  gesture: null,
                  action: u.action,
                  hasEagerState: u.hasEagerState,
                  eagerState: u.eagerState,
                  next: null,
                }),
                  l === null ? ((c = l = f), (s = o)) : (l = l.next = f),
                  (R.lanes |= p),
                  (ql |= p));
              ((f = u.action),
                yo && n(o, f),
                (o = u.hasEagerState ? u.eagerState : n(o, f)));
            } else
              ((p = {
                lane: f,
                revertLane: u.revertLane,
                gesture: u.gesture,
                action: u.action,
                hasEagerState: u.hasEagerState,
                eagerState: u.eagerState,
                next: null,
              }),
                l === null ? ((c = l = p), (s = o)) : (l = l.next = p),
                (R.lanes |= f),
                (ql |= f));
            u = u.next;
          } while (u !== null && u !== t);
          if (
            (l === null ? (s = o) : (l.next = c),
            !Ar(o, e.memoizedState) && ((ic = !0), d && ((n = xa), n !== null)))
          )
            throw n;
          ((e.memoizedState = o),
            (e.baseState = s),
            (e.baseQueue = l),
            (r.lastRenderedState = o));
        }
        return (a === null && (r.lanes = 0), [e.memoizedState, r.dispatch]);
      }
      function Bo(e) {
        var t = Mo(),
          n = t.queue;
        if (n === null) throw Error(i(311));
        n.lastRenderedReducer = e;
        var r = n.dispatch,
          a = n.pending,
          o = t.memoizedState;
        if (a !== null) {
          n.pending = null;
          var s = (a = a.next);
          do ((o = e(o, s.action)), (s = s.next));
          while (s !== a);
          (Ar(o, t.memoizedState) || (ic = !0),
            (t.memoizedState = o),
            t.baseQueue === null && (t.baseState = o),
            (n.lastRenderedState = o));
        }
        return [o, r];
      }
      function Vo(e, t, n) {
        var r = R,
          a = Mo(),
          o = j;
        if (o) {
          if (n === void 0) throw Error(i(407));
          n = n();
        } else n = t();
        var s = !Ar((go || a).memoizedState, n);
        if (
          (s && ((a.memoizedState = n), (ic = !0)),
          (a = a.queue),
          fs(Wo.bind(null, r, a, e), [e]),
          a.getSnapshot !== t || s || (z !== null && z.memoizedState.tag & 1))
        ) {
          if (
            ((r.flags |= 2048),
            ss(9, { destroy: void 0 }, Uo.bind(null, r, a, n, t), null),
            zl === null)
          )
            throw Error(i(349));
          o || ho & 127 || Ho(r, t, n);
        }
        return n;
      }
      function Ho(e, t, n) {
        ((e.flags |= 16384),
          (e = { getSnapshot: t, value: n }),
          (t = R.updateQueue),
          t === null
            ? ((t = No()), (R.updateQueue = t), (t.stores = [e]))
            : ((n = t.stores), n === null ? (t.stores = [e]) : n.push(e)));
      }
      function Uo(e, t, n, r) {
        ((t.value = n), (t.getSnapshot = r), Go(t) && Ko(e));
      }
      function Wo(e, t, n) {
        return n(function () {
          Go(t) && Ko(e);
        });
      }
      function Go(e) {
        var t = e.getSnapshot;
        e = e.value;
        try {
          var n = t();
          return !Ar(e, n);
        } catch {
          return !0;
        }
      }
      function Ko(e) {
        var t = di(e, 2);
        t !== null && _u(t, e, 2);
      }
      function qo(e) {
        var t = jo();
        if (typeof e == `function`) {
          var n = e;
          if (((e = n()), yo)) {
            We(!0);
            try {
              n();
            } finally {
              We(!1);
            }
          }
        }
        return (
          (t.memoizedState = t.baseState = e),
          (t.queue = {
            pending: null,
            lanes: 0,
            dispatch: null,
            lastRenderedReducer: Lo,
            lastRenderedState: e,
          }),
          t
        );
      }
      function Jo(e, t, n, r) {
        return ((e.baseState = n), zo(e, go, typeof r == `function` ? r : Lo));
      }
      function Yo(e, t, n, r, a) {
        if (Is(e)) throw Error(i(485));
        if (((e = t.action), e !== null)) {
          var o = {
            payload: a,
            action: e,
            next: null,
            isTransition: !0,
            status: `pending`,
            value: null,
            reason: null,
            listeners: [],
            then: function (e) {
              o.listeners.push(e);
            },
          };
          (E.T === null ? (o.isTransition = !1) : n(!0),
            r(o),
            (n = t.pending),
            n === null
              ? ((o.next = t.pending = o), Xo(t, o))
              : ((o.next = n.next), (t.pending = n.next = o)));
        }
      }
      function Xo(e, t) {
        var n = t.action,
          r = t.payload,
          i = e.state;
        if (t.isTransition) {
          var a = E.T,
            o = {};
          E.T = o;
          try {
            var s = n(i, r),
              c = E.S;
            (c !== null && c(o, s), Zo(e, t, s));
          } catch (n) {
            $o(e, t, n);
          } finally {
            (a !== null && o.types !== null && (a.types = o.types), (E.T = a));
          }
        } else
          try {
            ((a = n(i, r)), Zo(e, t, a));
          } catch (n) {
            $o(e, t, n);
          }
      }
      function Zo(e, t, n) {
        typeof n == `object` && n && typeof n.then == `function`
          ? n.then(
              function (n) {
                Qo(e, t, n);
              },
              function (n) {
                return $o(e, t, n);
              },
            )
          : Qo(e, t, n);
      }
      function Qo(e, t, n) {
        ((t.status = `fulfilled`),
          (t.value = n),
          es(t),
          (e.state = n),
          (t = e.pending),
          t !== null &&
            ((n = t.next),
            n === t
              ? (e.pending = null)
              : ((n = n.next), (t.next = n), Xo(e, n))));
      }
      function $o(e, t, n) {
        var r = e.pending;
        if (((e.pending = null), r !== null)) {
          r = r.next;
          do ((t.status = `rejected`), (t.reason = n), es(t), (t = t.next));
          while (t !== r);
        }
        e.action = null;
      }
      function es(e) {
        e = e.listeners;
        for (var t = 0; t < e.length; t++) (0, e[t])();
      }
      function ts(e, t) {
        return t;
      }
      function ns(e, t) {
        if (j) {
          var n = zl.formState;
          if (n !== null) {
            a: {
              var r = R;
              if (j) {
                if (Hi) {
                  b: {
                    for (var i = Hi, a = Wi; i.nodeType !== 8; ) {
                      if (!a) {
                        i = null;
                        break b;
                      }
                      if (((i = ff(i.nextSibling)), i === null)) {
                        i = null;
                        break b;
                      }
                    }
                    ((a = i.data), (i = a === `F!` || a === `F` ? i : null));
                  }
                  if (i) {
                    ((Hi = ff(i.nextSibling)), (r = i.data === `F!`));
                    break a;
                  }
                }
                Ki(r);
              }
              r = !1;
            }
            r && (t = n[0]);
          }
        }
        return (
          (n = jo()),
          (n.memoizedState = n.baseState = t),
          (r = {
            pending: null,
            lanes: 0,
            dispatch: null,
            lastRenderedReducer: ts,
            lastRenderedState: t,
          }),
          (n.queue = r),
          (n = Ns.bind(null, R, r)),
          (r.dispatch = n),
          (r = qo(!1)),
          (a = Fs.bind(null, R, !1, r.queue)),
          (r = jo()),
          (i = { state: t, dispatch: null, action: e, pending: null }),
          (r.queue = i),
          (n = Yo.bind(null, R, i, a, n)),
          (i.dispatch = n),
          (r.memoizedState = e),
          [t, n, !1]
        );
      }
      function rs(e) {
        return is(Mo(), go, e);
      }
      function is(e, t, n) {
        if (
          ((t = zo(e, t, ts)[0]),
          (e = Ro(Lo)[0]),
          typeof t == `object` && t && typeof t.then == `function`)
        )
          try {
            var r = Po(t);
          } catch (e) {
            throw e === Oa ? Aa : e;
          }
        else r = t;
        t = Mo();
        var i = t.queue,
          a = i.dispatch;
        return (
          n !== t.memoizedState &&
            ((R.flags |= 2048),
            ss(9, { destroy: void 0 }, as.bind(null, i, n), null)),
          [r, a, e]
        );
      }
      function as(e, t) {
        e.action = t;
      }
      function os(e) {
        var t = Mo(),
          n = go;
        if (n !== null) return is(t, n, e);
        (Mo(), (t = t.memoizedState), (n = Mo()));
        var r = n.queue.dispatch;
        return ((n.memoizedState = e), [t, r, !1]);
      }
      function ss(e, t, n, r) {
        return (
          (e = { tag: e, create: n, deps: r, inst: t, next: null }),
          (t = R.updateQueue),
          t === null && ((t = No()), (R.updateQueue = t)),
          (n = t.lastEffect),
          n === null
            ? (t.lastEffect = e.next = e)
            : ((r = n.next), (n.next = e), (e.next = r), (t.lastEffect = e)),
          e
        );
      }
      function cs() {
        return Mo().memoizedState;
      }
      function ls(e, t, n, r) {
        var i = jo();
        ((R.flags |= e),
          (i.memoizedState = ss(
            1 | t,
            { destroy: void 0 },
            n,
            r === void 0 ? null : r,
          )));
      }
      function us(e, t, n, r) {
        var i = Mo();
        r = r === void 0 ? null : r;
        var a = i.memoizedState.inst;
        go !== null && r !== null && Co(r, go.memoizedState.deps)
          ? (i.memoizedState = ss(t, a, n, r))
          : ((R.flags |= e), (i.memoizedState = ss(1 | t, a, n, r)));
      }
      function ds(e, t) {
        ls(8390656, 8, e, t);
      }
      function fs(e, t) {
        us(2048, 8, e, t);
      }
      function ps(e) {
        R.flags |= 4;
        var t = R.updateQueue;
        if (t === null) ((t = No()), (R.updateQueue = t), (t.events = [e]));
        else {
          var n = t.events;
          n === null ? (t.events = [e]) : n.push(e);
        }
      }
      function ms(e) {
        var t = Mo().memoizedState;
        return (
          ps({ ref: t, nextImpl: e }),
          function () {
            if (K & 2) throw Error(i(440));
            return t.impl.apply(void 0, arguments);
          }
        );
      }
      function hs(e, t) {
        return us(4, 2, e, t);
      }
      function gs(e, t) {
        return us(4, 4, e, t);
      }
      function H(e, t) {
        if (typeof t == `function`) {
          e = e();
          var n = t(e);
          return function () {
            typeof n == `function` ? n() : t(null);
          };
        }
        if (t != null)
          return (
            (e = e()),
            (t.current = e),
            function () {
              t.current = null;
            }
          );
      }
      function _s(e, t, n) {
        ((n = n == null ? null : n.concat([e])),
          us(4, 4, H.bind(null, t, e), n));
      }
      function vs() {}
      function ys(e, t) {
        var n = Mo();
        t = t === void 0 ? null : t;
        var r = n.memoizedState;
        return t !== null && Co(t, r[1])
          ? r[0]
          : ((n.memoizedState = [e, t]), e);
      }
      function bs(e, t) {
        var n = Mo();
        t = t === void 0 ? null : t;
        var r = n.memoizedState;
        if (t !== null && Co(t, r[1])) return r[0];
        if (((r = e()), yo)) {
          We(!0);
          try {
            e();
          } finally {
            We(!1);
          }
        }
        return ((n.memoizedState = [r, t]), r);
      }
      function xs(e, t, n) {
        return n === void 0 || (ho & 1073741824 && !(J & 261930))
          ? (e.memoizedState = t)
          : ((e.memoizedState = n), (e = gu()), (R.lanes |= e), (ql |= e), n);
      }
      function Ss(e, t, n, r) {
        return Ar(n, t)
          ? n
          : ro.current === null
            ? !(ho & 42) || (ho & 1073741824 && !(J & 261930))
              ? ((ic = !0), (e.memoizedState = n))
              : ((e = gu()), (R.lanes |= e), (ql |= e), t)
            : ((e = xs(e, n, r)), Ar(e, t) || (ic = !0), e);
      }
      function Cs(e, t, n, r, i) {
        var a = D.p;
        D.p = a !== 0 && 8 > a ? a : 8;
        var o = E.T,
          s = {};
        ((E.T = s), Fs(e, !1, t, n));
        try {
          var c = i(),
            l = E.S;
          (l !== null && l(s, c),
            typeof c == `object` && c && typeof c.then == `function`
              ? Ps(e, t, wa(c, r), hu(e))
              : Ps(e, t, r, hu(e)));
        } catch (n) {
          Ps(
            e,
            t,
            { then: function () {}, status: `rejected`, reason: n },
            hu(),
          );
        } finally {
          ((D.p = a),
            o !== null && s.types !== null && (o.types = s.types),
            (E.T = o));
        }
      }
      function ws() {}
      function Ts(e, t, n, r) {
        if (e.tag !== 5) throw Error(i(476));
        var a = Es(e).queue;
        Cs(
          e,
          a,
          t,
          ue,
          n === null
            ? ws
            : function () {
                return (Ds(e), n(r));
              },
        );
      }
      function Es(e) {
        var t = e.memoizedState;
        if (t !== null) return t;
        t = {
          memoizedState: ue,
          baseState: ue,
          baseQueue: null,
          queue: {
            pending: null,
            lanes: 0,
            dispatch: null,
            lastRenderedReducer: Lo,
            lastRenderedState: ue,
          },
          next: null,
        };
        var n = {};
        return (
          (t.next = {
            memoizedState: n,
            baseState: n,
            baseQueue: null,
            queue: {
              pending: null,
              lanes: 0,
              dispatch: null,
              lastRenderedReducer: Lo,
              lastRenderedState: n,
            },
            next: null,
          }),
          (e.memoizedState = t),
          (e = e.alternate),
          e !== null && (e.memoizedState = t),
          t
        );
      }
      function Ds(e) {
        var t = Es(e);
        (t.next === null && (t = e.alternate.memoizedState),
          Ps(e, t.next.queue, {}, hu()));
      }
      function Os() {
        return la(np);
      }
      function ks() {
        return Mo().memoizedState;
      }
      function As() {
        return Mo().memoizedState;
      }
      function js(e) {
        for (var t = e.return; t !== null; ) {
          switch (t.tag) {
            case 24:
            case 3:
              var n = hu();
              e = Ya(n);
              var r = P(t, e, n);
              (r !== null && (_u(r, t, n), Xa(r, t, n)),
                (t = { cache: ga() }),
                (e.payload = t));
              return;
          }
          t = t.return;
        }
      }
      function Ms(e, t, n) {
        var r = hu();
        ((n = {
          lane: r,
          revertLane: 0,
          gesture: null,
          action: n,
          hasEagerState: !1,
          eagerState: null,
          next: null,
        }),
          Is(e)
            ? Ls(t, n)
            : ((n = ui(e, t, n, r)), n !== null && (_u(n, e, r), Rs(n, t, r))));
      }
      function Ns(e, t, n) {
        Ps(e, t, n, hu());
      }
      function Ps(e, t, n, r) {
        var i = {
          lane: r,
          revertLane: 0,
          gesture: null,
          action: n,
          hasEagerState: !1,
          eagerState: null,
          next: null,
        };
        if (Is(e)) Ls(t, i);
        else {
          var a = e.alternate;
          if (
            e.lanes === 0 &&
            (a === null || a.lanes === 0) &&
            ((a = t.lastRenderedReducer), a !== null)
          )
            try {
              var o = t.lastRenderedState,
                s = a(o, n);
              if (((i.hasEagerState = !0), (i.eagerState = s), Ar(s, o)))
                return (li(e, t, i, 0), zl === null && ci(), !1);
            } catch {}
          if (((n = ui(e, t, i, r)), n !== null))
            return (_u(n, e, r), Rs(n, t, r), !0);
        }
        return !1;
      }
      function Fs(e, t, n, r) {
        if (
          ((r = {
            lane: 2,
            revertLane: md(),
            gesture: null,
            action: r,
            hasEagerState: !1,
            eagerState: null,
            next: null,
          }),
          Is(e))
        ) {
          if (t) throw Error(i(479));
        } else ((t = ui(e, n, r, 2)), t !== null && _u(t, e, 2));
      }
      function Is(e) {
        var t = e.alternate;
        return e === R || (t !== null && t === R);
      }
      function Ls(e, t) {
        vo = _o = !0;
        var n = e.pending;
        (n === null ? (t.next = t) : ((t.next = n.next), (n.next = t)),
          (e.pending = t));
      }
      function Rs(e, t, n) {
        if (n & 4194048) {
          var r = t.lanes;
          ((r &= e.pendingLanes), (n |= r), (t.lanes = n), st(e, n));
        }
      }
      var zs = {
        readContext: la,
        use: Fo,
        useCallback: V,
        useContext: V,
        useEffect: V,
        useImperativeHandle: V,
        useLayoutEffect: V,
        useInsertionEffect: V,
        useMemo: V,
        useReducer: V,
        useRef: V,
        useState: V,
        useDebugValue: V,
        useDeferredValue: V,
        useTransition: V,
        useSyncExternalStore: V,
        useId: V,
        useHostTransitionStatus: V,
        useFormState: V,
        useActionState: V,
        useOptimistic: V,
        useMemoCache: V,
        useCacheRefresh: V,
      };
      zs.useEffectEvent = V;
      var Bs = {
          readContext: la,
          use: Fo,
          useCallback: function (e, t) {
            return ((jo().memoizedState = [e, t === void 0 ? null : t]), e);
          },
          useContext: la,
          useEffect: ds,
          useImperativeHandle: function (e, t, n) {
            ((n = n == null ? null : n.concat([e])),
              ls(4194308, 4, H.bind(null, t, e), n));
          },
          useLayoutEffect: function (e, t) {
            return ls(4194308, 4, e, t);
          },
          useInsertionEffect: function (e, t) {
            ls(4, 2, e, t);
          },
          useMemo: function (e, t) {
            var n = jo();
            t = t === void 0 ? null : t;
            var r = e();
            if (yo) {
              We(!0);
              try {
                e();
              } finally {
                We(!1);
              }
            }
            return ((n.memoizedState = [r, t]), r);
          },
          useReducer: function (e, t, n) {
            var r = jo();
            if (n !== void 0) {
              var i = n(t);
              if (yo) {
                We(!0);
                try {
                  n(t);
                } finally {
                  We(!1);
                }
              }
            } else i = t;
            return (
              (r.memoizedState = r.baseState = i),
              (e = {
                pending: null,
                lanes: 0,
                dispatch: null,
                lastRenderedReducer: e,
                lastRenderedState: i,
              }),
              (r.queue = e),
              (e = e.dispatch = Ms.bind(null, R, e)),
              [r.memoizedState, e]
            );
          },
          useRef: function (e) {
            var t = jo();
            return ((e = { current: e }), (t.memoizedState = e));
          },
          useState: function (e) {
            e = qo(e);
            var t = e.queue,
              n = Ns.bind(null, R, t);
            return ((t.dispatch = n), [e.memoizedState, n]);
          },
          useDebugValue: vs,
          useDeferredValue: function (e, t) {
            return xs(jo(), e, t);
          },
          useTransition: function () {
            var e = qo(!1);
            return (
              (e = Cs.bind(null, R, e.queue, !0, !1)),
              (jo().memoizedState = e),
              [!1, e]
            );
          },
          useSyncExternalStore: function (e, t, n) {
            var r = R,
              a = jo();
            if (j) {
              if (n === void 0) throw Error(i(407));
              n = n();
            } else {
              if (((n = t()), zl === null)) throw Error(i(349));
              J & 127 || Ho(r, t, n);
            }
            a.memoizedState = n;
            var o = { value: n, getSnapshot: t };
            return (
              (a.queue = o),
              ds(Wo.bind(null, r, o, e), [e]),
              (r.flags |= 2048),
              ss(9, { destroy: void 0 }, Uo.bind(null, r, o, n, t), null),
              n
            );
          },
          useId: function () {
            var e = jo(),
              t = zl.identifierPrefix;
            if (j) {
              var n = Fi,
                r = Pi;
              ((n = (r & ~(1 << (32 - Ge(r) - 1))).toString(32) + n),
                (t = `_` + t + `R_` + n),
                (n = B++),
                0 < n && (t += `H` + n.toString(32)),
                (t += `_`));
            } else ((n = So++), (t = `_` + t + `r_` + n.toString(32) + `_`));
            return (e.memoizedState = t);
          },
          useHostTransitionStatus: Os,
          useFormState: ns,
          useActionState: ns,
          useOptimistic: function (e) {
            var t = jo();
            t.memoizedState = t.baseState = e;
            var n = {
              pending: null,
              lanes: 0,
              dispatch: null,
              lastRenderedReducer: null,
              lastRenderedState: null,
            };
            return (
              (t.queue = n),
              (t = Fs.bind(null, R, !0, n)),
              (n.dispatch = t),
              [e, t]
            );
          },
          useMemoCache: Io,
          useCacheRefresh: function () {
            return (jo().memoizedState = js.bind(null, R));
          },
          useEffectEvent: function (e) {
            var t = jo(),
              n = { impl: e };
            return (
              (t.memoizedState = n),
              function () {
                if (K & 2) throw Error(i(440));
                return n.impl.apply(void 0, arguments);
              }
            );
          },
        },
        Vs = {
          readContext: la,
          use: Fo,
          useCallback: ys,
          useContext: la,
          useEffect: fs,
          useImperativeHandle: _s,
          useInsertionEffect: hs,
          useLayoutEffect: gs,
          useMemo: bs,
          useReducer: Ro,
          useRef: cs,
          useState: function () {
            return Ro(Lo);
          },
          useDebugValue: vs,
          useDeferredValue: function (e, t) {
            return Ss(Mo(), go.memoizedState, e, t);
          },
          useTransition: function () {
            var e = Ro(Lo)[0],
              t = Mo().memoizedState;
            return [typeof e == `boolean` ? e : Po(e), t];
          },
          useSyncExternalStore: Vo,
          useId: ks,
          useHostTransitionStatus: Os,
          useFormState: rs,
          useActionState: rs,
          useOptimistic: function (e, t) {
            return Jo(Mo(), go, e, t);
          },
          useMemoCache: Io,
          useCacheRefresh: As,
        };
      Vs.useEffectEvent = ms;
      var Hs = {
        readContext: la,
        use: Fo,
        useCallback: ys,
        useContext: la,
        useEffect: fs,
        useImperativeHandle: _s,
        useInsertionEffect: hs,
        useLayoutEffect: gs,
        useMemo: bs,
        useReducer: Bo,
        useRef: cs,
        useState: function () {
          return Bo(Lo);
        },
        useDebugValue: vs,
        useDeferredValue: function (e, t) {
          var n = Mo();
          return go === null ? xs(n, e, t) : Ss(n, go.memoizedState, e, t);
        },
        useTransition: function () {
          var e = Bo(Lo)[0],
            t = Mo().memoizedState;
          return [typeof e == `boolean` ? e : Po(e), t];
        },
        useSyncExternalStore: Vo,
        useId: ks,
        useHostTransitionStatus: Os,
        useFormState: os,
        useActionState: os,
        useOptimistic: function (e, t) {
          var n = Mo();
          return go === null
            ? ((n.baseState = e), [e, n.queue.dispatch])
            : Jo(n, go, e, t);
        },
        useMemoCache: Io,
        useCacheRefresh: As,
      };
      Hs.useEffectEvent = ms;
      function Us(e, t, n, r) {
        ((t = e.memoizedState),
          (n = n(r, t)),
          (n = n == null ? t : f({}, t, n)),
          (e.memoizedState = n),
          e.lanes === 0 && (e.updateQueue.baseState = n));
      }
      var Ws = {
        enqueueSetState: function (e, t, n) {
          e = e._reactInternals;
          var r = hu(),
            i = Ya(r);
          ((i.payload = t),
            n != null && (i.callback = n),
            (t = P(e, i, r)),
            t !== null && (_u(t, e, r), Xa(t, e, r)));
        },
        enqueueReplaceState: function (e, t, n) {
          e = e._reactInternals;
          var r = hu(),
            i = Ya(r);
          ((i.tag = 1),
            (i.payload = t),
            n != null && (i.callback = n),
            (t = P(e, i, r)),
            t !== null && (_u(t, e, r), Xa(t, e, r)));
        },
        enqueueForceUpdate: function (e, t) {
          e = e._reactInternals;
          var n = hu(),
            r = Ya(n);
          ((r.tag = 2),
            t != null && (r.callback = t),
            (t = P(e, r, n)),
            t !== null && (_u(t, e, n), Xa(t, e, n)));
        },
      };
      function Gs(e, t, n, r, i, a, o) {
        return (
          (e = e.stateNode),
          typeof e.shouldComponentUpdate == `function`
            ? e.shouldComponentUpdate(r, a, o)
            : t.prototype && t.prototype.isPureReactComponent
              ? !jr(n, r) || !jr(i, a)
              : !0
        );
      }
      function Ks(e, t, n, r) {
        ((e = t.state),
          typeof t.componentWillReceiveProps == `function` &&
            t.componentWillReceiveProps(n, r),
          typeof t.UNSAFE_componentWillReceiveProps == `function` &&
            t.UNSAFE_componentWillReceiveProps(n, r),
          t.state !== e && Ws.enqueueReplaceState(t, t.state, null));
      }
      function qs(e, t) {
        var n = t;
        if (`ref` in t)
          for (var r in ((n = {}), t)) r !== `ref` && (n[r] = t[r]);
        if ((e = e.defaultProps))
          for (var i in (n === t && (n = f({}, n)), e))
            n[i] === void 0 && (n[i] = e[i]);
        return n;
      }
      function Js(e) {
        ii(e);
      }
      function Ys(e) {
        console.error(e);
      }
      function Xs(e) {
        ii(e);
      }
      function Zs(e, t) {
        try {
          var n = e.onUncaughtError;
          n(t.value, { componentStack: t.stack });
        } catch (e) {
          setTimeout(function () {
            throw e;
          });
        }
      }
      function Qs(e, t, n) {
        try {
          var r = e.onCaughtError;
          r(n.value, {
            componentStack: n.stack,
            errorBoundary: t.tag === 1 ? t.stateNode : null,
          });
        } catch (e) {
          setTimeout(function () {
            throw e;
          });
        }
      }
      function $s(e, t, n) {
        return (
          (n = Ya(n)),
          (n.tag = 3),
          (n.payload = { element: null }),
          (n.callback = function () {
            Zs(e, t);
          }),
          n
        );
      }
      function ec(e) {
        return ((e = Ya(e)), (e.tag = 3), e);
      }
      function tc(e, t, n, r) {
        var i = n.type.getDerivedStateFromError;
        if (typeof i == `function`) {
          var a = r.value;
          ((e.payload = function () {
            return i(a);
          }),
            (e.callback = function () {
              Qs(t, n, r);
            }));
        }
        var o = n.stateNode;
        o !== null &&
          typeof o.componentDidCatch == `function` &&
          (e.callback = function () {
            (Qs(t, n, r),
              typeof i != `function` &&
                (au === null ? (au = new Set([this])) : au.add(this)));
            var e = r.stack;
            this.componentDidCatch(r.value, {
              componentStack: e === null ? `` : e,
            });
          });
      }
      function nc(e, t, n, r, a) {
        if (
          ((n.flags |= 32768),
          typeof r == `object` && r && typeof r.then == `function`)
        ) {
          if (
            ((t = n.alternate),
            t !== null && oa(t, n, a, !0),
            (n = co.current),
            n !== null)
          ) {
            switch (n.tag) {
              case 31:
              case 13:
                return (
                  F === null
                    ? ku()
                    : n.alternate === null && Kl === 0 && (Kl = 3),
                  (n.flags &= -257),
                  (n.flags |= 65536),
                  (n.lanes = a),
                  r === ja
                    ? (n.flags |= 16384)
                    : ((t = n.updateQueue),
                      t === null ? (n.updateQueue = new Set([r])) : t.add(r),
                      Ju(e, r, a)),
                  !1
                );
              case 22:
                return (
                  (n.flags |= 65536),
                  r === ja
                    ? (n.flags |= 16384)
                    : ((t = n.updateQueue),
                      t === null
                        ? ((t = {
                            transitions: null,
                            markerInstances: null,
                            retryQueue: new Set([r]),
                          }),
                          (n.updateQueue = t))
                        : ((n = t.retryQueue),
                          n === null
                            ? (t.retryQueue = new Set([r]))
                            : n.add(r)),
                      Ju(e, r, a)),
                  !1
                );
            }
            throw Error(i(435, n.tag));
          }
          return (Ju(e, r, a), ku(), !1);
        }
        if (j)
          return (
            (t = co.current),
            t === null
              ? (r !== Gi && ((t = Error(i(423), { cause: r })), Qi(Ei(t, n))),
                (e = e.current.alternate),
                (e.flags |= 65536),
                (a &= -a),
                (e.lanes |= a),
                (r = Ei(r, n)),
                (a = $s(e.stateNode, r, a)),
                Za(e, a),
                Kl !== 4 && (Kl = 2))
              : (!(t.flags & 65536) && (t.flags |= 256),
                (t.flags |= 65536),
                (t.lanes = a),
                r !== Gi && ((e = Error(i(422), { cause: r })), Qi(Ei(e, n)))),
            !1
          );
        var o = Error(i(520), { cause: r });
        if (
          ((o = Ei(o, n)),
          Ql === null ? (Ql = [o]) : Ql.push(o),
          Kl !== 4 && (Kl = 2),
          t === null)
        )
          return !0;
        ((r = Ei(r, n)), (n = t));
        do {
          switch (n.tag) {
            case 3:
              return (
                (n.flags |= 65536),
                (e = a & -a),
                (n.lanes |= e),
                (e = $s(n.stateNode, r, e)),
                Za(n, e),
                !1
              );
            case 1:
              if (
                ((t = n.type),
                (o = n.stateNode),
                !(n.flags & 128) &&
                  (typeof t.getDerivedStateFromError == `function` ||
                    (o !== null &&
                      typeof o.componentDidCatch == `function` &&
                      (au === null || !au.has(o)))))
              )
                return (
                  (n.flags |= 65536),
                  (a &= -a),
                  (n.lanes |= a),
                  (a = ec(a)),
                  tc(a, e, n, r),
                  Za(n, a),
                  !1
                );
          }
          n = n.return;
        } while (n !== null);
        return !1;
      }
      var rc = Error(i(461)),
        ic = !1;
      function ac(e, t, n, r) {
        t.child = e === null ? Ga(t, null, n, r) : Wa(t, e.child, n, r);
      }
      function oc(e, t, n, r, i) {
        n = n.render;
        var a = t.ref;
        if (`ref` in r) {
          var o = {};
          for (var s in r) s !== `ref` && (o[s] = r[s]);
        } else o = r;
        return (
          ca(t),
          (r = wo(e, t, n, o, a, i)),
          (s = Oo()),
          e !== null && !ic
            ? (ko(e, t, i), Ac(e, t, i))
            : (j && s && Ri(t), (t.flags |= 1), ac(e, t, r, i), t.child)
        );
      }
      function sc(e, t, n, r, i) {
        if (e === null) {
          var a = n.type;
          return typeof a == `function` &&
            !_i(a) &&
            a.defaultProps === void 0 &&
            n.compare === null
            ? ((t.tag = 15), (t.type = a), cc(e, t, a, r, i))
            : ((e = bi(n.type, null, r, t, t.mode, i)),
              (e.ref = t.ref),
              (e.return = t),
              (t.child = e));
        }
        if (((a = e.child), !jc(e, i))) {
          var o = a.memoizedProps;
          if (
            ((n = n.compare),
            (n = n === null ? jr : n),
            n(o, r) && e.ref === t.ref)
          )
            return Ac(e, t, i);
        }
        return (
          (t.flags |= 1),
          (e = vi(a, r)),
          (e.ref = t.ref),
          (e.return = t),
          (t.child = e)
        );
      }
      function cc(e, t, n, r, i) {
        if (e !== null) {
          var a = e.memoizedProps;
          if (jr(a, r) && e.ref === t.ref)
            if (((ic = !1), (t.pendingProps = r = a), jc(e, i)))
              e.flags & 131072 && (ic = !0);
            else return ((t.lanes = e.lanes), Ac(e, t, i));
        }
        return gc(e, t, n, r, i);
      }
      function lc(e, t, n, r) {
        var i = r.children,
          a = e === null ? null : e.memoizedState;
        if (
          (e === null &&
            t.stateNode === null &&
            (t.stateNode = {
              _visibility: 1,
              _pendingMarkers: null,
              _retryCache: null,
              _transitions: null,
            }),
          r.mode === `hidden`)
        ) {
          if (t.flags & 128) {
            if (((a = a === null ? n : a.baseLanes | n), e !== null)) {
              for (r = t.child = e.child, i = 0; r !== null; )
                ((i = i | r.lanes | r.childLanes), (r = r.sibling));
              r = i & ~a;
            } else ((r = 0), (t.child = null));
            return dc(e, t, a, n, r);
          }
          if (n & 536870912)
            ((t.memoizedState = { baseLanes: 0, cachePool: null }),
              e !== null && Da(t, a === null ? null : a.cachePool),
              a === null ? oo() : ao(t, a),
              uo(t));
          else
            return (
              (r = t.lanes = 536870912),
              dc(e, t, a === null ? n : a.baseLanes | n, n, r)
            );
        } else
          a === null
            ? (e !== null && Da(t, null), oo(), fo(t))
            : (Da(t, a.cachePool), ao(t, a), fo(t), (t.memoizedState = null));
        return (ac(e, t, i, n), t.child);
      }
      function uc(e, t) {
        return (
          (e !== null && e.tag === 22) ||
            t.stateNode !== null ||
            (t.stateNode = {
              _visibility: 1,
              _pendingMarkers: null,
              _retryCache: null,
              _transitions: null,
            }),
          t.sibling
        );
      }
      function dc(e, t, n, r, i) {
        var a = Ea();
        return (
          (a = a === null ? null : { parent: ha._currentValue, pool: a }),
          (t.memoizedState = { baseLanes: n, cachePool: a }),
          e !== null && Da(t, null),
          oo(),
          uo(t),
          e !== null && oa(e, t, r, !0),
          (t.childLanes = i),
          null
        );
      }
      function fc(e, t) {
        return (
          (t = Tc({ mode: t.mode, children: t.children }, e.mode)),
          (t.ref = e.ref),
          (e.child = t),
          (t.return = e),
          t
        );
      }
      function pc(e, t, n) {
        return (
          Wa(t, e.child, null, n),
          (e = fc(t, t.pendingProps)),
          (e.flags |= 2),
          L(t),
          (t.memoizedState = null),
          e
        );
      }
      function mc(e, t, n) {
        var r = t.pendingProps,
          a = (t.flags & 128) != 0;
        if (((t.flags &= -129), e === null)) {
          if (j) {
            if (r.mode === `hidden`)
              return ((e = fc(t, r)), (t.lanes = 536870912), uc(null, e));
            if (
              (I(t),
              (e = Hi)
                ? ((e = cf(e, Wi)),
                  (e = e !== null && e.data === `&` ? e : null),
                  e !== null &&
                    ((t.memoizedState = {
                      dehydrated: e,
                      treeContext:
                        Ni === null ? null : { id: Pi, overflow: Fi },
                      retryLane: 536870912,
                      hydrationErrors: null,
                    }),
                    (n = Ci(e)),
                    (n.return = t),
                    (t.child = n),
                    (Vi = t),
                    (Hi = null)))
                : (e = null),
              e === null)
            )
              throw Ki(t);
            return ((t.lanes = 536870912), null);
          }
          return fc(t, r);
        }
        var o = e.memoizedState;
        if (o !== null) {
          var s = o.dehydrated;
          if ((I(t), a))
            if (t.flags & 256) ((t.flags &= -257), (t = pc(e, t, n)));
            else if (t.memoizedState !== null)
              ((t.child = e.child), (t.flags |= 128), (t = null));
            else throw Error(i(558));
          else if (
            (ic || oa(e, t, n, !1), (a = (n & e.childLanes) !== 0), ic || a)
          ) {
            if (
              ((r = zl),
              r !== null && ((s = ct(r, n)), s !== 0 && s !== o.retryLane))
            )
              throw ((o.retryLane = s), di(e, s), _u(r, e, s), rc);
            (ku(), (t = pc(e, t, n)));
          } else
            ((e = o.treeContext),
              (Hi = ff(s.nextSibling)),
              (Vi = t),
              (j = !0),
              (Ui = null),
              (Wi = !1),
              e !== null && Bi(t, e),
              (t = fc(t, r)),
              (t.flags |= 4096));
          return t;
        }
        return (
          (e = vi(e.child, { mode: r.mode, children: r.children })),
          (e.ref = t.ref),
          (t.child = e),
          (e.return = t),
          e
        );
      }
      function hc(e, t) {
        var n = t.ref;
        if (n === null) e !== null && e.ref !== null && (t.flags |= 4194816);
        else {
          if (typeof n != `function` && typeof n != `object`)
            throw Error(i(284));
          (e === null || e.ref !== n) && (t.flags |= 4194816);
        }
      }
      function gc(e, t, n, r, i) {
        return (
          ca(t),
          (n = wo(e, t, n, r, void 0, i)),
          (r = Oo()),
          e !== null && !ic
            ? (ko(e, t, i), Ac(e, t, i))
            : (j && r && Ri(t), (t.flags |= 1), ac(e, t, n, i), t.child)
        );
      }
      function _c(e, t, n, r, i, a) {
        return (
          ca(t),
          (t.updateQueue = null),
          (n = Eo(t, r, n, i)),
          To(e),
          (r = Oo()),
          e !== null && !ic
            ? (ko(e, t, a), Ac(e, t, a))
            : (j && r && Ri(t), (t.flags |= 1), ac(e, t, n, a), t.child)
        );
      }
      function vc(e, t, n, r, i) {
        if ((ca(t), t.stateNode === null)) {
          var a = mi,
            o = n.contextType;
          (typeof o == `object` && o && (a = la(o)),
            (a = new n(r, a)),
            (t.memoizedState =
              a.state !== null && a.state !== void 0 ? a.state : null),
            (a.updater = Ws),
            (t.stateNode = a),
            (a._reactInternals = t),
            (a = t.stateNode),
            (a.props = r),
            (a.state = t.memoizedState),
            (a.refs = {}),
            qa(t),
            (o = n.contextType),
            (a.context = typeof o == `object` && o ? la(o) : mi),
            (a.state = t.memoizedState),
            (o = n.getDerivedStateFromProps),
            typeof o == `function` &&
              (Us(t, n, o, r), (a.state = t.memoizedState)),
            typeof n.getDerivedStateFromProps == `function` ||
              typeof a.getSnapshotBeforeUpdate == `function` ||
              (typeof a.UNSAFE_componentWillMount != `function` &&
                typeof a.componentWillMount != `function`) ||
              ((o = a.state),
              typeof a.componentWillMount == `function` &&
                a.componentWillMount(),
              typeof a.UNSAFE_componentWillMount == `function` &&
                a.UNSAFE_componentWillMount(),
              o !== a.state && Ws.enqueueReplaceState(a, a.state, null),
              eo(t, r, a, i),
              $a(),
              (a.state = t.memoizedState)),
            typeof a.componentDidMount == `function` && (t.flags |= 4194308),
            (r = !0));
        } else if (e === null) {
          a = t.stateNode;
          var s = t.memoizedProps,
            c = qs(n, s);
          a.props = c;
          var l = a.context,
            u = n.contextType;
          ((o = mi), typeof u == `object` && u && (o = la(u)));
          var d = n.getDerivedStateFromProps;
          ((u =
            typeof d == `function` ||
            typeof a.getSnapshotBeforeUpdate == `function`),
            (s = t.pendingProps !== s),
            u ||
              (typeof a.UNSAFE_componentWillReceiveProps != `function` &&
                typeof a.componentWillReceiveProps != `function`) ||
              ((s || l !== o) && Ks(t, a, r, o)),
            (Ka = !1));
          var f = t.memoizedState;
          ((a.state = f),
            eo(t, r, a, i),
            $a(),
            (l = t.memoizedState),
            s || f !== l || Ka
              ? (typeof d == `function` &&
                  (Us(t, n, d, r), (l = t.memoizedState)),
                (c = Ka || Gs(t, n, c, r, f, l, o))
                  ? (u ||
                      (typeof a.UNSAFE_componentWillMount != `function` &&
                        typeof a.componentWillMount != `function`) ||
                      (typeof a.componentWillMount == `function` &&
                        a.componentWillMount(),
                      typeof a.UNSAFE_componentWillMount == `function` &&
                        a.UNSAFE_componentWillMount()),
                    typeof a.componentDidMount == `function` &&
                      (t.flags |= 4194308))
                  : (typeof a.componentDidMount == `function` &&
                      (t.flags |= 4194308),
                    (t.memoizedProps = r),
                    (t.memoizedState = l)),
                (a.props = r),
                (a.state = l),
                (a.context = o),
                (r = c))
              : (typeof a.componentDidMount == `function` &&
                  (t.flags |= 4194308),
                (r = !1)));
        } else {
          ((a = t.stateNode),
            Ja(e, t),
            (o = t.memoizedProps),
            (u = qs(n, o)),
            (a.props = u),
            (d = t.pendingProps),
            (f = a.context),
            (l = n.contextType),
            (c = mi),
            typeof l == `object` && l && (c = la(l)),
            (s = n.getDerivedStateFromProps),
            (l =
              typeof s == `function` ||
              typeof a.getSnapshotBeforeUpdate == `function`) ||
              (typeof a.UNSAFE_componentWillReceiveProps != `function` &&
                typeof a.componentWillReceiveProps != `function`) ||
              ((o !== d || f !== c) && Ks(t, a, r, c)),
            (Ka = !1),
            (f = t.memoizedState),
            (a.state = f),
            eo(t, r, a, i),
            $a());
          var p = t.memoizedState;
          o !== d ||
          f !== p ||
          Ka ||
          (e !== null && e.dependencies !== null && sa(e.dependencies))
            ? (typeof s == `function` &&
                (Us(t, n, s, r), (p = t.memoizedState)),
              (u =
                Ka ||
                Gs(t, n, u, r, f, p, c) ||
                (e !== null && e.dependencies !== null && sa(e.dependencies)))
                ? (l ||
                    (typeof a.UNSAFE_componentWillUpdate != `function` &&
                      typeof a.componentWillUpdate != `function`) ||
                    (typeof a.componentWillUpdate == `function` &&
                      a.componentWillUpdate(r, p, c),
                    typeof a.UNSAFE_componentWillUpdate == `function` &&
                      a.UNSAFE_componentWillUpdate(r, p, c)),
                  typeof a.componentDidUpdate == `function` && (t.flags |= 4),
                  typeof a.getSnapshotBeforeUpdate == `function` &&
                    (t.flags |= 1024))
                : (typeof a.componentDidUpdate != `function` ||
                    (o === e.memoizedProps && f === e.memoizedState) ||
                    (t.flags |= 4),
                  typeof a.getSnapshotBeforeUpdate != `function` ||
                    (o === e.memoizedProps && f === e.memoizedState) ||
                    (t.flags |= 1024),
                  (t.memoizedProps = r),
                  (t.memoizedState = p)),
              (a.props = r),
              (a.state = p),
              (a.context = c),
              (r = u))
            : (typeof a.componentDidUpdate != `function` ||
                (o === e.memoizedProps && f === e.memoizedState) ||
                (t.flags |= 4),
              typeof a.getSnapshotBeforeUpdate != `function` ||
                (o === e.memoizedProps && f === e.memoizedState) ||
                (t.flags |= 1024),
              (r = !1));
        }
        return (
          (a = r),
          hc(e, t),
          (r = (t.flags & 128) != 0),
          a || r
            ? ((a = t.stateNode),
              (n =
                r && typeof n.getDerivedStateFromError != `function`
                  ? null
                  : a.render()),
              (t.flags |= 1),
              e !== null && r
                ? ((t.child = Wa(t, e.child, null, i)),
                  (t.child = Wa(t, null, n, i)))
                : ac(e, t, n, i),
              (t.memoizedState = a.state),
              (e = t.child))
            : (e = Ac(e, t, i)),
          e
        );
      }
      function yc(e, t, n, r) {
        return (Xi(), (t.flags |= 256), ac(e, t, n, r), t.child);
      }
      var bc = {
        dehydrated: null,
        treeContext: null,
        retryLane: 0,
        hydrationErrors: null,
      };
      function xc(e) {
        return { baseLanes: e, cachePool: N() };
      }
      function Sc(e, t, n) {
        return ((e = e === null ? 0 : e.childLanes & ~n), t && (e |= Xl), e);
      }
      function Cc(e, t, n) {
        var r = t.pendingProps,
          a = !1,
          o = (t.flags & 128) != 0,
          s;
        if (
          ((s = o) ||
            (s =
              e !== null && e.memoizedState === null
                ? !1
                : (po.current & 2) != 0),
          s && ((a = !0), (t.flags &= -129)),
          (s = (t.flags & 32) != 0),
          (t.flags &= -33),
          e === null)
        ) {
          if (j) {
            if (
              (a ? lo(t) : fo(t),
              (e = Hi)
                ? ((e = cf(e, Wi)),
                  (e = e !== null && e.data !== `&` ? e : null),
                  e !== null &&
                    ((t.memoizedState = {
                      dehydrated: e,
                      treeContext:
                        Ni === null ? null : { id: Pi, overflow: Fi },
                      retryLane: 536870912,
                      hydrationErrors: null,
                    }),
                    (n = Ci(e)),
                    (n.return = t),
                    (t.child = n),
                    (Vi = t),
                    (Hi = null)))
                : (e = null),
              e === null)
            )
              throw Ki(t);
            return (uf(e) ? (t.lanes = 32) : (t.lanes = 536870912), null);
          }
          var c = r.children;
          return (
            (r = r.fallback),
            a
              ? (fo(t),
                (a = t.mode),
                (c = Tc({ mode: `hidden`, children: c }, a)),
                (r = xi(r, a, n, null)),
                (c.return = t),
                (r.return = t),
                (c.sibling = r),
                (t.child = c),
                (r = t.child),
                (r.memoizedState = xc(n)),
                (r.childLanes = Sc(e, s, n)),
                (t.memoizedState = bc),
                uc(null, r))
              : (lo(t), wc(t, c))
          );
        }
        var l = e.memoizedState;
        if (l !== null && ((c = l.dehydrated), c !== null)) {
          if (o)
            t.flags & 256
              ? (lo(t), (t.flags &= -257), (t = Ec(e, t, n)))
              : t.memoizedState === null
                ? (fo(t),
                  (c = r.fallback),
                  (a = t.mode),
                  (r = Tc({ mode: `visible`, children: r.children }, a)),
                  (c = xi(c, a, n, null)),
                  (c.flags |= 2),
                  (r.return = t),
                  (c.return = t),
                  (r.sibling = c),
                  (t.child = r),
                  Wa(t, e.child, null, n),
                  (r = t.child),
                  (r.memoizedState = xc(n)),
                  (r.childLanes = Sc(e, s, n)),
                  (t.memoizedState = bc),
                  (t = uc(null, r)))
                : (fo(t), (t.child = e.child), (t.flags |= 128), (t = null));
          else if ((lo(t), uf(c))) {
            if (((s = c.nextSibling && c.nextSibling.dataset), s))
              var u = s.dgst;
            ((s = u),
              (r = Error(i(419))),
              (r.stack = ``),
              (r.digest = s),
              Qi({ value: r, source: null, stack: null }),
              (t = Ec(e, t, n)));
          } else if (
            (ic || oa(e, t, n, !1), (s = (n & e.childLanes) !== 0), ic || s)
          ) {
            if (
              ((s = zl),
              s !== null && ((r = ct(s, n)), r !== 0 && r !== l.retryLane))
            )
              throw ((l.retryLane = r), di(e, r), _u(s, e, r), rc);
            (lf(c) || ku(), (t = Ec(e, t, n)));
          } else
            lf(c)
              ? ((t.flags |= 192), (t.child = e.child), (t = null))
              : ((e = l.treeContext),
                (Hi = ff(c.nextSibling)),
                (Vi = t),
                (j = !0),
                (Ui = null),
                (Wi = !1),
                e !== null && Bi(t, e),
                (t = wc(t, r.children)),
                (t.flags |= 4096));
          return t;
        }
        return a
          ? (fo(t),
            (c = r.fallback),
            (a = t.mode),
            (l = e.child),
            (u = l.sibling),
            (r = vi(l, { mode: `hidden`, children: r.children })),
            (r.subtreeFlags = l.subtreeFlags & 65011712),
            u === null
              ? ((c = xi(c, a, n, null)), (c.flags |= 2))
              : (c = vi(u, c)),
            (c.return = t),
            (r.return = t),
            (r.sibling = c),
            (t.child = r),
            uc(null, r),
            (r = t.child),
            (c = e.child.memoizedState),
            c === null
              ? (c = xc(n))
              : ((a = c.cachePool),
                a === null
                  ? (a = N())
                  : ((l = ha._currentValue),
                    (a = a.parent === l ? a : { parent: l, pool: l })),
                (c = { baseLanes: c.baseLanes | n, cachePool: a })),
            (r.memoizedState = c),
            (r.childLanes = Sc(e, s, n)),
            (t.memoizedState = bc),
            uc(e.child, r))
          : (lo(t),
            (n = e.child),
            (e = n.sibling),
            (n = vi(n, { mode: `visible`, children: r.children })),
            (n.return = t),
            (n.sibling = null),
            e !== null &&
              ((s = t.deletions),
              s === null ? ((t.deletions = [e]), (t.flags |= 16)) : s.push(e)),
            (t.child = n),
            (t.memoizedState = null),
            n);
      }
      function wc(e, t) {
        return (
          (t = Tc({ mode: `visible`, children: t }, e.mode)),
          (t.return = e),
          (e.child = t)
        );
      }
      function Tc(e, t) {
        return ((e = gi(22, e, null, t)), (e.lanes = 0), e);
      }
      function Ec(e, t, n) {
        return (
          Wa(t, e.child, null, n),
          (e = wc(t, t.pendingProps.children)),
          (e.flags |= 2),
          (t.memoizedState = null),
          e
        );
      }
      function Dc(e, t, n) {
        e.lanes |= t;
        var r = e.alternate;
        (r !== null && (r.lanes |= t), ia(e.return, t, n));
      }
      function Oc(e, t, n, r, i, a) {
        var o = e.memoizedState;
        o === null
          ? (e.memoizedState = {
              isBackwards: t,
              rendering: null,
              renderingStartTime: 0,
              last: r,
              tail: n,
              tailMode: i,
              treeForkCount: a,
            })
          : ((o.isBackwards = t),
            (o.rendering = null),
            (o.renderingStartTime = 0),
            (o.last = r),
            (o.tail = n),
            (o.tailMode = i),
            (o.treeForkCount = a));
      }
      function kc(e, t, n) {
        var r = t.pendingProps,
          i = r.revealOrder,
          a = r.tail;
        r = r.children;
        var o = po.current,
          s = (o & 2) != 0;
        if (
          (s ? ((o = (o & 1) | 2), (t.flags |= 128)) : (o &= 1),
          k(po, o),
          ac(e, t, r, n),
          (r = j ? Ai : 0),
          !s && e !== null && e.flags & 128)
        )
          a: for (e = t.child; e !== null; ) {
            if (e.tag === 13) e.memoizedState !== null && Dc(e, n, t);
            else if (e.tag === 19) Dc(e, n, t);
            else if (e.child !== null) {
              ((e.child.return = e), (e = e.child));
              continue;
            }
            if (e === t) break a;
            for (; e.sibling === null; ) {
              if (e.return === null || e.return === t) break a;
              e = e.return;
            }
            ((e.sibling.return = e.return), (e = e.sibling));
          }
        switch (i) {
          case `forwards`:
            for (n = t.child, i = null; n !== null; )
              ((e = n.alternate),
                e !== null && mo(e) === null && (i = n),
                (n = n.sibling));
            ((n = i),
              n === null
                ? ((i = t.child), (t.child = null))
                : ((i = n.sibling), (n.sibling = null)),
              Oc(t, !1, i, n, a, r));
            break;
          case `backwards`:
          case `unstable_legacy-backwards`:
            for (n = null, i = t.child, t.child = null; i !== null; ) {
              if (((e = i.alternate), e !== null && mo(e) === null)) {
                t.child = i;
                break;
              }
              ((e = i.sibling), (i.sibling = n), (n = i), (i = e));
            }
            Oc(t, !0, n, null, a, r);
            break;
          case `together`:
            Oc(t, !1, null, null, void 0, r);
            break;
          default:
            t.memoizedState = null;
        }
        return t.child;
      }
      function Ac(e, t, n) {
        if (
          (e !== null && (t.dependencies = e.dependencies),
          (ql |= t.lanes),
          (n & t.childLanes) === 0)
        )
          if (e !== null) {
            if ((oa(e, t, n, !1), (n & t.childLanes) === 0)) return null;
          } else return null;
        if (e !== null && t.child !== e.child) throw Error(i(153));
        if (t.child !== null) {
          for (
            e = t.child, n = vi(e, e.pendingProps), t.child = n, n.return = t;
            e.sibling !== null;
          )
            ((e = e.sibling),
              (n = n.sibling = vi(e, e.pendingProps)),
              (n.return = t));
          n.sibling = null;
        }
        return t.child;
      }
      function jc(e, t) {
        return (e.lanes & t) === 0
          ? ((e = e.dependencies), !!(e !== null && sa(e)))
          : !0;
      }
      function Mc(e, t, n) {
        switch (t.tag) {
          case 3:
            (A(t, t.stateNode.containerInfo),
              na(t, ha, e.memoizedState.cache),
              Xi());
            break;
          case 27:
          case 5:
            ye(t);
            break;
          case 4:
            A(t, t.stateNode.containerInfo);
            break;
          case 10:
            na(t, t.type, t.memoizedProps.value);
            break;
          case 31:
            if (t.memoizedState !== null) return ((t.flags |= 128), I(t), null);
            break;
          case 13:
            var r = t.memoizedState;
            if (r !== null)
              return r.dehydrated === null
                ? (n & t.child.childLanes) === 0
                  ? (lo(t), (e = Ac(e, t, n)), e === null ? null : e.sibling)
                  : Cc(e, t, n)
                : (lo(t), (t.flags |= 128), null);
            lo(t);
            break;
          case 19:
            var i = (e.flags & 128) != 0;
            if (
              ((r = (n & t.childLanes) !== 0),
              (r ||= (oa(e, t, n, !1), (n & t.childLanes) !== 0)),
              i)
            ) {
              if (r) return kc(e, t, n);
              t.flags |= 128;
            }
            if (
              ((i = t.memoizedState),
              i !== null &&
                ((i.rendering = null), (i.tail = null), (i.lastEffect = null)),
              k(po, po.current),
              r)
            )
              break;
            return null;
          case 22:
            return ((t.lanes = 0), lc(e, t, n, t.pendingProps));
          case 24:
            na(t, ha, e.memoizedState.cache);
        }
        return Ac(e, t, n);
      }
      function Nc(e, t, n) {
        if (e !== null)
          if (e.memoizedProps !== t.pendingProps) ic = !0;
          else {
            if (!jc(e, n) && !(t.flags & 128)) return ((ic = !1), Mc(e, t, n));
            ic = !!(e.flags & 131072);
          }
        else ((ic = !1), j && t.flags & 1048576 && Li(t, Ai, t.index));
        switch (((t.lanes = 0), t.tag)) {
          case 16:
            a: {
              var r = t.pendingProps;
              if (
                ((e = Pa(t.elementType)), (t.type = e), typeof e == `function`)
              )
                _i(e)
                  ? ((r = qs(e, r)), (t.tag = 1), (t = vc(null, t, e, r, n)))
                  : ((t.tag = 0), (t = gc(null, t, e, r, n)));
              else {
                if (e != null) {
                  var a = e.$$typeof;
                  if (a === C) {
                    ((t.tag = 11), (t = oc(null, t, e, r, n)));
                    break a;
                  } else if (a === ne) {
                    ((t.tag = 14), (t = sc(null, t, e, r, n)));
                    break a;
                  }
                }
                throw ((t = le(e) || e), Error(i(306, t, ``)));
              }
            }
            return t;
          case 0:
            return gc(e, t, t.type, t.pendingProps, n);
          case 1:
            return (
              (r = t.type), (a = qs(r, t.pendingProps)), vc(e, t, r, a, n)
            );
          case 3:
            a: {
              if ((A(t, t.stateNode.containerInfo), e === null))
                throw Error(i(387));
              r = t.pendingProps;
              var o = t.memoizedState;
              ((a = o.element), Ja(e, t), eo(t, r, null, n));
              var s = t.memoizedState;
              if (
                ((r = s.cache),
                na(t, ha, r),
                r !== o.cache && aa(t, [ha], n, !0),
                $a(),
                (r = s.element),
                o.isDehydrated)
              )
                if (
                  ((o = { element: r, isDehydrated: !1, cache: s.cache }),
                  (t.updateQueue.baseState = o),
                  (t.memoizedState = o),
                  t.flags & 256)
                ) {
                  t = yc(e, t, r, n);
                  break a;
                } else if (r !== a) {
                  ((a = Ei(Error(i(424)), t)), Qi(a), (t = yc(e, t, r, n)));
                  break a;
                } else {
                  switch (((e = t.stateNode.containerInfo), e.nodeType)) {
                    case 9:
                      e = e.body;
                      break;
                    default:
                      e = e.nodeName === `HTML` ? e.ownerDocument.body : e;
                  }
                  for (
                    Hi = ff(e.firstChild),
                      Vi = t,
                      j = !0,
                      Ui = null,
                      Wi = !0,
                      n = Ga(t, null, r, n),
                      t.child = n;
                    n;
                  )
                    ((n.flags = (n.flags & -3) | 4096), (n = n.sibling));
                }
              else {
                if ((Xi(), r === a)) {
                  t = Ac(e, t, n);
                  break a;
                }
                ac(e, t, r, n);
              }
              t = t.child;
            }
            return t;
          case 26:
            return (
              hc(e, t),
              e === null
                ? (n = Nf(t.type, null, t.pendingProps, null))
                  ? (t.memoizedState = n)
                  : j ||
                    ((n = t.type),
                    (e = t.pendingProps),
                    (r = Wd(ge.current).createElement(n)),
                    (r[mt] = t),
                    (r[ht] = e),
                    Rd(r, n, e),
                    Dt(r),
                    (t.stateNode = r))
                : (t.memoizedState = Nf(
                    t.type,
                    e.memoizedProps,
                    t.pendingProps,
                    e.memoizedState,
                  )),
              null
            );
          case 27:
            return (
              ye(t),
              e === null &&
                j &&
                ((r = t.stateNode = gf(t.type, t.pendingProps, ge.current)),
                (Vi = t),
                (Wi = !0),
                (a = Hi),
                tf(t.type) ? ((pf = a), (Hi = ff(r.firstChild))) : (Hi = a)),
              ac(e, t, t.pendingProps.children, n),
              hc(e, t),
              e === null && (t.flags |= 4194304),
              t.child
            );
          case 5:
            return (
              e === null &&
                j &&
                ((a = r = Hi) &&
                  ((r = of(r, t.type, t.pendingProps, Wi)),
                  r === null
                    ? (a = !1)
                    : ((t.stateNode = r),
                      (Vi = t),
                      (Hi = ff(r.firstChild)),
                      (Wi = !1),
                      (a = !0))),
                a || Ki(t)),
              ye(t),
              (a = t.type),
              (o = t.pendingProps),
              (s = e === null ? null : e.memoizedProps),
              (r = o.children),
              qd(a, o) ? (r = null) : s !== null && qd(a, s) && (t.flags |= 32),
              t.memoizedState !== null &&
                ((a = wo(e, t, Do, null, null, n)), (np._currentValue = a)),
              hc(e, t),
              ac(e, t, r, n),
              t.child
            );
          case 6:
            return (
              e === null &&
                j &&
                ((e = n = Hi) &&
                  ((n = sf(n, t.pendingProps, Wi)),
                  n === null
                    ? (e = !1)
                    : ((t.stateNode = n), (Vi = t), (Hi = null), (e = !0))),
                e || Ki(t)),
              null
            );
          case 13:
            return Cc(e, t, n);
          case 4:
            return (
              A(t, t.stateNode.containerInfo),
              (r = t.pendingProps),
              e === null ? (t.child = Wa(t, null, r, n)) : ac(e, t, r, n),
              t.child
            );
          case 11:
            return oc(e, t, t.type, t.pendingProps, n);
          case 7:
            return (ac(e, t, t.pendingProps, n), t.child);
          case 8:
            return (ac(e, t, t.pendingProps.children, n), t.child);
          case 12:
            return (ac(e, t, t.pendingProps.children, n), t.child);
          case 10:
            return (
              (r = t.pendingProps),
              na(t, t.type, r.value),
              ac(e, t, r.children, n),
              t.child
            );
          case 9:
            return (
              (a = t.type._context),
              (r = t.pendingProps.children),
              ca(t),
              (a = la(a)),
              (r = r(a)),
              (t.flags |= 1),
              ac(e, t, r, n),
              t.child
            );
          case 14:
            return sc(e, t, t.type, t.pendingProps, n);
          case 15:
            return cc(e, t, t.type, t.pendingProps, n);
          case 19:
            return kc(e, t, n);
          case 31:
            return mc(e, t, n);
          case 22:
            return lc(e, t, n, t.pendingProps);
          case 24:
            return (
              ca(t),
              (r = la(ha)),
              e === null
                ? ((a = Ea()),
                  a === null &&
                    ((a = zl),
                    (o = ga()),
                    (a.pooledCache = o),
                    o.refCount++,
                    o !== null && (a.pooledCacheLanes |= n),
                    (a = o)),
                  (t.memoizedState = { parent: r, cache: a }),
                  qa(t),
                  na(t, ha, a))
                : ((e.lanes & n) !== 0 &&
                    (Ja(e, t), eo(t, null, null, n), $a()),
                  (a = e.memoizedState),
                  (o = t.memoizedState),
                  a.parent === r
                    ? ((r = o.cache),
                      na(t, ha, r),
                      r !== a.cache && aa(t, [ha], n, !0))
                    : ((a = { parent: r, cache: r }),
                      (t.memoizedState = a),
                      t.lanes === 0 &&
                        (t.memoizedState = t.updateQueue.baseState = a),
                      na(t, ha, r))),
              ac(e, t, t.pendingProps.children, n),
              t.child
            );
          case 29:
            throw t.pendingProps;
        }
        throw Error(i(156, t.tag));
      }
      function Pc(e) {
        e.flags |= 4;
      }
      function Fc(e, t, n, r, i) {
        if (((t = (e.mode & 32) != 0) && (t = !1), t)) {
          if (((e.flags |= 16777216), (i & 335544128) === i))
            if (e.stateNode.complete) e.flags |= 8192;
            else if (Eu()) e.flags |= 8192;
            else throw ((Fa = ja), ka);
        } else e.flags &= -16777217;
      }
      function Ic(e, t) {
        if (t.type !== `stylesheet` || t.state.loading & 4)
          e.flags &= -16777217;
        else if (((e.flags |= 16777216), !Jf(t)))
          if (Eu()) e.flags |= 8192;
          else throw ((Fa = ja), ka);
      }
      function Lc(e, t) {
        (t !== null && (e.flags |= 4),
          e.flags & 16384 &&
            ((t = e.tag === 22 ? 536870912 : nt()), (e.lanes |= t), (Zl |= t)));
      }
      function Rc(e, t) {
        if (!j)
          switch (e.tailMode) {
            case `hidden`:
              t = e.tail;
              for (var n = null; t !== null; )
                (t.alternate !== null && (n = t), (t = t.sibling));
              n === null ? (e.tail = null) : (n.sibling = null);
              break;
            case `collapsed`:
              n = e.tail;
              for (var r = null; n !== null; )
                (n.alternate !== null && (r = n), (n = n.sibling));
              r === null
                ? t || e.tail === null
                  ? (e.tail = null)
                  : (e.tail.sibling = null)
                : (r.sibling = null);
          }
      }
      function zc(e) {
        var t = e.alternate !== null && e.alternate.child === e.child,
          n = 0,
          r = 0;
        if (t)
          for (var i = e.child; i !== null; )
            ((n |= i.lanes | i.childLanes),
              (r |= i.subtreeFlags & 65011712),
              (r |= i.flags & 65011712),
              (i.return = e),
              (i = i.sibling));
        else
          for (i = e.child; i !== null; )
            ((n |= i.lanes | i.childLanes),
              (r |= i.subtreeFlags),
              (r |= i.flags),
              (i.return = e),
              (i = i.sibling));
        return ((e.subtreeFlags |= r), (e.childLanes = n), t);
      }
      function Bc(e, t, n) {
        var r = t.pendingProps;
        switch ((zi(t), t.tag)) {
          case 16:
          case 15:
          case 0:
          case 11:
          case 7:
          case 8:
          case 12:
          case 9:
          case 14:
            return (zc(t), null);
          case 1:
            return (zc(t), null);
          case 3:
            return (
              (n = t.stateNode),
              (r = null),
              e !== null && (r = e.memoizedState.cache),
              t.memoizedState.cache !== r && (t.flags |= 2048),
              ra(ha),
              ve(),
              n.pendingContext &&
                ((n.context = n.pendingContext), (n.pendingContext = null)),
              (e === null || e.child === null) &&
                (Yi(t)
                  ? Pc(t)
                  : e === null ||
                    (e.memoizedState.isDehydrated && !(t.flags & 256)) ||
                    ((t.flags |= 1024), Zi())),
              zc(t),
              null
            );
          case 26:
            var a = t.type,
              o = t.memoizedState;
            return (
              e === null
                ? (Pc(t),
                  o === null
                    ? (zc(t), Fc(t, a, null, r, n))
                    : (zc(t), Ic(t, o)))
                : o
                  ? o === e.memoizedState
                    ? (zc(t), (t.flags &= -16777217))
                    : (Pc(t), zc(t), Ic(t, o))
                  : ((e = e.memoizedProps),
                    e !== r && Pc(t),
                    zc(t),
                    Fc(t, a, e, r, n)),
              null
            );
          case 27:
            if (
              (be(t),
              (n = ge.current),
              (a = t.type),
              e !== null && t.stateNode != null)
            )
              e.memoizedProps !== r && Pc(t);
            else {
              if (!r) {
                if (t.stateNode === null) throw Error(i(166));
                return (zc(t), null);
              }
              ((e = me.current),
                Yi(t)
                  ? qi(t, e)
                  : ((e = gf(a, r, n)), (t.stateNode = e), Pc(t)));
            }
            return (zc(t), null);
          case 5:
            if ((be(t), (a = t.type), e !== null && t.stateNode != null))
              e.memoizedProps !== r && Pc(t);
            else {
              if (!r) {
                if (t.stateNode === null) throw Error(i(166));
                return (zc(t), null);
              }
              if (((o = me.current), Yi(t))) qi(t, o);
              else {
                var s = Wd(ge.current);
                switch (o) {
                  case 1:
                    o = s.createElementNS(`http://www.w3.org/2000/svg`, a);
                    break;
                  case 2:
                    o = s.createElementNS(
                      `http://www.w3.org/1998/Math/MathML`,
                      a,
                    );
                    break;
                  default:
                    switch (a) {
                      case `svg`:
                        o = s.createElementNS(`http://www.w3.org/2000/svg`, a);
                        break;
                      case `math`:
                        o = s.createElementNS(
                          `http://www.w3.org/1998/Math/MathML`,
                          a,
                        );
                        break;
                      case `script`:
                        ((o = s.createElement(`div`)),
                          (o.innerHTML = `<script><\/script>`),
                          (o = o.removeChild(o.firstChild)));
                        break;
                      case `select`:
                        ((o =
                          typeof r.is == `string`
                            ? s.createElement(`select`, { is: r.is })
                            : s.createElement(`select`)),
                          r.multiple
                            ? (o.multiple = !0)
                            : r.size && (o.size = r.size));
                        break;
                      default:
                        o =
                          typeof r.is == `string`
                            ? s.createElement(a, { is: r.is })
                            : s.createElement(a);
                    }
                }
                ((o[mt] = t), (o[ht] = r));
                a: for (s = t.child; s !== null; ) {
                  if (s.tag === 5 || s.tag === 6) o.appendChild(s.stateNode);
                  else if (s.tag !== 4 && s.tag !== 27 && s.child !== null) {
                    ((s.child.return = s), (s = s.child));
                    continue;
                  }
                  if (s === t) break a;
                  for (; s.sibling === null; ) {
                    if (s.return === null || s.return === t) break a;
                    s = s.return;
                  }
                  ((s.sibling.return = s.return), (s = s.sibling));
                }
                t.stateNode = o;
                a: switch ((Rd(o, a, r), a)) {
                  case `button`:
                  case `input`:
                  case `select`:
                  case `textarea`:
                    r = !!r.autoFocus;
                    break a;
                  case `img`:
                    r = !0;
                    break a;
                  default:
                    r = !1;
                }
                r && Pc(t);
              }
            }
            return (
              zc(t),
              Fc(
                t,
                t.type,
                e === null ? null : e.memoizedProps,
                t.pendingProps,
                n,
              ),
              null
            );
          case 6:
            if (e && t.stateNode != null) e.memoizedProps !== r && Pc(t);
            else {
              if (typeof r != `string` && t.stateNode === null)
                throw Error(i(166));
              if (((e = ge.current), Yi(t))) {
                if (
                  ((e = t.stateNode),
                  (n = t.memoizedProps),
                  (r = null),
                  (a = Vi),
                  a !== null)
                )
                  switch (a.tag) {
                    case 27:
                    case 5:
                      r = a.memoizedProps;
                  }
                ((e[mt] = t),
                  (e = !!(
                    e.nodeValue === n ||
                    (r !== null && !0 === r.suppressHydrationWarning) ||
                    Fd(e.nodeValue, n)
                  )),
                  e || Ki(t, !0));
              } else
                ((e = Wd(e).createTextNode(r)), (e[mt] = t), (t.stateNode = e));
            }
            return (zc(t), null);
          case 31:
            if (
              ((n = t.memoizedState), e === null || e.memoizedState !== null)
            ) {
              if (((r = Yi(t)), n !== null)) {
                if (e === null) {
                  if (!r) throw Error(i(318));
                  if (
                    ((e = t.memoizedState),
                    (e = e === null ? null : e.dehydrated),
                    !e)
                  )
                    throw Error(i(557));
                  e[mt] = t;
                } else
                  (Xi(),
                    !(t.flags & 128) && (t.memoizedState = null),
                    (t.flags |= 4));
                (zc(t), (e = !1));
              } else
                ((n = Zi()),
                  e !== null &&
                    e.memoizedState !== null &&
                    (e.memoizedState.hydrationErrors = n),
                  (e = !0));
              if (!e) return t.flags & 256 ? (L(t), t) : (L(t), null);
              if (t.flags & 128) throw Error(i(558));
            }
            return (zc(t), null);
          case 13:
            if (
              ((r = t.memoizedState),
              e === null ||
                (e.memoizedState !== null &&
                  e.memoizedState.dehydrated !== null))
            ) {
              if (((a = Yi(t)), r !== null && r.dehydrated !== null)) {
                if (e === null) {
                  if (!a) throw Error(i(318));
                  if (
                    ((a = t.memoizedState),
                    (a = a === null ? null : a.dehydrated),
                    !a)
                  )
                    throw Error(i(317));
                  a[mt] = t;
                } else
                  (Xi(),
                    !(t.flags & 128) && (t.memoizedState = null),
                    (t.flags |= 4));
                (zc(t), (a = !1));
              } else
                ((a = Zi()),
                  e !== null &&
                    e.memoizedState !== null &&
                    (e.memoizedState.hydrationErrors = a),
                  (a = !0));
              if (!a) return t.flags & 256 ? (L(t), t) : (L(t), null);
            }
            return (
              L(t),
              t.flags & 128
                ? ((t.lanes = n), t)
                : ((n = r !== null),
                  (e = e !== null && e.memoizedState !== null),
                  n &&
                    ((r = t.child),
                    (a = null),
                    r.alternate !== null &&
                      r.alternate.memoizedState !== null &&
                      r.alternate.memoizedState.cachePool !== null &&
                      (a = r.alternate.memoizedState.cachePool.pool),
                    (o = null),
                    r.memoizedState !== null &&
                      r.memoizedState.cachePool !== null &&
                      (o = r.memoizedState.cachePool.pool),
                    o !== a && (r.flags |= 2048)),
                  n !== e && n && (t.child.flags |= 8192),
                  Lc(t, t.updateQueue),
                  zc(t),
                  null)
            );
          case 4:
            return (
              ve(), e === null && Td(t.stateNode.containerInfo), zc(t), null
            );
          case 10:
            return (ra(t.type), zc(t), null);
          case 19:
            if ((O(po), (r = t.memoizedState), r === null))
              return (zc(t), null);
            if (((a = (t.flags & 128) != 0), (o = r.rendering), o === null))
              if (a) Rc(r, !1);
              else {
                if (Kl !== 0 || (e !== null && e.flags & 128))
                  for (e = t.child; e !== null; ) {
                    if (((o = mo(e)), o !== null)) {
                      for (
                        t.flags |= 128,
                          Rc(r, !1),
                          e = o.updateQueue,
                          t.updateQueue = e,
                          Lc(t, e),
                          t.subtreeFlags = 0,
                          e = n,
                          n = t.child;
                        n !== null;
                      )
                        (yi(n, e), (n = n.sibling));
                      return (
                        k(po, (po.current & 1) | 2),
                        j && Ii(t, r.treeForkCount),
                        t.child
                      );
                    }
                    e = e.sibling;
                  }
                r.tail !== null &&
                  Ne() > ru &&
                  ((t.flags |= 128), (a = !0), Rc(r, !1), (t.lanes = 4194304));
              }
            else {
              if (!a)
                if (((e = mo(o)), e !== null)) {
                  if (
                    ((t.flags |= 128),
                    (a = !0),
                    (e = e.updateQueue),
                    (t.updateQueue = e),
                    Lc(t, e),
                    Rc(r, !0),
                    r.tail === null &&
                      r.tailMode === `hidden` &&
                      !o.alternate &&
                      !j)
                  )
                    return (zc(t), null);
                } else
                  2 * Ne() - r.renderingStartTime > ru &&
                    n !== 536870912 &&
                    ((t.flags |= 128),
                    (a = !0),
                    Rc(r, !1),
                    (t.lanes = 4194304));
              r.isBackwards
                ? ((o.sibling = t.child), (t.child = o))
                : ((e = r.last),
                  e === null ? (t.child = o) : (e.sibling = o),
                  (r.last = o));
            }
            return r.tail === null
              ? (zc(t), null)
              : ((e = r.tail),
                (r.rendering = e),
                (r.tail = e.sibling),
                (r.renderingStartTime = Ne()),
                (e.sibling = null),
                (n = po.current),
                k(po, a ? (n & 1) | 2 : n & 1),
                j && Ii(t, r.treeForkCount),
                e);
          case 22:
          case 23:
            return (
              L(t),
              so(),
              (r = t.memoizedState !== null),
              e === null
                ? r && (t.flags |= 8192)
                : (e.memoizedState !== null) !== r && (t.flags |= 8192),
              r
                ? n & 536870912 &&
                  !(t.flags & 128) &&
                  (zc(t), t.subtreeFlags & 6 && (t.flags |= 8192))
                : zc(t),
              (n = t.updateQueue),
              n !== null && Lc(t, n.retryQueue),
              (n = null),
              e !== null &&
                e.memoizedState !== null &&
                e.memoizedState.cachePool !== null &&
                (n = e.memoizedState.cachePool.pool),
              (r = null),
              t.memoizedState !== null &&
                t.memoizedState.cachePool !== null &&
                (r = t.memoizedState.cachePool.pool),
              r !== n && (t.flags |= 2048),
              e !== null && O(M),
              null
            );
          case 24:
            return (
              (n = null),
              e !== null && (n = e.memoizedState.cache),
              t.memoizedState.cache !== n && (t.flags |= 2048),
              ra(ha),
              zc(t),
              null
            );
          case 25:
            return null;
          case 30:
            return null;
        }
        throw Error(i(156, t.tag));
      }
      function Vc(e, t) {
        switch ((zi(t), t.tag)) {
          case 1:
            return (
              (e = t.flags),
              e & 65536 ? ((t.flags = (e & -65537) | 128), t) : null
            );
          case 3:
            return (
              ra(ha),
              ve(),
              (e = t.flags),
              e & 65536 && !(e & 128)
                ? ((t.flags = (e & -65537) | 128), t)
                : null
            );
          case 26:
          case 27:
          case 5:
            return (be(t), null);
          case 31:
            if (t.memoizedState !== null) {
              if ((L(t), t.alternate === null)) throw Error(i(340));
              Xi();
            }
            return (
              (e = t.flags),
              e & 65536 ? ((t.flags = (e & -65537) | 128), t) : null
            );
          case 13:
            if (
              (L(t), (e = t.memoizedState), e !== null && e.dehydrated !== null)
            ) {
              if (t.alternate === null) throw Error(i(340));
              Xi();
            }
            return (
              (e = t.flags),
              e & 65536 ? ((t.flags = (e & -65537) | 128), t) : null
            );
          case 19:
            return (O(po), null);
          case 4:
            return (ve(), null);
          case 10:
            return (ra(t.type), null);
          case 22:
          case 23:
            return (
              L(t),
              so(),
              e !== null && O(M),
              (e = t.flags),
              e & 65536 ? ((t.flags = (e & -65537) | 128), t) : null
            );
          case 24:
            return (ra(ha), null);
          case 25:
            return null;
          default:
            return null;
        }
      }
      function Hc(e, t) {
        switch ((zi(t), t.tag)) {
          case 3:
            (ra(ha), ve());
            break;
          case 26:
          case 27:
          case 5:
            be(t);
            break;
          case 4:
            ve();
            break;
          case 31:
            t.memoizedState !== null && L(t);
            break;
          case 13:
            L(t);
            break;
          case 19:
            O(po);
            break;
          case 10:
            ra(t.type);
            break;
          case 22:
          case 23:
            (L(t), so(), e !== null && O(M));
            break;
          case 24:
            ra(ha);
        }
      }
      function Uc(e, t) {
        try {
          var n = t.updateQueue,
            r = n === null ? null : n.lastEffect;
          if (r !== null) {
            var i = r.next;
            n = i;
            do {
              if ((n.tag & e) === e) {
                r = void 0;
                var a = n.create,
                  o = n.inst;
                ((r = a()), (o.destroy = r));
              }
              n = n.next;
            } while (n !== i);
          }
        } catch (e) {
          qu(t, t.return, e);
        }
      }
      function Wc(e, t, n) {
        try {
          var r = t.updateQueue,
            i = r === null ? null : r.lastEffect;
          if (i !== null) {
            var a = i.next;
            r = a;
            do {
              if ((r.tag & e) === e) {
                var o = r.inst,
                  s = o.destroy;
                if (s !== void 0) {
                  ((o.destroy = void 0), (i = t));
                  var c = n,
                    l = s;
                  try {
                    l();
                  } catch (e) {
                    qu(i, c, e);
                  }
                }
              }
              r = r.next;
            } while (r !== a);
          }
        } catch (e) {
          qu(t, t.return, e);
        }
      }
      function Gc(e) {
        var t = e.updateQueue;
        if (t !== null) {
          var n = e.stateNode;
          try {
            no(t, n);
          } catch (t) {
            qu(e, e.return, t);
          }
        }
      }
      function Kc(e, t, n) {
        ((n.props = qs(e.type, e.memoizedProps)), (n.state = e.memoizedState));
        try {
          n.componentWillUnmount();
        } catch (n) {
          qu(e, t, n);
        }
      }
      function qc(e, t) {
        try {
          var n = e.ref;
          if (n !== null) {
            switch (e.tag) {
              case 26:
              case 27:
              case 5:
                var r = e.stateNode;
                break;
              case 30:
                r = e.stateNode;
                break;
              default:
                r = e.stateNode;
            }
            typeof n == `function` ? (e.refCleanup = n(r)) : (n.current = r);
          }
        } catch (n) {
          qu(e, t, n);
        }
      }
      function Jc(e, t) {
        var n = e.ref,
          r = e.refCleanup;
        if (n !== null)
          if (typeof r == `function`)
            try {
              r();
            } catch (n) {
              qu(e, t, n);
            } finally {
              ((e.refCleanup = null),
                (e = e.alternate),
                e != null && (e.refCleanup = null));
            }
          else if (typeof n == `function`)
            try {
              n(null);
            } catch (n) {
              qu(e, t, n);
            }
          else n.current = null;
      }
      function Yc(e) {
        var t = e.type,
          n = e.memoizedProps,
          r = e.stateNode;
        try {
          a: switch (t) {
            case `button`:
            case `input`:
            case `select`:
            case `textarea`:
              n.autoFocus && r.focus();
              break a;
            case `img`:
              n.src ? (r.src = n.src) : n.srcSet && (r.srcset = n.srcSet);
          }
        } catch (t) {
          qu(e, e.return, t);
        }
      }
      function Xc(e, t, n) {
        try {
          var r = e.stateNode;
          (zd(r, e.type, n, t), (r[ht] = t));
        } catch (t) {
          qu(e, e.return, t);
        }
      }
      function Zc(e) {
        return (
          e.tag === 5 ||
          e.tag === 3 ||
          e.tag === 26 ||
          (e.tag === 27 && tf(e.type)) ||
          e.tag === 4
        );
      }
      function Qc(e) {
        a: for (;;) {
          for (; e.sibling === null; ) {
            if (e.return === null || Zc(e.return)) return null;
            e = e.return;
          }
          for (
            e.sibling.return = e.return, e = e.sibling;
            e.tag !== 5 && e.tag !== 6 && e.tag !== 18;
          ) {
            if (
              (e.tag === 27 && tf(e.type)) ||
              e.flags & 2 ||
              e.child === null ||
              e.tag === 4
            )
              continue a;
            ((e.child.return = e), (e = e.child));
          }
          if (!(e.flags & 2)) return e.stateNode;
        }
      }
      function $c(e, t, n) {
        var r = e.tag;
        if (r === 5 || r === 6)
          ((e = e.stateNode),
            t
              ? (n.nodeType === 9
                  ? n.body
                  : n.nodeName === `HTML`
                    ? n.ownerDocument.body
                    : n
                ).insertBefore(e, t)
              : ((t =
                  n.nodeType === 9
                    ? n.body
                    : n.nodeName === `HTML`
                      ? n.ownerDocument.body
                      : n),
                t.appendChild(e),
                (n = n._reactRootContainer),
                n != null || t.onclick !== null || (t.onclick = cn)));
        else if (
          r !== 4 &&
          (r === 27 && tf(e.type) && ((n = e.stateNode), (t = null)),
          (e = e.child),
          e !== null)
        )
          for ($c(e, t, n), e = e.sibling; e !== null; )
            ($c(e, t, n), (e = e.sibling));
      }
      function el(e, t, n) {
        var r = e.tag;
        if (r === 5 || r === 6)
          ((e = e.stateNode), t ? n.insertBefore(e, t) : n.appendChild(e));
        else if (
          r !== 4 &&
          (r === 27 && tf(e.type) && (n = e.stateNode),
          (e = e.child),
          e !== null)
        )
          for (el(e, t, n), e = e.sibling; e !== null; )
            (el(e, t, n), (e = e.sibling));
      }
      function tl(e) {
        var t = e.stateNode,
          n = e.memoizedProps;
        try {
          for (var r = e.type, i = t.attributes; i.length; )
            t.removeAttributeNode(i[0]);
          (Rd(t, r, n), (t[mt] = e), (t[ht] = n));
        } catch (t) {
          qu(e, e.return, t);
        }
      }
      var nl = !1,
        rl = !1,
        il = !1,
        al = typeof WeakSet == `function` ? WeakSet : Set,
        ol = null;
      function sl(e, t) {
        if (((e = e.containerInfo), (Hd = dp), (e = Fr(e)), Ir(e))) {
          if (`selectionStart` in e)
            var n = { start: e.selectionStart, end: e.selectionEnd };
          else
            a: {
              n = ((n = e.ownerDocument) && n.defaultView) || window;
              var r = n.getSelection && n.getSelection();
              if (r && r.rangeCount !== 0) {
                n = r.anchorNode;
                var a = r.anchorOffset,
                  o = r.focusNode;
                r = r.focusOffset;
                try {
                  (n.nodeType, o.nodeType);
                } catch {
                  n = null;
                  break a;
                }
                var s = 0,
                  c = -1,
                  l = -1,
                  u = 0,
                  d = 0,
                  f = e,
                  p = null;
                b: for (;;) {
                  for (
                    var m;
                    f !== n || (a !== 0 && f.nodeType !== 3) || (c = s + a),
                      f !== o || (r !== 0 && f.nodeType !== 3) || (l = s + r),
                      f.nodeType === 3 && (s += f.nodeValue.length),
                      (m = f.firstChild) !== null;
                  )
                    ((p = f), (f = m));
                  for (;;) {
                    if (f === e) break b;
                    if (
                      (p === n && ++u === a && (c = s),
                      p === o && ++d === r && (l = s),
                      (m = f.nextSibling) !== null)
                    )
                      break;
                    ((f = p), (p = f.parentNode));
                  }
                  f = m;
                }
                n = c === -1 || l === -1 ? null : { start: c, end: l };
              } else n = null;
            }
          n ||= { start: 0, end: 0 };
        } else n = null;
        for (
          Ud = { focusedElem: e, selectionRange: n }, dp = !1, ol = t;
          ol !== null;
        )
          if (((t = ol), (e = t.child), t.subtreeFlags & 1028 && e !== null))
            ((e.return = t), (ol = e));
          else
            for (; ol !== null; ) {
              switch (((t = ol), (o = t.alternate), (e = t.flags), t.tag)) {
                case 0:
                  if (
                    e & 4 &&
                    ((e = t.updateQueue),
                    (e = e === null ? null : e.events),
                    e !== null)
                  )
                    for (n = 0; n < e.length; n++)
                      ((a = e[n]), (a.ref.impl = a.nextImpl));
                  break;
                case 11:
                case 15:
                  break;
                case 1:
                  if (e & 1024 && o !== null) {
                    ((e = void 0),
                      (n = t),
                      (a = o.memoizedProps),
                      (o = o.memoizedState),
                      (r = n.stateNode));
                    try {
                      var h = qs(n.type, a);
                      ((e = r.getSnapshotBeforeUpdate(h, o)),
                        (r.__reactInternalSnapshotBeforeUpdate = e));
                    } catch (e) {
                      qu(n, n.return, e);
                    }
                  }
                  break;
                case 3:
                  if (e & 1024) {
                    if (
                      ((e = t.stateNode.containerInfo),
                      (n = e.nodeType),
                      n === 9)
                    )
                      af(e);
                    else if (n === 1)
                      switch (e.nodeName) {
                        case `HEAD`:
                        case `HTML`:
                        case `BODY`:
                          af(e);
                          break;
                        default:
                          e.textContent = ``;
                      }
                  }
                  break;
                case 5:
                case 26:
                case 27:
                case 6:
                case 4:
                case 17:
                  break;
                default:
                  if (e & 1024) throw Error(i(163));
              }
              if (((e = t.sibling), e !== null)) {
                ((e.return = t.return), (ol = e));
                break;
              }
              ol = t.return;
            }
      }
      function cl(e, t, n) {
        var r = n.flags;
        switch (n.tag) {
          case 0:
          case 11:
          case 15:
            (Cl(e, n), r & 4 && Uc(5, n));
            break;
          case 1:
            if ((Cl(e, n), r & 4))
              if (((e = n.stateNode), t === null))
                try {
                  e.componentDidMount();
                } catch (e) {
                  qu(n, n.return, e);
                }
              else {
                var i = qs(n.type, t.memoizedProps);
                t = t.memoizedState;
                try {
                  e.componentDidUpdate(
                    i,
                    t,
                    e.__reactInternalSnapshotBeforeUpdate,
                  );
                } catch (e) {
                  qu(n, n.return, e);
                }
              }
            (r & 64 && Gc(n), r & 512 && qc(n, n.return));
            break;
          case 3:
            if ((Cl(e, n), r & 64 && ((e = n.updateQueue), e !== null))) {
              if (((t = null), n.child !== null))
                switch (n.child.tag) {
                  case 27:
                  case 5:
                    t = n.child.stateNode;
                    break;
                  case 1:
                    t = n.child.stateNode;
                }
              try {
                no(e, t);
              } catch (e) {
                qu(n, n.return, e);
              }
            }
            break;
          case 27:
            t === null && r & 4 && tl(n);
          case 26:
          case 5:
            (Cl(e, n),
              t === null && r & 4 && Yc(n),
              r & 512 && qc(n, n.return));
            break;
          case 12:
            Cl(e, n);
            break;
          case 31:
            (Cl(e, n), r & 4 && ml(e, n));
            break;
          case 13:
            (Cl(e, n),
              r & 4 && hl(e, n),
              r & 64 &&
                ((e = n.memoizedState),
                e !== null &&
                  ((e = e.dehydrated),
                  e !== null && ((n = Zu.bind(null, n)), df(e, n)))));
            break;
          case 22:
            if (((r = n.memoizedState !== null || nl), !r)) {
              ((t = (t !== null && t.memoizedState !== null) || rl), (i = nl));
              var a = rl;
              ((nl = r),
                (rl = t) && !a
                  ? Tl(e, n, (n.subtreeFlags & 8772) != 0)
                  : Cl(e, n),
                (nl = i),
                (rl = a));
            }
            break;
          case 30:
            break;
          default:
            Cl(e, n);
        }
      }
      function ll(e) {
        var t = e.alternate;
        (t !== null && ((e.alternate = null), ll(t)),
          (e.child = null),
          (e.deletions = null),
          (e.sibling = null),
          e.tag === 5 && ((t = e.stateNode), t !== null && St(t)),
          (e.stateNode = null),
          (e.return = null),
          (e.dependencies = null),
          (e.memoizedProps = null),
          (e.memoizedState = null),
          (e.pendingProps = null),
          (e.stateNode = null),
          (e.updateQueue = null));
      }
      var ul = null,
        dl = !1;
      function fl(e, t, n) {
        for (n = n.child; n !== null; ) (pl(e, t, n), (n = n.sibling));
      }
      function pl(e, t, n) {
        if (Ue && typeof Ue.onCommitFiberUnmount == `function`)
          try {
            Ue.onCommitFiberUnmount(He, n);
          } catch {}
        switch (n.tag) {
          case 26:
            (rl || Jc(n, t),
              fl(e, t, n),
              n.memoizedState
                ? n.memoizedState.count--
                : n.stateNode &&
                  ((n = n.stateNode), n.parentNode.removeChild(n)));
            break;
          case 27:
            rl || Jc(n, t);
            var r = ul,
              i = dl;
            (tf(n.type) && ((ul = n.stateNode), (dl = !1)),
              fl(e, t, n),
              _f(n.stateNode),
              (ul = r),
              (dl = i));
            break;
          case 5:
            rl || Jc(n, t);
          case 6:
            if (
              ((r = ul),
              (i = dl),
              (ul = null),
              fl(e, t, n),
              (ul = r),
              (dl = i),
              ul !== null)
            )
              if (dl)
                try {
                  (ul.nodeType === 9
                    ? ul.body
                    : ul.nodeName === `HTML`
                      ? ul.ownerDocument.body
                      : ul
                  ).removeChild(n.stateNode);
                } catch (e) {
                  qu(n, t, e);
                }
              else
                try {
                  ul.removeChild(n.stateNode);
                } catch (e) {
                  qu(n, t, e);
                }
            break;
          case 18:
            ul !== null &&
              (dl
                ? ((e = ul),
                  nf(
                    e.nodeType === 9
                      ? e.body
                      : e.nodeName === `HTML`
                        ? e.ownerDocument.body
                        : e,
                    n.stateNode,
                  ),
                  Lp(e))
                : nf(ul, n.stateNode));
            break;
          case 4:
            ((r = ul),
              (i = dl),
              (ul = n.stateNode.containerInfo),
              (dl = !0),
              fl(e, t, n),
              (ul = r),
              (dl = i));
            break;
          case 0:
          case 11:
          case 14:
          case 15:
            (Wc(2, n, t), rl || Wc(4, n, t), fl(e, t, n));
            break;
          case 1:
            (rl ||
              (Jc(n, t),
              (r = n.stateNode),
              typeof r.componentWillUnmount == `function` && Kc(n, t, r)),
              fl(e, t, n));
            break;
          case 21:
            fl(e, t, n);
            break;
          case 22:
            ((rl = (r = rl) || n.memoizedState !== null),
              fl(e, t, n),
              (rl = r));
            break;
          default:
            fl(e, t, n);
        }
      }
      function ml(e, t) {
        if (
          t.memoizedState === null &&
          ((e = t.alternate), e !== null && ((e = e.memoizedState), e !== null))
        ) {
          e = e.dehydrated;
          try {
            Lp(e);
          } catch (e) {
            qu(t, t.return, e);
          }
        }
      }
      function hl(e, t) {
        if (
          t.memoizedState === null &&
          ((e = t.alternate),
          e !== null &&
            ((e = e.memoizedState),
            e !== null && ((e = e.dehydrated), e !== null)))
        )
          try {
            Lp(e);
          } catch (e) {
            qu(t, t.return, e);
          }
      }
      function gl(e) {
        switch (e.tag) {
          case 31:
          case 13:
          case 19:
            var t = e.stateNode;
            return (t === null && (t = e.stateNode = new al()), t);
          case 22:
            return (
              (e = e.stateNode),
              (t = e._retryCache),
              t === null && (t = e._retryCache = new al()),
              t
            );
          default:
            throw Error(i(435, e.tag));
        }
      }
      function _l(e, t) {
        var n = gl(e);
        t.forEach(function (t) {
          if (!n.has(t)) {
            n.add(t);
            var r = Qu.bind(null, e, t);
            t.then(r, r);
          }
        });
      }
      function vl(e, t) {
        var n = t.deletions;
        if (n !== null)
          for (var r = 0; r < n.length; r++) {
            var a = n[r],
              o = e,
              s = t,
              c = s;
            a: for (; c !== null; ) {
              switch (c.tag) {
                case 27:
                  if (tf(c.type)) {
                    ((ul = c.stateNode), (dl = !1));
                    break a;
                  }
                  break;
                case 5:
                  ((ul = c.stateNode), (dl = !1));
                  break a;
                case 3:
                case 4:
                  ((ul = c.stateNode.containerInfo), (dl = !0));
                  break a;
              }
              c = c.return;
            }
            if (ul === null) throw Error(i(160));
            (pl(o, s, a),
              (ul = null),
              (dl = !1),
              (o = a.alternate),
              o !== null && (o.return = null),
              (a.return = null));
          }
        if (t.subtreeFlags & 13886)
          for (t = t.child; t !== null; ) (bl(t, e), (t = t.sibling));
      }
      var yl = null;
      function bl(e, t) {
        var n = e.alternate,
          r = e.flags;
        switch (e.tag) {
          case 0:
          case 11:
          case 14:
          case 15:
            (vl(t, e),
              xl(e),
              r & 4 && (Wc(3, e, e.return), Uc(3, e), Wc(5, e, e.return)));
            break;
          case 1:
            (vl(t, e),
              xl(e),
              r & 512 && (rl || n === null || Jc(n, n.return)),
              r & 64 &&
                nl &&
                ((e = e.updateQueue),
                e !== null &&
                  ((r = e.callbacks),
                  r !== null &&
                    ((n = e.shared.hiddenCallbacks),
                    (e.shared.hiddenCallbacks =
                      n === null ? r : n.concat(r))))));
            break;
          case 26:
            var a = yl;
            if (
              (vl(t, e),
              xl(e),
              r & 512 && (rl || n === null || Jc(n, n.return)),
              r & 4)
            ) {
              var o = n === null ? null : n.memoizedState;
              if (((r = e.memoizedState), n === null))
                if (r === null)
                  if (e.stateNode === null) {
                    a: {
                      ((r = e.type),
                        (n = e.memoizedProps),
                        (a = a.ownerDocument || a));
                      b: switch (r) {
                        case `title`:
                          ((o = a.getElementsByTagName(`title`)[0]),
                            (!o ||
                              o[xt] ||
                              o[mt] ||
                              o.namespaceURI === `http://www.w3.org/2000/svg` ||
                              o.hasAttribute(`itemprop`)) &&
                              ((o = a.createElement(r)),
                              a.head.insertBefore(
                                o,
                                a.querySelector(`head > title`),
                              )),
                            Rd(o, r, n),
                            (o[mt] = e),
                            Dt(o),
                            (r = o));
                          break a;
                        case `link`:
                          var s = Gf(`link`, `href`, a).get(r + (n.href || ``));
                          if (s) {
                            for (var c = 0; c < s.length; c++)
                              if (
                                ((o = s[c]),
                                o.getAttribute(`href`) ===
                                  (n.href == null || n.href === ``
                                    ? null
                                    : n.href) &&
                                  o.getAttribute(`rel`) ===
                                    (n.rel == null ? null : n.rel) &&
                                  o.getAttribute(`title`) ===
                                    (n.title == null ? null : n.title) &&
                                  o.getAttribute(`crossorigin`) ===
                                    (n.crossOrigin == null
                                      ? null
                                      : n.crossOrigin))
                              ) {
                                s.splice(c, 1);
                                break b;
                              }
                          }
                          ((o = a.createElement(r)),
                            Rd(o, r, n),
                            a.head.appendChild(o));
                          break;
                        case `meta`:
                          if (
                            (s = Gf(`meta`, `content`, a).get(
                              r + (n.content || ``),
                            ))
                          ) {
                            for (c = 0; c < s.length; c++)
                              if (
                                ((o = s[c]),
                                o.getAttribute(`content`) ===
                                  (n.content == null ? null : `` + n.content) &&
                                  o.getAttribute(`name`) ===
                                    (n.name == null ? null : n.name) &&
                                  o.getAttribute(`property`) ===
                                    (n.property == null ? null : n.property) &&
                                  o.getAttribute(`http-equiv`) ===
                                    (n.httpEquiv == null
                                      ? null
                                      : n.httpEquiv) &&
                                  o.getAttribute(`charset`) ===
                                    (n.charSet == null ? null : n.charSet))
                              ) {
                                s.splice(c, 1);
                                break b;
                              }
                          }
                          ((o = a.createElement(r)),
                            Rd(o, r, n),
                            a.head.appendChild(o));
                          break;
                        default:
                          throw Error(i(468, r));
                      }
                      ((o[mt] = e), Dt(o), (r = o));
                    }
                    e.stateNode = r;
                  } else Kf(a, e.type, e.stateNode);
                else e.stateNode = Bf(a, r, e.memoizedProps);
              else
                o === r
                  ? r === null &&
                    e.stateNode !== null &&
                    Xc(e, e.memoizedProps, n.memoizedProps)
                  : (o === null
                      ? n.stateNode !== null &&
                        ((n = n.stateNode), n.parentNode.removeChild(n))
                      : o.count--,
                    r === null
                      ? Kf(a, e.type, e.stateNode)
                      : Bf(a, r, e.memoizedProps));
            }
            break;
          case 27:
            (vl(t, e),
              xl(e),
              r & 512 && (rl || n === null || Jc(n, n.return)),
              n !== null && r & 4 && Xc(e, e.memoizedProps, n.memoizedProps));
            break;
          case 5:
            if (
              (vl(t, e),
              xl(e),
              r & 512 && (rl || n === null || Jc(n, n.return)),
              e.flags & 32)
            ) {
              a = e.stateNode;
              try {
                $t(a, ``);
              } catch (t) {
                qu(e, e.return, t);
              }
            }
            (r & 4 &&
              e.stateNode != null &&
              ((a = e.memoizedProps),
              Xc(e, a, n === null ? a : n.memoizedProps)),
              r & 1024 && (il = !0));
            break;
          case 6:
            if ((vl(t, e), xl(e), r & 4)) {
              if (e.stateNode === null) throw Error(i(162));
              ((r = e.memoizedProps), (n = e.stateNode));
              try {
                n.nodeValue = r;
              } catch (t) {
                qu(e, e.return, t);
              }
            }
            break;
          case 3:
            if (
              ((Wf = null),
              (a = yl),
              (yl = bf(t.containerInfo)),
              vl(t, e),
              (yl = a),
              xl(e),
              r & 4 && n !== null && n.memoizedState.isDehydrated)
            )
              try {
                Lp(t.containerInfo);
              } catch (t) {
                qu(e, e.return, t);
              }
            il && ((il = !1), Sl(e));
            break;
          case 4:
            ((r = yl),
              (yl = bf(e.stateNode.containerInfo)),
              vl(t, e),
              xl(e),
              (yl = r));
            break;
          case 12:
            (vl(t, e), xl(e));
            break;
          case 31:
            (vl(t, e),
              xl(e),
              r & 4 &&
                ((r = e.updateQueue),
                r !== null && ((e.updateQueue = null), _l(e, r))));
            break;
          case 13:
            (vl(t, e),
              xl(e),
              e.child.flags & 8192 &&
                (e.memoizedState !== null) !=
                  (n !== null && n.memoizedState !== null) &&
                (tu = Ne()),
              r & 4 &&
                ((r = e.updateQueue),
                r !== null && ((e.updateQueue = null), _l(e, r))));
            break;
          case 22:
            a = e.memoizedState !== null;
            var l = n !== null && n.memoizedState !== null,
              u = nl,
              d = rl;
            if (
              ((nl = u || a),
              (rl = d || l),
              vl(t, e),
              (rl = d),
              (nl = u),
              xl(e),
              r & 8192)
            )
              a: for (
                t = e.stateNode,
                  t._visibility = a ? t._visibility & -2 : t._visibility | 1,
                  a && (n === null || l || nl || rl || wl(e)),
                  n = null,
                  t = e;
                ;
              ) {
                if (t.tag === 5 || t.tag === 26) {
                  if (n === null) {
                    l = n = t;
                    try {
                      if (((o = l.stateNode), a))
                        ((s = o.style),
                          typeof s.setProperty == `function`
                            ? s.setProperty(`display`, `none`, `important`)
                            : (s.display = `none`));
                      else {
                        c = l.stateNode;
                        var f = l.memoizedProps.style,
                          p =
                            f != null && f.hasOwnProperty(`display`)
                              ? f.display
                              : null;
                        c.style.display =
                          p == null || typeof p == `boolean`
                            ? ``
                            : (`` + p).trim();
                      }
                    } catch (e) {
                      qu(l, l.return, e);
                    }
                  }
                } else if (t.tag === 6) {
                  if (n === null) {
                    l = t;
                    try {
                      l.stateNode.nodeValue = a ? `` : l.memoizedProps;
                    } catch (e) {
                      qu(l, l.return, e);
                    }
                  }
                } else if (t.tag === 18) {
                  if (n === null) {
                    l = t;
                    try {
                      var m = l.stateNode;
                      a ? rf(m, !0) : rf(l.stateNode, !1);
                    } catch (e) {
                      qu(l, l.return, e);
                    }
                  }
                } else if (
                  ((t.tag !== 22 && t.tag !== 23) ||
                    t.memoizedState === null ||
                    t === e) &&
                  t.child !== null
                ) {
                  ((t.child.return = t), (t = t.child));
                  continue;
                }
                if (t === e) break a;
                for (; t.sibling === null; ) {
                  if (t.return === null || t.return === e) break a;
                  (n === t && (n = null), (t = t.return));
                }
                (n === t && (n = null),
                  (t.sibling.return = t.return),
                  (t = t.sibling));
              }
            r & 4 &&
              ((r = e.updateQueue),
              r !== null &&
                ((n = r.retryQueue),
                n !== null && ((r.retryQueue = null), _l(e, n))));
            break;
          case 19:
            (vl(t, e),
              xl(e),
              r & 4 &&
                ((r = e.updateQueue),
                r !== null && ((e.updateQueue = null), _l(e, r))));
            break;
          case 30:
            break;
          case 21:
            break;
          default:
            (vl(t, e), xl(e));
        }
      }
      function xl(e) {
        var t = e.flags;
        if (t & 2) {
          try {
            for (var n, r = e.return; r !== null; ) {
              if (Zc(r)) {
                n = r;
                break;
              }
              r = r.return;
            }
            if (n == null) throw Error(i(160));
            switch (n.tag) {
              case 27:
                var a = n.stateNode;
                el(e, Qc(e), a);
                break;
              case 5:
                var o = n.stateNode;
                (n.flags & 32 && ($t(o, ``), (n.flags &= -33)),
                  el(e, Qc(e), o));
                break;
              case 3:
              case 4:
                var s = n.stateNode.containerInfo;
                $c(e, Qc(e), s);
                break;
              default:
                throw Error(i(161));
            }
          } catch (t) {
            qu(e, e.return, t);
          }
          e.flags &= -3;
        }
        t & 4096 && (e.flags &= -4097);
      }
      function Sl(e) {
        if (e.subtreeFlags & 1024)
          for (e = e.child; e !== null; ) {
            var t = e;
            (Sl(t),
              t.tag === 5 && t.flags & 1024 && t.stateNode.reset(),
              (e = e.sibling));
          }
      }
      function Cl(e, t) {
        if (t.subtreeFlags & 8772)
          for (t = t.child; t !== null; )
            (cl(e, t.alternate, t), (t = t.sibling));
      }
      function wl(e) {
        for (e = e.child; e !== null; ) {
          var t = e;
          switch (t.tag) {
            case 0:
            case 11:
            case 14:
            case 15:
              (Wc(4, t, t.return), wl(t));
              break;
            case 1:
              Jc(t, t.return);
              var n = t.stateNode;
              (typeof n.componentWillUnmount == `function` &&
                Kc(t, t.return, n),
                wl(t));
              break;
            case 27:
              _f(t.stateNode);
            case 26:
            case 5:
              (Jc(t, t.return), wl(t));
              break;
            case 22:
              t.memoizedState === null && wl(t);
              break;
            case 30:
              wl(t);
              break;
            default:
              wl(t);
          }
          e = e.sibling;
        }
      }
      function Tl(e, t, n) {
        for (n &&= (t.subtreeFlags & 8772) != 0, t = t.child; t !== null; ) {
          var r = t.alternate,
            i = e,
            a = t,
            o = a.flags;
          switch (a.tag) {
            case 0:
            case 11:
            case 15:
              (Tl(i, a, n), Uc(4, a));
              break;
            case 1:
              if (
                (Tl(i, a, n),
                (r = a),
                (i = r.stateNode),
                typeof i.componentDidMount == `function`)
              )
                try {
                  i.componentDidMount();
                } catch (e) {
                  qu(r, r.return, e);
                }
              if (((r = a), (i = r.updateQueue), i !== null)) {
                var s = r.stateNode;
                try {
                  var c = i.shared.hiddenCallbacks;
                  if (c !== null)
                    for (
                      i.shared.hiddenCallbacks = null, i = 0;
                      i < c.length;
                      i++
                    )
                      to(c[i], s);
                } catch (e) {
                  qu(r, r.return, e);
                }
              }
              (n && o & 64 && Gc(a), qc(a, a.return));
              break;
            case 27:
              tl(a);
            case 26:
            case 5:
              (Tl(i, a, n), n && r === null && o & 4 && Yc(a), qc(a, a.return));
              break;
            case 12:
              Tl(i, a, n);
              break;
            case 31:
              (Tl(i, a, n), n && o & 4 && ml(i, a));
              break;
            case 13:
              (Tl(i, a, n), n && o & 4 && hl(i, a));
              break;
            case 22:
              (a.memoizedState === null && Tl(i, a, n), qc(a, a.return));
              break;
            case 30:
              break;
            default:
              Tl(i, a, n);
          }
          t = t.sibling;
        }
      }
      function El(e, t) {
        var n = null;
        (e !== null &&
          e.memoizedState !== null &&
          e.memoizedState.cachePool !== null &&
          (n = e.memoizedState.cachePool.pool),
          (e = null),
          t.memoizedState !== null &&
            t.memoizedState.cachePool !== null &&
            (e = t.memoizedState.cachePool.pool),
          e !== n && (e != null && e.refCount++, n != null && _a(n)));
      }
      function Dl(e, t) {
        ((e = null),
          t.alternate !== null && (e = t.alternate.memoizedState.cache),
          (t = t.memoizedState.cache),
          t !== e && (t.refCount++, e != null && _a(e)));
      }
      function Ol(e, t, n, r) {
        if (t.subtreeFlags & 10256)
          for (t = t.child; t !== null; ) (kl(e, t, n, r), (t = t.sibling));
      }
      function kl(e, t, n, r) {
        var i = t.flags;
        switch (t.tag) {
          case 0:
          case 11:
          case 15:
            (Ol(e, t, n, r), i & 2048 && Uc(9, t));
            break;
          case 1:
            Ol(e, t, n, r);
            break;
          case 3:
            (Ol(e, t, n, r),
              i & 2048 &&
                ((e = null),
                t.alternate !== null && (e = t.alternate.memoizedState.cache),
                (t = t.memoizedState.cache),
                t !== e && (t.refCount++, e != null && _a(e))));
            break;
          case 12:
            if (i & 2048) {
              (Ol(e, t, n, r), (e = t.stateNode));
              try {
                var a = t.memoizedProps,
                  o = a.id,
                  s = a.onPostCommit;
                typeof s == `function` &&
                  s(
                    o,
                    t.alternate === null ? `mount` : `update`,
                    e.passiveEffectDuration,
                    -0,
                  );
              } catch (e) {
                qu(t, t.return, e);
              }
            } else Ol(e, t, n, r);
            break;
          case 31:
            Ol(e, t, n, r);
            break;
          case 13:
            Ol(e, t, n, r);
            break;
          case 23:
            break;
          case 22:
            ((a = t.stateNode),
              (o = t.alternate),
              t.memoizedState === null
                ? a._visibility & 2
                  ? Ol(e, t, n, r)
                  : ((a._visibility |= 2),
                    U(e, t, n, r, (t.subtreeFlags & 10256) != 0 || !1))
                : a._visibility & 2
                  ? Ol(e, t, n, r)
                  : Al(e, t),
              i & 2048 && El(o, t));
            break;
          case 24:
            (Ol(e, t, n, r), i & 2048 && Dl(t.alternate, t));
            break;
          default:
            Ol(e, t, n, r);
        }
      }
      function U(e, t, n, r, i) {
        for (
          i &&= (t.subtreeFlags & 10256) != 0 || !1, t = t.child;
          t !== null;
        ) {
          var a = e,
            o = t,
            s = n,
            c = r,
            l = o.flags;
          switch (o.tag) {
            case 0:
            case 11:
            case 15:
              (U(a, o, s, c, i), Uc(8, o));
              break;
            case 23:
              break;
            case 22:
              var u = o.stateNode;
              (o.memoizedState === null
                ? ((u._visibility |= 2), U(a, o, s, c, i))
                : u._visibility & 2
                  ? U(a, o, s, c, i)
                  : Al(a, o),
                i && l & 2048 && El(o.alternate, o));
              break;
            case 24:
              (U(a, o, s, c, i), i && l & 2048 && Dl(o.alternate, o));
              break;
            default:
              U(a, o, s, c, i);
          }
          t = t.sibling;
        }
      }
      function Al(e, t) {
        if (t.subtreeFlags & 10256)
          for (t = t.child; t !== null; ) {
            var n = e,
              r = t,
              i = r.flags;
            switch (r.tag) {
              case 22:
                (Al(n, r), i & 2048 && El(r.alternate, r));
                break;
              case 24:
                (Al(n, r), i & 2048 && Dl(r.alternate, r));
                break;
              default:
                Al(n, r);
            }
            t = t.sibling;
          }
      }
      var jl = 8192;
      function Ml(e, t, n) {
        if (e.subtreeFlags & jl)
          for (e = e.child; e !== null; ) (Nl(e, t, n), (e = e.sibling));
      }
      function Nl(e, t, n) {
        switch (e.tag) {
          case 26:
            (Ml(e, t, n),
              e.flags & jl &&
                e.memoizedState !== null &&
                Yf(n, yl, e.memoizedState, e.memoizedProps));
            break;
          case 5:
            Ml(e, t, n);
            break;
          case 3:
          case 4:
            var r = yl;
            ((yl = bf(e.stateNode.containerInfo)), Ml(e, t, n), (yl = r));
            break;
          case 22:
            e.memoizedState === null &&
              ((r = e.alternate),
              r !== null && r.memoizedState !== null
                ? ((r = jl), (jl = 16777216), Ml(e, t, n), (jl = r))
                : Ml(e, t, n));
            break;
          default:
            Ml(e, t, n);
        }
      }
      function Pl(e) {
        var t = e.alternate;
        if (t !== null && ((e = t.child), e !== null)) {
          t.child = null;
          do ((t = e.sibling), (e.sibling = null), (e = t));
          while (e !== null);
        }
      }
      function Fl(e) {
        var t = e.deletions;
        if (e.flags & 16) {
          if (t !== null)
            for (var n = 0; n < t.length; n++) {
              var r = t[n];
              ((ol = r), Rl(r, e));
            }
          Pl(e);
        }
        if (e.subtreeFlags & 10256)
          for (e = e.child; e !== null; ) (Il(e), (e = e.sibling));
      }
      function Il(e) {
        switch (e.tag) {
          case 0:
          case 11:
          case 15:
            (Fl(e), e.flags & 2048 && Wc(9, e, e.return));
            break;
          case 3:
            Fl(e);
            break;
          case 12:
            Fl(e);
            break;
          case 22:
            var t = e.stateNode;
            e.memoizedState !== null &&
            t._visibility & 2 &&
            (e.return === null || e.return.tag !== 13)
              ? ((t._visibility &= -3), Ll(e))
              : Fl(e);
            break;
          default:
            Fl(e);
        }
      }
      function Ll(e) {
        var t = e.deletions;
        if (e.flags & 16) {
          if (t !== null)
            for (var n = 0; n < t.length; n++) {
              var r = t[n];
              ((ol = r), Rl(r, e));
            }
          Pl(e);
        }
        for (e = e.child; e !== null; ) {
          switch (((t = e), t.tag)) {
            case 0:
            case 11:
            case 15:
              (Wc(8, t, t.return), Ll(t));
              break;
            case 22:
              ((n = t.stateNode),
                n._visibility & 2 && ((n._visibility &= -3), Ll(t)));
              break;
            default:
              Ll(t);
          }
          e = e.sibling;
        }
      }
      function Rl(e, t) {
        for (; ol !== null; ) {
          var n = ol;
          switch (n.tag) {
            case 0:
            case 11:
            case 15:
              Wc(8, n, t);
              break;
            case 23:
            case 22:
              if (
                n.memoizedState !== null &&
                n.memoizedState.cachePool !== null
              ) {
                var r = n.memoizedState.cachePool.pool;
                r != null && r.refCount++;
              }
              break;
            case 24:
              _a(n.memoizedState.cache);
          }
          if (((r = n.child), r !== null)) ((r.return = n), (ol = r));
          else
            a: for (n = e; ol !== null; ) {
              r = ol;
              var i = r.sibling,
                a = r.return;
              if ((ll(r), r === n)) {
                ol = null;
                break a;
              }
              if (i !== null) {
                ((i.return = a), (ol = i));
                break a;
              }
              ol = a;
            }
        }
      }
      var W = {
          getCacheForType: function (e) {
            var t = la(ha),
              n = t.data.get(e);
            return (n === void 0 && ((n = e()), t.data.set(e, n)), n);
          },
          cacheSignal: function () {
            return la(ha).controller.signal;
          },
        },
        G = typeof WeakMap == `function` ? WeakMap : Map,
        K = 0,
        zl = null,
        q = null,
        J = 0,
        Bl = 0,
        Vl = null,
        Hl = !1,
        Ul = !1,
        Wl = !1,
        Gl = 0,
        Kl = 0,
        ql = 0,
        Jl = 0,
        Yl = 0,
        Xl = 0,
        Zl = 0,
        Ql = null,
        $l = null,
        eu = !1,
        tu = 0,
        nu = 0,
        ru = 1 / 0,
        iu = null,
        au = null,
        ou = 0,
        su = null,
        cu = null,
        lu = 0,
        uu = 0,
        du = null,
        fu = null,
        pu = 0,
        mu = null;
      function hu() {
        return K & 2 && J !== 0 ? J & -J : E.T === null ? dt() : md();
      }
      function gu() {
        if (Xl === 0)
          if (!(J & 536870912) || j) {
            var e = Xe;
            ((Xe <<= 1), !(Xe & 3932160) && (Xe = 262144), (Xl = e));
          } else Xl = 536870912;
        return ((e = co.current), e !== null && (e.flags |= 32), Xl);
      }
      function _u(e, t, n) {
        (((e === zl && (Bl === 2 || Bl === 9)) ||
          e.cancelPendingCommit !== null) &&
          (wu(e, 0), xu(e, J, Xl, !1)),
          it(e, n),
          (!(K & 2) || e !== zl) &&
            (e === zl && (!(K & 2) && (Jl |= n), Kl === 4 && xu(e, J, Xl, !1)),
            od(e)));
      }
      function vu(e, t, n) {
        if (K & 6) throw Error(i(327));
        var r =
            (!n && (t & 127) == 0 && (t & e.expiredLanes) === 0) || et(e, t),
          a = r ? Mu(e, t) : Au(e, t, !0),
          o = r;
        do {
          if (a === 0) {
            Ul && !r && xu(e, t, 0, !1);
            break;
          } else {
            if (((n = e.current.alternate), o && !bu(n))) {
              ((a = Au(e, t, !1)), (o = !1));
              continue;
            }
            if (a === 2) {
              if (((o = t), e.errorRecoveryDisabledLanes & o)) var s = 0;
              else
                ((s = e.pendingLanes & -536870913),
                  (s = s === 0 ? (s & 536870912 ? 536870912 : 0) : s));
              if (s !== 0) {
                t = s;
                a: {
                  var c = e;
                  a = Ql;
                  var l = c.current.memoizedState.isDehydrated;
                  if (
                    (l && (wu(c, s).flags |= 256), (s = Au(c, s, !1)), s !== 2)
                  ) {
                    if (Wl && !l) {
                      ((c.errorRecoveryDisabledLanes |= o), (Jl |= o), (a = 4));
                      break a;
                    }
                    ((o = $l),
                      ($l = a),
                      o !== null &&
                        ($l === null ? ($l = o) : $l.push.apply($l, o)));
                  }
                  a = s;
                }
                if (((o = !1), a !== 2)) continue;
              }
            }
            if (a === 1) {
              (wu(e, 0), xu(e, t, 0, !0));
              break;
            }
            a: {
              switch (((r = e), (o = a), o)) {
                case 0:
                case 1:
                  throw Error(i(345));
                case 4:
                  if ((t & 4194048) !== t) break;
                case 6:
                  xu(r, t, Xl, !Hl);
                  break a;
                case 2:
                  $l = null;
                  break;
                case 3:
                case 5:
                  break;
                default:
                  throw Error(i(329));
              }
              if ((t & 62914560) === t && ((a = tu + 300 - Ne()), 10 < a)) {
                if ((xu(r, t, Xl, !Hl), $e(r, 0, !0) !== 0)) break a;
                ((lu = t),
                  (r.timeoutHandle = Xd(
                    yu.bind(
                      null,
                      r,
                      n,
                      $l,
                      iu,
                      eu,
                      t,
                      Xl,
                      Jl,
                      Zl,
                      Hl,
                      o,
                      `Throttled`,
                      -0,
                      0,
                    ),
                    a,
                  )));
                break a;
              }
              yu(r, n, $l, iu, eu, t, Xl, Jl, Zl, Hl, o, null, -0, 0);
            }
          }
          break;
        } while (1);
        od(e);
      }
      function yu(e, t, n, r, i, a, o, s, c, l, u, d, f, p) {
        if (
          ((e.timeoutHandle = -1),
          (d = t.subtreeFlags),
          d & 8192 || (d & 16785408) == 16785408)
        ) {
          ((d = {
            stylesheets: null,
            count: 0,
            imgCount: 0,
            imgBytes: 0,
            suspenseyImages: [],
            waitingForImages: !0,
            waitingForViewTransition: !1,
            unsuspend: cn,
          }),
            Nl(t, a, d));
          var m =
            (a & 62914560) === a
              ? tu - Ne()
              : (a & 4194048) === a
                ? nu - Ne()
                : 0;
          if (((m = Zf(d, m)), m !== null)) {
            ((lu = a),
              (e.cancelPendingCommit = m(
                zu.bind(null, e, t, a, n, r, i, o, s, c, u, d, null, f, p),
              )),
              xu(e, a, o, !l));
            return;
          }
        }
        zu(e, t, a, n, r, i, o, s, c);
      }
      function bu(e) {
        for (var t = e; ; ) {
          var n = t.tag;
          if (
            (n === 0 || n === 11 || n === 15) &&
            t.flags & 16384 &&
            ((n = t.updateQueue), n !== null && ((n = n.stores), n !== null))
          )
            for (var r = 0; r < n.length; r++) {
              var i = n[r],
                a = i.getSnapshot;
              i = i.value;
              try {
                if (!Ar(a(), i)) return !1;
              } catch {
                return !1;
              }
            }
          if (((n = t.child), t.subtreeFlags & 16384 && n !== null))
            ((n.return = t), (t = n));
          else {
            if (t === e) break;
            for (; t.sibling === null; ) {
              if (t.return === null || t.return === e) return !0;
              t = t.return;
            }
            ((t.sibling.return = t.return), (t = t.sibling));
          }
        }
        return !0;
      }
      function xu(e, t, n, r) {
        ((t &= ~Yl),
          (t &= ~Jl),
          (e.suspendedLanes |= t),
          (e.pingedLanes &= ~t),
          r && (e.warmLanes |= t),
          (r = e.expirationTimes));
        for (var i = t; 0 < i; ) {
          var a = 31 - Ge(i),
            o = 1 << a;
          ((r[a] = -1), (i &= ~o));
        }
        n !== 0 && ot(e, n, t);
      }
      function Su() {
        return K & 6 ? !0 : (sd(0, !1), !1);
      }
      function Cu() {
        if (q !== null) {
          if (Bl === 0) var e = q.return;
          else
            ((e = q), (ta = ea = null), Ao(e), (Ra = null), (za = 0), (e = q));
          for (; e !== null; ) (Hc(e.alternate, e), (e = e.return));
          q = null;
        }
      }
      function wu(e, t) {
        var n = e.timeoutHandle;
        (n !== -1 && ((e.timeoutHandle = -1), Zd(n)),
          (n = e.cancelPendingCommit),
          n !== null && ((e.cancelPendingCommit = null), n()),
          (lu = 0),
          Cu(),
          (zl = e),
          (q = n = vi(e.current, null)),
          (J = t),
          (Bl = 0),
          (Vl = null),
          (Hl = !1),
          (Ul = et(e, t)),
          (Wl = !1),
          (Zl = Xl = Yl = Jl = ql = Kl = 0),
          ($l = Ql = null),
          (eu = !1),
          t & 8 && (t |= t & 32));
        var r = e.entangledLanes;
        if (r !== 0)
          for (e = e.entanglements, r &= t; 0 < r; ) {
            var i = 31 - Ge(r),
              a = 1 << i;
            ((t |= e[i]), (r &= ~a));
          }
        return ((Gl = t), ci(), n);
      }
      function Tu(e, t) {
        ((R = null),
          (E.H = zs),
          t === Oa || t === Aa
            ? ((t = Ia()), (Bl = 3))
            : t === ka
              ? ((t = Ia()), (Bl = 4))
              : (Bl =
                  t === rc
                    ? 8
                    : typeof t == `object` && t && typeof t.then == `function`
                      ? 6
                      : 1),
          (Vl = t),
          q === null && ((Kl = 1), Zs(e, Ei(t, e.current))));
      }
      function Eu() {
        var e = co.current;
        return e === null
          ? !0
          : (J & 4194048) === J
            ? F === null
            : (J & 62914560) === J || J & 536870912
              ? e === F
              : !1;
      }
      function Du() {
        var e = E.H;
        return ((E.H = zs), e === null ? zs : e);
      }
      function Ou() {
        var e = E.A;
        return ((E.A = W), e);
      }
      function ku() {
        ((Kl = 4),
          Hl || ((J & 4194048) !== J && co.current !== null) || (Ul = !0),
          (!(ql & 134217727) && !(Jl & 134217727)) ||
            zl === null ||
            xu(zl, J, Xl, !1));
      }
      function Au(e, t, n) {
        var r = K;
        K |= 2;
        var i = Du(),
          a = Ou();
        ((zl !== e || J !== t) && ((iu = null), wu(e, t)), (t = !1));
        var o = Kl;
        a: do
          try {
            if (Bl !== 0 && q !== null) {
              var s = q,
                c = Vl;
              switch (Bl) {
                case 8:
                  (Cu(), (o = 6));
                  break a;
                case 3:
                case 2:
                case 9:
                case 6:
                  co.current === null && (t = !0);
                  var l = Bl;
                  if (((Bl = 0), (Vl = null), Iu(e, s, c, l), n && Ul)) {
                    o = 0;
                    break a;
                  }
                  break;
                default:
                  ((l = Bl), (Bl = 0), (Vl = null), Iu(e, s, c, l));
              }
            }
            (ju(), (o = Kl));
            break;
          } catch (t) {
            Tu(e, t);
          }
        while (1);
        return (
          t && e.shellSuspendCounter++,
          (ta = ea = null),
          (K = r),
          (E.H = i),
          (E.A = a),
          q === null && ((zl = null), (J = 0), ci()),
          o
        );
      }
      function ju() {
        for (; q !== null; ) Pu(q);
      }
      function Mu(e, t) {
        var n = K;
        K |= 2;
        var r = Du(),
          a = Ou();
        zl !== e || J !== t
          ? ((iu = null), (ru = Ne() + 500), wu(e, t))
          : (Ul = et(e, t));
        a: do
          try {
            if (Bl !== 0 && q !== null) {
              t = q;
              var o = Vl;
              b: switch (Bl) {
                case 1:
                  ((Bl = 0), (Vl = null), Iu(e, t, o, 1));
                  break;
                case 2:
                case 9:
                  if (Ma(o)) {
                    ((Bl = 0), (Vl = null), Fu(t));
                    break;
                  }
                  ((t = function () {
                    ((Bl !== 2 && Bl !== 9) || zl !== e || (Bl = 7), od(e));
                  }),
                    o.then(t, t));
                  break a;
                case 3:
                  Bl = 7;
                  break a;
                case 4:
                  Bl = 5;
                  break a;
                case 7:
                  Ma(o)
                    ? ((Bl = 0), (Vl = null), Fu(t))
                    : ((Bl = 0), (Vl = null), Iu(e, t, o, 7));
                  break;
                case 5:
                  var s = null;
                  switch (q.tag) {
                    case 26:
                      s = q.memoizedState;
                    case 5:
                    case 27:
                      var c = q;
                      if (s ? Jf(s) : c.stateNode.complete) {
                        ((Bl = 0), (Vl = null));
                        var l = c.sibling;
                        if (l !== null) q = l;
                        else {
                          var u = c.return;
                          u === null ? (q = null) : ((q = u), Lu(u));
                        }
                        break b;
                      }
                  }
                  ((Bl = 0), (Vl = null), Iu(e, t, o, 5));
                  break;
                case 6:
                  ((Bl = 0), (Vl = null), Iu(e, t, o, 6));
                  break;
                case 8:
                  (Cu(), (Kl = 6));
                  break a;
                default:
                  throw Error(i(462));
              }
            }
            Nu();
            break;
          } catch (t) {
            Tu(e, t);
          }
        while (1);
        return (
          (ta = ea = null),
          (E.H = r),
          (E.A = a),
          (K = n),
          q === null ? ((zl = null), (J = 0), ci(), Kl) : 0
        );
      }
      function Nu() {
        for (; q !== null && !je(); ) Pu(q);
      }
      function Pu(e) {
        var t = Nc(e.alternate, e, Gl);
        ((e.memoizedProps = e.pendingProps), t === null ? Lu(e) : (q = t));
      }
      function Fu(e) {
        var t = e,
          n = t.alternate;
        switch (t.tag) {
          case 15:
          case 0:
            t = _c(n, t, t.pendingProps, t.type, void 0, J);
            break;
          case 11:
            t = _c(n, t, t.pendingProps, t.type.render, t.ref, J);
            break;
          case 5:
            Ao(t);
          default:
            (Hc(n, t), (t = q = yi(t, Gl)), (t = Nc(n, t, Gl)));
        }
        ((e.memoizedProps = e.pendingProps), t === null ? Lu(e) : (q = t));
      }
      function Iu(e, t, n, r) {
        ((ta = ea = null), Ao(t), (Ra = null), (za = 0));
        var i = t.return;
        try {
          if (nc(e, i, t, n, J)) {
            ((Kl = 1), Zs(e, Ei(n, e.current)), (q = null));
            return;
          }
        } catch (t) {
          if (i !== null) throw ((q = i), t);
          ((Kl = 1), Zs(e, Ei(n, e.current)), (q = null));
          return;
        }
        t.flags & 32768
          ? (j || r === 1
              ? (e = !0)
              : Ul || J & 536870912
                ? (e = !1)
                : ((Hl = e = !0),
                  (r === 2 || r === 9 || r === 3 || r === 6) &&
                    ((r = co.current),
                    r !== null && r.tag === 13 && (r.flags |= 16384))),
            Ru(t, e))
          : Lu(t);
      }
      function Lu(e) {
        var t = e;
        do {
          if (t.flags & 32768) {
            Ru(t, Hl);
            return;
          }
          e = t.return;
          var n = Bc(t.alternate, t, Gl);
          if (n !== null) {
            q = n;
            return;
          }
          if (((t = t.sibling), t !== null)) {
            q = t;
            return;
          }
          q = t = e;
        } while (t !== null);
        Kl === 0 && (Kl = 5);
      }
      function Ru(e, t) {
        do {
          var n = Vc(e.alternate, e);
          if (n !== null) {
            ((n.flags &= 32767), (q = n));
            return;
          }
          if (
            ((n = e.return),
            n !== null &&
              ((n.flags |= 32768), (n.subtreeFlags = 0), (n.deletions = null)),
            !t && ((e = e.sibling), e !== null))
          ) {
            q = e;
            return;
          }
          q = e = n;
        } while (e !== null);
        ((Kl = 6), (q = null));
      }
      function zu(e, t, n, r, a, o, s, c, l) {
        e.cancelPendingCommit = null;
        do Wu();
        while (ou !== 0);
        if (K & 6) throw Error(i(327));
        if (t !== null) {
          if (t === e.current) throw Error(i(177));
          if (
            ((o = t.lanes | t.childLanes),
            (o |= si),
            at(e, n, o, s, c, l),
            e === zl && ((q = zl = null), (J = 0)),
            (cu = t),
            (su = e),
            (lu = n),
            (uu = o),
            (du = a),
            (fu = r),
            t.subtreeFlags & 10256 || t.flags & 10256
              ? ((e.callbackNode = null),
                (e.callbackPriority = 0),
                $u(Le, function () {
                  return (Gu(), null);
                }))
              : ((e.callbackNode = null), (e.callbackPriority = 0)),
            (r = (t.flags & 13878) != 0),
            t.subtreeFlags & 13878 || r)
          ) {
            ((r = E.T), (E.T = null), (a = D.p), (D.p = 2), (s = K), (K |= 4));
            try {
              sl(e, t, n);
            } finally {
              ((K = s), (D.p = a), (E.T = r));
            }
          }
          ((ou = 1), Bu(), Vu(), Hu());
        }
      }
      function Bu() {
        if (ou === 1) {
          ou = 0;
          var e = su,
            t = cu,
            n = (t.flags & 13878) != 0;
          if (t.subtreeFlags & 13878 || n) {
            ((n = E.T), (E.T = null));
            var r = D.p;
            D.p = 2;
            var i = K;
            K |= 4;
            try {
              bl(t, e);
              var a = Ud,
                o = Fr(e.containerInfo),
                s = a.focusedElem,
                c = a.selectionRange;
              if (
                o !== s &&
                s &&
                s.ownerDocument &&
                Pr(s.ownerDocument.documentElement, s)
              ) {
                if (c !== null && Ir(s)) {
                  var l = c.start,
                    u = c.end;
                  if ((u === void 0 && (u = l), `selectionStart` in s))
                    ((s.selectionStart = l),
                      (s.selectionEnd = Math.min(u, s.value.length)));
                  else {
                    var d = s.ownerDocument || document,
                      f = (d && d.defaultView) || window;
                    if (f.getSelection) {
                      var p = f.getSelection(),
                        m = s.textContent.length,
                        h = Math.min(c.start, m),
                        g = c.end === void 0 ? h : Math.min(c.end, m);
                      !p.extend && h > g && ((o = g), (g = h), (h = o));
                      var _ = Nr(s, h),
                        v = Nr(s, g);
                      if (
                        _ &&
                        v &&
                        (p.rangeCount !== 1 ||
                          p.anchorNode !== _.node ||
                          p.anchorOffset !== _.offset ||
                          p.focusNode !== v.node ||
                          p.focusOffset !== v.offset)
                      ) {
                        var y = d.createRange();
                        (y.setStart(_.node, _.offset),
                          p.removeAllRanges(),
                          h > g
                            ? (p.addRange(y), p.extend(v.node, v.offset))
                            : (y.setEnd(v.node, v.offset), p.addRange(y)));
                      }
                    }
                  }
                }
                for (d = [], p = s; (p = p.parentNode); )
                  p.nodeType === 1 &&
                    d.push({
                      element: p,
                      left: p.scrollLeft,
                      top: p.scrollTop,
                    });
                for (
                  typeof s.focus == `function` && s.focus(), s = 0;
                  s < d.length;
                  s++
                ) {
                  var b = d[s];
                  ((b.element.scrollLeft = b.left),
                    (b.element.scrollTop = b.top));
                }
              }
              ((dp = !!Hd), (Ud = Hd = null));
            } finally {
              ((K = i), (D.p = r), (E.T = n));
            }
          }
          ((e.current = t), (ou = 2));
        }
      }
      function Vu() {
        if (ou === 2) {
          ou = 0;
          var e = su,
            t = cu,
            n = (t.flags & 8772) != 0;
          if (t.subtreeFlags & 8772 || n) {
            ((n = E.T), (E.T = null));
            var r = D.p;
            D.p = 2;
            var i = K;
            K |= 4;
            try {
              cl(e, t.alternate, t);
            } finally {
              ((K = i), (D.p = r), (E.T = n));
            }
          }
          ou = 3;
        }
      }
      function Hu() {
        if (ou === 4 || ou === 3) {
          ((ou = 0), Me());
          var e = su,
            t = cu,
            n = lu,
            r = fu;
          t.subtreeFlags & 10256 || t.flags & 10256
            ? (ou = 5)
            : ((ou = 0), (cu = su = null), Uu(e, e.pendingLanes));
          var i = e.pendingLanes;
          if (
            (i === 0 && (au = null),
            ut(n),
            (t = t.stateNode),
            Ue && typeof Ue.onCommitFiberRoot == `function`)
          )
            try {
              Ue.onCommitFiberRoot(
                He,
                t,
                void 0,
                (t.current.flags & 128) == 128,
              );
            } catch {}
          if (r !== null) {
            ((t = E.T), (i = D.p), (D.p = 2), (E.T = null));
            try {
              for (var a = e.onRecoverableError, o = 0; o < r.length; o++) {
                var s = r[o];
                a(s.value, { componentStack: s.stack });
              }
            } finally {
              ((E.T = t), (D.p = i));
            }
          }
          (lu & 3 && Wu(),
            od(e),
            (i = e.pendingLanes),
            n & 261930 && i & 42
              ? e === mu
                ? pu++
                : ((pu = 0), (mu = e))
              : (pu = 0),
            sd(0, !1));
        }
      }
      function Uu(e, t) {
        (e.pooledCacheLanes &= t) === 0 &&
          ((t = e.pooledCache), t != null && ((e.pooledCache = null), _a(t)));
      }
      function Wu() {
        return (Bu(), Vu(), Hu(), Gu());
      }
      function Gu() {
        if (ou !== 5) return !1;
        var e = su,
          t = uu;
        uu = 0;
        var n = ut(lu),
          r = E.T,
          a = D.p;
        try {
          ((D.p = 32 > n ? 32 : n), (E.T = null), (n = du), (du = null));
          var o = su,
            s = lu;
          if (((ou = 0), (cu = su = null), (lu = 0), K & 6))
            throw Error(i(331));
          var c = K;
          if (
            ((K |= 4),
            Il(o.current),
            kl(o, o.current, s, n),
            (K = c),
            sd(0, !1),
            Ue && typeof Ue.onPostCommitFiberRoot == `function`)
          )
            try {
              Ue.onPostCommitFiberRoot(He, o);
            } catch {}
          return !0;
        } finally {
          ((D.p = a), (E.T = r), Uu(e, t));
        }
      }
      function Ku(e, t, n) {
        ((t = Ei(n, t)),
          (t = $s(e.stateNode, t, 2)),
          (e = P(e, t, 2)),
          e !== null && (it(e, 2), od(e)));
      }
      function qu(e, t, n) {
        if (e.tag === 3) Ku(e, e, n);
        else
          for (; t !== null; ) {
            if (t.tag === 3) {
              Ku(t, e, n);
              break;
            } else if (t.tag === 1) {
              var r = t.stateNode;
              if (
                typeof t.type.getDerivedStateFromError == `function` ||
                (typeof r.componentDidCatch == `function` &&
                  (au === null || !au.has(r)))
              ) {
                ((e = Ei(n, e)),
                  (n = ec(2)),
                  (r = P(t, n, 2)),
                  r !== null && (tc(n, r, t, e), it(r, 2), od(r)));
                break;
              }
            }
            t = t.return;
          }
      }
      function Ju(e, t, n) {
        var r = e.pingCache;
        if (r === null) {
          r = e.pingCache = new G();
          var i = new Set();
          r.set(t, i);
        } else ((i = r.get(t)), i === void 0 && ((i = new Set()), r.set(t, i)));
        i.has(n) ||
          ((Wl = !0), i.add(n), (e = Yu.bind(null, e, t, n)), t.then(e, e));
      }
      function Yu(e, t, n) {
        var r = e.pingCache;
        (r !== null && r.delete(t),
          (e.pingedLanes |= e.suspendedLanes & n),
          (e.warmLanes &= ~n),
          zl === e &&
            (J & n) === n &&
            (Kl === 4 || (Kl === 3 && (J & 62914560) === J && 300 > Ne() - tu)
              ? !(K & 2) && wu(e, 0)
              : (Yl |= n),
            Zl === J && (Zl = 0)),
          od(e));
      }
      function Xu(e, t) {
        (t === 0 && (t = nt()),
          (e = di(e, t)),
          e !== null && (it(e, t), od(e)));
      }
      function Zu(e) {
        var t = e.memoizedState,
          n = 0;
        (t !== null && (n = t.retryLane), Xu(e, n));
      }
      function Qu(e, t) {
        var n = 0;
        switch (e.tag) {
          case 31:
          case 13:
            var r = e.stateNode,
              a = e.memoizedState;
            a !== null && (n = a.retryLane);
            break;
          case 19:
            r = e.stateNode;
            break;
          case 22:
            r = e.stateNode._retryCache;
            break;
          default:
            throw Error(i(314));
        }
        (r !== null && r.delete(t), Xu(e, n));
      }
      function $u(e, t) {
        return ke(e, t);
      }
      var ed = null,
        td = null,
        nd = !1,
        rd = !1,
        id = !1,
        ad = 0;
      function od(e) {
        (e !== td &&
          e.next === null &&
          (td === null ? (ed = td = e) : (td = td.next = e)),
          (rd = !0),
          nd || ((nd = !0), pd()));
      }
      function sd(e, t) {
        if (!id && rd) {
          id = !0;
          do
            for (var n = !1, r = ed; r !== null; ) {
              if (!t)
                if (e !== 0) {
                  var i = r.pendingLanes;
                  if (i === 0) var a = 0;
                  else {
                    var o = r.suspendedLanes,
                      s = r.pingedLanes;
                    ((a = (1 << (31 - Ge(42 | e) + 1)) - 1),
                      (a &= i & ~(o & ~s)),
                      (a =
                        a & 201326741 ? (a & 201326741) | 1 : a ? a | 2 : 0));
                  }
                  a !== 0 && ((n = !0), fd(r, a));
                } else
                  ((a = J),
                    (a = $e(
                      r,
                      r === zl ? a : 0,
                      r.cancelPendingCommit !== null || r.timeoutHandle !== -1,
                    )),
                    !(a & 3) || et(r, a) || ((n = !0), fd(r, a)));
              r = r.next;
            }
          while (n);
          id = !1;
        }
      }
      function cd() {
        ld();
      }
      function ld() {
        rd = nd = !1;
        var e = 0;
        ad !== 0 && Yd() && (e = ad);
        for (var t = Ne(), n = null, r = ed; r !== null; ) {
          var i = r.next,
            a = ud(r, t);
          (a === 0
            ? ((r.next = null),
              n === null ? (ed = i) : (n.next = i),
              i === null && (td = n))
            : ((n = r), (e !== 0 || a & 3) && (rd = !0)),
            (r = i));
        }
        ((ou !== 0 && ou !== 5) || sd(e, !1), ad !== 0 && (ad = 0));
      }
      function ud(e, t) {
        for (
          var n = e.suspendedLanes,
            r = e.pingedLanes,
            i = e.expirationTimes,
            a = e.pendingLanes & -62914561;
          0 < a;
        ) {
          var o = 31 - Ge(a),
            s = 1 << o,
            c = i[o];
          (c === -1
            ? ((s & n) === 0 || (s & r) !== 0) && (i[o] = tt(s, t))
            : c <= t && (e.expiredLanes |= s),
            (a &= ~s));
        }
        if (
          ((t = zl),
          (n = J),
          (n = $e(
            e,
            e === t ? n : 0,
            e.cancelPendingCommit !== null || e.timeoutHandle !== -1,
          )),
          (r = e.callbackNode),
          n === 0 ||
            (e === t && (Bl === 2 || Bl === 9)) ||
            e.cancelPendingCommit !== null)
        )
          return (
            r !== null && r !== null && Ae(r),
            (e.callbackNode = null),
            (e.callbackPriority = 0)
          );
        if (!(n & 3) || et(e, n)) {
          if (((t = n & -n), t === e.callbackPriority)) return t;
          switch ((r !== null && Ae(r), ut(n))) {
            case 2:
            case 8:
              n = Ie;
              break;
            case 32:
              n = Le;
              break;
            case 268435456:
              n = ze;
              break;
            default:
              n = Le;
          }
          return (
            (r = dd.bind(null, e)),
            (n = ke(n, r)),
            (e.callbackPriority = t),
            (e.callbackNode = n),
            t
          );
        }
        return (
          r !== null && r !== null && Ae(r),
          (e.callbackPriority = 2),
          (e.callbackNode = null),
          2
        );
      }
      function dd(e, t) {
        if (ou !== 0 && ou !== 5)
          return ((e.callbackNode = null), (e.callbackPriority = 0), null);
        var n = e.callbackNode;
        if (Wu() && e.callbackNode !== n) return null;
        var r = J;
        return (
          (r = $e(
            e,
            e === zl ? r : 0,
            e.cancelPendingCommit !== null || e.timeoutHandle !== -1,
          )),
          r === 0
            ? null
            : (vu(e, r, t),
              ud(e, Ne()),
              e.callbackNode != null && e.callbackNode === n
                ? dd.bind(null, e)
                : null)
        );
      }
      function fd(e, t) {
        if (Wu()) return null;
        vu(e, t, !0);
      }
      function pd() {
        $d(function () {
          K & 6 ? ke(Fe, cd) : ld();
        });
      }
      function md() {
        if (ad === 0) {
          var e = ba;
          (e === 0 && ((e = Ye), (Ye <<= 1), !(Ye & 261888) && (Ye = 256)),
            (ad = e));
        }
        return ad;
      }
      function hd(e) {
        return e == null || typeof e == `symbol` || typeof e == `boolean`
          ? null
          : typeof e == `function`
            ? e
            : sn(`` + e);
      }
      function gd(e, t) {
        var n = t.ownerDocument.createElement(`input`);
        return (
          (n.name = t.name),
          (n.value = t.value),
          e.id && n.setAttribute(`form`, e.id),
          t.parentNode.insertBefore(n, t),
          (e = new FormData(e)),
          n.parentNode.removeChild(n),
          e
        );
      }
      function _d(e, t, n, r, i) {
        if (t === `submit` && n && n.stateNode === i) {
          var a = hd((i[ht] || null).action),
            o = r.submitter;
          o &&
            ((t = (t = o[ht] || null)
              ? hd(t.formAction)
              : o.getAttribute(`formAction`)),
            t !== null && ((a = t), (o = null)));
          var s = new kn(`action`, `action`, null, r, i);
          e.push({
            event: s,
            listeners: [
              {
                instance: null,
                listener: function () {
                  if (r.defaultPrevented) {
                    if (ad !== 0) {
                      var e = o ? gd(i, o) : new FormData(i);
                      Ts(
                        n,
                        { pending: !0, data: e, method: i.method, action: a },
                        null,
                        e,
                      );
                    }
                  } else
                    typeof a == `function` &&
                      (s.preventDefault(),
                      (e = o ? gd(i, o) : new FormData(i)),
                      Ts(
                        n,
                        { pending: !0, data: e, method: i.method, action: a },
                        a,
                        e,
                      ));
                },
                currentTarget: i,
              },
            ],
          });
        }
      }
      for (var vd = 0; vd < ni.length; vd++) {
        var yd = ni[vd];
        ri(yd.toLowerCase(), `on` + (yd[0].toUpperCase() + yd.slice(1)));
      }
      (ri(Jr, `onAnimationEnd`),
        ri(Yr, `onAnimationIteration`),
        ri(Xr, `onAnimationStart`),
        ri(`dblclick`, `onDoubleClick`),
        ri(`focusin`, `onFocus`),
        ri(`focusout`, `onBlur`),
        ri(Zr, `onTransitionRun`),
        ri(Qr, `onTransitionStart`),
        ri($r, `onTransitionCancel`),
        ri(ei, `onTransitionEnd`),
        jt(`onMouseEnter`, [`mouseout`, `mouseover`]),
        jt(`onMouseLeave`, [`mouseout`, `mouseover`]),
        jt(`onPointerEnter`, [`pointerout`, `pointerover`]),
        jt(`onPointerLeave`, [`pointerout`, `pointerover`]),
        At(
          `onChange`,
          `change click focusin focusout input keydown keyup selectionchange`.split(
            ` `,
          ),
        ),
        At(
          `onSelect`,
          `focusout contextmenu dragend focusin keydown keyup mousedown mouseup selectionchange`.split(
            ` `,
          ),
        ),
        At(`onBeforeInput`, [
          `compositionend`,
          `keypress`,
          `textInput`,
          `paste`,
        ]),
        At(
          `onCompositionEnd`,
          `compositionend focusout keydown keypress keyup mousedown`.split(` `),
        ),
        At(
          `onCompositionStart`,
          `compositionstart focusout keydown keypress keyup mousedown`.split(
            ` `,
          ),
        ),
        At(
          `onCompositionUpdate`,
          `compositionupdate focusout keydown keypress keyup mousedown`.split(
            ` `,
          ),
        ));
      var bd =
          `abort canplay canplaythrough durationchange emptied encrypted ended error loadeddata loadedmetadata loadstart pause play playing progress ratechange resize seeked seeking stalled suspend timeupdate volumechange waiting`.split(
            ` `,
          ),
        xd = new Set(
          `beforetoggle cancel close invalid load scroll scrollend toggle`
            .split(` `)
            .concat(bd),
        );
      function Sd(e, t) {
        t = (t & 4) != 0;
        for (var n = 0; n < e.length; n++) {
          var r = e[n],
            i = r.event;
          r = r.listeners;
          a: {
            var a = void 0;
            if (t)
              for (var o = r.length - 1; 0 <= o; o--) {
                var s = r[o],
                  c = s.instance,
                  l = s.currentTarget;
                if (((s = s.listener), c !== a && i.isPropagationStopped()))
                  break a;
                ((a = s), (i.currentTarget = l));
                try {
                  a(i);
                } catch (e) {
                  ii(e);
                }
                ((i.currentTarget = null), (a = c));
              }
            else
              for (o = 0; o < r.length; o++) {
                if (
                  ((s = r[o]),
                  (c = s.instance),
                  (l = s.currentTarget),
                  (s = s.listener),
                  c !== a && i.isPropagationStopped())
                )
                  break a;
                ((a = s), (i.currentTarget = l));
                try {
                  a(i);
                } catch (e) {
                  ii(e);
                }
                ((i.currentTarget = null), (a = c));
              }
          }
        }
      }
      function Y(e, t) {
        var n = t[_t];
        n === void 0 && (n = t[_t] = new Set());
        var r = e + `__bubble`;
        n.has(r) || (Ed(t, e, 2, !1), n.add(r));
      }
      function Cd(e, t, n) {
        var r = 0;
        (t && (r |= 4), Ed(n, e, r, t));
      }
      var wd = `_reactListening` + Math.random().toString(36).slice(2);
      function Td(e) {
        if (!e[wd]) {
          ((e[wd] = !0),
            Ot.forEach(function (t) {
              t !== `selectionchange` &&
                (xd.has(t) || Cd(t, !1, e), Cd(t, !0, e));
            }));
          var t = e.nodeType === 9 ? e : e.ownerDocument;
          t === null || t[wd] || ((t[wd] = !0), Cd(`selectionchange`, !1, t));
        }
      }
      function Ed(e, t, n, r) {
        switch (vp(t)) {
          case 2:
            var i = fp;
            break;
          case 8:
            i = pp;
            break;
          default:
            i = mp;
        }
        ((n = i.bind(null, t, n, e)),
          (i = void 0),
          !vn ||
            (t !== `touchstart` && t !== `touchmove` && t !== `wheel`) ||
            (i = !0),
          r
            ? i === void 0
              ? e.addEventListener(t, n, !0)
              : e.addEventListener(t, n, { capture: !0, passive: i })
            : i === void 0
              ? e.addEventListener(t, n, !1)
              : e.addEventListener(t, n, { passive: i }));
      }
      function Dd(e, t, n, r, i) {
        var a = r;
        if (!(t & 1) && !(t & 2) && r !== null)
          a: for (;;) {
            if (r === null) return;
            var s = r.tag;
            if (s === 3 || s === 4) {
              var c = r.stateNode.containerInfo;
              if (c === i) break;
              if (s === 4)
                for (s = r.return; s !== null; ) {
                  var l = s.tag;
                  if ((l === 3 || l === 4) && s.stateNode.containerInfo === i)
                    return;
                  s = s.return;
                }
              for (; c !== null; ) {
                if (((s = Ct(c)), s === null)) return;
                if (((l = s.tag), l === 5 || l === 6 || l === 26 || l === 27)) {
                  r = a = s;
                  continue a;
                }
                c = c.parentNode;
              }
            }
            r = r.return;
          }
        hn(function () {
          var r = a,
            i = un(n),
            s = [];
          a: {
            var c = ti.get(e);
            if (c !== void 0) {
              var l = kn,
                u = e;
              switch (e) {
                case `keypress`:
                  if (wn(n) === 0) break a;
                case `keydown`:
                case `keyup`:
                  l = qn;
                  break;
                case `focusin`:
                  ((u = `focus`), (l = Rn));
                  break;
                case `focusout`:
                  ((u = `blur`), (l = Rn));
                  break;
                case `beforeblur`:
                case `afterblur`:
                  l = Rn;
                  break;
                case `click`:
                  if (n.button === 2) break a;
                case `auxclick`:
                case `dblclick`:
                case `mousedown`:
                case `mousemove`:
                case `mouseup`:
                case `mouseout`:
                case `mouseover`:
                case `contextmenu`:
                  l = In;
                  break;
                case `drag`:
                case `dragend`:
                case `dragenter`:
                case `dragexit`:
                case `dragleave`:
                case `dragover`:
                case `dragstart`:
                case `drop`:
                  l = Ln;
                  break;
                case `touchcancel`:
                case `touchend`:
                case `touchmove`:
                case `touchstart`:
                  l = Yn;
                  break;
                case Jr:
                case Yr:
                case Xr:
                  l = zn;
                  break;
                case ei:
                  l = Xn;
                  break;
                case `scroll`:
                case `scrollend`:
                  l = jn;
                  break;
                case `wheel`:
                  l = Zn;
                  break;
                case `copy`:
                case `cut`:
                case `paste`:
                  l = Bn;
                  break;
                case `gotpointercapture`:
                case `lostpointercapture`:
                case `pointercancel`:
                case `pointerdown`:
                case `pointermove`:
                case `pointerout`:
                case `pointerover`:
                case `pointerup`:
                  l = Jn;
                  break;
                case `toggle`:
                case `beforetoggle`:
                  l = Qn;
              }
              var d = (t & 4) != 0,
                f = !d && (e === `scroll` || e === `scrollend`),
                p = d ? (c === null ? null : c + `Capture`) : c;
              d = [];
              for (var m = r, h; m !== null; ) {
                var g = m;
                if (
                  ((h = g.stateNode),
                  (g = g.tag),
                  (g !== 5 && g !== 26 && g !== 27) ||
                    h === null ||
                    p === null ||
                    ((g = gn(m, p)), g != null && d.push(Od(m, g, h))),
                  f)
                )
                  break;
                m = m.return;
              }
              0 < d.length &&
                ((c = new l(c, u, null, n, i)),
                s.push({ event: c, listeners: d }));
            }
          }
          if (!(t & 7)) {
            a: {
              if (
                ((c = e === `mouseover` || e === `pointerover`),
                (l = e === `mouseout` || e === `pointerout`),
                c &&
                  n !== ln &&
                  (u = n.relatedTarget || n.fromElement) &&
                  (Ct(u) || u[gt]))
              )
                break a;
              if (
                (l || c) &&
                ((c =
                  i.window === i
                    ? i
                    : (c = i.ownerDocument)
                      ? c.defaultView || c.parentWindow
                      : window),
                l
                  ? ((u = n.relatedTarget || n.toElement),
                    (l = r),
                    (u = u ? Ct(u) : null),
                    u !== null &&
                      ((f = o(u)),
                      (d = u.tag),
                      u !== f || (d !== 5 && d !== 27 && d !== 6)) &&
                      (u = null))
                  : ((l = null), (u = r)),
                l !== u)
              ) {
                if (
                  ((d = In),
                  (g = `onMouseLeave`),
                  (p = `onMouseEnter`),
                  (m = `mouse`),
                  (e === `pointerout` || e === `pointerover`) &&
                    ((d = Jn),
                    (g = `onPointerLeave`),
                    (p = `onPointerEnter`),
                    (m = `pointer`)),
                  (f = l == null ? c : Tt(l)),
                  (h = u == null ? c : Tt(u)),
                  (c = new d(g, m + `leave`, l, n, i)),
                  (c.target = f),
                  (c.relatedTarget = h),
                  (g = null),
                  Ct(i) === r &&
                    ((d = new d(p, m + `enter`, u, n, i)),
                    (d.target = h),
                    (d.relatedTarget = f),
                    (g = d)),
                  (f = g),
                  l && u)
                )
                  b: {
                    for (d = Ad, p = l, m = u, h = 0, g = p; g; g = d(g)) h++;
                    g = 0;
                    for (var _ = m; _; _ = d(_)) g++;
                    for (; 0 < h - g; ) ((p = d(p)), h--);
                    for (; 0 < g - h; ) ((m = d(m)), g--);
                    for (; h--; ) {
                      if (p === m || (m !== null && p === m.alternate)) {
                        d = p;
                        break b;
                      }
                      ((p = d(p)), (m = d(m)));
                    }
                    d = null;
                  }
                else d = null;
                (l !== null && jd(s, c, l, d, !1),
                  u !== null && f !== null && jd(s, f, u, d, !0));
              }
            }
            a: {
              if (
                ((c = r ? Tt(r) : window),
                (l = c.nodeName && c.nodeName.toLowerCase()),
                l === `select` || (l === `input` && c.type === `file`))
              )
                var v = vr;
              else if (fr(c))
                if (yr) v = Or;
                else {
                  v = Er;
                  var y = Tr;
                }
              else
                ((l = c.nodeName),
                  !l ||
                  l.toLowerCase() !== `input` ||
                  (c.type !== `checkbox` && c.type !== `radio`)
                    ? r && rn(r.elementType) && (v = vr)
                    : (v = Dr));
              if ((v &&= v(e, r))) {
                pr(s, v, n, i);
                break a;
              }
              (y && y(e, c, r),
                e === `focusout` &&
                  r &&
                  c.type === `number` &&
                  r.memoizedProps.value != null &&
                  Yt(c, `number`, c.value));
            }
            switch (((y = r ? Tt(r) : window), e)) {
              case `focusin`:
                (fr(y) || y.contentEditable === `true`) &&
                  ((Rr = y), (zr = r), (Br = null));
                break;
              case `focusout`:
                Br = zr = Rr = null;
                break;
              case `mousedown`:
                Vr = !0;
                break;
              case `contextmenu`:
              case `mouseup`:
              case `dragend`:
                ((Vr = !1), Hr(s, n, i));
                break;
              case `selectionchange`:
                if (Lr) break;
              case `keydown`:
              case `keyup`:
                Hr(s, n, i);
            }
            var b;
            if (er)
              b: {
                switch (e) {
                  case `compositionstart`:
                    var x = `onCompositionStart`;
                    break b;
                  case `compositionend`:
                    x = `onCompositionEnd`;
                    break b;
                  case `compositionupdate`:
                    x = `onCompositionUpdate`;
                    break b;
                }
                x = void 0;
              }
            else
              cr
                ? or(e, n) && (x = `onCompositionEnd`)
                : e === `keydown` &&
                  n.keyCode === 229 &&
                  (x = `onCompositionStart`);
            (x &&
              (rr &&
                n.locale !== `ko` &&
                (cr || x !== `onCompositionStart`
                  ? x === `onCompositionEnd` && cr && (b = Cn())
                  : ((bn = i),
                    (xn = `value` in bn ? bn.value : bn.textContent),
                    (cr = !0))),
              (y = kd(r, x)),
              0 < y.length &&
                ((x = new Vn(x, e, null, n, i)),
                s.push({ event: x, listeners: y }),
                b ? (x.data = b) : ((b = sr(n)), b !== null && (x.data = b)))),
              (b = nr ? lr(e, n) : ur(e, n)) &&
                ((x = kd(r, `onBeforeInput`)),
                0 < x.length &&
                  ((y = new Vn(`onBeforeInput`, `beforeinput`, null, n, i)),
                  s.push({ event: y, listeners: x }),
                  (y.data = b))),
              _d(s, e, r, n, i));
          }
          Sd(s, t);
        });
      }
      function Od(e, t, n) {
        return { instance: e, listener: t, currentTarget: n };
      }
      function kd(e, t) {
        for (var n = t + `Capture`, r = []; e !== null; ) {
          var i = e,
            a = i.stateNode;
          if (
            ((i = i.tag),
            (i !== 5 && i !== 26 && i !== 27) ||
              a === null ||
              ((i = gn(e, n)),
              i != null && r.unshift(Od(e, i, a)),
              (i = gn(e, t)),
              i != null && r.push(Od(e, i, a))),
            e.tag === 3)
          )
            return r;
          e = e.return;
        }
        return [];
      }
      function Ad(e) {
        if (e === null) return null;
        do e = e.return;
        while (e && e.tag !== 5 && e.tag !== 27);
        return e || null;
      }
      function jd(e, t, n, r, i) {
        for (var a = t._reactName, o = []; n !== null && n !== r; ) {
          var s = n,
            c = s.alternate,
            l = s.stateNode;
          if (((s = s.tag), c !== null && c === r)) break;
          ((s !== 5 && s !== 26 && s !== 27) ||
            l === null ||
            ((c = l),
            i
              ? ((l = gn(n, a)), l != null && o.unshift(Od(n, l, c)))
              : i || ((l = gn(n, a)), l != null && o.push(Od(n, l, c)))),
            (n = n.return));
        }
        o.length !== 0 && e.push({ event: t, listeners: o });
      }
      var Md = /\r\n?/g,
        Nd = /\u0000|\uFFFD/g;
      function Pd(e) {
        return (typeof e == `string` ? e : `` + e)
          .replace(
            Md,
            `
`,
          )
          .replace(Nd, ``);
      }
      function Fd(e, t) {
        return ((t = Pd(t)), Pd(e) === t);
      }
      function Id(e, t, n, r, a, o) {
        switch (n) {
          case `children`:
            typeof r == `string`
              ? t === `body` || (t === `textarea` && r === ``) || $t(e, r)
              : (typeof r == `number` || typeof r == `bigint`) &&
                t !== `body` &&
                $t(e, `` + r);
            break;
          case `className`:
            Lt(e, `class`, r);
            break;
          case `tabIndex`:
            Lt(e, `tabindex`, r);
            break;
          case `dir`:
          case `role`:
          case `viewBox`:
          case `width`:
          case `height`:
            Lt(e, n, r);
            break;
          case `style`:
            nn(e, r, o);
            break;
          case `data`:
            if (t !== `object`) {
              Lt(e, `data`, r);
              break;
            }
          case `src`:
          case `href`:
            if (r === `` && (t !== `a` || n !== `href`)) {
              e.removeAttribute(n);
              break;
            }
            if (
              r == null ||
              typeof r == `function` ||
              typeof r == `symbol` ||
              typeof r == `boolean`
            ) {
              e.removeAttribute(n);
              break;
            }
            ((r = sn(`` + r)), e.setAttribute(n, r));
            break;
          case `action`:
          case `formAction`:
            if (typeof r == `function`) {
              e.setAttribute(
                n,
                `javascript:throw new Error('A React form was unexpectedly submitted. If you called form.submit() manually, consider using form.requestSubmit() instead. If you\\'re trying to use event.stopPropagation() in a submit event handler, consider also calling event.preventDefault().')`,
              );
              break;
            } else
              typeof o == `function` &&
                (n === `formAction`
                  ? (t !== `input` && Id(e, t, `name`, a.name, a, null),
                    Id(e, t, `formEncType`, a.formEncType, a, null),
                    Id(e, t, `formMethod`, a.formMethod, a, null),
                    Id(e, t, `formTarget`, a.formTarget, a, null))
                  : (Id(e, t, `encType`, a.encType, a, null),
                    Id(e, t, `method`, a.method, a, null),
                    Id(e, t, `target`, a.target, a, null)));
            if (r == null || typeof r == `symbol` || typeof r == `boolean`) {
              e.removeAttribute(n);
              break;
            }
            ((r = sn(`` + r)), e.setAttribute(n, r));
            break;
          case `onClick`:
            r != null && (e.onclick = cn);
            break;
          case `onScroll`:
            r != null && Y(`scroll`, e);
            break;
          case `onScrollEnd`:
            r != null && Y(`scrollend`, e);
            break;
          case `dangerouslySetInnerHTML`:
            if (r != null) {
              if (typeof r != `object` || !(`__html` in r)) throw Error(i(61));
              if (((n = r.__html), n != null)) {
                if (a.children != null) throw Error(i(60));
                e.innerHTML = n;
              }
            }
            break;
          case `multiple`:
            e.multiple = r && typeof r != `function` && typeof r != `symbol`;
            break;
          case `muted`:
            e.muted = r && typeof r != `function` && typeof r != `symbol`;
            break;
          case `suppressContentEditableWarning`:
          case `suppressHydrationWarning`:
          case `defaultValue`:
          case `defaultChecked`:
          case `innerHTML`:
          case `ref`:
            break;
          case `autoFocus`:
            break;
          case `xlinkHref`:
            if (
              r == null ||
              typeof r == `function` ||
              typeof r == `boolean` ||
              typeof r == `symbol`
            ) {
              e.removeAttribute(`xlink:href`);
              break;
            }
            ((n = sn(`` + r)),
              e.setAttributeNS(
                `http://www.w3.org/1999/xlink`,
                `xlink:href`,
                n,
              ));
            break;
          case `contentEditable`:
          case `spellCheck`:
          case `draggable`:
          case `value`:
          case `autoReverse`:
          case `externalResourcesRequired`:
          case `focusable`:
          case `preserveAlpha`:
            r != null && typeof r != `function` && typeof r != `symbol`
              ? e.setAttribute(n, `` + r)
              : e.removeAttribute(n);
            break;
          case `inert`:
          case `allowFullScreen`:
          case `async`:
          case `autoPlay`:
          case `controls`:
          case `default`:
          case `defer`:
          case `disabled`:
          case `disablePictureInPicture`:
          case `disableRemotePlayback`:
          case `formNoValidate`:
          case `hidden`:
          case `loop`:
          case `noModule`:
          case `noValidate`:
          case `open`:
          case `playsInline`:
          case `readOnly`:
          case `required`:
          case `reversed`:
          case `scoped`:
          case `seamless`:
          case `itemScope`:
            r && typeof r != `function` && typeof r != `symbol`
              ? e.setAttribute(n, ``)
              : e.removeAttribute(n);
            break;
          case `capture`:
          case `download`:
            !0 === r
              ? e.setAttribute(n, ``)
              : !1 !== r &&
                  r != null &&
                  typeof r != `function` &&
                  typeof r != `symbol`
                ? e.setAttribute(n, r)
                : e.removeAttribute(n);
            break;
          case `cols`:
          case `rows`:
          case `size`:
          case `span`:
            r != null &&
            typeof r != `function` &&
            typeof r != `symbol` &&
            !isNaN(r) &&
            1 <= r
              ? e.setAttribute(n, r)
              : e.removeAttribute(n);
            break;
          case `rowSpan`:
          case `start`:
            r == null ||
            typeof r == `function` ||
            typeof r == `symbol` ||
            isNaN(r)
              ? e.removeAttribute(n)
              : e.setAttribute(n, r);
            break;
          case `popover`:
            (Y(`beforetoggle`, e), Y(`toggle`, e), It(e, `popover`, r));
            break;
          case `xlinkActuate`:
            Rt(e, `http://www.w3.org/1999/xlink`, `xlink:actuate`, r);
            break;
          case `xlinkArcrole`:
            Rt(e, `http://www.w3.org/1999/xlink`, `xlink:arcrole`, r);
            break;
          case `xlinkRole`:
            Rt(e, `http://www.w3.org/1999/xlink`, `xlink:role`, r);
            break;
          case `xlinkShow`:
            Rt(e, `http://www.w3.org/1999/xlink`, `xlink:show`, r);
            break;
          case `xlinkTitle`:
            Rt(e, `http://www.w3.org/1999/xlink`, `xlink:title`, r);
            break;
          case `xlinkType`:
            Rt(e, `http://www.w3.org/1999/xlink`, `xlink:type`, r);
            break;
          case `xmlBase`:
            Rt(e, `http://www.w3.org/XML/1998/namespace`, `xml:base`, r);
            break;
          case `xmlLang`:
            Rt(e, `http://www.w3.org/XML/1998/namespace`, `xml:lang`, r);
            break;
          case `xmlSpace`:
            Rt(e, `http://www.w3.org/XML/1998/namespace`, `xml:space`, r);
            break;
          case `is`:
            It(e, `is`, r);
            break;
          case `innerText`:
          case `textContent`:
            break;
          default:
            (!(2 < n.length) ||
              (n[0] !== `o` && n[0] !== `O`) ||
              (n[1] !== `n` && n[1] !== `N`)) &&
              ((n = an.get(n) || n), It(e, n, r));
        }
      }
      function Ld(e, t, n, r, a, o) {
        switch (n) {
          case `style`:
            nn(e, r, o);
            break;
          case `dangerouslySetInnerHTML`:
            if (r != null) {
              if (typeof r != `object` || !(`__html` in r)) throw Error(i(61));
              if (((n = r.__html), n != null)) {
                if (a.children != null) throw Error(i(60));
                e.innerHTML = n;
              }
            }
            break;
          case `children`:
            typeof r == `string`
              ? $t(e, r)
              : (typeof r == `number` || typeof r == `bigint`) && $t(e, `` + r);
            break;
          case `onScroll`:
            r != null && Y(`scroll`, e);
            break;
          case `onScrollEnd`:
            r != null && Y(`scrollend`, e);
            break;
          case `onClick`:
            r != null && (e.onclick = cn);
            break;
          case `suppressContentEditableWarning`:
          case `suppressHydrationWarning`:
          case `innerHTML`:
          case `ref`:
            break;
          case `innerText`:
          case `textContent`:
            break;
          default:
            if (!kt.hasOwnProperty(n))
              a: {
                if (
                  n[0] === `o` &&
                  n[1] === `n` &&
                  ((a = n.endsWith(`Capture`)),
                  (t = n.slice(2, a ? n.length - 7 : void 0)),
                  (o = e[ht] || null),
                  (o = o == null ? null : o[n]),
                  typeof o == `function` && e.removeEventListener(t, o, a),
                  typeof r == `function`)
                ) {
                  (typeof o != `function` &&
                    o !== null &&
                    (n in e
                      ? (e[n] = null)
                      : e.hasAttribute(n) && e.removeAttribute(n)),
                    e.addEventListener(t, r, a));
                  break a;
                }
                n in e
                  ? (e[n] = r)
                  : !0 === r
                    ? e.setAttribute(n, ``)
                    : It(e, n, r);
              }
        }
      }
      function Rd(e, t, n) {
        switch (t) {
          case `div`:
          case `span`:
          case `svg`:
          case `path`:
          case `a`:
          case `g`:
          case `p`:
          case `li`:
            break;
          case `img`:
            (Y(`error`, e), Y(`load`, e));
            var r = !1,
              a = !1,
              o;
            for (o in n)
              if (n.hasOwnProperty(o)) {
                var s = n[o];
                if (s != null)
                  switch (o) {
                    case `src`:
                      r = !0;
                      break;
                    case `srcSet`:
                      a = !0;
                      break;
                    case `children`:
                    case `dangerouslySetInnerHTML`:
                      throw Error(i(137, t));
                    default:
                      Id(e, t, o, s, n, null);
                  }
              }
            (a && Id(e, t, `srcSet`, n.srcSet, n, null),
              r && Id(e, t, `src`, n.src, n, null));
            return;
          case `input`:
            Y(`invalid`, e);
            var c = (o = s = a = null),
              l = null,
              u = null;
            for (r in n)
              if (n.hasOwnProperty(r)) {
                var d = n[r];
                if (d != null)
                  switch (r) {
                    case `name`:
                      a = d;
                      break;
                    case `type`:
                      s = d;
                      break;
                    case `checked`:
                      l = d;
                      break;
                    case `defaultChecked`:
                      u = d;
                      break;
                    case `value`:
                      o = d;
                      break;
                    case `defaultValue`:
                      c = d;
                      break;
                    case `children`:
                    case `dangerouslySetInnerHTML`:
                      if (d != null) throw Error(i(137, t));
                      break;
                    default:
                      Id(e, t, r, d, n, null);
                  }
              }
            Jt(e, o, c, l, u, s, a, !1);
            return;
          case `select`:
            for (a in (Y(`invalid`, e), (r = s = o = null), n))
              if (n.hasOwnProperty(a) && ((c = n[a]), c != null))
                switch (a) {
                  case `value`:
                    o = c;
                    break;
                  case `defaultValue`:
                    s = c;
                    break;
                  case `multiple`:
                    r = c;
                  default:
                    Id(e, t, a, c, n, null);
                }
            ((t = o),
              (n = s),
              (e.multiple = !!r),
              t == null ? n != null && Xt(e, !!r, n, !0) : Xt(e, !!r, t, !1));
            return;
          case `textarea`:
            for (s in (Y(`invalid`, e), (o = a = r = null), n))
              if (n.hasOwnProperty(s) && ((c = n[s]), c != null))
                switch (s) {
                  case `value`:
                    r = c;
                    break;
                  case `defaultValue`:
                    a = c;
                    break;
                  case `children`:
                    o = c;
                    break;
                  case `dangerouslySetInnerHTML`:
                    if (c != null) throw Error(i(91));
                    break;
                  default:
                    Id(e, t, s, c, n, null);
                }
            Qt(e, r, a, o);
            return;
          case `option`:
            for (l in n)
              if (n.hasOwnProperty(l) && ((r = n[l]), r != null))
                switch (l) {
                  case `selected`:
                    e.selected =
                      r && typeof r != `function` && typeof r != `symbol`;
                    break;
                  default:
                    Id(e, t, l, r, n, null);
                }
            return;
          case `dialog`:
            (Y(`beforetoggle`, e),
              Y(`toggle`, e),
              Y(`cancel`, e),
              Y(`close`, e));
            break;
          case `iframe`:
          case `object`:
            Y(`load`, e);
            break;
          case `video`:
          case `audio`:
            for (r = 0; r < bd.length; r++) Y(bd[r], e);
            break;
          case `image`:
            (Y(`error`, e), Y(`load`, e));
            break;
          case `details`:
            Y(`toggle`, e);
            break;
          case `embed`:
          case `source`:
          case `link`:
            (Y(`error`, e), Y(`load`, e));
          case `area`:
          case `base`:
          case `br`:
          case `col`:
          case `hr`:
          case `keygen`:
          case `meta`:
          case `param`:
          case `track`:
          case `wbr`:
          case `menuitem`:
            for (u in n)
              if (n.hasOwnProperty(u) && ((r = n[u]), r != null))
                switch (u) {
                  case `children`:
                  case `dangerouslySetInnerHTML`:
                    throw Error(i(137, t));
                  default:
                    Id(e, t, u, r, n, null);
                }
            return;
          default:
            if (rn(t)) {
              for (d in n)
                n.hasOwnProperty(d) &&
                  ((r = n[d]), r !== void 0 && Ld(e, t, d, r, n, void 0));
              return;
            }
        }
        for (c in n)
          n.hasOwnProperty(c) &&
            ((r = n[c]), r != null && Id(e, t, c, r, n, null));
      }
      function zd(e, t, n, r) {
        switch (t) {
          case `div`:
          case `span`:
          case `svg`:
          case `path`:
          case `a`:
          case `g`:
          case `p`:
          case `li`:
            break;
          case `input`:
            var a = null,
              o = null,
              s = null,
              c = null,
              l = null,
              u = null,
              d = null;
            for (m in n) {
              var f = n[m];
              if (n.hasOwnProperty(m) && f != null)
                switch (m) {
                  case `checked`:
                    break;
                  case `value`:
                    break;
                  case `defaultValue`:
                    l = f;
                  default:
                    r.hasOwnProperty(m) || Id(e, t, m, null, r, f);
                }
            }
            for (var p in r) {
              var m = r[p];
              if (((f = n[p]), r.hasOwnProperty(p) && (m != null || f != null)))
                switch (p) {
                  case `type`:
                    o = m;
                    break;
                  case `name`:
                    a = m;
                    break;
                  case `checked`:
                    u = m;
                    break;
                  case `defaultChecked`:
                    d = m;
                    break;
                  case `value`:
                    s = m;
                    break;
                  case `defaultValue`:
                    c = m;
                    break;
                  case `children`:
                  case `dangerouslySetInnerHTML`:
                    if (m != null) throw Error(i(137, t));
                    break;
                  default:
                    m !== f && Id(e, t, p, m, r, f);
                }
            }
            qt(e, s, c, l, u, d, o, a);
            return;
          case `select`:
            for (o in ((m = s = c = p = null), n))
              if (((l = n[o]), n.hasOwnProperty(o) && l != null))
                switch (o) {
                  case `value`:
                    break;
                  case `multiple`:
                    m = l;
                  default:
                    r.hasOwnProperty(o) || Id(e, t, o, null, r, l);
                }
            for (a in r)
              if (
                ((o = r[a]),
                (l = n[a]),
                r.hasOwnProperty(a) && (o != null || l != null))
              )
                switch (a) {
                  case `value`:
                    p = o;
                    break;
                  case `defaultValue`:
                    c = o;
                    break;
                  case `multiple`:
                    s = o;
                  default:
                    o !== l && Id(e, t, a, o, r, l);
                }
            ((t = c),
              (n = s),
              (r = m),
              p == null
                ? !!r != !!n &&
                  (t == null ? Xt(e, !!n, n ? [] : ``, !1) : Xt(e, !!n, t, !0))
                : Xt(e, !!n, p, !1));
            return;
          case `textarea`:
            for (c in ((m = p = null), n))
              if (
                ((a = n[c]),
                n.hasOwnProperty(c) && a != null && !r.hasOwnProperty(c))
              )
                switch (c) {
                  case `value`:
                    break;
                  case `children`:
                    break;
                  default:
                    Id(e, t, c, null, r, a);
                }
            for (s in r)
              if (
                ((a = r[s]),
                (o = n[s]),
                r.hasOwnProperty(s) && (a != null || o != null))
              )
                switch (s) {
                  case `value`:
                    p = a;
                    break;
                  case `defaultValue`:
                    m = a;
                    break;
                  case `children`:
                    break;
                  case `dangerouslySetInnerHTML`:
                    if (a != null) throw Error(i(91));
                    break;
                  default:
                    a !== o && Id(e, t, s, a, r, o);
                }
            Zt(e, p, m);
            return;
          case `option`:
            for (var h in n)
              if (
                ((p = n[h]),
                n.hasOwnProperty(h) && p != null && !r.hasOwnProperty(h))
              )
                switch (h) {
                  case `selected`:
                    e.selected = !1;
                    break;
                  default:
                    Id(e, t, h, null, r, p);
                }
            for (l in r)
              if (
                ((p = r[l]),
                (m = n[l]),
                r.hasOwnProperty(l) && p !== m && (p != null || m != null))
              )
                switch (l) {
                  case `selected`:
                    e.selected =
                      p && typeof p != `function` && typeof p != `symbol`;
                    break;
                  default:
                    Id(e, t, l, p, r, m);
                }
            return;
          case `img`:
          case `link`:
          case `area`:
          case `base`:
          case `br`:
          case `col`:
          case `embed`:
          case `hr`:
          case `keygen`:
          case `meta`:
          case `param`:
          case `source`:
          case `track`:
          case `wbr`:
          case `menuitem`:
            for (var g in n)
              ((p = n[g]),
                n.hasOwnProperty(g) &&
                  p != null &&
                  !r.hasOwnProperty(g) &&
                  Id(e, t, g, null, r, p));
            for (u in r)
              if (
                ((p = r[u]),
                (m = n[u]),
                r.hasOwnProperty(u) && p !== m && (p != null || m != null))
              )
                switch (u) {
                  case `children`:
                  case `dangerouslySetInnerHTML`:
                    if (p != null) throw Error(i(137, t));
                    break;
                  default:
                    Id(e, t, u, p, r, m);
                }
            return;
          default:
            if (rn(t)) {
              for (var _ in n)
                ((p = n[_]),
                  n.hasOwnProperty(_) &&
                    p !== void 0 &&
                    !r.hasOwnProperty(_) &&
                    Ld(e, t, _, void 0, r, p));
              for (d in r)
                ((p = r[d]),
                  (m = n[d]),
                  !r.hasOwnProperty(d) ||
                    p === m ||
                    (p === void 0 && m === void 0) ||
                    Ld(e, t, d, p, r, m));
              return;
            }
        }
        for (var v in n)
          ((p = n[v]),
            n.hasOwnProperty(v) &&
              p != null &&
              !r.hasOwnProperty(v) &&
              Id(e, t, v, null, r, p));
        for (f in r)
          ((p = r[f]),
            (m = n[f]),
            !r.hasOwnProperty(f) ||
              p === m ||
              (p == null && m == null) ||
              Id(e, t, f, p, r, m));
      }
      function Bd(e) {
        switch (e) {
          case `css`:
          case `script`:
          case `font`:
          case `img`:
          case `image`:
          case `input`:
          case `link`:
            return !0;
          default:
            return !1;
        }
      }
      function Vd() {
        if (typeof performance.getEntriesByType == `function`) {
          for (
            var e = 0,
              t = 0,
              n = performance.getEntriesByType(`resource`),
              r = 0;
            r < n.length;
            r++
          ) {
            var i = n[r],
              a = i.transferSize,
              o = i.initiatorType,
              s = i.duration;
            if (a && s && Bd(o)) {
              for (o = 0, s = i.responseEnd, r += 1; r < n.length; r++) {
                var c = n[r],
                  l = c.startTime;
                if (l > s) break;
                var u = c.transferSize,
                  d = c.initiatorType;
                u &&
                  Bd(d) &&
                  ((c = c.responseEnd),
                  (o += u * (c < s ? 1 : (s - l) / (c - l))));
              }
              if ((--r, (t += (8 * (a + o)) / (i.duration / 1e3)), e++, 10 < e))
                break;
            }
          }
          if (0 < e) return t / e / 1e6;
        }
        return navigator.connection &&
          ((e = navigator.connection.downlink), typeof e == `number`)
          ? e
          : 5;
      }
      var Hd = null,
        Ud = null;
      function Wd(e) {
        return e.nodeType === 9 ? e : e.ownerDocument;
      }
      function Gd(e) {
        switch (e) {
          case `http://www.w3.org/2000/svg`:
            return 1;
          case `http://www.w3.org/1998/Math/MathML`:
            return 2;
          default:
            return 0;
        }
      }
      function Kd(e, t) {
        if (e === 0)
          switch (t) {
            case `svg`:
              return 1;
            case `math`:
              return 2;
            default:
              return 0;
          }
        return e === 1 && t === `foreignObject` ? 0 : e;
      }
      function qd(e, t) {
        return (
          e === `textarea` ||
          e === `noscript` ||
          typeof t.children == `string` ||
          typeof t.children == `number` ||
          typeof t.children == `bigint` ||
          (typeof t.dangerouslySetInnerHTML == `object` &&
            t.dangerouslySetInnerHTML !== null &&
            t.dangerouslySetInnerHTML.__html != null)
        );
      }
      var Jd = null;
      function Yd() {
        var e = window.event;
        return e && e.type === `popstate`
          ? e === Jd
            ? !1
            : ((Jd = e), !0)
          : ((Jd = null), !1);
      }
      var Xd = typeof setTimeout == `function` ? setTimeout : void 0,
        Zd = typeof clearTimeout == `function` ? clearTimeout : void 0,
        Qd = typeof Promise == `function` ? Promise : void 0,
        $d =
          typeof queueMicrotask == `function`
            ? queueMicrotask
            : Qd === void 0
              ? Xd
              : function (e) {
                  return Qd.resolve(null).then(e).catch(ef);
                };
      function ef(e) {
        setTimeout(function () {
          throw e;
        });
      }
      function tf(e) {
        return e === `head`;
      }
      function nf(e, t) {
        var n = t,
          r = 0;
        do {
          var i = n.nextSibling;
          if ((e.removeChild(n), i && i.nodeType === 8))
            if (((n = i.data), n === `/$` || n === `/&`)) {
              if (r === 0) {
                (e.removeChild(i), Lp(t));
                return;
              }
              r--;
            } else if (
              n === `$` ||
              n === `$?` ||
              n === `$~` ||
              n === `$!` ||
              n === `&`
            )
              r++;
            else if (n === `html`) _f(e.ownerDocument.documentElement);
            else if (n === `head`) {
              ((n = e.ownerDocument.head), _f(n));
              for (var a = n.firstChild; a; ) {
                var o = a.nextSibling,
                  s = a.nodeName;
                (a[xt] ||
                  s === `SCRIPT` ||
                  s === `STYLE` ||
                  (s === `LINK` && a.rel.toLowerCase() === `stylesheet`) ||
                  n.removeChild(a),
                  (a = o));
              }
            } else n === `body` && _f(e.ownerDocument.body);
          n = i;
        } while (n);
        Lp(t);
      }
      function rf(e, t) {
        var n = e;
        e = 0;
        do {
          var r = n.nextSibling;
          if (
            (n.nodeType === 1
              ? t
                ? ((n._stashedDisplay = n.style.display),
                  (n.style.display = `none`))
                : ((n.style.display = n._stashedDisplay || ``),
                  n.getAttribute(`style`) === `` && n.removeAttribute(`style`))
              : n.nodeType === 3 &&
                (t
                  ? ((n._stashedText = n.nodeValue), (n.nodeValue = ``))
                  : (n.nodeValue = n._stashedText || ``)),
            r && r.nodeType === 8)
          )
            if (((n = r.data), n === `/$`)) {
              if (e === 0) break;
              e--;
            } else (n !== `$` && n !== `$?` && n !== `$~` && n !== `$!`) || e++;
          n = r;
        } while (n);
      }
      function af(e) {
        var t = e.firstChild;
        for (t && t.nodeType === 10 && (t = t.nextSibling); t; ) {
          var n = t;
          switch (((t = t.nextSibling), n.nodeName)) {
            case `HTML`:
            case `HEAD`:
            case `BODY`:
              (af(n), St(n));
              continue;
            case `SCRIPT`:
            case `STYLE`:
              continue;
            case `LINK`:
              if (n.rel.toLowerCase() === `stylesheet`) continue;
          }
          e.removeChild(n);
        }
      }
      function of(e, t, n, r) {
        for (; e.nodeType === 1; ) {
          var i = n;
          if (e.nodeName.toLowerCase() !== t.toLowerCase()) {
            if (!r && (e.nodeName !== `INPUT` || e.type !== `hidden`)) break;
          } else if (!r)
            if (t === `input` && e.type === `hidden`) {
              var a = i.name == null ? null : `` + i.name;
              if (i.type === `hidden` && e.getAttribute(`name`) === a) return e;
            } else return e;
          else if (!e[xt])
            switch (t) {
              case `meta`:
                if (!e.hasAttribute(`itemprop`)) break;
                return e;
              case `link`:
                if (
                  ((a = e.getAttribute(`rel`)),
                  (a === `stylesheet` && e.hasAttribute(`data-precedence`)) ||
                    a !== i.rel ||
                    e.getAttribute(`href`) !==
                      (i.href == null || i.href === `` ? null : i.href) ||
                    e.getAttribute(`crossorigin`) !==
                      (i.crossOrigin == null ? null : i.crossOrigin) ||
                    e.getAttribute(`title`) !==
                      (i.title == null ? null : i.title))
                )
                  break;
                return e;
              case `style`:
                if (e.hasAttribute(`data-precedence`)) break;
                return e;
              case `script`:
                if (
                  ((a = e.getAttribute(`src`)),
                  (a !== (i.src == null ? null : i.src) ||
                    e.getAttribute(`type`) !==
                      (i.type == null ? null : i.type) ||
                    e.getAttribute(`crossorigin`) !==
                      (i.crossOrigin == null ? null : i.crossOrigin)) &&
                    a &&
                    e.hasAttribute(`async`) &&
                    !e.hasAttribute(`itemprop`))
                )
                  break;
                return e;
              default:
                return e;
            }
          if (((e = ff(e.nextSibling)), e === null)) break;
        }
        return null;
      }
      function sf(e, t, n) {
        if (t === ``) return null;
        for (; e.nodeType !== 3; )
          if (
            ((e.nodeType !== 1 ||
              e.nodeName !== `INPUT` ||
              e.type !== `hidden`) &&
              !n) ||
            ((e = ff(e.nextSibling)), e === null)
          )
            return null;
        return e;
      }
      function cf(e, t) {
        for (; e.nodeType !== 8; )
          if (
            ((e.nodeType !== 1 ||
              e.nodeName !== `INPUT` ||
              e.type !== `hidden`) &&
              !t) ||
            ((e = ff(e.nextSibling)), e === null)
          )
            return null;
        return e;
      }
      function lf(e) {
        return e.data === `$?` || e.data === `$~`;
      }
      function uf(e) {
        return (
          e.data === `$!` ||
          (e.data === `$?` && e.ownerDocument.readyState !== `loading`)
        );
      }
      function df(e, t) {
        var n = e.ownerDocument;
        if (e.data === `$~`) e._reactRetry = t;
        else if (e.data !== `$?` || n.readyState !== `loading`) t();
        else {
          var r = function () {
            (t(), n.removeEventListener(`DOMContentLoaded`, r));
          };
          (n.addEventListener(`DOMContentLoaded`, r), (e._reactRetry = r));
        }
      }
      function ff(e) {
        for (; e != null; e = e.nextSibling) {
          var t = e.nodeType;
          if (t === 1 || t === 3) break;
          if (t === 8) {
            if (
              ((t = e.data),
              t === `$` ||
                t === `$!` ||
                t === `$?` ||
                t === `$~` ||
                t === `&` ||
                t === `F!` ||
                t === `F`)
            )
              break;
            if (t === `/$` || t === `/&`) return null;
          }
        }
        return e;
      }
      var pf = null;
      function mf(e) {
        e = e.nextSibling;
        for (var t = 0; e; ) {
          if (e.nodeType === 8) {
            var n = e.data;
            if (n === `/$` || n === `/&`) {
              if (t === 0) return ff(e.nextSibling);
              t--;
            } else
              (n !== `$` &&
                n !== `$!` &&
                n !== `$?` &&
                n !== `$~` &&
                n !== `&`) ||
                t++;
          }
          e = e.nextSibling;
        }
        return null;
      }
      function hf(e) {
        e = e.previousSibling;
        for (var t = 0; e; ) {
          if (e.nodeType === 8) {
            var n = e.data;
            if (
              n === `$` ||
              n === `$!` ||
              n === `$?` ||
              n === `$~` ||
              n === `&`
            ) {
              if (t === 0) return e;
              t--;
            } else (n !== `/$` && n !== `/&`) || t++;
          }
          e = e.previousSibling;
        }
        return null;
      }
      function gf(e, t, n) {
        switch (((t = Wd(n)), e)) {
          case `html`:
            if (((e = t.documentElement), !e)) throw Error(i(452));
            return e;
          case `head`:
            if (((e = t.head), !e)) throw Error(i(453));
            return e;
          case `body`:
            if (((e = t.body), !e)) throw Error(i(454));
            return e;
          default:
            throw Error(i(451));
        }
      }
      function _f(e) {
        for (var t = e.attributes; t.length; ) e.removeAttributeNode(t[0]);
        St(e);
      }
      var vf = new Map(),
        yf = new Set();
      function bf(e) {
        return typeof e.getRootNode == `function`
          ? e.getRootNode()
          : e.nodeType === 9
            ? e
            : e.ownerDocument;
      }
      var xf = D.d;
      D.d = { f: Sf, r: Cf, D: Ef, C: Df, L: Of, m: kf, X: jf, S: Af, M: Mf };
      function Sf() {
        var e = xf.f(),
          t = Su();
        return e || t;
      }
      function Cf(e) {
        var t = wt(e);
        t !== null && t.tag === 5 && t.type === `form` ? Ds(t) : xf.r(e);
      }
      var wf = typeof document > `u` ? null : document;
      function Tf(e, t, n) {
        var r = wf;
        if (r && typeof t == `string` && t) {
          var i = Kt(t);
          ((i = `link[rel="` + e + `"][href="` + i + `"]`),
            typeof n == `string` && (i += `[crossorigin="` + n + `"]`),
            yf.has(i) ||
              (yf.add(i),
              (e = { rel: e, crossOrigin: n, href: t }),
              r.querySelector(i) === null &&
                ((t = r.createElement(`link`)),
                Rd(t, `link`, e),
                Dt(t),
                r.head.appendChild(t))));
        }
      }
      function Ef(e) {
        (xf.D(e), Tf(`dns-prefetch`, e, null));
      }
      function Df(e, t) {
        (xf.C(e, t), Tf(`preconnect`, e, t));
      }
      function Of(e, t, n) {
        xf.L(e, t, n);
        var r = wf;
        if (r && e && t) {
          var i = `link[rel="preload"][as="` + Kt(t) + `"]`;
          t === `image` && n && n.imageSrcSet
            ? ((i += `[imagesrcset="` + Kt(n.imageSrcSet) + `"]`),
              typeof n.imageSizes == `string` &&
                (i += `[imagesizes="` + Kt(n.imageSizes) + `"]`))
            : (i += `[href="` + Kt(e) + `"]`);
          var a = i;
          switch (t) {
            case `style`:
              a = Pf(e);
              break;
            case `script`:
              a = Rf(e);
          }
          vf.has(a) ||
            ((e = f(
              {
                rel: `preload`,
                href: t === `image` && n && n.imageSrcSet ? void 0 : e,
                as: t,
              },
              n,
            )),
            vf.set(a, e),
            r.querySelector(i) !== null ||
              (t === `style` && r.querySelector(Ff(a))) ||
              (t === `script` && r.querySelector(zf(a))) ||
              ((t = r.createElement(`link`)),
              Rd(t, `link`, e),
              Dt(t),
              r.head.appendChild(t)));
        }
      }
      function kf(e, t) {
        xf.m(e, t);
        var n = wf;
        if (n && e) {
          var r = t && typeof t.as == `string` ? t.as : `script`,
            i =
              `link[rel="modulepreload"][as="` +
              Kt(r) +
              `"][href="` +
              Kt(e) +
              `"]`,
            a = i;
          switch (r) {
            case `audioworklet`:
            case `paintworklet`:
            case `serviceworker`:
            case `sharedworker`:
            case `worker`:
            case `script`:
              a = Rf(e);
          }
          if (
            !vf.has(a) &&
            ((e = f({ rel: `modulepreload`, href: e }, t)),
            vf.set(a, e),
            n.querySelector(i) === null)
          ) {
            switch (r) {
              case `audioworklet`:
              case `paintworklet`:
              case `serviceworker`:
              case `sharedworker`:
              case `worker`:
              case `script`:
                if (n.querySelector(zf(a))) return;
            }
            ((r = n.createElement(`link`)),
              Rd(r, `link`, e),
              Dt(r),
              n.head.appendChild(r));
          }
        }
      }
      function Af(e, t, n) {
        xf.S(e, t, n);
        var r = wf;
        if (r && e) {
          var i = Et(r).hoistableStyles,
            a = Pf(e);
          t ||= `default`;
          var o = i.get(a);
          if (!o) {
            var s = { loading: 0, preload: null };
            if ((o = r.querySelector(Ff(a)))) s.loading = 5;
            else {
              ((e = f({ rel: `stylesheet`, href: e, "data-precedence": t }, n)),
                (n = vf.get(a)) && Hf(e, n));
              var c = (o = r.createElement(`link`));
              (Dt(c),
                Rd(c, `link`, e),
                (c._p = new Promise(function (e, t) {
                  ((c.onload = e), (c.onerror = t));
                })),
                c.addEventListener(`load`, function () {
                  s.loading |= 1;
                }),
                c.addEventListener(`error`, function () {
                  s.loading |= 2;
                }),
                (s.loading |= 4),
                Vf(o, t, r));
            }
            ((o = { type: `stylesheet`, instance: o, count: 1, state: s }),
              i.set(a, o));
          }
        }
      }
      function jf(e, t) {
        xf.X(e, t);
        var n = wf;
        if (n && e) {
          var r = Et(n).hoistableScripts,
            i = Rf(e),
            a = r.get(i);
          a ||
            ((a = n.querySelector(zf(i))),
            a ||
              ((e = f({ src: e, async: !0 }, t)),
              (t = vf.get(i)) && Uf(e, t),
              (a = n.createElement(`script`)),
              Dt(a),
              Rd(a, `link`, e),
              n.head.appendChild(a)),
            (a = { type: `script`, instance: a, count: 1, state: null }),
            r.set(i, a));
        }
      }
      function Mf(e, t) {
        xf.M(e, t);
        var n = wf;
        if (n && e) {
          var r = Et(n).hoistableScripts,
            i = Rf(e),
            a = r.get(i);
          a ||
            ((a = n.querySelector(zf(i))),
            a ||
              ((e = f({ src: e, async: !0, type: `module` }, t)),
              (t = vf.get(i)) && Uf(e, t),
              (a = n.createElement(`script`)),
              Dt(a),
              Rd(a, `link`, e),
              n.head.appendChild(a)),
            (a = { type: `script`, instance: a, count: 1, state: null }),
            r.set(i, a));
        }
      }
      function Nf(e, t, n, r) {
        var a = (a = ge.current) ? bf(a) : null;
        if (!a) throw Error(i(446));
        switch (e) {
          case `meta`:
          case `title`:
            return null;
          case `style`:
            return typeof n.precedence == `string` && typeof n.href == `string`
              ? ((t = Pf(n.href)),
                (n = Et(a).hoistableStyles),
                (r = n.get(t)),
                r ||
                  ((r = {
                    type: `style`,
                    instance: null,
                    count: 0,
                    state: null,
                  }),
                  n.set(t, r)),
                r)
              : { type: `void`, instance: null, count: 0, state: null };
          case `link`:
            if (
              n.rel === `stylesheet` &&
              typeof n.href == `string` &&
              typeof n.precedence == `string`
            ) {
              e = Pf(n.href);
              var o = Et(a).hoistableStyles,
                s = o.get(e);
              if (
                (s ||
                  ((a = a.ownerDocument || a),
                  (s = {
                    type: `stylesheet`,
                    instance: null,
                    count: 0,
                    state: { loading: 0, preload: null },
                  }),
                  o.set(e, s),
                  (o = a.querySelector(Ff(e))) &&
                    !o._p &&
                    ((s.instance = o), (s.state.loading = 5)),
                  vf.has(e) ||
                    ((n = {
                      rel: `preload`,
                      as: `style`,
                      href: n.href,
                      crossOrigin: n.crossOrigin,
                      integrity: n.integrity,
                      media: n.media,
                      hrefLang: n.hrefLang,
                      referrerPolicy: n.referrerPolicy,
                    }),
                    vf.set(e, n),
                    o || Lf(a, e, n, s.state))),
                t && r === null)
              )
                throw Error(i(528, ``));
              return s;
            }
            if (t && r !== null) throw Error(i(529, ``));
            return null;
          case `script`:
            return (
              (t = n.async),
              (n = n.src),
              typeof n == `string` &&
              t &&
              typeof t != `function` &&
              typeof t != `symbol`
                ? ((t = Rf(n)),
                  (n = Et(a).hoistableScripts),
                  (r = n.get(t)),
                  r ||
                    ((r = {
                      type: `script`,
                      instance: null,
                      count: 0,
                      state: null,
                    }),
                    n.set(t, r)),
                  r)
                : { type: `void`, instance: null, count: 0, state: null }
            );
          default:
            throw Error(i(444, e));
        }
      }
      function Pf(e) {
        return `href="` + Kt(e) + `"`;
      }
      function Ff(e) {
        return `link[rel="stylesheet"][` + e + `]`;
      }
      function If(e) {
        return f({}, e, { "data-precedence": e.precedence, precedence: null });
      }
      function Lf(e, t, n, r) {
        e.querySelector(`link[rel="preload"][as="style"][` + t + `]`)
          ? (r.loading = 1)
          : ((t = e.createElement(`link`)),
            (r.preload = t),
            t.addEventListener(`load`, function () {
              return (r.loading |= 1);
            }),
            t.addEventListener(`error`, function () {
              return (r.loading |= 2);
            }),
            Rd(t, `link`, n),
            Dt(t),
            e.head.appendChild(t));
      }
      function Rf(e) {
        return `[src="` + Kt(e) + `"]`;
      }
      function zf(e) {
        return `script[async]` + e;
      }
      function Bf(e, t, n) {
        if ((t.count++, t.instance === null))
          switch (t.type) {
            case `style`:
              var r = e.querySelector(`style[data-href~="` + Kt(n.href) + `"]`);
              if (r) return ((t.instance = r), Dt(r), r);
              var a = f({}, n, {
                "data-href": n.href,
                "data-precedence": n.precedence,
                href: null,
                precedence: null,
              });
              return (
                (r = (e.ownerDocument || e).createElement(`style`)),
                Dt(r),
                Rd(r, `style`, a),
                Vf(r, n.precedence, e),
                (t.instance = r)
              );
            case `stylesheet`:
              a = Pf(n.href);
              var o = e.querySelector(Ff(a));
              if (o)
                return ((t.state.loading |= 4), (t.instance = o), Dt(o), o);
              ((r = If(n)),
                (a = vf.get(a)) && Hf(r, a),
                (o = (e.ownerDocument || e).createElement(`link`)),
                Dt(o));
              var s = o;
              return (
                (s._p = new Promise(function (e, t) {
                  ((s.onload = e), (s.onerror = t));
                })),
                Rd(o, `link`, r),
                (t.state.loading |= 4),
                Vf(o, n.precedence, e),
                (t.instance = o)
              );
            case `script`:
              return (
                (o = Rf(n.src)),
                (a = e.querySelector(zf(o)))
                  ? ((t.instance = a), Dt(a), a)
                  : ((r = n),
                    (a = vf.get(o)) && ((r = f({}, n)), Uf(r, a)),
                    (e = e.ownerDocument || e),
                    (a = e.createElement(`script`)),
                    Dt(a),
                    Rd(a, `link`, r),
                    e.head.appendChild(a),
                    (t.instance = a))
              );
            case `void`:
              return null;
            default:
              throw Error(i(443, t.type));
          }
        else
          t.type === `stylesheet` &&
            !(t.state.loading & 4) &&
            ((r = t.instance), (t.state.loading |= 4), Vf(r, n.precedence, e));
        return t.instance;
      }
      function Vf(e, t, n) {
        for (
          var r = n.querySelectorAll(
              `link[rel="stylesheet"][data-precedence],style[data-precedence]`,
            ),
            i = r.length ? r[r.length - 1] : null,
            a = i,
            o = 0;
          o < r.length;
          o++
        ) {
          var s = r[o];
          if (s.dataset.precedence === t) a = s;
          else if (a !== i) break;
        }
        a
          ? a.parentNode.insertBefore(e, a.nextSibling)
          : ((t = n.nodeType === 9 ? n.head : n),
            t.insertBefore(e, t.firstChild));
      }
      function Hf(e, t) {
        ((e.crossOrigin ??= t.crossOrigin),
          (e.referrerPolicy ??= t.referrerPolicy),
          (e.title ??= t.title));
      }
      function Uf(e, t) {
        ((e.crossOrigin ??= t.crossOrigin),
          (e.referrerPolicy ??= t.referrerPolicy),
          (e.integrity ??= t.integrity));
      }
      var Wf = null;
      function Gf(e, t, n) {
        if (Wf === null) {
          var r = new Map(),
            i = (Wf = new Map());
          i.set(n, r);
        } else ((i = Wf), (r = i.get(n)), r || ((r = new Map()), i.set(n, r)));
        if (r.has(e)) return r;
        for (
          r.set(e, null), n = n.getElementsByTagName(e), i = 0;
          i < n.length;
          i++
        ) {
          var a = n[i];
          if (
            !(
              a[xt] ||
              a[mt] ||
              (e === `link` && a.getAttribute(`rel`) === `stylesheet`)
            ) &&
            a.namespaceURI !== `http://www.w3.org/2000/svg`
          ) {
            var o = a.getAttribute(t) || ``;
            o = e + o;
            var s = r.get(o);
            s ? s.push(a) : r.set(o, [a]);
          }
        }
        return r;
      }
      function Kf(e, t, n) {
        ((e = e.ownerDocument || e),
          e.head.insertBefore(
            n,
            t === `title` ? e.querySelector(`head > title`) : null,
          ));
      }
      function qf(e, t, n) {
        if (n === 1 || t.itemProp != null) return !1;
        switch (e) {
          case `meta`:
          case `title`:
            return !0;
          case `style`:
            if (
              typeof t.precedence != `string` ||
              typeof t.href != `string` ||
              t.href === ``
            )
              break;
            return !0;
          case `link`:
            if (
              typeof t.rel != `string` ||
              typeof t.href != `string` ||
              t.href === `` ||
              t.onLoad ||
              t.onError
            )
              break;
            switch (t.rel) {
              case `stylesheet`:
                return (
                  (e = t.disabled), typeof t.precedence == `string` && e == null
                );
              default:
                return !0;
            }
          case `script`:
            if (
              t.async &&
              typeof t.async != `function` &&
              typeof t.async != `symbol` &&
              !t.onLoad &&
              !t.onError &&
              t.src &&
              typeof t.src == `string`
            )
              return !0;
        }
        return !1;
      }
      function Jf(e) {
        return !(e.type === `stylesheet` && !(e.state.loading & 3));
      }
      function Yf(e, t, n, r) {
        if (
          n.type === `stylesheet` &&
          (typeof r.media != `string` || !1 !== matchMedia(r.media).matches) &&
          !(n.state.loading & 4)
        ) {
          if (n.instance === null) {
            var i = Pf(r.href),
              a = t.querySelector(Ff(i));
            if (a) {
              ((t = a._p),
                typeof t == `object` &&
                  t &&
                  typeof t.then == `function` &&
                  (e.count++, (e = Qf.bind(e)), t.then(e, e)),
                (n.state.loading |= 4),
                (n.instance = a),
                Dt(a));
              return;
            }
            ((a = t.ownerDocument || t),
              (r = If(r)),
              (i = vf.get(i)) && Hf(r, i),
              (a = a.createElement(`link`)),
              Dt(a));
            var o = a;
            ((o._p = new Promise(function (e, t) {
              ((o.onload = e), (o.onerror = t));
            })),
              Rd(a, `link`, r),
              (n.instance = a));
          }
          (e.stylesheets === null && (e.stylesheets = new Map()),
            e.stylesheets.set(n, t),
            (t = n.state.preload) &&
              !(n.state.loading & 3) &&
              (e.count++,
              (n = Qf.bind(e)),
              t.addEventListener(`load`, n),
              t.addEventListener(`error`, n)));
        }
      }
      var Xf = 0;
      function Zf(e, t) {
        return (
          e.stylesheets && e.count === 0 && ep(e, e.stylesheets),
          0 < e.count || 0 < e.imgCount
            ? function (n) {
                var r = setTimeout(function () {
                  if ((e.stylesheets && ep(e, e.stylesheets), e.unsuspend)) {
                    var t = e.unsuspend;
                    ((e.unsuspend = null), t());
                  }
                }, 6e4 + t);
                0 < e.imgBytes && Xf === 0 && (Xf = 62500 * Vd());
                var i = setTimeout(
                  function () {
                    if (
                      ((e.waitingForImages = !1),
                      e.count === 0 &&
                        (e.stylesheets && ep(e, e.stylesheets), e.unsuspend))
                    ) {
                      var t = e.unsuspend;
                      ((e.unsuspend = null), t());
                    }
                  },
                  (e.imgBytes > Xf ? 50 : 800) + t,
                );
                return (
                  (e.unsuspend = n),
                  function () {
                    ((e.unsuspend = null), clearTimeout(r), clearTimeout(i));
                  }
                );
              }
            : null
        );
      }
      function Qf() {
        if (
          (this.count--,
          this.count === 0 && (this.imgCount === 0 || !this.waitingForImages))
        ) {
          if (this.stylesheets) ep(this, this.stylesheets);
          else if (this.unsuspend) {
            var e = this.unsuspend;
            ((this.unsuspend = null), e());
          }
        }
      }
      var $f = null;
      function ep(e, t) {
        ((e.stylesheets = null),
          e.unsuspend !== null &&
            (e.count++,
            ($f = new Map()),
            t.forEach(tp, e),
            ($f = null),
            Qf.call(e)));
      }
      function tp(e, t) {
        if (!(t.state.loading & 4)) {
          var n = $f.get(e);
          if (n) var r = n.get(null);
          else {
            ((n = new Map()), $f.set(e, n));
            for (
              var i = e.querySelectorAll(
                  `link[data-precedence],style[data-precedence]`,
                ),
                a = 0;
              a < i.length;
              a++
            ) {
              var o = i[a];
              (o.nodeName === `LINK` ||
                o.getAttribute(`media`) !== `not all`) &&
                (n.set(o.dataset.precedence, o), (r = o));
            }
            r && n.set(null, r);
          }
          ((i = t.instance),
            (o = i.getAttribute(`data-precedence`)),
            (a = n.get(o) || r),
            a === r && n.set(null, i),
            n.set(o, i),
            this.count++,
            (r = Qf.bind(this)),
            i.addEventListener(`load`, r),
            i.addEventListener(`error`, r),
            a
              ? a.parentNode.insertBefore(i, a.nextSibling)
              : ((e = e.nodeType === 9 ? e.head : e),
                e.insertBefore(i, e.firstChild)),
            (t.state.loading |= 4));
        }
      }
      var np = {
        $$typeof: S,
        Provider: null,
        Consumer: null,
        _currentValue: ue,
        _currentValue2: ue,
        _threadCount: 0,
      };
      function rp(e, t, n, r, i, a, o, s, c) {
        ((this.tag = 1),
          (this.containerInfo = e),
          (this.pingCache = this.current = this.pendingChildren = null),
          (this.timeoutHandle = -1),
          (this.callbackNode =
            this.next =
            this.pendingContext =
            this.context =
            this.cancelPendingCommit =
              null),
          (this.callbackPriority = 0),
          (this.expirationTimes = rt(-1)),
          (this.entangledLanes =
            this.shellSuspendCounter =
            this.errorRecoveryDisabledLanes =
            this.expiredLanes =
            this.warmLanes =
            this.pingedLanes =
            this.suspendedLanes =
            this.pendingLanes =
              0),
          (this.entanglements = rt(0)),
          (this.hiddenUpdates = rt(null)),
          (this.identifierPrefix = r),
          (this.onUncaughtError = i),
          (this.onCaughtError = a),
          (this.onRecoverableError = o),
          (this.pooledCache = null),
          (this.pooledCacheLanes = 0),
          (this.formState = c),
          (this.incompleteTransitions = new Map()));
      }
      function ip(e, t, n, r, i, a, o, s, c, l, u, d) {
        return (
          (e = new rp(e, t, n, o, c, l, u, d, s)),
          (t = 1),
          !0 === a && (t |= 24),
          (a = gi(3, null, null, t)),
          (e.current = a),
          (a.stateNode = e),
          (t = ga()),
          t.refCount++,
          (e.pooledCache = t),
          t.refCount++,
          (a.memoizedState = { element: r, isDehydrated: n, cache: t }),
          qa(a),
          e
        );
      }
      function ap(e) {
        return e ? ((e = mi), e) : mi;
      }
      function op(e, t, n, r, i, a) {
        ((i = ap(i)),
          r.context === null ? (r.context = i) : (r.pendingContext = i),
          (r = Ya(t)),
          (r.payload = { element: n }),
          (a = a === void 0 ? null : a),
          a !== null && (r.callback = a),
          (n = P(e, r, t)),
          n !== null && (_u(n, e, t), Xa(n, e, t)));
      }
      function sp(e, t) {
        if (((e = e.memoizedState), e !== null && e.dehydrated !== null)) {
          var n = e.retryLane;
          e.retryLane = n !== 0 && n < t ? n : t;
        }
      }
      function cp(e, t) {
        (sp(e, t), (e = e.alternate) && sp(e, t));
      }
      function lp(e) {
        if (e.tag === 13 || e.tag === 31) {
          var t = di(e, 67108864);
          (t !== null && _u(t, e, 67108864), cp(e, 67108864));
        }
      }
      function up(e) {
        if (e.tag === 13 || e.tag === 31) {
          var t = hu();
          t = lt(t);
          var n = di(e, t);
          (n !== null && _u(n, e, t), cp(e, t));
        }
      }
      var dp = !0;
      function fp(e, t, n, r) {
        var i = E.T;
        E.T = null;
        var a = D.p;
        try {
          ((D.p = 2), mp(e, t, n, r));
        } finally {
          ((D.p = a), (E.T = i));
        }
      }
      function pp(e, t, n, r) {
        var i = E.T;
        E.T = null;
        var a = D.p;
        try {
          ((D.p = 8), mp(e, t, n, r));
        } finally {
          ((D.p = a), (E.T = i));
        }
      }
      function mp(e, t, n, r) {
        if (dp) {
          var i = hp(r);
          if (i === null) (Dd(e, t, r, gp, n), Dp(e, r));
          else if (kp(i, e, t, n, r)) r.stopPropagation();
          else if ((Dp(e, r), t & 4 && -1 < Ep.indexOf(e))) {
            for (; i !== null; ) {
              var a = wt(i);
              if (a !== null)
                switch (a.tag) {
                  case 3:
                    if (
                      ((a = a.stateNode), a.current.memoizedState.isDehydrated)
                    ) {
                      var o = Qe(a.pendingLanes);
                      if (o !== 0) {
                        var s = a;
                        for (s.pendingLanes |= 2, s.entangledLanes |= 2; o; ) {
                          var c = 1 << (31 - Ge(o));
                          ((s.entanglements[1] |= c), (o &= ~c));
                        }
                        (od(a), !(K & 6) && ((ru = Ne() + 500), sd(0, !1)));
                      }
                    }
                    break;
                  case 31:
                  case 13:
                    ((s = di(a, 2)), s !== null && _u(s, a, 2), Su(), cp(a, 2));
                }
              if (((a = hp(r)), a === null && Dd(e, t, r, gp, n), a === i))
                break;
              i = a;
            }
            i !== null && r.stopPropagation();
          } else Dd(e, t, r, null, n);
        }
      }
      function hp(e) {
        return ((e = un(e)), _p(e));
      }
      var gp = null;
      function _p(e) {
        if (((gp = null), (e = Ct(e)), e !== null)) {
          var t = o(e);
          if (t === null) e = null;
          else {
            var n = t.tag;
            if (n === 13) {
              if (((e = s(t)), e !== null)) return e;
              e = null;
            } else if (n === 31) {
              if (((e = c(t)), e !== null)) return e;
              e = null;
            } else if (n === 3) {
              if (t.stateNode.current.memoizedState.isDehydrated)
                return t.tag === 3 ? t.stateNode.containerInfo : null;
              e = null;
            } else t !== e && (e = null);
          }
        }
        return ((gp = e), null);
      }
      function vp(e) {
        switch (e) {
          case `beforetoggle`:
          case `cancel`:
          case `click`:
          case `close`:
          case `contextmenu`:
          case `copy`:
          case `cut`:
          case `auxclick`:
          case `dblclick`:
          case `dragend`:
          case `dragstart`:
          case `drop`:
          case `focusin`:
          case `focusout`:
          case `input`:
          case `invalid`:
          case `keydown`:
          case `keypress`:
          case `keyup`:
          case `mousedown`:
          case `mouseup`:
          case `paste`:
          case `pause`:
          case `play`:
          case `pointercancel`:
          case `pointerdown`:
          case `pointerup`:
          case `ratechange`:
          case `reset`:
          case `resize`:
          case `seeked`:
          case `submit`:
          case `toggle`:
          case `touchcancel`:
          case `touchend`:
          case `touchstart`:
          case `volumechange`:
          case `change`:
          case `selectionchange`:
          case `textInput`:
          case `compositionstart`:
          case `compositionend`:
          case `compositionupdate`:
          case `beforeblur`:
          case `afterblur`:
          case `beforeinput`:
          case `blur`:
          case `fullscreenchange`:
          case `focus`:
          case `hashchange`:
          case `popstate`:
          case `select`:
          case `selectstart`:
            return 2;
          case `drag`:
          case `dragenter`:
          case `dragexit`:
          case `dragleave`:
          case `dragover`:
          case `mousemove`:
          case `mouseout`:
          case `mouseover`:
          case `pointermove`:
          case `pointerout`:
          case `pointerover`:
          case `scroll`:
          case `touchmove`:
          case `wheel`:
          case `mouseenter`:
          case `mouseleave`:
          case `pointerenter`:
          case `pointerleave`:
            return 8;
          case `message`:
            switch (Pe()) {
              case Fe:
                return 2;
              case Ie:
                return 8;
              case Le:
              case Re:
                return 32;
              case ze:
                return 268435456;
              default:
                return 32;
            }
          default:
            return 32;
        }
      }
      var yp = !1,
        bp = null,
        xp = null,
        Sp = null,
        Cp = new Map(),
        wp = new Map(),
        Tp = [],
        Ep =
          `mousedown mouseup touchcancel touchend touchstart auxclick dblclick pointercancel pointerdown pointerup dragend dragstart drop compositionend compositionstart keydown keypress keyup input textInput copy cut paste click change contextmenu reset`.split(
            ` `,
          );
      function Dp(e, t) {
        switch (e) {
          case `focusin`:
          case `focusout`:
            bp = null;
            break;
          case `dragenter`:
          case `dragleave`:
            xp = null;
            break;
          case `mouseover`:
          case `mouseout`:
            Sp = null;
            break;
          case `pointerover`:
          case `pointerout`:
            Cp.delete(t.pointerId);
            break;
          case `gotpointercapture`:
          case `lostpointercapture`:
            wp.delete(t.pointerId);
        }
      }
      function Op(e, t, n, r, i, a) {
        return e === null || e.nativeEvent !== a
          ? ((e = {
              blockedOn: t,
              domEventName: n,
              eventSystemFlags: r,
              nativeEvent: a,
              targetContainers: [i],
            }),
            t !== null && ((t = wt(t)), t !== null && lp(t)),
            e)
          : ((e.eventSystemFlags |= r),
            (t = e.targetContainers),
            i !== null && t.indexOf(i) === -1 && t.push(i),
            e);
      }
      function kp(e, t, n, r, i) {
        switch (t) {
          case `focusin`:
            return ((bp = Op(bp, e, t, n, r, i)), !0);
          case `dragenter`:
            return ((xp = Op(xp, e, t, n, r, i)), !0);
          case `mouseover`:
            return ((Sp = Op(Sp, e, t, n, r, i)), !0);
          case `pointerover`:
            var a = i.pointerId;
            return (Cp.set(a, Op(Cp.get(a) || null, e, t, n, r, i)), !0);
          case `gotpointercapture`:
            return (
              (a = i.pointerId),
              wp.set(a, Op(wp.get(a) || null, e, t, n, r, i)),
              !0
            );
        }
        return !1;
      }
      function Ap(e) {
        var t = Ct(e.target);
        if (t !== null) {
          var n = o(t);
          if (n !== null) {
            if (((t = n.tag), t === 13)) {
              if (((t = s(n)), t !== null)) {
                ((e.blockedOn = t),
                  ft(e.priority, function () {
                    up(n);
                  }));
                return;
              }
            } else if (t === 31) {
              if (((t = c(n)), t !== null)) {
                ((e.blockedOn = t),
                  ft(e.priority, function () {
                    up(n);
                  }));
                return;
              }
            } else if (
              t === 3 &&
              n.stateNode.current.memoizedState.isDehydrated
            ) {
              e.blockedOn = n.tag === 3 ? n.stateNode.containerInfo : null;
              return;
            }
          }
        }
        e.blockedOn = null;
      }
      function jp(e) {
        if (e.blockedOn !== null) return !1;
        for (var t = e.targetContainers; 0 < t.length; ) {
          var n = hp(e.nativeEvent);
          if (n === null) {
            n = e.nativeEvent;
            var r = new n.constructor(n.type, n);
            ((ln = r), n.target.dispatchEvent(r), (ln = null));
          } else
            return ((t = wt(n)), t !== null && lp(t), (e.blockedOn = n), !1);
          t.shift();
        }
        return !0;
      }
      function Mp(e, t, n) {
        jp(e) && n.delete(t);
      }
      function Np() {
        ((yp = !1),
          bp !== null && jp(bp) && (bp = null),
          xp !== null && jp(xp) && (xp = null),
          Sp !== null && jp(Sp) && (Sp = null),
          Cp.forEach(Mp),
          wp.forEach(Mp));
      }
      function Pp(e, n) {
        e.blockedOn === n &&
          ((e.blockedOn = null),
          yp ||
            ((yp = !0),
            t.unstable_scheduleCallback(t.unstable_NormalPriority, Np)));
      }
      var Fp = null;
      function Ip(e) {
        Fp !== e &&
          ((Fp = e),
          t.unstable_scheduleCallback(t.unstable_NormalPriority, function () {
            Fp === e && (Fp = null);
            for (var t = 0; t < e.length; t += 3) {
              var n = e[t],
                r = e[t + 1],
                i = e[t + 2];
              if (typeof r != `function`) {
                if (_p(r || n) === null) continue;
                break;
              }
              var a = wt(n);
              a !== null &&
                (e.splice(t, 3),
                (t -= 3),
                Ts(
                  a,
                  { pending: !0, data: i, method: n.method, action: r },
                  r,
                  i,
                ));
            }
          }));
      }
      function Lp(e) {
        function t(t) {
          return Pp(t, e);
        }
        (bp !== null && Pp(bp, e),
          xp !== null && Pp(xp, e),
          Sp !== null && Pp(Sp, e),
          Cp.forEach(t),
          wp.forEach(t));
        for (var n = 0; n < Tp.length; n++) {
          var r = Tp[n];
          r.blockedOn === e && (r.blockedOn = null);
        }
        for (; 0 < Tp.length && ((n = Tp[0]), n.blockedOn === null); )
          (Ap(n), n.blockedOn === null && Tp.shift());
        if (((n = (e.ownerDocument || e).$$reactFormReplay), n != null))
          for (r = 0; r < n.length; r += 3) {
            var i = n[r],
              a = n[r + 1],
              o = i[ht] || null;
            if (typeof a == `function`) o || Ip(n);
            else if (o) {
              var s = null;
              if (a && a.hasAttribute(`formAction`)) {
                if (((i = a), (o = a[ht] || null))) s = o.formAction;
                else if (_p(i) !== null) continue;
              } else s = o.action;
              (typeof s == `function`
                ? (n[r + 1] = s)
                : (n.splice(r, 3), (r -= 3)),
                Ip(n));
            }
          }
      }
      function Rp() {
        function e(e) {
          e.canIntercept &&
            e.info === `react-transition` &&
            e.intercept({
              handler: function () {
                return new Promise(function (e) {
                  return (i = e);
                });
              },
              focusReset: `manual`,
              scroll: `manual`,
            });
        }
        function t() {
          (i !== null && (i(), (i = null)), r || setTimeout(n, 20));
        }
        function n() {
          if (!r && !navigation.transition) {
            var e = navigation.currentEntry;
            e &&
              e.url != null &&
              navigation.navigate(e.url, {
                state: e.getState(),
                info: `react-transition`,
                history: `replace`,
              });
          }
        }
        if (typeof navigation == `object`) {
          var r = !1,
            i = null;
          return (
            navigation.addEventListener(`navigate`, e),
            navigation.addEventListener(`navigatesuccess`, t),
            navigation.addEventListener(`navigateerror`, t),
            setTimeout(n, 100),
            function () {
              ((r = !0),
                navigation.removeEventListener(`navigate`, e),
                navigation.removeEventListener(`navigatesuccess`, t),
                navigation.removeEventListener(`navigateerror`, t),
                i !== null && (i(), (i = null)));
            }
          );
        }
      }
      function zp(e) {
        this._internalRoot = e;
      }
      ((Bp.prototype.render = zp.prototype.render =
        function (e) {
          var t = this._internalRoot;
          if (t === null) throw Error(i(409));
          var n = t.current;
          op(n, hu(), e, t, null, null);
        }),
        (Bp.prototype.unmount = zp.prototype.unmount =
          function () {
            var e = this._internalRoot;
            if (e !== null) {
              this._internalRoot = null;
              var t = e.containerInfo;
              (op(e.current, 2, null, e, null, null), Su(), (t[gt] = null));
            }
          }));
      function Bp(e) {
        this._internalRoot = e;
      }
      Bp.prototype.unstable_scheduleHydration = function (e) {
        if (e) {
          var t = dt();
          e = { blockedOn: null, target: e, priority: t };
          for (var n = 0; n < Tp.length && t !== 0 && t < Tp[n].priority; n++);
          (Tp.splice(n, 0, e), n === 0 && Ap(e));
        }
      };
      var Vp = n.version;
      if (Vp !== `19.2.8`) throw Error(i(527, Vp, `19.2.8`));
      D.findDOMNode = function (e) {
        var t = e._reactInternals;
        if (t === void 0)
          throw typeof e.render == `function`
            ? Error(i(188))
            : ((e = Object.keys(e).join(`,`)), Error(i(268, e)));
        return (
          (e = u(t)),
          (e = e === null ? null : d(e)),
          (e = e === null ? null : e.stateNode),
          e
        );
      };
      var Hp = {
        bundleType: 0,
        version: `19.2.8`,
        rendererPackageName: `react-dom`,
        currentDispatcherRef: E,
        reconcilerVersion: `19.2.8`,
      };
      if (typeof __REACT_DEVTOOLS_GLOBAL_HOOK__ < `u`) {
        var Up = __REACT_DEVTOOLS_GLOBAL_HOOK__;
        if (!Up.isDisabled && Up.supportsFiber)
          try {
            ((He = Up.inject(Hp)), (Ue = Up));
          } catch {}
      }
      e.createRoot = function (e, t) {
        if (!a(e)) throw Error(i(299));
        var n = !1,
          r = ``,
          o = Js,
          s = Ys,
          c = Xs;
        return (
          t != null &&
            (!0 === t.unstable_strictMode && (n = !0),
            t.identifierPrefix !== void 0 && (r = t.identifierPrefix),
            t.onUncaughtError !== void 0 && (o = t.onUncaughtError),
            t.onCaughtError !== void 0 && (s = t.onCaughtError),
            t.onRecoverableError !== void 0 && (c = t.onRecoverableError)),
          (t = ip(e, 1, !1, null, null, n, r, null, o, s, c, Rp)),
          (e[gt] = t.current),
          Td(e),
          new zp(t)
        );
      };
    }),
    b = c((e, t) => {
      function n() {
        if (
          !(
            typeof __REACT_DEVTOOLS_GLOBAL_HOOK__ > `u` ||
            typeof __REACT_DEVTOOLS_GLOBAL_HOOK__.checkDCE != `function`
          )
        )
          try {
            __REACT_DEVTOOLS_GLOBAL_HOOK__.checkDCE(n);
          } catch (e) {
            console.error(e);
          }
      }
      (n(), (t.exports = y()));
    }),
    x,
    ee = Object.freeze({ status: `aborted` });
  function S(e, t, n) {
    function r(n, r) {
      if (
        (n._zod ||
          Object.defineProperty(n, "_zod", {
            value: { def: r, constr: o, traits: new Set() },
            enumerable: !1,
          }),
        n._zod.traits.has(e))
      )
        return;
      (n._zod.traits.add(e), t(n, r));
      let i = o.prototype,
        a = Object.keys(i);
      for (let e = 0; e < a.length; e++) {
        let t = a[e];
        t in n || (n[t] = i[t].bind(n));
      }
    }
    let i = n?.Parent ?? Object;
    class a extends i {}
    Object.defineProperty(a, "name", { value: e });
    function o(e) {
      var t;
      let i = n?.Parent ? new a() : this;
      (r(i, e), (t = i._zod).deferred ?? (t.deferred = []));
      for (let e of i._zod.deferred) e();
      return i;
    }
    return (
      Object.defineProperty(o, "init", { value: r }),
      Object.defineProperty(o, Symbol.hasInstance, {
        value: (t) =>
          n?.Parent && t instanceof n.Parent ? !0 : t?._zod?.traits?.has(e),
      }),
      Object.defineProperty(o, "name", { value: e }),
      o
    );
  }
  var C = class extends Error {
      constructor() {
        super(
          `Encountered Promise during synchronous parse. Use .parseAsync() instead.`,
        );
      }
    },
    w = class extends Error {
      constructor(e) {
        (super(`Encountered unidirectional transform during encode: ${e}`),
          (this.name = `ZodEncodeError`));
      }
    };
  (x = globalThis).__zod_globalConfig ?? (x.__zod_globalConfig = {});
  var te = globalThis.__zod_globalConfig;
  function ne(e) {
    return (e && Object.assign(te, e), te);
  }
  function re(e) {
    let t = Object.values(e).filter((e) => typeof e == `number`);
    return Object.entries(e)
      .filter(([e, n]) => t.indexOf(+e) === -1)
      .map(([e, t]) => t);
  }
  function ie(e, t) {
    return typeof t == `bigint` ? t.toString() : t;
  }
  function ae(e) {
    return {
      get value() {
        {
          let t = e();
          return (Object.defineProperty(this, "value", { value: t }), t);
        }
        throw Error(`cached value already set`);
      },
    };
  }
  function oe(e) {
    return e == null;
  }
  function se(e) {
    let t = +!!e.startsWith(`^`),
      n = e.endsWith(`$`) ? e.length - 1 : e.length;
    return e.slice(t, n);
  }
  function ce(e, t) {
    let n = e / t,
      r = Math.round(n),
      i = 2 ** -52 * Math.max(Math.abs(n), 1);
    return Math.abs(n - r) < i ? 0 : n - r;
  }
  var le = Symbol(`evaluating`);
  function T(e, t, n) {
    let r;
    Object.defineProperty(e, t, {
      get() {
        if (r !== le) return (r === void 0 && ((r = le), (r = n())), r);
      },
      set(n) {
        Object.defineProperty(e, t, { value: n });
      },
      configurable: !0,
    });
  }
  function E(e, t, n) {
    Object.defineProperty(e, t, {
      value: n,
      writable: !0,
      enumerable: !0,
      configurable: !0,
    });
  }
  function D(...e) {
    let t = {};
    for (let n of e) {
      let e = Object.getOwnPropertyDescriptors(n);
      Object.assign(t, e);
    }
    return Object.defineProperties({}, t);
  }
  function ue(e) {
    return JSON.stringify(e);
  }
  function de(e) {
    return e
      .toLowerCase()
      .trim()
      .replace(/[^\w\s-]/g, ``)
      .replace(/[\s_-]+/g, `-`)
      .replace(/^-+|-+$/g, ``);
  }
  var fe =
    `captureStackTrace` in Error ? Error.captureStackTrace : (...e) => {};
  function pe(e) {
    return typeof e == `object` && !!e && !Array.isArray(e);
  }
  var O = ae(() => {
    if (
      te.jitless ||
      (typeof navigator < `u` && navigator?.userAgent?.includes(`Cloudflare`))
    )
      return !1;
    try {
      return (Function(``), !0);
    } catch {
      return !1;
    }
  });
  function k(e) {
    if (pe(e) === !1) return !1;
    let t = e.constructor;
    if (t === void 0 || typeof t != `function`) return !0;
    let n = t.prototype;
    return !(
      pe(n) === !1 ||
      Object.prototype.hasOwnProperty.call(n, `isPrototypeOf`) === !1
    );
  }
  function me(e) {
    return k(e)
      ? { ...e }
      : Array.isArray(e)
        ? [...e]
        : e instanceof Map
          ? new Map(e)
          : e instanceof Set
            ? new Set(e)
            : e;
  }
  var he = new Set([`string`, `number`, `symbol`]);
  function ge(e) {
    return e.replace(/[.*+?^${}()|[\]\\]/g, `\\$&`);
  }
  function _e(e, t, n) {
    let r = new e._zod.constr(t ?? e._zod.def);
    return ((!t || n?.parent) && (r._zod.parent = e), r);
  }
  function A(e) {
    let t = e;
    if (!t) return {};
    if (typeof t == `string`) return { error: () => t };
    if (t?.message !== void 0) {
      if (t?.error !== void 0)
        throw Error("Cannot specify both `message` and `error` params");
      t.error = t.message;
    }
    return (
      delete t.message,
      typeof t.error == `string` ? { ...t, error: () => t.error } : t
    );
  }
  function ve(e) {
    return Object.keys(e).filter(
      (t) => e[t]._zod.optin === `optional` && e[t]._zod.optout === `optional`,
    );
  }
  var ye = {
    safeint: [-(2 ** 53 - 1), 2 ** 53 - 1],
    int32: [-2147483648, 2147483647],
    uint32: [0, 4294967295],
    float32: [-34028234663852886e22, 34028234663852886e22],
    float64: [-Number.MAX_VALUE, Number.MAX_VALUE],
  };
  function be(e, t) {
    let n = e._zod.def,
      r = n.checks;
    if (r && r.length > 0)
      throw Error(
        `.pick() cannot be used on object schemas containing refinements`,
      );
    return _e(
      e,
      D(e._zod.def, {
        get shape() {
          let e = {};
          for (let r in t) {
            if (!(r in n.shape)) throw Error(`Unrecognized key: "${r}"`);
            t[r] && (e[r] = n.shape[r]);
          }
          return (E(this, `shape`, e), e);
        },
        checks: [],
      }),
    );
  }
  function xe(e, t) {
    let n = e._zod.def,
      r = n.checks;
    if (r && r.length > 0)
      throw Error(
        `.omit() cannot be used on object schemas containing refinements`,
      );
    return _e(
      e,
      D(e._zod.def, {
        get shape() {
          let r = { ...e._zod.def.shape };
          for (let e in t) {
            if (!(e in n.shape)) throw Error(`Unrecognized key: "${e}"`);
            t[e] && delete r[e];
          }
          return (E(this, `shape`, r), r);
        },
        checks: [],
      }),
    );
  }
  function Se(e, t) {
    if (!k(t)) throw Error(`Invalid input to extend: expected a plain object`);
    let n = e._zod.def.checks;
    if (n && n.length > 0) {
      let n = e._zod.def.shape;
      for (let e in t)
        if (Object.getOwnPropertyDescriptor(n, e) !== void 0)
          throw Error(
            "Cannot overwrite keys on object schemas containing refinements. Use `.safeExtend()` instead.",
          );
    }
    return _e(
      e,
      D(e._zod.def, {
        get shape() {
          let n = { ...e._zod.def.shape, ...t };
          return (E(this, `shape`, n), n);
        },
      }),
    );
  }
  function Ce(e, t) {
    if (!k(t))
      throw Error(`Invalid input to safeExtend: expected a plain object`);
    return _e(
      e,
      D(e._zod.def, {
        get shape() {
          let n = { ...e._zod.def.shape, ...t };
          return (E(this, `shape`, n), n);
        },
      }),
    );
  }
  function we(e, t) {
    if (e._zod.def.checks?.length)
      throw Error(
        `.merge() cannot be used on object schemas containing refinements. Use .safeExtend() instead.`,
      );
    return _e(
      e,
      D(e._zod.def, {
        get shape() {
          let n = { ...e._zod.def.shape, ...t._zod.def.shape };
          return (E(this, `shape`, n), n);
        },
        get catchall() {
          return t._zod.def.catchall;
        },
        checks: t._zod.def.checks ?? [],
      }),
    );
  }
  function Te(e, t, n) {
    let r = t._zod.def.checks;
    if (r && r.length > 0)
      throw Error(
        `.partial() cannot be used on object schemas containing refinements`,
      );
    return _e(
      t,
      D(t._zod.def, {
        get shape() {
          let r = t._zod.def.shape,
            i = { ...r };
          if (n)
            for (let t in n) {
              if (!(t in r)) throw Error(`Unrecognized key: "${t}"`);
              n[t] &&
                (i[t] = e
                  ? new e({ type: `optional`, innerType: r[t] })
                  : r[t]);
            }
          else
            for (let t in r)
              i[t] = e ? new e({ type: `optional`, innerType: r[t] }) : r[t];
          return (E(this, `shape`, i), i);
        },
        checks: [],
      }),
    );
  }
  function Ee(e, t, n) {
    return _e(
      t,
      D(t._zod.def, {
        get shape() {
          let r = t._zod.def.shape,
            i = { ...r };
          if (n)
            for (let t in n) {
              if (!(t in i)) throw Error(`Unrecognized key: "${t}"`);
              n[t] && (i[t] = new e({ type: `nonoptional`, innerType: r[t] }));
            }
          else
            for (let t in r)
              i[t] = new e({ type: `nonoptional`, innerType: r[t] });
          return (E(this, `shape`, i), i);
        },
      }),
    );
  }
  function De(e, t = 0) {
    if (e.aborted === !0) return !0;
    for (let n = t; n < e.issues.length; n++)
      if (e.issues[n]?.continue !== !0) return !0;
    return !1;
  }
  function Oe(e, t = 0) {
    if (e.aborted === !0) return !0;
    for (let n = t; n < e.issues.length; n++)
      if (e.issues[n]?.continue === !1) return !0;
    return !1;
  }
  function ke(e, t) {
    return t.map((t) => {
      var n;
      return ((n = t).path ?? (n.path = []), t.path.unshift(e), t);
    });
  }
  function Ae(e) {
    return typeof e == `string` ? e : e?.message;
  }
  function je(e, t, n) {
    let r = e.message
        ? e.message
        : (Ae(e.inst?._zod.def?.error?.(e)) ??
          Ae(t?.error?.(e)) ??
          Ae(n.customError?.(e)) ??
          Ae(n.localeError?.(e)) ??
          `Invalid input`),
      { inst: i, continue: a, input: o, ...s } = e;
    return (
      (s.path ??= []), (s.message = r), t?.reportInput && (s.input = o), s
    );
  }
  function Me(e) {
    return Array.isArray(e)
      ? `array`
      : typeof e == `string`
        ? `string`
        : `unknown`;
  }
  function Ne(...e) {
    let [t, n, r] = e;
    return typeof t == `string`
      ? { message: t, code: `custom`, input: n, inst: r }
      : { ...t };
  }
  var Pe = (e, t) => {
      ((e.name = `$ZodError`),
        Object.defineProperty(e, "_zod", { value: e._zod, enumerable: !1 }),
        Object.defineProperty(e, "issues", { value: t, enumerable: !1 }),
        (e.message = JSON.stringify(t, ie, 2)),
        Object.defineProperty(e, "toString", {
          value: () => e.message,
          enumerable: !1,
        }));
    },
    Fe = S(`$ZodError`, Pe),
    Ie = S(`$ZodError`, Pe, { Parent: Error });
  function Le(e, t = (e) => e.message) {
    let n = {},
      r = [];
    for (let i of e.issues)
      i.path.length > 0
        ? ((n[i.path[0]] = n[i.path[0]] || []), n[i.path[0]].push(t(i)))
        : r.push(t(i));
    return { formErrors: r, fieldErrors: n };
  }
  function Re(e, t = (e) => e.message) {
    let n = { _errors: [] },
      r = (e, i = []) => {
        for (let a of e.issues)
          if (a.code === `invalid_union` && a.errors.length)
            a.errors.map((e) => r({ issues: e }, [...i, ...a.path]));
          else if (a.code === `invalid_key`)
            r({ issues: a.issues }, [...i, ...a.path]);
          else if (a.code === `invalid_element`)
            r({ issues: a.issues }, [...i, ...a.path]);
          else {
            let e = [...i, ...a.path];
            if (e.length === 0) n._errors.push(t(a));
            else {
              let r = n,
                i = 0;
              for (; i < e.length; ) {
                let n = e[i];
                (i === e.length - 1
                  ? ((r[n] = r[n] || { _errors: [] }), r[n]._errors.push(t(a)))
                  : (r[n] = r[n] || { _errors: [] }),
                  (r = r[n]),
                  i++);
              }
            }
          }
      };
    return (r(e), n);
  }
  var ze = (e) => (t, n, r, i) => {
      let a = r ? { ...r, async: !1 } : { async: !1 },
        o = t._zod.run({ value: n, issues: [] }, a);
      if (o instanceof Promise) throw new C();
      if (o.issues.length) {
        let t = new (i?.Err ?? e)(o.issues.map((e) => je(e, a, ne())));
        throw (fe(t, i?.callee), t);
      }
      return o.value;
    },
    Be = (e) => async (t, n, r, i) => {
      let a = r ? { ...r, async: !0 } : { async: !0 },
        o = t._zod.run({ value: n, issues: [] }, a);
      if ((o instanceof Promise && (o = await o), o.issues.length)) {
        let t = new (i?.Err ?? e)(o.issues.map((e) => je(e, a, ne())));
        throw (fe(t, i?.callee), t);
      }
      return o.value;
    },
    Ve = (e) => (t, n, r) => {
      let i = r ? { ...r, async: !1 } : { async: !1 },
        a = t._zod.run({ value: n, issues: [] }, i);
      if (a instanceof Promise) throw new C();
      return a.issues.length
        ? {
            success: !1,
            error: new (e ?? Fe)(a.issues.map((e) => je(e, i, ne()))),
          }
        : { success: !0, data: a.value };
    },
    He = Ve(Ie),
    Ue = (e) => async (t, n, r) => {
      let i = r ? { ...r, async: !0 } : { async: !0 },
        a = t._zod.run({ value: n, issues: [] }, i);
      return (
        a instanceof Promise && (a = await a),
        a.issues.length
          ? { success: !1, error: new e(a.issues.map((e) => je(e, i, ne()))) }
          : { success: !0, data: a.value }
      );
    },
    We = Ue(Ie),
    Ge = (e) => (t, n, r) => {
      let i = r ? { ...r, direction: `backward` } : { direction: `backward` };
      return ze(e)(t, n, i);
    },
    Ke = (e) => (t, n, r) => ze(e)(t, n, r),
    qe = (e) => async (t, n, r) => {
      let i = r ? { ...r, direction: `backward` } : { direction: `backward` };
      return Be(e)(t, n, i);
    },
    Je = (e) => async (t, n, r) => Be(e)(t, n, r),
    Ye = (e) => (t, n, r) => {
      let i = r ? { ...r, direction: `backward` } : { direction: `backward` };
      return Ve(e)(t, n, i);
    },
    Xe = (e) => (t, n, r) => Ve(e)(t, n, r),
    Ze = (e) => async (t, n, r) => {
      let i = r ? { ...r, direction: `backward` } : { direction: `backward` };
      return Ue(e)(t, n, i);
    },
    Qe = (e) => async (t, n, r) => Ue(e)(t, n, r),
    $e = /^[cC][0-9a-z]{6,}$/,
    et = /^[0-9a-z]+$/,
    tt = /^[0-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{26}$/,
    nt = /^[0-9a-vA-V]{20}$/,
    rt = /^[A-Za-z0-9]{27}$/,
    it = /^[a-zA-Z0-9_-]{21}$/,
    at =
      /^P(?:(\d+W)|(?!.*W)(?=\d|T\d)(\d+Y)?(\d+M)?(\d+D)?(T(?=\d)(\d+H)?(\d+M)?(\d+([.,]\d+)?S)?)?)$/,
    ot =
      /^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$/,
    st = (e) =>
      e
        ? RegExp(
            `^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-${e}[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12})$`,
          )
        : /^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$/,
    ct =
      /^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$/,
    lt = `^(\\p{Extended_Pictographic}|\\p{Emoji_Component})+$`;
  function ut() {
    return new RegExp(lt, `u`);
  }
  var dt =
      /^(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])$/,
    ft =
      /^(([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,7}:|([0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,5}(:[0-9a-fA-F]{1,4}){1,2}|([0-9a-fA-F]{1,4}:){1,4}(:[0-9a-fA-F]{1,4}){1,3}|([0-9a-fA-F]{1,4}:){1,3}(:[0-9a-fA-F]{1,4}){1,4}|([0-9a-fA-F]{1,4}:){1,2}(:[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:((:[0-9a-fA-F]{1,4}){1,6})|:((:[0-9a-fA-F]{1,4}){1,7}|:))$/,
    pt =
      /^((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\/([0-9]|[1-2][0-9]|3[0-2])$/,
    mt =
      /^(([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|::|([0-9a-fA-F]{1,4})?::([0-9a-fA-F]{1,4}:?){0,6})\/(12[0-8]|1[01][0-9]|[1-9]?[0-9])$/,
    ht =
      /^$|^(?:[0-9a-zA-Z+/]{4})*(?:(?:[0-9a-zA-Z+/]{2}==)|(?:[0-9a-zA-Z+/]{3}=))?$/,
    gt = /^[A-Za-z0-9_-]*$/,
    _t = /^https?$/,
    vt = /^\+[1-9]\d{6,14}$/,
    yt = `(?:(?:\\d\\d[2468][048]|\\d\\d[13579][26]|\\d\\d0[48]|[02468][048]00|[13579][26]00)-02-29|\\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\\d|30)|(?:02)-(?:0[1-9]|1\\d|2[0-8])))`,
    bt = RegExp(`^${yt}$`);
  function xt(e) {
    let t = `(?:[01]\\d|2[0-3]):[0-5]\\d`;
    return typeof e.precision == `number`
      ? e.precision === -1
        ? `${t}`
        : e.precision === 0
          ? `${t}:[0-5]\\d`
          : `${t}:[0-5]\\d\\.\\d{${e.precision}}`
      : `${t}(?::[0-5]\\d(?:\\.\\d+)?)?`;
  }
  function St(e) {
    return RegExp(`^${xt(e)}$`);
  }
  function Ct(e) {
    let t = xt({ precision: e.precision }),
      n = [`Z`];
    (e.local && n.push(``),
      e.offset && n.push(`([+-](?:[01]\\d|2[0-3]):[0-5]\\d)`));
    let r = `${t}(?:${n.join(`|`)})`;
    return RegExp(`^${yt}T(?:${r})$`);
  }
  var wt = (e) => {
      let t = e
        ? `[\\s\\S]{${e?.minimum ?? 0},${e?.maximum ?? ``}}`
        : `[\\s\\S]*`;
      return RegExp(`^${t}$`);
    },
    Tt = /^-?\d+$/,
    Et = /^-?\d+(?:\.\d+)?$/,
    Dt = /^(?:true|false)$/i,
    Ot = /^null$/i,
    kt = /^[^A-Z]*$/,
    At = /^[^a-z]*$/,
    jt = S(`$ZodCheck`, (e, t) => {
      var n;
      ((e._zod ??= {}),
        (e._zod.def = t),
        (n = e._zod).onattach ?? (n.onattach = []));
    }),
    Mt = { number: `number`, bigint: `bigint`, object: `date` },
    Nt = S(`$ZodCheckLessThan`, (e, t) => {
      jt.init(e, t);
      let n = Mt[typeof t.value];
      (e._zod.onattach.push((e) => {
        let n = e._zod.bag,
          r = (t.inclusive ? n.maximum : n.exclusiveMaximum) ?? 1 / 0;
        t.value < r &&
          (t.inclusive
            ? (n.maximum = t.value)
            : (n.exclusiveMaximum = t.value));
      }),
        (e._zod.check = (r) => {
          (t.inclusive ? r.value <= t.value : r.value < t.value) ||
            r.issues.push({
              origin: n,
              code: `too_big`,
              maximum: typeof t.value == `object` ? t.value.getTime() : t.value,
              input: r.value,
              inclusive: t.inclusive,
              inst: e,
              continue: !t.abort,
            });
        }));
    }),
    Pt = S(`$ZodCheckGreaterThan`, (e, t) => {
      jt.init(e, t);
      let n = Mt[typeof t.value];
      (e._zod.onattach.push((e) => {
        let n = e._zod.bag,
          r = (t.inclusive ? n.minimum : n.exclusiveMinimum) ?? -1 / 0;
        t.value > r &&
          (t.inclusive
            ? (n.minimum = t.value)
            : (n.exclusiveMinimum = t.value));
      }),
        (e._zod.check = (r) => {
          (t.inclusive ? r.value >= t.value : r.value > t.value) ||
            r.issues.push({
              origin: n,
              code: `too_small`,
              minimum: typeof t.value == `object` ? t.value.getTime() : t.value,
              input: r.value,
              inclusive: t.inclusive,
              inst: e,
              continue: !t.abort,
            });
        }));
    }),
    Ft = S(`$ZodCheckMultipleOf`, (e, t) => {
      (jt.init(e, t),
        e._zod.onattach.push((e) => {
          var n;
          (n = e._zod.bag).multipleOf ?? (n.multipleOf = t.value);
        }),
        (e._zod.check = (n) => {
          if (typeof n.value != typeof t.value)
            throw Error(`Cannot mix number and bigint in multiple_of check.`);
          (typeof n.value == `bigint`
            ? n.value % t.value === BigInt(0)
            : ce(n.value, t.value) === 0) ||
            n.issues.push({
              origin: typeof n.value,
              code: `not_multiple_of`,
              divisor: t.value,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
        }));
    }),
    It = S(`$ZodCheckNumberFormat`, (e, t) => {
      (jt.init(e, t), (t.format = t.format || `float64`));
      let n = t.format?.includes(`int`),
        r = n ? `int` : `number`,
        [i, a] = ye[t.format];
      (e._zod.onattach.push((e) => {
        let r = e._zod.bag;
        ((r.format = t.format),
          (r.minimum = i),
          (r.maximum = a),
          n && (r.pattern = Tt));
      }),
        (e._zod.check = (o) => {
          let s = o.value;
          if (n) {
            if (!Number.isInteger(s)) {
              o.issues.push({
                expected: r,
                format: t.format,
                code: `invalid_type`,
                continue: !1,
                input: s,
                inst: e,
              });
              return;
            }
            if (!Number.isSafeInteger(s)) {
              s > 0
                ? o.issues.push({
                    input: s,
                    code: `too_big`,
                    maximum: 2 ** 53 - 1,
                    note: `Integers must be within the safe integer range.`,
                    inst: e,
                    origin: r,
                    inclusive: !0,
                    continue: !t.abort,
                  })
                : o.issues.push({
                    input: s,
                    code: `too_small`,
                    minimum: -(2 ** 53 - 1),
                    note: `Integers must be within the safe integer range.`,
                    inst: e,
                    origin: r,
                    inclusive: !0,
                    continue: !t.abort,
                  });
              return;
            }
          }
          (s < i &&
            o.issues.push({
              origin: `number`,
              input: s,
              code: `too_small`,
              minimum: i,
              inclusive: !0,
              inst: e,
              continue: !t.abort,
            }),
            s > a &&
              o.issues.push({
                origin: `number`,
                input: s,
                code: `too_big`,
                maximum: a,
                inclusive: !0,
                inst: e,
                continue: !t.abort,
              }));
        }));
    }),
    Lt = S(`$ZodCheckMaxLength`, (e, t) => {
      var n;
      (jt.init(e, t),
        (n = e._zod.def).when ??
          (n.when = (e) => {
            let t = e.value;
            return !oe(t) && t.length !== void 0;
          }),
        e._zod.onattach.push((e) => {
          let n = e._zod.bag.maximum ?? 1 / 0;
          t.maximum < n && (e._zod.bag.maximum = t.maximum);
        }),
        (e._zod.check = (n) => {
          let r = n.value;
          if (r.length <= t.maximum) return;
          let i = Me(r);
          n.issues.push({
            origin: i,
            code: `too_big`,
            maximum: t.maximum,
            inclusive: !0,
            input: r,
            inst: e,
            continue: !t.abort,
          });
        }));
    }),
    Rt = S(`$ZodCheckMinLength`, (e, t) => {
      var n;
      (jt.init(e, t),
        (n = e._zod.def).when ??
          (n.when = (e) => {
            let t = e.value;
            return !oe(t) && t.length !== void 0;
          }),
        e._zod.onattach.push((e) => {
          let n = e._zod.bag.minimum ?? -1 / 0;
          t.minimum > n && (e._zod.bag.minimum = t.minimum);
        }),
        (e._zod.check = (n) => {
          let r = n.value;
          if (r.length >= t.minimum) return;
          let i = Me(r);
          n.issues.push({
            origin: i,
            code: `too_small`,
            minimum: t.minimum,
            inclusive: !0,
            input: r,
            inst: e,
            continue: !t.abort,
          });
        }));
    }),
    zt = S(`$ZodCheckLengthEquals`, (e, t) => {
      var n;
      (jt.init(e, t),
        (n = e._zod.def).when ??
          (n.when = (e) => {
            let t = e.value;
            return !oe(t) && t.length !== void 0;
          }),
        e._zod.onattach.push((e) => {
          let n = e._zod.bag;
          ((n.minimum = t.length),
            (n.maximum = t.length),
            (n.length = t.length));
        }),
        (e._zod.check = (n) => {
          let r = n.value,
            i = r.length;
          if (i === t.length) return;
          let a = Me(r),
            o = i > t.length;
          n.issues.push({
            origin: a,
            ...(o
              ? { code: `too_big`, maximum: t.length }
              : { code: `too_small`, minimum: t.length }),
            inclusive: !0,
            exact: !0,
            input: n.value,
            inst: e,
            continue: !t.abort,
          });
        }));
    }),
    Bt = S(`$ZodCheckStringFormat`, (e, t) => {
      var n, r;
      (jt.init(e, t),
        e._zod.onattach.push((e) => {
          let n = e._zod.bag;
          ((n.format = t.format),
            t.pattern &&
              ((n.patterns ??= new Set()), n.patterns.add(t.pattern)));
        }),
        t.pattern
          ? ((n = e._zod).check ??
            (n.check = (n) => {
              ((t.pattern.lastIndex = 0),
                !t.pattern.test(n.value) &&
                  n.issues.push({
                    origin: `string`,
                    code: `invalid_format`,
                    format: t.format,
                    input: n.value,
                    ...(t.pattern ? { pattern: t.pattern.toString() } : {}),
                    inst: e,
                    continue: !t.abort,
                  }));
            }))
          : ((r = e._zod).check ?? (r.check = () => {})));
    }),
    Vt = S(`$ZodCheckRegex`, (e, t) => {
      (Bt.init(e, t),
        (e._zod.check = (n) => {
          ((t.pattern.lastIndex = 0),
            !t.pattern.test(n.value) &&
              n.issues.push({
                origin: `string`,
                code: `invalid_format`,
                format: `regex`,
                input: n.value,
                pattern: t.pattern.toString(),
                inst: e,
                continue: !t.abort,
              }));
        }));
    }),
    Ht = S(`$ZodCheckLowerCase`, (e, t) => {
      ((t.pattern ??= kt), Bt.init(e, t));
    }),
    Ut = S(`$ZodCheckUpperCase`, (e, t) => {
      ((t.pattern ??= At), Bt.init(e, t));
    }),
    Wt = S(`$ZodCheckIncludes`, (e, t) => {
      jt.init(e, t);
      let n = ge(t.includes),
        r = new RegExp(
          typeof t.position == `number` ? `^.{${t.position}}${n}` : n,
        );
      ((t.pattern = r),
        e._zod.onattach.push((e) => {
          let t = e._zod.bag;
          ((t.patterns ??= new Set()), t.patterns.add(r));
        }),
        (e._zod.check = (n) => {
          n.value.includes(t.includes, t.position) ||
            n.issues.push({
              origin: `string`,
              code: `invalid_format`,
              format: `includes`,
              includes: t.includes,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
        }));
    }),
    Gt = S(`$ZodCheckStartsWith`, (e, t) => {
      jt.init(e, t);
      let n = RegExp(`^${ge(t.prefix)}.*`);
      ((t.pattern ??= n),
        e._zod.onattach.push((e) => {
          let t = e._zod.bag;
          ((t.patterns ??= new Set()), t.patterns.add(n));
        }),
        (e._zod.check = (n) => {
          n.value.startsWith(t.prefix) ||
            n.issues.push({
              origin: `string`,
              code: `invalid_format`,
              format: `starts_with`,
              prefix: t.prefix,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
        }));
    }),
    Kt = S(`$ZodCheckEndsWith`, (e, t) => {
      jt.init(e, t);
      let n = RegExp(`.*${ge(t.suffix)}$`);
      ((t.pattern ??= n),
        e._zod.onattach.push((e) => {
          let t = e._zod.bag;
          ((t.patterns ??= new Set()), t.patterns.add(n));
        }),
        (e._zod.check = (n) => {
          n.value.endsWith(t.suffix) ||
            n.issues.push({
              origin: `string`,
              code: `invalid_format`,
              format: `ends_with`,
              suffix: t.suffix,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
        }));
    }),
    qt = S(`$ZodCheckOverwrite`, (e, t) => {
      (jt.init(e, t),
        (e._zod.check = (e) => {
          e.value = t.tx(e.value);
        }));
    }),
    Jt = class {
      constructor(e = []) {
        ((this.content = []), (this.indent = 0), this && (this.args = e));
      }
      indented(e) {
        ((this.indent += 1), e(this), --this.indent);
      }
      write(e) {
        if (typeof e == `function`) {
          (e(this, { execution: `sync` }), e(this, { execution: `async` }));
          return;
        }
        let t = e
            .split(`
`)
            .filter((e) => e),
          n = Math.min(...t.map((e) => e.length - e.trimStart().length)),
          r = t
            .map((e) => e.slice(n))
            .map((e) => ` `.repeat(this.indent * 2) + e);
        for (let e of r) this.content.push(e);
      }
      compile() {
        let e = Function,
          t = this?.args,
          n = [...(this?.content ?? [``]).map((e) => `  ${e}`)];
        return new e(
          ...t,
          n.join(`
`),
        );
      }
    },
    Yt = { major: 4, minor: 4, patch: 3 },
    Xt = S(`$ZodType`, (e, t) => {
      var n;
      ((e ??= {}),
        (e._zod.def = t),
        (e._zod.bag = e._zod.bag || {}),
        (e._zod.version = Yt));
      let r = [...(e._zod.def.checks ?? [])];
      e._zod.traits.has(`$ZodCheck`) && r.unshift(e);
      for (let t of r) for (let n of t._zod.onattach) n(e);
      if (r.length === 0)
        ((n = e._zod).deferred ?? (n.deferred = []),
          e._zod.deferred?.push(() => {
            e._zod.run = e._zod.parse;
          }));
      else {
        let t = (e, t, n) => {
            let r = De(e),
              i;
            for (let a of t) {
              if (a._zod.def.when) {
                if (Oe(e) || !a._zod.def.when(e)) continue;
              } else if (r) continue;
              let t = e.issues.length,
                o = a._zod.check(e);
              if (o instanceof Promise && n?.async === !1) throw new C();
              if (i || o instanceof Promise)
                i = (i ?? Promise.resolve()).then(async () => {
                  (await o, e.issues.length !== t && (r ||= De(e, t)));
                });
              else {
                if (e.issues.length === t) continue;
                r ||= De(e, t);
              }
            }
            return i ? i.then(() => e) : e;
          },
          n = (n, i, a) => {
            if (De(n)) return ((n.aborted = !0), n);
            let o = t(i, r, a);
            if (o instanceof Promise) {
              if (a.async === !1) throw new C();
              return o.then((t) => e._zod.parse(t, a));
            }
            return e._zod.parse(o, a);
          };
        e._zod.run = (i, a) => {
          if (a.skipChecks) return e._zod.parse(i, a);
          if (a.direction === `backward`) {
            let t = e._zod.parse(
              { value: i.value, issues: [] },
              { ...a, skipChecks: !0 },
            );
            return t instanceof Promise
              ? t.then((e) => n(e, i, a))
              : n(t, i, a);
          }
          let o = e._zod.parse(i, a);
          if (o instanceof Promise) {
            if (a.async === !1) throw new C();
            return o.then((e) => t(e, r, a));
          }
          return t(o, r, a);
        };
      }
      T(e, `~standard`, () => ({
        validate: (t) => {
          try {
            let n = He(e, t);
            return n.success ? { value: n.data } : { issues: n.error?.issues };
          } catch {
            return We(e, t).then((e) =>
              e.success ? { value: e.data } : { issues: e.error?.issues },
            );
          }
        },
        vendor: `zod`,
        version: 1,
      }));
    }),
    Zt = S(`$ZodString`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.pattern =
          [...(e?._zod.bag?.patterns ?? [])].pop() ?? wt(e._zod.bag)),
        (e._zod.parse = (n, r) => {
          if (t.coerce)
            try {
              n.value = String(n.value);
            } catch {}
          return (
            typeof n.value == `string` ||
              n.issues.push({
                expected: `string`,
                code: `invalid_type`,
                input: n.value,
                inst: e,
              }),
            n
          );
        }));
    }),
    Qt = S(`$ZodStringFormat`, (e, t) => {
      (Bt.init(e, t), Zt.init(e, t));
    }),
    $t = S(`$ZodGUID`, (e, t) => {
      ((t.pattern ??= ot), Qt.init(e, t));
    }),
    en = S(`$ZodUUID`, (e, t) => {
      if (t.version) {
        let e = { v1: 1, v2: 2, v3: 3, v4: 4, v5: 5, v6: 6, v7: 7, v8: 8 }[
          t.version
        ];
        if (e === void 0) throw Error(`Invalid UUID version: "${t.version}"`);
        t.pattern ??= st(e);
      } else t.pattern ??= st();
      Qt.init(e, t);
    }),
    tn = S(`$ZodEmail`, (e, t) => {
      ((t.pattern ??= ct), Qt.init(e, t));
    }),
    nn = S(`$ZodURL`, (e, t) => {
      (Qt.init(e, t),
        (e._zod.check = (n) => {
          try {
            let r = n.value.trim();
            if (
              !t.normalize &&
              t.protocol?.source === _t.source &&
              !/^https?:\/\//i.test(r)
            ) {
              n.issues.push({
                code: `invalid_format`,
                format: `url`,
                note: `Invalid URL format`,
                input: n.value,
                inst: e,
                continue: !t.abort,
              });
              return;
            }
            let i = new URL(r);
            (t.hostname &&
              ((t.hostname.lastIndex = 0),
              t.hostname.test(i.hostname) ||
                n.issues.push({
                  code: `invalid_format`,
                  format: `url`,
                  note: `Invalid hostname`,
                  pattern: t.hostname.source,
                  input: n.value,
                  inst: e,
                  continue: !t.abort,
                })),
              t.protocol &&
                ((t.protocol.lastIndex = 0),
                t.protocol.test(
                  i.protocol.endsWith(`:`)
                    ? i.protocol.slice(0, -1)
                    : i.protocol,
                ) ||
                  n.issues.push({
                    code: `invalid_format`,
                    format: `url`,
                    note: `Invalid protocol`,
                    pattern: t.protocol.source,
                    input: n.value,
                    inst: e,
                    continue: !t.abort,
                  })),
              t.normalize ? (n.value = i.href) : (n.value = r));
            return;
          } catch {
            n.issues.push({
              code: `invalid_format`,
              format: `url`,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
          }
        }));
    }),
    rn = S(`$ZodEmoji`, (e, t) => {
      ((t.pattern ??= ut()), Qt.init(e, t));
    }),
    an = S(`$ZodNanoID`, (e, t) => {
      ((t.pattern ??= it), Qt.init(e, t));
    }),
    on = S(`$ZodCUID`, (e, t) => {
      ((t.pattern ??= $e), Qt.init(e, t));
    }),
    sn = S(`$ZodCUID2`, (e, t) => {
      ((t.pattern ??= et), Qt.init(e, t));
    }),
    cn = S(`$ZodULID`, (e, t) => {
      ((t.pattern ??= tt), Qt.init(e, t));
    }),
    ln = S(`$ZodXID`, (e, t) => {
      ((t.pattern ??= nt), Qt.init(e, t));
    }),
    un = S(`$ZodKSUID`, (e, t) => {
      ((t.pattern ??= rt), Qt.init(e, t));
    }),
    dn = S(`$ZodISODateTime`, (e, t) => {
      ((t.pattern ??= Ct(t)), Qt.init(e, t));
    }),
    fn = S(`$ZodISODate`, (e, t) => {
      ((t.pattern ??= bt), Qt.init(e, t));
    }),
    pn = S(`$ZodISOTime`, (e, t) => {
      ((t.pattern ??= St(t)), Qt.init(e, t));
    }),
    mn = S(`$ZodISODuration`, (e, t) => {
      ((t.pattern ??= at), Qt.init(e, t));
    }),
    hn = S(`$ZodIPv4`, (e, t) => {
      ((t.pattern ??= dt), Qt.init(e, t), (e._zod.bag.format = `ipv4`));
    }),
    gn = S(`$ZodIPv6`, (e, t) => {
      ((t.pattern ??= ft),
        Qt.init(e, t),
        (e._zod.bag.format = `ipv6`),
        (e._zod.check = (n) => {
          try {
            new URL(`http://[${n.value}]`);
          } catch {
            n.issues.push({
              code: `invalid_format`,
              format: `ipv6`,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
          }
        }));
    }),
    _n = S(`$ZodCIDRv4`, (e, t) => {
      ((t.pattern ??= pt), Qt.init(e, t));
    }),
    vn = S(`$ZodCIDRv6`, (e, t) => {
      ((t.pattern ??= mt),
        Qt.init(e, t),
        (e._zod.check = (n) => {
          let r = n.value.split(`/`);
          try {
            if (r.length !== 2) throw Error();
            let [e, t] = r;
            if (!t) throw Error();
            let n = Number(t);
            if (`${n}` !== t || n < 0 || n > 128) throw Error();
            new URL(`http://[${e}]`);
          } catch {
            n.issues.push({
              code: `invalid_format`,
              format: `cidrv6`,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
          }
        }));
    });
  function yn(e) {
    if (e === ``) return !0;
    if (/\s/.test(e) || e.length % 4 != 0) return !1;
    try {
      return (atob(e), !0);
    } catch {
      return !1;
    }
  }
  var bn = S(`$ZodBase64`, (e, t) => {
    ((t.pattern ??= ht),
      Qt.init(e, t),
      (e._zod.bag.contentEncoding = `base64`),
      (e._zod.check = (n) => {
        yn(n.value) ||
          n.issues.push({
            code: `invalid_format`,
            format: `base64`,
            input: n.value,
            inst: e,
            continue: !t.abort,
          });
      }));
  });
  function xn(e) {
    if (!gt.test(e)) return !1;
    let t = e.replace(/[-_]/g, (e) => (e === `-` ? `+` : `/`));
    return yn(t.padEnd(Math.ceil(t.length / 4) * 4, `=`));
  }
  var Sn = S(`$ZodBase64URL`, (e, t) => {
      ((t.pattern ??= gt),
        Qt.init(e, t),
        (e._zod.bag.contentEncoding = `base64url`),
        (e._zod.check = (n) => {
          xn(n.value) ||
            n.issues.push({
              code: `invalid_format`,
              format: `base64url`,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
        }));
    }),
    Cn = S(`$ZodE164`, (e, t) => {
      ((t.pattern ??= vt), Qt.init(e, t));
    });
  function wn(e, t = null) {
    try {
      let n = e.split(`.`);
      if (n.length !== 3) return !1;
      let [r] = n;
      if (!r) return !1;
      let i = JSON.parse(atob(r));
      return !(
        (`typ` in i && i?.typ !== `JWT`) ||
        !i.alg ||
        (t && (!(`alg` in i) || i.alg !== t))
      );
    } catch {
      return !1;
    }
  }
  var Tn = S(`$ZodJWT`, (e, t) => {
      (Qt.init(e, t),
        (e._zod.check = (n) => {
          wn(n.value, t.alg) ||
            n.issues.push({
              code: `invalid_format`,
              format: `jwt`,
              input: n.value,
              inst: e,
              continue: !t.abort,
            });
        }));
    }),
    En = S(`$ZodNumber`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.pattern = e._zod.bag.pattern ?? Et),
        (e._zod.parse = (n, r) => {
          if (t.coerce)
            try {
              n.value = Number(n.value);
            } catch {}
          let i = n.value;
          if (typeof i == `number` && !Number.isNaN(i) && Number.isFinite(i))
            return n;
          let a =
            typeof i == `number`
              ? Number.isNaN(i)
                ? `NaN`
                : Number.isFinite(i)
                  ? void 0
                  : `Infinity`
              : void 0;
          return (
            n.issues.push({
              expected: `number`,
              code: `invalid_type`,
              input: i,
              inst: e,
              ...(a ? { received: a } : {}),
            }),
            n
          );
        }));
    }),
    Dn = S(`$ZodNumberFormat`, (e, t) => {
      (It.init(e, t), En.init(e, t));
    }),
    On = S(`$ZodBoolean`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.pattern = Dt),
        (e._zod.parse = (n, r) => {
          if (t.coerce)
            try {
              n.value = !!n.value;
            } catch {}
          let i = n.value;
          return (
            typeof i == `boolean` ||
              n.issues.push({
                expected: `boolean`,
                code: `invalid_type`,
                input: i,
                inst: e,
              }),
            n
          );
        }));
    }),
    kn = S(`$ZodNull`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.pattern = Ot),
        (e._zod.values = new Set([null])),
        (e._zod.parse = (t, n) => {
          let r = t.value;
          return (
            r === null ||
              t.issues.push({
                expected: `null`,
                code: `invalid_type`,
                input: r,
                inst: e,
              }),
            t
          );
        }));
    }),
    An = S(`$ZodAny`, (e, t) => {
      (Xt.init(e, t), (e._zod.parse = (e) => e));
    }),
    jn = S(`$ZodUnknown`, (e, t) => {
      (Xt.init(e, t), (e._zod.parse = (e) => e));
    }),
    Mn = S(`$ZodNever`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.parse = (t, n) => (
          t.issues.push({
            expected: `never`,
            code: `invalid_type`,
            input: t.value,
            inst: e,
          }),
          t
        )));
    });
  function Nn(e, t, n) {
    (e.issues.length && t.issues.push(...ke(n, e.issues)),
      (t.value[n] = e.value));
  }
  var Pn = S(`$ZodArray`, (e, t) => {
    (Xt.init(e, t),
      (e._zod.parse = (n, r) => {
        let i = n.value;
        if (!Array.isArray(i))
          return (
            n.issues.push({
              expected: `array`,
              code: `invalid_type`,
              input: i,
              inst: e,
            }),
            n
          );
        n.value = Array(i.length);
        let a = [];
        for (let e = 0; e < i.length; e++) {
          let o = i[e],
            s = t.element._zod.run({ value: o, issues: [] }, r);
          s instanceof Promise
            ? a.push(s.then((t) => Nn(t, n, e)))
            : Nn(s, n, e);
        }
        return a.length ? Promise.all(a).then(() => n) : n;
      }));
  });
  function Fn(e, t, n, r, i, a) {
    let o = n in r;
    if (e.issues.length) {
      if (i && a && !o) return;
      t.issues.push(...ke(n, e.issues));
    }
    if (!o && !i) {
      e.issues.length ||
        t.issues.push({
          code: `invalid_type`,
          expected: `nonoptional`,
          input: void 0,
          path: [n],
        });
      return;
    }
    e.value === void 0 ? o && (t.value[n] = void 0) : (t.value[n] = e.value);
  }
  function In(e) {
    let t = Object.keys(e.shape);
    for (let n of t)
      if (!e.shape?.[n]?._zod?.traits?.has(`$ZodType`))
        throw Error(`Invalid element at key "${n}": expected a Zod schema`);
    let n = ve(e.shape);
    return {
      ...e,
      keys: t,
      keySet: new Set(t),
      numKeys: t.length,
      optionalKeys: new Set(n),
    };
  }
  function Ln(e, t, n, r, i, a) {
    let o = [],
      s = i.keySet,
      c = i.catchall._zod,
      l = c.def.type,
      u = c.optin === `optional`,
      d = c.optout === `optional`;
    for (let i in t) {
      if (i === `__proto__` || s.has(i)) continue;
      if (l === `never`) {
        o.push(i);
        continue;
      }
      let a = c.run({ value: t[i], issues: [] }, r);
      a instanceof Promise
        ? e.push(a.then((e) => Fn(e, n, i, t, u, d)))
        : Fn(a, n, i, t, u, d);
    }
    return (
      o.length &&
        n.issues.push({
          code: `unrecognized_keys`,
          keys: o,
          input: t,
          inst: a,
        }),
      e.length ? Promise.all(e).then(() => n) : n
    );
  }
  var Rn = S(`$ZodObject`, (e, t) => {
      if ((Xt.init(e, t), !Object.getOwnPropertyDescriptor(t, `shape`)?.get)) {
        let e = t.shape;
        Object.defineProperty(t, "shape", {
          get: () => {
            let n = { ...e };
            return (Object.defineProperty(t, "shape", { value: n }), n);
          },
        });
      }
      let n = ae(() => In(t));
      T(e._zod, `propValues`, () => {
        let e = t.shape,
          n = {};
        for (let t in e) {
          let r = e[t]._zod;
          if (r.values) {
            n[t] ?? (n[t] = new Set());
            for (let e of r.values) n[t].add(e);
          }
        }
        return n;
      });
      let r = pe,
        i = t.catchall,
        a;
      e._zod.parse = (t, o) => {
        a ??= n.value;
        let s = t.value;
        if (!r(s))
          return (
            t.issues.push({
              expected: `object`,
              code: `invalid_type`,
              input: s,
              inst: e,
            }),
            t
          );
        t.value = {};
        let c = [],
          l = a.shape;
        for (let e of a.keys) {
          let n = l[e],
            r = n._zod.optin === `optional`,
            i = n._zod.optout === `optional`,
            a = n._zod.run({ value: s[e], issues: [] }, o);
          a instanceof Promise
            ? c.push(a.then((n) => Fn(n, t, e, s, r, i)))
            : Fn(a, t, e, s, r, i);
        }
        return i
          ? Ln(c, s, t, o, n.value, e)
          : c.length
            ? Promise.all(c).then(() => t)
            : t;
      };
    }),
    zn = S(`$ZodObjectJIT`, (e, t) => {
      Rn.init(e, t);
      let n = e._zod.parse,
        r = ae(() => In(t)),
        i = (e) => {
          let t = new Jt([`shape`, `payload`, `ctx`]),
            n = r.value,
            i = (e) => {
              let t = ue(e);
              return `shape[${t}]._zod.run({ value: input[${t}], issues: [] }, ctx)`;
            };
          t.write(`const input = payload.value;`);
          let a = Object.create(null),
            o = 0;
          for (let e of n.keys) a[e] = `key_${o++}`;
          t.write(`const newResult = {};`);
          for (let r of n.keys) {
            let n = a[r],
              o = ue(r),
              s = e[r],
              c = s?._zod?.optin === `optional`,
              l = s?._zod?.optout === `optional`;
            (t.write(`const ${n} = ${i(r)};`),
              c && l
                ? t.write(`
        if (${n}.issues.length) {
          if (${o} in input) {
            payload.issues = payload.issues.concat(${n}.issues.map(iss => ({
              ...iss,
              path: iss.path ? [${o}, ...iss.path] : [${o}]
            })));
          }
        }
        
        if (${n}.value === undefined) {
          if (${o} in input) {
            newResult[${o}] = undefined;
          }
        } else {
          newResult[${o}] = ${n}.value;
        }
        
      `)
                : c
                  ? t.write(`
        if (${n}.issues.length) {
          payload.issues = payload.issues.concat(${n}.issues.map(iss => ({
            ...iss,
            path: iss.path ? [${o}, ...iss.path] : [${o}]
          })));
        }
        
        if (${n}.value === undefined) {
          if (${o} in input) {
            newResult[${o}] = undefined;
          }
        } else {
          newResult[${o}] = ${n}.value;
        }
        
      `)
                  : t.write(`
        const ${n}_present = ${o} in input;
        if (${n}.issues.length) {
          payload.issues = payload.issues.concat(${n}.issues.map(iss => ({
            ...iss,
            path: iss.path ? [${o}, ...iss.path] : [${o}]
          })));
        }
        if (!${n}_present && !${n}.issues.length) {
          payload.issues.push({
            code: "invalid_type",
            expected: "nonoptional",
            input: undefined,
            path: [${o}]
          });
        }

        if (${n}_present) {
          if (${n}.value === undefined) {
            newResult[${o}] = undefined;
          } else {
            newResult[${o}] = ${n}.value;
          }
        }

      `));
          }
          (t.write(`payload.value = newResult;`), t.write(`return payload;`));
          let s = t.compile();
          return (t, n) => s(e, t, n);
        },
        a,
        o = pe,
        s = !te.jitless,
        c = s && O.value,
        l = t.catchall,
        u;
      e._zod.parse = (d, f) => {
        u ??= r.value;
        let p = d.value;
        return o(p)
          ? s && c && f?.async === !1 && f.jitless !== !0
            ? ((a ||= i(t.shape)), (d = a(d, f)), l ? Ln([], p, d, f, u, e) : d)
            : n(d, f)
          : (d.issues.push({
              expected: `object`,
              code: `invalid_type`,
              input: p,
              inst: e,
            }),
            d);
      };
    });
  function Bn(e, t, n, r) {
    for (let n of e) if (n.issues.length === 0) return ((t.value = n.value), t);
    let i = e.filter((e) => !De(e));
    return i.length === 1
      ? ((t.value = i[0].value), i[0])
      : (t.issues.push({
          code: `invalid_union`,
          input: t.value,
          inst: n,
          errors: e.map((e) => e.issues.map((e) => je(e, r, ne()))),
        }),
        t);
  }
  var Vn = S(`$ZodUnion`, (e, t) => {
      (Xt.init(e, t),
        T(e._zod, `optin`, () =>
          t.options.some((e) => e._zod.optin === `optional`)
            ? `optional`
            : void 0,
        ),
        T(e._zod, `optout`, () =>
          t.options.some((e) => e._zod.optout === `optional`)
            ? `optional`
            : void 0,
        ),
        T(e._zod, `values`, () => {
          if (t.options.every((e) => e._zod.values))
            return new Set(t.options.flatMap((e) => Array.from(e._zod.values)));
        }),
        T(e._zod, `pattern`, () => {
          if (t.options.every((e) => e._zod.pattern)) {
            let e = t.options.map((e) => e._zod.pattern);
            return RegExp(`^(${e.map((e) => se(e.source)).join(`|`)})$`);
          }
        }));
      let n = t.options.length === 1 ? t.options[0]._zod.run : null;
      e._zod.parse = (r, i) => {
        if (n) return n(r, i);
        let a = !1,
          o = [];
        for (let e of t.options) {
          let t = e._zod.run({ value: r.value, issues: [] }, i);
          if (t instanceof Promise) (o.push(t), (a = !0));
          else {
            if (t.issues.length === 0) return t;
            o.push(t);
          }
        }
        return a ? Promise.all(o).then((t) => Bn(t, r, e, i)) : Bn(o, r, e, i);
      };
    }),
    Hn = S(`$ZodDiscriminatedUnion`, (e, t) => {
      ((t.inclusive = !1), Vn.init(e, t));
      let n = e._zod.parse;
      T(e._zod, `propValues`, () => {
        let e = {};
        for (let n of t.options) {
          let r = n._zod.propValues;
          if (!r || Object.keys(r).length === 0)
            throw Error(
              `Invalid discriminated union option at index "${t.options.indexOf(n)}"`,
            );
          for (let [t, n] of Object.entries(r)) {
            e[t] || (e[t] = new Set());
            for (let r of n) e[t].add(r);
          }
        }
        return e;
      });
      let r = ae(() => {
        let e = t.options,
          n = new Map();
        for (let r of e) {
          let e = r._zod.propValues?.[t.discriminator];
          if (!e || e.size === 0)
            throw Error(
              `Invalid discriminated union option at index "${t.options.indexOf(r)}"`,
            );
          for (let t of e) {
            if (n.has(t))
              throw Error(`Duplicate discriminator value "${String(t)}"`);
            n.set(t, r);
          }
        }
        return n;
      });
      e._zod.parse = (i, a) => {
        let o = i.value;
        if (!pe(o))
          return (
            i.issues.push({
              code: `invalid_type`,
              expected: `object`,
              input: o,
              inst: e,
            }),
            i
          );
        let s = r.value.get(o?.[t.discriminator]);
        return s
          ? s._zod.run(i, a)
          : t.unionFallback || a.direction === `backward`
            ? n(i, a)
            : (i.issues.push({
                code: `invalid_union`,
                errors: [],
                note: `No matching discriminator`,
                discriminator: t.discriminator,
                options: Array.from(r.value.keys()),
                input: o,
                path: [t.discriminator],
                inst: e,
              }),
              i);
      };
    }),
    Un = S(`$ZodIntersection`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.parse = (e, n) => {
          let r = e.value,
            i = t.left._zod.run({ value: r, issues: [] }, n),
            a = t.right._zod.run({ value: r, issues: [] }, n);
          return i instanceof Promise || a instanceof Promise
            ? Promise.all([i, a]).then(([t, n]) => Gn(e, t, n))
            : Gn(e, i, a);
        }));
    });
  function Wn(e, t) {
    if (e === t || (e instanceof Date && t instanceof Date && +e == +t))
      return { valid: !0, data: e };
    if (k(e) && k(t)) {
      let n = Object.keys(t),
        r = Object.keys(e).filter((e) => n.indexOf(e) !== -1),
        i = { ...e, ...t };
      for (let n of r) {
        let r = Wn(e[n], t[n]);
        if (!r.valid)
          return { valid: !1, mergeErrorPath: [n, ...r.mergeErrorPath] };
        i[n] = r.data;
      }
      return { valid: !0, data: i };
    }
    if (Array.isArray(e) && Array.isArray(t)) {
      if (e.length !== t.length) return { valid: !1, mergeErrorPath: [] };
      let n = [];
      for (let r = 0; r < e.length; r++) {
        let i = e[r],
          a = t[r],
          o = Wn(i, a);
        if (!o.valid)
          return { valid: !1, mergeErrorPath: [r, ...o.mergeErrorPath] };
        n.push(o.data);
      }
      return { valid: !0, data: n };
    }
    return { valid: !1, mergeErrorPath: [] };
  }
  function Gn(e, t, n) {
    let r = new Map(),
      i;
    for (let n of t.issues)
      if (n.code === `unrecognized_keys`) {
        i ??= n;
        for (let e of n.keys) (r.has(e) || r.set(e, {}), (r.get(e).l = !0));
      } else e.issues.push(n);
    for (let t of n.issues)
      if (t.code === `unrecognized_keys`)
        for (let e of t.keys) (r.has(e) || r.set(e, {}), (r.get(e).r = !0));
      else e.issues.push(t);
    let a = [...r].filter(([, e]) => e.l && e.r).map(([e]) => e);
    if ((a.length && i && e.issues.push({ ...i, keys: a }), De(e))) return e;
    let o = Wn(t.value, n.value);
    if (!o.valid)
      throw Error(
        `Unmergable intersection. Error path: ${JSON.stringify(o.mergeErrorPath)}`,
      );
    return ((e.value = o.data), e);
  }
  var Kn = S(`$ZodRecord`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.parse = (n, r) => {
          let i = n.value;
          if (!k(i))
            return (
              n.issues.push({
                expected: `record`,
                code: `invalid_type`,
                input: i,
                inst: e,
              }),
              n
            );
          let a = [],
            o = t.keyType._zod.values;
          if (o) {
            n.value = {};
            let s = new Set();
            for (let c of o)
              if (
                typeof c == `string` ||
                typeof c == `number` ||
                typeof c == `symbol`
              ) {
                s.add(typeof c == `number` ? c.toString() : c);
                let o = t.keyType._zod.run({ value: c, issues: [] }, r);
                if (o instanceof Promise)
                  throw Error(
                    `Async schemas not supported in object keys currently`,
                  );
                if (o.issues.length) {
                  n.issues.push({
                    code: `invalid_key`,
                    origin: `record`,
                    issues: o.issues.map((e) => je(e, r, ne())),
                    input: c,
                    path: [c],
                    inst: e,
                  });
                  continue;
                }
                let l = o.value,
                  u = t.valueType._zod.run({ value: i[c], issues: [] }, r);
                u instanceof Promise
                  ? a.push(
                      u.then((e) => {
                        (e.issues.length && n.issues.push(...ke(c, e.issues)),
                          (n.value[l] = e.value));
                      }),
                    )
                  : (u.issues.length && n.issues.push(...ke(c, u.issues)),
                    (n.value[l] = u.value));
              }
            let c;
            for (let e in i) s.has(e) || ((c ??= []), c.push(e));
            c &&
              c.length > 0 &&
              n.issues.push({
                code: `unrecognized_keys`,
                input: i,
                inst: e,
                keys: c,
              });
          } else {
            n.value = {};
            for (let o of Reflect.ownKeys(i)) {
              if (
                o === `__proto__` ||
                !Object.prototype.propertyIsEnumerable.call(i, o)
              )
                continue;
              let s = t.keyType._zod.run({ value: o, issues: [] }, r);
              if (s instanceof Promise)
                throw Error(
                  `Async schemas not supported in object keys currently`,
                );
              if (typeof o == `string` && Et.test(o) && s.issues.length) {
                let e = t.keyType._zod.run({ value: Number(o), issues: [] }, r);
                if (e instanceof Promise)
                  throw Error(
                    `Async schemas not supported in object keys currently`,
                  );
                e.issues.length === 0 && (s = e);
              }
              if (s.issues.length) {
                t.mode === `loose`
                  ? (n.value[o] = i[o])
                  : n.issues.push({
                      code: `invalid_key`,
                      origin: `record`,
                      issues: s.issues.map((e) => je(e, r, ne())),
                      input: o,
                      path: [o],
                      inst: e,
                    });
                continue;
              }
              let c = t.valueType._zod.run({ value: i[o], issues: [] }, r);
              c instanceof Promise
                ? a.push(
                    c.then((e) => {
                      (e.issues.length && n.issues.push(...ke(o, e.issues)),
                        (n.value[s.value] = e.value));
                    }),
                  )
                : (c.issues.length && n.issues.push(...ke(o, c.issues)),
                  (n.value[s.value] = c.value));
            }
          }
          return a.length ? Promise.all(a).then(() => n) : n;
        }));
    }),
    qn = S(`$ZodEnum`, (e, t) => {
      Xt.init(e, t);
      let n = re(t.entries),
        r = new Set(n);
      ((e._zod.values = r),
        (e._zod.pattern = RegExp(
          `^(${n
            .filter((e) => he.has(typeof e))
            .map((e) => (typeof e == `string` ? ge(e) : e.toString()))
            .join(`|`)})$`,
        )),
        (e._zod.parse = (t, i) => {
          let a = t.value;
          return (
            r.has(a) ||
              t.issues.push({
                code: `invalid_value`,
                values: n,
                input: a,
                inst: e,
              }),
            t
          );
        }));
    }),
    Jn = S(`$ZodLiteral`, (e, t) => {
      if ((Xt.init(e, t), t.values.length === 0))
        throw Error(`Cannot create literal schema with no valid values`);
      let n = new Set(t.values);
      ((e._zod.values = n),
        (e._zod.pattern = RegExp(
          `^(${t.values.map((e) => (typeof e == `string` ? ge(e) : e ? ge(e.toString()) : String(e))).join(`|`)})$`,
        )),
        (e._zod.parse = (r, i) => {
          let a = r.value;
          return (
            n.has(a) ||
              r.issues.push({
                code: `invalid_value`,
                values: t.values,
                input: a,
                inst: e,
              }),
            r
          );
        }));
    }),
    Yn = S(`$ZodTransform`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.optin = `optional`),
        (e._zod.parse = (n, r) => {
          if (r.direction === `backward`) throw new w(e.constructor.name);
          let i = t.transform(n.value, n);
          if (r.async)
            return (i instanceof Promise ? i : Promise.resolve(i)).then(
              (e) => ((n.value = e), (n.fallback = !0), n),
            );
          if (i instanceof Promise) throw new C();
          return ((n.value = i), (n.fallback = !0), n);
        }));
    });
  function Xn(e, t) {
    return t === void 0 && (e.issues.length || e.fallback)
      ? { issues: [], value: void 0 }
      : e;
  }
  var Zn = S(`$ZodOptional`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.optin = `optional`),
        (e._zod.optout = `optional`),
        T(e._zod, `values`, () =>
          t.innerType._zod.values
            ? new Set([...t.innerType._zod.values, void 0])
            : void 0,
        ),
        T(e._zod, `pattern`, () => {
          let e = t.innerType._zod.pattern;
          return e ? RegExp(`^(${se(e.source)})?$`) : void 0;
        }),
        (e._zod.parse = (e, n) => {
          if (t.innerType._zod.optin === `optional`) {
            let r = e.value,
              i = t.innerType._zod.run(e, n);
            return i instanceof Promise ? i.then((e) => Xn(e, r)) : Xn(i, r);
          }
          return e.value === void 0 ? e : t.innerType._zod.run(e, n);
        }));
    }),
    Qn = S(`$ZodExactOptional`, (e, t) => {
      (Zn.init(e, t),
        T(e._zod, `values`, () => t.innerType._zod.values),
        T(e._zod, `pattern`, () => t.innerType._zod.pattern),
        (e._zod.parse = (e, n) => t.innerType._zod.run(e, n)));
    }),
    $n = S(`$ZodNullable`, (e, t) => {
      (Xt.init(e, t),
        T(e._zod, `optin`, () => t.innerType._zod.optin),
        T(e._zod, `optout`, () => t.innerType._zod.optout),
        T(e._zod, `pattern`, () => {
          let e = t.innerType._zod.pattern;
          return e ? RegExp(`^(${se(e.source)}|null)$`) : void 0;
        }),
        T(e._zod, `values`, () =>
          t.innerType._zod.values
            ? new Set([...t.innerType._zod.values, null])
            : void 0,
        ),
        (e._zod.parse = (e, n) =>
          e.value === null ? e : t.innerType._zod.run(e, n)));
    }),
    er = S(`$ZodDefault`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.optin = `optional`),
        T(e._zod, `values`, () => t.innerType._zod.values),
        (e._zod.parse = (e, n) => {
          if (n.direction === `backward`) return t.innerType._zod.run(e, n);
          if (e.value === void 0) return ((e.value = t.defaultValue), e);
          let r = t.innerType._zod.run(e, n);
          return r instanceof Promise ? r.then((e) => tr(e, t)) : tr(r, t);
        }));
    });
  function tr(e, t) {
    return (e.value === void 0 && (e.value = t.defaultValue), e);
  }
  var nr = S(`$ZodPrefault`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.optin = `optional`),
        T(e._zod, `values`, () => t.innerType._zod.values),
        (e._zod.parse = (e, n) => (
          n.direction === `backward` ||
            (e.value === void 0 && (e.value = t.defaultValue)),
          t.innerType._zod.run(e, n)
        )));
    }),
    rr = S(`$ZodNonOptional`, (e, t) => {
      (Xt.init(e, t),
        T(e._zod, `values`, () => {
          let e = t.innerType._zod.values;
          return e ? new Set([...e].filter((e) => e !== void 0)) : void 0;
        }),
        (e._zod.parse = (n, r) => {
          let i = t.innerType._zod.run(n, r);
          return i instanceof Promise ? i.then((t) => ir(t, e)) : ir(i, e);
        }));
    });
  function ir(e, t) {
    return (
      !e.issues.length &&
        e.value === void 0 &&
        e.issues.push({
          code: `invalid_type`,
          expected: `nonoptional`,
          input: e.value,
          inst: t,
        }),
      e
    );
  }
  var ar = S(`$ZodCatch`, (e, t) => {
      (Xt.init(e, t),
        (e._zod.optin = `optional`),
        T(e._zod, `optout`, () => t.innerType._zod.optout),
        T(e._zod, `values`, () => t.innerType._zod.values),
        (e._zod.parse = (e, n) => {
          if (n.direction === `backward`) return t.innerType._zod.run(e, n);
          let r = t.innerType._zod.run(e, n);
          return r instanceof Promise
            ? r.then(
                (r) => (
                  (e.value = r.value),
                  r.issues.length &&
                    ((e.value = t.catchValue({
                      ...e,
                      error: { issues: r.issues.map((e) => je(e, n, ne())) },
                      input: e.value,
                    })),
                    (e.issues = []),
                    (e.fallback = !0)),
                  e
                ),
              )
            : ((e.value = r.value),
              r.issues.length &&
                ((e.value = t.catchValue({
                  ...e,
                  error: { issues: r.issues.map((e) => je(e, n, ne())) },
                  input: e.value,
                })),
                (e.issues = []),
                (e.fallback = !0)),
              e);
        }));
    }),
    or = S(`$ZodPipe`, (e, t) => {
      (Xt.init(e, t),
        T(e._zod, `values`, () => t.in._zod.values),
        T(e._zod, `optin`, () => t.in._zod.optin),
        T(e._zod, `optout`, () => t.out._zod.optout),
        T(e._zod, `propValues`, () => t.in._zod.propValues),
        (e._zod.parse = (e, n) => {
          if (n.direction === `backward`) {
            let r = t.out._zod.run(e, n);
            return r instanceof Promise
              ? r.then((e) => sr(e, t.in, n))
              : sr(r, t.in, n);
          }
          let r = t.in._zod.run(e, n);
          return r instanceof Promise
            ? r.then((e) => sr(e, t.out, n))
            : sr(r, t.out, n);
        }));
    });
  function sr(e, t, n) {
    return e.issues.length
      ? ((e.aborted = !0), e)
      : t._zod.run(
          { value: e.value, issues: e.issues, fallback: e.fallback },
          n,
        );
  }
  var cr = S(`$ZodPreprocess`, (e, t) => {
      or.init(e, t);
    }),
    lr = S(`$ZodReadonly`, (e, t) => {
      (Xt.init(e, t),
        T(e._zod, `propValues`, () => t.innerType._zod.propValues),
        T(e._zod, `values`, () => t.innerType._zod.values),
        T(e._zod, `optin`, () => t.innerType?._zod?.optin),
        T(e._zod, `optout`, () => t.innerType?._zod?.optout),
        (e._zod.parse = (e, n) => {
          if (n.direction === `backward`) return t.innerType._zod.run(e, n);
          let r = t.innerType._zod.run(e, n);
          return r instanceof Promise ? r.then(ur) : ur(r);
        }));
    });
  function ur(e) {
    return ((e.value = Object.freeze(e.value)), e);
  }
  var dr = S(`$ZodCustom`, (e, t) => {
    (jt.init(e, t),
      Xt.init(e, t),
      (e._zod.parse = (e, t) => e),
      (e._zod.check = (n) => {
        let r = n.value,
          i = t.fn(r);
        if (i instanceof Promise) return i.then((t) => fr(t, n, r, e));
        fr(i, n, r, e);
      }));
  });
  function fr(e, t, n, r) {
    if (!e) {
      let e = {
        code: `custom`,
        input: n,
        inst: r,
        path: [...(r._zod.def.path ?? [])],
        continue: !r._zod.def.abort,
      };
      (r._zod.def.params && (e.params = r._zod.def.params),
        t.issues.push(Ne(e)));
    }
  }
  var pr,
    mr = class {
      constructor() {
        ((this._map = new WeakMap()), (this._idmap = new Map()));
      }
      add(e, ...t) {
        let n = t[0];
        return (
          this._map.set(e, n),
          n && typeof n == `object` && `id` in n && this._idmap.set(n.id, e),
          this
        );
      }
      clear() {
        return ((this._map = new WeakMap()), (this._idmap = new Map()), this);
      }
      remove(e) {
        let t = this._map.get(e);
        return (
          t && typeof t == `object` && `id` in t && this._idmap.delete(t.id),
          this._map.delete(e),
          this
        );
      }
      get(e) {
        let t = e._zod.parent;
        if (t) {
          let n = { ...(this.get(t) ?? {}) };
          delete n.id;
          let r = { ...n, ...this._map.get(e) };
          return Object.keys(r).length ? r : void 0;
        }
        return this._map.get(e);
      }
      has(e) {
        return this._map.has(e);
      }
    };
  function hr() {
    return new mr();
  }
  (pr = globalThis).__zod_globalRegistry ?? (pr.__zod_globalRegistry = hr());
  var gr = globalThis.__zod_globalRegistry;
  function _r(e, t) {
    return new e({ type: `string`, ...A(t) });
  }
  function vr(e, t) {
    return new e({
      type: `string`,
      format: `email`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function yr(e, t) {
    return new e({
      type: `string`,
      format: `guid`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function br(e, t) {
    return new e({
      type: `string`,
      format: `uuid`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function xr(e, t) {
    return new e({
      type: `string`,
      format: `uuid`,
      check: `string_format`,
      abort: !1,
      version: `v4`,
      ...A(t),
    });
  }
  function Sr(e, t) {
    return new e({
      type: `string`,
      format: `uuid`,
      check: `string_format`,
      abort: !1,
      version: `v6`,
      ...A(t),
    });
  }
  function Cr(e, t) {
    return new e({
      type: `string`,
      format: `uuid`,
      check: `string_format`,
      abort: !1,
      version: `v7`,
      ...A(t),
    });
  }
  function wr(e, t) {
    return new e({
      type: `string`,
      format: `url`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Tr(e, t) {
    return new e({
      type: `string`,
      format: `emoji`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Er(e, t) {
    return new e({
      type: `string`,
      format: `nanoid`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Dr(e, t) {
    return new e({
      type: `string`,
      format: `cuid`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Or(e, t) {
    return new e({
      type: `string`,
      format: `cuid2`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function kr(e, t) {
    return new e({
      type: `string`,
      format: `ulid`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Ar(e, t) {
    return new e({
      type: `string`,
      format: `xid`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function jr(e, t) {
    return new e({
      type: `string`,
      format: `ksuid`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Mr(e, t) {
    return new e({
      type: `string`,
      format: `ipv4`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Nr(e, t) {
    return new e({
      type: `string`,
      format: `ipv6`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Pr(e, t) {
    return new e({
      type: `string`,
      format: `cidrv4`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Fr(e, t) {
    return new e({
      type: `string`,
      format: `cidrv6`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Ir(e, t) {
    return new e({
      type: `string`,
      format: `base64`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Lr(e, t) {
    return new e({
      type: `string`,
      format: `base64url`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Rr(e, t) {
    return new e({
      type: `string`,
      format: `e164`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function zr(e, t) {
    return new e({
      type: `string`,
      format: `jwt`,
      check: `string_format`,
      abort: !1,
      ...A(t),
    });
  }
  function Br(e, t) {
    return new e({
      type: `string`,
      format: `datetime`,
      check: `string_format`,
      offset: !1,
      local: !1,
      precision: null,
      ...A(t),
    });
  }
  function Vr(e, t) {
    return new e({
      type: `string`,
      format: `date`,
      check: `string_format`,
      ...A(t),
    });
  }
  function Hr(e, t) {
    return new e({
      type: `string`,
      format: `time`,
      check: `string_format`,
      precision: null,
      ...A(t),
    });
  }
  function Ur(e, t) {
    return new e({
      type: `string`,
      format: `duration`,
      check: `string_format`,
      ...A(t),
    });
  }
  function Wr(e, t) {
    return new e({ type: `number`, checks: [], ...A(t) });
  }
  function Gr(e, t) {
    return new e({ type: `number`, coerce: !0, checks: [], ...A(t) });
  }
  function Kr(e, t) {
    return new e({
      type: `number`,
      check: `number_format`,
      abort: !1,
      format: `safeint`,
      ...A(t),
    });
  }
  function qr(e, t) {
    return new e({ type: `boolean`, ...A(t) });
  }
  function Jr(e, t) {
    return new e({ type: `null`, ...A(t) });
  }
  function Yr(e) {
    return new e({ type: `any` });
  }
  function Xr(e) {
    return new e({ type: `unknown` });
  }
  function Zr(e, t) {
    return new e({ type: `never`, ...A(t) });
  }
  function Qr(e, t) {
    return new Nt({ check: `less_than`, ...A(t), value: e, inclusive: !1 });
  }
  function $r(e, t) {
    return new Nt({ check: `less_than`, ...A(t), value: e, inclusive: !0 });
  }
  function ei(e, t) {
    return new Pt({ check: `greater_than`, ...A(t), value: e, inclusive: !1 });
  }
  function ti(e, t) {
    return new Pt({ check: `greater_than`, ...A(t), value: e, inclusive: !0 });
  }
  function ni(e, t) {
    return new Ft({ check: `multiple_of`, ...A(t), value: e });
  }
  function ri(e, t) {
    return new Lt({ check: `max_length`, ...A(t), maximum: e });
  }
  function ii(e, t) {
    return new Rt({ check: `min_length`, ...A(t), minimum: e });
  }
  function ai(e, t) {
    return new zt({ check: `length_equals`, ...A(t), length: e });
  }
  function oi(e, t) {
    return new Vt({
      check: `string_format`,
      format: `regex`,
      ...A(t),
      pattern: e,
    });
  }
  function si(e) {
    return new Ht({ check: `string_format`, format: `lowercase`, ...A(e) });
  }
  function ci(e) {
    return new Ut({ check: `string_format`, format: `uppercase`, ...A(e) });
  }
  function li(e, t) {
    return new Wt({
      check: `string_format`,
      format: `includes`,
      ...A(t),
      includes: e,
    });
  }
  function ui(e, t) {
    return new Gt({
      check: `string_format`,
      format: `starts_with`,
      ...A(t),
      prefix: e,
    });
  }
  function di(e, t) {
    return new Kt({
      check: `string_format`,
      format: `ends_with`,
      ...A(t),
      suffix: e,
    });
  }
  function fi(e) {
    return new qt({ check: `overwrite`, tx: e });
  }
  function pi(e) {
    return fi((t) => t.normalize(e));
  }
  function mi() {
    return fi((e) => e.trim());
  }
  function hi() {
    return fi((e) => e.toLowerCase());
  }
  function gi() {
    return fi((e) => e.toUpperCase());
  }
  function _i() {
    return fi((e) => de(e));
  }
  function vi(e, t, n) {
    return new e({ type: `array`, element: t, ...A(n) });
  }
  function yi(e, t, n) {
    let r = A(n);
    return (
      (r.abort ??= !0), new e({ type: `custom`, check: `custom`, fn: t, ...r })
    );
  }
  function bi(e, t, n) {
    return new e({ type: `custom`, check: `custom`, fn: t, ...A(n) });
  }
  function xi(e, t) {
    let n = Si(
      (t) => (
        (t.addIssue = (e) => {
          if (typeof e == `string`) t.issues.push(Ne(e, t.value, n._zod.def));
          else {
            let r = e;
            (r.fatal && (r.continue = !1),
              (r.code ??= `custom`),
              (r.input ??= t.value),
              (r.inst ??= n),
              (r.continue ??= !n._zod.def.abort),
              t.issues.push(Ne(r)));
          }
        }),
        e(t.value, t)
      ),
      t,
    );
    return n;
  }
  function Si(e, t) {
    let n = new jt({ check: `custom`, ...A(t) });
    return ((n._zod.check = e), n);
  }
  function Ci(e) {
    let t = e?.target ?? `draft-2020-12`;
    return (
      t === `draft-4` && (t = `draft-04`),
      t === `draft-7` && (t = `draft-07`),
      {
        processors: e.processors ?? {},
        metadataRegistry: e?.metadata ?? gr,
        target: t,
        unrepresentable: e?.unrepresentable ?? `throw`,
        override: e?.override ?? (() => {}),
        io: e?.io ?? `output`,
        counter: 0,
        seen: new Map(),
        cycles: e?.cycles ?? `ref`,
        reused: e?.reused ?? `inline`,
        external: e?.external ?? void 0,
      }
    );
  }
  function wi(e, t, n = { path: [], schemaPath: [] }) {
    var r;
    let i = e._zod.def,
      a = t.seen.get(e);
    if (a)
      return (
        a.count++, n.schemaPath.includes(e) && (a.cycle = n.path), a.schema
      );
    let o = { schema: {}, count: 1, cycle: void 0, path: n.path };
    t.seen.set(e, o);
    let s = e._zod.toJSONSchema?.();
    if (s) o.schema = s;
    else {
      let r = { ...n, schemaPath: [...n.schemaPath, e], path: n.path };
      if (e._zod.processJSONSchema) e._zod.processJSONSchema(t, o.schema, r);
      else {
        let n = o.schema,
          a = t.processors[i.type];
        if (!a)
          throw Error(
            `[toJSONSchema]: Non-representable type encountered: ${i.type}`,
          );
        a(e, t, n, r);
      }
      let a = e._zod.parent;
      a && ((o.ref ||= a), wi(a, t, r), (t.seen.get(a).isParent = !0));
    }
    let c = t.metadataRegistry.get(e);
    return (
      c && Object.assign(o.schema, c),
      t.io === `input` &&
        Di(e) &&
        (delete o.schema.examples, delete o.schema.default),
      t.io === `input` &&
        `_prefault` in o.schema &&
        ((r = o.schema).default ?? (r.default = o.schema._prefault)),
      delete o.schema._prefault,
      t.seen.get(e).schema
    );
  }
  function Ti(e, t) {
    let n = e.seen.get(t);
    if (!n) throw Error(`Unprocessed schema. This is a bug in Zod.`);
    let r = new Map();
    for (let t of e.seen.entries()) {
      let n = e.metadataRegistry.get(t[0])?.id;
      if (n) {
        let e = r.get(n);
        if (e && e !== t[0])
          throw Error(
            `Duplicate schema id "${n}" detected during JSON Schema conversion. Two different schemas cannot share the same id when converted together.`,
          );
        r.set(n, t[0]);
      }
    }
    let i = (t) => {
        let r = e.target === `draft-2020-12` ? `$defs` : `definitions`;
        if (e.external) {
          let n = e.external.registry.get(t[0])?.id,
            i = e.external.uri ?? ((e) => e);
          if (n) return { ref: i(n) };
          let a = t[1].defId ?? t[1].schema.id ?? `schema${e.counter++}`;
          return (
            (t[1].defId = a), { defId: a, ref: `${i(`__shared`)}#/${r}/${a}` }
          );
        }
        if (t[1] === n) return { ref: `#` };
        let i = `#/${r}/`,
          a = t[1].schema.id ?? `__schema${e.counter++}`;
        return { defId: a, ref: i + a };
      },
      a = (e) => {
        if (e[1].schema.$ref) return;
        let t = e[1],
          { ref: n, defId: r } = i(e);
        ((t.def = { ...t.schema }), r && (t.defId = r));
        let a = t.schema;
        for (let e in a) delete a[e];
        a.$ref = n;
      };
    if (e.cycles === `throw`)
      for (let t of e.seen.entries()) {
        let e = t[1];
        if (e.cycle)
          throw Error(`Cycle detected: #/${e.cycle?.join(`/`)}/<root>

Set the \`cycles\` parameter to \`"ref"\` to resolve cyclical schemas with defs.`);
      }
    for (let n of e.seen.entries()) {
      let r = n[1];
      if (t === n[0]) {
        a(n);
        continue;
      }
      if (e.external) {
        let r = e.external.registry.get(n[0])?.id;
        if (t !== n[0] && r) {
          a(n);
          continue;
        }
      }
      if (e.metadataRegistry.get(n[0])?.id) {
        a(n);
        continue;
      }
      if (r.cycle) {
        a(n);
        continue;
      }
      if (r.count > 1 && e.reused === `ref`) {
        a(n);
        continue;
      }
    }
  }
  function Ei(e, t) {
    let n = e.seen.get(t);
    if (!n) throw Error(`Unprocessed schema. This is a bug in Zod.`);
    let r = (t) => {
      let n = e.seen.get(t);
      if (n.ref === null) return;
      let i = n.def ?? n.schema,
        a = { ...i },
        o = n.ref;
      if (((n.ref = null), o)) {
        r(o);
        let n = e.seen.get(o),
          s = n.schema;
        if (
          (s.$ref &&
          (e.target === `draft-07` ||
            e.target === `draft-04` ||
            e.target === `openapi-3.0`)
            ? ((i.allOf = i.allOf ?? []), i.allOf.push(s))
            : Object.assign(i, s),
          Object.assign(i, a),
          t._zod.parent === o)
        )
          for (let e in i)
            e === `$ref` || e === `allOf` || e in a || delete i[e];
        if (s.$ref && n.def)
          for (let e in i)
            e === `$ref` ||
              e === `allOf` ||
              (e in n.def &&
                JSON.stringify(i[e]) === JSON.stringify(n.def[e]) &&
                delete i[e]);
      }
      let s = t._zod.parent;
      if (s && s !== o) {
        r(s);
        let t = e.seen.get(s);
        if (t?.schema.$ref && ((i.$ref = t.schema.$ref), t.def))
          for (let e in i)
            e === `$ref` ||
              e === `allOf` ||
              (e in t.def &&
                JSON.stringify(i[e]) === JSON.stringify(t.def[e]) &&
                delete i[e]);
      }
      e.override({ zodSchema: t, jsonSchema: i, path: n.path ?? [] });
    };
    for (let t of [...e.seen.entries()].reverse()) r(t[0]);
    let i = {};
    if (
      (e.target === `draft-2020-12`
        ? (i.$schema = `https://json-schema.org/draft/2020-12/schema`)
        : e.target === `draft-07`
          ? (i.$schema = `http://json-schema.org/draft-07/schema#`)
          : e.target === `draft-04`
            ? (i.$schema = `http://json-schema.org/draft-04/schema#`)
            : e.target,
      e.external?.uri)
    ) {
      let n = e.external.registry.get(t)?.id;
      if (!n) throw Error("Schema is missing an `id` property");
      i.$id = e.external.uri(n);
    }
    Object.assign(i, n.def ?? n.schema);
    let a = e.metadataRegistry.get(t)?.id;
    a !== void 0 && i.id === a && delete i.id;
    let o = e.external?.defs ?? {};
    for (let t of e.seen.entries()) {
      let e = t[1];
      e.def &&
        e.defId &&
        (e.def.id === e.defId && delete e.def.id, (o[e.defId] = e.def));
    }
    e.external ||
      (Object.keys(o).length > 0 &&
        (e.target === `draft-2020-12` ? (i.$defs = o) : (i.definitions = o)));
    try {
      let n = JSON.parse(JSON.stringify(i));
      return (
        Object.defineProperty(n, "~standard", {
          value: {
            ...t[`~standard`],
            jsonSchema: {
              input: ki(t, `input`, e.processors),
              output: ki(t, `output`, e.processors),
            },
          },
          enumerable: !1,
          writable: !1,
        }),
        n
      );
    } catch {
      throw Error(`Error converting schema to JSON.`);
    }
  }
  function Di(e, t) {
    let n = t ?? { seen: new Set() };
    if (n.seen.has(e)) return !1;
    n.seen.add(e);
    let r = e._zod.def;
    if (r.type === `transform`) return !0;
    if (r.type === `array`) return Di(r.element, n);
    if (r.type === `set`) return Di(r.valueType, n);
    if (r.type === `lazy`) return Di(r.getter(), n);
    if (
      r.type === `promise` ||
      r.type === `optional` ||
      r.type === `nonoptional` ||
      r.type === `nullable` ||
      r.type === `readonly` ||
      r.type === "default" ||
      r.type === `prefault`
    )
      return Di(r.innerType, n);
    if (r.type === `intersection`) return Di(r.left, n) || Di(r.right, n);
    if (r.type === `record` || r.type === `map`)
      return Di(r.keyType, n) || Di(r.valueType, n);
    if (r.type === `pipe`)
      return e._zod.traits.has(`$ZodCodec`) ? !0 : Di(r.in, n) || Di(r.out, n);
    if (r.type === `object`) {
      for (let e in r.shape) if (Di(r.shape[e], n)) return !0;
      return !1;
    }
    if (r.type === `union`) {
      for (let e of r.options) if (Di(e, n)) return !0;
      return !1;
    }
    if (r.type === `tuple`) {
      for (let e of r.items) if (Di(e, n)) return !0;
      return !!(r.rest && Di(r.rest, n));
    }
    return !1;
  }
  var Oi =
      (e, t = {}) =>
      (n) => {
        let r = Ci({ ...n, processors: t });
        return (wi(e, r), Ti(r, e), Ei(r, e));
      },
    ki =
      (e, t, n = {}) =>
      (r) => {
        let { libraryOptions: i, target: a } = r ?? {},
          o = Ci({ ...(i ?? {}), target: a, io: t, processors: n });
        return (wi(e, o), Ti(o, e), Ei(o, e));
      },
    Ai = {
      guid: `uuid`,
      url: `uri`,
      datetime: `date-time`,
      json_string: `json-string`,
      regex: ``,
    },
    ji = (e, t, n, r) => {
      let i = n;
      i.type = `string`;
      let {
        minimum: a,
        maximum: o,
        format: s,
        patterns: c,
        contentEncoding: l,
      } = e._zod.bag;
      if (
        (typeof a == `number` && (i.minLength = a),
        typeof o == `number` && (i.maxLength = o),
        s &&
          ((i.format = Ai[s] ?? s),
          i.format === `` && delete i.format,
          s === `time` && delete i.format),
        l && (i.contentEncoding = l),
        c && c.size > 0)
      ) {
        let e = [...c];
        e.length === 1
          ? (i.pattern = e[0].source)
          : e.length > 1 &&
            (i.allOf = [
              ...e.map((e) => ({
                ...(t.target === `draft-07` ||
                t.target === `draft-04` ||
                t.target === `openapi-3.0`
                  ? { type: `string` }
                  : {}),
                pattern: e.source,
              })),
            ]);
      }
    },
    Mi = (e, t, n, r) => {
      let i = n,
        {
          minimum: a,
          maximum: o,
          format: s,
          multipleOf: c,
          exclusiveMaximum: l,
          exclusiveMinimum: u,
        } = e._zod.bag;
      typeof s == `string` && s.includes(`int`)
        ? (i.type = `integer`)
        : (i.type = `number`);
      let d = typeof u == `number` && u >= (a ?? -1 / 0),
        f = typeof l == `number` && l <= (o ?? 1 / 0),
        p = t.target === `draft-04` || t.target === `openapi-3.0`;
      (d
        ? p
          ? ((i.minimum = u), (i.exclusiveMinimum = !0))
          : (i.exclusiveMinimum = u)
        : typeof a == `number` && (i.minimum = a),
        f
          ? p
            ? ((i.maximum = l), (i.exclusiveMaximum = !0))
            : (i.exclusiveMaximum = l)
          : typeof o == `number` && (i.maximum = o),
        typeof c == `number` && (i.multipleOf = c));
    },
    Ni = (e, t, n, r) => {
      n.type = `boolean`;
    },
    Pi = (e, t, n, r) => {
      t.target === `openapi-3.0`
        ? ((n.type = `string`), (n.nullable = !0), (n.enum = [null]))
        : (n.type = `null`);
    },
    Fi = (e, t, n, r) => {
      n.not = {};
    },
    Ii = (e, t, n, r) => {
      let i = e._zod.def,
        a = re(i.entries);
      (a.every((e) => typeof e == `number`) && (n.type = `number`),
        a.every((e) => typeof e == `string`) && (n.type = `string`),
        (n.enum = a));
    },
    Li = (e, t, n, r) => {
      let i = e._zod.def,
        a = [];
      for (let e of i.values)
        if (e === void 0) {
          if (t.unrepresentable === `throw`)
            throw Error(
              "Literal `undefined` cannot be represented in JSON Schema",
            );
        } else if (typeof e == `bigint`) {
          if (t.unrepresentable === `throw`)
            throw Error(`BigInt literals cannot be represented in JSON Schema`);
          a.push(Number(e));
        } else a.push(e);
      if (a.length !== 0)
        if (a.length === 1) {
          let e = a[0];
          ((n.type = e === null ? `null` : typeof e),
            t.target === `draft-04` || t.target === `openapi-3.0`
              ? (n.enum = [e])
              : (n.const = e));
        } else
          (a.every((e) => typeof e == `number`) && (n.type = `number`),
            a.every((e) => typeof e == `string`) && (n.type = `string`),
            a.every((e) => typeof e == `boolean`) && (n.type = `boolean`),
            a.every((e) => e === null) && (n.type = `null`),
            (n.enum = a));
    },
    Ri = (e, t, n, r) => {
      if (t.unrepresentable === `throw`)
        throw Error(`Custom types cannot be represented in JSON Schema`);
    },
    zi = (e, t, n, r) => {
      if (t.unrepresentable === `throw`)
        throw Error(`Transforms cannot be represented in JSON Schema`);
    },
    Bi = (e, t, n, r) => {
      let i = n,
        a = e._zod.def,
        { minimum: o, maximum: s } = e._zod.bag;
      (typeof o == `number` && (i.minItems = o),
        typeof s == `number` && (i.maxItems = s),
        (i.type = `array`),
        (i.items = wi(a.element, t, { ...r, path: [...r.path, `items`] })));
    },
    Vi = (e, t, n, r) => {
      let i = n,
        a = e._zod.def;
      ((i.type = `object`), (i.properties = {}));
      let o = a.shape;
      for (let e in o)
        i.properties[e] = wi(o[e], t, {
          ...r,
          path: [...r.path, `properties`, e],
        });
      let s = new Set(Object.keys(o)),
        c = new Set(
          [...s].filter((e) => {
            let n = a.shape[e]._zod;
            return t.io === `input` ? n.optin === void 0 : n.optout === void 0;
          }),
        );
      (c.size > 0 && (i.required = Array.from(c)),
        a.catchall?._zod.def.type === `never`
          ? (i.additionalProperties = !1)
          : a.catchall
            ? a.catchall &&
              (i.additionalProperties = wi(a.catchall, t, {
                ...r,
                path: [...r.path, `additionalProperties`],
              }))
            : t.io === `output` && (i.additionalProperties = !1));
    },
    Hi = (e, t, n, r) => {
      let i = e._zod.def,
        a = i.inclusive === !1,
        o = i.options.map((e, n) =>
          wi(e, t, { ...r, path: [...r.path, a ? `oneOf` : `anyOf`, n] }),
        );
      a ? (n.oneOf = o) : (n.anyOf = o);
    },
    j = (e, t, n, r) => {
      let i = e._zod.def,
        a = wi(i.left, t, { ...r, path: [...r.path, `allOf`, 0] }),
        o = wi(i.right, t, { ...r, path: [...r.path, `allOf`, 1] }),
        s = (e) => `allOf` in e && Object.keys(e).length === 1;
      n.allOf = [...(s(a) ? a.allOf : [a]), ...(s(o) ? o.allOf : [o])];
    },
    Ui = (e, t, n, r) => {
      let i = n,
        a = e._zod.def;
      i.type = `object`;
      let o = a.keyType,
        s = o._zod.bag?.patterns;
      if (a.mode === `loose` && s && s.size > 0) {
        let e = wi(a.valueType, t, {
          ...r,
          path: [...r.path, `patternProperties`, `*`],
        });
        i.patternProperties = {};
        for (let t of s) i.patternProperties[t.source] = e;
      } else
        ((t.target === `draft-07` || t.target === `draft-2020-12`) &&
          (i.propertyNames = wi(a.keyType, t, {
            ...r,
            path: [...r.path, `propertyNames`],
          })),
          (i.additionalProperties = wi(a.valueType, t, {
            ...r,
            path: [...r.path, `additionalProperties`],
          })));
      let c = o._zod.values;
      if (c) {
        let e = [...c].filter(
          (e) => typeof e == `string` || typeof e == `number`,
        );
        e.length > 0 && (i.required = e);
      }
    },
    Wi = (e, t, n, r) => {
      let i = e._zod.def,
        a = wi(i.innerType, t, r),
        o = t.seen.get(e);
      t.target === `openapi-3.0`
        ? ((o.ref = i.innerType), (n.nullable = !0))
        : (n.anyOf = [a, { type: `null` }]);
    },
    Gi = (e, t, n, r) => {
      let i = e._zod.def;
      wi(i.innerType, t, r);
      let a = t.seen.get(e);
      a.ref = i.innerType;
    },
    Ki = (e, t, n, r) => {
      let i = e._zod.def;
      wi(i.innerType, t, r);
      let a = t.seen.get(e);
      ((a.ref = i.innerType),
        (n.default = JSON.parse(JSON.stringify(i.defaultValue))));
    },
    qi = (e, t, n, r) => {
      let i = e._zod.def;
      wi(i.innerType, t, r);
      let a = t.seen.get(e);
      ((a.ref = i.innerType),
        t.io === `input` &&
          (n._prefault = JSON.parse(JSON.stringify(i.defaultValue))));
    },
    Ji = (e, t, n, r) => {
      let i = e._zod.def;
      wi(i.innerType, t, r);
      let a = t.seen.get(e);
      a.ref = i.innerType;
      let o;
      try {
        o = i.catchValue(void 0);
      } catch {
        throw Error(`Dynamic catch values are not supported in JSON Schema`);
      }
      n.default = o;
    },
    Yi = (e, t, n, r) => {
      let i = e._zod.def,
        a = i.in._zod.traits.has(`$ZodTransform`),
        o = t.io === `input` ? (a ? i.out : i.in) : i.out;
      wi(o, t, r);
      let s = t.seen.get(e);
      s.ref = o;
    },
    Xi = (e, t, n, r) => {
      let i = e._zod.def;
      wi(i.innerType, t, r);
      let a = t.seen.get(e);
      ((a.ref = i.innerType), (n.readOnly = !0));
    },
    Zi = (e, t, n, r) => {
      let i = e._zod.def;
      wi(i.innerType, t, r);
      let a = t.seen.get(e);
      a.ref = i.innerType;
    },
    Qi = b();
  function $i(e) {
    return !!e._zod;
  }
  function ea(e, t) {
    return $i(e) ? He(e, t) : e.safeParse(t);
  }
  function ta(e) {
    if (!e) return;
    let t;
    if (((t = $i(e) ? e._zod?.def?.shape : e.shape), t)) {
      if (typeof t == `function`)
        try {
          return t();
        } catch {
          return;
        }
      return t;
    }
  }
  function na(e) {
    if ($i(e)) {
      let t = e._zod?.def;
      if (t) {
        if (t.value !== void 0) return t.value;
        if (Array.isArray(t.values) && t.values.length > 0) return t.values[0];
      }
    }
    let t = e._def;
    if (t) {
      if (t.value !== void 0) return t.value;
      if (Array.isArray(t.values) && t.values.length > 0) return t.values[0];
    }
    let n = e.value;
    if (n !== void 0) return n;
  }
  var ra = S(`ZodISODateTime`, (e, t) => {
    (dn.init(e, t), Oa.init(e, t));
  });
  function ia(e) {
    return Br(ra, e);
  }
  var aa = S(`ZodISODate`, (e, t) => {
    (fn.init(e, t), Oa.init(e, t));
  });
  function oa(e) {
    return Vr(aa, e);
  }
  var sa = S(`ZodISOTime`, (e, t) => {
    (pn.init(e, t), Oa.init(e, t));
  });
  function ca(e) {
    return Hr(sa, e);
  }
  var la = S(`ZodISODuration`, (e, t) => {
    (mn.init(e, t), Oa.init(e, t));
  });
  function ua(e) {
    return Ur(la, e);
  }
  var da = S(
      `ZodError`,
      (e, t) => {
        (Fe.init(e, t),
          (e.name = `ZodError`),
          Object.defineProperties(e, {
            format: { value: (t) => Re(e, t) },
            flatten: { value: (t) => Le(e, t) },
            addIssue: {
              value: (t) => {
                (e.issues.push(t),
                  (e.message = JSON.stringify(e.issues, ie, 2)));
              },
            },
            addIssues: {
              value: (t) => {
                (e.issues.push(...t),
                  (e.message = JSON.stringify(e.issues, ie, 2)));
              },
            },
            isEmpty: {
              get() {
                return e.issues.length === 0;
              },
            },
          }));
      },
      { Parent: Error },
    ),
    fa = ze(da),
    pa = Be(da),
    ma = Ve(da),
    ha = Ue(da),
    ga = Ge(da),
    _a = Ke(da),
    va = qe(da),
    ya = Je(da),
    ba = Ye(da),
    xa = Xe(da),
    Sa = Ze(da),
    Ca = Qe(da),
    wa = new WeakMap();
  function Ta(e, t, n) {
    let r = Object.getPrototypeOf(e),
      i = wa.get(r);
    if ((i || ((i = new Set()), wa.set(r, i)), !i.has(t))) {
      i.add(t);
      for (let e in n) {
        let t = n[e];
        Object.defineProperty(r, e, {
          configurable: !0,
          enumerable: !1,
          get() {
            let n = t.bind(this);
            return (
              Object.defineProperty(this, e, {
                configurable: !0,
                writable: !0,
                enumerable: !0,
                value: n,
              }),
              n
            );
          },
          set(t) {
            Object.defineProperty(this, e, {
              configurable: !0,
              writable: !0,
              enumerable: !0,
              value: t,
            });
          },
        });
      }
    }
  }
  var M = S(
      `ZodType`,
      (e, t) => (
        Xt.init(e, t),
        Object.assign(e[`~standard`], {
          jsonSchema: { input: ki(e, `input`), output: ki(e, `output`) },
        }),
        (e.toJSONSchema = Oi(e, {})),
        (e.def = t),
        (e.type = t.type),
        Object.defineProperty(e, "_def", { value: t }),
        (e.parse = (t, n) => fa(e, t, n, { callee: e.parse })),
        (e.safeParse = (t, n) => ma(e, t, n)),
        (e.parseAsync = async (t, n) => pa(e, t, n, { callee: e.parseAsync })),
        (e.safeParseAsync = async (t, n) => ha(e, t, n)),
        (e.spa = e.safeParseAsync),
        (e.encode = (t, n) => ga(e, t, n)),
        (e.decode = (t, n) => _a(e, t, n)),
        (e.encodeAsync = async (t, n) => va(e, t, n)),
        (e.decodeAsync = async (t, n) => ya(e, t, n)),
        (e.safeEncode = (t, n) => ba(e, t, n)),
        (e.safeDecode = (t, n) => xa(e, t, n)),
        (e.safeEncodeAsync = async (t, n) => Sa(e, t, n)),
        (e.safeDecodeAsync = async (t, n) => Ca(e, t, n)),
        Ta(e, `ZodType`, {
          check(...e) {
            let t = this.def;
            return this.clone(
              D(t, {
                checks: [
                  ...(t.checks ?? []),
                  ...e.map((e) =>
                    typeof e == `function`
                      ? {
                          _zod: {
                            check: e,
                            def: { check: `custom` },
                            onattach: [],
                          },
                        }
                      : e,
                  ),
                ],
              }),
              { parent: !0 },
            );
          },
          with(...e) {
            return this.check(...e);
          },
          clone(e, t) {
            return _e(this, e, t);
          },
          brand() {
            return this;
          },
          register(e, t) {
            return (e.add(this, t), this);
          },
          refine(e, t) {
            return this.check(Ho(e, t));
          },
          superRefine(e, t) {
            return this.check(Uo(e, t));
          },
          overwrite(e) {
            return this.check(fi(e));
          },
          optional() {
            return V(this);
          },
          exactOptional() {
            return wo(this);
          },
          nullable() {
            return Eo(this);
          },
          nullish() {
            return V(Eo(this));
          },
          nonoptional(e) {
            return Mo(this, e);
          },
          array() {
            return F(this);
          },
          or(e) {
            return L([this, e]);
          },
          and(e) {
            return R(this, e);
          },
          transform(e) {
            return Io(this, xo(e));
          },
          default(e) {
            return Oo(this, e);
          },
          prefault(e) {
            return Ao(this, e);
          },
          catch(e) {
            return Po(this, e);
          },
          pipe(e) {
            return Io(this, e);
          },
          readonly() {
            return zo(this);
          },
          describe(e) {
            let t = this.clone();
            return (gr.add(t, { description: e }), t);
          },
          meta(...e) {
            if (e.length === 0) return gr.get(this);
            let t = this.clone();
            return (gr.add(t, e[0]), t);
          },
          isOptional() {
            return this.safeParse(void 0).success;
          },
          isNullable() {
            return this.safeParse(null).success;
          },
          apply(e) {
            return e(this);
          },
        }),
        Object.defineProperty(e, "description", {
          get() {
            return gr.get(e)?.description;
          },
          configurable: !0,
        }),
        e
      ),
    ),
    Ea = S(`_ZodString`, (e, t) => {
      (Zt.init(e, t),
        M.init(e, t),
        (e._zod.processJSONSchema = (t, n, r) => ji(e, t, n, r)));
      let n = e._zod.bag;
      ((e.format = n.format ?? null),
        (e.minLength = n.minimum ?? null),
        (e.maxLength = n.maximum ?? null),
        Ta(e, `_ZodString`, {
          regex(...e) {
            return this.check(oi(...e));
          },
          includes(...e) {
            return this.check(li(...e));
          },
          startsWith(...e) {
            return this.check(ui(...e));
          },
          endsWith(...e) {
            return this.check(di(...e));
          },
          min(...e) {
            return this.check(ii(...e));
          },
          max(...e) {
            return this.check(ri(...e));
          },
          length(...e) {
            return this.check(ai(...e));
          },
          nonempty(...e) {
            return this.check(ii(1, ...e));
          },
          lowercase(e) {
            return this.check(si(e));
          },
          uppercase(e) {
            return this.check(ci(e));
          },
          trim() {
            return this.check(mi());
          },
          normalize(...e) {
            return this.check(pi(...e));
          },
          toLowerCase() {
            return this.check(hi());
          },
          toUpperCase() {
            return this.check(gi());
          },
          slugify() {
            return this.check(_i());
          },
        }));
    }),
    Da = S(`ZodString`, (e, t) => {
      (Zt.init(e, t),
        Ea.init(e, t),
        (e.email = (t) => e.check(vr(ka, t))),
        (e.url = (t) => e.check(wr(Ma, t))),
        (e.jwt = (t) => e.check(zr(Ja, t))),
        (e.emoji = (t) => e.check(Tr(Pa, t))),
        (e.guid = (t) => e.check(yr(Aa, t))),
        (e.uuid = (t) => e.check(br(ja, t))),
        (e.uuidv4 = (t) => e.check(xr(ja, t))),
        (e.uuidv6 = (t) => e.check(Sr(ja, t))),
        (e.uuidv7 = (t) => e.check(Cr(ja, t))),
        (e.nanoid = (t) => e.check(Er(Fa, t))),
        (e.guid = (t) => e.check(yr(Aa, t))),
        (e.cuid = (t) => e.check(Dr(Ia, t))),
        (e.cuid2 = (t) => e.check(Or(La, t))),
        (e.ulid = (t) => e.check(kr(Ra, t))),
        (e.base64 = (t) => e.check(Ir(Ga, t))),
        (e.base64url = (t) => e.check(Lr(Ka, t))),
        (e.xid = (t) => e.check(Ar(za, t))),
        (e.ksuid = (t) => e.check(jr(Ba, t))),
        (e.ipv4 = (t) => e.check(Mr(Va, t))),
        (e.ipv6 = (t) => e.check(Nr(Ha, t))),
        (e.cidrv4 = (t) => e.check(Pr(Ua, t))),
        (e.cidrv6 = (t) => e.check(Fr(Wa, t))),
        (e.e164 = (t) => e.check(Rr(qa, t))),
        (e.datetime = (t) => e.check(ia(t))),
        (e.date = (t) => e.check(oa(t))),
        (e.time = (t) => e.check(ca(t))),
        (e.duration = (t) => e.check(ua(t))));
    });
  function N(e) {
    return _r(Da, e);
  }
  var Oa = S(`ZodStringFormat`, (e, t) => {
      (Qt.init(e, t), Ea.init(e, t));
    }),
    ka = S(`ZodEmail`, (e, t) => {
      (tn.init(e, t), Oa.init(e, t));
    }),
    Aa = S(`ZodGUID`, (e, t) => {
      ($t.init(e, t), Oa.init(e, t));
    }),
    ja = S(`ZodUUID`, (e, t) => {
      (en.init(e, t), Oa.init(e, t));
    }),
    Ma = S(`ZodURL`, (e, t) => {
      (nn.init(e, t), Oa.init(e, t));
    });
  function Na(e) {
    return wr(Ma, e);
  }
  var Pa = S(`ZodEmoji`, (e, t) => {
      (rn.init(e, t), Oa.init(e, t));
    }),
    Fa = S(`ZodNanoID`, (e, t) => {
      (an.init(e, t), Oa.init(e, t));
    }),
    Ia = S(`ZodCUID`, (e, t) => {
      (on.init(e, t), Oa.init(e, t));
    }),
    La = S(`ZodCUID2`, (e, t) => {
      (sn.init(e, t), Oa.init(e, t));
    }),
    Ra = S(`ZodULID`, (e, t) => {
      (cn.init(e, t), Oa.init(e, t));
    }),
    za = S(`ZodXID`, (e, t) => {
      (ln.init(e, t), Oa.init(e, t));
    }),
    Ba = S(`ZodKSUID`, (e, t) => {
      (un.init(e, t), Oa.init(e, t));
    }),
    Va = S(`ZodIPv4`, (e, t) => {
      (hn.init(e, t), Oa.init(e, t));
    }),
    Ha = S(`ZodIPv6`, (e, t) => {
      (gn.init(e, t), Oa.init(e, t));
    }),
    Ua = S(`ZodCIDRv4`, (e, t) => {
      (_n.init(e, t), Oa.init(e, t));
    }),
    Wa = S(`ZodCIDRv6`, (e, t) => {
      (vn.init(e, t), Oa.init(e, t));
    }),
    Ga = S(`ZodBase64`, (e, t) => {
      (bn.init(e, t), Oa.init(e, t));
    }),
    Ka = S(`ZodBase64URL`, (e, t) => {
      (Sn.init(e, t), Oa.init(e, t));
    }),
    qa = S(`ZodE164`, (e, t) => {
      (Cn.init(e, t), Oa.init(e, t));
    }),
    Ja = S(`ZodJWT`, (e, t) => {
      (Tn.init(e, t), Oa.init(e, t));
    }),
    Ya = S(`ZodNumber`, (e, t) => {
      (En.init(e, t),
        M.init(e, t),
        (e._zod.processJSONSchema = (t, n, r) => Mi(e, t, n, r)),
        Ta(e, `ZodNumber`, {
          gt(e, t) {
            return this.check(ei(e, t));
          },
          gte(e, t) {
            return this.check(ti(e, t));
          },
          min(e, t) {
            return this.check(ti(e, t));
          },
          lt(e, t) {
            return this.check(Qr(e, t));
          },
          lte(e, t) {
            return this.check($r(e, t));
          },
          max(e, t) {
            return this.check($r(e, t));
          },
          int(e) {
            return this.check(Za(e));
          },
          safe(e) {
            return this.check(Za(e));
          },
          positive(e) {
            return this.check(ei(0, e));
          },
          nonnegative(e) {
            return this.check(ti(0, e));
          },
          negative(e) {
            return this.check(Qr(0, e));
          },
          nonpositive(e) {
            return this.check($r(0, e));
          },
          multipleOf(e, t) {
            return this.check(ni(e, t));
          },
          step(e, t) {
            return this.check(ni(e, t));
          },
          finite() {
            return this;
          },
        }));
      let n = e._zod.bag;
      ((e.minValue =
        Math.max(n.minimum ?? -1 / 0, n.exclusiveMinimum ?? -1 / 0) ?? null),
        (e.maxValue =
          Math.min(n.maximum ?? 1 / 0, n.exclusiveMaximum ?? 1 / 0) ?? null),
        (e.isInt =
          (n.format ?? ``).includes(`int`) ||
          Number.isSafeInteger(n.multipleOf ?? 0.5)),
        (e.isFinite = !0),
        (e.format = n.format ?? null));
    });
  function P(e) {
    return Wr(Ya, e);
  }
  var Xa = S(`ZodNumberFormat`, (e, t) => {
    (Dn.init(e, t), Ya.init(e, t));
  });
  function Za(e) {
    return Kr(Xa, e);
  }
  var Qa = S(`ZodBoolean`, (e, t) => {
    (On.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Ni(e, t, n, r)));
  });
  function $a(e) {
    return qr(Qa, e);
  }
  var eo = S(`ZodNull`, (e, t) => {
    (kn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Pi(e, t, n, r)));
  });
  function to(e) {
    return Jr(eo, e);
  }
  var no = S(`ZodAny`, (e, t) => {
    (An.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (e, t, n) => void 0));
  });
  function ro() {
    return Yr(no);
  }
  var io = S(`ZodUnknown`, (e, t) => {
    (jn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (e, t, n) => void 0));
  });
  function ao() {
    return Xr(io);
  }
  var oo = S(`ZodNever`, (e, t) => {
    (Mn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Fi(e, t, n, r)));
  });
  function so(e) {
    return Zr(oo, e);
  }
  var co = S(`ZodArray`, (e, t) => {
    (Pn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Bi(e, t, n, r)),
      (e.element = t.element),
      Ta(e, `ZodArray`, {
        min(e, t) {
          return this.check(ii(e, t));
        },
        nonempty(e) {
          return this.check(ii(1, e));
        },
        max(e, t) {
          return this.check(ri(e, t));
        },
        length(e, t) {
          return this.check(ai(e, t));
        },
        unwrap() {
          return this.element;
        },
      }));
  });
  function F(e, t) {
    return vi(co, e, t);
  }
  var lo = S(`ZodObject`, (e, t) => {
    (zn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Vi(e, t, n, r)),
      T(e, `shape`, () => t.shape),
      Ta(e, `ZodObject`, {
        keyof() {
          return vo(Object.keys(this._zod.def.shape));
        },
        catchall(e) {
          return this.clone({ ...this._zod.def, catchall: e });
        },
        passthrough() {
          return this.clone({ ...this._zod.def, catchall: ao() });
        },
        loose() {
          return this.clone({ ...this._zod.def, catchall: ao() });
        },
        strict() {
          return this.clone({ ...this._zod.def, catchall: so() });
        },
        strip() {
          return this.clone({ ...this._zod.def, catchall: void 0 });
        },
        extend(e) {
          return Se(this, e);
        },
        safeExtend(e) {
          return Ce(this, e);
        },
        merge(e) {
          return we(this, e);
        },
        pick(e) {
          return be(this, e);
        },
        omit(e) {
          return xe(this, e);
        },
        partial(...e) {
          return Te(So, this, e[0]);
        },
        required(...e) {
          return Ee(jo, this, e[0]);
        },
      }));
  });
  function I(e, t) {
    return new lo({ type: `object`, shape: e ?? {}, ...A(t) });
  }
  function uo(e, t) {
    return new lo({ type: `object`, shape: e, catchall: ao(), ...A(t) });
  }
  var fo = S(`ZodUnion`, (e, t) => {
    (Vn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Hi(e, t, n, r)),
      (e.options = t.options));
  });
  function L(e, t) {
    return new fo({ type: `union`, options: e, ...A(t) });
  }
  var po = S(`ZodDiscriminatedUnion`, (e, t) => {
    (fo.init(e, t), Hn.init(e, t));
  });
  function mo(e, t, n) {
    return new po({ type: `union`, options: t, discriminator: e, ...A(n) });
  }
  var ho = S(`ZodIntersection`, (e, t) => {
    (Un.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => j(e, t, n, r)));
  });
  function R(e, t) {
    return new ho({ type: `intersection`, left: e, right: t });
  }
  var go = S(`ZodRecord`, (e, t) => {
    (Kn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Ui(e, t, n, r)),
      (e.keyType = t.keyType),
      (e.valueType = t.valueType));
  });
  function z(e, t, n) {
    return !t || !t._zod
      ? new go({ type: `record`, keyType: N(), valueType: e, ...A(t) })
      : new go({ type: `record`, keyType: e, valueType: t, ...A(n) });
  }
  var _o = S(`ZodEnum`, (e, t) => {
    (qn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Ii(e, t, n, r)),
      (e.enum = t.entries),
      (e.options = Object.values(t.entries)));
    let n = new Set(Object.keys(t.entries));
    ((e.extract = (e, r) => {
      let i = {};
      for (let r of e)
        if (n.has(r)) i[r] = t.entries[r];
        else throw Error(`Key ${r} not found in enum`);
      return new _o({ ...t, checks: [], ...A(r), entries: i });
    }),
      (e.exclude = (e, r) => {
        let i = { ...t.entries };
        for (let t of e)
          if (n.has(t)) delete i[t];
          else throw Error(`Key ${t} not found in enum`);
        return new _o({ ...t, checks: [], ...A(r), entries: i });
      }));
  });
  function vo(e, t) {
    return new _o({
      type: `enum`,
      entries: Array.isArray(e) ? Object.fromEntries(e.map((e) => [e, e])) : e,
      ...A(t),
    });
  }
  var yo = S(`ZodLiteral`, (e, t) => {
    (Jn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Li(e, t, n, r)),
      (e.values = new Set(t.values)),
      Object.defineProperty(e, "value", {
        get() {
          if (t.values.length > 1)
            throw Error(
              "This schema contains multiple valid literal values. Use `.values` instead.",
            );
          return t.values[0];
        },
      }));
  });
  function B(e, t) {
    return new yo({
      type: `literal`,
      values: Array.isArray(e) ? e : [e],
      ...A(t),
    });
  }
  var bo = S(`ZodTransform`, (e, t) => {
    (Yn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => zi(e, t, n, r)),
      (e._zod.parse = (n, r) => {
        if (r.direction === `backward`) throw new w(e.constructor.name);
        n.addIssue = (r) => {
          if (typeof r == `string`) n.issues.push(Ne(r, n.value, t));
          else {
            let t = r;
            (t.fatal && (t.continue = !1),
              (t.code ??= `custom`),
              (t.input ??= n.value),
              (t.inst ??= e),
              n.issues.push(Ne(t)));
          }
        };
        let i = t.transform(n.value, n);
        return i instanceof Promise
          ? i.then((e) => ((n.value = e), (n.fallback = !0), n))
          : ((n.value = i), (n.fallback = !0), n);
      }));
  });
  function xo(e) {
    return new bo({ type: `transform`, transform: e });
  }
  var So = S(`ZodOptional`, (e, t) => {
    (Zn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Zi(e, t, n, r)),
      (e.unwrap = () => e._zod.def.innerType));
  });
  function V(e) {
    return new So({ type: `optional`, innerType: e });
  }
  var Co = S(`ZodExactOptional`, (e, t) => {
    (Qn.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Zi(e, t, n, r)),
      (e.unwrap = () => e._zod.def.innerType));
  });
  function wo(e) {
    return new Co({ type: `optional`, innerType: e });
  }
  var To = S(`ZodNullable`, (e, t) => {
    ($n.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Wi(e, t, n, r)),
      (e.unwrap = () => e._zod.def.innerType));
  });
  function Eo(e) {
    return new To({ type: `nullable`, innerType: e });
  }
  var Do = S(`ZodDefault`, (e, t) => {
    (er.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Ki(e, t, n, r)),
      (e.unwrap = () => e._zod.def.innerType),
      (e.removeDefault = e.unwrap));
  });
  function Oo(e, t) {
    return new Do({
      type: `default`,
      innerType: e,
      get defaultValue() {
        return typeof t == `function` ? t() : me(t);
      },
    });
  }
  var ko = S(`ZodPrefault`, (e, t) => {
    (nr.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => qi(e, t, n, r)),
      (e.unwrap = () => e._zod.def.innerType));
  });
  function Ao(e, t) {
    return new ko({
      type: `prefault`,
      innerType: e,
      get defaultValue() {
        return typeof t == `function` ? t() : me(t);
      },
    });
  }
  var jo = S(`ZodNonOptional`, (e, t) => {
    (rr.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Gi(e, t, n, r)),
      (e.unwrap = () => e._zod.def.innerType));
  });
  function Mo(e, t) {
    return new jo({ type: `nonoptional`, innerType: e, ...A(t) });
  }
  var No = S(`ZodCatch`, (e, t) => {
    (ar.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Ji(e, t, n, r)),
      (e.unwrap = () => e._zod.def.innerType),
      (e.removeCatch = e.unwrap));
  });
  function Po(e, t) {
    return new No({
      type: `catch`,
      innerType: e,
      catchValue: typeof t == `function` ? t : () => t,
    });
  }
  var Fo = S(`ZodPipe`, (e, t) => {
    (or.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Yi(e, t, n, r)),
      (e.in = t.in),
      (e.out = t.out));
  });
  function Io(e, t) {
    return new Fo({ type: `pipe`, in: e, out: t });
  }
  var Lo = S(`ZodPreprocess`, (e, t) => {
      (Fo.init(e, t), cr.init(e, t));
    }),
    Ro = S(`ZodReadonly`, (e, t) => {
      (lr.init(e, t),
        M.init(e, t),
        (e._zod.processJSONSchema = (t, n, r) => Xi(e, t, n, r)),
        (e.unwrap = () => e._zod.def.innerType));
    });
  function zo(e) {
    return new Ro({ type: `readonly`, innerType: e });
  }
  var Bo = S(`ZodCustom`, (e, t) => {
    (dr.init(e, t),
      M.init(e, t),
      (e._zod.processJSONSchema = (t, n, r) => Ri(e, t, n, r)));
  });
  function Vo(e, t) {
    return yi(Bo, e ?? (() => !0), t);
  }
  function Ho(e, t = {}) {
    return bi(Bo, e, t);
  }
  function Uo(e, t) {
    return xi(e, t);
  }
  function Wo(e, t) {
    return new Lo({ type: `pipe`, in: xo(e), out: t });
  }
  var Go = {
      invalid_type: `invalid_type`,
      too_big: `too_big`,
      too_small: `too_small`,
      invalid_format: `invalid_format`,
      not_multiple_of: `not_multiple_of`,
      unrecognized_keys: `unrecognized_keys`,
      invalid_union: `invalid_union`,
      invalid_key: `invalid_key`,
      invalid_element: `invalid_element`,
      invalid_value: `invalid_value`,
      custom: `custom`,
    },
    Ko;
  Ko ||= {};
  function qo(e) {
    return Gr(Ya, e);
  }
  var Jo = `2025-11-25`,
    Yo = [Jo, `2025-06-18`, `2025-03-26`, `2024-11-05`, `2024-10-07`],
    Xo = `io.modelcontextprotocol/related-task`,
    Zo = Vo(
      (e) => e !== null && (typeof e == `object` || typeof e == `function`),
    ),
    Qo = L([N(), P().int()]),
    $o = N();
  uo({ ttl: P().optional(), pollInterval: P().optional() });
  var es = I({ ttl: P().optional() }),
    ts = I({ taskId: N() }),
    ns = uo({ progressToken: Qo.optional(), [Xo]: ts.optional() }),
    rs = I({ _meta: ns.optional() }),
    is = rs.extend({ task: es.optional() }),
    as = (e) => is.safeParse(e).success,
    os = I({ method: N(), params: rs.loose().optional() }),
    ss = I({ _meta: ns.optional() }),
    cs = I({ method: N(), params: ss.loose().optional() }),
    ls = uo({ _meta: ns.optional() }),
    us = L([N(), P().int()]),
    ds = I({ jsonrpc: B(`2.0`), id: us, ...os.shape }).strict(),
    fs = (e) => ds.safeParse(e).success,
    ps = I({ jsonrpc: B(`2.0`), ...cs.shape }).strict(),
    ms = (e) => ps.safeParse(e).success,
    hs = I({ jsonrpc: B(`2.0`), id: us, result: ls }).strict(),
    gs = (e) => hs.safeParse(e).success,
    H;
  (function (e) {
    ((e[(e.ConnectionClosed = -32e3)] = `ConnectionClosed`),
      (e[(e.RequestTimeout = -32001)] = `RequestTimeout`),
      (e[(e.ParseError = -32700)] = `ParseError`),
      (e[(e.InvalidRequest = -32600)] = `InvalidRequest`),
      (e[(e.MethodNotFound = -32601)] = `MethodNotFound`),
      (e[(e.InvalidParams = -32602)] = `InvalidParams`),
      (e[(e.InternalError = -32603)] = `InternalError`),
      (e[(e.UrlElicitationRequired = -32042)] = `UrlElicitationRequired`));
  })((H ||= {}));
  var _s = I({
      jsonrpc: B(`2.0`),
      id: us.optional(),
      error: I({ code: P().int(), message: N(), data: ao().optional() }),
    }).strict(),
    vs = (e) => _s.safeParse(e).success,
    ys = L([ds, ps, hs, _s]);
  L([hs, _s]);
  var bs = ls.strict(),
    xs = ss.extend({ requestId: us.optional(), reason: N().optional() }),
    Ss = cs.extend({ method: B(`notifications/cancelled`), params: xs }),
    Cs = I({
      icons: F(
        I({
          src: N(),
          mimeType: N().optional(),
          sizes: F(N()).optional(),
          theme: vo([`light`, `dark`]).optional(),
        }),
      ).optional(),
    }),
    ws = I({ name: N(), title: N().optional() }),
    Ts = ws.extend({
      ...ws.shape,
      ...Cs.shape,
      version: N(),
      websiteUrl: N().optional(),
      description: N().optional(),
    }),
    Es = Wo(
      (e) =>
        e &&
        typeof e == `object` &&
        !Array.isArray(e) &&
        Object.keys(e).length === 0
          ? { form: {} }
          : e,
      R(
        I({
          form: R(
            I({ applyDefaults: $a().optional() }),
            z(N(), ao()),
          ).optional(),
          url: Zo.optional(),
        }),
        z(N(), ao()).optional(),
      ),
    ),
    Ds = uo({
      list: Zo.optional(),
      cancel: Zo.optional(),
      requests: uo({
        sampling: uo({ createMessage: Zo.optional() }).optional(),
        elicitation: uo({ create: Zo.optional() }).optional(),
      }).optional(),
    }),
    Os = uo({
      list: Zo.optional(),
      cancel: Zo.optional(),
      requests: uo({
        tools: uo({ call: Zo.optional() }).optional(),
      }).optional(),
    }),
    ks = I({
      experimental: z(N(), Zo).optional(),
      sampling: I({ context: Zo.optional(), tools: Zo.optional() }).optional(),
      elicitation: Es.optional(),
      roots: I({ listChanged: $a().optional() }).optional(),
      tasks: Ds.optional(),
      extensions: z(N(), Zo).optional(),
    }),
    As = rs.extend({ protocolVersion: N(), capabilities: ks, clientInfo: Ts }),
    js = os.extend({ method: B(`initialize`), params: As }),
    Ms = I({
      experimental: z(N(), Zo).optional(),
      logging: Zo.optional(),
      completions: Zo.optional(),
      prompts: I({ listChanged: $a().optional() }).optional(),
      resources: I({
        subscribe: $a().optional(),
        listChanged: $a().optional(),
      }).optional(),
      tools: I({ listChanged: $a().optional() }).optional(),
      tasks: Os.optional(),
      extensions: z(N(), Zo).optional(),
    }),
    Ns = ls.extend({
      protocolVersion: N(),
      capabilities: Ms,
      serverInfo: Ts,
      instructions: N().optional(),
    }),
    Ps = cs.extend({
      method: B(`notifications/initialized`),
      params: ss.optional(),
    }),
    Fs = (e) => Ps.safeParse(e).success,
    Is = os.extend({ method: B(`ping`), params: rs.optional() }),
    Ls = I({ progress: P(), total: V(P()), message: V(N()) }),
    Rs = I({ ...ss.shape, ...Ls.shape, progressToken: Qo }),
    zs = cs.extend({ method: B(`notifications/progress`), params: Rs }),
    Bs = rs.extend({ cursor: $o.optional() }),
    Vs = os.extend({ params: Bs.optional() }),
    Hs = ls.extend({ nextCursor: $o.optional() }),
    Us = vo([`working`, `input_required`, `completed`, `failed`, `cancelled`]),
    Ws = I({
      taskId: N(),
      status: Us,
      ttl: L([P(), to()]),
      createdAt: N(),
      lastUpdatedAt: N(),
      pollInterval: V(P()),
      statusMessage: V(N()),
    }),
    Gs = ls.extend({ task: Ws }),
    Ks = ss.merge(Ws),
    qs = cs.extend({ method: B(`notifications/tasks/status`), params: Ks }),
    Js = os.extend({
      method: B(`tasks/get`),
      params: rs.extend({ taskId: N() }),
    }),
    Ys = ls.merge(Ws),
    Xs = os.extend({
      method: B(`tasks/result`),
      params: rs.extend({ taskId: N() }),
    });
  ls.loose();
  var Zs = Vs.extend({ method: B(`tasks/list`) }),
    Qs = Hs.extend({ tasks: F(Ws) }),
    $s = os.extend({
      method: B(`tasks/cancel`),
      params: rs.extend({ taskId: N() }),
    }),
    ec = ls.merge(Ws),
    tc = I({ uri: N(), mimeType: V(N()), _meta: z(N(), ao()).optional() }),
    nc = tc.extend({ text: N() }),
    rc = N().refine(
      (e) => {
        try {
          return (atob(e), !0);
        } catch {
          return !1;
        }
      },
      { message: `Invalid Base64 string` },
    ),
    ic = tc.extend({ blob: rc }),
    ac = vo([`user`, `assistant`]),
    oc = I({
      audience: F(ac).optional(),
      priority: P().min(0).max(1).optional(),
      lastModified: ia({ offset: !0 }).optional(),
    }),
    sc = I({
      ...ws.shape,
      ...Cs.shape,
      uri: N(),
      description: V(N()),
      mimeType: V(N()),
      size: V(P()),
      annotations: oc.optional(),
      _meta: V(uo({})),
    }),
    cc = I({
      ...ws.shape,
      ...Cs.shape,
      uriTemplate: N(),
      description: V(N()),
      mimeType: V(N()),
      annotations: oc.optional(),
      _meta: V(uo({})),
    }),
    lc = Vs.extend({ method: B(`resources/list`) }),
    uc = Hs.extend({ resources: F(sc) }),
    dc = Vs.extend({ method: B(`resources/templates/list`) }),
    fc = Hs.extend({ resourceTemplates: F(cc) }),
    pc = rs.extend({ uri: N() }),
    mc = pc,
    hc = os.extend({ method: B(`resources/read`), params: mc }),
    gc = ls.extend({ contents: F(L([nc, ic])) }),
    _c = cs.extend({
      method: B(`notifications/resources/list_changed`),
      params: ss.optional(),
    }),
    vc = pc,
    yc = os.extend({ method: B(`resources/subscribe`), params: vc }),
    bc = pc,
    xc = os.extend({ method: B(`resources/unsubscribe`), params: bc }),
    Sc = ss.extend({ uri: N() }),
    Cc = cs.extend({
      method: B(`notifications/resources/updated`),
      params: Sc,
    }),
    wc = I({ name: N(), description: V(N()), required: V($a()) }),
    Tc = I({
      ...ws.shape,
      ...Cs.shape,
      description: V(N()),
      arguments: V(F(wc)),
      _meta: V(uo({})),
    }),
    Ec = Vs.extend({ method: B(`prompts/list`) }),
    Dc = Hs.extend({ prompts: F(Tc) }),
    Oc = rs.extend({ name: N(), arguments: z(N(), N()).optional() }),
    kc = os.extend({ method: B(`prompts/get`), params: Oc }),
    Ac = I({
      type: B(`text`),
      text: N(),
      annotations: oc.optional(),
      _meta: z(N(), ao()).optional(),
    }),
    jc = I({
      type: B(`image`),
      data: rc,
      mimeType: N(),
      annotations: oc.optional(),
      _meta: z(N(), ao()).optional(),
    }),
    Mc = I({
      type: B(`audio`),
      data: rc,
      mimeType: N(),
      annotations: oc.optional(),
      _meta: z(N(), ao()).optional(),
    }),
    Nc = I({
      type: B(`tool_use`),
      name: N(),
      id: N(),
      input: z(N(), ao()),
      _meta: z(N(), ao()).optional(),
    }),
    Pc = I({
      type: B(`resource`),
      resource: L([nc, ic]),
      annotations: oc.optional(),
      _meta: z(N(), ao()).optional(),
    }),
    Fc = L([Ac, jc, Mc, sc.extend({ type: B(`resource_link`) }), Pc]),
    Ic = I({ role: ac, content: Fc }),
    Lc = ls.extend({ description: N().optional(), messages: F(Ic) }),
    Rc = cs.extend({
      method: B(`notifications/prompts/list_changed`),
      params: ss.optional(),
    }),
    zc = I({
      title: N().optional(),
      readOnlyHint: $a().optional(),
      destructiveHint: $a().optional(),
      idempotentHint: $a().optional(),
      openWorldHint: $a().optional(),
    }),
    Bc = I({
      taskSupport: vo([`required`, `optional`, `forbidden`]).optional(),
    }),
    Vc = I({
      ...ws.shape,
      ...Cs.shape,
      description: N().optional(),
      inputSchema: I({
        type: B(`object`),
        properties: z(N(), Zo).optional(),
        required: F(N()).optional(),
      }).catchall(ao()),
      outputSchema: I({
        type: B(`object`),
        properties: z(N(), Zo).optional(),
        required: F(N()).optional(),
      })
        .catchall(ao())
        .optional(),
      annotations: zc.optional(),
      execution: Bc.optional(),
      _meta: z(N(), ao()).optional(),
    }),
    Hc = Vs.extend({ method: B(`tools/list`) }),
    Uc = Hs.extend({ tools: F(Vc) }),
    Wc = ls.extend({
      content: F(Fc).default([]),
      structuredContent: z(N(), ao()).optional(),
      isError: $a().optional(),
    });
  Wc.or(ls.extend({ toolResult: ao() }));
  var Gc = is.extend({ name: N(), arguments: z(N(), ao()).optional() }),
    Kc = os.extend({ method: B(`tools/call`), params: Gc }),
    qc = cs.extend({
      method: B(`notifications/tools/list_changed`),
      params: ss.optional(),
    }),
    Jc = I({
      autoRefresh: $a().default(!0),
      debounceMs: P().int().nonnegative().default(300),
    }),
    Yc = vo([
      `debug`,
      `info`,
      `notice`,
      `warning`,
      `error`,
      `critical`,
      `alert`,
      `emergency`,
    ]),
    Xc = rs.extend({ level: Yc }),
    Zc = os.extend({ method: B(`logging/setLevel`), params: Xc }),
    Qc = ss.extend({ level: Yc, logger: N().optional(), data: ao() }),
    $c = cs.extend({ method: B(`notifications/message`), params: Qc }),
    el = I({
      hints: F(I({ name: N().optional() })).optional(),
      costPriority: P().min(0).max(1).optional(),
      speedPriority: P().min(0).max(1).optional(),
      intelligencePriority: P().min(0).max(1).optional(),
    }),
    tl = I({ mode: vo([`auto`, `required`, `none`]).optional() }),
    nl = I({
      type: B(`tool_result`),
      toolUseId: N().describe(
        `The unique identifier for the corresponding tool call.`,
      ),
      content: F(Fc).default([]),
      structuredContent: I({}).loose().optional(),
      isError: $a().optional(),
      _meta: z(N(), ao()).optional(),
    }),
    rl = mo(`type`, [Ac, jc, Mc]),
    il = mo(`type`, [Ac, jc, Mc, Nc, nl]),
    al = I({
      role: ac,
      content: L([il, F(il)]),
      _meta: z(N(), ao()).optional(),
    }),
    ol = is.extend({
      messages: F(al),
      modelPreferences: el.optional(),
      systemPrompt: N().optional(),
      includeContext: vo([`none`, `thisServer`, `allServers`]).optional(),
      temperature: P().optional(),
      maxTokens: P().int(),
      stopSequences: F(N()).optional(),
      metadata: Zo.optional(),
      tools: F(Vc).optional(),
      toolChoice: tl.optional(),
    }),
    sl = os.extend({ method: B(`sampling/createMessage`), params: ol }),
    cl = ls.extend({
      model: N(),
      stopReason: V(vo([`endTurn`, `stopSequence`, `maxTokens`]).or(N())),
      role: ac,
      content: rl,
    }),
    ll = ls.extend({
      model: N(),
      stopReason: V(
        vo([`endTurn`, `stopSequence`, `maxTokens`, `toolUse`]).or(N()),
      ),
      role: ac,
      content: L([il, F(il)]),
    }),
    ul = I({
      type: B(`boolean`),
      title: N().optional(),
      description: N().optional(),
      default: $a().optional(),
    }),
    dl = I({
      type: B(`string`),
      title: N().optional(),
      description: N().optional(),
      minLength: P().optional(),
      maxLength: P().optional(),
      format: vo([`email`, `uri`, `date`, `date-time`]).optional(),
      default: N().optional(),
    }),
    fl = I({
      type: vo([`number`, `integer`]),
      title: N().optional(),
      description: N().optional(),
      minimum: P().optional(),
      maximum: P().optional(),
      default: P().optional(),
    }),
    pl = I({
      type: B(`string`),
      title: N().optional(),
      description: N().optional(),
      enum: F(N()),
      default: N().optional(),
    }),
    ml = I({
      type: B(`string`),
      title: N().optional(),
      description: N().optional(),
      oneOf: F(I({ const: N(), title: N() })),
      default: N().optional(),
    }),
    hl = L([
      L([
        I({
          type: B(`string`),
          title: N().optional(),
          description: N().optional(),
          enum: F(N()),
          enumNames: F(N()).optional(),
          default: N().optional(),
        }),
        L([pl, ml]),
        L([
          I({
            type: B(`array`),
            title: N().optional(),
            description: N().optional(),
            minItems: P().optional(),
            maxItems: P().optional(),
            items: I({ type: B(`string`), enum: F(N()) }),
            default: F(N()).optional(),
          }),
          I({
            type: B(`array`),
            title: N().optional(),
            description: N().optional(),
            minItems: P().optional(),
            maxItems: P().optional(),
            items: I({ anyOf: F(I({ const: N(), title: N() })) }),
            default: F(N()).optional(),
          }),
        ]),
      ]),
      ul,
      dl,
      fl,
    ]),
    gl = L([
      is.extend({
        mode: B(`form`).optional(),
        message: N(),
        requestedSchema: I({
          type: B(`object`),
          properties: z(N(), hl),
          required: F(N()).optional(),
        }),
      }),
      is.extend({
        mode: B(`url`),
        message: N(),
        elicitationId: N(),
        url: N().url(),
      }),
    ]),
    _l = os.extend({ method: B(`elicitation/create`), params: gl }),
    vl = ss.extend({ elicitationId: N() }),
    yl = cs.extend({
      method: B(`notifications/elicitation/complete`),
      params: vl,
    }),
    bl = ls.extend({
      action: vo([`accept`, `decline`, `cancel`]),
      content: Wo(
        (e) => (e === null ? void 0 : e),
        z(N(), L([N(), P(), $a(), F(N())])).optional(),
      ),
    }),
    xl = I({ type: B(`ref/resource`), uri: N() }),
    Sl = I({ type: B(`ref/prompt`), name: N() }),
    Cl = rs.extend({
      ref: L([Sl, xl]),
      argument: I({ name: N(), value: N() }),
      context: I({ arguments: z(N(), N()).optional() }).optional(),
    }),
    wl = os.extend({ method: B(`completion/complete`), params: Cl }),
    Tl = ls.extend({
      completion: uo({
        values: F(N()).max(100),
        total: V(P().int()),
        hasMore: V($a()),
      }),
    }),
    El = I({
      uri: N().startsWith(`file://`),
      name: N().optional(),
      _meta: z(N(), ao()).optional(),
    }),
    Dl = os.extend({ method: B(`roots/list`), params: rs.optional() }),
    Ol = ls.extend({ roots: F(El) }),
    kl = cs.extend({
      method: B(`notifications/roots/list_changed`),
      params: ss.optional(),
    });
  (L([Is, js, wl, Zc, kc, Ec, lc, dc, hc, yc, xc, Kc, Hc, Js, Xs, Zs, $s]),
    L([Ss, zs, Ps, kl, qs]),
    L([bs, cl, ll, bl, Ol, Ys, Qs, Gs]),
    L([Is, sl, _l, Dl, Js, Xs, Zs, $s]),
    L([Ss, zs, $c, Cc, _c, qc, Rc, qs, yl]),
    L([bs, Ns, Tl, Lc, Dc, uc, fc, gc, Wc, Uc, Ys, Qs, Gs]));
  var U = class e extends Error {
      constructor(e, t, n) {
        (super(`MCP error ${e}: ${t}`),
          (this.code = e),
          (this.data = n),
          (this.name = `McpError`));
      }
      static fromError(t, n, r) {
        if (t === H.UrlElicitationRequired && r) {
          let e = r;
          if (e.elicitations) return new Al(e.elicitations, n);
        }
        return new e(t, n, r);
      }
    },
    Al = class extends U {
      constructor(e, t = `URL elicitation${e.length > 1 ? `s` : ``} required`) {
        super(H.UrlElicitationRequired, t, { elicitations: e });
      }
      get elicitations() {
        return this.data?.elicitations ?? [];
      }
    };
  function jl(e) {
    return e === `completed` || e === `failed` || e === `cancelled`;
  }
  function Ml(e) {
    let t = ta(e)?.method;
    if (!t) throw Error(`Schema is missing a method literal`);
    let n = na(t);
    if (typeof n != `string`)
      throw Error(`Schema method literal must be a string`);
    return n;
  }
  function Nl(e, t) {
    let n = ea(e, t);
    if (!n.success) throw n.error;
    return n.data;
  }
  var Pl = class {
    constructor(e) {
      ((this._options = e),
        (this._requestMessageId = 0),
        (this._requestHandlers = new Map()),
        (this._requestHandlerAbortControllers = new Map()),
        (this._notificationHandlers = new Map()),
        (this._responseHandlers = new Map()),
        (this._progressHandlers = new Map()),
        (this._timeoutInfo = new Map()),
        (this._pendingDebouncedNotifications = new Set()),
        (this._taskProgressTokens = new Map()),
        (this._requestResolvers = new Map()),
        this.setNotificationHandler(Ss, (e) => {
          this._oncancel(e);
        }),
        this.setNotificationHandler(zs, (e) => {
          this._onprogress(e);
        }),
        this.setRequestHandler(Is, (e) => ({})),
        (this._taskStore = e?.taskStore),
        (this._taskMessageQueue = e?.taskMessageQueue),
        this._taskStore &&
          (this.setRequestHandler(Js, async (e, t) => {
            let n = await this._taskStore.getTask(e.params.taskId, t.sessionId);
            if (!n)
              throw new U(
                H.InvalidParams,
                `Failed to retrieve task: Task not found`,
              );
            return { ...n };
          }),
          this.setRequestHandler(Xs, async (e, t) => {
            let n = async () => {
              let r = e.params.taskId;
              if (this._taskMessageQueue) {
                let e;
                for (
                  ;
                  (e = await this._taskMessageQueue.dequeue(r, t.sessionId));
                ) {
                  if (e.type === `response` || e.type === `error`) {
                    let t = e.message,
                      n = t.id,
                      r = this._requestResolvers.get(n);
                    if (r)
                      if (
                        (this._requestResolvers.delete(n),
                        e.type === `response`)
                      )
                        r(t);
                      else {
                        let e = t;
                        r(new U(e.error.code, e.error.message, e.error.data));
                      }
                    else {
                      let t = e.type === `response` ? `Response` : `Error`;
                      this._onerror(
                        Error(`${t} handler missing for request ${n}`),
                      );
                    }
                    continue;
                  }
                  await this._transport?.send(e.message, {
                    relatedRequestId: t.requestId,
                  });
                }
              }
              let i = await this._taskStore.getTask(r, t.sessionId);
              if (!i) throw new U(H.InvalidParams, `Task not found: ${r}`);
              if (!jl(i.status))
                return (await this._waitForTaskUpdate(r, t.signal), await n());
              if (jl(i.status)) {
                let e = await this._taskStore.getTaskResult(r, t.sessionId);
                return (
                  this._clearTaskQueue(r),
                  { ...e, _meta: { ...e._meta, [Xo]: { taskId: r } } }
                );
              }
              return await n();
            };
            return await n();
          }),
          this.setRequestHandler(Zs, async (e, t) => {
            try {
              let { tasks: n, nextCursor: r } = await this._taskStore.listTasks(
                e.params?.cursor,
                t.sessionId,
              );
              return { tasks: n, nextCursor: r, _meta: {} };
            } catch (e) {
              throw new U(
                H.InvalidParams,
                `Failed to list tasks: ${e instanceof Error ? e.message : String(e)}`,
              );
            }
          }),
          this.setRequestHandler($s, async (e, t) => {
            try {
              let n = await this._taskStore.getTask(
                e.params.taskId,
                t.sessionId,
              );
              if (!n)
                throw new U(
                  H.InvalidParams,
                  `Task not found: ${e.params.taskId}`,
                );
              if (jl(n.status))
                throw new U(
                  H.InvalidParams,
                  `Cannot cancel task in terminal status: ${n.status}`,
                );
              (await this._taskStore.updateTaskStatus(
                e.params.taskId,
                `cancelled`,
                `Client cancelled task execution.`,
                t.sessionId,
              ),
                this._clearTaskQueue(e.params.taskId));
              let r = await this._taskStore.getTask(
                e.params.taskId,
                t.sessionId,
              );
              if (!r)
                throw new U(
                  H.InvalidParams,
                  `Task not found after cancellation: ${e.params.taskId}`,
                );
              return { _meta: {}, ...r };
            } catch (e) {
              throw e instanceof U
                ? e
                : new U(
                    H.InvalidRequest,
                    `Failed to cancel task: ${e instanceof Error ? e.message : String(e)}`,
                  );
            }
          })));
    }
    async _oncancel(e) {
      e.params.requestId &&
        this._requestHandlerAbortControllers
          .get(e.params.requestId)
          ?.abort(e.params.reason);
    }
    _setupTimeout(e, t, n, r, i = !1) {
      this._timeoutInfo.set(e, {
        timeoutId: setTimeout(r, t),
        startTime: Date.now(),
        timeout: t,
        maxTotalTimeout: n,
        resetTimeoutOnProgress: i,
        onTimeout: r,
      });
    }
    _resetTimeout(e) {
      let t = this._timeoutInfo.get(e);
      if (!t) return !1;
      let n = Date.now() - t.startTime;
      if (t.maxTotalTimeout && n >= t.maxTotalTimeout)
        throw (
          this._timeoutInfo.delete(e),
          U.fromError(H.RequestTimeout, `Maximum total timeout exceeded`, {
            maxTotalTimeout: t.maxTotalTimeout,
            totalElapsed: n,
          })
        );
      return (
        clearTimeout(t.timeoutId),
        (t.timeoutId = setTimeout(t.onTimeout, t.timeout)),
        !0
      );
    }
    _cleanupTimeout(e) {
      let t = this._timeoutInfo.get(e);
      t && (clearTimeout(t.timeoutId), this._timeoutInfo.delete(e));
    }
    async connect(e) {
      if (this._transport)
        throw Error(
          `Already connected to a transport. Call close() before connecting to a new transport, or use a separate Protocol instance per connection.`,
        );
      this._transport = e;
      let t = this.transport?.onclose;
      this._transport.onclose = () => {
        (t?.(), this._onclose());
      };
      let n = this.transport?.onerror;
      this._transport.onerror = (e) => {
        (n?.(e), this._onerror(e));
      };
      let r = this._transport?.onmessage;
      ((this._transport.onmessage = (e, t) => {
        (r?.(e, t),
          gs(e) || vs(e)
            ? this._onresponse(e)
            : fs(e)
              ? this._onrequest(e, t)
              : ms(e)
                ? this._onnotification(e)
                : this._onerror(
                    Error(`Unknown message type: ${JSON.stringify(e)}`),
                  ));
      }),
        await this._transport.start());
    }
    _onclose() {
      let e = this._responseHandlers;
      ((this._responseHandlers = new Map()),
        this._progressHandlers.clear(),
        this._taskProgressTokens.clear(),
        this._pendingDebouncedNotifications.clear());
      for (let e of this._timeoutInfo.values()) clearTimeout(e.timeoutId);
      this._timeoutInfo.clear();
      for (let e of this._requestHandlerAbortControllers.values()) e.abort();
      this._requestHandlerAbortControllers.clear();
      let t = U.fromError(H.ConnectionClosed, `Connection closed`);
      ((this._transport = void 0), this.onclose?.());
      for (let n of e.values()) n(t);
    }
    _onerror(e) {
      this.onerror?.(e);
    }
    _onnotification(e) {
      let t =
        this._notificationHandlers.get(e.method) ??
        this.fallbackNotificationHandler;
      t !== void 0 &&
        Promise.resolve()
          .then(() => t(e))
          .catch((e) =>
            this._onerror(
              Error(`Uncaught error in notification handler: ${e}`),
            ),
          );
    }
    _onrequest(e, t) {
      let n =
          this._requestHandlers.get(e.method) ?? this.fallbackRequestHandler,
        r = this._transport,
        i = e.params?._meta?.[Xo]?.taskId;
      if (n === void 0) {
        let t = {
          jsonrpc: `2.0`,
          id: e.id,
          error: { code: H.MethodNotFound, message: `Method not found` },
        };
        i && this._taskMessageQueue
          ? this._enqueueTaskMessage(
              i,
              { type: `error`, message: t, timestamp: Date.now() },
              r?.sessionId,
            ).catch((e) =>
              this._onerror(Error(`Failed to enqueue error response: ${e}`)),
            )
          : r
              ?.send(t)
              .catch((e) =>
                this._onerror(Error(`Failed to send an error response: ${e}`)),
              );
        return;
      }
      let a = new AbortController();
      this._requestHandlerAbortControllers.set(e.id, a);
      let o = as(e.params) ? e.params.task : void 0,
        s = this._taskStore ? this.requestTaskStore(e, r?.sessionId) : void 0,
        c = {
          signal: a.signal,
          sessionId: r?.sessionId,
          _meta: e.params?._meta,
          sendNotification: async (t) => {
            if (a.signal.aborted) return;
            let n = { relatedRequestId: e.id };
            (i && (n.relatedTask = { taskId: i }),
              await this.notification(t, n));
          },
          sendRequest: async (t, n, r) => {
            if (a.signal.aborted)
              throw new U(H.ConnectionClosed, `Request was cancelled`);
            let o = { ...r, relatedRequestId: e.id };
            i && !o.relatedTask && (o.relatedTask = { taskId: i });
            let c = o.relatedTask?.taskId ?? i;
            return (
              c && s && (await s.updateTaskStatus(c, `input_required`)),
              await this.request(t, n, o)
            );
          },
          authInfo: t?.authInfo,
          requestId: e.id,
          requestInfo: t?.requestInfo,
          taskId: i,
          taskStore: s,
          taskRequestedTtl: o?.ttl,
          closeSSEStream: t?.closeSSEStream,
          closeStandaloneSSEStream: t?.closeStandaloneSSEStream,
        };
      Promise.resolve()
        .then(() => {
          o && this.assertTaskHandlerCapability(e.method);
        })
        .then(() => n(e, c))
        .then(
          async (t) => {
            if (a.signal.aborted) return;
            let n = { result: t, jsonrpc: `2.0`, id: e.id };
            i && this._taskMessageQueue
              ? await this._enqueueTaskMessage(
                  i,
                  { type: `response`, message: n, timestamp: Date.now() },
                  r?.sessionId,
                )
              : await r?.send(n);
          },
          async (t) => {
            if (a.signal.aborted) return;
            let n = {
              jsonrpc: `2.0`,
              id: e.id,
              error: {
                code: Number.isSafeInteger(t.code) ? t.code : H.InternalError,
                message: t.message ?? `Internal error`,
                ...(t.data !== void 0 && { data: t.data }),
              },
            };
            i && this._taskMessageQueue
              ? await this._enqueueTaskMessage(
                  i,
                  { type: `error`, message: n, timestamp: Date.now() },
                  r?.sessionId,
                )
              : await r?.send(n);
          },
        )
        .catch((e) => this._onerror(Error(`Failed to send response: ${e}`)))
        .finally(() => {
          this._requestHandlerAbortControllers.get(e.id) === a &&
            this._requestHandlerAbortControllers.delete(e.id);
        });
    }
    _onprogress(e) {
      let { progressToken: t, ...n } = e.params,
        r = Number(t),
        i = this._progressHandlers.get(r);
      if (!i) {
        this._onerror(
          Error(
            `Received a progress notification for an unknown token: ${JSON.stringify(e)}`,
          ),
        );
        return;
      }
      let a = this._responseHandlers.get(r),
        o = this._timeoutInfo.get(r);
      if (o && a && o.resetTimeoutOnProgress)
        try {
          this._resetTimeout(r);
        } catch (e) {
          (this._responseHandlers.delete(r),
            this._progressHandlers.delete(r),
            this._cleanupTimeout(r),
            a(e));
          return;
        }
      i(n);
    }
    _onresponse(e) {
      let t = Number(e.id),
        n = this._requestResolvers.get(t);
      if (n) {
        (this._requestResolvers.delete(t),
          gs(e) ? n(e) : n(new U(e.error.code, e.error.message, e.error.data)));
        return;
      }
      let r = this._responseHandlers.get(t);
      if (r === void 0) {
        this._onerror(
          Error(
            `Received a response for an unknown message ID: ${JSON.stringify(e)}`,
          ),
        );
        return;
      }
      (this._responseHandlers.delete(t), this._cleanupTimeout(t));
      let i = !1;
      if (gs(e) && e.result && typeof e.result == `object`) {
        let n = e.result;
        if (n.task && typeof n.task == `object`) {
          let e = n.task;
          typeof e.taskId == `string` &&
            ((i = !0), this._taskProgressTokens.set(e.taskId, t));
        }
      }
      (i || this._progressHandlers.delete(t),
        gs(e)
          ? r(e)
          : r(U.fromError(e.error.code, e.error.message, e.error.data)));
    }
    get transport() {
      return this._transport;
    }
    async close() {
      await this._transport?.close();
    }
    async *requestStream(e, t, n) {
      let { task: r } = n ?? {};
      if (!r) {
        try {
          yield { type: `result`, result: await this.request(e, t, n) };
        } catch (e) {
          yield {
            type: `error`,
            error: e instanceof U ? e : new U(H.InternalError, String(e)),
          };
        }
        return;
      }
      let i;
      try {
        let r = await this.request(e, Gs, n);
        if (r.task)
          ((i = r.task.taskId), yield { type: `taskCreated`, task: r.task });
        else
          throw new U(H.InternalError, `Task creation did not return a task`);
        for (;;) {
          let e = await this.getTask({ taskId: i }, n);
          if ((yield { type: `taskStatus`, task: e }, jl(e.status))) {
            e.status === `completed`
              ? yield {
                  type: `result`,
                  result: await this.getTaskResult({ taskId: i }, t, n),
                }
              : e.status === `failed`
                ? yield {
                    type: `error`,
                    error: new U(H.InternalError, `Task ${i} failed`),
                  }
                : e.status === `cancelled` &&
                  (yield {
                    type: `error`,
                    error: new U(H.InternalError, `Task ${i} was cancelled`),
                  });
            return;
          }
          if (e.status === `input_required`) {
            yield {
              type: `result`,
              result: await this.getTaskResult({ taskId: i }, t, n),
            };
            return;
          }
          let r =
            e.pollInterval ?? this._options?.defaultTaskPollInterval ?? 1e3;
          (await new Promise((e) => setTimeout(e, r)),
            n?.signal?.throwIfAborted());
        }
      } catch (e) {
        yield {
          type: `error`,
          error: e instanceof U ? e : new U(H.InternalError, String(e)),
        };
      }
    }
    request(e, t, n) {
      let {
        relatedRequestId: r,
        resumptionToken: i,
        onresumptiontoken: a,
        task: o,
        relatedTask: s,
      } = n ?? {};
      return new Promise((c, l) => {
        let u = (e) => {
          l(e);
        };
        if (!this._transport) {
          u(Error(`Not connected`));
          return;
        }
        if (this._options?.enforceStrictCapabilities === !0)
          try {
            (this.assertCapabilityForMethod(e.method),
              o && this.assertTaskCapability(e.method));
          } catch (e) {
            u(e);
            return;
          }
        n?.signal?.throwIfAborted();
        let d = this._requestMessageId++,
          f = { ...e, jsonrpc: `2.0`, id: d };
        (n?.onprogress &&
          (this._progressHandlers.set(d, n.onprogress),
          (f.params = {
            ...e.params,
            _meta: { ...(e.params?._meta || {}), progressToken: d },
          })),
          o && (f.params = { ...f.params, task: o }),
          s &&
            (f.params = {
              ...f.params,
              _meta: { ...(f.params?._meta || {}), [Xo]: s },
            }));
        let p = (e) => {
          (this._responseHandlers.delete(d),
            this._progressHandlers.delete(d),
            this._cleanupTimeout(d),
            this._transport
              ?.send(
                {
                  jsonrpc: `2.0`,
                  method: `notifications/cancelled`,
                  params: { requestId: d, reason: String(e) },
                },
                {
                  relatedRequestId: r,
                  resumptionToken: i,
                  onresumptiontoken: a,
                },
              )
              .catch((e) =>
                this._onerror(Error(`Failed to send cancellation: ${e}`)),
              ),
            l(e instanceof U ? e : new U(H.RequestTimeout, String(e))));
        };
        (this._responseHandlers.set(d, (e) => {
          if (!n?.signal?.aborted) {
            if (e instanceof Error) return l(e);
            try {
              let n = ea(t, e.result);
              n.success ? c(n.data) : l(n.error);
            } catch (e) {
              l(e);
            }
          }
        }),
          n?.signal?.addEventListener(`abort`, () => {
            p(n?.signal?.reason);
          }));
        let m = n?.timeout ?? 6e4;
        this._setupTimeout(
          d,
          m,
          n?.maxTotalTimeout,
          () =>
            p(
              U.fromError(H.RequestTimeout, `Request timed out`, {
                timeout: m,
              }),
            ),
          n?.resetTimeoutOnProgress ?? !1,
        );
        let h = s?.taskId;
        h
          ? (this._requestResolvers.set(d, (e) => {
              let t = this._responseHandlers.get(d);
              t
                ? t(e)
                : this._onerror(
                    Error(
                      `Response handler missing for side-channeled request ${d}`,
                    ),
                  );
            }),
            this._enqueueTaskMessage(h, {
              type: `request`,
              message: f,
              timestamp: Date.now(),
            }).catch((e) => {
              (this._cleanupTimeout(d), l(e));
            }))
          : this._transport
              .send(f, {
                relatedRequestId: r,
                resumptionToken: i,
                onresumptiontoken: a,
              })
              .catch((e) => {
                (this._cleanupTimeout(d), l(e));
              });
      });
    }
    async getTask(e, t) {
      return this.request({ method: `tasks/get`, params: e }, Ys, t);
    }
    async getTaskResult(e, t, n) {
      return this.request({ method: `tasks/result`, params: e }, t, n);
    }
    async listTasks(e, t) {
      return this.request({ method: `tasks/list`, params: e }, Qs, t);
    }
    async cancelTask(e, t) {
      return this.request({ method: `tasks/cancel`, params: e }, ec, t);
    }
    async notification(e, t) {
      if (!this._transport) throw Error(`Not connected`);
      this.assertNotificationCapability(e.method);
      let n = t?.relatedTask?.taskId;
      if (n) {
        let r = {
          ...e,
          jsonrpc: `2.0`,
          params: {
            ...e.params,
            _meta: { ...(e.params?._meta || {}), [Xo]: t.relatedTask },
          },
        };
        await this._enqueueTaskMessage(n, {
          type: `notification`,
          message: r,
          timestamp: Date.now(),
        });
        return;
      }
      if (
        (this._options?.debouncedNotificationMethods ?? []).includes(
          e.method,
        ) &&
        !e.params &&
        !t?.relatedRequestId &&
        !t?.relatedTask
      ) {
        if (this._pendingDebouncedNotifications.has(e.method)) return;
        (this._pendingDebouncedNotifications.add(e.method),
          Promise.resolve().then(() => {
            if (
              (this._pendingDebouncedNotifications.delete(e.method),
              !this._transport)
            )
              return;
            let n = { ...e, jsonrpc: `2.0` };
            (t?.relatedTask &&
              (n = {
                ...n,
                params: {
                  ...n.params,
                  _meta: { ...(n.params?._meta || {}), [Xo]: t.relatedTask },
                },
              }),
              this._transport?.send(n, t).catch((e) => this._onerror(e)));
          }));
        return;
      }
      let r = { ...e, jsonrpc: `2.0` };
      (t?.relatedTask &&
        (r = {
          ...r,
          params: {
            ...r.params,
            _meta: { ...(r.params?._meta || {}), [Xo]: t.relatedTask },
          },
        }),
        await this._transport.send(r, t));
    }
    setRequestHandler(e, t) {
      let n = Ml(e);
      (this.assertRequestHandlerCapability(n),
        this._requestHandlers.set(n, (n, r) => {
          let i = Nl(e, n);
          return Promise.resolve(t(i, r));
        }));
    }
    removeRequestHandler(e) {
      this._requestHandlers.delete(e);
    }
    assertCanSetRequestHandler(e) {
      if (this._requestHandlers.has(e))
        throw Error(
          `A request handler for ${e} already exists, which would be overridden`,
        );
    }
    setNotificationHandler(e, t) {
      let n = Ml(e);
      this._notificationHandlers.set(n, (n) => {
        let r = Nl(e, n);
        return Promise.resolve(t(r));
      });
    }
    removeNotificationHandler(e) {
      this._notificationHandlers.delete(e);
    }
    _cleanupTaskProgressHandler(e) {
      let t = this._taskProgressTokens.get(e);
      t !== void 0 &&
        (this._progressHandlers.delete(t), this._taskProgressTokens.delete(e));
    }
    async _enqueueTaskMessage(e, t, n) {
      if (!this._taskStore || !this._taskMessageQueue)
        throw Error(
          `Cannot enqueue task message: taskStore and taskMessageQueue are not configured`,
        );
      let r = this._options?.maxTaskQueueSize;
      await this._taskMessageQueue.enqueue(e, t, n, r);
    }
    async _clearTaskQueue(e, t) {
      if (this._taskMessageQueue) {
        let n = await this._taskMessageQueue.dequeueAll(e, t);
        for (let t of n)
          if (t.type === `request` && fs(t.message)) {
            let n = t.message.id,
              r = this._requestResolvers.get(n);
            r
              ? (r(new U(H.InternalError, `Task cancelled or completed`)),
                this._requestResolvers.delete(n))
              : this._onerror(
                  Error(
                    `Resolver missing for request ${n} during task ${e} cleanup`,
                  ),
                );
          }
      }
    }
    async _waitForTaskUpdate(e, t) {
      let n = this._options?.defaultTaskPollInterval ?? 1e3;
      try {
        let t = await this._taskStore?.getTask(e);
        t?.pollInterval && (n = t.pollInterval);
      } catch {}
      return new Promise((e, r) => {
        if (t.aborted) {
          r(new U(H.InvalidRequest, `Request cancelled`));
          return;
        }
        let i = setTimeout(e, n);
        t.addEventListener(
          `abort`,
          () => {
            (clearTimeout(i), r(new U(H.InvalidRequest, `Request cancelled`)));
          },
          { once: !0 },
        );
      });
    }
    requestTaskStore(e, t) {
      let n = this._taskStore;
      if (!n) throw Error(`No task store configured`);
      return {
        createTask: async (r) => {
          if (!e) throw Error(`No request provided`);
          return await n.createTask(
            r,
            e.id,
            { method: e.method, params: e.params },
            t,
          );
        },
        getTask: async (e) => {
          let r = await n.getTask(e, t);
          if (!r)
            throw new U(
              H.InvalidParams,
              `Failed to retrieve task: Task not found`,
            );
          return r;
        },
        storeTaskResult: async (e, r, i) => {
          await n.storeTaskResult(e, r, i, t);
          let a = await n.getTask(e, t);
          if (a) {
            let t = qs.parse({
              method: `notifications/tasks/status`,
              params: a,
            });
            (await this.notification(t),
              jl(a.status) && this._cleanupTaskProgressHandler(e));
          }
        },
        getTaskResult: (e) => n.getTaskResult(e, t),
        updateTaskStatus: async (e, r, i) => {
          let a = await n.getTask(e, t);
          if (!a)
            throw new U(
              H.InvalidParams,
              `Task "${e}" not found - it may have been cleaned up`,
            );
          if (jl(a.status))
            throw new U(
              H.InvalidParams,
              `Cannot update task "${e}" from terminal status "${a.status}" to "${r}". Terminal states (completed, failed, cancelled) cannot transition to other states.`,
            );
          await n.updateTaskStatus(e, r, i, t);
          let o = await n.getTask(e, t);
          if (o) {
            let t = qs.parse({
              method: `notifications/tasks/status`,
              params: o,
            });
            (await this.notification(t),
              jl(o.status) && this._cleanupTaskProgressHandler(e));
          }
        },
        listTasks: (e) => n.listTasks(e, t),
      };
    }
  };
  function Fl(e) {
    return typeof e == `object` && !!e && !Array.isArray(e);
  }
  function Il(e, t) {
    let n = { ...e };
    for (let e in t) {
      let r = e,
        i = t[r];
      if (i === void 0) continue;
      let a = n[r];
      Fl(a) && Fl(i) ? (n[r] = { ...a, ...i }) : (n[r] = i);
    }
    return n;
  }
  var Ll = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.regexpCode =
          e.getEsmExportName =
          e.getProperty =
          e.safeStringify =
          e.stringify =
          e.strConcat =
          e.addCodeArg =
          e.str =
          e._ =
          e.nil =
          e._Code =
          e.Name =
          e.IDENTIFIER =
          e._CodeOrName =
            void 0));
      var t = class {};
      ((e._CodeOrName = t), (e.IDENTIFIER = /^[a-z$_][a-z$_0-9]*$/i));
      var n = class extends t {
        constructor(t) {
          if ((super(), !e.IDENTIFIER.test(t)))
            throw Error(`CodeGen: name must be a valid identifier`);
          this.str = t;
        }
        toString() {
          return this.str;
        }
        emptyStr() {
          return !1;
        }
        get names() {
          return { [this.str]: 1 };
        }
      };
      e.Name = n;
      var r = class extends t {
        constructor(e) {
          (super(), (this._items = typeof e == `string` ? [e] : e));
        }
        toString() {
          return this.str;
        }
        emptyStr() {
          if (this._items.length > 1) return !1;
          let e = this._items[0];
          return e === `` || e === `""`;
        }
        get str() {
          return (this._str ??= this._items.reduce((e, t) => `${e}${t}`, ``));
        }
        get names() {
          return (this._names ??= this._items.reduce(
            (e, t) => (t instanceof n && (e[t.str] = (e[t.str] || 0) + 1), e),
            {},
          ));
        }
      };
      ((e._Code = r), (e.nil = new r(``)));
      function i(e, ...t) {
        let n = [e[0]],
          i = 0;
        for (; i < t.length; ) (s(n, t[i]), n.push(e[++i]));
        return new r(n);
      }
      e._ = i;
      var a = new r(`+`);
      function o(e, ...t) {
        let n = [p(e[0])],
          i = 0;
        for (; i < t.length; ) (n.push(a), s(n, t[i]), n.push(a, p(e[++i])));
        return (c(n), new r(n));
      }
      e.str = o;
      function s(e, t) {
        t instanceof r
          ? e.push(...t._items)
          : t instanceof n
            ? e.push(t)
            : e.push(d(t));
      }
      e.addCodeArg = s;
      function c(e) {
        let t = 1;
        for (; t < e.length - 1; ) {
          if (e[t] === a) {
            let n = l(e[t - 1], e[t + 1]);
            if (n !== void 0) {
              e.splice(t - 1, 3, n);
              continue;
            }
            e[t++] = `+`;
          }
          t++;
        }
      }
      function l(e, t) {
        if (t === `""`) return e;
        if (e === `""`) return t;
        if (typeof e == `string`)
          return t instanceof n || e[e.length - 1] !== `"`
            ? void 0
            : typeof t == `string`
              ? t[0] === `"`
                ? e.slice(0, -1) + t.slice(1)
                : void 0
              : `${e.slice(0, -1)}${t}"`;
        if (typeof t == `string` && t[0] === `"` && !(e instanceof n))
          return `"${e}${t.slice(1)}`;
      }
      function u(e, t) {
        return t.emptyStr() ? e : e.emptyStr() ? t : o`${e}${t}`;
      }
      e.strConcat = u;
      function d(e) {
        return typeof e == `number` || typeof e == `boolean` || e === null
          ? e
          : p(Array.isArray(e) ? e.join(`,`) : e);
      }
      function f(e) {
        return new r(p(e));
      }
      e.stringify = f;
      function p(e) {
        return JSON.stringify(e)
          .replace(/\u2028/g, `\\u2028`)
          .replace(/\u2029/g, `\\u2029`);
      }
      e.safeStringify = p;
      function m(t) {
        return typeof t == `string` && e.IDENTIFIER.test(t)
          ? new r(`.${t}`)
          : i`[${t}]`;
      }
      e.getProperty = m;
      function h(t) {
        if (typeof t == `string` && e.IDENTIFIER.test(t)) return new r(`${t}`);
        throw Error(
          `CodeGen: invalid export name: ${t}, use explicit $id name mapping`,
        );
      }
      e.getEsmExportName = h;
      function g(e) {
        return new r(e.toString());
      }
      e.regexpCode = g;
    }),
    Rl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.ValueScope =
          e.ValueScopeName =
          e.Scope =
          e.varKinds =
          e.UsedValueState =
            void 0));
      var t = Ll(),
        n = class extends Error {
          constructor(e) {
            (super(`CodeGen: "code" for ${e} not defined`),
              (this.value = e.value));
          }
        },
        r;
      ((function (e) {
        ((e[(e.Started = 0)] = `Started`),
          (e[(e.Completed = 1)] = `Completed`));
      })(r || (e.UsedValueState = r = {})),
        (e.varKinds = {
          const: new t.Name(`const`),
          let: new t.Name(`let`),
          var: new t.Name(`var`),
        }));
      var i = class {
        constructor({ prefixes: e, parent: t } = {}) {
          ((this._names = {}), (this._prefixes = e), (this._parent = t));
        }
        toName(e) {
          return e instanceof t.Name ? e : this.name(e);
        }
        name(e) {
          return new t.Name(this._newName(e));
        }
        _newName(e) {
          let t = this._names[e] || this._nameGroup(e);
          return `${e}${t.index++}`;
        }
        _nameGroup(e) {
          if (
            this._parent?._prefixes?.has(e) ||
            (this._prefixes && !this._prefixes.has(e))
          )
            throw Error(`CodeGen: prefix "${e}" is not allowed in this scope`);
          return (this._names[e] = { prefix: e, index: 0 });
        }
      };
      e.Scope = i;
      var a = class extends t.Name {
        constructor(e, t) {
          (super(t), (this.prefix = e));
        }
        setValue(e, { property: n, itemIndex: r }) {
          ((this.value = e),
            (this.scopePath = (0, t._)`.${new t.Name(n)}[${r}]`));
        }
      };
      e.ValueScopeName = a;
      var o = (0, t._)`\n`;
      e.ValueScope = class extends i {
        constructor(e) {
          (super(e),
            (this._values = {}),
            (this._scope = e.scope),
            (this.opts = { ...e, _n: e.lines ? o : t.nil }));
        }
        get() {
          return this._scope;
        }
        name(e) {
          return new a(e, this._newName(e));
        }
        value(e, t) {
          if (t.ref === void 0)
            throw Error(`CodeGen: ref must be passed in value`);
          let n = this.toName(e),
            { prefix: r } = n,
            i = t.key ?? t.ref,
            a = this._values[r];
          if (a) {
            let e = a.get(i);
            if (e) return e;
          } else a = this._values[r] = new Map();
          a.set(i, n);
          let o = this._scope[r] || (this._scope[r] = []),
            s = o.length;
          return (
            (o[s] = t.ref), n.setValue(t, { property: r, itemIndex: s }), n
          );
        }
        getValue(e, t) {
          let n = this._values[e];
          if (n) return n.get(t);
        }
        scopeRefs(e, n = this._values) {
          return this._reduceValues(n, (n) => {
            if (n.scopePath === void 0)
              throw Error(`CodeGen: name "${n}" has no value`);
            return (0, t._)`${e}${n.scopePath}`;
          });
        }
        scopeCode(e = this._values, t, n) {
          return this._reduceValues(
            e,
            (e) => {
              if (e.value === void 0)
                throw Error(`CodeGen: name "${e}" has no value`);
              return e.value.code;
            },
            t,
            n,
          );
        }
        _reduceValues(i, a, o = {}, s) {
          let c = t.nil;
          for (let l in i) {
            let u = i[l];
            if (!u) continue;
            let d = (o[l] = o[l] || new Map());
            u.forEach((i) => {
              if (d.has(i)) return;
              d.set(i, r.Started);
              let o = a(i);
              if (o) {
                let n = this.opts.es5 ? e.varKinds.var : e.varKinds.const;
                c = (0, t._)`${c}${n} ${i} = ${o};${this.opts._n}`;
              } else if ((o = s?.(i))) c = (0, t._)`${c}${o}${this.opts._n}`;
              else throw new n(i);
              d.set(i, r.Completed);
            });
          }
          return c;
        }
      };
    }),
    W = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.or =
          e.and =
          e.not =
          e.CodeGen =
          e.operators =
          e.varKinds =
          e.ValueScopeName =
          e.ValueScope =
          e.Scope =
          e.Name =
          e.regexpCode =
          e.stringify =
          e.getProperty =
          e.nil =
          e.strConcat =
          e.str =
          e._ =
            void 0));
      var t = Ll(),
        n = Rl(),
        r = Ll();
      (Object.defineProperty(e, "_", {
        enumerable: !0,
        get: function () {
          return r._;
        },
      }),
        Object.defineProperty(e, "str", {
          enumerable: !0,
          get: function () {
            return r.str;
          },
        }),
        Object.defineProperty(e, "strConcat", {
          enumerable: !0,
          get: function () {
            return r.strConcat;
          },
        }),
        Object.defineProperty(e, "nil", {
          enumerable: !0,
          get: function () {
            return r.nil;
          },
        }),
        Object.defineProperty(e, "getProperty", {
          enumerable: !0,
          get: function () {
            return r.getProperty;
          },
        }),
        Object.defineProperty(e, "stringify", {
          enumerable: !0,
          get: function () {
            return r.stringify;
          },
        }),
        Object.defineProperty(e, "regexpCode", {
          enumerable: !0,
          get: function () {
            return r.regexpCode;
          },
        }),
        Object.defineProperty(e, "Name", {
          enumerable: !0,
          get: function () {
            return r.Name;
          },
        }));
      var i = Rl();
      (Object.defineProperty(e, "Scope", {
        enumerable: !0,
        get: function () {
          return i.Scope;
        },
      }),
        Object.defineProperty(e, "ValueScope", {
          enumerable: !0,
          get: function () {
            return i.ValueScope;
          },
        }),
        Object.defineProperty(e, "ValueScopeName", {
          enumerable: !0,
          get: function () {
            return i.ValueScopeName;
          },
        }),
        Object.defineProperty(e, "varKinds", {
          enumerable: !0,
          get: function () {
            return i.varKinds;
          },
        }),
        (e.operators = {
          GT: new t._Code(`>`),
          GTE: new t._Code(`>=`),
          LT: new t._Code(`<`),
          LTE: new t._Code(`<=`),
          EQ: new t._Code(`===`),
          NEQ: new t._Code(`!==`),
          NOT: new t._Code(`!`),
          OR: new t._Code(`||`),
          AND: new t._Code(`&&`),
          ADD: new t._Code(`+`),
        }));
      var a = class {
          optimizeNodes() {
            return this;
          }
          optimizeNames(e, t) {
            return this;
          }
        },
        o = class extends a {
          constructor(e, t, n) {
            (super(), (this.varKind = e), (this.name = t), (this.rhs = n));
          }
          render({ es5: e, _n: t }) {
            let r = e ? n.varKinds.var : this.varKind,
              i = this.rhs === void 0 ? `` : ` = ${this.rhs}`;
            return `${r} ${this.name}${i};` + t;
          }
          optimizeNames(e, t) {
            if (e[this.name.str])
              return ((this.rhs &&= ie(this.rhs, e, t)), this);
          }
          get names() {
            return this.rhs instanceof t._CodeOrName ? this.rhs.names : {};
          }
        },
        s = class extends a {
          constructor(e, t, n) {
            (super(), (this.lhs = e), (this.rhs = t), (this.sideEffects = n));
          }
          render({ _n: e }) {
            return `${this.lhs} = ${this.rhs};` + e;
          }
          optimizeNames(e, n) {
            if (
              !(
                this.lhs instanceof t.Name &&
                !e[this.lhs.str] &&
                !this.sideEffects
              )
            )
              return ((this.rhs = ie(this.rhs, e, n)), this);
          }
          get names() {
            return re(
              this.lhs instanceof t.Name ? {} : { ...this.lhs.names },
              this.rhs,
            );
          }
        },
        c = class extends s {
          constructor(e, t, n, r) {
            (super(e, n, r), (this.op = t));
          }
          render({ _n: e }) {
            return `${this.lhs} ${this.op}= ${this.rhs};` + e;
          }
        },
        l = class extends a {
          constructor(e) {
            (super(), (this.label = e), (this.names = {}));
          }
          render({ _n: e }) {
            return `${this.label}:` + e;
          }
        },
        u = class extends a {
          constructor(e) {
            (super(), (this.label = e), (this.names = {}));
          }
          render({ _n: e }) {
            return `break${this.label ? ` ${this.label}` : ``};` + e;
          }
        },
        d = class extends a {
          constructor(e) {
            (super(), (this.error = e));
          }
          render({ _n: e }) {
            return `throw ${this.error};` + e;
          }
          get names() {
            return this.error.names;
          }
        },
        f = class extends a {
          constructor(e) {
            (super(), (this.code = e));
          }
          render({ _n: e }) {
            return `${this.code};` + e;
          }
          optimizeNodes() {
            return `${this.code}` ? this : void 0;
          }
          optimizeNames(e, t) {
            return ((this.code = ie(this.code, e, t)), this);
          }
          get names() {
            return this.code instanceof t._CodeOrName ? this.code.names : {};
          }
        },
        p = class extends a {
          constructor(e = []) {
            (super(), (this.nodes = e));
          }
          render(e) {
            return this.nodes.reduce((t, n) => t + n.render(e), ``);
          }
          optimizeNodes() {
            let { nodes: e } = this,
              t = e.length;
            for (; t--; ) {
              let n = e[t].optimizeNodes();
              Array.isArray(n)
                ? e.splice(t, 1, ...n)
                : n
                  ? (e[t] = n)
                  : e.splice(t, 1);
            }
            return e.length > 0 ? this : void 0;
          }
          optimizeNames(e, t) {
            let { nodes: n } = this,
              r = n.length;
            for (; r--; ) {
              let i = n[r];
              i.optimizeNames(e, t) || (ae(e, i.names), n.splice(r, 1));
            }
            return n.length > 0 ? this : void 0;
          }
          get names() {
            return this.nodes.reduce((e, t) => ne(e, t.names), {});
          }
        },
        m = class extends p {
          render(e) {
            return `{` + e._n + super.render(e) + `}` + e._n;
          }
        },
        h = class extends p {},
        g = class extends m {};
      g.kind = `else`;
      var _ = class e extends m {
        constructor(e, t) {
          (super(t), (this.condition = e));
        }
        render(e) {
          let t = `if(${this.condition})` + super.render(e);
          return (this.else && (t += `else ` + this.else.render(e)), t);
        }
        optimizeNodes() {
          super.optimizeNodes();
          let t = this.condition;
          if (t === !0) return this.nodes;
          let n = this.else;
          if (n) {
            let e = n.optimizeNodes();
            n = this.else = Array.isArray(e) ? new g(e) : e;
          }
          if (n)
            return t === !1
              ? n instanceof e
                ? n
                : n.nodes
              : this.nodes.length
                ? this
                : new e(oe(t), n instanceof e ? [n] : n.nodes);
          if (!(t === !1 || !this.nodes.length)) return this;
        }
        optimizeNames(e, t) {
          if (
            ((this.else = this.else?.optimizeNames(e, t)),
            super.optimizeNames(e, t) || this.else)
          )
            return ((this.condition = ie(this.condition, e, t)), this);
        }
        get names() {
          let e = super.names;
          return (
            re(e, this.condition), this.else && ne(e, this.else.names), e
          );
        }
      };
      _.kind = `if`;
      var v = class extends m {};
      v.kind = `for`;
      var y = class extends v {
          constructor(e) {
            (super(), (this.iteration = e));
          }
          render(e) {
            return `for(${this.iteration})` + super.render(e);
          }
          optimizeNames(e, t) {
            if (super.optimizeNames(e, t))
              return ((this.iteration = ie(this.iteration, e, t)), this);
          }
          get names() {
            return ne(super.names, this.iteration.names);
          }
        },
        b = class extends v {
          constructor(e, t, n, r) {
            (super(),
              (this.varKind = e),
              (this.name = t),
              (this.from = n),
              (this.to = r));
          }
          render(e) {
            let t = e.es5 ? n.varKinds.var : this.varKind,
              { name: r, from: i, to: a } = this;
            return `for(${t} ${r}=${i}; ${r}<${a}; ${r}++)` + super.render(e);
          }
          get names() {
            return re(re(super.names, this.from), this.to);
          }
        },
        x = class extends v {
          constructor(e, t, n, r) {
            (super(),
              (this.loop = e),
              (this.varKind = t),
              (this.name = n),
              (this.iterable = r));
          }
          render(e) {
            return (
              `for(${this.varKind} ${this.name} ${this.loop} ${this.iterable})` +
              super.render(e)
            );
          }
          optimizeNames(e, t) {
            if (super.optimizeNames(e, t))
              return ((this.iterable = ie(this.iterable, e, t)), this);
          }
          get names() {
            return ne(super.names, this.iterable.names);
          }
        },
        ee = class extends m {
          constructor(e, t, n) {
            (super(), (this.name = e), (this.args = t), (this.async = n));
          }
          render(e) {
            return (
              `${this.async ? `async ` : ``}function ${this.name}(${this.args})` +
              super.render(e)
            );
          }
        };
      ee.kind = `func`;
      var S = class extends p {
        render(e) {
          return `return ` + super.render(e);
        }
      };
      S.kind = `return`;
      var C = class extends m {
          render(e) {
            let t = `try` + super.render(e);
            return (
              this.catch && (t += this.catch.render(e)),
              this.finally && (t += this.finally.render(e)),
              t
            );
          }
          optimizeNodes() {
            var e, t;
            return (
              super.optimizeNodes(),
              (e = this.catch) == null || e.optimizeNodes(),
              (t = this.finally) == null || t.optimizeNodes(),
              this
            );
          }
          optimizeNames(e, t) {
            var n, r;
            return (
              super.optimizeNames(e, t),
              (n = this.catch) == null || n.optimizeNames(e, t),
              (r = this.finally) == null || r.optimizeNames(e, t),
              this
            );
          }
          get names() {
            let e = super.names;
            return (
              this.catch && ne(e, this.catch.names),
              this.finally && ne(e, this.finally.names),
              e
            );
          }
        },
        w = class extends m {
          constructor(e) {
            (super(), (this.error = e));
          }
          render(e) {
            return `catch(${this.error})` + super.render(e);
          }
        };
      w.kind = `catch`;
      var te = class extends m {
        render(e) {
          return `finally` + super.render(e);
        }
      };
      ((te.kind = `finally`),
        (e.CodeGen = class {
          constructor(e, t = {}) {
            ((this._values = {}),
              (this._blockStarts = []),
              (this._constants = {}),
              (this.opts = {
                ...t,
                _n: t.lines
                  ? `
`
                  : ``,
              }),
              (this._extScope = e),
              (this._scope = new n.Scope({ parent: e })),
              (this._nodes = [new h()]));
          }
          toString() {
            return this._root.render(this.opts);
          }
          name(e) {
            return this._scope.name(e);
          }
          scopeName(e) {
            return this._extScope.name(e);
          }
          scopeValue(e, t) {
            let n = this._extScope.value(e, t);
            return (
              (
                this._values[n.prefix] || (this._values[n.prefix] = new Set())
              ).add(n),
              n
            );
          }
          getScopeValue(e, t) {
            return this._extScope.getValue(e, t);
          }
          scopeRefs(e) {
            return this._extScope.scopeRefs(e, this._values);
          }
          scopeCode() {
            return this._extScope.scopeCode(this._values);
          }
          _def(e, t, n, r) {
            let i = this._scope.toName(t);
            return (
              n !== void 0 && r && (this._constants[i.str] = n),
              this._leafNode(new o(e, i, n)),
              i
            );
          }
          const(e, t, r) {
            return this._def(n.varKinds.const, e, t, r);
          }
          let(e, t, r) {
            return this._def(n.varKinds.let, e, t, r);
          }
          var(e, t, r) {
            return this._def(n.varKinds.var, e, t, r);
          }
          assign(e, t, n) {
            return this._leafNode(new s(e, t, n));
          }
          add(t, n) {
            return this._leafNode(new c(t, e.operators.ADD, n));
          }
          code(e) {
            return (
              typeof e == `function`
                ? e()
                : e !== t.nil && this._leafNode(new f(e)),
              this
            );
          }
          object(...e) {
            let n = [`{`];
            for (let [r, i] of e)
              (n.length > 1 && n.push(`,`),
                n.push(r),
                (r !== i || this.opts.es5) &&
                  (n.push(`:`), (0, t.addCodeArg)(n, i)));
            return (n.push(`}`), new t._Code(n));
          }
          if(e, t, n) {
            if ((this._blockNode(new _(e)), t && n))
              this.code(t).else().code(n).endIf();
            else if (t) this.code(t).endIf();
            else if (n) throw Error(`CodeGen: "else" body without "then" body`);
            return this;
          }
          elseIf(e) {
            return this._elseNode(new _(e));
          }
          else() {
            return this._elseNode(new g());
          }
          endIf() {
            return this._endBlockNode(_, g);
          }
          _for(e, t) {
            return (this._blockNode(e), t && this.code(t).endFor(), this);
          }
          for(e, t) {
            return this._for(new y(e), t);
          }
          forRange(
            e,
            t,
            r,
            i,
            a = this.opts.es5 ? n.varKinds.var : n.varKinds.let,
          ) {
            let o = this._scope.toName(e);
            return this._for(new b(a, o, t, r), () => i(o));
          }
          forOf(e, r, i, a = n.varKinds.const) {
            let o = this._scope.toName(e);
            if (this.opts.es5) {
              let e = r instanceof t.Name ? r : this.var(`_arr`, r);
              return this.forRange(`_i`, 0, (0, t._)`${e}.length`, (n) => {
                (this.var(o, (0, t._)`${e}[${n}]`), i(o));
              });
            }
            return this._for(new x(`of`, a, o, r), () => i(o));
          }
          forIn(
            e,
            r,
            i,
            a = this.opts.es5 ? n.varKinds.var : n.varKinds.const,
          ) {
            if (this.opts.ownProperties)
              return this.forOf(e, (0, t._)`Object.keys(${r})`, i);
            let o = this._scope.toName(e);
            return this._for(new x(`in`, a, o, r), () => i(o));
          }
          endFor() {
            return this._endBlockNode(v);
          }
          label(e) {
            return this._leafNode(new l(e));
          }
          break(e) {
            return this._leafNode(new u(e));
          }
          return(e) {
            let t = new S();
            if ((this._blockNode(t), this.code(e), t.nodes.length !== 1))
              throw Error(`CodeGen: "return" should have one node`);
            return this._endBlockNode(S);
          }
          try(e, t, n) {
            if (!t && !n)
              throw Error(`CodeGen: "try" without "catch" and "finally"`);
            let r = new C();
            if ((this._blockNode(r), this.code(e), t)) {
              let e = this.name(`e`);
              ((this._currNode = r.catch = new w(e)), t(e));
            }
            return (
              n && ((this._currNode = r.finally = new te()), this.code(n)),
              this._endBlockNode(w, te)
            );
          }
          throw(e) {
            return this._leafNode(new d(e));
          }
          block(e, t) {
            return (
              this._blockStarts.push(this._nodes.length),
              e && this.code(e).endBlock(t),
              this
            );
          }
          endBlock(e) {
            let t = this._blockStarts.pop();
            if (t === void 0)
              throw Error(`CodeGen: not in self-balancing block`);
            let n = this._nodes.length - t;
            if (n < 0 || (e !== void 0 && n !== e))
              throw Error(
                `CodeGen: wrong number of nodes: ${n} vs ${e} expected`,
              );
            return ((this._nodes.length = t), this);
          }
          func(e, n = t.nil, r, i) {
            return (
              this._blockNode(new ee(e, n, r)),
              i && this.code(i).endFunc(),
              this
            );
          }
          endFunc() {
            return this._endBlockNode(ee);
          }
          optimize(e = 1) {
            for (; e-- > 0; )
              (this._root.optimizeNodes(),
                this._root.optimizeNames(this._root.names, this._constants));
          }
          _leafNode(e) {
            return (this._currNode.nodes.push(e), this);
          }
          _blockNode(e) {
            (this._currNode.nodes.push(e), this._nodes.push(e));
          }
          _endBlockNode(e, t) {
            let n = this._currNode;
            if (n instanceof e || (t && n instanceof t))
              return (this._nodes.pop(), this);
            throw Error(
              `CodeGen: not in block "${t ? `${e.kind}/${t.kind}` : e.kind}"`,
            );
          }
          _elseNode(e) {
            let t = this._currNode;
            if (!(t instanceof _)) throw Error(`CodeGen: "else" without "if"`);
            return ((this._currNode = t.else = e), this);
          }
          get _root() {
            return this._nodes[0];
          }
          get _currNode() {
            let e = this._nodes;
            return e[e.length - 1];
          }
          set _currNode(e) {
            let t = this._nodes;
            t[t.length - 1] = e;
          }
        }));
      function ne(e, t) {
        for (let n in t) e[n] = (e[n] || 0) + (t[n] || 0);
        return e;
      }
      function re(e, n) {
        return n instanceof t._CodeOrName ? ne(e, n.names) : e;
      }
      function ie(e, n, r) {
        if (e instanceof t.Name) return i(e);
        if (!a(e)) return e;
        return new t._Code(
          e._items.reduce(
            (e, n) => (
              n instanceof t.Name && (n = i(n)),
              n instanceof t._Code ? e.push(...n._items) : e.push(n),
              e
            ),
            [],
          ),
        );
        function i(e) {
          let t = r[e.str];
          return t === void 0 || n[e.str] !== 1 ? e : (delete n[e.str], t);
        }
        function a(e) {
          return (
            e instanceof t._Code &&
            e._items.some(
              (e) =>
                e instanceof t.Name && n[e.str] === 1 && r[e.str] !== void 0,
            )
          );
        }
      }
      function ae(e, t) {
        for (let n in t) e[n] = (e[n] || 0) - (t[n] || 0);
      }
      function oe(e) {
        return typeof e == `boolean` || typeof e == `number` || e === null
          ? !e
          : (0, t._)`!${D(e)}`;
      }
      e.not = oe;
      var se = E(e.operators.AND);
      function ce(...e) {
        return e.reduce(se);
      }
      e.and = ce;
      var le = E(e.operators.OR);
      function T(...e) {
        return e.reduce(le);
      }
      e.or = T;
      function E(e) {
        return (n, r) =>
          n === t.nil ? r : r === t.nil ? n : (0, t._)`${D(n)} ${e} ${D(r)}`;
      }
      function D(e) {
        return e instanceof t.Name ? e : (0, t._)`(${e})`;
      }
    }),
    G = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.checkStrictMode =
          e.getErrorPath =
          e.Type =
          e.useFunc =
          e.setEvaluated =
          e.evaluatedPropsToName =
          e.mergeEvaluated =
          e.eachItem =
          e.unescapeJsonPointer =
          e.escapeJsonPointer =
          e.escapeFragment =
          e.unescapeFragment =
          e.schemaRefOrVal =
          e.schemaHasRulesButRef =
          e.schemaHasRules =
          e.checkUnknownRules =
          e.alwaysValidSchema =
          e.toHash =
            void 0));
      var t = W(),
        n = Ll();
      function r(e) {
        let t = {};
        for (let n of e) t[n] = !0;
        return t;
      }
      e.toHash = r;
      function i(e, t) {
        return typeof t == `boolean`
          ? t
          : Object.keys(t).length === 0
            ? !0
            : (a(e, t), !o(t, e.self.RULES.all));
      }
      e.alwaysValidSchema = i;
      function a(e, t = e.schema) {
        let { opts: n, self: r } = e;
        if (!n.strictSchema || typeof t == `boolean`) return;
        let i = r.RULES.keywords;
        for (let n in t) i[n] || x(e, `unknown keyword: "${n}"`);
      }
      e.checkUnknownRules = a;
      function o(e, t) {
        if (typeof e == `boolean`) return !e;
        for (let n in e) if (t[n]) return !0;
        return !1;
      }
      e.schemaHasRules = o;
      function s(e, t) {
        if (typeof e == `boolean`) return !e;
        for (let n in e) if (n !== `$ref` && t.all[n]) return !0;
        return !1;
      }
      e.schemaHasRulesButRef = s;
      function c({ topSchemaRef: e, schemaPath: n }, r, i, a) {
        if (!a) {
          if (typeof r == `number` || typeof r == `boolean`) return r;
          if (typeof r == `string`) return (0, t._)`${r}`;
        }
        return (0, t._)`${e}${n}${(0, t.getProperty)(i)}`;
      }
      e.schemaRefOrVal = c;
      function l(e) {
        return f(decodeURIComponent(e));
      }
      e.unescapeFragment = l;
      function u(e) {
        return encodeURIComponent(d(e));
      }
      e.escapeFragment = u;
      function d(e) {
        return typeof e == `number`
          ? `${e}`
          : e.replace(/~/g, `~0`).replace(/\//g, `~1`);
      }
      e.escapeJsonPointer = d;
      function f(e) {
        return e.replace(/~1/g, `/`).replace(/~0/g, `~`);
      }
      e.unescapeJsonPointer = f;
      function p(e, t) {
        if (Array.isArray(e)) for (let n of e) t(n);
        else t(e);
      }
      e.eachItem = p;
      function m({
        mergeNames: e,
        mergeToName: n,
        mergeValues: r,
        resultToName: i,
      }) {
        return (a, o, s, c) => {
          let l =
            s === void 0
              ? o
              : s instanceof t.Name
                ? (o instanceof t.Name ? e(a, o, s) : n(a, o, s), s)
                : o instanceof t.Name
                  ? (n(a, s, o), o)
                  : r(o, s);
          return c === t.Name && !(l instanceof t.Name) ? i(a, l) : l;
        };
      }
      e.mergeEvaluated = {
        props: m({
          mergeNames: (e, n, r) =>
            e.if((0, t._)`${r} !== true && ${n} !== undefined`, () => {
              e.if(
                (0, t._)`${n} === true`,
                () => e.assign(r, !0),
                () =>
                  e
                    .assign(r, (0, t._)`${r} || {}`)
                    .code((0, t._)`Object.assign(${r}, ${n})`),
              );
            }),
          mergeToName: (e, n, r) =>
            e.if((0, t._)`${r} !== true`, () => {
              n === !0
                ? e.assign(r, !0)
                : (e.assign(r, (0, t._)`${r} || {}`), g(e, r, n));
            }),
          mergeValues: (e, t) => e === !0 || { ...e, ...t },
          resultToName: h,
        }),
        items: m({
          mergeNames: (e, n, r) =>
            e.if((0, t._)`${r} !== true && ${n} !== undefined`, () =>
              e.assign(
                r,
                (0, t._)`${n} === true ? true : ${r} > ${n} ? ${r} : ${n}`,
              ),
            ),
          mergeToName: (e, n, r) =>
            e.if((0, t._)`${r} !== true`, () =>
              e.assign(r, n === !0 || (0, t._)`${r} > ${n} ? ${r} : ${n}`),
            ),
          mergeValues: (e, t) => e === !0 || Math.max(e, t),
          resultToName: (e, t) => e.var(`items`, t),
        }),
      };
      function h(e, n) {
        if (n === !0) return e.var(`props`, !0);
        let r = e.var(`props`, (0, t._)`{}`);
        return (n !== void 0 && g(e, r, n), r);
      }
      e.evaluatedPropsToName = h;
      function g(e, n, r) {
        Object.keys(r).forEach((r) =>
          e.assign((0, t._)`${n}${(0, t.getProperty)(r)}`, !0),
        );
      }
      e.setEvaluated = g;
      var _ = {};
      function v(e, t) {
        return e.scopeValue(`func`, {
          ref: t,
          code: _[t.code] || (_[t.code] = new n._Code(t.code)),
        });
      }
      e.useFunc = v;
      var y;
      (function (e) {
        ((e[(e.Num = 0)] = `Num`), (e[(e.Str = 1)] = `Str`));
      })(y || (e.Type = y = {}));
      function b(e, n, r) {
        if (e instanceof t.Name) {
          let i = n === y.Num;
          return r
            ? i
              ? (0, t._)`"[" + ${e} + "]"`
              : (0, t._)`"['" + ${e} + "']"`
            : i
              ? (0, t._)`"/" + ${e}`
              : (0, t._)`"/" + ${e}.replace(/~/g, "~0").replace(/\\//g, "~1")`;
        }
        return r ? (0, t.getProperty)(e).toString() : `/` + d(e);
      }
      e.getErrorPath = b;
      function x(e, t, n = e.opts.strictSchema) {
        if (n) {
          if (((t = `strict mode: ${t}`), n === !0)) throw Error(t);
          e.self.logger.warn(t);
        }
      }
      e.checkStrictMode = x;
    }),
    K = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W();
      e.default = {
        data: new t.Name(`data`),
        valCxt: new t.Name(`valCxt`),
        instancePath: new t.Name(`instancePath`),
        parentData: new t.Name(`parentData`),
        parentDataProperty: new t.Name(`parentDataProperty`),
        rootData: new t.Name(`rootData`),
        dynamicAnchors: new t.Name(`dynamicAnchors`),
        vErrors: new t.Name(`vErrors`),
        errors: new t.Name(`errors`),
        this: new t.Name(`this`),
        self: new t.Name(`self`),
        scope: new t.Name(`scope`),
        json: new t.Name(`json`),
        jsonPos: new t.Name(`jsonPos`),
        jsonLen: new t.Name(`jsonLen`),
        jsonPart: new t.Name(`jsonPart`),
      };
    }),
    zl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.extendErrors =
          e.resetErrorsCount =
          e.reportExtraError =
          e.reportError =
          e.keyword$DataError =
          e.keywordError =
            void 0));
      var t = W(),
        n = G(),
        r = K();
      ((e.keywordError = {
        message: ({ keyword: e }) =>
          (0, t.str)`must pass "${e}" keyword validation`,
      }),
        (e.keyword$DataError = {
          message: ({ keyword: e, schemaType: n }) =>
            n
              ? (0, t.str)`"${e}" keyword must be ${n} ($data)`
              : (0, t.str)`"${e}" keyword is invalid ($data)`,
        }));
      function i(n, r = e.keywordError, i, a) {
        let { it: o } = n,
          { gen: s, compositeRule: u, allErrors: f } = o,
          p = d(n, r, i);
        (a ?? (u || f)) ? c(s, p) : l(o, (0, t._)`[${p}]`);
      }
      e.reportError = i;
      function a(t, n = e.keywordError, i) {
        let { it: a } = t,
          { gen: o, compositeRule: s, allErrors: u } = a;
        (c(o, d(t, n, i)), s || u || l(a, r.default.vErrors));
      }
      e.reportExtraError = a;
      function o(e, n) {
        (e.assign(r.default.errors, n),
          e.if((0, t._)`${r.default.vErrors} !== null`, () =>
            e.if(
              n,
              () => e.assign((0, t._)`${r.default.vErrors}.length`, n),
              () => e.assign(r.default.vErrors, null),
            ),
          ));
      }
      e.resetErrorsCount = o;
      function s({
        gen: e,
        keyword: n,
        schemaValue: i,
        data: a,
        errsCount: o,
        it: s,
      }) {
        if (o === void 0) throw Error(`ajv implementation error`);
        let c = e.name(`err`);
        e.forRange(`i`, o, r.default.errors, (o) => {
          (e.const(c, (0, t._)`${r.default.vErrors}[${o}]`),
            e.if((0, t._)`${c}.instancePath === undefined`, () =>
              e.assign(
                (0, t._)`${c}.instancePath`,
                (0, t.strConcat)(r.default.instancePath, s.errorPath),
              ),
            ),
            e.assign(
              (0, t._)`${c}.schemaPath`,
              (0, t.str)`${s.errSchemaPath}/${n}`,
            ),
            s.opts.verbose &&
              (e.assign((0, t._)`${c}.schema`, i),
              e.assign((0, t._)`${c}.data`, a)));
        });
      }
      e.extendErrors = s;
      function c(e, n) {
        let i = e.const(`err`, n);
        (e.if(
          (0, t._)`${r.default.vErrors} === null`,
          () => e.assign(r.default.vErrors, (0, t._)`[${i}]`),
          (0, t._)`${r.default.vErrors}.push(${i})`,
        ),
          e.code((0, t._)`${r.default.errors}++`));
      }
      function l(e, n) {
        let { gen: r, validateName: i, schemaEnv: a } = e;
        a.$async
          ? r.throw((0, t._)`new ${e.ValidationError}(${n})`)
          : (r.assign((0, t._)`${i}.errors`, n), r.return(!1));
      }
      var u = {
        keyword: new t.Name(`keyword`),
        schemaPath: new t.Name(`schemaPath`),
        params: new t.Name(`params`),
        propertyName: new t.Name(`propertyName`),
        message: new t.Name(`message`),
        schema: new t.Name(`schema`),
        parentSchema: new t.Name(`parentSchema`),
      };
      function d(e, n, r) {
        let { createErrors: i } = e.it;
        return i === !1 ? (0, t._)`{}` : f(e, n, r);
      }
      function f(e, t, n = {}) {
        let { gen: r, it: i } = e,
          a = [p(i, n), m(e, n)];
        return (h(e, t, a), r.object(...a));
      }
      function p({ errorPath: e }, { instancePath: i }) {
        let a = i ? (0, t.str)`${e}${(0, n.getErrorPath)(i, n.Type.Str)}` : e;
        return [
          r.default.instancePath,
          (0, t.strConcat)(r.default.instancePath, a),
        ];
      }
      function m(
        { keyword: e, it: { errSchemaPath: r } },
        { schemaPath: i, parentSchema: a },
      ) {
        let o = a ? r : (0, t.str)`${r}/${e}`;
        return (
          i && (o = (0, t.str)`${o}${(0, n.getErrorPath)(i, n.Type.Str)}`),
          [u.schemaPath, o]
        );
      }
      function h(e, { params: n, message: i }, a) {
        let { keyword: o, data: s, schemaValue: c, it: l } = e,
          { opts: d, propertyName: f, topSchemaRef: p, schemaPath: m } = l;
        (a.push(
          [u.keyword, o],
          [u.params, typeof n == `function` ? n(e) : n || (0, t._)`{}`],
        ),
          d.messages && a.push([u.message, typeof i == `function` ? i(e) : i]),
          d.verbose &&
            a.push(
              [u.schema, c],
              [u.parentSchema, (0, t._)`${p}${m}`],
              [r.default.data, s],
            ),
          f && a.push([u.propertyName, f]));
      }
    }),
    q = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.boolOrEmptySchema = e.topBoolOrEmptySchema = void 0));
      var t = zl(),
        n = W(),
        r = K(),
        i = { message: `boolean schema is false` };
      function a(e) {
        let { gen: t, schema: i, validateName: a } = e;
        i === !1
          ? s(e, !1)
          : typeof i == `object` && i.$async === !0
            ? t.return(r.default.data)
            : (t.assign((0, n._)`${a}.errors`, null), t.return(!0));
      }
      e.topBoolOrEmptySchema = a;
      function o(e, t) {
        let { gen: n, schema: r } = e;
        r === !1 ? (n.var(t, !1), s(e)) : n.var(t, !0);
      }
      e.boolOrEmptySchema = o;
      function s(e, n) {
        let { gen: r, data: a } = e,
          o = {
            gen: r,
            keyword: `false schema`,
            data: a,
            schema: !1,
            schemaCode: !1,
            schemaValue: !1,
            params: {},
            it: e,
          };
        (0, t.reportError)(o, i, void 0, n);
      }
    }),
    J = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.getRules = e.isJSONType = void 0));
      var t = new Set([
        `string`,
        `number`,
        `integer`,
        `boolean`,
        `null`,
        `object`,
        `array`,
      ]);
      function n(e) {
        return typeof e == `string` && t.has(e);
      }
      e.isJSONType = n;
      function r() {
        let e = {
          number: { type: `number`, rules: [] },
          string: { type: `string`, rules: [] },
          array: { type: `array`, rules: [] },
          object: { type: `object`, rules: [] },
        };
        return {
          types: { ...e, integer: !0, boolean: !0, null: !0 },
          rules: [{ rules: [] }, e.number, e.string, e.array, e.object],
          post: { rules: [] },
          all: {},
          keywords: {},
        };
      }
      e.getRules = r;
    }),
    Bl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.shouldUseRule =
          e.shouldUseGroup =
          e.schemaHasRulesForType =
            void 0));
      function t({ schema: e, self: t }, r) {
        let i = t.RULES.types[r];
        return i && i !== !0 && n(e, i);
      }
      e.schemaHasRulesForType = t;
      function n(e, t) {
        return t.rules.some((t) => r(e, t));
      }
      e.shouldUseGroup = n;
      function r(e, t) {
        return (
          e[t.keyword] !== void 0 ||
          t.definition.implements?.some((t) => e[t] !== void 0)
        );
      }
      e.shouldUseRule = r;
    }),
    Vl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.reportTypeError =
          e.checkDataTypes =
          e.checkDataType =
          e.coerceAndCheckDataType =
          e.getJSONTypes =
          e.getSchemaTypes =
          e.DataType =
            void 0));
      var t = J(),
        n = Bl(),
        r = zl(),
        i = W(),
        a = G(),
        o;
      (function (e) {
        ((e[(e.Correct = 0)] = `Correct`), (e[(e.Wrong = 1)] = `Wrong`));
      })(o || (e.DataType = o = {}));
      function s(e) {
        let t = c(e.type);
        if (t.includes(`null`)) {
          if (e.nullable === !1)
            throw Error(`type: null contradicts nullable: false`);
        } else {
          if (!t.length && e.nullable !== void 0)
            throw Error(`"nullable" cannot be used without "type"`);
          e.nullable === !0 && t.push(`null`);
        }
        return t;
      }
      e.getSchemaTypes = s;
      function c(e) {
        let n = Array.isArray(e) ? e : e ? [e] : [];
        if (n.every(t.isJSONType)) return n;
        throw Error(`type must be JSONType or JSONType[]: ` + n.join(`,`));
      }
      e.getJSONTypes = c;
      function l(e, t) {
        let { gen: r, data: i, opts: a } = e,
          s = d(t, a.coerceTypes),
          c =
            t.length > 0 &&
            !(
              s.length === 0 &&
              t.length === 1 &&
              (0, n.schemaHasRulesForType)(e, t[0])
            );
        if (c) {
          let n = h(t, i, a.strictNumbers, o.Wrong);
          r.if(n, () => {
            s.length ? f(e, t, s) : _(e);
          });
        }
        return c;
      }
      e.coerceAndCheckDataType = l;
      var u = new Set([`string`, `number`, `integer`, `boolean`, `null`]);
      function d(e, t) {
        return t
          ? e.filter((e) => u.has(e) || (t === `array` && e === `array`))
          : [];
      }
      function f(e, t, n) {
        let { gen: r, data: a, opts: o } = e,
          s = r.let(`dataType`, (0, i._)`typeof ${a}`),
          c = r.let(`coerced`, (0, i._)`undefined`);
        (o.coerceTypes === `array` &&
          r.if(
            (0,
            i._)`${s} == 'object' && Array.isArray(${a}) && ${a}.length == 1`,
            () =>
              r
                .assign(a, (0, i._)`${a}[0]`)
                .assign(s, (0, i._)`typeof ${a}`)
                .if(h(t, a, o.strictNumbers), () => r.assign(c, a)),
          ),
          r.if((0, i._)`${c} !== undefined`));
        for (let e of n)
          (u.has(e) || (e === `array` && o.coerceTypes === `array`)) && l(e);
        (r.else(),
          _(e),
          r.endIf(),
          r.if((0, i._)`${c} !== undefined`, () => {
            (r.assign(a, c), p(e, c));
          }));
        function l(e) {
          switch (e) {
            case `string`:
              r.elseIf((0, i._)`${s} == "number" || ${s} == "boolean"`)
                .assign(c, (0, i._)`"" + ${a}`)
                .elseIf((0, i._)`${a} === null`)
                .assign(c, (0, i._)`""`);
              return;
            case `number`:
              r.elseIf((0, i._)`${s} == "boolean" || ${a} === null
              || (${s} == "string" && ${a} && ${a} == +${a})`).assign(
                c,
                (0, i._)`+${a}`,
              );
              return;
            case `integer`:
              r.elseIf((0, i._)`${s} === "boolean" || ${a} === null
              || (${s} === "string" && ${a} && ${a} == +${a} && !(${a} % 1))`).assign(
                c,
                (0, i._)`+${a}`,
              );
              return;
            case `boolean`:
              r.elseIf(
                (0, i._)`${a} === "false" || ${a} === 0 || ${a} === null`,
              )
                .assign(c, !1)
                .elseIf((0, i._)`${a} === "true" || ${a} === 1`)
                .assign(c, !0);
              return;
            case `null`:
              (r.elseIf((0, i._)`${a} === "" || ${a} === 0 || ${a} === false`),
                r.assign(c, null));
              return;
            case `array`:
              r.elseIf((0, i._)`${s} === "string" || ${s} === "number"
              || ${s} === "boolean" || ${a} === null`).assign(
                c,
                (0, i._)`[${a}]`,
              );
          }
        }
      }
      function p({ gen: e, parentData: t, parentDataProperty: n }, r) {
        e.if((0, i._)`${t} !== undefined`, () =>
          e.assign((0, i._)`${t}[${n}]`, r),
        );
      }
      function m(e, t, n, r = o.Correct) {
        let a = r === o.Correct ? i.operators.EQ : i.operators.NEQ,
          s;
        switch (e) {
          case `null`:
            return (0, i._)`${t} ${a} null`;
          case `array`:
            s = (0, i._)`Array.isArray(${t})`;
            break;
          case `object`:
            s = (0,
            i._)`${t} && typeof ${t} == "object" && !Array.isArray(${t})`;
            break;
          case `integer`:
            s = c((0, i._)`!(${t} % 1) && !isNaN(${t})`);
            break;
          case `number`:
            s = c();
            break;
          default:
            return (0, i._)`typeof ${t} ${a} ${e}`;
        }
        return r === o.Correct ? s : (0, i.not)(s);
        function c(e = i.nil) {
          return (0, i.and)(
            (0, i._)`typeof ${t} == "number"`,
            e,
            n ? (0, i._)`isFinite(${t})` : i.nil,
          );
        }
      }
      e.checkDataType = m;
      function h(e, t, n, r) {
        if (e.length === 1) return m(e[0], t, n, r);
        let o,
          s = (0, a.toHash)(e);
        if (s.array && s.object) {
          let e = (0, i._)`typeof ${t} != "object"`;
          ((o = s.null ? e : (0, i._)`!${t} || ${e}`),
            delete s.null,
            delete s.array,
            delete s.object);
        } else o = i.nil;
        s.number && delete s.integer;
        for (let e in s) o = (0, i.and)(o, m(e, t, n, r));
        return o;
      }
      e.checkDataTypes = h;
      var g = {
        message: ({ schema: e }) => `must be ${e}`,
        params: ({ schema: e, schemaValue: t }) =>
          typeof e == `string`
            ? (0, i._)`{type: ${e}}`
            : (0, i._)`{type: ${t}}`,
      };
      function _(e) {
        let t = v(e);
        (0, r.reportError)(t, g);
      }
      e.reportTypeError = _;
      function v(e) {
        let { gen: t, data: n, schema: r } = e,
          i = (0, a.schemaRefOrVal)(e, r, `type`);
        return {
          gen: t,
          keyword: `type`,
          data: n,
          schema: r.type,
          schemaCode: i,
          schemaValue: i,
          parentSchema: r,
          params: {},
          it: e,
        };
      }
    }),
    Hl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.assignDefaults = void 0));
      var t = W(),
        n = G();
      function r(e, t) {
        let { properties: n, items: r } = e.schema;
        if (t === `object` && n) for (let t in n) i(e, t, n[t].default);
        else
          t === `array` &&
            Array.isArray(r) &&
            r.forEach((t, n) => i(e, n, t.default));
      }
      e.assignDefaults = r;
      function i(e, r, i) {
        let { gen: a, compositeRule: o, data: s, opts: c } = e;
        if (i === void 0) return;
        let l = (0, t._)`${s}${(0, t.getProperty)(r)}`;
        if (o) {
          (0, n.checkStrictMode)(e, `default is ignored for: ${l}`);
          return;
        }
        let u = (0, t._)`${l} === undefined`;
        (c.useDefaults === `empty` &&
          (u = (0, t._)`${u} || ${l} === null || ${l} === ""`),
          a.if(u, (0, t._)`${l} = ${(0, t.stringify)(i)}`));
      }
    }),
    Ul = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.validateUnion =
          e.validateArray =
          e.usePattern =
          e.callValidateCode =
          e.schemaProperties =
          e.allSchemaProperties =
          e.noPropertyInData =
          e.propertyInData =
          e.isOwnProperty =
          e.hasPropFunc =
          e.reportMissingProp =
          e.checkMissingProp =
          e.checkReportMissingProp =
            void 0));
      var t = W(),
        n = G(),
        r = K(),
        i = G();
      function a(e, n) {
        let { gen: r, data: i, it: a } = e;
        r.if(d(r, i, n, a.opts.ownProperties), () => {
          (e.setParams({ missingProperty: (0, t._)`${n}` }, !0), e.error());
        });
      }
      e.checkReportMissingProp = a;
      function o({ gen: e, data: n, it: { opts: r } }, i, a) {
        return (0, t.or)(
          ...i.map((i) =>
            (0, t.and)(d(e, n, i, r.ownProperties), (0, t._)`${a} = ${i}`),
          ),
        );
      }
      e.checkMissingProp = o;
      function s(e, t) {
        (e.setParams({ missingProperty: t }, !0), e.error());
      }
      e.reportMissingProp = s;
      function c(e) {
        return e.scopeValue(`func`, {
          ref: Object.prototype.hasOwnProperty,
          code: (0, t._)`Object.prototype.hasOwnProperty`,
        });
      }
      e.hasPropFunc = c;
      function l(e, n, r) {
        return (0, t._)`${c(e)}.call(${n}, ${r})`;
      }
      e.isOwnProperty = l;
      function u(e, n, r, i) {
        let a = (0, t._)`${n}${(0, t.getProperty)(r)} !== undefined`;
        return i ? (0, t._)`${a} && ${l(e, n, r)}` : a;
      }
      e.propertyInData = u;
      function d(e, n, r, i) {
        let a = (0, t._)`${n}${(0, t.getProperty)(r)} === undefined`;
        return i ? (0, t.or)(a, (0, t.not)(l(e, n, r))) : a;
      }
      e.noPropertyInData = d;
      function f(e) {
        return e ? Object.keys(e).filter((e) => e !== `__proto__`) : [];
      }
      e.allSchemaProperties = f;
      function p(e, t) {
        return f(t).filter((r) => !(0, n.alwaysValidSchema)(e, t[r]));
      }
      e.schemaProperties = p;
      function m(
        {
          schemaCode: e,
          data: n,
          it: { gen: i, topSchemaRef: a, schemaPath: o, errorPath: s },
          it: c,
        },
        l,
        u,
        d,
      ) {
        let f = d ? (0, t._)`${e}, ${n}, ${a}${o}` : n,
          p = [
            [
              r.default.instancePath,
              (0, t.strConcat)(r.default.instancePath, s),
            ],
            [r.default.parentData, c.parentData],
            [r.default.parentDataProperty, c.parentDataProperty],
            [r.default.rootData, r.default.rootData],
          ];
        c.opts.dynamicRef &&
          p.push([r.default.dynamicAnchors, r.default.dynamicAnchors]);
        let m = (0, t._)`${f}, ${i.object(...p)}`;
        return u === t.nil
          ? (0, t._)`${l}(${m})`
          : (0, t._)`${l}.call(${u}, ${m})`;
      }
      e.callValidateCode = m;
      var h = (0, t._)`new RegExp`;
      function g({ gen: e, it: { opts: n } }, r) {
        let a = n.unicodeRegExp ? `u` : ``,
          { regExp: o } = n.code,
          s = o(r, a);
        return e.scopeValue(`pattern`, {
          key: s.toString(),
          ref: s,
          code: (0,
          t._)`${o.code === `new RegExp` ? h : (0, i.useFunc)(e, o)}(${r}, ${a})`,
        });
      }
      e.usePattern = g;
      function _(e) {
        let { gen: r, data: i, keyword: a, it: o } = e,
          s = r.name(`valid`);
        if (o.allErrors) {
          let e = r.let(`valid`, !0);
          return (c(() => r.assign(e, !1)), e);
        }
        return (r.var(s, !0), c(() => r.break()), s);
        function c(o) {
          let c = r.const(`len`, (0, t._)`${i}.length`);
          r.forRange(`i`, 0, c, (i) => {
            (e.subschema(
              { keyword: a, dataProp: i, dataPropType: n.Type.Num },
              s,
            ),
              r.if((0, t.not)(s), o));
          });
        }
      }
      e.validateArray = _;
      function v(e) {
        let { gen: r, schema: i, keyword: a, it: o } = e;
        if (!Array.isArray(i)) throw Error(`ajv implementation error`);
        if (
          i.some((e) => (0, n.alwaysValidSchema)(o, e)) &&
          !o.opts.unevaluated
        )
          return;
        let s = r.let(`valid`, !1),
          c = r.name(`_valid`);
        (r.block(() =>
          i.forEach((n, i) => {
            let o = e.subschema(
              { keyword: a, schemaProp: i, compositeRule: !0 },
              c,
            );
            (r.assign(s, (0, t._)`${s} || ${c}`),
              e.mergeValidEvaluated(o, c) || r.if((0, t.not)(s)));
          }),
        ),
          e.result(
            s,
            () => e.reset(),
            () => e.error(!0),
          ));
      }
      e.validateUnion = v;
    }),
    Wl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.validateKeywordUsage =
          e.validSchemaType =
          e.funcKeywordCode =
          e.macroKeywordCode =
            void 0));
      var t = W(),
        n = K(),
        r = Ul(),
        i = zl();
      function a(e, n) {
        let { gen: r, keyword: i, schema: a, parentSchema: o, it: s } = e,
          c = n.macro.call(s.self, a, o, s),
          l = u(r, i, c);
        s.opts.validateSchema !== !1 && s.self.validateSchema(c, !0);
        let d = r.name(`valid`);
        (e.subschema(
          {
            schema: c,
            schemaPath: t.nil,
            errSchemaPath: `${s.errSchemaPath}/${i}`,
            topSchemaRef: l,
            compositeRule: !0,
          },
          d,
        ),
          e.pass(d, () => e.error(!0)));
      }
      e.macroKeywordCode = a;
      function o(e, i) {
        let {
          gen: a,
          keyword: o,
          schema: d,
          parentSchema: f,
          $data: p,
          it: m,
        } = e;
        l(m, i);
        let h = u(
            a,
            o,
            !p && i.compile ? i.compile.call(m.self, d, f, m) : i.validate,
          ),
          g = a.let(`valid`);
        (e.block$data(g, _), e.ok(i.valid ?? g));
        function _() {
          if (i.errors === !1) (b(), i.modifying && s(e), x(() => e.error()));
          else {
            let t = i.async ? v() : y();
            (i.modifying && s(e), x(() => c(e, t)));
          }
        }
        function v() {
          let e = a.let(`ruleErrs`, null);
          return (
            a.try(
              () => b((0, t._)`await `),
              (n) =>
                a.assign(g, !1).if(
                  (0, t._)`${n} instanceof ${m.ValidationError}`,
                  () => a.assign(e, (0, t._)`${n}.errors`),
                  () => a.throw(n),
                ),
            ),
            e
          );
        }
        function y() {
          let e = (0, t._)`${h}.errors`;
          return (a.assign(e, null), b(t.nil), e);
        }
        function b(o = i.async ? (0, t._)`await ` : t.nil) {
          let s = m.opts.passContext ? n.default.this : n.default.self,
            c = !((`compile` in i && !p) || i.schema === !1);
          a.assign(
            g,
            (0, t._)`${o}${(0, r.callValidateCode)(e, h, s, c)}`,
            i.modifying,
          );
        }
        function x(e) {
          a.if((0, t.not)(i.valid ?? g), e);
        }
      }
      e.funcKeywordCode = o;
      function s(e) {
        let { gen: n, data: r, it: i } = e;
        n.if(i.parentData, () =>
          n.assign(r, (0, t._)`${i.parentData}[${i.parentDataProperty}]`),
        );
      }
      function c(e, r) {
        let { gen: a } = e;
        a.if(
          (0, t._)`Array.isArray(${r})`,
          () => {
            (a
              .assign(
                n.default.vErrors,
                (0,
                t._)`${n.default.vErrors} === null ? ${r} : ${n.default.vErrors}.concat(${r})`,
              )
              .assign(n.default.errors, (0, t._)`${n.default.vErrors}.length`),
              (0, i.extendErrors)(e));
          },
          () => e.error(),
        );
      }
      function l({ schemaEnv: e }, t) {
        if (t.async && !e.$async) throw Error(`async keyword in sync schema`);
      }
      function u(e, n, r) {
        if (r === void 0) throw Error(`keyword "${n}" failed to compile`);
        return e.scopeValue(
          `keyword`,
          typeof r == `function`
            ? { ref: r }
            : { ref: r, code: (0, t.stringify)(r) },
        );
      }
      function d(e, t, n = !1) {
        return (
          !t.length ||
          t.some((t) =>
            t === `array`
              ? Array.isArray(e)
              : t === `object`
                ? e && typeof e == `object` && !Array.isArray(e)
                : typeof e == t || (n && e === void 0),
          )
        );
      }
      e.validSchemaType = d;
      function f({ schema: e, opts: t, self: n, errSchemaPath: r }, i, a) {
        if (Array.isArray(i.keyword) ? !i.keyword.includes(a) : i.keyword !== a)
          throw Error(`ajv implementation error`);
        let o = i.dependencies;
        if (o?.some((t) => !Object.prototype.hasOwnProperty.call(e, t)))
          throw Error(
            `parent schema must have dependencies of ${a}: ${o.join(`,`)}`,
          );
        if (i.validateSchema && !i.validateSchema(e[a])) {
          let e =
            `keyword "${a}" value is invalid at path "${r}": ` +
            n.errorsText(i.validateSchema.errors);
          if (t.validateSchema === `log`) n.logger.error(e);
          else throw Error(e);
        }
      }
      e.validateKeywordUsage = f;
    }),
    Gl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.extendSubschemaMode =
          e.extendSubschemaData =
          e.getSubschema =
            void 0));
      var t = W(),
        n = G();
      function r(
        e,
        {
          keyword: r,
          schemaProp: i,
          schema: a,
          schemaPath: o,
          errSchemaPath: s,
          topSchemaRef: c,
        },
      ) {
        if (r !== void 0 && a !== void 0)
          throw Error(`both "keyword" and "schema" passed, only one allowed`);
        if (r !== void 0) {
          let a = e.schema[r];
          return i === void 0
            ? {
                schema: a,
                schemaPath: (0, t._)`${e.schemaPath}${(0, t.getProperty)(r)}`,
                errSchemaPath: `${e.errSchemaPath}/${r}`,
              }
            : {
                schema: a[i],
                schemaPath: (0,
                t._)`${e.schemaPath}${(0, t.getProperty)(r)}${(0, t.getProperty)(i)}`,
                errSchemaPath: `${e.errSchemaPath}/${r}/${(0, n.escapeFragment)(i)}`,
              };
        }
        if (a !== void 0) {
          if (o === void 0 || s === void 0 || c === void 0)
            throw Error(
              `"schemaPath", "errSchemaPath" and "topSchemaRef" are required with "schema"`,
            );
          return {
            schema: a,
            schemaPath: o,
            topSchemaRef: c,
            errSchemaPath: s,
          };
        }
        throw Error(`either "keyword" or "schema" must be passed`);
      }
      e.getSubschema = r;
      function i(
        e,
        r,
        {
          dataProp: i,
          dataPropType: a,
          data: o,
          dataTypes: s,
          propertyName: c,
        },
      ) {
        if (o !== void 0 && i !== void 0)
          throw Error(`both "data" and "dataProp" passed, only one allowed`);
        let { gen: l } = r;
        if (i !== void 0) {
          let { errorPath: o, dataPathArr: s, opts: c } = r;
          (u(l.let(`data`, (0, t._)`${r.data}${(0, t.getProperty)(i)}`, !0)),
            (e.errorPath = (0,
            t.str)`${o}${(0, n.getErrorPath)(i, a, c.jsPropertySyntax)}`),
            (e.parentDataProperty = (0, t._)`${i}`),
            (e.dataPathArr = [...s, e.parentDataProperty]));
        }
        (o !== void 0 &&
          (u(o instanceof t.Name ? o : l.let(`data`, o, !0)),
          c !== void 0 && (e.propertyName = c)),
          s && (e.dataTypes = s));
        function u(t) {
          ((e.data = t),
            (e.dataLevel = r.dataLevel + 1),
            (e.dataTypes = []),
            (r.definedProperties = new Set()),
            (e.parentData = r.data),
            (e.dataNames = [...r.dataNames, t]));
        }
      }
      e.extendSubschemaData = i;
      function a(
        e,
        {
          jtdDiscriminator: t,
          jtdMetadata: n,
          compositeRule: r,
          createErrors: i,
          allErrors: a,
        },
      ) {
        (r !== void 0 && (e.compositeRule = r),
          i !== void 0 && (e.createErrors = i),
          a !== void 0 && (e.allErrors = a),
          (e.jtdDiscriminator = t),
          (e.jtdMetadata = n));
      }
      e.extendSubschemaMode = a;
    }),
    Kl = c((e, t) => {
      t.exports = function e(t, n) {
        if (t === n) return !0;
        if (t && n && typeof t == `object` && typeof n == `object`) {
          if (t.constructor !== n.constructor) return !1;
          var r, i, a;
          if (Array.isArray(t)) {
            if (((r = t.length), r != n.length)) return !1;
            for (i = r; i-- !== 0; ) if (!e(t[i], n[i])) return !1;
            return !0;
          }
          if (t.constructor === RegExp)
            return t.source === n.source && t.flags === n.flags;
          if (t.valueOf !== Object.prototype.valueOf)
            return t.valueOf() === n.valueOf();
          if (t.toString !== Object.prototype.toString)
            return t.toString() === n.toString();
          if (
            ((a = Object.keys(t)), (r = a.length), r !== Object.keys(n).length)
          )
            return !1;
          for (i = r; i-- !== 0; )
            if (!Object.prototype.hasOwnProperty.call(n, a[i])) return !1;
          for (i = r; i-- !== 0; ) {
            var o = a[i];
            if (!e(t[o], n[o])) return !1;
          }
          return !0;
        }
        return t !== t && n !== n;
      };
    }),
    ql = c((e, t) => {
      var n = (t.exports = function (e, t, n) {
        (typeof t == `function` && ((n = t), (t = {})), (n = t.cb || n));
        var i = typeof n == `function` ? n : n.pre || function () {},
          a = n.post || function () {};
        r(t, i, a, e, ``, e);
      });
      ((n.keywords = {
        additionalItems: !0,
        items: !0,
        contains: !0,
        additionalProperties: !0,
        propertyNames: !0,
        not: !0,
        if: !0,
        then: !0,
        else: !0,
      }),
        (n.arrayKeywords = { items: !0, allOf: !0, anyOf: !0, oneOf: !0 }),
        (n.propsKeywords = {
          $defs: !0,
          definitions: !0,
          properties: !0,
          patternProperties: !0,
          dependencies: !0,
        }),
        (n.skipKeywords = {
          default: !0,
          enum: !0,
          const: !0,
          required: !0,
          maximum: !0,
          minimum: !0,
          exclusiveMaximum: !0,
          exclusiveMinimum: !0,
          multipleOf: !0,
          maxLength: !0,
          minLength: !0,
          pattern: !0,
          format: !0,
          maxItems: !0,
          minItems: !0,
          uniqueItems: !0,
          maxProperties: !0,
          minProperties: !0,
        }));
      function r(e, t, a, o, s, c, l, u, d, f) {
        if (o && typeof o == `object` && !Array.isArray(o)) {
          for (var p in (t(o, s, c, l, u, d, f), o)) {
            var m = o[p];
            if (Array.isArray(m)) {
              if (p in n.arrayKeywords)
                for (var h = 0; h < m.length; h++)
                  r(e, t, a, m[h], s + `/` + p + `/` + h, c, s, p, o, h);
            } else if (p in n.propsKeywords) {
              if (m && typeof m == `object`)
                for (var g in m)
                  r(e, t, a, m[g], s + `/` + p + `/` + i(g), c, s, p, o, g);
            } else
              (p in n.keywords || (e.allKeys && !(p in n.skipKeywords))) &&
                r(e, t, a, m, s + `/` + p, c, s, p, o);
          }
          a(o, s, c, l, u, d, f);
        }
      }
      function i(e) {
        return e.replace(/~/g, `~0`).replace(/\//g, `~1`);
      }
    }),
    Jl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.getSchemaRefs =
          e.resolveUrl =
          e.normalizeId =
          e._getFullPath =
          e.getFullPath =
          e.inlineRef =
            void 0));
      var t = G(),
        n = Kl(),
        r = ql(),
        i = new Set([
          `type`,
          `format`,
          `pattern`,
          `maxLength`,
          `minLength`,
          `maxProperties`,
          `minProperties`,
          `maxItems`,
          `minItems`,
          `maximum`,
          `minimum`,
          `uniqueItems`,
          `multipleOf`,
          `required`,
          `enum`,
          `const`,
        ]);
      function a(e, t = !0) {
        return typeof e == `boolean`
          ? !0
          : t === !0
            ? !s(e)
            : t
              ? c(e) <= t
              : !1;
      }
      e.inlineRef = a;
      var o = new Set([
        `$ref`,
        `$recursiveRef`,
        `$recursiveAnchor`,
        `$dynamicRef`,
        `$dynamicAnchor`,
      ]);
      function s(e) {
        for (let t in e) {
          if (o.has(t)) return !0;
          let n = e[t];
          if ((Array.isArray(n) && n.some(s)) || (typeof n == `object` && s(n)))
            return !0;
        }
        return !1;
      }
      function c(e) {
        let n = 0;
        for (let r in e)
          if (
            r === `$ref` ||
            (n++,
            !i.has(r) &&
              (typeof e[r] == `object` &&
                (0, t.eachItem)(e[r], (e) => (n += c(e))),
              n === 1 / 0))
          )
            return 1 / 0;
        return n;
      }
      function l(e, t = ``, n) {
        return (n !== !1 && (t = f(t)), u(e, e.parse(t)));
      }
      e.getFullPath = l;
      function u(e, t) {
        return e.serialize(t).split(`#`)[0] + `#`;
      }
      e._getFullPath = u;
      var d = /#\/?$/;
      function f(e) {
        return e ? e.replace(d, ``) : ``;
      }
      e.normalizeId = f;
      function p(e, t, n) {
        return ((n = f(n)), e.resolve(t, n));
      }
      e.resolveUrl = p;
      var m = /^[a-z_][-a-z0-9._]*$/i;
      function h(e, t) {
        if (typeof e == `boolean`) return {};
        let { schemaId: i, uriResolver: a } = this.opts,
          o = f(e[i] || t),
          s = { "": o },
          c = l(a, o, !1),
          u = {},
          d = new Set();
        return (
          r(e, { allKeys: !0 }, (e, t, n, r) => {
            if (r === void 0) return;
            let a = c + t,
              o = s[r];
            (typeof e[i] == `string` && (o = l.call(this, e[i])),
              g.call(this, e.$anchor),
              g.call(this, e.$dynamicAnchor),
              (s[t] = o));
            function l(t) {
              let n = this.opts.uriResolver.resolve;
              if (((t = f(o ? n(o, t) : t)), d.has(t))) throw h(t);
              d.add(t);
              let r = this.refs[t];
              return (
                typeof r == `string` && (r = this.refs[r]),
                typeof r == `object`
                  ? p(e, r.schema, t)
                  : t !== f(a) &&
                    (t[0] === `#`
                      ? (p(e, u[t], t), (u[t] = e))
                      : (this.refs[t] = a)),
                t
              );
            }
            function g(e) {
              if (typeof e == `string`) {
                if (!m.test(e)) throw Error(`invalid anchor "${e}"`);
                l.call(this, `#${e}`);
              }
            }
          }),
          u
        );
        function p(e, t, r) {
          if (t !== void 0 && !n(e, t)) throw h(r);
        }
        function h(e) {
          return Error(`reference "${e}" resolves to more than one schema`);
        }
      }
      e.getSchemaRefs = h;
    }),
    Yl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.getData = e.KeywordCxt = e.validateFunctionCode = void 0));
      var t = q(),
        n = Vl(),
        r = Bl(),
        i = Vl(),
        a = Hl(),
        o = Wl(),
        s = Gl(),
        c = W(),
        l = K(),
        u = Jl(),
        d = G(),
        f = zl();
      function p(e) {
        if (ee(e) && (C(e), x(e))) {
          _(e);
          return;
        }
        m(e, () => (0, t.topBoolOrEmptySchema)(e));
      }
      e.validateFunctionCode = p;
      function m(
        { gen: e, validateName: t, schema: n, schemaEnv: r, opts: i },
        a,
      ) {
        i.code.es5
          ? e.func(
              t,
              (0, c._)`${l.default.data}, ${l.default.valCxt}`,
              r.$async,
              () => {
                (e.code((0, c._)`"use strict"; ${y(n, i)}`),
                  g(e, i),
                  e.code(a));
              },
            )
          : e.func(t, (0, c._)`${l.default.data}, ${h(i)}`, r.$async, () =>
              e.code(y(n, i)).code(a),
            );
      }
      function h(e) {
        return (0,
        c._)`{${l.default.instancePath}="", ${l.default.parentData}, ${l.default.parentDataProperty}, ${l.default.rootData}=${l.default.data}${e.dynamicRef ? (0, c._)`, ${l.default.dynamicAnchors}={}` : c.nil}}={}`;
      }
      function g(e, t) {
        e.if(
          l.default.valCxt,
          () => {
            (e.var(
              l.default.instancePath,
              (0, c._)`${l.default.valCxt}.${l.default.instancePath}`,
            ),
              e.var(
                l.default.parentData,
                (0, c._)`${l.default.valCxt}.${l.default.parentData}`,
              ),
              e.var(
                l.default.parentDataProperty,
                (0, c._)`${l.default.valCxt}.${l.default.parentDataProperty}`,
              ),
              e.var(
                l.default.rootData,
                (0, c._)`${l.default.valCxt}.${l.default.rootData}`,
              ),
              t.dynamicRef &&
                e.var(
                  l.default.dynamicAnchors,
                  (0, c._)`${l.default.valCxt}.${l.default.dynamicAnchors}`,
                ));
          },
          () => {
            (e.var(l.default.instancePath, (0, c._)`""`),
              e.var(l.default.parentData, (0, c._)`undefined`),
              e.var(l.default.parentDataProperty, (0, c._)`undefined`),
              e.var(l.default.rootData, l.default.data),
              t.dynamicRef && e.var(l.default.dynamicAnchors, (0, c._)`{}`));
          },
        );
      }
      function _(e) {
        let { schema: t, opts: n, gen: r } = e;
        m(e, () => {
          (n.$comment && t.$comment && ae(e),
            ne(e),
            r.let(l.default.vErrors, null),
            r.let(l.default.errors, 0),
            n.unevaluated && v(e),
            w(e),
            oe(e));
        });
      }
      function v(e) {
        let { gen: t, validateName: n } = e;
        ((e.evaluated = t.const(`evaluated`, (0, c._)`${n}.evaluated`)),
          t.if((0, c._)`${e.evaluated}.dynamicProps`, () =>
            t.assign((0, c._)`${e.evaluated}.props`, (0, c._)`undefined`),
          ),
          t.if((0, c._)`${e.evaluated}.dynamicItems`, () =>
            t.assign((0, c._)`${e.evaluated}.items`, (0, c._)`undefined`),
          ));
      }
      function y(e, t) {
        let n = typeof e == `object` && e[t.schemaId];
        return n && (t.code.source || t.code.process)
          ? (0, c._)`/*# sourceURL=${n} */`
          : c.nil;
      }
      function b(e, n) {
        if (ee(e) && (C(e), x(e))) {
          S(e, n);
          return;
        }
        (0, t.boolOrEmptySchema)(e, n);
      }
      function x({ schema: e, self: t }) {
        if (typeof e == `boolean`) return !e;
        for (let n in e) if (t.RULES.all[n]) return !0;
        return !1;
      }
      function ee(e) {
        return typeof e.schema != `boolean`;
      }
      function S(e, t) {
        let { schema: n, gen: r, opts: i } = e;
        (i.$comment && n.$comment && ae(e), re(e), ie(e));
        let a = r.const(`_errs`, l.default.errors);
        (w(e, a), r.var(t, (0, c._)`${a} === ${l.default.errors}`));
      }
      function C(e) {
        ((0, d.checkUnknownRules)(e), te(e));
      }
      function w(e, t) {
        if (e.opts.jtd) return ce(e, [], !1, t);
        let r = (0, n.getSchemaTypes)(e.schema);
        ce(e, r, !(0, n.coerceAndCheckDataType)(e, r), t);
      }
      function te(e) {
        let { schema: t, errSchemaPath: n, opts: r, self: i } = e;
        t.$ref &&
          r.ignoreKeywordsWithRef &&
          (0, d.schemaHasRulesButRef)(t, i.RULES) &&
          i.logger.warn(`$ref: keywords ignored in schema at path "${n}"`);
      }
      function ne(e) {
        let { schema: t, opts: n } = e;
        t.default !== void 0 &&
          n.useDefaults &&
          n.strictSchema &&
          (0, d.checkStrictMode)(e, `default is ignored in the schema root`);
      }
      function re(e) {
        let t = e.schema[e.opts.schemaId];
        t && (e.baseId = (0, u.resolveUrl)(e.opts.uriResolver, e.baseId, t));
      }
      function ie(e) {
        if (e.schema.$async && !e.schemaEnv.$async)
          throw Error(`async schema in sync schema`);
      }
      function ae({
        gen: e,
        schemaEnv: t,
        schema: n,
        errSchemaPath: r,
        opts: i,
      }) {
        let a = n.$comment;
        if (i.$comment === !0)
          e.code((0, c._)`${l.default.self}.logger.log(${a})`);
        else if (typeof i.$comment == `function`) {
          let n = (0, c.str)`${r}/$comment`,
            i = e.scopeValue(`root`, { ref: t.root });
          e.code(
            (0, c._)`${l.default.self}.opts.$comment(${a}, ${n}, ${i}.schema)`,
          );
        }
      }
      function oe(e) {
        let {
          gen: t,
          schemaEnv: n,
          validateName: r,
          ValidationError: i,
          opts: a,
        } = e;
        n.$async
          ? t.if(
              (0, c._)`${l.default.errors} === 0`,
              () => t.return(l.default.data),
              () => t.throw((0, c._)`new ${i}(${l.default.vErrors})`),
            )
          : (t.assign((0, c._)`${r}.errors`, l.default.vErrors),
            a.unevaluated && se(e),
            t.return((0, c._)`${l.default.errors} === 0`));
      }
      function se({ gen: e, evaluated: t, props: n, items: r }) {
        (n instanceof c.Name && e.assign((0, c._)`${t}.props`, n),
          r instanceof c.Name && e.assign((0, c._)`${t}.items`, r));
      }
      function ce(e, t, n, a) {
        let { gen: o, schema: s, data: u, allErrors: f, opts: p, self: m } = e,
          { RULES: h } = m;
        if (
          s.$ref &&
          (p.ignoreKeywordsWithRef || !(0, d.schemaHasRulesButRef)(s, h))
        ) {
          o.block(() => me(e, `$ref`, h.all.$ref.definition));
          return;
        }
        (p.jtd || T(e, t),
          o.block(() => {
            for (let e of h.rules) g(e);
            g(h.post);
          }));
        function g(d) {
          (0, r.shouldUseGroup)(s, d) &&
            (d.type
              ? (o.if((0, i.checkDataType)(d.type, u, p.strictNumbers)),
                le(e, d),
                t.length === 1 &&
                  t[0] === d.type &&
                  n &&
                  (o.else(), (0, i.reportTypeError)(e)),
                o.endIf())
              : le(e, d),
            f || o.if((0, c._)`${l.default.errors} === ${a || 0}`));
        }
      }
      function le(e, t) {
        let {
          gen: n,
          schema: i,
          opts: { useDefaults: o },
        } = e;
        (o && (0, a.assignDefaults)(e, t.type),
          n.block(() => {
            for (let n of t.rules)
              (0, r.shouldUseRule)(i, n) &&
                me(e, n.keyword, n.definition, t.type);
          }));
      }
      function T(e, t) {
        e.schemaEnv.meta ||
          !e.opts.strictTypes ||
          (E(e, t), e.opts.allowUnionTypes || D(e, t), ue(e, e.dataTypes));
      }
      function E(e, t) {
        if (t.length) {
          if (!e.dataTypes.length) {
            e.dataTypes = t;
            return;
          }
          (t.forEach((t) => {
            fe(e.dataTypes, t) ||
              O(
                e,
                `type "${t}" not allowed by context "${e.dataTypes.join(`,`)}"`,
              );
          }),
            pe(e, t));
        }
      }
      function D(e, t) {
        t.length > 1 &&
          !(t.length === 2 && t.includes(`null`)) &&
          O(e, `use allowUnionTypes to allow union type keyword`);
      }
      function ue(e, t) {
        let n = e.self.RULES.all;
        for (let i in n) {
          let a = n[i];
          if (typeof a == `object` && (0, r.shouldUseRule)(e.schema, a)) {
            let { type: n } = a.definition;
            n.length &&
              !n.some((e) => de(t, e)) &&
              O(e, `missing type "${n.join(`,`)}" for keyword "${i}"`);
          }
        }
      }
      function de(e, t) {
        return e.includes(t) || (t === `number` && e.includes(`integer`));
      }
      function fe(e, t) {
        return e.includes(t) || (t === `integer` && e.includes(`number`));
      }
      function pe(e, t) {
        let n = [];
        for (let r of e.dataTypes)
          fe(t, r)
            ? n.push(r)
            : t.includes(`integer`) && r === `number` && n.push(`integer`);
        e.dataTypes = n;
      }
      function O(e, t) {
        let n = e.schemaEnv.baseId + e.errSchemaPath;
        ((t += ` at "${n}" (strictTypes)`),
          (0, d.checkStrictMode)(e, t, e.opts.strictTypes));
      }
      var k = class {
        constructor(e, t, n) {
          if (
            ((0, o.validateKeywordUsage)(e, t, n),
            (this.gen = e.gen),
            (this.allErrors = e.allErrors),
            (this.keyword = n),
            (this.data = e.data),
            (this.schema = e.schema[n]),
            (this.$data =
              t.$data && e.opts.$data && this.schema && this.schema.$data),
            (this.schemaValue = (0, d.schemaRefOrVal)(
              e,
              this.schema,
              n,
              this.$data,
            )),
            (this.schemaType = t.schemaType),
            (this.parentSchema = e.schema),
            (this.params = {}),
            (this.it = e),
            (this.def = t),
            this.$data)
          )
            this.schemaCode = e.gen.const(`vSchema`, _e(this.$data, e));
          else if (
            ((this.schemaCode = this.schemaValue),
            !(0, o.validSchemaType)(
              this.schema,
              t.schemaType,
              t.allowUndefined,
            ))
          )
            throw Error(`${n} value must be ${JSON.stringify(t.schemaType)}`);
          (`code` in t ? t.trackErrors : t.errors !== !1) &&
            (this.errsCount = e.gen.const(`_errs`, l.default.errors));
        }
        result(e, t, n) {
          this.failResult((0, c.not)(e), t, n);
        }
        failResult(e, t, n) {
          (this.gen.if(e),
            n ? n() : this.error(),
            t
              ? (this.gen.else(), t(), this.allErrors && this.gen.endIf())
              : this.allErrors
                ? this.gen.endIf()
                : this.gen.else());
        }
        pass(e, t) {
          this.failResult((0, c.not)(e), void 0, t);
        }
        fail(e) {
          if (e === void 0) {
            (this.error(), this.allErrors || this.gen.if(!1));
            return;
          }
          (this.gen.if(e),
            this.error(),
            this.allErrors ? this.gen.endIf() : this.gen.else());
        }
        fail$data(e) {
          if (!this.$data) return this.fail(e);
          let { schemaCode: t } = this;
          this.fail(
            (0,
            c._)`${t} !== undefined && (${(0, c.or)(this.invalid$data(), e)})`,
          );
        }
        error(e, t, n) {
          if (t) {
            (this.setParams(t), this._error(e, n), this.setParams({}));
            return;
          }
          this._error(e, n);
        }
        _error(e, t) {
          (e ? f.reportExtraError : f.reportError)(this, this.def.error, t);
        }
        $dataError() {
          (0, f.reportError)(this, this.def.$dataError || f.keyword$DataError);
        }
        reset() {
          if (this.errsCount === void 0)
            throw Error(`add "trackErrors" to keyword definition`);
          (0, f.resetErrorsCount)(this.gen, this.errsCount);
        }
        ok(e) {
          this.allErrors || this.gen.if(e);
        }
        setParams(e, t) {
          t ? Object.assign(this.params, e) : (this.params = e);
        }
        block$data(e, t, n = c.nil) {
          this.gen.block(() => {
            (this.check$data(e, n), t());
          });
        }
        check$data(e = c.nil, t = c.nil) {
          if (!this.$data) return;
          let { gen: n, schemaCode: r, schemaType: i, def: a } = this;
          (n.if((0, c.or)((0, c._)`${r} === undefined`, t)),
            e !== c.nil && n.assign(e, !0),
            (i.length || a.validateSchema) &&
              (n.elseIf(this.invalid$data()),
              this.$dataError(),
              e !== c.nil && n.assign(e, !1)),
            n.else());
        }
        invalid$data() {
          let { gen: e, schemaCode: t, schemaType: n, def: r, it: a } = this;
          return (0, c.or)(o(), s());
          function o() {
            if (n.length) {
              if (!(t instanceof c.Name))
                throw Error(`ajv implementation error`);
              let e = Array.isArray(n) ? n : [n];
              return (0,
              c._)`${(0, i.checkDataTypes)(e, t, a.opts.strictNumbers, i.DataType.Wrong)}`;
            }
            return c.nil;
          }
          function s() {
            if (r.validateSchema) {
              let n = e.scopeValue(`validate$data`, { ref: r.validateSchema });
              return (0, c._)`!${n}(${t})`;
            }
            return c.nil;
          }
        }
        subschema(e, t) {
          let n = (0, s.getSubschema)(this.it, e);
          ((0, s.extendSubschemaData)(n, this.it, e),
            (0, s.extendSubschemaMode)(n, e));
          let r = { ...this.it, ...n, items: void 0, props: void 0 };
          return (b(r, t), r);
        }
        mergeEvaluated(e, t) {
          let { it: n, gen: r } = this;
          n.opts.unevaluated &&
            (n.props !== !0 &&
              e.props !== void 0 &&
              (n.props = d.mergeEvaluated.props(r, e.props, n.props, t)),
            n.items !== !0 &&
              e.items !== void 0 &&
              (n.items = d.mergeEvaluated.items(r, e.items, n.items, t)));
        }
        mergeValidEvaluated(e, t) {
          let { it: n, gen: r } = this;
          if (n.opts.unevaluated && (n.props !== !0 || n.items !== !0))
            return (r.if(t, () => this.mergeEvaluated(e, c.Name)), !0);
        }
      };
      e.KeywordCxt = k;
      function me(e, t, n, r) {
        let i = new k(e, n, t);
        `code` in n
          ? n.code(i, r)
          : i.$data && n.validate
            ? (0, o.funcKeywordCode)(i, n)
            : `macro` in n
              ? (0, o.macroKeywordCode)(i, n)
              : (n.compile || n.validate) && (0, o.funcKeywordCode)(i, n);
      }
      var he = /^\/(?:[^~]|~0|~1)*$/,
        ge = /^([0-9]+)(#|\/(?:[^~]|~0|~1)*)?$/;
      function _e(e, { dataLevel: t, dataNames: n, dataPathArr: r }) {
        let i, a;
        if (e === ``) return l.default.rootData;
        if (e[0] === `/`) {
          if (!he.test(e)) throw Error(`Invalid JSON-pointer: ${e}`);
          ((i = e), (a = l.default.rootData));
        } else {
          let o = ge.exec(e);
          if (!o) throw Error(`Invalid JSON-pointer: ${e}`);
          let s = +o[1];
          if (((i = o[2]), i === `#`)) {
            if (s >= t) throw Error(u(`property/index`, s));
            return r[t - s];
          }
          if (s > t) throw Error(u(`data`, s));
          if (((a = n[t - s]), !i)) return a;
        }
        let o = a,
          s = i.split(`/`);
        for (let e of s)
          e &&
            ((a = (0,
            c._)`${a}${(0, c.getProperty)((0, d.unescapeJsonPointer)(e))}`),
            (o = (0, c._)`${o} && ${a}`));
        return o;
        function u(e, n) {
          return `Cannot access ${e} ${n} levels up, current level is ${t}`;
        }
      }
      e.getData = _e;
    }),
    Xl = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.default = class extends Error {
          constructor(e) {
            (super(`validation failed`),
              (this.errors = e),
              (this.ajv = this.validation = !0));
          }
        }));
    }),
    Zl = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Jl();
      e.default = class extends Error {
        constructor(e, n, r, i) {
          (super(i || `can't resolve reference ${r} from id ${n}`),
            (this.missingRef = (0, t.resolveUrl)(e, n, r)),
            (this.missingSchema = (0, t.normalizeId)(
              (0, t.getFullPath)(e, this.missingRef),
            )));
        }
      };
    }),
    Ql = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.resolveSchema =
          e.getCompilingSchema =
          e.resolveRef =
          e.compileSchema =
          e.SchemaEnv =
            void 0));
      var t = W(),
        n = Xl(),
        r = K(),
        i = Jl(),
        a = G(),
        o = Yl(),
        s = class {
          constructor(e) {
            ((this.refs = {}), (this.dynamicAnchors = {}));
            let t;
            (typeof e.schema == `object` && (t = e.schema),
              (this.schema = e.schema),
              (this.schemaId = e.schemaId),
              (this.root = e.root || this),
              (this.baseId =
                e.baseId ?? (0, i.normalizeId)(t?.[e.schemaId || `$id`])),
              (this.schemaPath = e.schemaPath),
              (this.localRefs = e.localRefs),
              (this.meta = e.meta),
              (this.$async = t?.$async),
              (this.refs = {}));
          }
        };
      e.SchemaEnv = s;
      function c(e) {
        let a = d.call(this, e);
        if (a) return a;
        let s = (0, i.getFullPath)(this.opts.uriResolver, e.root.baseId),
          { es5: c, lines: l } = this.opts.code,
          { ownProperties: u } = this.opts,
          f = new t.CodeGen(this.scope, { es5: c, lines: l, ownProperties: u }),
          p;
        e.$async &&
          (p = f.scopeValue(`Error`, {
            ref: n.default,
            code: (0,
            t._)`require("ajv/dist/runtime/validation_error").default`,
          }));
        let m = f.scopeName(`validate`);
        e.validateName = m;
        let h = {
            gen: f,
            allErrors: this.opts.allErrors,
            data: r.default.data,
            parentData: r.default.parentData,
            parentDataProperty: r.default.parentDataProperty,
            dataNames: [r.default.data],
            dataPathArr: [t.nil],
            dataLevel: 0,
            dataTypes: [],
            definedProperties: new Set(),
            topSchemaRef: f.scopeValue(
              `schema`,
              this.opts.code.source === !0
                ? { ref: e.schema, code: (0, t.stringify)(e.schema) }
                : { ref: e.schema },
            ),
            validateName: m,
            ValidationError: p,
            schema: e.schema,
            schemaEnv: e,
            rootId: s,
            baseId: e.baseId || s,
            schemaPath: t.nil,
            errSchemaPath: e.schemaPath || (this.opts.jtd ? `` : `#`),
            errorPath: (0, t._)`""`,
            opts: this.opts,
            self: this,
          },
          g;
        try {
          (this._compilations.add(e),
            (0, o.validateFunctionCode)(h),
            f.optimize(this.opts.code.optimize));
          let n = f.toString();
          ((g = `${f.scopeRefs(r.default.scope)}return ${n}`),
            this.opts.code.process && (g = this.opts.code.process(g, e)));
          let i = Function(
            `${r.default.self}`,
            `${r.default.scope}`,
            g,
          )(this, this.scope.get());
          if (
            (this.scope.value(m, { ref: i }),
            (i.errors = null),
            (i.schema = e.schema),
            (i.schemaEnv = e),
            e.$async && (i.$async = !0),
            this.opts.code.source === !0 &&
              (i.source = {
                validateName: m,
                validateCode: n,
                scopeValues: f._values,
              }),
            this.opts.unevaluated)
          ) {
            let { props: e, items: n } = h;
            ((i.evaluated = {
              props: e instanceof t.Name ? void 0 : e,
              items: n instanceof t.Name ? void 0 : n,
              dynamicProps: e instanceof t.Name,
              dynamicItems: n instanceof t.Name,
            }),
              i.source && (i.source.evaluated = (0, t.stringify)(i.evaluated)));
          }
          return ((e.validate = i), e);
        } catch (t) {
          throw (
            delete e.validate,
            delete e.validateName,
            g && this.logger.error(`Error compiling schema, function code:`, g),
            t
          );
        } finally {
          this._compilations.delete(e);
        }
      }
      e.compileSchema = c;
      function l(e, t, n) {
        n = (0, i.resolveUrl)(this.opts.uriResolver, t, n);
        let r = e.refs[n];
        if (r) return r;
        let a = p.call(this, e, n);
        if (a === void 0) {
          let r = e.localRefs?.[n],
            { schemaId: i } = this.opts;
          r && (a = new s({ schema: r, schemaId: i, root: e, baseId: t }));
        }
        if (a !== void 0) return (e.refs[n] = u.call(this, a));
      }
      e.resolveRef = l;
      function u(e) {
        return (0, i.inlineRef)(e.schema, this.opts.inlineRefs)
          ? e.schema
          : e.validate
            ? e
            : c.call(this, e);
      }
      function d(e) {
        for (let t of this._compilations) if (f(t, e)) return t;
      }
      e.getCompilingSchema = d;
      function f(e, t) {
        return (
          e.schema === t.schema && e.root === t.root && e.baseId === t.baseId
        );
      }
      function p(e, t) {
        let n;
        for (; typeof (n = this.refs[t]) == `string`; ) t = n;
        return n || this.schemas[t] || m.call(this, e, t);
      }
      function m(e, t) {
        let n = this.opts.uriResolver.parse(t),
          r = (0, i._getFullPath)(this.opts.uriResolver, n),
          a = (0, i.getFullPath)(this.opts.uriResolver, e.baseId, void 0);
        if (Object.keys(e.schema).length > 0 && r === a)
          return g.call(this, n, e);
        let o = (0, i.normalizeId)(r),
          l = this.refs[o] || this.schemas[o];
        if (typeof l == `string`) {
          let t = m.call(this, e, l);
          return typeof t?.schema == `object` ? g.call(this, n, t) : void 0;
        }
        if (typeof l?.schema == `object`) {
          if ((l.validate || c.call(this, l), o === (0, i.normalizeId)(t))) {
            let { schema: t } = l,
              { schemaId: n } = this.opts,
              r = t[n];
            return (
              r && (a = (0, i.resolveUrl)(this.opts.uriResolver, a, r)),
              new s({ schema: t, schemaId: n, root: e, baseId: a })
            );
          }
          return g.call(this, n, l);
        }
      }
      e.resolveSchema = m;
      var h = new Set([
        `properties`,
        `patternProperties`,
        `enum`,
        `dependencies`,
        `definitions`,
      ]);
      function g(e, { baseId: t, schema: n, root: r }) {
        if (e.fragment?.[0] !== `/`) return;
        for (let r of e.fragment.slice(1).split(`/`)) {
          if (typeof n == `boolean`) return;
          let e = n[(0, a.unescapeFragment)(r)];
          if (e === void 0) return;
          n = e;
          let o = typeof n == `object` && n[this.opts.schemaId];
          !h.has(r) &&
            o &&
            (t = (0, i.resolveUrl)(this.opts.uriResolver, t, o));
        }
        let o;
        if (
          typeof n != `boolean` &&
          n.$ref &&
          !(0, a.schemaHasRulesButRef)(n, this.RULES)
        ) {
          let e = (0, i.resolveUrl)(this.opts.uriResolver, t, n.$ref);
          o = m.call(this, r, e);
        }
        let { schemaId: c } = this.opts;
        if (
          ((o ||= new s({ schema: n, schemaId: c, root: r, baseId: t })),
          o.schema !== o.root.schema)
        )
          return o;
      }
    }),
    $l = l({
      $id: () => eu,
      additionalProperties: () => !1,
      default: () => au,
      description: () => tu,
      properties: () => iu,
      required: () => ru,
      type: () => nu,
    }),
    eu,
    tu,
    nu,
    ru,
    iu,
    au,
    ou = s(() => {
      ((eu = `https://raw.githubusercontent.com/ajv-validator/ajv/master/lib/refs/data.json#`),
        (tu = `Meta-schema for $data reference (JSON AnySchema extension proposal)`),
        (nu = `object`),
        (ru = [`$data`]),
        (iu = {
          $data: {
            type: `string`,
            anyOf: [
              { format: `relative-json-pointer` },
              { format: `json-pointer` },
            ],
          },
        }),
        (au = {
          $id: eu,
          description: tu,
          type: nu,
          required: ru,
          properties: iu,
          additionalProperties: !1,
        }));
    }),
    su = c((e, t) => {
      var n = RegExp.prototype.test.bind(
          /^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$/iu,
        ),
        r = RegExp.prototype.test.bind(
          /^(?:(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]\d|\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]\d|\d)$/u,
        ),
        i = RegExp.prototype.test.bind(/^[\da-f]{2}$/iu),
        a = RegExp.prototype.test.bind(/^[\da-z\-._~]$/iu),
        o = RegExp.prototype.test.bind(/^[\da-z\-._~!$&'()*+,;=:@/]$/iu);
      function s(e) {
        let t = ``,
          n = 0,
          r = 0;
        for (r = 0; r < e.length; r++)
          if (((n = e[r].charCodeAt(0)), n !== 48)) {
            if (
              !(
                (n >= 48 && n <= 57) ||
                (n >= 65 && n <= 70) ||
                (n >= 97 && n <= 102)
              )
            )
              return ``;
            t += e[r];
            break;
          }
        for (r += 1; r < e.length; r++) {
          if (
            ((n = e[r].charCodeAt(0)),
            !(
              (n >= 48 && n <= 57) ||
              (n >= 65 && n <= 70) ||
              (n >= 97 && n <= 102)
            ))
          )
            return ``;
          t += e[r];
        }
        return t;
      }
      var c = RegExp.prototype.test.bind(/[^!"$&'()*+,\-.;=_`a-z{}~]/u);
      function l(e) {
        return ((e.length = 0), !0);
      }
      function u(e, t, n) {
        if (e.length) {
          let r = s(e);
          if (r !== ``) t.push(r);
          else return ((n.error = !0), !1);
          e.length = 0;
        }
        return !0;
      }
      function d(e) {
        let t = 0,
          n = { error: !1, address: ``, zone: `` },
          r = [],
          i = [],
          a = !1,
          o = !1,
          c = u;
        for (let s = 0; s < e.length; s++) {
          let u = e[s];
          if (!(u === `[` || u === `]`))
            if (u === `:`) {
              if ((a === !0 && (o = !0), !c(i, r, n))) break;
              if (++t > 7) {
                n.error = !0;
                break;
              }
              (s > 0 && e[s - 1] === `:` && (a = !0), r.push(`:`));
              continue;
            } else if (u === `%`) {
              if (!c(i, r, n)) break;
              c = l;
            } else {
              i.push(u);
              continue;
            }
        }
        return (
          i.length &&
            (c === l
              ? (n.zone = i.join(``))
              : o
                ? r.push(i.join(``))
                : r.push(s(i))),
          (n.address = r.join(``)),
          n
        );
      }
      function f(e) {
        if (p(e, `:`) < 2) return { host: e, isIPV6: !1 };
        let t = d(e);
        if (t.error) return { host: e, isIPV6: !1 };
        {
          let e = t.address,
            n = t.address;
          return (
            t.zone && ((e += `%` + t.zone), (n += `%25` + t.zone)),
            { host: e, isIPV6: !0, escapedHost: n }
          );
        }
      }
      function p(e, t) {
        let n = 0;
        for (let r = 0; r < e.length; r++) e[r] === t && n++;
        return n;
      }
      function m(e) {
        let t = e,
          n = [],
          r = -1,
          i = 0;
        for (; (i = t.length); ) {
          if (i === 1) {
            if (t === `.`) break;
            if (t === `/`) {
              n.push(`/`);
              break;
            } else {
              n.push(t);
              break;
            }
          } else if (i === 2) {
            if (t[0] === `.`) {
              if (t[1] === `.`) break;
              if (t[1] === `/`) {
                t = t.slice(2);
                continue;
              }
            } else if (t[0] === `/` && (t[1] === `.` || t[1] === `/`)) {
              n.push(`/`);
              break;
            }
          } else if (i === 3 && t === `/..`) {
            (n.length !== 0 && n.pop(), n.push(`/`));
            break;
          }
          if (t[0] === `.`) {
            if (t[1] === `.`) {
              if (t[2] === `/`) {
                t = t.slice(3);
                continue;
              }
            } else if (t[1] === `/`) {
              t = t.slice(2);
              continue;
            }
          } else if (t[0] === `/` && t[1] === `.`) {
            if (t[2] === `/`) {
              t = t.slice(2);
              continue;
            } else if (t[2] === `.` && t[3] === `/`) {
              ((t = t.slice(3)), n.length !== 0 && n.pop());
              continue;
            }
          }
          if ((r = t.indexOf(`/`, 1)) === -1) {
            n.push(t);
            break;
          } else (n.push(t.slice(0, r)), (t = t.slice(r)));
        }
        return n.join(``);
      }
      var h = { "@": `%40`, "/": `%2F`, "?": `%3F`, "#": `%23`, ":": `%3A` },
        g = /[@/?#:]/g,
        _ = /[@/?#]/g;
      function v(e, t) {
        let n = t ? _ : g;
        return ((n.lastIndex = 0), e.replace(n, (e) => h[e]));
      }
      function y(e, t = !1) {
        if (e.indexOf(`%`) === -1) return e;
        let n = ``;
        for (let r = 0; r < e.length; r++) {
          if (e[r] === `%` && r + 2 < e.length) {
            let o = e.slice(r + 1, r + 3);
            if (i(o)) {
              let e = o.toUpperCase(),
                i = String.fromCharCode(parseInt(e, 16));
              (t && a(i) ? (n += i) : (n += `%` + e), (r += 2));
              continue;
            }
          }
          n += e[r];
        }
        return n;
      }
      function b(e) {
        let t = ``;
        for (let n = 0; n < e.length; n++) {
          if (e[n] === `%` && n + 2 < e.length) {
            let r = e.slice(n + 1, n + 3);
            if (i(r)) {
              let e = r.toUpperCase(),
                i = String.fromCharCode(parseInt(e, 16));
              (i !== `.` && a(i) ? (t += i) : (t += `%` + e), (n += 2));
              continue;
            }
          }
          o(e[n]) ? (t += e[n]) : (t += escape(e[n]));
        }
        return t;
      }
      function x(e) {
        let t = ``;
        for (let n = 0; n < e.length; n++) {
          if (e[n] === `%` && n + 2 < e.length) {
            let r = e.slice(n + 1, n + 3);
            if (i(r)) {
              ((t += `%` + r.toUpperCase()), (n += 2));
              continue;
            }
          }
          t += escape(e[n]);
        }
        return t;
      }
      function ee(e) {
        let t = [];
        if (
          (e.userinfo !== void 0 && (t.push(e.userinfo), t.push(`@`)),
          e.host !== void 0)
        ) {
          let n = unescape(e.host);
          if (!r(n)) {
            let e = f(n);
            n = e.isIPV6 === !0 ? `[${e.escapedHost}]` : v(n, !1);
          }
          t.push(n);
        }
        return (
          (typeof e.port == `number` || typeof e.port == `string`) &&
            (t.push(`:`), t.push(String(e.port))),
          t.length ? t.join(``) : void 0
        );
      }
      t.exports = {
        nonSimpleDomain: c,
        recomposeAuthority: ee,
        reescapeHostDelimiters: v,
        normalizePercentEncoding: y,
        normalizePathEncoding: b,
        escapePreservingEscapes: x,
        removeDotSegments: m,
        isIPv4: r,
        isUUID: n,
        normalizeIPv6: f,
        stringArrayToHexStripped: s,
      };
    }),
    cu = c((e, t) => {
      var { isUUID: n } = su(),
        r = /([\da-z][\d\-a-z]{0,31}):((?:[\w!$'()*+,\-.:;=@]|%[\da-f]{2})+)/iu,
        i = [`http`, `https`, `ws`, `wss`, `urn`, `urn:uuid`];
      function a(e) {
        return i.indexOf(e) !== -1;
      }
      function o(e) {
        return e.secure === !0
          ? !0
          : e.secure === !1
            ? !1
            : e.scheme
              ? e.scheme.length === 3 &&
                (e.scheme[0] === `w` || e.scheme[0] === `W`) &&
                (e.scheme[1] === `s` || e.scheme[1] === `S`) &&
                (e.scheme[2] === `s` || e.scheme[2] === `S`)
              : !1;
      }
      function s(e) {
        return (
          e.host || (e.error = e.error || `HTTP URIs must have a host.`), e
        );
      }
      function c(e) {
        let t = String(e.scheme).toLowerCase() === `https`;
        return (
          (e.port === (t ? 443 : 80) || e.port === ``) && (e.port = void 0),
          (e.path ||= `/`),
          e
        );
      }
      function l(e) {
        return (
          (e.secure = o(e)),
          (e.resourceName = (e.path || `/`) + (e.query ? `?` + e.query : ``)),
          (e.path = void 0),
          (e.query = void 0),
          e
        );
      }
      function u(e) {
        if (
          ((e.port === (o(e) ? 443 : 80) || e.port === ``) && (e.port = void 0),
          typeof e.secure == `boolean` &&
            ((e.scheme = e.secure ? `wss` : `ws`), (e.secure = void 0)),
          e.resourceName)
        ) {
          let [t, n] = e.resourceName.split(`?`);
          ((e.path = t && t !== `/` ? t : void 0),
            (e.query = n),
            (e.resourceName = void 0));
        }
        return ((e.fragment = void 0), e);
      }
      function d(e, t) {
        if (!e.path) return ((e.error = `URN can not be parsed`), e);
        let n = e.path.match(r);
        if (n) {
          let r = t.scheme || e.scheme || `urn`;
          ((e.nid = n[1].toLowerCase()), (e.nss = n[2]));
          let i = y(`${r}:${t.nid || e.nid}`);
          ((e.path = void 0), i && (e = i.parse(e, t)));
        } else e.error = e.error || `URN can not be parsed.`;
        return e;
      }
      function f(e, t) {
        if (e.nid === void 0)
          throw Error(`URN without nid cannot be serialized`);
        let n = t.scheme || e.scheme || `urn`,
          r = e.nid.toLowerCase(),
          i = y(`${n}:${t.nid || r}`);
        i && (e = i.serialize(e, t));
        let a = e,
          o = e.nss;
        return ((a.path = `${r || t.nid}:${o}`), (t.skipEscape = !0), a);
      }
      function p(e, t) {
        let r = e;
        return (
          (r.uuid = r.nss),
          (r.nss = void 0),
          !t.tolerant &&
            (!r.uuid || !n(r.uuid)) &&
            (r.error = r.error || `UUID is not valid.`),
          r
        );
      }
      function m(e) {
        let t = e;
        return ((t.nss = (e.uuid || ``).toLowerCase()), t);
      }
      var h = { scheme: `http`, domainHost: !0, parse: s, serialize: c },
        g = {
          scheme: `https`,
          domainHost: h.domainHost,
          parse: s,
          serialize: c,
        },
        _ = { scheme: `ws`, domainHost: !0, parse: l, serialize: u },
        v = {
          http: h,
          https: g,
          ws: _,
          wss: {
            scheme: `wss`,
            domainHost: _.domainHost,
            parse: _.parse,
            serialize: _.serialize,
          },
          urn: { scheme: `urn`, parse: d, serialize: f, skipNormalize: !0 },
          "urn:uuid": {
            scheme: `urn:uuid`,
            parse: p,
            serialize: m,
            skipNormalize: !0,
          },
        };
      Object.setPrototypeOf(v, null);
      function y(e) {
        return (e && (v[e] || v[e.toLowerCase()])) || void 0;
      }
      t.exports = {
        wsIsSecure: o,
        SCHEMES: v,
        isValidSchemeName: a,
        getSchemeHandler: y,
      };
    }),
    lu = c((e, t) => {
      var {
          normalizeIPv6: n,
          removeDotSegments: r,
          recomposeAuthority: i,
          normalizePercentEncoding: a,
          normalizePathEncoding: o,
          escapePreservingEscapes: s,
          reescapeHostDelimiters: c,
          isIPv4: l,
          nonSimpleDomain: u,
        } = su(),
        { SCHEMES: d, getSchemeHandler: f } = cu();
      function p(e, t) {
        return (
          typeof e == `string`
            ? (e = C(e, t))
            : typeof e == `object` && (e = S(_(e, t), t)),
          e
        );
      }
      function m(e, t, n) {
        let r = n ? Object.assign({ scheme: `null` }, n) : { scheme: `null` },
          { parsed: i, malformedAuthorityOrPort: a } = ee(e, r),
          { parsed: o, malformedAuthorityOrPort: s } = ee(t, r);
        if (a || s) throw Error(i.error || o.error || `URI is malformed.`);
        let c = h(i, o, r, !0);
        return ((r.skipEscape = !0), _(c, r));
      }
      function h(e, t, n, i) {
        let a = {};
        return (
          i || ((e = S(_(e, n), n)), (t = S(_(t, n), n))),
          (n ||= {}),
          !n.tolerant && t.scheme
            ? ((a.scheme = t.scheme),
              (a.userinfo = t.userinfo),
              (a.host = t.host),
              (a.port = t.port),
              (a.path = r(t.path || ``)),
              (a.query = t.query))
            : (t.userinfo !== void 0 || t.host !== void 0 || t.port !== void 0
                ? ((a.userinfo = t.userinfo),
                  (a.host = t.host),
                  (a.port = t.port),
                  (a.path = r(t.path || ``)),
                  (a.query = t.query))
                : (t.path
                    ? (t.path[0] === `/`
                        ? (a.path = r(t.path))
                        : ((e.userinfo !== void 0 ||
                            e.host !== void 0 ||
                            e.port !== void 0) &&
                          !e.path
                            ? (a.path = `/` + t.path)
                            : e.path
                              ? (a.path =
                                  e.path.slice(0, e.path.lastIndexOf(`/`) + 1) +
                                  t.path)
                              : (a.path = t.path),
                          (a.path = r(a.path))),
                      (a.query = t.query))
                    : ((a.path = e.path),
                      t.query === void 0
                        ? (a.query = e.query)
                        : (a.query = t.query)),
                  (a.userinfo = e.userinfo),
                  (a.host = e.host),
                  (a.port = e.port)),
              (a.scheme = e.scheme)),
          (a.fragment = t.fragment),
          a
        );
      }
      function g(e, t, n) {
        let r = te(e, n),
          i = te(t, n);
        return (
          r !== void 0 && i !== void 0 && r.toLowerCase() === i.toLowerCase()
        );
      }
      function _(e, t) {
        let n = {
            host: e.host,
            scheme: e.scheme,
            userinfo: e.userinfo,
            port: e.port,
            path: e.path,
            query: e.query,
            nid: e.nid,
            nss: e.nss,
            uuid: e.uuid,
            fragment: e.fragment,
            reference: e.reference,
            resourceName: e.resourceName,
            secure: e.secure,
            error: ``,
          },
          o = Object.assign({}, t),
          c = [],
          l = f(o.scheme || n.scheme);
        (l && l.serialize && l.serialize(n, o),
          n.path !== void 0 &&
            (o.skipEscape
              ? (n.path = a(n.path))
              : ((n.path = s(n.path)),
                n.scheme !== void 0 &&
                  (n.path = n.path.split(`%3A`).join(`:`)))),
          o.reference !== `suffix` && n.scheme && c.push(n.scheme, `:`));
        let u = i(n);
        if (
          (u !== void 0 &&
            (o.reference !== `suffix` && c.push(`//`),
            c.push(u),
            n.path && n.path[0] !== `/` && c.push(`/`)),
          n.path !== void 0)
        ) {
          let e = n.path;
          (!o.absolutePath && (!l || !l.absolutePath) && (e = r(e)),
            u === void 0 &&
              e[0] === `/` &&
              e[1] === `/` &&
              (e = `/%2F` + e.slice(2)),
            c.push(e));
        }
        return (
          n.query !== void 0 && c.push(`?`, n.query),
          n.fragment !== void 0 && c.push(`#`, n.fragment),
          c.join(``)
        );
      }
      var v =
          /^(?:([^#/:?]+):)?(?:\/\/((?:([^#/?@]*)@)?(\[[^#/?\]]+\]|[^#/:?]*)(?::(\d*))?))?([^#?]*)(?:\?([^#]*))?(?:#((?:.|[\n\r])*))?/u,
        y = /^(?:[^#/:?]+:)?\/\/([^/?#]*)/,
        b = /^(?:[^#/:?]+:)?([/\\\t\n\r]*)/;
      function x(e, t) {
        if (t[2] !== void 0 && e.path && e.path[0] !== `/`)
          return `URI path must start with "/" when authority is present.`;
        if (typeof e.port == `number` && (e.port < 0 || e.port > 65535))
          return `URI port is malformed.`;
      }
      function ee(e, t) {
        let r = Object.assign({}, t),
          i = {
            scheme: void 0,
            userinfo: void 0,
            host: ``,
            port: void 0,
            path: ``,
            query: void 0,
            fragment: void 0,
          },
          a = !1,
          s = !1;
        r.reference === `suffix` &&
          (e = r.scheme ? r.scheme + `:` + e : `//` + e);
        let d = e.match(y);
        d !== null &&
          d[1].indexOf(`\\`) !== -1 &&
          ((i.error = `URI authority must not contain a literal backslash.`),
          (a = !0));
        let p = e.match(b);
        if (p !== null) {
          let e = p[1],
            t = e.replace(/[\t\n\r]/g, ``);
          t.length >= 2 &&
            (t.slice(0, 2) === `//`
              ? e.length !== t.length &&
                ((i.error =
                  i.error ||
                  `URI authority introducer must not contain whitespace.`),
                (a = !0))
              : ((i.error =
                  i.error ||
                  `URI authority must not contain a literal backslash.`),
                (a = !0)));
        }
        let m = e.match(v);
        if (m) {
          ((i.scheme = m[1]),
            (i.userinfo = m[3]),
            (i.host = m[4]),
            (i.port = parseInt(m[5], 10)),
            (i.path = m[6] || ``),
            (i.query = m[7]),
            (i.fragment = m[8]),
            isNaN(i.port) && (i.port = m[5]));
          let t = x(i, m);
          if ((t !== void 0 && ((i.error = i.error || t), (a = !0)), i.host))
            if (l(i.host) === !1) {
              let e = n(i.host);
              ((i.host = e.host.toLowerCase()), (s = e.isIPV6));
            } else s = !0;
          (i.scheme === void 0 &&
          i.userinfo === void 0 &&
          i.host === void 0 &&
          i.port === void 0 &&
          i.query === void 0 &&
          !i.path
            ? (i.reference = `same-document`)
            : i.scheme === void 0
              ? (i.reference = `relative`)
              : i.fragment === void 0
                ? (i.reference = `absolute`)
                : (i.reference = `uri`),
            r.reference &&
              r.reference !== `suffix` &&
              r.reference !== i.reference &&
              (i.error =
                i.error || `URI is not a ` + r.reference + ` reference.`));
          let d = f(r.scheme || i.scheme);
          if (
            !r.unicodeSupport &&
            (!d || !d.unicodeSupport) &&
            i.host &&
            (r.domainHost || (d && d.domainHost)) &&
            s === !1 &&
            u(i.host)
          )
            try {
              i.host = new URL(`http://` + i.host).hostname;
            } catch (e) {
              i.error =
                i.error ||
                `Host's domain name can not be converted to ASCII: ` + e;
            }
          if (
            (!d || (d && !d.skipNormalize)) &&
            (e.indexOf(`%`) !== -1 &&
              (i.scheme !== void 0 && (i.scheme = unescape(i.scheme)),
              i.host !== void 0 && (i.host = c(unescape(i.host), s))),
            (i.path &&= o(i.path)),
            i.fragment)
          )
            try {
              i.fragment = encodeURI(decodeURIComponent(i.fragment));
            } catch {
              i.error = i.error || `URI malformed`;
            }
          d && d.parse && d.parse(i, r);
        } else i.error = i.error || `URI can not be parsed.`;
        return { parsed: i, malformedAuthorityOrPort: a };
      }
      function S(e, t) {
        return ee(e, t).parsed;
      }
      function C(e, t) {
        return w(e, t).normalized;
      }
      function w(e, t) {
        let { parsed: n, malformedAuthorityOrPort: r } = ee(e, t);
        return { normalized: r ? e : _(n, t), malformedAuthorityOrPort: r };
      }
      function te(e, t) {
        if (typeof e == `string`) {
          let { normalized: n, malformedAuthorityOrPort: r } = w(e, t);
          return r ? void 0 : n;
        }
        if (typeof e == `object`) return _(e, t);
      }
      var ne = {
        SCHEMES: d,
        normalize: p,
        resolve: m,
        resolveComponent: h,
        equal: g,
        serialize: _,
        parse: S,
      };
      ((t.exports = ne), (t.exports.default = ne), (t.exports.fastUri = ne));
    }),
    uu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = lu();
      ((t.code = `require("ajv/dist/runtime/uri").default`), (e.default = t));
    }),
    du = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.CodeGen =
          e.Name =
          e.nil =
          e.stringify =
          e.str =
          e._ =
          e.KeywordCxt =
            void 0));
      var t = Yl();
      Object.defineProperty(e, "KeywordCxt", {
        enumerable: !0,
        get: function () {
          return t.KeywordCxt;
        },
      });
      var n = W();
      (Object.defineProperty(e, "_", {
        enumerable: !0,
        get: function () {
          return n._;
        },
      }),
        Object.defineProperty(e, "str", {
          enumerable: !0,
          get: function () {
            return n.str;
          },
        }),
        Object.defineProperty(e, "stringify", {
          enumerable: !0,
          get: function () {
            return n.stringify;
          },
        }),
        Object.defineProperty(e, "nil", {
          enumerable: !0,
          get: function () {
            return n.nil;
          },
        }),
        Object.defineProperty(e, "Name", {
          enumerable: !0,
          get: function () {
            return n.Name;
          },
        }),
        Object.defineProperty(e, "CodeGen", {
          enumerable: !0,
          get: function () {
            return n.CodeGen;
          },
        }));
      var r = Xl(),
        i = Zl(),
        a = J(),
        o = Ql(),
        s = W(),
        c = Jl(),
        l = Vl(),
        u = G(),
        d = (ou(), f($l).default),
        p = uu(),
        m = (e, t) => new RegExp(e, t);
      m.code = `new RegExp`;
      var h = [`removeAdditional`, `useDefaults`, `coerceTypes`],
        g = new Set([
          `validate`,
          `serialize`,
          `parse`,
          `wrapper`,
          `root`,
          `schema`,
          `keyword`,
          `pattern`,
          `formats`,
          `validate$data`,
          `func`,
          `obj`,
          `Error`,
        ]),
        _ = {
          errorDataPath: ``,
          format: "`validateFormats: false` can be used instead.",
          nullable: `"nullable" keyword is supported by default.`,
          jsonPointers: `Deprecated jsPropertySyntax can be used instead.`,
          extendRefs: `Deprecated ignoreKeywordsWithRef can be used instead.`,
          missingRefs: `Pass empty schema with $id that should be ignored to ajv.addSchema.`,
          processCode:
            "Use option `code: {process: (code, schemaEnv: object) => string}`",
          sourceCode: "Use option `code: {source: true}`",
          strictDefaults: "It is default now, see option `strict`.",
          strictKeywords: "It is default now, see option `strict`.",
          uniqueItems: `"uniqueItems" keyword is always validated.`,
          unknownFormats:
            "Disable strict mode or pass `true` to `ajv.addFormat` (or `formats` option).",
          cache: `Map is used as cache, schema object as key.`,
          serialize: `Map is used as cache, schema object as key.`,
          ajvErrors: `It is default now.`,
        },
        v = {
          ignoreKeywordsWithRef: ``,
          jsPropertySyntax: ``,
          unicode: `"minLength"/"maxLength" account for unicode characters by default.`,
        },
        y = 200;
      function b(e) {
        let t = e.strict,
          n = e.code?.optimize,
          r = n === !0 || n === void 0 ? 1 : n || 0,
          i = e.code?.regExp ?? m,
          a = e.uriResolver ?? p.default;
        return {
          strictSchema: e.strictSchema ?? t ?? !0,
          strictNumbers: e.strictNumbers ?? t ?? !0,
          strictTypes: e.strictTypes ?? t ?? `log`,
          strictTuples: e.strictTuples ?? t ?? `log`,
          strictRequired: e.strictRequired ?? t ?? !1,
          code: e.code
            ? { ...e.code, optimize: r, regExp: i }
            : { optimize: r, regExp: i },
          loopRequired: e.loopRequired ?? y,
          loopEnum: e.loopEnum ?? y,
          meta: e.meta ?? !0,
          messages: e.messages ?? !0,
          inlineRefs: e.inlineRefs ?? !0,
          schemaId: e.schemaId ?? `$id`,
          addUsedSchema: e.addUsedSchema ?? !0,
          validateSchema: e.validateSchema ?? !0,
          validateFormats: e.validateFormats ?? !0,
          unicodeRegExp: e.unicodeRegExp ?? !0,
          int32range: e.int32range ?? !0,
          uriResolver: a,
        };
      }
      var x = class {
        constructor(e = {}) {
          ((this.schemas = {}),
            (this.refs = {}),
            (this.formats = Object.create(null)),
            (this._compilations = new Set()),
            (this._loading = {}),
            (this._cache = new Map()),
            (e = this.opts = { ...e, ...b(e) }));
          let { es5: t, lines: n } = this.opts.code;
          ((this.scope = new s.ValueScope({
            scope: {},
            prefixes: g,
            es5: t,
            lines: n,
          })),
            (this.logger = ie(e.logger)));
          let r = e.validateFormats;
          ((e.validateFormats = !1),
            (this.RULES = (0, a.getRules)()),
            ee.call(this, _, e, `NOT SUPPORTED`),
            ee.call(this, v, e, `DEPRECATED`, `warn`),
            (this._metaOpts = ne.call(this)),
            e.formats && w.call(this),
            this._addVocabularies(),
            this._addDefaultMetaSchema(),
            e.keywords && te.call(this, e.keywords),
            typeof e.meta == `object` && this.addMetaSchema(e.meta),
            C.call(this),
            (e.validateFormats = r));
        }
        _addVocabularies() {
          this.addKeyword(`$async`);
        }
        _addDefaultMetaSchema() {
          let { $data: e, meta: t, schemaId: n } = this.opts,
            r = d;
          (n === `id` && ((r = { ...d }), (r.id = r.$id), delete r.$id),
            t && e && this.addMetaSchema(r, r[n], !1));
        }
        defaultMeta() {
          let { meta: e, schemaId: t } = this.opts;
          return (this.opts.defaultMeta =
            typeof e == `object` ? e[t] || e : void 0);
        }
        validate(e, t) {
          let n;
          if (typeof e == `string`) {
            if (((n = this.getSchema(e)), !n))
              throw Error(`no schema with key or ref "${e}"`);
          } else n = this.compile(e);
          let r = n(t);
          return (`$async` in n || (this.errors = n.errors), r);
        }
        compile(e, t) {
          let n = this._addSchema(e, t);
          return n.validate || this._compileSchemaEnv(n);
        }
        compileAsync(e, t) {
          if (typeof this.opts.loadSchema != `function`)
            throw Error(`options.loadSchema should be a function`);
          let { loadSchema: n } = this.opts;
          return r.call(this, e, t);
          async function r(e, t) {
            await a.call(this, e.$schema);
            let n = this._addSchema(e, t);
            return n.validate || o.call(this, n);
          }
          async function a(e) {
            e && !this.getSchema(e) && (await r.call(this, { $ref: e }, !0));
          }
          async function o(e) {
            try {
              return this._compileSchemaEnv(e);
            } catch (t) {
              if (!(t instanceof i.default)) throw t;
              return (
                s.call(this, t),
                await c.call(this, t.missingSchema),
                o.call(this, e)
              );
            }
          }
          function s({ missingSchema: e, missingRef: t }) {
            if (this.refs[e])
              throw Error(
                `AnySchema ${e} is loaded but ${t} cannot be resolved`,
              );
          }
          async function c(e) {
            let n = await l.call(this, e);
            (this.refs[e] || (await a.call(this, n.$schema)),
              this.refs[e] || this.addSchema(n, e, t));
          }
          async function l(e) {
            let t = this._loading[e];
            if (t) return t;
            try {
              return await (this._loading[e] = n(e));
            } finally {
              delete this._loading[e];
            }
          }
        }
        addSchema(e, t, n, r = this.opts.validateSchema) {
          if (Array.isArray(e)) {
            for (let t of e) this.addSchema(t, void 0, n, r);
            return this;
          }
          let i;
          if (typeof e == `object`) {
            let { schemaId: t } = this.opts;
            if (((i = e[t]), i !== void 0 && typeof i != `string`))
              throw Error(`schema ${t} must be string`);
          }
          return (
            (t = (0, c.normalizeId)(t || i)),
            this._checkUnique(t),
            (this.schemas[t] = this._addSchema(e, n, t, r, !0)),
            this
          );
        }
        addMetaSchema(e, t, n = this.opts.validateSchema) {
          return (this.addSchema(e, t, !0, n), this);
        }
        validateSchema(e, t) {
          if (typeof e == `boolean`) return !0;
          let n;
          if (((n = e.$schema), n !== void 0 && typeof n != `string`))
            throw Error(`$schema must be a string`);
          if (((n = n || this.opts.defaultMeta || this.defaultMeta()), !n))
            return (
              this.logger.warn(`meta-schema not available`),
              (this.errors = null),
              !0
            );
          let r = this.validate(n, e);
          if (!r && t) {
            let e = `schema is invalid: ` + this.errorsText();
            if (this.opts.validateSchema === `log`) this.logger.error(e);
            else throw Error(e);
          }
          return r;
        }
        getSchema(e) {
          let t;
          for (; typeof (t = S.call(this, e)) == `string`; ) e = t;
          if (t === void 0) {
            let { schemaId: n } = this.opts,
              r = new o.SchemaEnv({ schema: {}, schemaId: n });
            if (((t = o.resolveSchema.call(this, r, e)), !t)) return;
            this.refs[e] = t;
          }
          return t.validate || this._compileSchemaEnv(t);
        }
        removeSchema(e) {
          if (e instanceof RegExp)
            return (
              this._removeAllSchemas(this.schemas, e),
              this._removeAllSchemas(this.refs, e),
              this
            );
          switch (typeof e) {
            case `undefined`:
              return (
                this._removeAllSchemas(this.schemas),
                this._removeAllSchemas(this.refs),
                this._cache.clear(),
                this
              );
            case `string`: {
              let t = S.call(this, e);
              return (
                typeof t == `object` && this._cache.delete(t.schema),
                delete this.schemas[e],
                delete this.refs[e],
                this
              );
            }
            case `object`: {
              let t = e;
              this._cache.delete(t);
              let n = e[this.opts.schemaId];
              return (
                n &&
                  ((n = (0, c.normalizeId)(n)),
                  delete this.schemas[n],
                  delete this.refs[n]),
                this
              );
            }
            default:
              throw Error(`ajv.removeSchema: invalid parameter`);
          }
        }
        addVocabulary(e) {
          for (let t of e) this.addKeyword(t);
          return this;
        }
        addKeyword(e, t) {
          let n;
          if (typeof e == `string`)
            ((n = e),
              typeof t == `object` &&
                (this.logger.warn(
                  `these parameters are deprecated, see docs for addKeyword`,
                ),
                (t.keyword = n)));
          else if (typeof e == `object` && t === void 0) {
            if (((t = e), (n = t.keyword), Array.isArray(n) && !n.length))
              throw Error(
                `addKeywords: keyword must be string or non-empty array`,
              );
          } else throw Error(`invalid addKeywords parameters`);
          if ((oe.call(this, n, t), !t))
            return ((0, u.eachItem)(n, (e) => se.call(this, e)), this);
          le.call(this, t);
          let r = {
            ...t,
            type: (0, l.getJSONTypes)(t.type),
            schemaType: (0, l.getJSONTypes)(t.schemaType),
          };
          return (
            (0, u.eachItem)(
              n,
              r.type.length === 0
                ? (e) => se.call(this, e, r)
                : (e) => r.type.forEach((t) => se.call(this, e, r, t)),
            ),
            this
          );
        }
        getKeyword(e) {
          let t = this.RULES.all[e];
          return typeof t == `object` ? t.definition : !!t;
        }
        removeKeyword(e) {
          let { RULES: t } = this;
          (delete t.keywords[e], delete t.all[e]);
          for (let n of t.rules) {
            let t = n.rules.findIndex((t) => t.keyword === e);
            t >= 0 && n.rules.splice(t, 1);
          }
          return this;
        }
        addFormat(e, t) {
          return (
            typeof t == `string` && (t = new RegExp(t)),
            (this.formats[e] = t),
            this
          );
        }
        errorsText(
          e = this.errors,
          { separator: t = `, `, dataVar: n = `data` } = {},
        ) {
          return !e || e.length === 0
            ? `No errors`
            : e
                .map((e) => `${n}${e.instancePath} ${e.message}`)
                .reduce((e, n) => e + t + n);
        }
        $dataMetaSchema(e, t) {
          let n = this.RULES.all;
          e = JSON.parse(JSON.stringify(e));
          for (let r of t) {
            let t = r.split(`/`).slice(1),
              i = e;
            for (let e of t) i = i[e];
            for (let e in n) {
              let t = n[e];
              if (typeof t != `object`) continue;
              let { $data: r } = t.definition,
                a = i[e];
              r && a && (i[e] = E(a));
            }
          }
          return e;
        }
        _removeAllSchemas(e, t) {
          for (let n in e) {
            let r = e[n];
            (!t || t.test(n)) &&
              (typeof r == `string`
                ? delete e[n]
                : r && !r.meta && (this._cache.delete(r.schema), delete e[n]));
          }
        }
        _addSchema(
          e,
          t,
          n,
          r = this.opts.validateSchema,
          i = this.opts.addUsedSchema,
        ) {
          let a,
            { schemaId: s } = this.opts;
          if (typeof e == `object`) a = e[s];
          else if (this.opts.jtd) throw Error(`schema must be object`);
          else if (typeof e != `boolean`)
            throw Error(`schema must be object or boolean`);
          let l = this._cache.get(e);
          if (l !== void 0) return l;
          n = (0, c.normalizeId)(a || n);
          let u = c.getSchemaRefs.call(this, e, n);
          return (
            (l = new o.SchemaEnv({
              schema: e,
              schemaId: s,
              meta: t,
              baseId: n,
              localRefs: u,
            })),
            this._cache.set(l.schema, l),
            i &&
              !n.startsWith(`#`) &&
              (n && this._checkUnique(n), (this.refs[n] = l)),
            r && this.validateSchema(e, !0),
            l
          );
        }
        _checkUnique(e) {
          if (this.schemas[e] || this.refs[e])
            throw Error(`schema with key or id "${e}" already exists`);
        }
        _compileSchemaEnv(e) {
          if (
            (e.meta
              ? this._compileMetaSchema(e)
              : o.compileSchema.call(this, e),
            !e.validate)
          )
            throw Error(`ajv implementation error`);
          return e.validate;
        }
        _compileMetaSchema(e) {
          let t = this.opts;
          this.opts = this._metaOpts;
          try {
            o.compileSchema.call(this, e);
          } finally {
            this.opts = t;
          }
        }
      };
      ((x.ValidationError = r.default),
        (x.MissingRefError = i.default),
        (e.default = x));
      function ee(e, t, n, r = `error`) {
        for (let i in e) {
          let a = i;
          a in t && this.logger[r](`${n}: option ${i}. ${e[a]}`);
        }
      }
      function S(e) {
        return ((e = (0, c.normalizeId)(e)), this.schemas[e] || this.refs[e]);
      }
      function C() {
        let e = this.opts.schemas;
        if (e)
          if (Array.isArray(e)) this.addSchema(e);
          else for (let t in e) this.addSchema(e[t], t);
      }
      function w() {
        for (let e in this.opts.formats) {
          let t = this.opts.formats[e];
          t && this.addFormat(e, t);
        }
      }
      function te(e) {
        if (Array.isArray(e)) {
          this.addVocabulary(e);
          return;
        }
        this.logger.warn(`keywords option as map is deprecated, pass array`);
        for (let t in e) {
          let n = e[t];
          ((n.keyword ||= t), this.addKeyword(n));
        }
      }
      function ne() {
        let e = { ...this.opts };
        for (let t of h) delete e[t];
        return e;
      }
      var re = { log() {}, warn() {}, error() {} };
      function ie(e) {
        if (e === !1) return re;
        if (e === void 0) return console;
        if (e.log && e.warn && e.error) return e;
        throw Error(`logger must implement log, warn and error methods`);
      }
      var ae = /^[a-z_$][a-z0-9_$:-]*$/i;
      function oe(e, t) {
        let { RULES: n } = this;
        if (
          ((0, u.eachItem)(e, (e) => {
            if (n.keywords[e]) throw Error(`Keyword ${e} is already defined`);
            if (!ae.test(e)) throw Error(`Keyword ${e} has invalid name`);
          }),
          t && t.$data && !(`code` in t || `validate` in t))
        )
          throw Error(`$data keyword must have "code" or "validate" function`);
      }
      function se(e, t, n) {
        var r;
        let i = t?.post;
        if (n && i) throw Error(`keyword with "post" flag cannot have "type"`);
        let { RULES: a } = this,
          o = i ? a.post : a.rules.find(({ type: e }) => e === n);
        if (
          (o || ((o = { type: n, rules: [] }), a.rules.push(o)),
          (a.keywords[e] = !0),
          !t)
        )
          return;
        let s = {
          keyword: e,
          definition: {
            ...t,
            type: (0, l.getJSONTypes)(t.type),
            schemaType: (0, l.getJSONTypes)(t.schemaType),
          },
        };
        (t.before ? ce.call(this, o, s, t.before) : o.rules.push(s),
          (a.all[e] = s),
          (r = t.implements) == null || r.forEach((e) => this.addKeyword(e)));
      }
      function ce(e, t, n) {
        let r = e.rules.findIndex((e) => e.keyword === n);
        r >= 0
          ? e.rules.splice(r, 0, t)
          : (e.rules.push(t), this.logger.warn(`rule ${n} is not defined`));
      }
      function le(e) {
        let { metaSchema: t } = e;
        t !== void 0 &&
          (e.$data && this.opts.$data && (t = E(t)),
          (e.validateSchema = this.compile(t, !0)));
      }
      var T = {
        $ref: `https://raw.githubusercontent.com/ajv-validator/ajv/master/lib/refs/data.json#`,
      };
      function E(e) {
        return { anyOf: [e, T] };
      }
    }),
    fu = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.default = {
          keyword: `id`,
          code() {
            throw Error(`NOT SUPPORTED: keyword "id", use "$id" for schema ID`);
          },
        }));
    }),
    pu = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.callRef = e.getValidate = void 0));
      var t = Zl(),
        n = Ul(),
        r = W(),
        i = K(),
        a = Ql(),
        o = G(),
        s = {
          keyword: `$ref`,
          schemaType: `string`,
          code(e) {
            let { gen: n, schema: i, it: o } = e,
              {
                baseId: s,
                schemaEnv: u,
                validateName: d,
                opts: f,
                self: p,
              } = o,
              { root: m } = u;
            if ((i === `#` || i === `#/`) && s === m.baseId) return g();
            let h = a.resolveRef.call(p, m, s, i);
            if (h === void 0) throw new t.default(o.opts.uriResolver, s, i);
            if (h instanceof a.SchemaEnv) return _(h);
            return v(h);
            function g() {
              if (u === m) return l(e, d, u, u.$async);
              let t = n.scopeValue(`root`, { ref: m });
              return l(e, (0, r._)`${t}.validate`, m, m.$async);
            }
            function _(t) {
              l(e, c(e, t), t, t.$async);
            }
            function v(t) {
              let a = n.scopeValue(
                  `schema`,
                  f.code.source === !0
                    ? { ref: t, code: (0, r.stringify)(t) }
                    : { ref: t },
                ),
                o = n.name(`valid`),
                s = e.subschema(
                  {
                    schema: t,
                    dataTypes: [],
                    schemaPath: r.nil,
                    topSchemaRef: a,
                    errSchemaPath: i,
                  },
                  o,
                );
              (e.mergeEvaluated(s), e.ok(o));
            }
          },
        };
      function c(e, t) {
        let { gen: n } = e;
        return t.validate
          ? n.scopeValue(`validate`, { ref: t.validate })
          : (0, r._)`${n.scopeValue(`wrapper`, { ref: t })}.validate`;
      }
      e.getValidate = c;
      function l(e, t, a, s) {
        let { gen: c, it: l } = e,
          { allErrors: u, schemaEnv: d, opts: f } = l,
          p = f.passContext ? i.default.this : r.nil;
        s ? m() : h();
        function m() {
          if (!d.$async) throw Error(`async schema referenced by sync schema`);
          let i = c.let(`valid`);
          (c.try(
            () => {
              (c.code((0, r._)`await ${(0, n.callValidateCode)(e, t, p)}`),
                _(t),
                u || c.assign(i, !0));
            },
            (e) => {
              (c.if((0, r._)`!(${e} instanceof ${l.ValidationError})`, () =>
                c.throw(e),
              ),
                g(e),
                u || c.assign(i, !1));
            },
          ),
            e.ok(i));
        }
        function h() {
          e.result(
            (0, n.callValidateCode)(e, t, p),
            () => _(t),
            () => g(t),
          );
        }
        function g(e) {
          let t = (0, r._)`${e}.errors`;
          (c.assign(
            i.default.vErrors,
            (0,
            r._)`${i.default.vErrors} === null ? ${t} : ${i.default.vErrors}.concat(${t})`,
          ),
            c.assign(i.default.errors, (0, r._)`${i.default.vErrors}.length`));
        }
        function _(e) {
          if (!l.opts.unevaluated) return;
          let t = a?.validate?.evaluated;
          if (l.props !== !0)
            if (t && !t.dynamicProps)
              t.props !== void 0 &&
                (l.props = o.mergeEvaluated.props(c, t.props, l.props));
            else {
              let t = c.var(`props`, (0, r._)`${e}.evaluated.props`);
              l.props = o.mergeEvaluated.props(c, t, l.props, r.Name);
            }
          if (l.items !== !0)
            if (t && !t.dynamicItems)
              t.items !== void 0 &&
                (l.items = o.mergeEvaluated.items(c, t.items, l.items));
            else {
              let t = c.var(`items`, (0, r._)`${e}.evaluated.items`);
              l.items = o.mergeEvaluated.items(c, t, l.items, r.Name);
            }
        }
      }
      ((e.callRef = l), (e.default = s));
    }),
    mu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = fu(),
        n = pu();
      e.default = [
        `$schema`,
        `$id`,
        `$defs`,
        `$vocabulary`,
        { keyword: `$comment` },
        `definitions`,
        t.default,
        n.default,
      ];
    }),
    hu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = t.operators,
        r = {
          maximum: { okStr: `<=`, ok: n.LTE, fail: n.GT },
          minimum: { okStr: `>=`, ok: n.GTE, fail: n.LT },
          exclusiveMaximum: { okStr: `<`, ok: n.LT, fail: n.GTE },
          exclusiveMinimum: { okStr: `>`, ok: n.GT, fail: n.LTE },
        };
      e.default = {
        keyword: Object.keys(r),
        type: `number`,
        schemaType: `number`,
        $data: !0,
        error: {
          message: ({ keyword: e, schemaCode: n }) =>
            (0, t.str)`must be ${r[e].okStr} ${n}`,
          params: ({ keyword: e, schemaCode: n }) =>
            (0, t._)`{comparison: ${r[e].okStr}, limit: ${n}}`,
        },
        code(e) {
          let { keyword: n, data: i, schemaCode: a } = e;
          e.fail$data((0, t._)`${i} ${r[n].fail} ${a} || isNaN(${i})`);
        },
      };
    }),
    gu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W();
      e.default = {
        keyword: `multipleOf`,
        type: `number`,
        schemaType: `number`,
        $data: !0,
        error: {
          message: ({ schemaCode: e }) => (0, t.str)`must be multiple of ${e}`,
          params: ({ schemaCode: e }) => (0, t._)`{multipleOf: ${e}}`,
        },
        code(e) {
          let { gen: n, data: r, schemaCode: i, it: a } = e,
            o = a.opts.multipleOfPrecision,
            s = n.let(`res`),
            c = o
              ? (0, t._)`Math.abs(Math.round(${s}) - ${s}) > 1e-${o}`
              : (0, t._)`${s} !== parseInt(${s})`;
          e.fail$data((0, t._)`(${i} === 0 || (${s} = ${r}/${i}, ${c}))`);
        },
      };
    }),
    _u = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      function t(e) {
        let t = e.length,
          n = 0,
          r = 0,
          i;
        for (; r < t; )
          (n++,
            (i = e.charCodeAt(r++)),
            i >= 55296 &&
              i <= 56319 &&
              r < t &&
              ((i = e.charCodeAt(r)), (i & 64512) == 56320 && r++));
        return n;
      }
      ((e.default = t),
        (t.code = `require("ajv/dist/runtime/ucs2length").default`));
    }),
    vu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = G(),
        r = _u();
      e.default = {
        keyword: [`maxLength`, `minLength`],
        type: `string`,
        schemaType: `number`,
        $data: !0,
        error: {
          message({ keyword: e, schemaCode: n }) {
            let r = e === `maxLength` ? `more` : `fewer`;
            return (0, t.str)`must NOT have ${r} than ${n} characters`;
          },
          params: ({ schemaCode: e }) => (0, t._)`{limit: ${e}}`,
        },
        code(e) {
          let { keyword: i, data: a, schemaCode: o, it: s } = e,
            c = i === `maxLength` ? t.operators.GT : t.operators.LT,
            l =
              s.opts.unicode === !1
                ? (0, t._)`${a}.length`
                : (0, t._)`${(0, n.useFunc)(e.gen, r.default)}(${a})`;
          e.fail$data((0, t._)`${l} ${c} ${o}`);
        },
      };
    }),
    yu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Ul(),
        n = G(),
        r = W();
      e.default = {
        keyword: `pattern`,
        type: `string`,
        schemaType: `string`,
        $data: !0,
        error: {
          message: ({ schemaCode: e }) => (0, r.str)`must match pattern "${e}"`,
          params: ({ schemaCode: e }) => (0, r._)`{pattern: ${e}}`,
        },
        code(e) {
          let {
              gen: i,
              data: a,
              $data: o,
              schema: s,
              schemaCode: c,
              it: l,
            } = e,
            u = l.opts.unicodeRegExp ? `u` : ``;
          if (o) {
            let { regExp: t } = l.opts.code,
              o =
                t.code === `new RegExp`
                  ? (0, r._)`new RegExp`
                  : (0, n.useFunc)(i, t),
              s = i.let(`valid`);
            (i.try(
              () => i.assign(s, (0, r._)`${o}(${c}, ${u}).test(${a})`),
              () => i.assign(s, !1),
            ),
              e.fail$data((0, r._)`!${s}`));
          } else {
            let n = (0, t.usePattern)(e, s);
            e.fail$data((0, r._)`!${n}.test(${a})`);
          }
        },
      };
    }),
    bu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W();
      e.default = {
        keyword: [`maxProperties`, `minProperties`],
        type: `object`,
        schemaType: `number`,
        $data: !0,
        error: {
          message({ keyword: e, schemaCode: n }) {
            let r = e === `maxProperties` ? `more` : `fewer`;
            return (0, t.str)`must NOT have ${r} than ${n} properties`;
          },
          params: ({ schemaCode: e }) => (0, t._)`{limit: ${e}}`,
        },
        code(e) {
          let { keyword: n, data: r, schemaCode: i } = e,
            a = n === `maxProperties` ? t.operators.GT : t.operators.LT;
          e.fail$data((0, t._)`Object.keys(${r}).length ${a} ${i}`);
        },
      };
    }),
    xu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Ul(),
        n = W(),
        r = G();
      e.default = {
        keyword: `required`,
        type: `object`,
        schemaType: `array`,
        $data: !0,
        error: {
          message: ({ params: { missingProperty: e } }) =>
            (0, n.str)`must have required property '${e}'`,
          params: ({ params: { missingProperty: e } }) =>
            (0, n._)`{missingProperty: ${e}}`,
        },
        code(e) {
          let {
              gen: i,
              schema: a,
              schemaCode: o,
              data: s,
              $data: c,
              it: l,
            } = e,
            { opts: u } = l;
          if (!c && a.length === 0) return;
          let d = a.length >= u.loopRequired;
          if ((l.allErrors ? f() : p(), u.strictRequired)) {
            let t = e.parentSchema.properties,
              { definedProperties: n } = e.it;
            for (let e of a)
              if (t?.[e] === void 0 && !n.has(e)) {
                let t = `required property "${e}" is not defined at "${l.schemaEnv.baseId + l.errSchemaPath}" (strictRequired)`;
                (0, r.checkStrictMode)(l, t, l.opts.strictRequired);
              }
          }
          function f() {
            if (d || c) e.block$data(n.nil, m);
            else for (let n of a) (0, t.checkReportMissingProp)(e, n);
          }
          function p() {
            let n = i.let(`missing`);
            if (d || c) {
              let t = i.let(`valid`, !0);
              (e.block$data(t, () => h(n, t)), e.ok(t));
            } else
              (i.if((0, t.checkMissingProp)(e, a, n)),
                (0, t.reportMissingProp)(e, n),
                i.else());
          }
          function m() {
            i.forOf(`prop`, o, (n) => {
              (e.setParams({ missingProperty: n }),
                i.if((0, t.noPropertyInData)(i, s, n, u.ownProperties), () =>
                  e.error(),
                ));
            });
          }
          function h(r, a) {
            (e.setParams({ missingProperty: r }),
              i.forOf(
                r,
                o,
                () => {
                  (i.assign(a, (0, t.propertyInData)(i, s, r, u.ownProperties)),
                    i.if((0, n.not)(a), () => {
                      (e.error(), i.break());
                    }));
                },
                n.nil,
              ));
          }
        },
      };
    }),
    Su = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W();
      e.default = {
        keyword: [`maxItems`, `minItems`],
        type: `array`,
        schemaType: `number`,
        $data: !0,
        error: {
          message({ keyword: e, schemaCode: n }) {
            let r = e === `maxItems` ? `more` : `fewer`;
            return (0, t.str)`must NOT have ${r} than ${n} items`;
          },
          params: ({ schemaCode: e }) => (0, t._)`{limit: ${e}}`,
        },
        code(e) {
          let { keyword: n, data: r, schemaCode: i } = e,
            a = n === `maxItems` ? t.operators.GT : t.operators.LT;
          e.fail$data((0, t._)`${r}.length ${a} ${i}`);
        },
      };
    }),
    Cu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Kl();
      ((t.code = `require("ajv/dist/runtime/equal").default`), (e.default = t));
    }),
    wu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Vl(),
        n = W(),
        r = G(),
        i = Cu();
      e.default = {
        keyword: `uniqueItems`,
        type: `array`,
        schemaType: `boolean`,
        $data: !0,
        error: {
          message: ({ params: { i: e, j: t } }) =>
            (0,
            n.str)`must NOT have duplicate items (items ## ${t} and ${e} are identical)`,
          params: ({ params: { i: e, j: t } }) => (0, n._)`{i: ${e}, j: ${t}}`,
        },
        code(e) {
          let {
            gen: a,
            data: o,
            $data: s,
            schema: c,
            parentSchema: l,
            schemaCode: u,
            it: d,
          } = e;
          if (!s && !c) return;
          let f = a.let(`valid`),
            p = l.items ? (0, t.getSchemaTypes)(l.items) : [];
          (e.block$data(f, m, (0, n._)`${u} === false`), e.ok(f));
          function m() {
            let t = a.let(`i`, (0, n._)`${o}.length`),
              r = a.let(`j`);
            (e.setParams({ i: t, j: r }),
              a.assign(f, !0),
              a.if((0, n._)`${t} > 1`, () => (h() ? g : _)(t, r)));
          }
          function h() {
            return (
              p.length > 0 && !p.some((e) => e === `object` || e === `array`)
            );
          }
          function g(r, i) {
            let s = a.name(`item`),
              c = (0, t.checkDataTypes)(
                p,
                s,
                d.opts.strictNumbers,
                t.DataType.Wrong,
              ),
              l = a.const(`indices`, (0, n._)`{}`);
            a.for((0, n._)`;${r}--;`, () => {
              (a.let(s, (0, n._)`${o}[${r}]`),
                a.if(c, (0, n._)`continue`),
                p.length > 1 &&
                  a.if(
                    (0, n._)`typeof ${s} == "string"`,
                    (0, n._)`${s} += "_"`,
                  ),
                a
                  .if((0, n._)`typeof ${l}[${s}] == "number"`, () => {
                    (a.assign(i, (0, n._)`${l}[${s}]`),
                      e.error(),
                      a.assign(f, !1).break());
                  })
                  .code((0, n._)`${l}[${s}] = ${r}`));
            });
          }
          function _(t, s) {
            let c = (0, r.useFunc)(a, i.default),
              l = a.name(`outer`);
            a.label(l).for((0, n._)`;${t}--;`, () =>
              a.for((0, n._)`${s} = ${t}; ${s}--;`, () =>
                a.if((0, n._)`${c}(${o}[${t}], ${o}[${s}])`, () => {
                  (e.error(), a.assign(f, !1).break(l));
                }),
              ),
            );
          }
        },
      };
    }),
    Tu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = G(),
        r = Cu();
      e.default = {
        keyword: `const`,
        $data: !0,
        error: {
          message: `must be equal to constant`,
          params: ({ schemaCode: e }) => (0, t._)`{allowedValue: ${e}}`,
        },
        code(e) {
          let { gen: i, data: a, $data: o, schemaCode: s, schema: c } = e;
          o || (c && typeof c == `object`)
            ? e.fail$data(
                (0, t._)`!${(0, n.useFunc)(i, r.default)}(${a}, ${s})`,
              )
            : e.fail((0, t._)`${c} !== ${a}`);
        },
      };
    }),
    Eu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = G(),
        r = Cu();
      e.default = {
        keyword: `enum`,
        schemaType: `array`,
        $data: !0,
        error: {
          message: `must be equal to one of the allowed values`,
          params: ({ schemaCode: e }) => (0, t._)`{allowedValues: ${e}}`,
        },
        code(e) {
          let {
            gen: i,
            data: a,
            $data: o,
            schema: s,
            schemaCode: c,
            it: l,
          } = e;
          if (!o && s.length === 0)
            throw Error(`enum must have non-empty array`);
          let u = s.length >= l.opts.loopEnum,
            d,
            f = () => (d ??= (0, n.useFunc)(i, r.default)),
            p;
          if (u || o) ((p = i.let(`valid`)), e.block$data(p, m));
          else {
            if (!Array.isArray(s)) throw Error(`ajv implementation error`);
            let e = i.const(`vSchema`, c);
            p = (0, t.or)(...s.map((t, n) => h(e, n)));
          }
          e.pass(p);
          function m() {
            (i.assign(p, !1),
              i.forOf(`v`, c, (e) =>
                i.if((0, t._)`${f()}(${a}, ${e})`, () =>
                  i.assign(p, !0).break(),
                ),
              ));
          }
          function h(e, n) {
            let r = s[n];
            return typeof r == `object` && r
              ? (0, t._)`${f()}(${a}, ${e}[${n}])`
              : (0, t._)`${a} === ${r}`;
          }
        },
      };
    }),
    Du = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = hu(),
        n = gu(),
        r = vu(),
        i = yu(),
        a = bu(),
        o = xu(),
        s = Su(),
        c = wu(),
        l = Tu(),
        u = Eu();
      e.default = [
        t.default,
        n.default,
        r.default,
        i.default,
        a.default,
        o.default,
        s.default,
        c.default,
        { keyword: `type`, schemaType: [`string`, `array`] },
        { keyword: `nullable`, schemaType: `boolean` },
        l.default,
        u.default,
      ];
    }),
    Ou = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.validateAdditionalItems = void 0));
      var t = W(),
        n = G(),
        r = {
          keyword: `additionalItems`,
          type: `array`,
          schemaType: [`boolean`, `object`],
          before: `uniqueItems`,
          error: {
            message: ({ params: { len: e } }) =>
              (0, t.str)`must NOT have more than ${e} items`,
            params: ({ params: { len: e } }) => (0, t._)`{limit: ${e}}`,
          },
          code(e) {
            let { parentSchema: t, it: r } = e,
              { items: a } = t;
            if (!Array.isArray(a)) {
              (0, n.checkStrictMode)(
                r,
                `"additionalItems" is ignored when "items" is not an array of schemas`,
              );
              return;
            }
            i(e, a);
          },
        };
      function i(e, r) {
        let { gen: i, schema: a, data: o, keyword: s, it: c } = e;
        c.items = !0;
        let l = i.const(`len`, (0, t._)`${o}.length`);
        if (a === !1)
          (e.setParams({ len: r.length }),
            e.pass((0, t._)`${l} <= ${r.length}`));
        else if (typeof a == `object` && !(0, n.alwaysValidSchema)(c, a)) {
          let n = i.var(`valid`, (0, t._)`${l} <= ${r.length}`);
          (i.if((0, t.not)(n), () => u(n)), e.ok(n));
        }
        function u(a) {
          i.forRange(`i`, r.length, l, (r) => {
            (e.subschema(
              { keyword: s, dataProp: r, dataPropType: n.Type.Num },
              a,
            ),
              c.allErrors || i.if((0, t.not)(a), () => i.break()));
          });
        }
      }
      ((e.validateAdditionalItems = i), (e.default = r));
    }),
    ku = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.validateTuple = void 0));
      var t = W(),
        n = G(),
        r = Ul(),
        i = {
          keyword: `items`,
          type: `array`,
          schemaType: [`object`, `array`, `boolean`],
          before: `uniqueItems`,
          code(e) {
            let { schema: t, it: i } = e;
            if (Array.isArray(t)) return a(e, `additionalItems`, t);
            ((i.items = !0),
              !(0, n.alwaysValidSchema)(i, t) && e.ok((0, r.validateArray)(e)));
          },
        };
      function a(e, r, i = e.schema) {
        let { gen: a, parentSchema: o, data: s, keyword: c, it: l } = e;
        (f(o),
          l.opts.unevaluated &&
            i.length &&
            l.items !== !0 &&
            (l.items = n.mergeEvaluated.items(a, i.length, l.items)));
        let u = a.name(`valid`),
          d = a.const(`len`, (0, t._)`${s}.length`);
        i.forEach((r, i) => {
          (0, n.alwaysValidSchema)(l, r) ||
            (a.if((0, t._)`${d} > ${i}`, () =>
              e.subschema({ keyword: c, schemaProp: i, dataProp: i }, u),
            ),
            e.ok(u));
        });
        function f(e) {
          let { opts: t, errSchemaPath: a } = l,
            o = i.length,
            s = o === e.minItems && (o === e.maxItems || e[r] === !1);
          if (t.strictTuples && !s) {
            let e = `"${c}" is ${o}-tuple, but minItems or maxItems/${r} are not specified or different at path "${a}"`;
            (0, n.checkStrictMode)(l, e, t.strictTuples);
          }
        }
      }
      ((e.validateTuple = a), (e.default = i));
    }),
    Au = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = ku();
      e.default = {
        keyword: `prefixItems`,
        type: `array`,
        schemaType: [`array`],
        before: `uniqueItems`,
        code: (e) => (0, t.validateTuple)(e, `items`),
      };
    }),
    ju = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = G(),
        r = Ul(),
        i = Ou();
      e.default = {
        keyword: `items`,
        type: `array`,
        schemaType: [`object`, `boolean`],
        before: `uniqueItems`,
        error: {
          message: ({ params: { len: e } }) =>
            (0, t.str)`must NOT have more than ${e} items`,
          params: ({ params: { len: e } }) => (0, t._)`{limit: ${e}}`,
        },
        code(e) {
          let { schema: t, parentSchema: a, it: o } = e,
            { prefixItems: s } = a;
          ((o.items = !0),
            !(0, n.alwaysValidSchema)(o, t) &&
              (s
                ? (0, i.validateAdditionalItems)(e, s)
                : e.ok((0, r.validateArray)(e))));
        },
      };
    }),
    Mu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = G();
      e.default = {
        keyword: `contains`,
        type: `array`,
        schemaType: [`object`, `boolean`],
        before: `uniqueItems`,
        trackErrors: !0,
        error: {
          message: ({ params: { min: e, max: n } }) =>
            n === void 0
              ? (0, t.str)`must contain at least ${e} valid item(s)`
              : (0,
                t.str)`must contain at least ${e} and no more than ${n} valid item(s)`,
          params: ({ params: { min: e, max: n } }) =>
            n === void 0
              ? (0, t._)`{minContains: ${e}}`
              : (0, t._)`{minContains: ${e}, maxContains: ${n}}`,
        },
        code(e) {
          let { gen: r, schema: i, parentSchema: a, data: o, it: s } = e,
            c,
            l,
            { minContains: u, maxContains: d } = a;
          s.opts.next ? ((c = u === void 0 ? 1 : u), (l = d)) : (c = 1);
          let f = r.const(`len`, (0, t._)`${o}.length`);
          if ((e.setParams({ min: c, max: l }), l === void 0 && c === 0)) {
            (0, n.checkStrictMode)(
              s,
              `"minContains" == 0 without "maxContains": "contains" keyword ignored`,
            );
            return;
          }
          if (l !== void 0 && c > l) {
            ((0, n.checkStrictMode)(
              s,
              `"minContains" > "maxContains" is always invalid`,
            ),
              e.fail());
            return;
          }
          if ((0, n.alwaysValidSchema)(s, i)) {
            let n = (0, t._)`${f} >= ${c}`;
            (l !== void 0 && (n = (0, t._)`${n} && ${f} <= ${l}`), e.pass(n));
            return;
          }
          s.items = !0;
          let p = r.name(`valid`);
          (l === void 0 && c === 1
            ? h(p, () => r.if(p, () => r.break()))
            : c === 0
              ? (r.let(p, !0),
                l !== void 0 && r.if((0, t._)`${o}.length > 0`, m))
              : (r.let(p, !1), m()),
            e.result(p, () => e.reset()));
          function m() {
            let e = r.name(`_valid`),
              t = r.let(`count`, 0);
            h(e, () => r.if(e, () => g(t)));
          }
          function h(t, i) {
            r.forRange(`i`, 0, f, (r) => {
              (e.subschema(
                {
                  keyword: `contains`,
                  dataProp: r,
                  dataPropType: n.Type.Num,
                  compositeRule: !0,
                },
                t,
              ),
                i());
            });
          }
          function g(e) {
            (r.code((0, t._)`${e}++`),
              l === void 0
                ? r.if((0, t._)`${e} >= ${c}`, () => r.assign(p, !0).break())
                : (r.if((0, t._)`${e} > ${l}`, () => r.assign(p, !1).break()),
                  c === 1
                    ? r.assign(p, !0)
                    : r.if((0, t._)`${e} >= ${c}`, () => r.assign(p, !0))));
          }
        },
      };
    }),
    Nu = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.validateSchemaDeps = e.validatePropertyDeps = e.error = void 0));
      var t = W(),
        n = G(),
        r = Ul();
      e.error = {
        message: ({ params: { property: e, depsCount: n, deps: r } }) => {
          let i = n === 1 ? `property` : `properties`;
          return (0, t.str)`must have ${i} ${r} when property ${e} is present`;
        },
        params: ({
          params: { property: e, depsCount: n, deps: r, missingProperty: i },
        }) => (0, t._)`{property: ${e},
    missingProperty: ${i},
    depsCount: ${n},
    deps: ${r}}`,
      };
      var i = {
        keyword: `dependencies`,
        type: `object`,
        schemaType: `object`,
        error: e.error,
        code(e) {
          let [t, n] = a(e);
          (o(e, t), s(e, n));
        },
      };
      function a({ schema: e }) {
        let t = {},
          n = {};
        for (let r in e) {
          if (r === `__proto__`) continue;
          let i = Array.isArray(e[r]) ? t : n;
          i[r] = e[r];
        }
        return [t, n];
      }
      function o(e, n = e.schema) {
        let { gen: i, data: a, it: o } = e;
        if (Object.keys(n).length === 0) return;
        let s = i.let(`missing`);
        for (let c in n) {
          let l = n[c];
          if (l.length === 0) continue;
          let u = (0, r.propertyInData)(i, a, c, o.opts.ownProperties);
          (e.setParams({
            property: c,
            depsCount: l.length,
            deps: l.join(`, `),
          }),
            o.allErrors
              ? i.if(u, () => {
                  for (let t of l) (0, r.checkReportMissingProp)(e, t);
                })
              : (i.if((0, t._)`${u} && (${(0, r.checkMissingProp)(e, l, s)})`),
                (0, r.reportMissingProp)(e, s),
                i.else()));
        }
      }
      e.validatePropertyDeps = o;
      function s(e, t = e.schema) {
        let { gen: i, data: a, keyword: o, it: s } = e,
          c = i.name(`valid`);
        for (let l in t)
          (0, n.alwaysValidSchema)(s, t[l]) ||
            (i.if(
              (0, r.propertyInData)(i, a, l, s.opts.ownProperties),
              () => {
                let t = e.subschema({ keyword: o, schemaProp: l }, c);
                e.mergeValidEvaluated(t, c);
              },
              () => i.var(c, !0),
            ),
            e.ok(c));
      }
      ((e.validateSchemaDeps = s), (e.default = i));
    }),
    Pu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = G();
      e.default = {
        keyword: `propertyNames`,
        type: `object`,
        schemaType: [`object`, `boolean`],
        error: {
          message: `property name must be valid`,
          params: ({ params: e }) =>
            (0, t._)`{propertyName: ${e.propertyName}}`,
        },
        code(e) {
          let { gen: r, schema: i, data: a, it: o } = e;
          if ((0, n.alwaysValidSchema)(o, i)) return;
          let s = r.name(`valid`);
          (r.forIn(`key`, a, (n) => {
            (e.setParams({ propertyName: n }),
              e.subschema(
                {
                  keyword: `propertyNames`,
                  data: n,
                  dataTypes: [`string`],
                  propertyName: n,
                  compositeRule: !0,
                },
                s,
              ),
              r.if((0, t.not)(s), () => {
                (e.error(!0), o.allErrors || r.break());
              }));
          }),
            e.ok(s));
        },
      };
    }),
    Fu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Ul(),
        n = W(),
        r = K(),
        i = G();
      e.default = {
        keyword: `additionalProperties`,
        type: [`object`],
        schemaType: [`boolean`, `object`],
        allowUndefined: !0,
        trackErrors: !0,
        error: {
          message: `must NOT have additional properties`,
          params: ({ params: e }) =>
            (0, n._)`{additionalProperty: ${e.additionalProperty}}`,
        },
        code(e) {
          let {
            gen: a,
            schema: o,
            parentSchema: s,
            data: c,
            errsCount: l,
            it: u,
          } = e;
          if (!l) throw Error(`ajv implementation error`);
          let { allErrors: d, opts: f } = u;
          if (
            ((u.props = !0),
            f.removeAdditional !== `all` && (0, i.alwaysValidSchema)(u, o))
          )
            return;
          let p = (0, t.allSchemaProperties)(s.properties),
            m = (0, t.allSchemaProperties)(s.patternProperties);
          (h(), e.ok((0, n._)`${l} === ${r.default.errors}`));
          function h() {
            a.forIn(`key`, c, (e) => {
              !p.length && !m.length ? v(e) : a.if(g(e), () => v(e));
            });
          }
          function g(r) {
            let o;
            if (p.length > 8) {
              let e = (0, i.schemaRefOrVal)(u, s.properties, `properties`);
              o = (0, t.isOwnProperty)(a, e, r);
            } else
              o = p.length
                ? (0, n.or)(...p.map((e) => (0, n._)`${r} === ${e}`))
                : n.nil;
            return (
              m.length &&
                (o = (0, n.or)(
                  o,
                  ...m.map(
                    (i) => (0, n._)`${(0, t.usePattern)(e, i)}.test(${r})`,
                  ),
                )),
              (0, n.not)(o)
            );
          }
          function _(e) {
            a.code((0, n._)`delete ${c}[${e}]`);
          }
          function v(t) {
            if (
              f.removeAdditional === `all` ||
              (f.removeAdditional && o === !1)
            ) {
              _(t);
              return;
            }
            if (o === !1) {
              (e.setParams({ additionalProperty: t }),
                e.error(),
                d || a.break());
              return;
            }
            if (typeof o == `object` && !(0, i.alwaysValidSchema)(u, o)) {
              let r = a.name(`valid`);
              f.removeAdditional === `failing`
                ? (y(t, r, !1),
                  a.if((0, n.not)(r), () => {
                    (e.reset(), _(t));
                  }))
                : (y(t, r), d || a.if((0, n.not)(r), () => a.break()));
            }
          }
          function y(t, n, r) {
            let a = {
              keyword: `additionalProperties`,
              dataProp: t,
              dataPropType: i.Type.Str,
            };
            (r === !1 &&
              Object.assign(a, {
                compositeRule: !0,
                createErrors: !1,
                allErrors: !1,
              }),
              e.subschema(a, n));
          }
        },
      };
    }),
    Iu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Yl(),
        n = Ul(),
        r = G(),
        i = Fu();
      e.default = {
        keyword: `properties`,
        type: `object`,
        schemaType: `object`,
        code(e) {
          let { gen: a, schema: o, parentSchema: s, data: c, it: l } = e;
          l.opts.removeAdditional === `all` &&
            s.additionalProperties === void 0 &&
            i.default.code(
              new t.KeywordCxt(l, i.default, `additionalProperties`),
            );
          let u = (0, n.allSchemaProperties)(o);
          for (let e of u) l.definedProperties.add(e);
          l.opts.unevaluated &&
            u.length &&
            l.props !== !0 &&
            (l.props = r.mergeEvaluated.props(a, (0, r.toHash)(u), l.props));
          let d = u.filter((e) => !(0, r.alwaysValidSchema)(l, o[e]));
          if (d.length === 0) return;
          let f = a.name(`valid`);
          for (let t of d)
            (p(t)
              ? m(t)
              : (a.if((0, n.propertyInData)(a, c, t, l.opts.ownProperties)),
                m(t),
                l.allErrors || a.else().var(f, !0),
                a.endIf()),
              e.it.definedProperties.add(t),
              e.ok(f));
          function p(e) {
            return (
              l.opts.useDefaults && !l.compositeRule && o[e].default !== void 0
            );
          }
          function m(t) {
            e.subschema(
              { keyword: `properties`, schemaProp: t, dataProp: t },
              f,
            );
          }
        },
      };
    }),
    Lu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Ul(),
        n = W(),
        r = G(),
        i = G();
      e.default = {
        keyword: `patternProperties`,
        type: `object`,
        schemaType: `object`,
        code(e) {
          let { gen: a, schema: o, data: s, parentSchema: c, it: l } = e,
            { opts: u } = l,
            d = (0, t.allSchemaProperties)(o),
            f = d.filter((e) => (0, r.alwaysValidSchema)(l, o[e]));
          if (
            d.length === 0 ||
            (f.length === d.length && (!l.opts.unevaluated || l.props === !0))
          )
            return;
          let p = u.strictSchema && !u.allowMatchingProperties && c.properties,
            m = a.name(`valid`);
          l.props !== !0 &&
            !(l.props instanceof n.Name) &&
            (l.props = (0, i.evaluatedPropsToName)(a, l.props));
          let { props: h } = l;
          g();
          function g() {
            for (let e of d)
              (p && _(e), l.allErrors ? v(e) : (a.var(m, !0), v(e), a.if(m)));
          }
          function _(e) {
            for (let t in p)
              new RegExp(e).test(t) &&
                (0, r.checkStrictMode)(
                  l,
                  `property ${t} matches pattern ${e} (use allowMatchingProperties)`,
                );
          }
          function v(r) {
            a.forIn(`key`, s, (o) => {
              a.if((0, n._)`${(0, t.usePattern)(e, r)}.test(${o})`, () => {
                let t = f.includes(r);
                (t ||
                  e.subschema(
                    {
                      keyword: `patternProperties`,
                      schemaProp: r,
                      dataProp: o,
                      dataPropType: i.Type.Str,
                    },
                    m,
                  ),
                  l.opts.unevaluated && h !== !0
                    ? a.assign((0, n._)`${h}[${o}]`, !0)
                    : !t &&
                      !l.allErrors &&
                      a.if((0, n.not)(m), () => a.break()));
              });
            });
          }
        },
      };
    }),
    Ru = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = G();
      e.default = {
        keyword: `not`,
        schemaType: [`object`, `boolean`],
        trackErrors: !0,
        code(e) {
          let { gen: n, schema: r, it: i } = e;
          if ((0, t.alwaysValidSchema)(i, r)) {
            e.fail();
            return;
          }
          let a = n.name(`valid`);
          (e.subschema(
            {
              keyword: `not`,
              compositeRule: !0,
              createErrors: !1,
              allErrors: !1,
            },
            a,
          ),
            e.failResult(
              a,
              () => e.reset(),
              () => e.error(),
            ));
        },
        error: { message: `must NOT be valid` },
      };
    }),
    zu = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.default = {
          keyword: `anyOf`,
          schemaType: `array`,
          trackErrors: !0,
          code: Ul().validateUnion,
          error: { message: `must match a schema in anyOf` },
        }));
    }),
    Bu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = G();
      e.default = {
        keyword: `oneOf`,
        schemaType: `array`,
        trackErrors: !0,
        error: {
          message: `must match exactly one schema in oneOf`,
          params: ({ params: e }) => (0, t._)`{passingSchemas: ${e.passing}}`,
        },
        code(e) {
          let { gen: r, schema: i, parentSchema: a, it: o } = e;
          if (!Array.isArray(i)) throw Error(`ajv implementation error`);
          if (o.opts.discriminator && a.discriminator) return;
          let s = i,
            c = r.let(`valid`, !1),
            l = r.let(`passing`, null),
            u = r.name(`_valid`);
          (e.setParams({ passing: l }),
            r.block(d),
            e.result(
              c,
              () => e.reset(),
              () => e.error(!0),
            ));
          function d() {
            s.forEach((i, a) => {
              let s;
              ((0, n.alwaysValidSchema)(o, i)
                ? r.var(u, !0)
                : (s = e.subschema(
                    { keyword: `oneOf`, schemaProp: a, compositeRule: !0 },
                    u,
                  )),
                a > 0 &&
                  r
                    .if((0, t._)`${u} && ${c}`)
                    .assign(c, !1)
                    .assign(l, (0, t._)`[${l}, ${a}]`)
                    .else(),
                r.if(u, () => {
                  (r.assign(c, !0),
                    r.assign(l, a),
                    s && e.mergeEvaluated(s, t.Name));
                }));
            });
          }
        },
      };
    }),
    Vu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = G();
      e.default = {
        keyword: `allOf`,
        schemaType: `array`,
        code(e) {
          let { gen: n, schema: r, it: i } = e;
          if (!Array.isArray(r)) throw Error(`ajv implementation error`);
          let a = n.name(`valid`);
          r.forEach((n, r) => {
            if ((0, t.alwaysValidSchema)(i, n)) return;
            let o = e.subschema({ keyword: `allOf`, schemaProp: r }, a);
            (e.ok(a), e.mergeEvaluated(o));
          });
        },
      };
    }),
    Hu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = G(),
        r = {
          keyword: `if`,
          schemaType: [`object`, `boolean`],
          trackErrors: !0,
          error: {
            message: ({ params: e }) =>
              (0, t.str)`must match "${e.ifClause}" schema`,
            params: ({ params: e }) =>
              (0, t._)`{failingKeyword: ${e.ifClause}}`,
          },
          code(e) {
            let { gen: r, parentSchema: a, it: o } = e;
            a.then === void 0 &&
              a.else === void 0 &&
              (0, n.checkStrictMode)(
                o,
                `"if" without "then" and "else" is ignored`,
              );
            let s = i(o, `then`),
              c = i(o, `else`);
            if (!s && !c) return;
            let l = r.let(`valid`, !0),
              u = r.name(`_valid`);
            if ((d(), e.reset(), s && c)) {
              let t = r.let(`ifClause`);
              (e.setParams({ ifClause: t }),
                r.if(u, f(`then`, t), f(`else`, t)));
            } else s ? r.if(u, f(`then`)) : r.if((0, t.not)(u), f(`else`));
            e.pass(l, () => e.error(!0));
            function d() {
              let t = e.subschema(
                {
                  keyword: `if`,
                  compositeRule: !0,
                  createErrors: !1,
                  allErrors: !1,
                },
                u,
              );
              e.mergeEvaluated(t);
            }
            function f(n, i) {
              return () => {
                let a = e.subschema({ keyword: n }, u);
                (r.assign(l, u),
                  e.mergeValidEvaluated(a, l),
                  i
                    ? r.assign(i, (0, t._)`${n}`)
                    : e.setParams({ ifClause: n }));
              };
            }
          },
        };
      function i(e, t) {
        let r = e.schema[t];
        return r !== void 0 && !(0, n.alwaysValidSchema)(e, r);
      }
      e.default = r;
    }),
    Uu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = G();
      e.default = {
        keyword: [`then`, `else`],
        schemaType: [`object`, `boolean`],
        code({ keyword: e, parentSchema: n, it: r }) {
          n.if === void 0 &&
            (0, t.checkStrictMode)(r, `"${e}" without "if" is ignored`);
        },
      };
    }),
    Wu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = Ou(),
        n = Au(),
        r = ku(),
        i = ju(),
        a = Mu(),
        o = Nu(),
        s = Pu(),
        c = Fu(),
        l = Iu(),
        u = Lu(),
        d = Ru(),
        f = zu(),
        p = Bu(),
        m = Vu(),
        h = Hu(),
        g = Uu();
      function _(e = !1) {
        let _ = [
          d.default,
          f.default,
          p.default,
          m.default,
          h.default,
          g.default,
          s.default,
          c.default,
          o.default,
          l.default,
          u.default,
        ];
        return (
          e ? _.push(n.default, i.default) : _.push(t.default, r.default),
          _.push(a.default),
          _
        );
      }
      e.default = _;
    }),
    Gu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W();
      e.default = {
        keyword: `format`,
        type: [`number`, `string`],
        schemaType: `string`,
        $data: !0,
        error: {
          message: ({ schemaCode: e }) => (0, t.str)`must match format "${e}"`,
          params: ({ schemaCode: e }) => (0, t._)`{format: ${e}}`,
        },
        code(e, n) {
          let {
              gen: r,
              data: i,
              $data: a,
              schema: o,
              schemaCode: s,
              it: c,
            } = e,
            { opts: l, errSchemaPath: u, schemaEnv: d, self: f } = c;
          if (!l.validateFormats) return;
          a ? p() : m();
          function p() {
            let a = r.scopeValue(`formats`, {
                ref: f.formats,
                code: l.code.formats,
              }),
              o = r.const(`fDef`, (0, t._)`${a}[${s}]`),
              c = r.let(`fType`),
              u = r.let(`format`);
            (r.if(
              (0, t._)`typeof ${o} == "object" && !(${o} instanceof RegExp)`,
              () =>
                r
                  .assign(c, (0, t._)`${o}.type || "string"`)
                  .assign(u, (0, t._)`${o}.validate`),
              () => r.assign(c, (0, t._)`"string"`).assign(u, o),
            ),
              e.fail$data((0, t.or)(p(), m())));
            function p() {
              return l.strictSchema === !1 ? t.nil : (0, t._)`${s} && !${u}`;
            }
            function m() {
              let e = d.$async
                  ? (0, t._)`(${o}.async ? await ${u}(${i}) : ${u}(${i}))`
                  : (0, t._)`${u}(${i})`,
                r = (0,
                t._)`(typeof ${u} == "function" ? ${e} : ${u}.test(${i}))`;
              return (0, t._)`${u} && ${u} !== true && ${c} === ${n} && !${r}`;
            }
          }
          function m() {
            let a = f.formats[o];
            if (!a) {
              m();
              return;
            }
            if (a === !0) return;
            let [s, c, p] = h(a);
            s === n && e.pass(g());
            function m() {
              if (l.strictSchema === !1) {
                f.logger.warn(e());
                return;
              }
              throw Error(e());
              function e() {
                return `unknown format "${o}" ignored in schema at path "${u}"`;
              }
            }
            function h(e) {
              let n =
                  e instanceof RegExp
                    ? (0, t.regexpCode)(e)
                    : l.code.formats
                      ? (0, t._)`${l.code.formats}${(0, t.getProperty)(o)}`
                      : void 0,
                i = r.scopeValue(`formats`, { key: o, ref: e, code: n });
              return typeof e == `object` && !(e instanceof RegExp)
                ? [e.type || `string`, e.validate, (0, t._)`${i}.validate`]
                : [`string`, e, i];
            }
            function g() {
              if (typeof a == `object` && !(a instanceof RegExp) && a.async) {
                if (!d.$async) throw Error(`async format in sync schema`);
                return (0, t._)`await ${p}(${i})`;
              }
              return typeof c == `function`
                ? (0, t._)`${p}(${i})`
                : (0, t._)`${p}.test(${i})`;
            }
          }
        },
      };
    }),
    Ku = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.default = [Gu().default]));
    }),
    qu = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.contentVocabulary = e.metadataVocabulary = void 0),
        (e.metadataVocabulary = [
          `title`,
          `description`,
          `default`,
          `deprecated`,
          `readOnly`,
          `writeOnly`,
          `examples`,
        ]),
        (e.contentVocabulary = [
          `contentMediaType`,
          `contentEncoding`,
          `contentSchema`,
        ]));
    }),
    Ju = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = mu(),
        n = Du(),
        r = Wu(),
        i = Ku(),
        a = qu();
      e.default = [
        t.default,
        n.default,
        (0, r.default)(),
        i.default,
        a.metadataVocabulary,
        a.contentVocabulary,
      ];
    }),
    Yu = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.DiscrError = void 0));
      var t;
      (function (e) {
        ((e.Tag = `tag`), (e.Mapping = `mapping`));
      })(t || (e.DiscrError = t = {}));
    }),
    Xu = c((e) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var t = W(),
        n = Yu(),
        r = Ql(),
        i = Zl(),
        a = G();
      e.default = {
        keyword: `discriminator`,
        type: `object`,
        schemaType: `object`,
        error: {
          message: ({ params: { discrError: e, tagName: t } }) =>
            e === n.DiscrError.Tag
              ? `tag "${t}" must be string`
              : `value of tag "${t}" must be in oneOf`,
          params: ({ params: { discrError: e, tag: n, tagName: r } }) =>
            (0, t._)`{error: ${e}, tag: ${r}, tagValue: ${n}}`,
        },
        code(e) {
          let { gen: o, data: s, schema: c, parentSchema: l, it: u } = e,
            { oneOf: d } = l;
          if (!u.opts.discriminator)
            throw Error(`discriminator: requires discriminator option`);
          let f = c.propertyName;
          if (typeof f != `string`)
            throw Error(`discriminator: requires propertyName`);
          if (c.mapping) throw Error(`discriminator: mapping is not supported`);
          if (!d) throw Error(`discriminator: requires oneOf keyword`);
          let p = o.let(`valid`, !1),
            m = o.const(`tag`, (0, t._)`${s}${(0, t.getProperty)(f)}`);
          (o.if(
            (0, t._)`typeof ${m} == "string"`,
            () => h(),
            () =>
              e.error(!1, { discrError: n.DiscrError.Tag, tag: m, tagName: f }),
          ),
            e.ok(p));
          function h() {
            let r = _();
            o.if(!1);
            for (let e in r)
              (o.elseIf((0, t._)`${m} === ${e}`), o.assign(p, g(r[e])));
            (o.else(),
              e.error(!1, {
                discrError: n.DiscrError.Mapping,
                tag: m,
                tagName: f,
              }),
              o.endIf());
          }
          function g(n) {
            let r = o.name(`valid`),
              i = e.subschema({ keyword: `oneOf`, schemaProp: n }, r);
            return (e.mergeEvaluated(i, t.Name), r);
          }
          function _() {
            let e = {},
              t = o(l),
              n = !0;
            for (let e = 0; e < d.length; e++) {
              let c = d[e];
              if (c?.$ref && !(0, a.schemaHasRulesButRef)(c, u.self.RULES)) {
                let e = c.$ref;
                if (
                  ((c = r.resolveRef.call(
                    u.self,
                    u.schemaEnv.root,
                    u.baseId,
                    e,
                  )),
                  c instanceof r.SchemaEnv && (c = c.schema),
                  c === void 0)
                )
                  throw new i.default(u.opts.uriResolver, u.baseId, e);
              }
              let l = c?.properties?.[f];
              if (typeof l != `object`)
                throw Error(
                  `discriminator: oneOf subschemas (or referenced schemas) must have "properties/${f}"`,
                );
              ((n &&= t || o(c)), s(l, e));
            }
            if (!n) throw Error(`discriminator: "${f}" must be required`);
            return e;
            function o({ required: e }) {
              return Array.isArray(e) && e.includes(f);
            }
            function s(e, t) {
              if (e.const) c(e.const, t);
              else if (e.enum) for (let n of e.enum) c(n, t);
              else
                throw Error(
                  `discriminator: "properties/${f}" must have "const" or "enum"`,
                );
            }
            function c(t, n) {
              if (typeof t != `string` || t in e)
                throw Error(
                  `discriminator: "${f}" values must be unique strings`,
                );
              e[t] = n;
            }
          }
        },
      };
    }),
    Zu = l({
      $id: () => $u,
      $schema: () => Qu,
      default: () => id,
      definitions: () => td,
      properties: () => rd,
      title: () => ed,
      type: () => nd,
    }),
    Qu,
    $u,
    ed,
    td,
    nd,
    rd,
    id,
    ad = s(() => {
      ((Qu = `http://json-schema.org/draft-07/schema#`),
        ($u = `http://json-schema.org/draft-07/schema#`),
        (ed = `Core schema meta-schema`),
        (td = {
          schemaArray: { type: `array`, minItems: 1, items: { $ref: `#` } },
          nonNegativeInteger: { type: `integer`, minimum: 0 },
          nonNegativeIntegerDefault0: {
            allOf: [
              { $ref: `#/definitions/nonNegativeInteger` },
              { default: 0 },
            ],
          },
          simpleTypes: {
            enum: [
              `array`,
              `boolean`,
              `integer`,
              `null`,
              `number`,
              `object`,
              `string`,
            ],
          },
          stringArray: {
            type: `array`,
            items: { type: `string` },
            uniqueItems: !0,
            default: [],
          },
        }),
        (nd = [`object`, `boolean`]),
        (rd = {
          $id: { type: `string`, format: `uri-reference` },
          $schema: { type: `string`, format: `uri` },
          $ref: { type: `string`, format: `uri-reference` },
          $comment: { type: `string` },
          title: { type: `string` },
          description: { type: `string` },
          default: !0,
          readOnly: { type: `boolean`, default: !1 },
          examples: { type: `array`, items: !0 },
          multipleOf: { type: `number`, exclusiveMinimum: 0 },
          maximum: { type: `number` },
          exclusiveMaximum: { type: `number` },
          minimum: { type: `number` },
          exclusiveMinimum: { type: `number` },
          maxLength: { $ref: `#/definitions/nonNegativeInteger` },
          minLength: { $ref: `#/definitions/nonNegativeIntegerDefault0` },
          pattern: { type: `string`, format: `regex` },
          additionalItems: { $ref: `#` },
          items: {
            anyOf: [{ $ref: `#` }, { $ref: `#/definitions/schemaArray` }],
            default: !0,
          },
          maxItems: { $ref: `#/definitions/nonNegativeInteger` },
          minItems: { $ref: `#/definitions/nonNegativeIntegerDefault0` },
          uniqueItems: { type: `boolean`, default: !1 },
          contains: { $ref: `#` },
          maxProperties: { $ref: `#/definitions/nonNegativeInteger` },
          minProperties: { $ref: `#/definitions/nonNegativeIntegerDefault0` },
          required: { $ref: `#/definitions/stringArray` },
          additionalProperties: { $ref: `#` },
          definitions: {
            type: `object`,
            additionalProperties: { $ref: `#` },
            default: {},
          },
          properties: {
            type: `object`,
            additionalProperties: { $ref: `#` },
            default: {},
          },
          patternProperties: {
            type: `object`,
            additionalProperties: { $ref: `#` },
            propertyNames: { format: `regex` },
            default: {},
          },
          dependencies: {
            type: `object`,
            additionalProperties: {
              anyOf: [{ $ref: `#` }, { $ref: `#/definitions/stringArray` }],
            },
          },
          propertyNames: { $ref: `#` },
          const: !0,
          enum: { type: `array`, items: !0, minItems: 1, uniqueItems: !0 },
          type: {
            anyOf: [
              { $ref: `#/definitions/simpleTypes` },
              {
                type: `array`,
                items: { $ref: `#/definitions/simpleTypes` },
                minItems: 1,
                uniqueItems: !0,
              },
            ],
          },
          format: { type: `string` },
          contentMediaType: { type: `string` },
          contentEncoding: { type: `string` },
          if: { $ref: `#` },
          then: { $ref: `#` },
          else: { $ref: `#` },
          allOf: { $ref: `#/definitions/schemaArray` },
          anyOf: { $ref: `#/definitions/schemaArray` },
          oneOf: { $ref: `#/definitions/schemaArray` },
          not: { $ref: `#` },
        }),
        (id = {
          $schema: Qu,
          $id: $u,
          title: ed,
          definitions: td,
          type: nd,
          properties: rd,
          default: !0,
        }));
    }),
    od = c((e, t) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.MissingRefError =
          e.ValidationError =
          e.CodeGen =
          e.Name =
          e.nil =
          e.stringify =
          e.str =
          e._ =
          e.KeywordCxt =
          e.Ajv =
            void 0));
      var n = du(),
        r = Ju(),
        i = Xu(),
        a = (ad(), f(Zu).default),
        o = [`/properties`],
        s = `http://json-schema.org/draft-07/schema`,
        c = class extends n.default {
          _addVocabularies() {
            (super._addVocabularies(),
              r.default.forEach((e) => this.addVocabulary(e)),
              this.opts.discriminator && this.addKeyword(i.default));
          }
          _addDefaultMetaSchema() {
            if ((super._addDefaultMetaSchema(), !this.opts.meta)) return;
            let e = this.opts.$data ? this.$dataMetaSchema(a, o) : a;
            (this.addMetaSchema(e, s, !1),
              (this.refs[`http://json-schema.org/schema`] = s));
          }
          defaultMeta() {
            return (this.opts.defaultMeta =
              super.defaultMeta() || (this.getSchema(s) ? s : void 0));
          }
        };
      ((e.Ajv = c),
        (t.exports = e = c),
        (t.exports.Ajv = c),
        Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.default = c));
      var l = Yl();
      Object.defineProperty(e, "KeywordCxt", {
        enumerable: !0,
        get: function () {
          return l.KeywordCxt;
        },
      });
      var u = W();
      (Object.defineProperty(e, "_", {
        enumerable: !0,
        get: function () {
          return u._;
        },
      }),
        Object.defineProperty(e, "str", {
          enumerable: !0,
          get: function () {
            return u.str;
          },
        }),
        Object.defineProperty(e, "stringify", {
          enumerable: !0,
          get: function () {
            return u.stringify;
          },
        }),
        Object.defineProperty(e, "nil", {
          enumerable: !0,
          get: function () {
            return u.nil;
          },
        }),
        Object.defineProperty(e, "Name", {
          enumerable: !0,
          get: function () {
            return u.Name;
          },
        }),
        Object.defineProperty(e, "CodeGen", {
          enumerable: !0,
          get: function () {
            return u.CodeGen;
          },
        }));
      var d = Xl();
      Object.defineProperty(e, "ValidationError", {
        enumerable: !0,
        get: function () {
          return d.default;
        },
      });
      var p = Zl();
      Object.defineProperty(e, "MissingRefError", {
        enumerable: !0,
        get: function () {
          return p.default;
        },
      });
    }),
    sd = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.formatNames = e.fastFormats = e.fullFormats = void 0));
      function t(e, t) {
        return { validate: e, compare: t };
      }
      ((e.fullFormats = {
        date: t(a, o),
        time: t(c(!0), l),
        "date-time": t(f(!0), p),
        "iso-time": t(c(), u),
        "iso-date-time": t(f(), m),
        duration:
          /^P(?!$)((\d+Y)?(\d+M)?(\d+D)?(T(?=\d)(\d+H)?(\d+M)?(\d+S)?)?|(\d+W)?)$/,
        uri: _,
        "uri-reference":
          /^(?:[a-z][a-z0-9+\-.]*:)?(?:\/?\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:]|%[0-9a-f]{2})*@)?(?:\[(?:(?:(?:(?:[0-9a-f]{1,4}:){6}|::(?:[0-9a-f]{1,4}:){5}|(?:[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){4}|(?:(?:[0-9a-f]{1,4}:){0,1}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){3}|(?:(?:[0-9a-f]{1,4}:){0,2}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){2}|(?:(?:[0-9a-f]{1,4}:){0,3}[0-9a-f]{1,4})?::[0-9a-f]{1,4}:|(?:(?:[0-9a-f]{1,4}:){0,4}[0-9a-f]{1,4})?::)(?:[0-9a-f]{1,4}:[0-9a-f]{1,4}|(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?))|(?:(?:[0-9a-f]{1,4}:){0,5}[0-9a-f]{1,4})?::[0-9a-f]{1,4}|(?:(?:[0-9a-f]{1,4}:){0,6}[0-9a-f]{1,4})?::)|[Vv][0-9a-f]+\.[a-z0-9\-._~!$&'()*+,;=:]+)\]|(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)|(?:[a-z0-9\-._~!$&'"()*+,;=]|%[0-9a-f]{2})*)(?::\d*)?(?:\/(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})*)*|\/(?:(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})*)*)?|(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})*)*)?(?:\?(?:[a-z0-9\-._~!$&'"()*+,;=:@/?]|%[0-9a-f]{2})*)?(?:#(?:[a-z0-9\-._~!$&'"()*+,;=:@/?]|%[0-9a-f]{2})*)?$/i,
        "uri-template":
          /^(?:(?:[^\x00-\x20"'<>%\\^`{|}]|%[0-9a-f]{2})|\{[+#./;?&=,!@|]?(?:[a-z0-9_]|%[0-9a-f]{2})+(?::[1-9][0-9]{0,3}|\*)?(?:,(?:[a-z0-9_]|%[0-9a-f]{2})+(?::[1-9][0-9]{0,3}|\*)?)*\})*$/i,
        url: /^(?:https?|ftp):\/\/(?:\S+(?::\S*)?@)?(?:(?!(?:10|127)(?:\.\d{1,3}){3})(?!(?:169\.254|192\.168)(?:\.\d{1,3}){2})(?!172\.(?:1[6-9]|2\d|3[0-1])(?:\.\d{1,3}){2})(?:[1-9]\d?|1\d\d|2[01]\d|22[0-3])(?:\.(?:1?\d{1,2}|2[0-4]\d|25[0-5])){2}(?:\.(?:[1-9]\d?|1\d\d|2[0-4]\d|25[0-4]))|(?:(?:[a-z0-9\u{00a1}-\u{ffff}]+-)*[a-z0-9\u{00a1}-\u{ffff}]+)(?:\.(?:[a-z0-9\u{00a1}-\u{ffff}]+-)*[a-z0-9\u{00a1}-\u{ffff}]+)*(?:\.(?:[a-z\u{00a1}-\u{ffff}]{2,})))(?::\d{2,5})?(?:\/[^\s]*)?$/iu,
        email:
          /^[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*@(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/i,
        hostname:
          /^(?=.{1,253}\.?$)[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[-0-9a-z]{0,61}[0-9a-z])?)*\.?$/i,
        ipv4: /^(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/,
        ipv6: /^((([0-9a-f]{1,4}:){7}([0-9a-f]{1,4}|:))|(([0-9a-f]{1,4}:){6}(:[0-9a-f]{1,4}|((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9a-f]{1,4}:){5}(((:[0-9a-f]{1,4}){1,2})|:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9a-f]{1,4}:){4}(((:[0-9a-f]{1,4}){1,3})|((:[0-9a-f]{1,4})?:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-f]{1,4}:){3}(((:[0-9a-f]{1,4}){1,4})|((:[0-9a-f]{1,4}){0,2}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-f]{1,4}:){2}(((:[0-9a-f]{1,4}){1,5})|((:[0-9a-f]{1,4}){0,3}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-f]{1,4}:){1}(((:[0-9a-f]{1,4}){1,6})|((:[0-9a-f]{1,4}){0,4}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(:(((:[0-9a-f]{1,4}){1,7})|((:[0-9a-f]{1,4}){0,5}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:)))$/i,
        regex: te,
        uuid: /^(?:urn:uuid:)?[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i,
        "json-pointer": /^(?:\/(?:[^~/]|~0|~1)*)*$/,
        "json-pointer-uri-fragment":
          /^#(?:\/(?:[a-z0-9_\-.!$&'()*+,;:=@]|%[0-9a-f]{2}|~0|~1)*)*$/i,
        "relative-json-pointer":
          /^(?:0|[1-9][0-9]*)(?:#|(?:\/(?:[^~/]|~0|~1)*)*)$/,
        byte: y,
        int32: { type: `number`, validate: ee },
        int64: { type: `number`, validate: S },
        float: { type: `number`, validate: C },
        double: { type: `number`, validate: C },
        password: !0,
        binary: !0,
      }),
        (e.fastFormats = {
          ...e.fullFormats,
          date: t(/^\d\d\d\d-[0-1]\d-[0-3]\d$/, o),
          time: t(
            /^(?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)(?:\.\d+)?(?:z|[+-]\d\d(?::?\d\d)?)$/i,
            l,
          ),
          "date-time": t(
            /^\d\d\d\d-[0-1]\d-[0-3]\dt(?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)(?:\.\d+)?(?:z|[+-]\d\d(?::?\d\d)?)$/i,
            p,
          ),
          "iso-time": t(
            /^(?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)(?:\.\d+)?(?:z|[+-]\d\d(?::?\d\d)?)?$/i,
            u,
          ),
          "iso-date-time": t(
            /^\d\d\d\d-[0-1]\d-[0-3]\d[t\s](?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)(?:\.\d+)?(?:z|[+-]\d\d(?::?\d\d)?)?$/i,
            m,
          ),
          uri: /^(?:[a-z][a-z0-9+\-.]*:)(?:\/?\/)?[^\s]*$/i,
          "uri-reference":
            /^(?:(?:[a-z][a-z0-9+\-.]*:)?\/?\/)?(?:[^\\\s#][^\s#]*)?(?:#[^\\\s]*)?$/i,
          email:
            /^[a-z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$/i,
        }),
        (e.formatNames = Object.keys(e.fullFormats)));
      function n(e) {
        return e % 4 == 0 && (e % 100 != 0 || e % 400 == 0);
      }
      var r = /^(\d\d\d\d)-(\d\d)-(\d\d)$/,
        i = [0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
      function a(e) {
        let t = r.exec(e);
        if (!t) return !1;
        let a = +t[1],
          o = +t[2],
          s = +t[3];
        return (
          o >= 1 && o <= 12 && s >= 1 && s <= (o === 2 && n(a) ? 29 : i[o])
        );
      }
      function o(e, t) {
        if (e && t) return e > t ? 1 : e < t ? -1 : 0;
      }
      var s = /^(\d\d):(\d\d):(\d\d(?:\.\d+)?)(z|([+-])(\d\d)(?::?(\d\d))?)?$/i;
      function c(e) {
        return function (t) {
          let n = s.exec(t);
          if (!n) return !1;
          let r = +n[1],
            i = +n[2],
            a = +n[3],
            o = n[4],
            c = n[5] === `-` ? -1 : 1,
            l = +(n[6] || 0),
            u = +(n[7] || 0);
          if (l > 23 || u > 59 || (e && !o)) return !1;
          if (r <= 23 && i <= 59 && a < 60) return !0;
          let d = i - u * c,
            f = r - l * c - +(d < 0);
          return (f === 23 || f === -1) && (d === 59 || d === -1) && a < 61;
        };
      }
      function l(e, t) {
        if (!(e && t)) return;
        let n = new Date(`2020-01-01T` + e).valueOf(),
          r = new Date(`2020-01-01T` + t).valueOf();
        if (n && r) return n - r;
      }
      function u(e, t) {
        if (!(e && t)) return;
        let n = s.exec(e),
          r = s.exec(t);
        if (n && r)
          return (
            (e = n[1] + n[2] + n[3]),
            (t = r[1] + r[2] + r[3]),
            e > t ? 1 : e < t ? -1 : 0
          );
      }
      var d = /t|\s/i;
      function f(e) {
        let t = c(e);
        return function (e) {
          let n = e.split(d);
          return n.length === 2 && a(n[0]) && t(n[1]);
        };
      }
      function p(e, t) {
        if (!(e && t)) return;
        let n = new Date(e).valueOf(),
          r = new Date(t).valueOf();
        if (n && r) return n - r;
      }
      function m(e, t) {
        if (!(e && t)) return;
        let [n, r] = e.split(d),
          [i, a] = t.split(d),
          s = o(n, i);
        if (s !== void 0) return s || l(r, a);
      }
      var h = /\/|:/,
        g =
          /^(?:[a-z][a-z0-9+\-.]*:)(?:\/?\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:]|%[0-9a-f]{2})*@)?(?:\[(?:(?:(?:(?:[0-9a-f]{1,4}:){6}|::(?:[0-9a-f]{1,4}:){5}|(?:[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){4}|(?:(?:[0-9a-f]{1,4}:){0,1}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){3}|(?:(?:[0-9a-f]{1,4}:){0,2}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){2}|(?:(?:[0-9a-f]{1,4}:){0,3}[0-9a-f]{1,4})?::[0-9a-f]{1,4}:|(?:(?:[0-9a-f]{1,4}:){0,4}[0-9a-f]{1,4})?::)(?:[0-9a-f]{1,4}:[0-9a-f]{1,4}|(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?))|(?:(?:[0-9a-f]{1,4}:){0,5}[0-9a-f]{1,4})?::[0-9a-f]{1,4}|(?:(?:[0-9a-f]{1,4}:){0,6}[0-9a-f]{1,4})?::)|[Vv][0-9a-f]+\.[a-z0-9\-._~!$&'()*+,;=:]+)\]|(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)|(?:[a-z0-9\-._~!$&'()*+,;=]|%[0-9a-f]{2})*)(?::\d*)?(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*|\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*)?|(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*)(?:\?(?:[a-z0-9\-._~!$&'()*+,;=:@/?]|%[0-9a-f]{2})*)?(?:#(?:[a-z0-9\-._~!$&'()*+,;=:@/?]|%[0-9a-f]{2})*)?$/i;
      function _(e) {
        return h.test(e) && g.test(e);
      }
      var v =
        /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/gm;
      function y(e) {
        return ((v.lastIndex = 0), v.test(e));
      }
      var b = -(2 ** 31),
        x = 2 ** 31 - 1;
      function ee(e) {
        return Number.isInteger(e) && e <= x && e >= b;
      }
      function S(e) {
        return Number.isInteger(e);
      }
      function C() {
        return !0;
      }
      var w = /[^\\]\\Z/;
      function te(e) {
        if (w.test(e)) return !1;
        try {
          return (new RegExp(e), !0);
        } catch {
          return !1;
        }
      }
    }),
    cd = c((e) => {
      (Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.formatLimitDefinition = void 0));
      var t = od(),
        n = W(),
        r = n.operators,
        i = {
          formatMaximum: { okStr: `<=`, ok: r.LTE, fail: r.GT },
          formatMinimum: { okStr: `>=`, ok: r.GTE, fail: r.LT },
          formatExclusiveMaximum: { okStr: `<`, ok: r.LT, fail: r.GTE },
          formatExclusiveMinimum: { okStr: `>`, ok: r.GT, fail: r.LTE },
        };
      ((e.formatLimitDefinition = {
        keyword: Object.keys(i),
        type: `string`,
        schemaType: `string`,
        $data: !0,
        error: {
          message: ({ keyword: e, schemaCode: t }) =>
            (0, n.str)`should be ${i[e].okStr} ${t}`,
          params: ({ keyword: e, schemaCode: t }) =>
            (0, n._)`{comparison: ${i[e].okStr}, limit: ${t}}`,
        },
        code(e) {
          let { gen: r, data: a, schemaCode: o, keyword: s, it: c } = e,
            { opts: l, self: u } = c;
          if (!l.validateFormats) return;
          let d = new t.KeywordCxt(c, u.RULES.all.format.definition, `format`);
          d.$data ? f() : p();
          function f() {
            let t = r.scopeValue(`formats`, {
                ref: u.formats,
                code: l.code.formats,
              }),
              i = r.const(`fmt`, (0, n._)`${t}[${d.schemaCode}]`);
            e.fail$data(
              (0, n.or)(
                (0, n._)`typeof ${i} != "object"`,
                (0, n._)`${i} instanceof RegExp`,
                (0, n._)`typeof ${i}.compare != "function"`,
                m(i),
              ),
            );
          }
          function p() {
            let t = d.schema,
              i = u.formats[t];
            if (!i || i === !0) return;
            if (
              typeof i != `object` ||
              i instanceof RegExp ||
              typeof i.compare != `function`
            )
              throw Error(
                `"${s}": format "${t}" does not define "compare" function`,
              );
            let a = r.scopeValue(`formats`, {
              key: t,
              ref: i,
              code: l.code.formats
                ? (0, n._)`${l.code.formats}${(0, n.getProperty)(t)}`
                : void 0,
            });
            e.fail$data(m(a));
          }
          function m(e) {
            return (0, n._)`${e}.compare(${a}, ${o}) ${i[s].fail} 0`;
          }
        },
        dependencies: [`format`],
      }),
        (e.default = (t) => (t.addKeyword(e.formatLimitDefinition), t)));
    }),
    ld = c((e, t) => {
      Object.defineProperty(e, "__esModule", { value: !0 });
      var n = sd(),
        r = cd(),
        i = W(),
        a = new i.Name(`fullFormats`),
        o = new i.Name(`fastFormats`),
        s = (e, t = { keywords: !0 }) => {
          if (Array.isArray(t)) return (c(e, t, n.fullFormats, a), e);
          let [i, s] =
            t.mode === `fast` ? [n.fastFormats, o] : [n.fullFormats, a];
          return (
            c(e, t.formats || n.formatNames, i, s),
            t.keywords && (0, r.default)(e),
            e
          );
        };
      s.get = (e, t = `full`) => {
        let r = (t === `fast` ? n.fastFormats : n.fullFormats)[e];
        if (!r) throw Error(`Unknown format "${e}"`);
        return r;
      };
      function c(e, t, n, r) {
        var a;
        (a = e.opts.code).formats ??
          (a.formats = (0, i._)`require("ajv-formats/dist/formats").${r}`);
        for (let r of t) e.addFormat(r, n[r]);
      }
      ((t.exports = e = s),
        Object.defineProperty(e, "__esModule", { value: !0 }),
        (e.default = s));
    }),
    ud = d(od(), 1),
    dd = d(ld(), 1);
  function fd() {
    let e = new ud.default({
      strict: !1,
      validateFormats: !0,
      validateSchema: !1,
      allErrors: !0,
    });
    return ((0, dd.default)(e), e);
  }
  var pd = class {
      constructor(e) {
        this._ajv = e ?? fd();
      }
      getValidator(e) {
        let t =
          `$id` in e && typeof e.$id == `string`
            ? (this._ajv.getSchema(e.$id) ?? this._ajv.compile(e))
            : this._ajv.compile(e);
        return (e) =>
          t(e)
            ? { valid: !0, data: e, errorMessage: void 0 }
            : {
                valid: !1,
                data: void 0,
                errorMessage: this._ajv.errorsText(t.errors),
              };
      }
    },
    md = class {
      constructor(e) {
        this._client = e;
      }
      async *callToolStream(e, t = Wc, n) {
        let r = this._client,
          i = { ...n, task: n?.task ?? (r.isToolTask(e.name) ? {} : void 0) },
          a = r.requestStream({ method: `tools/call`, params: e }, t, i),
          o = r.getToolOutputValidator(e.name);
        for await (let t of a) {
          if (t.type === `result` && o) {
            let n = t.result;
            if (!n.structuredContent && !n.isError) {
              yield {
                type: `error`,
                error: new U(
                  H.InvalidRequest,
                  `Tool ${e.name} has an output schema but did not return structured content`,
                ),
              };
              return;
            }
            if (n.structuredContent)
              try {
                let e = o(n.structuredContent);
                if (!e.valid) {
                  yield {
                    type: `error`,
                    error: new U(
                      H.InvalidParams,
                      `Structured content does not match the tool's output schema: ${e.errorMessage}`,
                    ),
                  };
                  return;
                }
              } catch (e) {
                if (e instanceof U) {
                  yield { type: `error`, error: e };
                  return;
                }
                yield {
                  type: `error`,
                  error: new U(
                    H.InvalidParams,
                    `Failed to validate structured content: ${e instanceof Error ? e.message : String(e)}`,
                  ),
                };
                return;
              }
          }
          yield t;
        }
      }
      async getTask(e, t) {
        return this._client.getTask({ taskId: e }, t);
      }
      async getTaskResult(e, t, n) {
        return this._client.getTaskResult({ taskId: e }, t, n);
      }
      async listTasks(e, t) {
        return this._client.listTasks(e ? { cursor: e } : void 0, t);
      }
      async cancelTask(e, t) {
        return this._client.cancelTask({ taskId: e }, t);
      }
      requestStream(e, t, n) {
        return this._client.requestStream(e, t, n);
      }
    };
  function hd(e, t, n) {
    if (!e)
      throw Error(`${n} does not support task creation (required for ${t})`);
    switch (t) {
      case `tools/call`:
        if (!e.tools?.call)
          throw Error(
            `${n} does not support task creation for tools/call (required for ${t})`,
          );
        break;
      default:
        break;
    }
  }
  function gd(e, t, n) {
    if (!e)
      throw Error(`${n} does not support task creation (required for ${t})`);
    switch (t) {
      case `sampling/createMessage`:
        if (!e.sampling?.createMessage)
          throw Error(
            `${n} does not support task creation for sampling/createMessage (required for ${t})`,
          );
        break;
      case `elicitation/create`:
        if (!e.elicitation?.create)
          throw Error(
            `${n} does not support task creation for elicitation/create (required for ${t})`,
          );
        break;
      default:
        break;
    }
  }
  function _d(e, t) {
    if (!(!e || typeof t != `object` || !t)) {
      if (
        e.type === `object` &&
        e.properties &&
        typeof e.properties == `object`
      ) {
        let n = t,
          r = e.properties;
        for (let e of Object.keys(r)) {
          let t = r[e];
          (n[e] === void 0 &&
            Object.prototype.hasOwnProperty.call(t, `default`) &&
            (n[e] = t.default),
            n[e] !== void 0 && _d(t, n[e]));
        }
      }
      if (Array.isArray(e.anyOf))
        for (let n of e.anyOf) typeof n != `boolean` && _d(n, t);
      if (Array.isArray(e.oneOf))
        for (let n of e.oneOf) typeof n != `boolean` && _d(n, t);
    }
  }
  function vd(e) {
    if (!e) return { supportsFormMode: !1, supportsUrlMode: !1 };
    let t = e.form !== void 0,
      n = e.url !== void 0;
    return { supportsFormMode: t || (!t && !n), supportsUrlMode: n };
  }
  var yd = class extends Pl {
    constructor(e, t) {
      (super(t),
        (this._clientInfo = e),
        (this._cachedToolOutputValidators = new Map()),
        (this._cachedKnownTaskTools = new Set()),
        (this._cachedRequiredTaskTools = new Set()),
        (this._listChangedDebounceTimers = new Map()),
        (this._capabilities = t?.capabilities ?? {}),
        (this._jsonSchemaValidator = t?.jsonSchemaValidator ?? new pd()),
        t?.listChanged && (this._pendingListChangedConfig = t.listChanged));
    }
    _setupListChangedHandlers(e) {
      (e.tools &&
        this._serverCapabilities?.tools?.listChanged &&
        this._setupListChangedHandler(
          `tools`,
          qc,
          e.tools,
          async () => (await this.listTools()).tools,
        ),
        e.prompts &&
          this._serverCapabilities?.prompts?.listChanged &&
          this._setupListChangedHandler(
            `prompts`,
            Rc,
            e.prompts,
            async () => (await this.listPrompts()).prompts,
          ),
        e.resources &&
          this._serverCapabilities?.resources?.listChanged &&
          this._setupListChangedHandler(
            `resources`,
            _c,
            e.resources,
            async () => (await this.listResources()).resources,
          ));
    }
    get experimental() {
      return (
        (this._experimental ||= { tasks: new md(this) }), this._experimental
      );
    }
    registerCapabilities(e) {
      if (this.transport)
        throw Error(
          `Cannot register capabilities after connecting to transport`,
        );
      this._capabilities = Il(this._capabilities, e);
    }
    setRequestHandler(e, t) {
      let n = ta(e)?.method;
      if (!n) throw Error(`Schema is missing a method literal`);
      let r;
      if ($i(n)) {
        let e = n;
        r = e._zod?.def?.value ?? e.value;
      } else {
        let e = n;
        r = e._def?.value ?? e.value;
      }
      if (typeof r != `string`)
        throw Error(`Schema method literal must be a string`);
      let i = r;
      return i === `elicitation/create`
        ? super.setRequestHandler(e, async (e, n) => {
            let r = ea(_l, e);
            if (!r.success) {
              let e =
                r.error instanceof Error ? r.error.message : String(r.error);
              throw new U(H.InvalidParams, `Invalid elicitation request: ${e}`);
            }
            let { params: i } = r.data;
            i.mode = i.mode ?? `form`;
            let { supportsFormMode: a, supportsUrlMode: o } = vd(
              this._capabilities.elicitation,
            );
            if (i.mode === `form` && !a)
              throw new U(
                H.InvalidParams,
                `Client does not support form-mode elicitation requests`,
              );
            if (i.mode === `url` && !o)
              throw new U(
                H.InvalidParams,
                `Client does not support URL-mode elicitation requests`,
              );
            let s = await Promise.resolve(t(e, n));
            if (i.task) {
              let e = ea(Gs, s);
              if (!e.success) {
                let t =
                  e.error instanceof Error ? e.error.message : String(e.error);
                throw new U(
                  H.InvalidParams,
                  `Invalid task creation result: ${t}`,
                );
              }
              return e.data;
            }
            let c = ea(bl, s);
            if (!c.success) {
              let e =
                c.error instanceof Error ? c.error.message : String(c.error);
              throw new U(H.InvalidParams, `Invalid elicitation result: ${e}`);
            }
            let l = c.data,
              u = i.mode === `form` ? i.requestedSchema : void 0;
            if (
              i.mode === `form` &&
              l.action === `accept` &&
              l.content &&
              u &&
              this._capabilities.elicitation?.form?.applyDefaults
            )
              try {
                _d(u, l.content);
              } catch {}
            return l;
          })
        : i === `sampling/createMessage`
          ? super.setRequestHandler(e, async (e, n) => {
              let r = ea(sl, e);
              if (!r.success) {
                let e =
                  r.error instanceof Error ? r.error.message : String(r.error);
                throw new U(H.InvalidParams, `Invalid sampling request: ${e}`);
              }
              let { params: i } = r.data,
                a = await Promise.resolve(t(e, n));
              if (i.task) {
                let e = ea(Gs, a);
                if (!e.success) {
                  let t =
                    e.error instanceof Error
                      ? e.error.message
                      : String(e.error);
                  throw new U(
                    H.InvalidParams,
                    `Invalid task creation result: ${t}`,
                  );
                }
                return e.data;
              }
              let o = ea(i.tools || i.toolChoice ? ll : cl, a);
              if (!o.success) {
                let e =
                  o.error instanceof Error ? o.error.message : String(o.error);
                throw new U(H.InvalidParams, `Invalid sampling result: ${e}`);
              }
              return o.data;
            })
          : super.setRequestHandler(e, t);
    }
    assertCapability(e, t) {
      if (!this._serverCapabilities?.[e])
        throw Error(`Server does not support ${e} (required for ${t})`);
    }
    async connect(e, t) {
      if ((await super.connect(e), e.sessionId === void 0))
        try {
          let n = await this.request(
            {
              method: `initialize`,
              params: {
                protocolVersion: Jo,
                capabilities: this._capabilities,
                clientInfo: this._clientInfo,
              },
            },
            Ns,
            t,
          );
          if (n === void 0)
            throw Error(`Server sent invalid initialize result: ${n}`);
          if (!Yo.includes(n.protocolVersion))
            throw Error(
              `Server's protocol version is not supported: ${n.protocolVersion}`,
            );
          ((this._serverCapabilities = n.capabilities),
            (this._serverVersion = n.serverInfo),
            e.setProtocolVersion && e.setProtocolVersion(n.protocolVersion),
            (this._instructions = n.instructions),
            await this.notification({ method: `notifications/initialized` }),
            (this._pendingListChangedConfig &&=
              (this._setupListChangedHandlers(this._pendingListChangedConfig),
              void 0)));
        } catch (e) {
          throw (this.close(), e);
        }
    }
    getServerCapabilities() {
      return this._serverCapabilities;
    }
    getServerVersion() {
      return this._serverVersion;
    }
    getInstructions() {
      return this._instructions;
    }
    assertCapabilityForMethod(e) {
      switch (e) {
        case `logging/setLevel`:
          if (!this._serverCapabilities?.logging)
            throw Error(`Server does not support logging (required for ${e})`);
          break;
        case `prompts/get`:
        case `prompts/list`:
          if (!this._serverCapabilities?.prompts)
            throw Error(`Server does not support prompts (required for ${e})`);
          break;
        case `resources/list`:
        case `resources/templates/list`:
        case `resources/read`:
        case `resources/subscribe`:
        case `resources/unsubscribe`:
          if (!this._serverCapabilities?.resources)
            throw Error(
              `Server does not support resources (required for ${e})`,
            );
          if (
            e === `resources/subscribe` &&
            !this._serverCapabilities.resources.subscribe
          )
            throw Error(
              `Server does not support resource subscriptions (required for ${e})`,
            );
          break;
        case `tools/call`:
        case `tools/list`:
          if (!this._serverCapabilities?.tools)
            throw Error(`Server does not support tools (required for ${e})`);
          break;
        case `completion/complete`:
          if (!this._serverCapabilities?.completions)
            throw Error(
              `Server does not support completions (required for ${e})`,
            );
          break;
        case `initialize`:
          break;
        case `ping`:
          break;
      }
    }
    assertNotificationCapability(e) {
      switch (e) {
        case `notifications/roots/list_changed`:
          if (!this._capabilities.roots?.listChanged)
            throw Error(
              `Client does not support roots list changed notifications (required for ${e})`,
            );
          break;
        case `notifications/initialized`:
          break;
        case `notifications/cancelled`:
          break;
        case `notifications/progress`:
          break;
      }
    }
    assertRequestHandlerCapability(e) {
      if (this._capabilities)
        switch (e) {
          case `sampling/createMessage`:
            if (!this._capabilities.sampling)
              throw Error(
                `Client does not support sampling capability (required for ${e})`,
              );
            break;
          case `elicitation/create`:
            if (!this._capabilities.elicitation)
              throw Error(
                `Client does not support elicitation capability (required for ${e})`,
              );
            break;
          case `roots/list`:
            if (!this._capabilities.roots)
              throw Error(
                `Client does not support roots capability (required for ${e})`,
              );
            break;
          case `tasks/get`:
          case `tasks/list`:
          case `tasks/result`:
          case `tasks/cancel`:
            if (!this._capabilities.tasks)
              throw Error(
                `Client does not support tasks capability (required for ${e})`,
              );
            break;
          case `ping`:
            break;
        }
    }
    assertTaskCapability(e) {
      hd(this._serverCapabilities?.tasks?.requests, e, `Server`);
    }
    assertTaskHandlerCapability(e) {
      this._capabilities && gd(this._capabilities.tasks?.requests, e, `Client`);
    }
    async ping(e) {
      return this.request({ method: `ping` }, bs, e);
    }
    async complete(e, t) {
      return this.request({ method: `completion/complete`, params: e }, Tl, t);
    }
    async setLoggingLevel(e, t) {
      return this.request(
        { method: `logging/setLevel`, params: { level: e } },
        bs,
        t,
      );
    }
    async getPrompt(e, t) {
      return this.request({ method: `prompts/get`, params: e }, Lc, t);
    }
    async listPrompts(e, t) {
      return this.request({ method: `prompts/list`, params: e }, Dc, t);
    }
    async listResources(e, t) {
      return this.request({ method: `resources/list`, params: e }, uc, t);
    }
    async listResourceTemplates(e, t) {
      return this.request(
        { method: `resources/templates/list`, params: e },
        fc,
        t,
      );
    }
    async readResource(e, t) {
      return this.request({ method: `resources/read`, params: e }, gc, t);
    }
    async subscribeResource(e, t) {
      return this.request({ method: `resources/subscribe`, params: e }, bs, t);
    }
    async unsubscribeResource(e, t) {
      return this.request(
        { method: `resources/unsubscribe`, params: e },
        bs,
        t,
      );
    }
    async callTool(e, t = Wc, n) {
      if (this.isToolTaskRequired(e.name))
        throw new U(
          H.InvalidRequest,
          `Tool "${e.name}" requires task-based execution. Use client.experimental.tasks.callToolStream() instead.`,
        );
      let r = await this.request({ method: `tools/call`, params: e }, t, n),
        i = this.getToolOutputValidator(e.name);
      if (i) {
        if (!r.structuredContent && !r.isError)
          throw new U(
            H.InvalidRequest,
            `Tool ${e.name} has an output schema but did not return structured content`,
          );
        if (r.structuredContent)
          try {
            let e = i(r.structuredContent);
            if (!e.valid)
              throw new U(
                H.InvalidParams,
                `Structured content does not match the tool's output schema: ${e.errorMessage}`,
              );
          } catch (e) {
            throw e instanceof U
              ? e
              : new U(
                  H.InvalidParams,
                  `Failed to validate structured content: ${e instanceof Error ? e.message : String(e)}`,
                );
          }
      }
      return r;
    }
    isToolTask(e) {
      return this._serverCapabilities?.tasks?.requests?.tools?.call
        ? this._cachedKnownTaskTools.has(e)
        : !1;
    }
    isToolTaskRequired(e) {
      return this._cachedRequiredTaskTools.has(e);
    }
    cacheToolMetadata(e) {
      (this._cachedToolOutputValidators.clear(),
        this._cachedKnownTaskTools.clear(),
        this._cachedRequiredTaskTools.clear());
      for (let t of e) {
        if (t.outputSchema) {
          let e = this._jsonSchemaValidator.getValidator(t.outputSchema);
          this._cachedToolOutputValidators.set(t.name, e);
        }
        let e = t.execution?.taskSupport;
        ((e === `required` || e === `optional`) &&
          this._cachedKnownTaskTools.add(t.name),
          e === `required` && this._cachedRequiredTaskTools.add(t.name));
      }
    }
    getToolOutputValidator(e) {
      return this._cachedToolOutputValidators.get(e);
    }
    async listTools(e, t) {
      let n = await this.request({ method: `tools/list`, params: e }, Uc, t);
      return (this.cacheToolMetadata(n.tools), n);
    }
    _setupListChangedHandler(e, t, n, r) {
      let i = Jc.safeParse(n);
      if (!i.success)
        throw Error(`Invalid ${e} listChanged options: ${i.error.message}`);
      if (typeof n.onChanged != `function`)
        throw Error(
          `Invalid ${e} listChanged options: onChanged must be a function`,
        );
      let { autoRefresh: a, debounceMs: o } = i.data,
        { onChanged: s } = n,
        c = async () => {
          if (!a) {
            s(null, null);
            return;
          }
          try {
            let e = await r();
            s(null, e);
          } catch (e) {
            let t = e instanceof Error ? e : Error(String(e));
            s(t, null);
          }
        };
      this.setNotificationHandler(t, () => {
        if (o) {
          let t = this._listChangedDebounceTimers.get(e);
          t && clearTimeout(t);
          let n = setTimeout(c, o);
          this._listChangedDebounceTimers.set(e, n);
        } else c();
      });
    }
    async sendRootsListChanged() {
      return this.notification({ method: `notifications/roots/list_changed` });
    }
  };
  function bd(e) {
    return e
      ? e instanceof Headers
        ? Object.fromEntries(e.entries())
        : Array.isArray(e)
          ? Object.fromEntries(e)
          : { ...e }
      : {};
  }
  function xd(e = fetch, t) {
    return t
      ? async (n, r) =>
          e(n, {
            ...t,
            ...r,
            headers: r?.headers
              ? { ...bd(t.headers), ...bd(r.headers) }
              : t.headers,
          })
      : e;
  }
  var Sd = globalThis.crypto;
  async function Y(e) {
    return (await Sd).getRandomValues(new Uint8Array(e));
  }
  async function Cd(e) {
    let t = ``;
    for (; t.length < e; ) {
      let n = await Y(e - t.length);
      for (let e of n)
        e < 198 &&
          (t +=
            `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~`[
              e % 66
            ]);
    }
    return t;
  }
  async function wd(e) {
    return await Cd(e);
  }
  async function Td(e) {
    let t = await (
      await Sd
    ).subtle.digest(`SHA-256`, new TextEncoder().encode(e));
    return btoa(String.fromCharCode(...new Uint8Array(t)))
      .replace(/\//g, `_`)
      .replace(/\+/g, `-`)
      .replace(/=/g, ``);
  }
  async function Ed(e) {
    if (((e ||= 43), e < 43 || e > 128))
      throw `Expected a length between 43 and 128. Received ${e}.`;
    let t = await wd(e);
    return { code_verifier: t, code_challenge: await Td(t) };
  }
  var Dd = Na()
      .superRefine((e, t) => {
        if (!URL.canParse(e))
          return (
            t.addIssue({
              code: Go.custom,
              message: `URL must be parseable`,
              fatal: !0,
            }),
            ee
          );
      })
      .refine(
        (e) => {
          let t = new URL(e);
          return (
            t.protocol !== `javascript:` &&
            t.protocol !== `data:` &&
            t.protocol !== `vbscript:`
          );
        },
        { message: `URL cannot use javascript:, data:, or vbscript: scheme` },
      ),
    Od = uo({
      resource: N().url(),
      authorization_servers: F(Dd).optional(),
      jwks_uri: N().url().optional(),
      scopes_supported: F(N()).optional(),
      bearer_methods_supported: F(N()).optional(),
      resource_signing_alg_values_supported: F(N()).optional(),
      resource_name: N().optional(),
      resource_documentation: N().optional(),
      resource_policy_uri: N().url().optional(),
      resource_tos_uri: N().url().optional(),
      tls_client_certificate_bound_access_tokens: $a().optional(),
      authorization_details_types_supported: F(N()).optional(),
      dpop_signing_alg_values_supported: F(N()).optional(),
      dpop_bound_access_tokens_required: $a().optional(),
    }),
    kd = uo({
      issuer: N(),
      authorization_endpoint: Dd,
      token_endpoint: Dd,
      registration_endpoint: Dd.optional(),
      scopes_supported: F(N()).optional(),
      response_types_supported: F(N()),
      response_modes_supported: F(N()).optional(),
      grant_types_supported: F(N()).optional(),
      token_endpoint_auth_methods_supported: F(N()).optional(),
      token_endpoint_auth_signing_alg_values_supported: F(N()).optional(),
      service_documentation: Dd.optional(),
      revocation_endpoint: Dd.optional(),
      revocation_endpoint_auth_methods_supported: F(N()).optional(),
      revocation_endpoint_auth_signing_alg_values_supported: F(N()).optional(),
      introspection_endpoint: N().optional(),
      introspection_endpoint_auth_methods_supported: F(N()).optional(),
      introspection_endpoint_auth_signing_alg_values_supported:
        F(N()).optional(),
      code_challenge_methods_supported: F(N()).optional(),
      client_id_metadata_document_supported: $a().optional(),
    }),
    Ad = I({
      ...uo({
        issuer: N(),
        authorization_endpoint: Dd,
        token_endpoint: Dd,
        userinfo_endpoint: Dd.optional(),
        jwks_uri: Dd,
        registration_endpoint: Dd.optional(),
        scopes_supported: F(N()).optional(),
        response_types_supported: F(N()),
        response_modes_supported: F(N()).optional(),
        grant_types_supported: F(N()).optional(),
        acr_values_supported: F(N()).optional(),
        subject_types_supported: F(N()),
        id_token_signing_alg_values_supported: F(N()),
        id_token_encryption_alg_values_supported: F(N()).optional(),
        id_token_encryption_enc_values_supported: F(N()).optional(),
        userinfo_signing_alg_values_supported: F(N()).optional(),
        userinfo_encryption_alg_values_supported: F(N()).optional(),
        userinfo_encryption_enc_values_supported: F(N()).optional(),
        request_object_signing_alg_values_supported: F(N()).optional(),
        request_object_encryption_alg_values_supported: F(N()).optional(),
        request_object_encryption_enc_values_supported: F(N()).optional(),
        token_endpoint_auth_methods_supported: F(N()).optional(),
        token_endpoint_auth_signing_alg_values_supported: F(N()).optional(),
        display_values_supported: F(N()).optional(),
        claim_types_supported: F(N()).optional(),
        claims_supported: F(N()).optional(),
        service_documentation: N().optional(),
        claims_locales_supported: F(N()).optional(),
        ui_locales_supported: F(N()).optional(),
        claims_parameter_supported: $a().optional(),
        request_parameter_supported: $a().optional(),
        request_uri_parameter_supported: $a().optional(),
        require_request_uri_registration: $a().optional(),
        op_policy_uri: Dd.optional(),
        op_tos_uri: Dd.optional(),
        client_id_metadata_document_supported: $a().optional(),
      }).shape,
      ...kd.pick({ code_challenge_methods_supported: !0 }).shape,
    }),
    jd = I({
      access_token: N(),
      id_token: N().optional(),
      token_type: N(),
      expires_in: qo().optional(),
      scope: N().optional(),
      refresh_token: N().optional(),
    }).strip(),
    Md = I({
      error: N(),
      error_description: N().optional(),
      error_uri: N().optional(),
    }),
    Nd = Dd.optional().or(B(``).transform(() => void 0)),
    Pd = I({
      redirect_uris: F(Dd),
      token_endpoint_auth_method: N().optional(),
      grant_types: F(N()).optional(),
      response_types: F(N()).optional(),
      client_name: N().optional(),
      client_uri: Dd.optional(),
      logo_uri: Nd,
      scope: N().optional(),
      contacts: F(N()).optional(),
      tos_uri: Nd,
      policy_uri: N().optional(),
      jwks_uri: Dd.optional(),
      jwks: ro().optional(),
      software_id: N().optional(),
      software_version: N().optional(),
      software_statement: N().optional(),
    }).strip(),
    Fd = I({
      client_id: N(),
      client_secret: N().optional(),
      client_id_issued_at: P().optional(),
      client_secret_expires_at: P().optional(),
    }).strip(),
    Id = Pd.merge(Fd);
  (I({ error: N(), error_description: N().optional() }).strip(),
    I({ token: N(), token_type_hint: N().optional() }).strip());
  function Ld(e) {
    let t = typeof e == `string` ? new URL(e) : new URL(e.href);
    return ((t.hash = ``), t);
  }
  function Rd({ requestedResource: e, configuredResource: t }) {
    let n = typeof e == `string` ? new URL(e) : new URL(e.href),
      r = typeof t == `string` ? new URL(t) : new URL(t.href);
    if (n.origin !== r.origin || n.pathname.length < r.pathname.length)
      return !1;
    let i = n.pathname.endsWith(`/`) ? n.pathname : n.pathname + `/`,
      a = r.pathname.endsWith(`/`) ? r.pathname : r.pathname + `/`;
    return i.startsWith(a);
  }
  var zd = class extends Error {
      constructor(e, t) {
        (super(e), (this.errorUri = t), (this.name = this.constructor.name));
      }
      toResponseObject() {
        let e = { error: this.errorCode, error_description: this.message };
        return (this.errorUri && (e.error_uri = this.errorUri), e);
      }
      get errorCode() {
        return this.constructor.errorCode;
      }
    },
    Bd = class extends zd {};
  Bd.errorCode = `invalid_request`;
  var Vd = class extends zd {};
  Vd.errorCode = `invalid_client`;
  var Hd = class extends zd {};
  Hd.errorCode = `invalid_grant`;
  var Ud = class extends zd {};
  Ud.errorCode = `unauthorized_client`;
  var Wd = class extends zd {};
  Wd.errorCode = `unsupported_grant_type`;
  var Gd = class extends zd {};
  Gd.errorCode = `invalid_scope`;
  var Kd = class extends zd {};
  Kd.errorCode = `access_denied`;
  var qd = class extends zd {};
  qd.errorCode = `server_error`;
  var Jd = class extends zd {};
  Jd.errorCode = `temporarily_unavailable`;
  var Yd = class extends zd {};
  Yd.errorCode = `unsupported_response_type`;
  var Xd = class extends zd {};
  Xd.errorCode = `unsupported_token_type`;
  var Zd = class extends zd {};
  Zd.errorCode = `invalid_token`;
  var Qd = class extends zd {};
  Qd.errorCode = `method_not_allowed`;
  var $d = class extends zd {};
  $d.errorCode = `too_many_requests`;
  var ef = class extends zd {};
  ef.errorCode = `invalid_client_metadata`;
  var tf = class extends zd {};
  tf.errorCode = `insufficient_scope`;
  var nf = class extends zd {};
  nf.errorCode = `invalid_target`;
  var rf = {
      [Bd.errorCode]: Bd,
      [Vd.errorCode]: Vd,
      [Hd.errorCode]: Hd,
      [Ud.errorCode]: Ud,
      [Wd.errorCode]: Wd,
      [Gd.errorCode]: Gd,
      [Kd.errorCode]: Kd,
      [qd.errorCode]: qd,
      [Jd.errorCode]: Jd,
      [Yd.errorCode]: Yd,
      [Xd.errorCode]: Xd,
      [Zd.errorCode]: Zd,
      [Qd.errorCode]: Qd,
      [$d.errorCode]: $d,
      [ef.errorCode]: ef,
      [tf.errorCode]: tf,
      [nf.errorCode]: nf,
    },
    af = class extends Error {
      constructor(e) {
        super(e ?? `Unauthorized`);
      }
    };
  function of(e) {
    return [`client_secret_basic`, `client_secret_post`, `none`].includes(e);
  }
  var sf = `code`,
    cf = `S256`;
  function lf(e, t) {
    let n = e.client_secret !== void 0;
    return `token_endpoint_auth_method` in e &&
      e.token_endpoint_auth_method &&
      of(e.token_endpoint_auth_method) &&
      (t.length === 0 || t.includes(e.token_endpoint_auth_method))
      ? e.token_endpoint_auth_method
      : t.length === 0
        ? n
          ? `client_secret_basic`
          : `none`
        : n && t.includes(`client_secret_basic`)
          ? `client_secret_basic`
          : n && t.includes(`client_secret_post`)
            ? `client_secret_post`
            : t.includes(`none`)
              ? `none`
              : n
                ? `client_secret_post`
                : `none`;
  }
  function uf(e, t, n, r) {
    let { client_id: i, client_secret: a } = t;
    switch (e) {
      case `client_secret_basic`:
        df(i, a, n);
        return;
      case `client_secret_post`:
        ff(i, a, r);
        return;
      case `none`:
        pf(i, r);
        return;
      default:
        throw Error(`Unsupported client authentication method: ${e}`);
    }
  }
  function df(e, t, n) {
    if (!t)
      throw Error(
        `client_secret_basic authentication requires a client_secret`,
      );
    let r = btoa(`${e}:${t}`);
    n.set(`Authorization`, `Basic ${r}`);
  }
  function ff(e, t, n) {
    (n.set(`client_id`, e), t && n.set(`client_secret`, t));
  }
  function pf(e, t) {
    t.set(`client_id`, e);
  }
  async function mf(e) {
    let t = e instanceof Response ? e.status : void 0,
      n = e instanceof Response ? await e.text() : e;
    try {
      let {
        error: e,
        error_description: t,
        error_uri: r,
      } = Md.parse(JSON.parse(n));
      return new (rf[e] || qd)(t || ``, r);
    } catch (e) {
      return new qd(
        `${t ? `HTTP ${t}: ` : ``}Invalid OAuth error response: ${e}. Raw body: ${n}`,
      );
    }
  }
  async function hf(e, t) {
    try {
      return await gf(e, t);
    } catch (n) {
      if (n instanceof Vd || n instanceof Ud)
        return (await e.invalidateCredentials?.(`all`), await gf(e, t));
      if (n instanceof Hd)
        return (await e.invalidateCredentials?.(`tokens`), await gf(e, t));
      throw n;
    }
  }
  async function gf(
    e,
    {
      serverUrl: t,
      authorizationCode: n,
      scope: r,
      resourceMetadataUrl: i,
      fetchFn: a,
    },
  ) {
    let o = await e.discoveryState?.(),
      s,
      c,
      l,
      u = i;
    if (
      (!u && o?.resourceMetadataUrl && (u = new URL(o.resourceMetadataUrl)),
      o?.authorizationServerUrl)
    ) {
      if (
        ((c = o.authorizationServerUrl),
        (s = o.resourceMetadata),
        (l = o.authorizationServerMetadata ?? (await Of(c, { fetchFn: a }))),
        !s)
      )
        try {
          s = await xf(t, { resourceMetadataUrl: u }, a);
        } catch {}
      (l !== o.authorizationServerMetadata || s !== o.resourceMetadata) &&
        (await e.saveDiscoveryState?.({
          authorizationServerUrl: String(c),
          resourceMetadataUrl: u?.toString(),
          resourceMetadata: s,
          authorizationServerMetadata: l,
        }));
    } else {
      let n = await kf(t, { resourceMetadataUrl: u, fetchFn: a });
      ((c = n.authorizationServerUrl),
        (l = n.authorizationServerMetadata),
        (s = n.resourceMetadata),
        await e.saveDiscoveryState?.({
          authorizationServerUrl: String(c),
          resourceMetadataUrl: u?.toString(),
          resourceMetadata: s,
          authorizationServerMetadata: l,
        }));
    }
    let d = await vf(t, e, s),
      f = r || s?.scopes_supported?.join(` `) || e.clientMetadata.scope,
      p = await Promise.resolve(e.clientInformation());
    if (!p) {
      if (n !== void 0)
        throw Error(
          `Existing OAuth client information is required when exchanging an authorization code`,
        );
      let t = l?.client_id_metadata_document_supported === !0,
        r = e.clientMetadataUrl;
      if (r && !_f(r))
        throw new ef(
          `clientMetadataUrl must be a valid HTTPS URL with a non-root pathname, got: ${r}`,
        );
      if (t && r) ((p = { client_id: r }), await e.saveClientInformation?.(p));
      else {
        if (!e.saveClientInformation)
          throw Error(
            `OAuth client information must be saveable for dynamic registration`,
          );
        let t = await Ff(c, {
          metadata: l,
          clientMetadata: e.clientMetadata,
          scope: f,
          fetchFn: a,
        });
        (await e.saveClientInformation(t), (p = t));
      }
    }
    let m = !e.redirectUrl;
    if (n !== void 0 || m) {
      let t = await Pf(e, c, {
        metadata: l,
        resource: d,
        authorizationCode: n,
        fetchFn: a,
      });
      return (await e.saveTokens(t), `AUTHORIZED`);
    }
    let h = await e.tokens();
    if (h?.refresh_token)
      try {
        let t = await Nf(c, {
          metadata: l,
          clientInformation: p,
          refreshToken: h.refresh_token,
          resource: d,
          addClientAuthentication: e.addClientAuthentication,
          fetchFn: a,
        });
        return (await e.saveTokens(t), `AUTHORIZED`);
      } catch (e) {
        if (!(!(e instanceof zd) || e instanceof qd)) throw e;
      }
    let g = e.state ? await e.state() : void 0,
      { authorizationUrl: _, codeVerifier: v } = await Af(c, {
        metadata: l,
        clientInformation: p,
        state: g,
        redirectUrl: e.redirectUrl,
        scope: f,
        resource: d,
      });
    return (
      await e.saveCodeVerifier(v),
      await e.redirectToAuthorization(_),
      `REDIRECT`
    );
  }
  function _f(e) {
    if (!e) return !1;
    try {
      let t = new URL(e);
      return t.protocol === `https:` && t.pathname !== `/`;
    } catch {
      return !1;
    }
  }
  async function vf(e, t, n) {
    let r = Ld(e);
    if (t.validateResourceURL)
      return await t.validateResourceURL(r, n?.resource);
    if (n) {
      if (!Rd({ requestedResource: r, configuredResource: n.resource }))
        throw Error(
          `Protected resource ${n.resource} does not match expected ${r} (or origin)`,
        );
      return new URL(n.resource);
    }
  }
  function yf(e) {
    let t = e.headers.get(`WWW-Authenticate`);
    if (!t) return {};
    let [n, r] = t.split(` `);
    if (n.toLowerCase() !== `bearer` || !r) return {};
    let i = bf(e, `resource_metadata`) || void 0,
      a;
    if (i)
      try {
        a = new URL(i);
      } catch {}
    let o = bf(e, `scope`) || void 0,
      s = bf(e, `error`) || void 0;
    return { resourceMetadataUrl: a, scope: o, error: s };
  }
  function bf(e, t) {
    let n = e.headers.get(`WWW-Authenticate`);
    if (!n) return null;
    let r = RegExp(`${t}=(?:"([^"]+)"|([^\\s,]+))`),
      i = n.match(r);
    return i ? i[1] || i[2] : null;
  }
  async function xf(e, t, n = fetch) {
    let r = await Ef(e, `oauth-protected-resource`, n, {
      protocolVersion: t?.protocolVersion,
      metadataUrl: t?.resourceMetadataUrl,
    });
    if (!r || r.status === 404)
      throw (
        await r?.body?.cancel(),
        Error(
          `Resource server does not implement OAuth 2.0 Protected Resource Metadata.`,
        )
      );
    if (!r.ok)
      throw (
        await r.body?.cancel(),
        Error(
          `HTTP ${r.status} trying to load well-known OAuth protected resource metadata.`,
        )
      );
    return Od.parse(await r.json());
  }
  async function Sf(e, t, n = fetch) {
    try {
      return await n(e, { headers: t });
    } catch (r) {
      if (r instanceof TypeError) return t ? Sf(e, void 0, n) : void 0;
      throw r;
    }
  }
  function Cf(e, t = ``, n = {}) {
    return (
      t.endsWith(`/`) && (t = t.slice(0, -1)),
      n.prependPathname ? `${t}/.well-known/${e}` : `/.well-known/${e}${t}`
    );
  }
  async function wf(e, t, n = fetch) {
    return await Sf(e, { "MCP-Protocol-Version": t }, n);
  }
  function Tf(e, t) {
    return !e || (e.status >= 400 && e.status < 500 && t !== `/`);
  }
  async function Ef(e, t, n, r) {
    let i = new URL(e),
      a = r?.protocolVersion ?? `2025-11-25`,
      o;
    if (r?.metadataUrl) o = new URL(r.metadataUrl);
    else {
      let e = Cf(t, i.pathname);
      ((o = new URL(e, r?.metadataServerUrl ?? i)), (o.search = i.search));
    }
    let s = await wf(o, a, n);
    return (
      !r?.metadataUrl &&
        Tf(s, i.pathname) &&
        (s = await wf(new URL(`/.well-known/${t}`, i), a, n)),
      s
    );
  }
  function Df(e) {
    let t = typeof e == `string` ? new URL(e) : e,
      n = t.pathname !== `/`,
      r = [];
    if (!n)
      return (
        r.push({
          url: new URL(`/.well-known/oauth-authorization-server`, t.origin),
          type: `oauth`,
        }),
        r.push({
          url: new URL(`/.well-known/openid-configuration`, t.origin),
          type: `oidc`,
        }),
        r
      );
    let i = t.pathname;
    return (
      i.endsWith(`/`) && (i = i.slice(0, -1)),
      r.push({
        url: new URL(`/.well-known/oauth-authorization-server${i}`, t.origin),
        type: `oauth`,
      }),
      r.push({
        url: new URL(`/.well-known/openid-configuration${i}`, t.origin),
        type: `oidc`,
      }),
      r.push({
        url: new URL(`${i}/.well-known/openid-configuration`, t.origin),
        type: `oidc`,
      }),
      r
    );
  }
  async function Of(e, { fetchFn: t = fetch, protocolVersion: n = Jo } = {}) {
    let r = { "MCP-Protocol-Version": n, Accept: `application/json` },
      i = Df(e);
    for (let { url: e, type: n } of i) {
      let i = await Sf(e, r, t);
      if (i) {
        if (!i.ok) {
          if ((await i.body?.cancel(), i.status >= 400 && i.status < 500))
            continue;
          throw Error(
            `HTTP ${i.status} trying to load ${n === `oauth` ? `OAuth` : `OpenID provider`} metadata from ${e}`,
          );
        }
        return n === `oauth`
          ? kd.parse(await i.json())
          : Ad.parse(await i.json());
      }
    }
  }
  async function kf(e, t) {
    let n, r;
    try {
      ((n = await xf(
        e,
        { resourceMetadataUrl: t?.resourceMetadataUrl },
        t?.fetchFn,
      )),
        n.authorization_servers &&
          n.authorization_servers.length > 0 &&
          (r = n.authorization_servers[0]));
    } catch {}
    r ||= String(new URL(`/`, e));
    let i = await Of(r, { fetchFn: t?.fetchFn });
    return {
      authorizationServerUrl: r,
      authorizationServerMetadata: i,
      resourceMetadata: n,
    };
  }
  async function Af(
    e,
    {
      metadata: t,
      clientInformation: n,
      redirectUrl: r,
      scope: i,
      state: a,
      resource: o,
    },
  ) {
    let s;
    if (t) {
      if (
        ((s = new URL(t.authorization_endpoint)),
        !t.response_types_supported.includes(sf))
      )
        throw Error(
          `Incompatible auth server: does not support response type ${sf}`,
        );
      if (
        t.code_challenge_methods_supported &&
        !t.code_challenge_methods_supported.includes(cf)
      )
        throw Error(
          `Incompatible auth server: does not support code challenge method ${cf}`,
        );
    } else s = new URL(`/authorize`, e);
    let c = await Ed(),
      l = c.code_verifier,
      u = c.code_challenge;
    return (
      s.searchParams.set(`response_type`, sf),
      s.searchParams.set(`client_id`, n.client_id),
      s.searchParams.set(`code_challenge`, u),
      s.searchParams.set(`code_challenge_method`, cf),
      s.searchParams.set(`redirect_uri`, String(r)),
      a && s.searchParams.set(`state`, a),
      i && s.searchParams.set(`scope`, i),
      i?.includes(`offline_access`) &&
        s.searchParams.append(`prompt`, `consent`),
      o && s.searchParams.set(`resource`, o.href),
      { authorizationUrl: s, codeVerifier: l }
    );
  }
  function jf(e, t, n) {
    return new URLSearchParams({
      grant_type: `authorization_code`,
      code: e,
      code_verifier: t,
      redirect_uri: String(n),
    });
  }
  async function Mf(
    e,
    {
      metadata: t,
      tokenRequestParams: n,
      clientInformation: r,
      addClientAuthentication: i,
      resource: a,
      fetchFn: o,
    },
  ) {
    let s = t?.token_endpoint
        ? new URL(t.token_endpoint)
        : new URL(`/token`, e),
      c = new Headers({
        "Content-Type": `application/x-www-form-urlencoded`,
        Accept: `application/json`,
      });
    (a && n.set(`resource`, a.href),
      i
        ? await i(c, n, s, t)
        : r &&
          uf(lf(r, t?.token_endpoint_auth_methods_supported ?? []), r, c, n));
    let l = await (o ?? fetch)(s, { method: `POST`, headers: c, body: n });
    if (!l.ok) throw await mf(l);
    return jd.parse(await l.json());
  }
  async function Nf(
    e,
    {
      metadata: t,
      clientInformation: n,
      refreshToken: r,
      resource: i,
      addClientAuthentication: a,
      fetchFn: o,
    },
  ) {
    return {
      refresh_token: r,
      ...(await Mf(e, {
        metadata: t,
        tokenRequestParams: new URLSearchParams({
          grant_type: `refresh_token`,
          refresh_token: r,
        }),
        clientInformation: n,
        addClientAuthentication: a,
        resource: i,
        fetchFn: o,
      })),
    };
  }
  async function Pf(
    e,
    t,
    { metadata: n, resource: r, authorizationCode: i, fetchFn: a } = {},
  ) {
    let o = e.clientMetadata.scope,
      s;
    if ((e.prepareTokenRequest && (s = await e.prepareTokenRequest(o)), !s)) {
      if (!i)
        throw Error(
          `Either provider.prepareTokenRequest() or authorizationCode is required`,
        );
      if (!e.redirectUrl)
        throw Error(`redirectUrl is required for authorization_code flow`);
      s = jf(i, await e.codeVerifier(), e.redirectUrl);
    }
    let c = await e.clientInformation();
    return Mf(t, {
      metadata: n,
      tokenRequestParams: s,
      clientInformation: c ?? void 0,
      addClientAuthentication: e.addClientAuthentication,
      resource: r,
      fetchFn: a,
    });
  }
  async function Ff(
    e,
    { metadata: t, clientMetadata: n, scope: r, fetchFn: i },
  ) {
    let a;
    if (t) {
      if (!t.registration_endpoint)
        throw Error(
          `Incompatible auth server: does not support dynamic client registration`,
        );
      a = new URL(t.registration_endpoint);
    } else a = new URL(`/register`, e);
    let o = await (i ?? fetch)(a, {
      method: `POST`,
      headers: { "Content-Type": `application/json` },
      body: JSON.stringify({ ...n, ...(r === void 0 ? {} : { scope: r }) }),
    });
    if (!o.ok) throw await mf(o);
    return Id.parse(await o.json());
  }
  var If = class extends Error {
      constructor(e, t) {
        (super(e),
          (this.name = `ParseError`),
          (this.type = t.type),
          (this.field = t.field),
          (this.value = t.value),
          (this.line = t.line));
      }
    },
    Lf = 10,
    Rf = 13,
    zf = 32;
  function Bf(e) {}
  function Vf(e) {
    if (typeof e == `function`)
      throw TypeError(
        "`config` must be an object, got a function instead. Did you mean `createParser({onEvent: fn})`?",
      );
    let {
        onEvent: t = Bf,
        onError: n = Bf,
        onRetry: r = Bf,
        onComment: i,
        maxBufferSize: a,
      } = e,
      o = [],
      s = 0,
      c = !0,
      l,
      u = ``,
      d = 0,
      f,
      p = !1;
    function m(e) {
      if (p)
        throw Error(
          "Cannot feed parser: it was terminated after exceeding the configured max buffer size. Call `reset()` to resume parsing.",
        );
      if (
        (c &&
          ((c = !1),
          e.charCodeAt(0) === 239 &&
            e.charCodeAt(1) === 187 &&
            e.charCodeAt(2) === 191 &&
            (e = e.slice(3))),
        o.length === 0)
      ) {
        let t = g(e);
        (t !== `` && (o.push(t), (s = t.length)), h());
        return;
      }
      if (
        e.indexOf(`
`) === -1 &&
        e.indexOf(`\r`) === -1
      ) {
        (o.push(e), (s += e.length), h());
        return;
      }
      o.push(e);
      let t = o.join(``);
      ((o.length = 0), (s = 0));
      let n = g(t);
      (n !== `` && (o.push(n), (s = n.length)), h());
    }
    function h() {
      a !== void 0 &&
        (s + u.length <= a ||
          ((p = !0),
          (o.length = 0),
          (s = 0),
          (l = void 0),
          (u = ``),
          (d = 0),
          (f = void 0),
          n(
            new If(
              `Buffered data exceeded max buffer size of ${a} characters`,
              { type: `max-buffer-size-exceeded` },
            ),
          )));
    }
    function g(e) {
      let n = 0;
      if (e.indexOf(`\r`) === -1) {
        let r = e.indexOf(
          `
`,
          n,
        );
        for (; r !== -1; ) {
          if (n === r) {
            (d > 0 && t({ id: l, event: f, data: u }),
              (l = void 0),
              (u = ``),
              (d = 0),
              (f = void 0),
              (n = r + 1),
              (r = e.indexOf(
                `
`,
                n,
              )));
            continue;
          }
          let i = e.charCodeAt(n);
          if (Hf(e, n, i)) {
            let i = e.charCodeAt(n + 5) === zf ? n + 6 : n + 5,
              a = e.slice(i, r);
            if (d === 0 && e.charCodeAt(r + 1) === Lf) {
              (t({ id: l, event: f, data: a }),
                (l = void 0),
                (u = ``),
                (f = void 0),
                (n = r + 2),
                (r = e.indexOf(
                  `
`,
                  n,
                )));
              continue;
            }
            ((u =
              d === 0
                ? a
                : `${u}
${a}`),
              d++);
          } else
            Uf(e, n, i)
              ? (f =
                  e.slice(e.charCodeAt(n + 6) === zf ? n + 7 : n + 6, r) ||
                  void 0)
              : _(e, n, r);
          ((n = r + 1),
            (r = e.indexOf(
              `
`,
              n,
            )));
        }
        return e.slice(n);
      }
      for (; n < e.length; ) {
        let t = e.indexOf(`\r`, n),
          r = e.indexOf(
            `
`,
            n,
          ),
          i = -1;
        if (
          (t !== -1 && r !== -1
            ? (i = t < r ? t : r)
            : t === -1
              ? r !== -1 && (i = r)
              : (i = t === e.length - 1 ? -1 : t),
          i === -1)
        )
          break;
        (_(e, n, i),
          (n = i + 1),
          e.charCodeAt(n - 1) === Rf && e.charCodeAt(n) === Lf && n++);
      }
      return e.slice(n);
    }
    function _(e, t, n) {
      if (t === n) {
        y();
        return;
      }
      let r = e.charCodeAt(t);
      if (Hf(e, t, r)) {
        let r = e.charCodeAt(t + 5) === zf ? t + 6 : t + 5,
          i = e.slice(r, n);
        ((u =
          d === 0
            ? i
            : `${u}
${i}`),
          d++);
        return;
      }
      if (Uf(e, t, r)) {
        f = e.slice(e.charCodeAt(t + 6) === zf ? t + 7 : t + 6, n) || void 0;
        return;
      }
      if (
        r === 105 &&
        e.charCodeAt(t + 1) === 100 &&
        e.charCodeAt(t + 2) === 58
      ) {
        let r = e.slice(e.charCodeAt(t + 3) === zf ? t + 4 : t + 3, n);
        l = r.includes(`\0`) ? void 0 : r;
        return;
      }
      if (r === 58) {
        if (i) {
          let r = e.slice(t, n);
          i(r.slice(e.charCodeAt(t + 1) === zf ? 2 : 1));
        }
        return;
      }
      let a = e.slice(t, n),
        o = a.indexOf(`:`);
      if (o === -1) {
        v(a, ``, a);
        return;
      }
      let s = a.slice(0, o),
        c = a.charCodeAt(o + 1) === zf ? 2 : 1;
      v(s, a.slice(o + c), a);
    }
    function v(e, t, i) {
      switch (e) {
        case `event`:
          f = t || void 0;
          break;
        case `data`:
          ((u =
            d === 0
              ? t
              : `${u}
${t}`),
            d++);
          break;
        case `id`:
          l = t.includes(`\0`) ? void 0 : t;
          break;
        case `retry`:
          /^\d+$/.test(t)
            ? r(parseInt(t, 10))
            : n(
                new If(`Invalid \`retry\` value: "${t}"`, {
                  type: `invalid-retry`,
                  value: t,
                  line: i,
                }),
              );
          break;
        default:
          n(
            new If(
              `Unknown field "${e.length > 20 ? `${e.slice(0, 20)}\u2026` : e}"`,
              { type: `unknown-field`, field: e, value: t, line: i },
            ),
          );
          break;
      }
    }
    function y() {
      (d > 0 && t({ id: l, event: f, data: u }),
        (l = void 0),
        (u = ``),
        (d = 0),
        (f = void 0));
    }
    function b(e = {}) {
      if (e.consume && o.length > 0) {
        let e = o.join(``);
        _(e, 0, e.length);
      }
      ((c = !0),
        (l = void 0),
        (u = ``),
        (d = 0),
        (f = void 0),
        (o.length = 0),
        (s = 0),
        (p = !1));
    }
    return { feed: m, reset: b };
  }
  function Hf(e, t, n) {
    return (
      n === 100 &&
      e.charCodeAt(t + 1) === 97 &&
      e.charCodeAt(t + 2) === 116 &&
      e.charCodeAt(t + 3) === 97 &&
      e.charCodeAt(t + 4) === 58
    );
  }
  function Uf(e, t, n) {
    return (
      n === 101 &&
      e.charCodeAt(t + 1) === 118 &&
      e.charCodeAt(t + 2) === 101 &&
      e.charCodeAt(t + 3) === 110 &&
      e.charCodeAt(t + 4) === 116 &&
      e.charCodeAt(t + 5) === 58
    );
  }
  var Wf = class extends TransformStream {
      constructor({
        onError: e,
        onRetry: t,
        onComment: n,
        maxBufferSize: r,
      } = {}) {
        let i;
        super({
          start(a) {
            i = Vf({
              onEvent: (e) => {
                a.enqueue(e);
              },
              onError(t) {
                (typeof e == `function` && e(t),
                  (e === `terminate` ||
                    t.type === `max-buffer-size-exceeded`) &&
                    a.error(t));
              },
              onRetry: t,
              onComment: n,
              maxBufferSize: r,
            });
          },
          transform(e) {
            i.feed(e);
          },
        });
      }
    },
    Gf = {
      initialReconnectionDelay: 1e3,
      maxReconnectionDelay: 3e4,
      reconnectionDelayGrowFactor: 1.5,
      maxRetries: 2,
    },
    Kf = class extends Error {
      constructor(e, t) {
        (super(`Streamable HTTP error: ${t}`), (this.code = e));
      }
    },
    qf = class {
      constructor(e, t) {
        ((this._hasCompletedAuthFlow = !1),
          (this._url = e),
          (this._resourceMetadataUrl = void 0),
          (this._scope = void 0),
          (this._requestInit = t?.requestInit),
          (this._authProvider = t?.authProvider),
          (this._fetch = t?.fetch),
          (this._fetchWithInit = xd(t?.fetch, t?.requestInit)),
          (this._sessionId = t?.sessionId),
          (this._reconnectionOptions = t?.reconnectionOptions ?? Gf));
      }
      async _authThenStart() {
        if (!this._authProvider) throw new af(`No auth provider`);
        let e;
        try {
          e = await hf(this._authProvider, {
            serverUrl: this._url,
            resourceMetadataUrl: this._resourceMetadataUrl,
            scope: this._scope,
            fetchFn: this._fetchWithInit,
          });
        } catch (e) {
          throw (this.onerror?.(e), e);
        }
        if (e !== `AUTHORIZED`) throw new af();
        return await this._startOrAuthSse({ resumptionToken: void 0 });
      }
      async _commonHeaders() {
        let e = {};
        if (this._authProvider) {
          let t = await this._authProvider.tokens();
          t && (e.Authorization = `Bearer ${t.access_token}`);
        }
        (this._sessionId && (e[`mcp-session-id`] = this._sessionId),
          this._protocolVersion &&
            (e[`mcp-protocol-version`] = this._protocolVersion));
        let t = bd(this._requestInit?.headers);
        return new Headers({ ...e, ...t });
      }
      async _startOrAuthSse(e) {
        let { resumptionToken: t } = e;
        try {
          let n = await this._commonHeaders();
          (n.set(`Accept`, `text/event-stream`),
            t && n.set(`last-event-id`, t));
          let r = await (this._fetch ?? fetch)(this._url, {
            method: `GET`,
            headers: n,
            signal: this._abortController?.signal,
          });
          if (!r.ok) {
            if (
              (await r.body?.cancel(), r.status === 401 && this._authProvider)
            )
              return await this._authThenStart();
            if (r.status === 405) return;
            throw new Kf(
              r.status,
              `Failed to open SSE stream: ${r.statusText}`,
            );
          }
          this._handleSseStream(r.body, e, !0);
        } catch (e) {
          throw (this.onerror?.(e), e);
        }
      }
      _getNextReconnectionDelay(e) {
        if (this._serverRetryMs !== void 0) return this._serverRetryMs;
        let t = this._reconnectionOptions.initialReconnectionDelay,
          n = this._reconnectionOptions.reconnectionDelayGrowFactor,
          r = this._reconnectionOptions.maxReconnectionDelay;
        return Math.min(t * n ** +e, r);
      }
      _scheduleReconnection(e, t = 0) {
        let n = this._reconnectionOptions.maxRetries;
        if (t >= n) {
          this.onerror?.(
            Error(`Maximum reconnection attempts (${n}) exceeded.`),
          );
          return;
        }
        let r = this._getNextReconnectionDelay(t);
        this._reconnectionTimeout = setTimeout(() => {
          this._startOrAuthSse(e).catch((n) => {
            (this.onerror?.(
              Error(
                `Failed to reconnect SSE stream: ${n instanceof Error ? n.message : String(n)}`,
              ),
            ),
              this._scheduleReconnection(e, t + 1));
          });
        }, r);
      }
      _handleSseStream(e, t, n) {
        if (!e) return;
        let { onresumptiontoken: r, replayMessageId: i } = t,
          a,
          o = !1,
          s = !1;
        (async () => {
          try {
            let t = e
              .pipeThrough(new TextDecoderStream())
              .pipeThrough(
                new Wf({
                  onRetry: (e) => {
                    this._serverRetryMs = e;
                  },
                }),
              )
              .getReader();
            for (;;) {
              let { value: e, done: n } = await t.read();
              if (n) break;
              if (
                (e.id && ((a = e.id), (o = !0), r?.(e.id)),
                e.data && (!e.event || e.event === `message`))
              )
                try {
                  let t = ys.parse(JSON.parse(e.data));
                  (gs(t) && ((s = !0), i !== void 0 && (t.id = i)),
                    this.onmessage?.(t));
                } catch (e) {
                  this.onerror?.(e);
                }
            }
            (n || o) &&
              !s &&
              this._abortController &&
              !this._abortController.signal.aborted &&
              this._scheduleReconnection(
                {
                  resumptionToken: a,
                  onresumptiontoken: r,
                  replayMessageId: i,
                },
                0,
              );
          } catch (e) {
            if (
              (this.onerror?.(Error(`SSE stream disconnected: ${e}`)),
              (n || o) &&
                !s &&
                this._abortController &&
                !this._abortController.signal.aborted)
            )
              try {
                this._scheduleReconnection(
                  {
                    resumptionToken: a,
                    onresumptiontoken: r,
                    replayMessageId: i,
                  },
                  0,
                );
              } catch (e) {
                this.onerror?.(
                  Error(
                    `Failed to reconnect: ${e instanceof Error ? e.message : String(e)}`,
                  ),
                );
              }
          }
        })();
      }
      async start() {
        if (this._abortController)
          throw Error(
            `StreamableHTTPClientTransport already started! If using Client class, note that connect() calls start() automatically.`,
          );
        this._abortController = new AbortController();
      }
      async finishAuth(e) {
        if (!this._authProvider) throw new af(`No auth provider`);
        if (
          (await hf(this._authProvider, {
            serverUrl: this._url,
            authorizationCode: e,
            resourceMetadataUrl: this._resourceMetadataUrl,
            scope: this._scope,
            fetchFn: this._fetchWithInit,
          })) !== `AUTHORIZED`
        )
          throw new af(`Failed to authorize`);
      }
      async close() {
        ((this._reconnectionTimeout &&=
          (clearTimeout(this._reconnectionTimeout), void 0)),
          this._abortController?.abort(),
          this.onclose?.());
      }
      async send(e, t) {
        try {
          let { resumptionToken: n, onresumptiontoken: r } = t || {};
          if (n) {
            this._startOrAuthSse({
              resumptionToken: n,
              replayMessageId: fs(e) ? e.id : void 0,
            }).catch((e) => this.onerror?.(e));
            return;
          }
          let i = await this._commonHeaders();
          (i.set(`content-type`, `application/json`),
            i.set(`accept`, `application/json, text/event-stream`));
          let a = {
              ...this._requestInit,
              method: `POST`,
              headers: i,
              body: JSON.stringify(e),
              signal: this._abortController?.signal,
            },
            o = await (this._fetch ?? fetch)(this._url, a),
            s = o.headers.get(`mcp-session-id`);
          if ((s && (this._sessionId = s), !o.ok)) {
            let t = await o.text().catch(() => null);
            if (o.status === 401 && this._authProvider) {
              if (this._hasCompletedAuthFlow)
                throw new Kf(
                  401,
                  `Server returned 401 after successful authentication`,
                );
              let { resourceMetadataUrl: t, scope: n } = yf(o);
              if (
                ((this._resourceMetadataUrl = t),
                (this._scope = n),
                (await hf(this._authProvider, {
                  serverUrl: this._url,
                  resourceMetadataUrl: this._resourceMetadataUrl,
                  scope: this._scope,
                  fetchFn: this._fetchWithInit,
                })) !== `AUTHORIZED`)
              )
                throw new af();
              return ((this._hasCompletedAuthFlow = !0), this.send(e));
            }
            if (o.status === 403 && this._authProvider) {
              let { resourceMetadataUrl: t, scope: n, error: r } = yf(o);
              if (r === `insufficient_scope`) {
                let r = o.headers.get(`WWW-Authenticate`);
                if (this._lastUpscopingHeader === r)
                  throw new Kf(
                    403,
                    `Server returned 403 after trying upscoping`,
                  );
                if (
                  (n && (this._scope = n),
                  t && (this._resourceMetadataUrl = t),
                  (this._lastUpscopingHeader = r ?? void 0),
                  (await hf(this._authProvider, {
                    serverUrl: this._url,
                    resourceMetadataUrl: this._resourceMetadataUrl,
                    scope: this._scope,
                    fetchFn: this._fetch,
                  })) !== `AUTHORIZED`)
                )
                  throw new af();
                return this.send(e);
              }
            }
            throw new Kf(o.status, `Error POSTing to endpoint: ${t}`);
          }
          if (
            ((this._hasCompletedAuthFlow = !1),
            (this._lastUpscopingHeader = void 0),
            o.status === 202)
          ) {
            (await o.body?.cancel(),
              Fs(e) &&
                this._startOrAuthSse({ resumptionToken: void 0 }).catch((e) =>
                  this.onerror?.(e),
                ));
            return;
          }
          let c =
              (Array.isArray(e) ? e : [e]).filter(
                (e) => `method` in e && `id` in e && e.id !== void 0,
              ).length > 0,
            l = o.headers.get(`content-type`);
          if (c)
            if (l?.includes(`text/event-stream`))
              this._handleSseStream(o.body, { onresumptiontoken: r }, !1);
            else if (l?.includes(`application/json`)) {
              let e = await o.json(),
                t = Array.isArray(e)
                  ? e.map((e) => ys.parse(e))
                  : [ys.parse(e)];
              for (let e of t) this.onmessage?.(e);
            } else
              throw (
                await o.body?.cancel(),
                new Kf(-1, `Unexpected content type: ${l}`)
              );
          else await o.body?.cancel();
        } catch (e) {
          throw (this.onerror?.(e), e);
        }
      }
      get sessionId() {
        return this._sessionId;
      }
      async terminateSession() {
        if (this._sessionId)
          try {
            let e = await this._commonHeaders(),
              t = {
                ...this._requestInit,
                method: `DELETE`,
                headers: e,
                signal: this._abortController?.signal,
              },
              n = await (this._fetch ?? fetch)(this._url, t);
            if ((await n.body?.cancel(), !n.ok && n.status !== 405))
              throw new Kf(
                n.status,
                `Failed to terminate session: ${n.statusText}`,
              );
            this._sessionId = void 0;
          } catch (e) {
            throw (this.onerror?.(e), e);
          }
      }
      setProtocolVersion(e) {
        this._protocolVersion = e;
      }
      get protocolVersion() {
        return this._protocolVersion;
      }
      async resumeStream(e, t) {
        await this._startOrAuthSse({
          resumptionToken: e,
          onresumptiontoken: t?.onresumptiontoken,
        });
      }
    },
    Jf = (...e) =>
      e
        .filter((e, t, n) => !!e && e.trim() !== `` && n.indexOf(e) === t)
        .join(` `)
        .trim(),
    Yf = (e) => e.replace(/([a-z0-9])([A-Z])/g, `$1-$2`).toLowerCase(),
    Xf = (e) =>
      e.replace(/^([A-Z])|[\s-_]+(\w)/g, (e, t, n) =>
        n ? n.toUpperCase() : t.toLowerCase(),
      ),
    Zf = (e) => {
      let t = Xf(e);
      return t.charAt(0).toUpperCase() + t.slice(1);
    },
    Qf = {
      xmlns: `http://www.w3.org/2000/svg`,
      width: 24,
      height: 24,
      viewBox: `0 0 24 24`,
      fill: `none`,
      stroke: `currentColor`,
      strokeWidth: 2,
      strokeLinecap: `round`,
      strokeLinejoin: `round`,
    },
    $f = (e) => {
      for (let t in e)
        if (t.startsWith(`aria-`) || t === `role` || t === `title`) return !0;
      return !1;
    },
    ep = g(),
    tp = (0, ep.createContext)({}),
    np = () => (0, ep.useContext)(tp),
    rp = (0, ep.forwardRef)(
      (
        {
          color: e,
          size: t,
          strokeWidth: n,
          absoluteStrokeWidth: r,
          className: i = ``,
          children: a,
          iconNode: o,
          ...s
        },
        c,
      ) => {
        let {
            size: l = 24,
            strokeWidth: u = 2,
            absoluteStrokeWidth: d = !1,
            color: f = `currentColor`,
            className: p = ``,
          } = np() ?? {},
          m = (r ?? d) ? (Number(n ?? u) * 24) / Number(t ?? l) : (n ?? u);
        return (0, ep.createElement)(
          `svg`,
          {
            ref: c,
            ...Qf,
            width: t ?? l ?? Qf.width,
            height: t ?? l ?? Qf.height,
            stroke: e ?? f,
            strokeWidth: m,
            className: Jf(`lucide`, p, i),
            ...(!a && !$f(s) && { "aria-hidden": `true` }),
            ...s,
          },
          [
            ...o.map(([e, t]) => (0, ep.createElement)(e, t)),
            ...(Array.isArray(a) ? a : [a]),
          ],
        );
      },
    ),
    ip = (e, t) => {
      let n = (0, ep.forwardRef)(({ className: n, ...r }, i) =>
        (0, ep.createElement)(rp, {
          ref: i,
          iconNode: t,
          className: Jf(`lucide-${Yf(Zf(e))}`, `lucide-${e}`, n),
          ...r,
        }),
      );
      return ((n.displayName = Zf(e)), n);
    },
    ap = ip(`check`, [[`path`, { d: `M20 6 9 17l-5-5`, key: `1gmf2c` }]]),
    op = ip(`globe`, [
      [`circle`, { cx: `12`, cy: `12`, r: `10`, key: `1mglay` }],
      [
        `path`,
        { d: `M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20`, key: `13o1zl` },
      ],
      [`path`, { d: `M2 12h20`, key: `9i4pu4` }],
    ]),
    sp = ip(`repeat`, [
      [`path`, { d: `m17 2 4 4-4 4`, key: `nntrym` }],
      [`path`, { d: `M3 11v-1a4 4 0 0 1 4-4h14`, key: `84bu3i` }],
      [`path`, { d: `m7 22-4-4 4-4`, key: `1wqhfi` }],
      [`path`, { d: `M21 13v1a4 4 0 0 1-4 4H3`, key: `1rx37r` }],
    ]),
    cp = ip(`shield`, [
      [
        `path`,
        {
          d: `M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z`,
          key: `oel41y`,
        },
      ],
    ]),
    lp = ip(`triangle-alert`, [
      [
        `path`,
        {
          d: `m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3`,
          key: `wmoenq`,
        },
      ],
      [`path`, { d: `M12 9v4`, key: `juzpu7` }],
      [`path`, { d: `M12 17h.01`, key: `p32p05` }],
    ]),
    up = [`read_only`, `destructive`, `idempotent`, `open_world`],
    dp = [
      {
        key: `read_only`,
        label: `Read-only`,
        description: `Tools that don't modify their environment`,
        icon: cp,
      },
      {
        key: `destructive`,
        label: `Destructive`,
        description: `Tools that perform destructive updates`,
        icon: lp,
      },
      {
        key: `idempotent`,
        label: `Idempotent`,
        description: `Repeated calls have no additional effect`,
        icon: sp,
      },
      {
        key: `open_world`,
        label: `Open-world`,
        description: `Tools that interact with external entities`,
        icon: op,
      },
    ];
  function fp(e) {
    var t,
      n,
      r = ``;
    if (typeof e == `string` || typeof e == `number`) r += e;
    else if (typeof e == `object`)
      if (Array.isArray(e)) {
        var i = e.length;
        for (t = 0; t < i; t++)
          e[t] && (n = fp(e[t])) && (r && (r += ` `), (r += n));
      } else for (n in e) e[n] && (r && (r += ` `), (r += n));
    return r;
  }
  function pp() {
    for (var e, t, n = 0, r = ``, i = arguments.length; n < i; n++)
      (e = arguments[n]) && (t = fp(e)) && (r && (r += ` `), (r += t));
    return r;
  }
  var mp = (e, t) => {
      let n = Array(e.length + t.length);
      for (let t = 0; t < e.length; t++) n[t] = e[t];
      for (let r = 0; r < t.length; r++) n[e.length + r] = t[r];
      return n;
    },
    hp = (e, t) => ({ classGroupId: e, validator: t }),
    gp = (e = new Map(), t = null, n) => ({
      nextPart: e,
      validators: t,
      classGroupId: n,
    }),
    _p = `-`,
    vp = [],
    yp = `arbitrary..`,
    bp = (e) => {
      let t = Cp(e),
        { conflictingClassGroups: n, conflictingClassGroupModifiers: r } = e;
      return {
        getClassGroupId: (e) => {
          if (e.startsWith(`[`) && e.endsWith(`]`)) return Sp(e);
          let n = e.split(_p);
          return xp(n, +(n[0] === `` && n.length > 1), t);
        },
        getConflictingClassGroupIds: (e, t) => {
          if (t) {
            let t = r[e],
              i = n[e];
            return t ? (i ? mp(i, t) : t) : i || vp;
          }
          return n[e] || vp;
        },
      };
    },
    xp = (e, t, n) => {
      if (e.length - t === 0) return n.classGroupId;
      let r = e[t],
        i = n.nextPart.get(r);
      if (i) {
        let n = xp(e, t + 1, i);
        if (n) return n;
      }
      let a = n.validators;
      if (a === null) return;
      let o = t === 0 ? e.join(_p) : e.slice(t).join(_p),
        s = a.length;
      for (let e = 0; e < s; e++) {
        let t = a[e];
        if (t.validator(o)) return t.classGroupId;
      }
    },
    Sp = (e) =>
      e.slice(1, -1).indexOf(`:`) === -1
        ? void 0
        : (() => {
            let t = e.slice(1, -1),
              n = t.indexOf(`:`),
              r = t.slice(0, n);
            return r ? yp + r : void 0;
          })(),
    Cp = (e) => {
      let { theme: t, classGroups: n } = e;
      return wp(n, t);
    },
    wp = (e, t) => {
      let n = gp();
      for (let r in e) {
        let i = e[r];
        Tp(i, n, r, t);
      }
      return n;
    },
    Tp = (e, t, n, r) => {
      let i = e.length;
      for (let a = 0; a < i; a++) {
        let i = e[a];
        Ep(i, t, n, r);
      }
    },
    Ep = (e, t, n, r) => {
      if (typeof e == `string`) {
        Dp(e, t, n);
        return;
      }
      if (typeof e == `function`) {
        Op(e, t, n, r);
        return;
      }
      kp(e, t, n, r);
    },
    Dp = (e, t, n) => {
      let r = e === `` ? t : Ap(t, e);
      r.classGroupId = n;
    },
    Op = (e, t, n, r) => {
      if (jp(e)) {
        Tp(e(r), t, n, r);
        return;
      }
      (t.validators === null && (t.validators = []),
        t.validators.push(hp(n, e)));
    },
    kp = (e, t, n, r) => {
      let i = Object.entries(e),
        a = i.length;
      for (let e = 0; e < a; e++) {
        let [a, o] = i[e];
        Tp(o, Ap(t, a), n, r);
      }
    },
    Ap = (e, t) => {
      let n = e,
        r = t.split(_p),
        i = r.length;
      for (let e = 0; e < i; e++) {
        let t = r[e],
          i = n.nextPart.get(t);
        (i || ((i = gp()), n.nextPart.set(t, i)), (n = i));
      }
      return n;
    },
    jp = (e) => `isThemeGetter` in e && e.isThemeGetter === !0,
    Mp = (e) => {
      if (e < 1) return { get: () => void 0, set: () => {} };
      let t = 0,
        n = Object.create(null),
        r = Object.create(null),
        i = (i, a) => {
          ((n[i] = a),
            t++,
            t > e && ((t = 0), (r = n), (n = Object.create(null))));
        };
      return {
        get(e) {
          let t = n[e];
          if (t !== void 0) return t;
          if ((t = r[e]) !== void 0) return (i(e, t), t);
        },
        set(e, t) {
          e in n ? (n[e] = t) : i(e, t);
        },
      };
    },
    Np = `!`,
    Pp = `:`,
    Fp = [],
    Ip = (e, t, n, r, i) => ({
      modifiers: e,
      hasImportantModifier: t,
      baseClassName: n,
      maybePostfixModifierPosition: r,
      isExternal: i,
    }),
    Lp = (e) => {
      let { prefix: t, experimentalParseClassName: n } = e,
        r = (e) => {
          let t = [],
            n = 0,
            r = 0,
            i = 0,
            a,
            o = e.length;
          for (let s = 0; s < o; s++) {
            let o = e[s];
            if (n === 0 && r === 0) {
              if (o === Pp) {
                (t.push(e.slice(i, s)), (i = s + 1));
                continue;
              }
              if (o === `/`) {
                a = s;
                continue;
              }
            }
            o === `[`
              ? n++
              : o === `]`
                ? n--
                : o === `(`
                  ? r++
                  : o === `)` && r--;
          }
          let s = t.length === 0 ? e : e.slice(i),
            c = s,
            l = !1;
          s.endsWith(Np)
            ? ((c = s.slice(0, -1)), (l = !0))
            : s.startsWith(Np) && ((c = s.slice(1)), (l = !0));
          let u = a && a > i ? a - i : void 0;
          return Ip(t, l, c, u);
        };
      if (t) {
        let e = t + Pp,
          n = r;
        r = (t) =>
          t.startsWith(e) ? n(t.slice(e.length)) : Ip(Fp, !1, t, void 0, !0);
      }
      if (n) {
        let e = r;
        r = (t) => n({ className: t, parseClassName: e });
      }
      return r;
    },
    Rp = (e) => {
      let t = new Map();
      return (
        e.orderSensitiveModifiers.forEach((e, n) => {
          t.set(e, 1e6 + n);
        }),
        (e) => {
          let n = [],
            r = [];
          for (let i = 0; i < e.length; i++) {
            let a = e[i],
              o = a[0] === `[`,
              s = t.has(a);
            o || s
              ? (r.length > 0 && (r.sort(), n.push(...r), (r = [])), n.push(a))
              : r.push(a);
          }
          return (r.length > 0 && (r.sort(), n.push(...r)), n);
        }
      );
    },
    zp = (e) => ({
      cache: Mp(e.cacheSize),
      parseClassName: Lp(e),
      sortModifiers: Rp(e),
      postfixLookupClassGroupIds: Bp(e),
      ...bp(e),
    }),
    Bp = (e) => {
      let t = Object.create(null),
        n = e.postfixLookupClassGroups;
      if (n) for (let e = 0; e < n.length; e++) t[n[e]] = !0;
      return t;
    },
    Vp = /\s+/,
    Hp = (e, t) => {
      let {
          parseClassName: n,
          getClassGroupId: r,
          getConflictingClassGroupIds: i,
          sortModifiers: a,
          postfixLookupClassGroupIds: o,
        } = t,
        s = [],
        c = e.trim().split(Vp),
        l = ``;
      for (let e = c.length - 1; e >= 0; --e) {
        let t = c[e],
          {
            isExternal: u,
            modifiers: d,
            hasImportantModifier: f,
            baseClassName: p,
            maybePostfixModifierPosition: m,
          } = n(t);
        if (u) {
          l = t + (l.length > 0 ? ` ` + l : l);
          continue;
        }
        let h = !!m,
          g;
        if (h) {
          g = r(p.substring(0, m));
          let e = g && o[g] ? r(p) : void 0;
          e && e !== g && ((g = e), (h = !1));
        } else g = r(p);
        if (!g) {
          if (!h) {
            l = t + (l.length > 0 ? ` ` + l : l);
            continue;
          }
          if (((g = r(p)), !g)) {
            l = t + (l.length > 0 ? ` ` + l : l);
            continue;
          }
          h = !1;
        }
        let _ = d.length === 0 ? `` : d.length === 1 ? d[0] : a(d).join(`:`),
          v = f ? _ + Np : _,
          y = v + g;
        if (s.indexOf(y) > -1) continue;
        s.push(y);
        let b = i(g, h);
        for (let e = 0; e < b.length; ++e) {
          let t = b[e];
          s.push(v + t);
        }
        l = t + (l.length > 0 ? ` ` + l : l);
      }
      return l;
    },
    Up = (...e) => {
      let t = 0,
        n,
        r,
        i = ``;
      for (; t < e.length; )
        (n = e[t++]) && (r = Wp(n)) && (i && (i += ` `), (i += r));
      return i;
    },
    Wp = (e) => {
      if (typeof e == `string`) return e;
      let t,
        n = ``;
      for (let r = 0; r < e.length; r++)
        e[r] && (t = Wp(e[r])) && (n && (n += ` `), (n += t));
      return n;
    },
    Gp = (e, ...t) => {
      let n,
        r,
        i,
        a,
        o = (o) => (
          (n = zp(t.reduce((e, t) => t(e), e()))),
          (r = n.cache.get),
          (i = n.cache.set),
          (a = s),
          s(o)
        ),
        s = (e) => {
          let t = r(e);
          if (t) return t;
          let a = Hp(e, n);
          return (i(e, a), a);
        };
      return ((a = o), (...e) => a(Up(...e)));
    },
    Kp = [],
    qp = (e) => {
      let t = (t) => t[e] || Kp;
      return ((t.isThemeGetter = !0), t);
    },
    Jp = /^\[(?:(\w[\w-]*):)?(.+)\]$/i,
    Yp = /^\((?:(\w[\w-]*):)?(.+)\)$/i,
    Xp = /^\d+(?:\.\d+)?\/\d+(?:\.\d+)?$/,
    Zp = /^(\d+(\.\d+)?)?(xs|sm|md|lg|xl)$/,
    Qp =
      /\d+(%|px|r?em|[sdl]?v([hwib]|min|max)|pt|pc|in|cm|mm|cap|ch|ex|r?lh|cq(w|h|i|b|min|max))|\b(calc|min|max|clamp)\(.+\)|^0$/,
    $p = /^(rgba?|hsla?|hwb|(ok)?(lab|lch)|color-mix)\(.+\)$/,
    em = /^(inset_)?-?((\d+)?\.?(\d+)[a-z]+|0)_-?((\d+)?\.?(\d+)[a-z]+|0)/,
    tm =
      /^(url|image|image-set|cross-fade|element|(repeating-)?(linear|radial|conic)-gradient)\(.+\)$/,
    nm = (e) => Xp.test(e),
    X = (e) => !!e && !Number.isNaN(Number(e)),
    rm = (e) => !!e && Number.isInteger(Number(e)),
    im = (e) => e.endsWith(`%`) && X(e.slice(0, -1)),
    am = (e) => Zp.test(e),
    om = () => !0,
    sm = (e) => Qp.test(e) && !$p.test(e),
    cm = () => !1,
    lm = (e) => em.test(e),
    um = (e) => tm.test(e),
    dm = (e) => !Z(e) && !Q(e),
    fm = (e) =>
      e.startsWith(`@container`) &&
      ((e[10] === `/` && e[11] !== void 0) ||
        (e[11] === `s` && e[16] !== void 0 && e.startsWith(`-size/`, 10)) ||
        (e[11] === `n` && e[18] !== void 0 && e.startsWith(`-normal/`, 10))),
    pm = (e) => Om(e, Mm, cm),
    Z = (e) => Jp.test(e),
    mm = (e) => Om(e, Nm, sm),
    hm = (e) => Om(e, Pm, X),
    gm = (e) => Om(e, Im, om),
    _m = (e) => Om(e, Fm, cm),
    vm = (e) => Om(e, Am, cm),
    ym = (e) => Om(e, jm, um),
    bm = (e) => Om(e, Lm, lm),
    Q = (e) => Yp.test(e),
    xm = (e) => km(e, Nm),
    Sm = (e) => km(e, Fm),
    Cm = (e) => km(e, Am),
    wm = (e) => km(e, Mm),
    Tm = (e) => km(e, jm),
    Em = (e) => km(e, Lm, !0),
    Dm = (e) => km(e, Im, !0),
    Om = (e, t, n) => {
      let r = Jp.exec(e);
      return r ? (r[1] ? t(r[1]) : n(r[2])) : !1;
    },
    km = (e, t, n = !1) => {
      let r = Yp.exec(e);
      return r ? (r[1] ? t(r[1]) : n) : !1;
    },
    Am = (e) => e === `position` || e === `percentage`,
    jm = (e) => e === `image` || e === `url`,
    Mm = (e) => e === `length` || e === `size` || e === `bg-size`,
    Nm = (e) => e === `length`,
    Pm = (e) => e === `number`,
    Fm = (e) => e === `family-name`,
    Im = (e) => e === `number` || e === `weight`,
    Lm = (e) => e === `shadow`,
    Rm = () => {
      let e = qp(`color`),
        t = qp(`font`),
        n = qp(`text`),
        r = qp(`font-weight`),
        i = qp(`tracking`),
        a = qp(`leading`),
        o = qp(`breakpoint`),
        s = qp(`container`),
        c = qp(`spacing`),
        l = qp(`radius`),
        u = qp(`shadow`),
        d = qp(`inset-shadow`),
        f = qp(`text-shadow`),
        p = qp(`drop-shadow`),
        m = qp(`blur`),
        h = qp(`perspective`),
        g = qp(`aspect`),
        _ = qp(`ease`),
        v = qp(`animate`),
        y = () => [
          `auto`,
          `avoid`,
          `all`,
          `avoid-page`,
          `page`,
          `left`,
          `right`,
          `column`,
        ],
        b = () => [
          `center`,
          `top`,
          `bottom`,
          `left`,
          `right`,
          `top-left`,
          `left-top`,
          `top-right`,
          `right-top`,
          `bottom-right`,
          `right-bottom`,
          `bottom-left`,
          `left-bottom`,
        ],
        x = () => [...b(), Q, Z],
        ee = () => [`auto`, `hidden`, `clip`, `visible`, `scroll`],
        S = () => [`auto`, `contain`, `none`],
        C = () => [Q, Z, c],
        w = () => [nm, `full`, `auto`, ...C()],
        te = () => [rm, `none`, `subgrid`, Q, Z],
        ne = () => [`auto`, { span: [`full`, rm, Q, Z] }, rm, Q, Z],
        re = () => [rm, `auto`, Q, Z],
        ie = () => [`auto`, `min`, `max`, `fr`, Q, Z],
        ae = () => [
          `start`,
          `end`,
          `center`,
          `between`,
          `around`,
          `evenly`,
          `stretch`,
          `baseline`,
          `center-safe`,
          `end-safe`,
        ],
        oe = () => [
          `start`,
          `end`,
          `center`,
          `stretch`,
          `center-safe`,
          `end-safe`,
        ],
        se = () => [`auto`, ...C()],
        ce = () => [
          nm,
          `auto`,
          `full`,
          `dvw`,
          `dvh`,
          `lvw`,
          `lvh`,
          `svw`,
          `svh`,
          `min`,
          `max`,
          `fit`,
          ...C(),
        ],
        le = () => [
          nm,
          `screen`,
          `full`,
          `dvw`,
          `lvw`,
          `svw`,
          `min`,
          `max`,
          `fit`,
          ...C(),
        ],
        T = () => [
          nm,
          `screen`,
          `full`,
          `lh`,
          `dvh`,
          `lvh`,
          `svh`,
          `min`,
          `max`,
          `fit`,
          ...C(),
        ],
        E = () => [e, Q, Z],
        D = () => [...b(), Cm, vm, { position: [Q, Z] }],
        ue = () => [`no-repeat`, { repeat: [``, `x`, `y`, `space`, `round`] }],
        de = () => [`auto`, `cover`, `contain`, wm, pm, { size: [Q, Z] }],
        fe = () => [im, xm, mm],
        pe = () => [``, `none`, `full`, l, Q, Z],
        O = () => [``, X, xm, mm],
        k = () => [`solid`, `dashed`, `dotted`, `double`],
        me = () => [
          `normal`,
          `multiply`,
          `screen`,
          `overlay`,
          `darken`,
          `lighten`,
          `color-dodge`,
          `color-burn`,
          `hard-light`,
          `soft-light`,
          `difference`,
          `exclusion`,
          `hue`,
          `saturation`,
          `color`,
          `luminosity`,
        ],
        he = () => [X, im, Cm, vm],
        ge = () => [``, `none`, m, Q, Z],
        _e = () => [`none`, X, Q, Z],
        A = () => [`none`, X, Q, Z],
        ve = () => [X, Q, Z],
        ye = () => [nm, `full`, ...C()];
      return {
        cacheSize: 500,
        theme: {
          animate: [`spin`, `ping`, `pulse`, `bounce`],
          aspect: [`video`],
          blur: [am],
          breakpoint: [am],
          color: [om],
          container: [am],
          "drop-shadow": [am],
          ease: [`in`, `out`, `in-out`],
          font: [dm],
          "font-weight": [
            `thin`,
            `extralight`,
            `light`,
            `normal`,
            `medium`,
            `semibold`,
            `bold`,
            `extrabold`,
            `black`,
          ],
          "inset-shadow": [am],
          leading: [`none`, `tight`, `snug`, `normal`, `relaxed`, `loose`],
          perspective: [
            `dramatic`,
            `near`,
            `normal`,
            `midrange`,
            `distant`,
            `none`,
          ],
          radius: [am],
          shadow: [am],
          spacing: [`px`, X],
          text: [am],
          "text-shadow": [am],
          tracking: [`tighter`, `tight`, `normal`, `wide`, `wider`, `widest`],
        },
        classGroups: {
          aspect: [{ aspect: [`auto`, `square`, nm, Z, Q, g] }],
          container: [`container`],
          "container-type": [{ "@container": [``, `normal`, `size`, Q, Z] }],
          "container-named": [fm],
          columns: [{ columns: [X, Z, Q, s] }],
          "break-after": [{ "break-after": y() }],
          "break-before": [{ "break-before": y() }],
          "break-inside": [
            { "break-inside": [`auto`, `avoid`, `avoid-page`, `avoid-column`] },
          ],
          "box-decoration": [{ "box-decoration": [`slice`, `clone`] }],
          box: [{ box: [`border`, `content`] }],
          display: [
            `block`,
            `inline-block`,
            `inline`,
            `flex`,
            `inline-flex`,
            `table`,
            `inline-table`,
            `table-caption`,
            `table-cell`,
            `table-column`,
            `table-column-group`,
            `table-footer-group`,
            `table-header-group`,
            `table-row-group`,
            `table-row`,
            `flow-root`,
            `grid`,
            `inline-grid`,
            `contents`,
            `list-item`,
            `hidden`,
          ],
          sr: [`sr-only`, `not-sr-only`],
          float: [{ float: [`right`, `left`, `none`, `start`, `end`] }],
          clear: [{ clear: [`left`, `right`, `both`, `none`, `start`, `end`] }],
          isolation: [`isolate`, `isolation-auto`],
          "object-fit": [
            { object: [`contain`, `cover`, `fill`, `none`, `scale-down`] },
          ],
          "object-position": [{ object: x() }],
          overflow: [{ overflow: ee() }],
          "overflow-x": [{ "overflow-x": ee() }],
          "overflow-y": [{ "overflow-y": ee() }],
          overscroll: [{ overscroll: S() }],
          "overscroll-x": [{ "overscroll-x": S() }],
          "overscroll-y": [{ "overscroll-y": S() }],
          position: [`static`, `fixed`, `absolute`, `relative`, `sticky`],
          inset: [{ inset: w() }],
          "inset-x": [{ "inset-x": w() }],
          "inset-y": [{ "inset-y": w() }],
          start: [{ "inset-s": w(), start: w() }],
          end: [{ "inset-e": w(), end: w() }],
          "inset-bs": [{ "inset-bs": w() }],
          "inset-be": [{ "inset-be": w() }],
          top: [{ top: w() }],
          right: [{ right: w() }],
          bottom: [{ bottom: w() }],
          left: [{ left: w() }],
          visibility: [`visible`, `invisible`, `collapse`],
          z: [{ z: [rm, `auto`, Q, Z] }],
          basis: [{ basis: [nm, `full`, `auto`, s, ...C()] }],
          "flex-direction": [
            { flex: [`row`, `row-reverse`, `col`, `col-reverse`] },
          ],
          "flex-wrap": [{ flex: [`nowrap`, `wrap`, `wrap-reverse`] }],
          flex: [{ flex: [X, nm, `auto`, `initial`, `none`, Z] }],
          grow: [{ grow: [``, X, Q, Z] }],
          shrink: [{ shrink: [``, X, Q, Z] }],
          order: [{ order: [rm, `first`, `last`, `none`, Q, Z] }],
          "grid-cols": [{ "grid-cols": te() }],
          "col-start-end": [{ col: ne() }],
          "col-start": [{ "col-start": re() }],
          "col-end": [{ "col-end": re() }],
          "grid-rows": [{ "grid-rows": te() }],
          "row-start-end": [{ row: ne() }],
          "row-start": [{ "row-start": re() }],
          "row-end": [{ "row-end": re() }],
          "grid-flow": [
            { "grid-flow": [`row`, `col`, `dense`, `row-dense`, `col-dense`] },
          ],
          "auto-cols": [{ "auto-cols": ie() }],
          "auto-rows": [{ "auto-rows": ie() }],
          gap: [{ gap: C() }],
          "gap-x": [{ "gap-x": C() }],
          "gap-y": [{ "gap-y": C() }],
          "justify-content": [{ justify: [...ae(), `normal`] }],
          "justify-items": [{ "justify-items": [...oe(), `normal`] }],
          "justify-self": [{ "justify-self": [`auto`, ...oe()] }],
          "align-content": [{ content: [`normal`, ...ae()] }],
          "align-items": [{ items: [...oe(), { baseline: [``, `last`] }] }],
          "align-self": [
            { self: [`auto`, ...oe(), { baseline: [``, `last`] }] },
          ],
          "place-content": [{ "place-content": ae() }],
          "place-items": [{ "place-items": [...oe(), `baseline`] }],
          "place-self": [{ "place-self": [`auto`, ...oe()] }],
          p: [{ p: C() }],
          px: [{ px: C() }],
          py: [{ py: C() }],
          ps: [{ ps: C() }],
          pe: [{ pe: C() }],
          pbs: [{ pbs: C() }],
          pbe: [{ pbe: C() }],
          pt: [{ pt: C() }],
          pr: [{ pr: C() }],
          pb: [{ pb: C() }],
          pl: [{ pl: C() }],
          m: [{ m: se() }],
          mx: [{ mx: se() }],
          my: [{ my: se() }],
          ms: [{ ms: se() }],
          me: [{ me: se() }],
          mbs: [{ mbs: se() }],
          mbe: [{ mbe: se() }],
          mt: [{ mt: se() }],
          mr: [{ mr: se() }],
          mb: [{ mb: se() }],
          ml: [{ ml: se() }],
          "space-x": [{ "space-x": C() }],
          "space-x-reverse": [`space-x-reverse`],
          "space-y": [{ "space-y": C() }],
          "space-y-reverse": [`space-y-reverse`],
          size: [{ size: ce() }],
          "inline-size": [{ inline: [`auto`, ...le()] }],
          "min-inline-size": [{ "min-inline": [`auto`, ...le()] }],
          "max-inline-size": [{ "max-inline": [`none`, ...le()] }],
          "block-size": [{ block: [`auto`, ...T()] }],
          "min-block-size": [{ "min-block": [`auto`, ...T()] }],
          "max-block-size": [{ "max-block": [`none`, ...T()] }],
          w: [{ w: [s, `screen`, ...ce()] }],
          "min-w": [{ "min-w": [s, `screen`, `none`, ...ce()] }],
          "max-w": [
            {
              "max-w": [s, `screen`, `none`, `prose`, { screen: [o] }, ...ce()],
            },
          ],
          h: [{ h: [`screen`, `lh`, ...ce()] }],
          "min-h": [{ "min-h": [`screen`, `lh`, `none`, ...ce()] }],
          "max-h": [{ "max-h": [`screen`, `lh`, ...ce()] }],
          "font-size": [{ text: [`base`, n, xm, mm] }],
          "font-smoothing": [`antialiased`, `subpixel-antialiased`],
          "font-style": [`italic`, `not-italic`],
          "font-weight": [{ font: [r, Dm, gm] }],
          "font-stretch": [
            {
              "font-stretch": [
                `ultra-condensed`,
                `extra-condensed`,
                `condensed`,
                `semi-condensed`,
                `normal`,
                `semi-expanded`,
                `expanded`,
                `extra-expanded`,
                `ultra-expanded`,
                im,
                Z,
              ],
            },
          ],
          "font-family": [{ font: [Sm, _m, t] }],
          "font-features": [{ "font-features": [Z] }],
          "fvn-normal": [`normal-nums`],
          "fvn-ordinal": [`ordinal`],
          "fvn-slashed-zero": [`slashed-zero`],
          "fvn-figure": [`lining-nums`, `oldstyle-nums`],
          "fvn-spacing": [`proportional-nums`, `tabular-nums`],
          "fvn-fraction": [`diagonal-fractions`, `stacked-fractions`],
          tracking: [{ tracking: [i, Q, Z] }],
          "line-clamp": [{ "line-clamp": [X, `none`, Q, hm] }],
          leading: [{ leading: [a, ...C()] }],
          "list-image": [{ "list-image": [`none`, Q, Z] }],
          "list-style-position": [{ list: [`inside`, `outside`] }],
          "list-style-type": [{ list: [`disc`, `decimal`, `none`, Q, Z] }],
          "text-alignment": [
            { text: [`left`, `center`, `right`, `justify`, `start`, `end`] },
          ],
          "placeholder-color": [{ placeholder: E() }],
          "text-color": [{ text: E() }],
          "text-decoration": [
            `underline`,
            `overline`,
            `line-through`,
            `no-underline`,
          ],
          "text-decoration-style": [{ decoration: [...k(), `wavy`] }],
          "text-decoration-thickness": [
            { decoration: [X, `from-font`, `auto`, Q, mm] },
          ],
          "text-decoration-color": [{ decoration: E() }],
          "underline-offset": [{ "underline-offset": [X, `auto`, Q, Z] }],
          "text-transform": [
            `uppercase`,
            `lowercase`,
            `capitalize`,
            `normal-case`,
          ],
          "text-overflow": [`truncate`, `text-ellipsis`, `text-clip`],
          "text-wrap": [{ text: [`wrap`, `nowrap`, `balance`, `pretty`] }],
          indent: [{ indent: C() }],
          "tab-size": [{ tab: [rm, Q, Z] }],
          "vertical-align": [
            {
              align: [
                `baseline`,
                `top`,
                `middle`,
                `bottom`,
                `text-top`,
                `text-bottom`,
                `sub`,
                `super`,
                Q,
                Z,
              ],
            },
          ],
          whitespace: [
            {
              whitespace: [
                `normal`,
                `nowrap`,
                `pre`,
                `pre-line`,
                `pre-wrap`,
                `break-spaces`,
              ],
            },
          ],
          break: [{ break: [`normal`, `words`, `all`, `keep`] }],
          wrap: [{ wrap: [`break-word`, `anywhere`, `normal`] }],
          hyphens: [{ hyphens: [`none`, `manual`, `auto`] }],
          content: [{ content: [`none`, Q, Z] }],
          "bg-attachment": [{ bg: [`fixed`, `local`, `scroll`] }],
          "bg-clip": [{ "bg-clip": [`border`, `padding`, `content`, `text`] }],
          "bg-origin": [{ "bg-origin": [`border`, `padding`, `content`] }],
          "bg-position": [{ bg: D() }],
          "bg-repeat": [{ bg: ue() }],
          "bg-size": [{ bg: de() }],
          "bg-image": [
            {
              bg: [
                `none`,
                {
                  linear: [
                    { to: [`t`, `tr`, `r`, `br`, `b`, `bl`, `l`, `tl`] },
                    rm,
                    Q,
                    Z,
                  ],
                  radial: [``, Q, Z],
                  conic: [rm, Q, Z],
                },
                Tm,
                ym,
              ],
            },
          ],
          "bg-color": [{ bg: E() }],
          "gradient-from-pos": [{ from: fe() }],
          "gradient-via-pos": [{ via: fe() }],
          "gradient-to-pos": [{ to: fe() }],
          "gradient-from": [{ from: E() }],
          "gradient-via": [{ via: E() }],
          "gradient-to": [{ to: E() }],
          rounded: [{ rounded: pe() }],
          "rounded-s": [{ "rounded-s": pe() }],
          "rounded-e": [{ "rounded-e": pe() }],
          "rounded-t": [{ "rounded-t": pe() }],
          "rounded-r": [{ "rounded-r": pe() }],
          "rounded-b": [{ "rounded-b": pe() }],
          "rounded-l": [{ "rounded-l": pe() }],
          "rounded-ss": [{ "rounded-ss": pe() }],
          "rounded-se": [{ "rounded-se": pe() }],
          "rounded-ee": [{ "rounded-ee": pe() }],
          "rounded-es": [{ "rounded-es": pe() }],
          "rounded-tl": [{ "rounded-tl": pe() }],
          "rounded-tr": [{ "rounded-tr": pe() }],
          "rounded-br": [{ "rounded-br": pe() }],
          "rounded-bl": [{ "rounded-bl": pe() }],
          "border-w": [{ border: O() }],
          "border-w-x": [{ "border-x": O() }],
          "border-w-y": [{ "border-y": O() }],
          "border-w-s": [{ "border-s": O() }],
          "border-w-e": [{ "border-e": O() }],
          "border-w-bs": [{ "border-bs": O() }],
          "border-w-be": [{ "border-be": O() }],
          "border-w-t": [{ "border-t": O() }],
          "border-w-r": [{ "border-r": O() }],
          "border-w-b": [{ "border-b": O() }],
          "border-w-l": [{ "border-l": O() }],
          "divide-x": [{ "divide-x": O() }],
          "divide-x-reverse": [`divide-x-reverse`],
          "divide-y": [{ "divide-y": O() }],
          "divide-y-reverse": [`divide-y-reverse`],
          "border-style": [{ border: [...k(), `hidden`, `none`] }],
          "divide-style": [{ divide: [...k(), `hidden`, `none`] }],
          "border-color": [{ border: E() }],
          "border-color-x": [{ "border-x": E() }],
          "border-color-y": [{ "border-y": E() }],
          "border-color-s": [{ "border-s": E() }],
          "border-color-e": [{ "border-e": E() }],
          "border-color-bs": [{ "border-bs": E() }],
          "border-color-be": [{ "border-be": E() }],
          "border-color-t": [{ "border-t": E() }],
          "border-color-r": [{ "border-r": E() }],
          "border-color-b": [{ "border-b": E() }],
          "border-color-l": [{ "border-l": E() }],
          "divide-color": [{ divide: E() }],
          "outline-style": [{ outline: [...k(), `none`, `hidden`] }],
          "outline-offset": [{ "outline-offset": [X, Q, Z] }],
          "outline-w": [{ outline: [``, X, xm, mm] }],
          "outline-color": [{ outline: E() }],
          shadow: [{ shadow: [``, `none`, u, Em, bm] }],
          "shadow-color": [{ shadow: E() }],
          "inset-shadow": [{ "inset-shadow": [`none`, d, Em, bm] }],
          "inset-shadow-color": [{ "inset-shadow": E() }],
          "ring-w": [{ ring: O() }],
          "ring-w-inset": [`ring-inset`],
          "ring-color": [{ ring: E() }],
          "ring-offset-w": [{ "ring-offset": [X, mm] }],
          "ring-offset-color": [{ "ring-offset": E() }],
          "inset-ring-w": [{ "inset-ring": O() }],
          "inset-ring-color": [{ "inset-ring": E() }],
          "text-shadow": [{ "text-shadow": [`none`, f, Em, bm] }],
          "text-shadow-color": [{ "text-shadow": E() }],
          opacity: [{ opacity: [X, Q, Z] }],
          "mix-blend": [
            { "mix-blend": [...me(), `plus-darker`, `plus-lighter`] },
          ],
          "bg-blend": [{ "bg-blend": me() }],
          "mask-clip": [
            {
              "mask-clip": [
                `border`,
                `padding`,
                `content`,
                `fill`,
                `stroke`,
                `view`,
              ],
            },
            `mask-no-clip`,
          ],
          "mask-composite": [
            { mask: [`add`, `subtract`, `intersect`, `exclude`] },
          ],
          "mask-image-linear-pos": [{ "mask-linear": [X] }],
          "mask-image-linear-from-pos": [{ "mask-linear-from": he() }],
          "mask-image-linear-to-pos": [{ "mask-linear-to": he() }],
          "mask-image-linear-from-color": [{ "mask-linear-from": E() }],
          "mask-image-linear-to-color": [{ "mask-linear-to": E() }],
          "mask-image-t-from-pos": [{ "mask-t-from": he() }],
          "mask-image-t-to-pos": [{ "mask-t-to": he() }],
          "mask-image-t-from-color": [{ "mask-t-from": E() }],
          "mask-image-t-to-color": [{ "mask-t-to": E() }],
          "mask-image-r-from-pos": [{ "mask-r-from": he() }],
          "mask-image-r-to-pos": [{ "mask-r-to": he() }],
          "mask-image-r-from-color": [{ "mask-r-from": E() }],
          "mask-image-r-to-color": [{ "mask-r-to": E() }],
          "mask-image-b-from-pos": [{ "mask-b-from": he() }],
          "mask-image-b-to-pos": [{ "mask-b-to": he() }],
          "mask-image-b-from-color": [{ "mask-b-from": E() }],
          "mask-image-b-to-color": [{ "mask-b-to": E() }],
          "mask-image-l-from-pos": [{ "mask-l-from": he() }],
          "mask-image-l-to-pos": [{ "mask-l-to": he() }],
          "mask-image-l-from-color": [{ "mask-l-from": E() }],
          "mask-image-l-to-color": [{ "mask-l-to": E() }],
          "mask-image-x-from-pos": [{ "mask-x-from": he() }],
          "mask-image-x-to-pos": [{ "mask-x-to": he() }],
          "mask-image-x-from-color": [{ "mask-x-from": E() }],
          "mask-image-x-to-color": [{ "mask-x-to": E() }],
          "mask-image-y-from-pos": [{ "mask-y-from": he() }],
          "mask-image-y-to-pos": [{ "mask-y-to": he() }],
          "mask-image-y-from-color": [{ "mask-y-from": E() }],
          "mask-image-y-to-color": [{ "mask-y-to": E() }],
          "mask-image-radial": [{ "mask-radial": [Q, Z] }],
          "mask-image-radial-from-pos": [{ "mask-radial-from": he() }],
          "mask-image-radial-to-pos": [{ "mask-radial-to": he() }],
          "mask-image-radial-from-color": [{ "mask-radial-from": E() }],
          "mask-image-radial-to-color": [{ "mask-radial-to": E() }],
          "mask-image-radial-shape": [{ "mask-radial": [`circle`, `ellipse`] }],
          "mask-image-radial-size": [
            {
              "mask-radial": [
                { closest: [`side`, `corner`], farthest: [`side`, `corner`] },
              ],
            },
          ],
          "mask-image-radial-pos": [{ "mask-radial-at": b() }],
          "mask-image-conic-pos": [{ "mask-conic": [X] }],
          "mask-image-conic-from-pos": [{ "mask-conic-from": he() }],
          "mask-image-conic-to-pos": [{ "mask-conic-to": he() }],
          "mask-image-conic-from-color": [{ "mask-conic-from": E() }],
          "mask-image-conic-to-color": [{ "mask-conic-to": E() }],
          "mask-mode": [{ mask: [`alpha`, `luminance`, `match`] }],
          "mask-origin": [
            {
              "mask-origin": [
                `border`,
                `padding`,
                `content`,
                `fill`,
                `stroke`,
                `view`,
              ],
            },
          ],
          "mask-position": [{ mask: D() }],
          "mask-repeat": [{ mask: ue() }],
          "mask-size": [{ mask: de() }],
          "mask-type": [{ "mask-type": [`alpha`, `luminance`] }],
          "mask-image": [{ mask: [`none`, Q, Z] }],
          filter: [{ filter: [``, `none`, Q, Z] }],
          blur: [{ blur: ge() }],
          brightness: [{ brightness: [X, Q, Z] }],
          contrast: [{ contrast: [X, Q, Z] }],
          "drop-shadow": [{ "drop-shadow": [``, `none`, p, Em, bm] }],
          "drop-shadow-color": [{ "drop-shadow": E() }],
          grayscale: [{ grayscale: [``, X, Q, Z] }],
          "hue-rotate": [{ "hue-rotate": [X, Q, Z] }],
          invert: [{ invert: [``, X, Q, Z] }],
          saturate: [{ saturate: [X, Q, Z] }],
          sepia: [{ sepia: [``, X, Q, Z] }],
          "backdrop-filter": [{ "backdrop-filter": [``, `none`, Q, Z] }],
          "backdrop-blur": [{ "backdrop-blur": ge() }],
          "backdrop-brightness": [{ "backdrop-brightness": [X, Q, Z] }],
          "backdrop-contrast": [{ "backdrop-contrast": [X, Q, Z] }],
          "backdrop-grayscale": [{ "backdrop-grayscale": [``, X, Q, Z] }],
          "backdrop-hue-rotate": [{ "backdrop-hue-rotate": [X, Q, Z] }],
          "backdrop-invert": [{ "backdrop-invert": [``, X, Q, Z] }],
          "backdrop-opacity": [{ "backdrop-opacity": [X, Q, Z] }],
          "backdrop-saturate": [{ "backdrop-saturate": [X, Q, Z] }],
          "backdrop-sepia": [{ "backdrop-sepia": [``, X, Q, Z] }],
          "border-collapse": [{ border: [`collapse`, `separate`] }],
          "border-spacing": [{ "border-spacing": C() }],
          "border-spacing-x": [{ "border-spacing-x": C() }],
          "border-spacing-y": [{ "border-spacing-y": C() }],
          "table-layout": [{ table: [`auto`, `fixed`] }],
          caption: [{ caption: [`top`, `bottom`] }],
          transition: [
            {
              transition: [
                ``,
                `all`,
                `colors`,
                `opacity`,
                `shadow`,
                `transform`,
                `none`,
                Q,
                Z,
              ],
            },
          ],
          "transition-behavior": [{ transition: [`normal`, `discrete`] }],
          duration: [{ duration: [X, `initial`, Q, Z] }],
          ease: [{ ease: [`linear`, `initial`, _, Q, Z] }],
          delay: [{ delay: [X, Q, Z] }],
          animate: [{ animate: [`none`, v, Q, Z] }],
          backface: [{ backface: [`hidden`, `visible`] }],
          perspective: [{ perspective: [h, Q, Z] }],
          "perspective-origin": [{ "perspective-origin": x() }],
          rotate: [{ rotate: _e() }],
          "rotate-x": [{ "rotate-x": _e() }],
          "rotate-y": [{ "rotate-y": _e() }],
          "rotate-z": [{ "rotate-z": _e() }],
          scale: [{ scale: A() }],
          "scale-x": [{ "scale-x": A() }],
          "scale-y": [{ "scale-y": A() }],
          "scale-z": [{ "scale-z": A() }],
          "scale-3d": [`scale-3d`],
          skew: [{ skew: ve() }],
          "skew-x": [{ "skew-x": ve() }],
          "skew-y": [{ "skew-y": ve() }],
          transform: [{ transform: [Q, Z, ``, `none`, `gpu`, `cpu`] }],
          "transform-origin": [{ origin: x() }],
          "transform-style": [{ transform: [`3d`, `flat`] }],
          translate: [{ translate: ye() }],
          "translate-x": [{ "translate-x": ye() }],
          "translate-y": [{ "translate-y": ye() }],
          "translate-z": [{ "translate-z": ye() }],
          "translate-none": [`translate-none`],
          zoom: [{ zoom: [rm, Q, Z] }],
          accent: [{ accent: E() }],
          appearance: [{ appearance: [`none`, `auto`] }],
          "caret-color": [{ caret: E() }],
          "color-scheme": [
            {
              scheme: [
                `normal`,
                `dark`,
                `light`,
                `light-dark`,
                `only-dark`,
                `only-light`,
              ],
            },
          ],
          cursor: [
            {
              cursor: [
                `auto`,
                `default`,
                `pointer`,
                `wait`,
                `text`,
                `move`,
                `help`,
                `not-allowed`,
                `none`,
                `context-menu`,
                `progress`,
                `cell`,
                `crosshair`,
                `vertical-text`,
                `alias`,
                `copy`,
                `no-drop`,
                `grab`,
                `grabbing`,
                `all-scroll`,
                `col-resize`,
                `row-resize`,
                `n-resize`,
                `e-resize`,
                `s-resize`,
                `w-resize`,
                `ne-resize`,
                `nw-resize`,
                `se-resize`,
                `sw-resize`,
                `ew-resize`,
                `ns-resize`,
                `nesw-resize`,
                `nwse-resize`,
                `zoom-in`,
                `zoom-out`,
                Q,
                Z,
              ],
            },
          ],
          "field-sizing": [{ "field-sizing": [`fixed`, `content`] }],
          "pointer-events": [{ "pointer-events": [`auto`, `none`] }],
          resize: [{ resize: [`none`, ``, `y`, `x`] }],
          "scroll-behavior": [{ scroll: [`auto`, `smooth`] }],
          "scrollbar-thumb-color": [{ "scrollbar-thumb": E() }],
          "scrollbar-track-color": [{ "scrollbar-track": E() }],
          "scrollbar-gutter": [
            { "scrollbar-gutter": [`auto`, `stable`, `both`] },
          ],
          "scrollbar-w": [{ scrollbar: [`auto`, `thin`, `none`] }],
          "scroll-m": [{ "scroll-m": C() }],
          "scroll-mx": [{ "scroll-mx": C() }],
          "scroll-my": [{ "scroll-my": C() }],
          "scroll-ms": [{ "scroll-ms": C() }],
          "scroll-me": [{ "scroll-me": C() }],
          "scroll-mbs": [{ "scroll-mbs": C() }],
          "scroll-mbe": [{ "scroll-mbe": C() }],
          "scroll-mt": [{ "scroll-mt": C() }],
          "scroll-mr": [{ "scroll-mr": C() }],
          "scroll-mb": [{ "scroll-mb": C() }],
          "scroll-ml": [{ "scroll-ml": C() }],
          "scroll-p": [{ "scroll-p": C() }],
          "scroll-px": [{ "scroll-px": C() }],
          "scroll-py": [{ "scroll-py": C() }],
          "scroll-ps": [{ "scroll-ps": C() }],
          "scroll-pe": [{ "scroll-pe": C() }],
          "scroll-pbs": [{ "scroll-pbs": C() }],
          "scroll-pbe": [{ "scroll-pbe": C() }],
          "scroll-pt": [{ "scroll-pt": C() }],
          "scroll-pr": [{ "scroll-pr": C() }],
          "scroll-pb": [{ "scroll-pb": C() }],
          "scroll-pl": [{ "scroll-pl": C() }],
          "snap-align": [{ snap: [`start`, `end`, `center`, `align-none`] }],
          "snap-stop": [{ snap: [`normal`, `always`] }],
          "snap-type": [{ snap: [`none`, `x`, `y`, `both`] }],
          "snap-strictness": [{ snap: [`mandatory`, `proximity`] }],
          touch: [{ touch: [`auto`, `none`, `manipulation`] }],
          "touch-x": [{ "touch-pan": [`x`, `left`, `right`] }],
          "touch-y": [{ "touch-pan": [`y`, `up`, `down`] }],
          "touch-pz": [`touch-pinch-zoom`],
          select: [{ select: [`none`, `text`, `all`, `auto`] }],
          "will-change": [
            {
              "will-change": [`auto`, `scroll`, `contents`, `transform`, Q, Z],
            },
          ],
          fill: [{ fill: [`none`, ...E()] }],
          "stroke-w": [{ stroke: [X, xm, mm, hm] }],
          stroke: [{ stroke: [`none`, ...E()] }],
          "forced-color-adjust": [{ "forced-color-adjust": [`auto`, `none`] }],
        },
        conflictingClassGroups: {
          "container-named": [`container-type`],
          overflow: [`overflow-x`, `overflow-y`],
          overscroll: [`overscroll-x`, `overscroll-y`],
          inset: [
            `inset-x`,
            `inset-y`,
            `inset-bs`,
            `inset-be`,
            `start`,
            `end`,
            `top`,
            `right`,
            `bottom`,
            `left`,
          ],
          "inset-x": [`right`, `left`],
          "inset-y": [`top`, `bottom`],
          flex: [`basis`, `grow`, `shrink`],
          gap: [`gap-x`, `gap-y`],
          p: [`px`, `py`, `ps`, `pe`, `pbs`, `pbe`, `pt`, `pr`, `pb`, `pl`],
          px: [`pr`, `pl`],
          py: [`pt`, `pb`],
          m: [`mx`, `my`, `ms`, `me`, `mbs`, `mbe`, `mt`, `mr`, `mb`, `ml`],
          mx: [`mr`, `ml`],
          my: [`mt`, `mb`],
          size: [`w`, `h`],
          "font-size": [`leading`],
          "fvn-normal": [
            `fvn-ordinal`,
            `fvn-slashed-zero`,
            `fvn-figure`,
            `fvn-spacing`,
            `fvn-fraction`,
          ],
          "fvn-ordinal": [`fvn-normal`],
          "fvn-slashed-zero": [`fvn-normal`],
          "fvn-figure": [`fvn-normal`],
          "fvn-spacing": [`fvn-normal`],
          "fvn-fraction": [`fvn-normal`],
          "line-clamp": [`display`, `overflow`],
          rounded: [
            `rounded-s`,
            `rounded-e`,
            `rounded-t`,
            `rounded-r`,
            `rounded-b`,
            `rounded-l`,
            `rounded-ss`,
            `rounded-se`,
            `rounded-ee`,
            `rounded-es`,
            `rounded-tl`,
            `rounded-tr`,
            `rounded-br`,
            `rounded-bl`,
          ],
          "rounded-s": [`rounded-ss`, `rounded-es`],
          "rounded-e": [`rounded-se`, `rounded-ee`],
          "rounded-t": [`rounded-tl`, `rounded-tr`],
          "rounded-r": [`rounded-tr`, `rounded-br`],
          "rounded-b": [`rounded-br`, `rounded-bl`],
          "rounded-l": [`rounded-tl`, `rounded-bl`],
          "border-spacing": [`border-spacing-x`, `border-spacing-y`],
          "border-w": [
            `border-w-x`,
            `border-w-y`,
            `border-w-s`,
            `border-w-e`,
            `border-w-bs`,
            `border-w-be`,
            `border-w-t`,
            `border-w-r`,
            `border-w-b`,
            `border-w-l`,
          ],
          "border-w-x": [`border-w-r`, `border-w-l`],
          "border-w-y": [`border-w-t`, `border-w-b`],
          "border-color": [
            `border-color-x`,
            `border-color-y`,
            `border-color-s`,
            `border-color-e`,
            `border-color-bs`,
            `border-color-be`,
            `border-color-t`,
            `border-color-r`,
            `border-color-b`,
            `border-color-l`,
          ],
          "border-color-x": [`border-color-r`, `border-color-l`],
          "border-color-y": [`border-color-t`, `border-color-b`],
          translate: [`translate-x`, `translate-y`, `translate-none`],
          "translate-none": [
            `translate`,
            `translate-x`,
            `translate-y`,
            `translate-z`,
          ],
          "scroll-m": [
            `scroll-mx`,
            `scroll-my`,
            `scroll-ms`,
            `scroll-me`,
            `scroll-mbs`,
            `scroll-mbe`,
            `scroll-mt`,
            `scroll-mr`,
            `scroll-mb`,
            `scroll-ml`,
          ],
          "scroll-mx": [`scroll-mr`, `scroll-ml`],
          "scroll-my": [`scroll-mt`, `scroll-mb`],
          "scroll-p": [
            `scroll-px`,
            `scroll-py`,
            `scroll-ps`,
            `scroll-pe`,
            `scroll-pbs`,
            `scroll-pbe`,
            `scroll-pt`,
            `scroll-pr`,
            `scroll-pb`,
            `scroll-pl`,
          ],
          "scroll-px": [`scroll-pr`, `scroll-pl`],
          "scroll-py": [`scroll-pt`, `scroll-pb`],
          touch: [`touch-x`, `touch-y`, `touch-pz`],
          "touch-x": [`touch`],
          "touch-y": [`touch`],
          "touch-pz": [`touch`],
        },
        conflictingClassGroupModifiers: { "font-size": [`leading`] },
        postfixLookupClassGroups: [`container-type`],
        orderSensitiveModifiers: [
          `*`,
          `**`,
          `after`,
          `backdrop`,
          `before`,
          `details-content`,
          `file`,
          `first-letter`,
          `first-line`,
          `marker`,
          `placeholder`,
          `selection`,
        ],
      };
    },
    zm = (
      e,
      {
        cacheSize: t,
        prefix: n,
        experimentalParseClassName: r,
        extend: i = {},
        override: a = {},
      },
    ) => (
      Bm(e, `cacheSize`, t),
      Bm(e, `prefix`, n),
      Bm(e, `experimentalParseClassName`, r),
      Vm(e.theme, a.theme),
      Vm(e.classGroups, a.classGroups),
      Vm(e.conflictingClassGroups, a.conflictingClassGroups),
      Vm(e.conflictingClassGroupModifiers, a.conflictingClassGroupModifiers),
      Bm(e, `postfixLookupClassGroups`, a.postfixLookupClassGroups),
      Bm(e, `orderSensitiveModifiers`, a.orderSensitiveModifiers),
      Hm(e.theme, i.theme),
      Hm(e.classGroups, i.classGroups),
      Hm(e.conflictingClassGroups, i.conflictingClassGroups),
      Hm(e.conflictingClassGroupModifiers, i.conflictingClassGroupModifiers),
      Um(e, i, `postfixLookupClassGroups`),
      Um(e, i, `orderSensitiveModifiers`),
      e
    ),
    Bm = (e, t, n) => {
      n !== void 0 && (e[t] = n);
    },
    Vm = (e, t) => {
      if (t) for (let n in t) Bm(e, n, t[n]);
    },
    Hm = (e, t) => {
      if (t) for (let n in t) Um(e, t, n);
    },
    Um = (e, t, n) => {
      let r = t[n];
      r !== void 0 && (e[n] = e[n] ? e[n].concat(r) : r);
    },
    Wm = ((e, ...t) =>
      typeof e == `function` ? Gp(Rm, e, ...t) : Gp(() => zm(Rm(), e), ...t))({
      extend: {
        classGroups: {
          "font-size": [
            {
              text: [
                (e) =>
                  [`heading`, `body`, `codeline`, `display`].some((t) =>
                    e.includes(t),
                  ) &&
                  [`xs`, `sm`, `md`, `lg`, `xl`, `2xl`].some((t) =>
                    e.includes(t),
                  ),
              ],
            },
          ],
        },
      },
    });
  function Gm(...e) {
    return Wm(pp(e));
  }
  function Km(e) {
    return typeof e == `object` && !!e && !Array.isArray(e);
  }
  function qm(e) {
    return typeof e == `string` && up.includes(e);
  }
  function Jm(e) {
    if (!e) return null;
    let t;
    try {
      t = JSON.parse(e);
    } catch {
      return null;
    }
    if (!Km(t)) return null;
    let n = t.annotations,
      r = t.tools;
    if (!Array.isArray(n) || !Array.isArray(r)) return null;
    let i = [];
    for (let e of n) {
      if (!Km(e)) return null;
      let t = e.name,
        n = e.mode;
      if (!qm(t) || (n !== `snapshot` && n !== `live`)) return null;
      i.push({ name: t, mode: n });
    }
    return r.every((e) => typeof e == `string`)
      ? { annotations: i, tools: r }
      : null;
  }
  var Ym = c((e) => {
      var t = Symbol.for(`react.transitional.element`),
        n = Symbol.for(`react.fragment`);
      function r(e, n, r) {
        var i = null;
        if (
          (r !== void 0 && (i = `` + r),
          n.key !== void 0 && (i = `` + n.key),
          `key` in n)
        )
          for (var a in ((r = {}), n)) a !== `key` && (r[a] = n[a]);
        else r = n;
        return (
          (n = r.ref),
          {
            $$typeof: t,
            type: e,
            key: i,
            ref: n === void 0 ? null : n,
            props: r,
          }
        );
      }
      ((e.Fragment = n), (e.jsx = r), (e.jsxs = r));
    }),
    $ = c((e, t) => {
      t.exports = Ym();
    })(),
    Xm = `Couldn't load this server's tools.`;
  async function Zm(e, t, n, r) {
    let i = new qf(new URL(e, window.location.origin), {
        requestInit: {
          credentials: `same-origin`,
          headers: {
            "Gram-Consent-State": t,
            "Gram-Consent-Csrf": n,
            "Gram-Consent-Inventory-Attempt": r,
          },
        },
      }),
      a = new yd({ name: `gram-consent`, version: `1.0.0` });
    try {
      await a.connect(i);
      let e = [],
        t = new Set(),
        n = { count: 0, names: [] },
        r;
      do {
        let i = await a.listTools(r ? { cursor: r } : void 0);
        for (let n of i.tools) {
          if (t.has(n.name)) throw Error(`duplicate tool name`);
          (t.add(n.name),
            e.push({ name: n.name, annotations: $m(n.annotations) }));
        }
        let o = i._meta?.[`gram.dev/roleHiddenTools`];
        if (
          Km(o) &&
          (typeof o.count == `number` && o.count > 0 && (n.count += o.count),
          Array.isArray(o.names))
        )
          for (let e of o.names)
            typeof e == `string` && e !== `` && n.names.push(e);
        r = i.nextCursor;
      } while (r);
      return { tools: e, roleHiddenTools: n };
    } finally {
      (await i.terminateSession().catch(() => void 0),
        await a.close().catch(() => void 0));
    }
  }
  var Qm = [
    [`readOnlyHint`, `read_only`],
    [`destructiveHint`, `destructive`],
    [`idempotentHint`, `idempotent`],
    [`openWorldHint`, `open_world`],
  ];
  function $m(e) {
    return Km(e) ? Qm.filter(([t]) => e[t] === !0).map(([, e]) => e) : [];
  }
  function eh(e) {
    return (e instanceof Kf ? e.code : void 0) === 409
      ? {
          name: `error`,
          conflict: !0,
          message: `The upstream service is not connected.`,
        }
      : { name: `error`, conflict: !1, message: Xm };
  }
  function th(e) {
    return e === `all`
      ? `All tools`
      : e === `none`
        ? `No annotation`
        : (dp.find((t) => t.key === e)?.label ?? e);
  }
  function nh(e, t) {
    return t === `all`
      ? e
      : t === `none`
        ? e.filter((e) => e.annotations.length === 0)
        : e.filter((e) => e.annotations.includes(t));
  }
  function rh({
    toolsUrl: e,
    state: t,
    csrfToken: n,
    formId: r,
    approveButtonId: i,
    consentEnabled: a,
    serverName: o,
    prefill: s,
  }) {
    let [c, l] = (0, ep.useState)({ name: `loading` }),
      [u, d] = (0, ep.useState)(() => crypto.randomUUID()),
      [f, p] = (0, ep.useState)(!0),
      [m, h] = (0, ep.useState)(new Map()),
      [g, _] = (0, ep.useState)(new Set()),
      [v, y] = (0, ep.useState)(`all`),
      [b, x] = (0, ep.useState)(0),
      [ee, S] = (0, ep.useState)(``);
    (0, ep.useEffect)(() => {
      let r = !1;
      return (
        l({ name: `loading` }),
        (async () => {
          let i = await Zm(e, t, n, u);
          if (!r) {
            if (s === null) (p(!0), h(new Map()), _(new Set()));
            else {
              let e = new Set(i.tools.map((e) => e.name)),
                t = new Map(),
                n = 0;
              for (let e of s.annotations) {
                if (!i.tools.some((t) => t.annotations.includes(e.name))) {
                  n += 1;
                  continue;
                }
                t.set(e.name, e.mode === `live` ? `live` : `snapshot`);
              }
              (x(n), p(!1), h(t), _(new Set(s.tools.filter((t) => e.has(t)))));
            }
            l({ name: `ready`, inventory: i });
          }
        })().catch((e) => {
          r || l(eh(e));
        }),
        () => {
          r = !0;
        }
      );
    }, [u, e, t, n, s]);
    let C = c.name === `ready` && a;
    (0, ep.useEffect)(() => {
      let e = document.getElementById(i);
      e instanceof HTMLButtonElement && (e.disabled = !C);
    }, [i, C]);
    let w = (0, ep.useMemo)(
        () => (c.name === `ready` ? c.inventory.tools : []),
        [c],
      ),
      te = (0, ep.useMemo)(() => {
        if (f) return new Set(w.map((e) => e.name));
        let e = new Set(g);
        for (let t of w) t.annotations.some((e) => m.has(e)) && e.add(t.name);
        return e;
      }, [f, m, g, w]),
      ne = (0, ep.useMemo)(
        () => [...m].filter(([, e]) => e === `snapshot`).map(([e]) => e),
        [m],
      ),
      re = (0, ep.useMemo)(
        () => [...m].filter(([, e]) => e === `live`).map(([e]) => e),
        [m],
      );
    if (c.name === `loading`)
      return (0, $.jsx)(`div`, {
        role: `status`,
        "aria-live": `polite`,
        className: `border-border text-muted-foreground border px-3 py-3 text-sm`,
        children: `Loading available tools…`,
      });
    if (c.name === `error`)
      return (0, $.jsxs)(`div`, {
        role: `alert`,
        className: `border-border space-y-1 border px-3 py-3 text-sm`,
        children: [
          (0, $.jsxs)(`p`, {
            className: `text-muted-foreground flex items-center gap-1.5`,
            children: [
              (0, $.jsx)(lp, { className: `h-3.5 w-3.5 shrink-0` }),
              c.message,
            ],
          }),
          c.conflict &&
            (0, $.jsx)(`p`, {
              className: `text-muted-foreground`,
              children: `Connect the service above, then retry.`,
            }),
          (0, $.jsx)(`button`, {
            type: `button`,
            onClick: () => d(crypto.randomUUID()),
            className: `text-primary hover:underline`,
            children: `Retry`,
          }),
        ],
      });
    let ie = [
        { id: `all`, label: `All tools`, count: w.length },
        ...dp
          .map((e) => ({
            id: e.key,
            label: e.label,
            count: nh(w, e.key).length,
          }))
          .filter((e) => e.count > 0),
        ...(nh(w, `none`).length > 0
          ? [
              {
                id: `none`,
                label: `No annotation`,
                count: nh(w, `none`).length,
              },
            ]
          : []),
      ],
      ae = nh(w, v),
      oe = ee.trim().toLowerCase(),
      se = oe === `` ? ae : ae.filter((e) => e.name.toLowerCase().includes(oe)),
      ce = (e) => {
        if (e === `all`) return f;
        if (e === `none`) {
          let e = nh(w, `none`);
          return e.length > 0 && e.every((e) => te.has(e.name));
        }
        return m.has(e);
      },
      le = () => {
        if (v === `all`) {
          p((e) => !e);
          return;
        }
        if (v === `none`) {
          let e = nh(w, `none`),
            t = e.every((e) => g.has(e.name)),
            n = new Set(g);
          (e.forEach((e) => {
            t ? n.delete(e.name) : n.add(e.name);
          }),
            _(n),
            p(!1));
          return;
        }
        let e = new Map(m);
        (e.has(v) ? e.delete(v) : e.set(v, `live`), h(e), p(!1));
      },
      T = (e) => {
        let t = new Set(g);
        (t.has(e) ? t.delete(e) : t.add(e), _(t), p(!1));
      },
      E = f
        ? 0
        : [...g].filter(
            (e) =>
              !w.some(
                (t) => t.name === e && t.annotations.some((e) => m.has(e)),
              ),
          ).length;
    return (0, $.jsxs)(`div`, {
      children: [
        (0, $.jsxs)(`div`, {
          className: `border-border grid grid-cols-[9.5rem_1fr] border`,
          children: [
            (0, $.jsx)(`nav`, {
              "aria-label": `Tool groups`,
              className: `border-border flex flex-col border-r py-1`,
              children: ie.map((e) =>
                (0, $.jsxs)(
                  `button`,
                  {
                    type: `button`,
                    "aria-current": v === e.id,
                    onClick: () => y(e.id),
                    className: Gm(
                      `hover:bg-accent flex cursor-pointer items-center gap-1.5 px-2.5 py-1.5 text-left text-sm`,
                      v === e.id && `bg-accent font-medium`,
                    ),
                    children: [
                      (0, $.jsx)(`span`, {
                        className: `flex w-3.5 shrink-0 justify-center`,
                        children:
                          ce(e.id) &&
                          (0, $.jsx)(ap, {
                            "aria-label": `granted`,
                            className: `text-success h-3 w-3`,
                          }),
                      }),
                      (0, $.jsx)(`span`, {
                        className: `min-w-0 flex-1 truncate`,
                        children: e.label,
                      }),
                      (0, $.jsx)(`span`, {
                        className: `text-muted-foreground text-xs`,
                        children: e.count,
                      }),
                    ],
                  },
                  e.id,
                ),
              ),
            }),
            (0, $.jsxs)(`div`, {
              className: `flex min-w-0 flex-col`,
              children: [
                (0, $.jsxs)(`div`, {
                  className: `border-border flex items-center justify-between border-b px-3 py-2`,
                  children: [
                    (0, $.jsx)(`span`, {
                      className: `text-sm font-medium`,
                      children: th(v),
                    }),
                    (0, $.jsxs)(`div`, {
                      className: `flex items-center gap-3`,
                      children: [
                        v !== `all` &&
                          v !== `none` &&
                          m.has(v) &&
                          (0, $.jsxs)(`button`, {
                            type: `button`,
                            role: `checkbox`,
                            "aria-checked": m.get(v) === `live`,
                            onClick: () => {
                              let e = new Map(m);
                              (e.set(
                                v,
                                e.get(v) === `live` ? `snapshot` : `live`,
                              ),
                                h(e));
                            },
                            className: `text-muted-foreground flex cursor-pointer items-center gap-1.5 text-xs`,
                            children: [
                              (0, $.jsx)(ah, { checked: m.get(v) === `live` }),
                              `Include future matching tools`,
                            ],
                          }),
                        (0, $.jsxs)(`button`, {
                          type: `button`,
                          role: `checkbox`,
                          "aria-checked": ce(v),
                          onClick: le,
                          className: `flex cursor-pointer items-center gap-1.5 text-sm`,
                          children: [
                            (0, $.jsx)(ah, { checked: ce(v) }),
                            `All `,
                            ae.length,
                          ],
                        }),
                      ],
                    }),
                  ],
                }),
                (0, $.jsx)(`input`, {
                  type: `search`,
                  value: ee,
                  onChange: (e) => S(e.target.value),
                  placeholder: `Search ${th(v).toLowerCase()}…`,
                  "aria-label": `Search tools`,
                  className: `border-border placeholder:text-muted-foreground border-b px-3 py-1.5 text-sm outline-none`,
                }),
                (0, $.jsxs)(`div`, {
                  className: `max-h-[300px] min-h-0 overflow-y-auto`,
                  children: [
                    se.map((e) => {
                      let t = f || e.annotations.some((e) => m.has(e)),
                        n = te.has(e.name);
                      return (0, $.jsxs)(
                        `div`,
                        {
                          role: `checkbox`,
                          "aria-checked": n,
                          "aria-disabled": t,
                          tabIndex: t ? -1 : 0,
                          onClick: t ? void 0 : () => T(e.name),
                          onKeyDown: t
                            ? void 0
                            : (t) => {
                                (t.key === ` ` || t.key === `Enter`) &&
                                  (t.preventDefault(), T(e.name));
                              },
                          className: Gm(
                            `flex items-center gap-2 px-3 py-1`,
                            t
                              ? `cursor-default`
                              : `hover:bg-accent cursor-pointer`,
                          ),
                          children: [
                            (0, $.jsx)(ah, { checked: n }),
                            (0, $.jsx)(`span`, {
                              className: `min-w-0 flex-1 truncate font-mono text-xs`,
                              title: e.name,
                              children: e.name,
                            }),
                            !f &&
                              t &&
                              (0, $.jsx)(`span`, {
                                className: `text-muted-foreground shrink-0 text-[10px]`,
                                children: `via annotation`,
                              }),
                            !t &&
                              g.has(e.name) &&
                              (0, $.jsx)(`span`, {
                                className: `text-muted-foreground shrink-0 text-[10px]`,
                                children: `picked`,
                              }),
                          ],
                        },
                        e.name,
                      );
                    }),
                    se.length === 0 &&
                      (0, $.jsx)(`p`, {
                        className: `text-muted-foreground px-3 py-2 text-sm`,
                        children:
                          oe === ``
                            ? `No tools in this group.`
                            : `No tools match your search.`,
                      }),
                  ],
                }),
              ],
            }),
          ],
        }),
        c.inventory.roleHiddenTools.count > 0 &&
          (0, $.jsx)(ih, { hidden: c.inventory.roleHiddenTools }),
        b > 0 &&
          (0, $.jsx)(`p`, {
            className: `text-muted-foreground pt-1 text-xs`,
            children:
              b === 1
                ? `One previously granted annotation no longer matches any tool and was removed.`
                : `${b} previously granted annotations no longer match any tool and were removed.`,
          }),
        (0, $.jsxs)(`div`, {
          className: `text-muted-foreground flex flex-wrap items-center justify-between gap-x-3 gap-y-0.5 pt-1.5 text-xs`,
          children: [
            (0, $.jsxs)(`span`, {
              children: [
                (0, $.jsx)(`b`, {
                  className: `text-foreground font-medium`,
                  children: te.size,
                }),
                ` of`,
                ` `,
                w.length,
                ` tools in scope on `,
                o,
                !f &&
                  m.size > 0 &&
                  (0, $.jsxs)($.Fragment, {
                    children: [
                      ` · `,
                      [...m]
                        .map(
                          ([e, t]) =>
                            th(e).toLowerCase() +
                            (t === `live` ? ` (live)` : ` (frozen)`),
                        )
                        .join(`, `),
                    ],
                  }),
                E > 0 && ` · ${E} picked`,
              ],
            }),
            (0, $.jsx)(`span`, {
              children: f
                ? `Includes tools the server adds later`
                : re.length > 0
                  ? `Live grants include future matching tools`
                  : `New tools require approval`,
            }),
          ],
        }),
        (0, $.jsx)(oh, {
          formId: r,
          inventoryID: u,
          allGrant: f,
          snapshotAnnotations: ne,
          liveAnnotations: re,
          tools: f ? [] : [...g].sort(),
        }),
      ],
    });
  }
  function ih({ hidden: e }) {
    let t =
        e.count === 1
          ? `1 tool is hidden by your role and cannot be granted here.`
          : `${e.count} tools are hidden by your role and cannot be granted here.`,
      n = e.count - e.names.length;
    return (0, $.jsxs)(`div`, {
      className: `group relative w-fit pt-1`,
      children: [
        (0, $.jsx)(`p`, {
          tabIndex: 0,
          className: `text-muted-foreground cursor-help text-xs underline decoration-dotted underline-offset-2`,
          children: t,
        }),
        e.names.length > 0 &&
          (0, $.jsxs)(`div`, {
            role: `tooltip`,
            className: `border-border bg-background absolute bottom-full left-0 z-10 mb-1 hidden max-h-44 w-max max-w-72 overflow-y-auto rounded-md border px-3 py-2 shadow-md group-focus-within:block group-hover:block`,
            children: [
              (0, $.jsx)(`ul`, {
                className: `m-0 list-none space-y-0.5 p-0 font-mono text-xs`,
                children: e.names.map((e) =>
                  (0, $.jsx)(`li`, { children: e }, e),
                ),
              }),
              n > 0 &&
                (0, $.jsxs)(`p`, {
                  className: `text-muted-foreground pt-1 text-xs`,
                  children: [`and `, n, ` more`],
                }),
            ],
          }),
      ],
    });
  }
  function ah({ checked: e }) {
    return (0, $.jsx)(`span`, {
      "aria-hidden": !0,
      className: Gm(
        `border-border flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-[3px] border`,
        e && `bg-foreground border-foreground`,
      ),
      children:
        e && (0, $.jsx)(ap, { className: `text-background h-2.5 w-2.5` }),
    });
  }
  function oh({
    formId: e,
    inventoryID: t,
    allGrant: n,
    snapshotAnnotations: r,
    liveAnnotations: i,
    tools: a,
  }) {
    let o = (0, $.jsx)(`input`, {
      type: `hidden`,
      name: `tool_inventory_id`,
      value: t,
      form: e,
    });
    return n
      ? (0, $.jsxs)($.Fragment, {
          children: [
            o,
            (0, $.jsx)(`input`, {
              type: `hidden`,
              name: `tool_filtering`,
              value: `off`,
              form: e,
            }),
          ],
        })
      : (0, $.jsxs)($.Fragment, {
          children: [
            o,
            (0, $.jsx)(`input`, {
              type: `hidden`,
              name: `tool_filtering`,
              value: `on`,
              form: e,
            }),
            r.map((t) =>
              (0, $.jsx)(
                `input`,
                { type: `hidden`, name: `tool_annotations`, value: t, form: e },
                t,
              ),
            ),
            i.map((t) =>
              (0, $.jsx)(
                `input`,
                {
                  type: `hidden`,
                  name: `tool_annotations_live`,
                  value: t,
                  form: e,
                },
                t,
              ),
            ),
            a.map((t) =>
              (0, $.jsx)(
                `input`,
                { type: `hidden`, name: `tools`, value: t, form: e },
                t,
              ),
            ),
          ],
        });
  }
  var sh = `consent-tools-root`;
  function ch(e) {
    let t = e.dataset.toolsUrl,
      n = e.dataset.state,
      r = e.dataset.csrfToken,
      i = e.dataset.formId,
      a = e.dataset.approveButtonId,
      o = e.dataset.consentEnabled,
      s = e.dataset.serverName;
    return !t ||
      !t.startsWith(`/`) ||
      !n ||
      !r ||
      !i ||
      !a ||
      !s ||
      (o !== `true` && o !== `false`)
      ? null
      : {
          toolsUrl: t,
          state: n,
          csrfToken: r,
          formId: i,
          approveButtonId: a,
          consentEnabled: o === `true`,
          serverName: s,
          prefill: Jm(e.dataset.prefill),
        };
  }
  var lh = document.getElementById(sh);
  if (lh) {
    let e = ch(lh);
    e
      ? (0, Qi.createRoot)(lh).render((0, $.jsx)(rh, { ...e }))
      : (lh.textContent = `Tool access could not be initialized. Reload the page to continue.`);
  }
})();
