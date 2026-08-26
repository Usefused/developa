import type {Config} from '@react-router/dev/config';

// Go remains the only production server; route modules hydrate in the browser.
export default {
  appDirectory: 'internal/webui/app',
  buildDirectory: 'internal/webui/.build',
  ssr: false,
} satisfies Config;
