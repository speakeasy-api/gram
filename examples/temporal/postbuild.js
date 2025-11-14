import { readFileSync, writeFileSync, existsSync } from 'fs';
import { execSync } from 'child_process';

// Modify functions.js
const file = 'dist/functions.js';
const content = readFileSync(file, 'utf8');
const prepend = "import { createRequire } from 'module'; const require = createRequire(import.meta.url);\n";
writeFileSync(file, prepend + content);

// Rezip
execSync('cd dist && zip -r gram.zip functions.js', { stdio: 'inherit' });
