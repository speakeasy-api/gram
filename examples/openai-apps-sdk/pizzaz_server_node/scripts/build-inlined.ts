import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function createInlinedTemplate(widgetName: string, rootElementId: string): string {
  const assetsDir = path.join(__dirname, "../../assets");
  const jsPath = path.join(assetsDir, `${widgetName}-2d2b.js`);
  const cssPath = path.join(assetsDir, `${widgetName}-2d2b.css`);
  
  let jsContent = '';
  let cssContent = '';
  
  if (fs.existsSync(jsPath)) {
    jsContent = fs.readFileSync(jsPath, 'utf8');
  } else {
    console.warn(`⚠️  JS file not found: ${jsPath}`);
  }
  
  if (fs.existsSync(cssPath)) {
    cssContent = fs.readFileSync(cssPath, 'utf8');
  } else {
    console.warn(`⚠️  CSS file not found: ${cssPath}`);
  }
  
  // Escape backticks and ${} in the content to prevent template literal issues
  const escapedJs = jsContent.replace(/\\/g, '\\\\').replace(/`/g, '\\`').replace(/\$\{/g, '\\${');
  const escapedCss = cssContent.replace(/\\/g, '\\\\').replace(/`/g, '\\`').replace(/\$\{/g, '\\${');
  
  return `\`<!doctype html>
<html>
<head>
  <style>
${escapedCss}
  </style>
</head>
<body>
  <div id="${rootElementId}"></div>
  <script type="module">
${escapedJs}
  </script>
</body>
</html>\``;
}

const widgets = [
  { name: 'pizzaz', rootId: 'pizzaz-root' },
];

const templatesCode = widgets.map(widget => 
  `  "${widget.name}": ${createInlinedTemplate(widget.name, widget.rootId)}`
).join(',\n');

const outputContent = `// Auto-generated file - do not edit manually
// Generated on ${new Date().toISOString()}

export const WIDGET_HTML_TEMPLATES: Record<string, string> = {
${templatesCode}
} as const;
`;

const outputPath = path.join(__dirname, "../src/widget-templates.ts");
fs.writeFileSync(outputPath, outputContent, 'utf8');

console.log(`✅ Generated inlined templates at ${outputPath}`);
