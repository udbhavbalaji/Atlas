'use strict';

const {test} = require('node:test');
const assert = require('node:assert/strict');
const {atlasSpokenPrompt, AtlasVoiceSpeech} = require('../web/voice-speech.js');

test('spoken lists are concise while the original answer stays intact', () => {
  const reminders = 'Here are your open reminders:\n“Pay rent” — tomorrow\n“Call the doctor” — today (due)';
  assert.equal(atlasSpokenPrompt(reminders), 'I found 2 open reminders. Pay rent, Call the doctor.');
  assert.equal(atlasSpokenPrompt('Here are the most recent saved tasks:\n“File taxes” (open)'), 'I found 1 task. File taxes.');
  assert.equal(atlasSpokenPrompt('Here are the most recent saved notes:\n“Trip ideas”'), 'I found 1 note. Trip ideas.');
  assert.equal(atlasSpokenPrompt('Review before saving.'), 'Review before saving.');
  assert.match(reminders, /tomorrow/);
});

test('Stop cancels playback and stale callbacks cannot hide the next Stop control', () => {
  let current;
  let cancellations = 0;
  const states = [];
  const synthesis = {cancel() { cancellations++; }, speak(utterance) { current = utterance; }};
  class Utterance { constructor(text) { this.text = text; } }
  const speech = new AtlasVoiceSpeech(synthesis, Utterance, speaking => states.push(speaking), () => {});
  speech.speak('First answer', 'en-IN');
  const first = current;
  assert.equal(first.text, 'First answer');
  assert.equal(speech.speaking, true);
  speech.stop();
  assert.equal(speech.speaking, false);
  speech.speak('Second answer', 'en-IN');
  first.onend();
  assert.equal(speech.speaking, true);
  current.onend();
  assert.equal(speech.speaking, false);
  assert.equal(cancellations, 3);
  assert.deepEqual(states, [false, true, false, false, true, false]);
});
