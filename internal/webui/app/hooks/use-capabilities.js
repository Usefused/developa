import {useSession} from './use-session.jsx';
import {useResource} from './use-resource.js';

export function useCapabilities() {
  const {api} = useSession();
  return useResource('capabilities',signal=>api.capabilities(signal));
}
