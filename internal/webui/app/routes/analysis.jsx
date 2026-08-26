import {SnapshotPage} from '../components/activity/snapshot-page.jsx';
import {Analysis} from '../components/activity/analysis.jsx';
export default function AnalysisRoute() { return <SnapshotPage>{details=><Analysis details={details}/>}</SnapshotPage>; }
