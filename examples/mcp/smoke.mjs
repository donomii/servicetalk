import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';

const client = new Client({ name: 'servicetalk-smoke', version: '1.0.0' });
const endpoint = new URL(process.argv[2] || 'http://127.0.0.1:8000/mcp');
const variable = 'mcpProbe' + randomUUID().replaceAll('-', '');

async function tool(name, args = {}) {
  const result = await client.callTool({ name, arguments: args });
  assert.equal(result.isError, false, JSON.stringify(result));
  assert.equal(result.content[0].type, 'text');
  return result.content[0].text;
}

async function checkStateAndInspection() {
  assert.equal(await tool('eval', { code: variable + ' := Array new. ' + variable + ' add: 42. ' + variable + ' at: 0' }), '42');
  assert.equal(await tool('eval', { code: variable + ' at: 0' }), '42');
  const objects = JSON.parse(await tool('objects'));
  const workspace = objects.find(entry => entry.className === '__workspace__');
  assert.ok(workspace);
  const inspection = JSON.parse(await tool('inspect', { objectId: workspace.id }));
  const objectId = inspection.instanceVars[variable].objid;
  const array = JSON.parse(await tool('inspect', { objectId }));
  assert.ok(array.runtime.pid > 0);
  assert.equal(array.retention.path.at(-1), objectId);
  const size = JSON.parse(await tool('call', { objectId, method: 'size', args: [] }));
  assert.equal(size.num, 1);
  assert.ok(JSON.parse(await tool('metrics')).registrySize >= 3);
}

async function checkErrors() {
  const failure = await client.callTool({ name: 'eval', arguments: { code: "[ 'MCP smoke failure' signal ] value" } });
  assert.equal(failure.isError, true);
  assert.match(failure.content[0].text, /MCP smoke failure/);
  assert.equal(await tool('eval', { code: "[ [ 'handled' signal ] value ] onError: [e | e]" }), 'handled');
  assert.equal(await tool('eval', { code: '6 * 7' }), '42');
}

try {
  await client.connect(new StreamableHTTPClientTransport(endpoint));
  const tools = (await client.listTools()).tools.map(tool => tool.name).sort();
  assert.deepEqual(tools, ['call', 'eval', 'inspect', 'metrics', 'objects']);
  try {
    await checkStateAndInspection();
    await checkErrors();
  } finally {
    await tool('eval', { code: variable + ' := nil' });
  }
  console.log(JSON.stringify({ passed: true, server: client.getServerVersion(), tools }));
} catch (error) {
  console.error(error);
  process.exitCode = 1;
} finally {
  await client.close();
}
