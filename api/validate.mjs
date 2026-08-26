import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import SwaggerParser from '@apidevtools/swagger-parser';

const document = JSON.parse(await readFile(new URL('./openapi.json', import.meta.url), 'utf8'));
// Resolve only the checked-in document: validation must never fetch remote references.
await SwaggerParser.validate(structuredClone(document), {resolve: {external: false}});
const ids = new Set();
for (const [path, methods] of Object.entries(document.paths)) {
  for (const operation of Object.values(methods)) checkOperation(path, operation);
}
console.log(`OpenAPI ${document.openapi}: ${ids.size} operations and ${Object.keys(document.components.schemas).length} schemas validated.`);

function checkOperation(path, operation) {
  assert.ok(operation.operationId, `${path}: missing operation ID`);
  assert.ok(!ids.has(operation.operationId), `Duplicate operation ID: ${operation.operationId}`);
  ids.add(operation.operationId);
  const parameters = operation.parameters || [];
  const names = parameters.map(parameter => `${parameter.in}:${parameter.name}`);
  assert.equal(new Set(names).size, names.length, `${path}: duplicate parameters`);
  const placeholders = [...path.matchAll(/\{([^}]+)\}/g)].map(match => match[1]).sort();
  const declared = parameters.filter(parameter => parameter.in === 'path');
  assert.deepEqual(declared.map(parameter => parameter.name).sort(), placeholders, `${path}: path parameters differ`);
  assert.ok(declared.every(parameter => parameter.required), `${path}: optional path parameter`);
}
