import {Link} from 'react-router';
import {EmptyState} from '../components/common.jsx';
import {useWorkspace} from '../hooks/use-workspace.jsx';
import {pageURL} from '../lib/routes.js';
export default function NotFoundRoute() {
  const {snapshot,repositoryID} = useWorkspace();
  return <EmptyState title="Page not found"><Link to={pageURL('blocks',snapshot?.id,{repo:repositoryID})}>Return to code blocks</Link></EmptyState>;
}
