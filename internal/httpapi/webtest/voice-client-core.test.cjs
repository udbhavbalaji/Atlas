'use strict';

const {test} = require('node:test');
const assert = require('node:assert/strict');
const {AtlasVoiceSession} = require('../web/voice-client-core.js');

function scriptedFetch(states, calls) {
  return async (path, options) => {
    calls.push({path, method: options.method, body: options.body ? JSON.parse(options.body) : null});
    const state = states.shift();
    return {ok: true, status: 200, json: async () => state};
  };
}

test('voice client routes once, asks, corrects, and requires a separate confirmation action', async () => {
  const calls = [];
  const base = {id: 'session_123', version: 1, state: 'awaiting_answer', prompt: 'Add a reminder?'};
  const preview = {id: 'session_123', version: 2, state: 'awaiting_confirmation', prompt: 'Review the task.', proposal: {input: {kind: 'task', title: 'Call Maya'}}};
  const corrected = {...preview, version: 3, proposal: {input: {kind: 'task', title: 'Call Maya tonight'}}};
  const saved = {...corrected, version: 4, state: 'saved', saved: {task_id: 'task_123'}};
  const answered = {...saved, version: 5, state: 'answered', saved: null, answer: {text: 'Call Maya is saved.'}};
  const client = new AtlasVoiceSession(scriptedFetch([base, preview, corrected, saved, answered], calls));
  await client.start({provider: 'mock', fixture: 'task', text: 'Call Maya', timezone: 'UTC', requestID: 'voice-1'});
  assert.equal(calls[0].path, '/api/v1/conversations/routing');
  assert.equal(calls[0].body.fixture, 'task');
  assert.throws(() => client.confirm(), /Review the pending action/);
  await client.reply('', 'reminder', 'skip');
  await assert.rejects(client.reply('yes'), /Use the confirmation button/);
  assert.equal(calls.length, 2);
  await client.reply('change title to Call Maya tonight');
  await client.confirm();
  assert.equal(calls[3].body.text, 'yes');
  assert.equal(calls[3].body.version, 3);
  assert.equal(client.state.state, 'saved');
  await client.reply('When is my call with Maya?');
  assert.equal(calls[4].body.version, 4);
  assert.equal(client.state.state, 'answered');
});

test('resume uses saved session ID and cancellation never calls confirm', async () => {
  const calls = [];
  const client = new AtlasVoiceSession(scriptedFetch([
    {id: 'session_1', version: 7, state: 'awaiting_route', prompt: 'Which channel?'},
    {id: 'session_1', version: 8, state: 'cancelled', prompt: 'Cancelled.'}
  ], calls));
  await client.resume('session_1');
  await client.cancel();
  assert.equal(calls[0].path, '/api/v1/conversations/routing/session_1');
  assert.equal(calls[1].body.text, 'cancel');
  assert.equal(calls[1].body.version, 7);
});
