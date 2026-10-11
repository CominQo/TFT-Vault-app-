// Bundles cb3d/main.js (+ three.js) into one file the app loads: ../frontend/lib/cb3d.js (global CB3D)
import { build } from 'esbuild';
import fs from 'fs';
fs.mkdirSync('../frontend/lib', { recursive: true });
await build({ entryPoints: ['cb3d/main.js'], bundle: true, minify: true, format: 'iife', globalName: 'CB3D', target: 'chrome100', outfile: '../frontend/lib/cb3d.js', legalComments: 'none', logLevel: 'info' });
