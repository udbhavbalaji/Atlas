'use strict';

const sample = 'Here are your open reminders:\n“Pay rent” — Sat, 24 Oct 2026 at 6:00 PM IST\n“Call the doctor” — Fri, 23 Oct 2026 at 9:00 AM IST (due)';
document.getElementById('speech-sample').textContent = sample;
const checks = [];
function check(name, condition) { checks.push(`${condition ? 'PASS' : 'FAIL'} · ${name}`); }
check('A short reminder summary is spoken', atlasSpokenPrompt(sample).includes('2 open reminders. Pay rent, Call the doctor.'));
check('Tasks and notes also get concise spoken summaries', atlasSpokenPrompt('Here are the most recent saved tasks:\n“File taxes” (open)') === 'I found 1 task. File taxes.' && atlasSpokenPrompt('Here are the most recent saved notes:\n“Trip ideas”') === 'I found 1 note. Trip ideas.');
check('The full answer stays available on screen', document.getElementById('speech-sample').textContent === sample);
const fakeSynthesis = {cancelled: 0, utterance: null, cancel() { this.cancelled++; }, speak(utterance) { this.utterance = utterance; }};
class FakeUtterance { constructor(text) { this.text = text; } }
const states = [];
const fixture = new AtlasVoiceSpeech(fakeSynthesis, FakeUtterance, speaking => states.push(speaking), () => {});
fixture.speak(sample, 'en-IN');
const first = fakeSynthesis.utterance;
check('Playback starts and offers Stop', fixture.speaking && first.text === atlasSpokenPrompt(sample));
fixture.stop();
check('Stop interrupts playback', !fixture.speaking && fakeSynthesis.cancelled >= 2);
fixture.speak(sample, 'en-IN');
first.onend();
check('A cancelled utterance cannot hide a newer Stop control', fixture.speaking);
fixture.stop();
document.getElementById('speech-results').textContent = checks.join('\n');
document.getElementById('speech-results').style.whiteSpace = 'pre-line';

const playback = new AtlasVoiceSpeech(window.speechSynthesis, window.SpeechSynthesisUtterance, speaking => {
  document.getElementById('speech-stop').hidden = !speaking;
  document.getElementById('speech-status').textContent = speaking ? 'Speaking sample. Tap Stop at any time.' : 'Playback stopped.';
}, () => { document.getElementById('speech-status').textContent = 'Speech playback is unavailable in this browser.'; });
document.getElementById('speech-play').onclick = () => playback.speak(sample, 'en-IN');
document.getElementById('speech-stop').onclick = () => playback.stop();
