import {useSession} from '../hooks/use-session.jsx';
import {useWorkspace,usePageNavigation} from '../hooks/use-workspace.jsx';
import {useResource} from '../hooks/use-resource.js';
import {offsetOf} from '../lib/routes.js';
import {FilterBar} from '../components/blocks/filter-bar.jsx';
import {FileCard} from '../components/blocks/file-card.jsx';
import {FileContents} from '../components/blocks/file-contents.jsx';
import {ExplorerFrame} from '../components/explorer-frame.jsx';
import {Resource,Pagination,Button,EmptyState} from '../components/common.jsx';

export default function BlocksRoute() {
  const {api} = useSession();
  const {snapshot} = useWorkspace();
  const {params,update,go,openSymbol} = usePageNavigation();
  const file = params.get('file') || '';
  const filters = {q:params.get('q') || '',kind:params.get('kind') || '',offset:offsetOf(params),limit:file ? 50 : 24};
  const resource = useResource(`blocks:${snapshot.id}:${file}:${JSON.stringify(filters)}`,signal=>loadBlocks(api,snapshot.id,file,filters,signal));
  return <ExplorerFrame><FilterBar query={filters.q} kind={filters.kind} onChange={values=>update({...values,symbol:null},true)}/>
    {file && <div className="breadcrumbs"><Button className="text-button" onClick={()=>go('blocks')}>All blocks</Button> / {file}</div>}
    <Resource state={resource}>{data=><>{file ? <FileContents file={data.file} page={data.page} selected={params.get('symbol')} onOpen={openSymbol}/> : <FileGrid page={data.page} onOpen={path=>go('blocks',{file:path})}/>}
      <Pagination page={data.page} onChange={offset=>update({offset,symbol:null})}/></>}</Resource>
  </ExplorerFrame>;
}

async function loadBlocks(api, snapshot, path, filters, signal) {
  if (!path) return {page:await api.files(snapshot,filters,signal)};
  const [file,page] = await Promise.all([api.file(snapshot,path,signal),api.symbols(snapshot,{...filters,file:path},signal)]);
  return {file,page};
}

function FileGrid({page,onOpen}) {
  if (!page.items.length) return <EmptyState title="No matching blocks">Try another filename, symbol name or kind.</EmptyState>;
  return <><p className="section-description">{page.total} file blocks · Select a block to explore its declarations</p><div className="file-grid">{page.items.map(file=><FileCard key={file.path} file={file} onOpen={onOpen}/>)}</div></>;
}
