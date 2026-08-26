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
  const content = await readFile(join(source,relative));
  if (process.argv.includes('--check')) {
    if (!(await readFile(target)).equals(content)) throw new Error('Embedded framework assets are stale. Run npm run build.');
  } else {
    await mkdir(dirname(target),{recursive:true});
    await writeFile(target,content);
  }
}
