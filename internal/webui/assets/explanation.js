export function explanationRequest(target) {
  if (target.type === 'flow') return {flow:target.options,question:'Explain the facts supported by this static call flow. Describe the entrypoints or candidate roots, the paths through the selected evidence, shared dependencies, and recursive relationships. Cite the source, distinguish unresolved gaps, and do not invent execution ordering or feature behavior.'};
  if (target.type === 'feature') return {feature_id:target.id,question:'Explain this inferred feature using its source evidence. Describe what the code supports, its limits, and any unsupported parts of the feature claim.'};
  return {symbol_id:target.id,question:'Explain what this declaration does, its inputs and outputs, important conditions, and side effects visible in the supplied source. Be concise and cite the code.'};
}
