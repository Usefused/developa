export function EvidenceTable({headers,rows}) {
  return <table className="evidence-table"><thead><tr>{headers.map(title=><th scope="col" key={title}>{title}</th>)}</tr></thead><tbody>{rows.map((cells,index)=><tr key={index}>{cells.map((cell,column)=><td key={column}>{cell}</td>)}</tr>)}</tbody></table>;
}
