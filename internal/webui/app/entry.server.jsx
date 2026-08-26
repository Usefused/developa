import {PassThrough} from 'node:stream';
import {renderToPipeableStream} from 'react-dom/server';
import {createReadableStreamFromReadable} from '@react-router/node';
import {ServerRouter} from 'react-router';

// SPA mode calls this only at build time to produce the public loading shell.
// No repository data, API credentials, or request-time Node process are involved.
export default function render(request, status, headers, context) {
  return new Promise((resolve,reject)=>{
    const {pipe,abort} = renderToPipeableStream(<ServerRouter context={context} url={request.url} nonce="__DEVELOPA_CSP_NONCE__"/>,{
      onAllReady() {
        clearTimeout(timer);
        const body = new PassThrough();
        headers.set('Content-Type','text/html; charset=utf-8');
        pipe(body);
        resolve(new Response(createReadableStreamFromReadable(body),{status,headers}));
      },
      onShellError:reject,
      onError:reject,
    });
    const timer = setTimeout(abort,6000);
  });
}
