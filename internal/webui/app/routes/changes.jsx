import {SnapshotPage} from '../components/activity/snapshot-page.jsx';
import {Changes} from '../components/activity/changes.jsx';
export default function ChangesRoute() { return <SnapshotPage>{details=><Changes details={details}/>}</SnapshotPage>; }
