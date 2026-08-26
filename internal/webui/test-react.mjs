import {build} from 'esbuild';
import {mkdtemp,rm} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';

const directory = await mkdtemp('internal/webui/.react-tests-');
try {
  await build({entryPoints:['internal/webui/react.test.jsx'],outfile:`${directory}/tests.mjs`,bundle:true,platform:'node',format:'esm',packages:'external',jsx:'automatic',loader:{'.css':'empty'}});
  execFileSync(process.execPath,['--test',`${directory}/tests.mjs`],{stdio:'inherit'});
} finally { await rm(directory,{recursive:true,force:true}); }
