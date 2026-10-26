import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, rm, access } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';

const client = new Client({ name: 'servicetalk-git-smoke', version: '1.0.0' });
const endpoint = new URL(process.argv[2] || 'http://127.0.0.1:8000/mcp');
const root = await mkdtemp(join(tmpdir(), 'servicetalk-git-'));
const work = join(root, 'working repository');
const remote = join(root, 'remote.git');
const variable = 'gitProbe' + randomUUID().replaceAll('-', '');
const quote = value => "'" + value.replaceAll("'", "''") + "'";
const array = values => '{' + values.map(quote).join('. ') + '}';

async function evaluate(code, failure = false) {
  const result = await client.callTool({ name: 'eval', arguments: { code } });
  assert.equal(result.isError, failure, JSON.stringify(result));
  return result.content[0].text;
}

const send = source => evaluate(variable + ' ' + source);
const checked = args => send('checked: ' + array(args));

try {
  await mkdir(work);
  await mkdir(remote);
  await client.connect(new StreamableHTTPClientTransport(endpoint));
  await evaluate(variable + ' := Git new. ' + variable + ' directory: ' + quote(remote));
  await checked(['init', '--bare']);
  await send('directory: ' + quote(work));
  await checked(['init', '-b', 'main']);
  await checked(['config', 'user.name', 'ServiceTalk Git Test']);
  await checked(['config', 'user.email', 'git-test@example.invalid']);
  const filename = '-literal;$(touch INJECTED).txt';
  await writeFile(join(work, filename), 'first version\n');
  assert.match(await send('status'), /literal/);
  await send('add: ' + array([filename]));
  assert.match(await send('stagedDiff'), /first version/);
  const message = "Literal 'quote' $(touch INJECTED) `touch INJECTED`\n\nSecond paragraph.";
  await send('commit: ' + quote(message));
  assert.equal((await checked(['log', '-1', '--format=%B'])).trim(), message);
  assert.equal((await send('branch')).trim(), 'main');
  assert.equal(await send('status size'), '0');
  await assert.rejects(access(join(work, 'INJECTED')));
  await checked(['remote', 'add', 'origin', remote]);
  await send("push: 'origin' branch: 'main'");
  const head = (await send('head')).trim();
  assert.match(await checked(['ls-remote', 'origin', 'refs/heads/main']), new RegExp('^' + head));
  await send("fetch: 'origin'");
  await writeFile(join(work, filename), 'second version\n');
  assert.match(await send('diff'), /second version/);
  assert.match(await send('log'), /Literal/);
  assert.match(await send('remotes'), /origin/);
  const failure = await evaluate(variable + ' checked: ' + array(['rev-parse', '--verify', 'nonexistent-ref']), true);
  assert.match(failure, /Git failed in .*exit 128/);
  assert.equal(await send('lastProcess exitCode'), '128');
  await send('run: ' + array(['rev-parse', '--verify', 'nonexistent-ref']));
  assert.equal(await send('lastProcess exitCode'), '128');
  await send('directory: ' + quote(join(root, 'missing')));
  assert.match(await evaluate(variable + ' status', true), /Git failed in .*exit -1/);
  console.log(JSON.stringify({ passed: true, head, checked: 'status, diff, staging, literal arguments, commit, push, fetch, errors' }));
} finally {
  try {
    await evaluate(variable + ' := nil');
  } finally {
    await client.close();
    await rm(root, { recursive: true, force: true });
  }
}
