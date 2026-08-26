import {Links,Meta,Outlet,Scripts,ScrollRestoration,useRouteError} from 'react-router';
import stylesheet from './styles.css?url';
import icon from '../assets/icon.svg';

export const links = ()=>[{rel:'stylesheet',href:stylesheet},{rel:'icon',href:icon,type:'image/svg+xml'}];

export function Layout({children}) {
  // The embedded document receives a fresh nonce from Go. Hydration must reuse
  // it, so framework bootstrap scripts never require an unsafe-inline policy.
  const nonce = typeof document === 'undefined' ? '__DENVERR_CSP_NONCE__' : document.querySelector('meta[name="csp-nonce"]').content;
  return <html lang="en"><head><meta charSet="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/>
    <meta name="csp-nonce" content={nonce}/><title>Denverr · Code explorer</title><Meta/><Links/></head>
    <body>{children}<ScrollRestoration nonce={nonce}/><Scripts nonce={nonce}/></body></html>;
}

export function HydrateFallback() { return <main className="access-panel" role="status">Opening your workspace…</main>; }
export default function App() { return <Outlet/>; }
export function ErrorBoundary() {
  useRouteError();
  return <main className="access-panel"><h1>This page could not open.</h1><p>Your saved index has not changed.</p><a href="/blocks">Return to code blocks</a></main>;
}
