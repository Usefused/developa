import {readFile,mkdir,writeFile,readdir,rm} from 'node:fs/promises';
import {dirname,join} from 'node:path';
import {execFileSync} from 'node:child_process';

execFileSync(process.execPath,['node_modules/@react-router/dev/bin.js','build'],{stdio:'inherit'});
const source = 'internal/webui/.build/client';
const destination = 'internal/webui/dist';
const files = await readdir(source,{recursive:true,withFileTypes:true});
if (!process.argv.includes('--check')) await rm(destination,{recursive:true,force:true});
for (const file of files.filter(entry=>entry.isFile())) {
  const parent = file.parentPath ?? file.path;
  const relative = join(parent,file.name).slice(source.length+1);
  const target = join(destination,relative);
  const content = normalizeGenerated(relative,await readFile(join(source,relative)));
  if (process.argv.includes('--check')) {
    if (!(await readFile(target)).equals(content)) throw new Error('Embedded framework assets are stale. Run npm run build.');
  } else {
    await mkdir(dirname(target),{recursive:true});
    await writeFile(target,content);
  }
}

function normalizeGenerated(relative,content) {
  if (!relative.endsWith('.html')) return content;
  // React Router can indent an otherwise empty generated line. Normalizing it
  // keeps release assets reproducible and prevents source whitespace failures.
  return Buffer.from(content.toString().replace(/[ \t]+(?=\r?\n|$)/g,''));
}
