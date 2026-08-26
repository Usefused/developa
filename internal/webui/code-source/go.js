import {parser} from '@lezer/go';
import {highlightCode,tagHighlighter,tags} from '@lezer/highlight';

const highlighter = tagHighlighter([
  {tag:tags.keyword,class:'go-keyword'},
  {tag:tags.comment,class:'go-comment'},
  {tag:[tags.string,tags.character],class:'go-string'},
  {tag:[tags.number,tags.bool,tags.null],class:'go-literal'},
  {tag:tags.typeName,class:'go-type'},
  {tag:tags.function(tags.variableName),class:'go-function'},
  {tag:tags.operator,class:'go-operator'},
]);

export function goSourceLines(source, firstLine = 1) {
  const lines = [{number:firstLine,tokens:[]}];
  // Parse the entire excerpt so raw strings and block comments retain their
  // language context across lines. Never reformat snapshot evidence for display.
  highlightCode(source,parser.parse(source),highlighter,
    (text,className)=>lines.at(-1).tokens.push({text,className}),
    ()=>lines.push({number:firstLine+lines.length,tokens:[]}));
  return lines;
}
