'use strict';

const voiceStorage = 'atlas.voice.session.v1';
const voiceClient = new AtlasVoiceSession(window.fetch.bind(window), voiceRender);
const VoiceRecognition = window.SpeechRecognition || window.webkitSpeechRecognition;
const VoiceDesktopDictation = !VoiceRecognition && location.pathname === '/desktop';
let voiceBusy = false;
let voiceListening = false;
let voiceRecognizer = null;

function voiceElement(id) { return document.getElementById('voice-' + id); }
function voiceStatus(message) { voiceElement('status').textContent = message; }
function voiceButton(text, action) {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = text;
  button.onclick = action;
  return button;
}

function voiceSpeakPrompt() {
  const state = voiceClient.state;
  if (!state || !voiceElement('speak').checked || !('speechSynthesis' in window) || !window.SpeechSynthesisUtterance) return;
  window.speechSynthesis.cancel();
  const utterance = new SpeechSynthesisUtterance(state.prompt);
  utterance.lang = voiceElement('language').value;
  utterance.rate = 1;
  utterance.onerror = event => {
    if (event.error === 'interrupted' || event.error === 'canceled') return;
    voiceElement('speak').checked = false;
    voiceElement('mic-status').textContent = 'Speech playback is unavailable here; the prompt is shown on screen.';
  };
  window.speechSynthesis.speak(utterance);
}

function voiceRender(state) {
  voiceElement('session-id').value = state.id;
  try { localStorage.setItem(voiceStorage, state.id); } catch {}
  voiceStatus(state.state.replaceAll('_', ' ') + ' · ' + (state.route?.mock ? 'mock route' : 'Jev route') + ' · session version ' + state.version);
  voiceElement('prompt').textContent = state.state === 'answered' ? '' : state.prompt;
  const transcript = voiceElement('transcript');
  transcript.replaceChildren();
  for (const turn of state.messages || []) {
    const item = document.createElement('div');
    item.className = 'voice-turn';
    const role = document.createElement('strong');
    role.textContent = turn.role === 'user' ? 'You' : 'Atlas';
    const line = document.createElement('p');
    line.textContent = turn.text;
    item.append(role, line);
    transcript.append(item);
  }
  const actions = voiceElement('question-actions');
  actions.replaceChildren();
  if (state.state === 'awaiting_route') {
    for (const channel of ['task', 'reminder', 'note', 'lookup']) actions.append(voiceButton(channel === 'lookup' ? 'Find existing records' : channel, () => voiceCall(() => voiceClient.reply(channel))));
  }
  if (state.state === 'awaiting_answer' && state.question?.choices) {
    for (const choice of state.question.choices) {
      actions.append(voiceButton(choice.label, () => voiceCall(() => voiceClient.reply('', state.question.field, choice.value))));
    }
  }
  const proposal = voiceElement('proposal');
  proposal.replaceChildren();
  if (state.proposal) {
    const heading = document.createElement('h3');
    heading.textContent = state.saved ? 'Saved records' : 'Review before saving';
    proposal.append(heading);
    for (const [key, value] of Object.entries(state.proposal.input || {})) {
      if (!value || ['preview_id', 'before_task_version'].includes(key)) continue;
      const line = document.createElement('p');
      line.textContent = key.replaceAll('_', ' ') + ': ' + value;
      proposal.append(line);
    }
    for (const effect of state.proposal.effects || []) {
      const line = document.createElement('p');
      line.className = 'meta';
      line.textContent = 'Will ' + effect.replaceAll('.', ' ');
      proposal.append(line);
    }
  }
  for (const warning of state.warnings || []) {
    const line = document.createElement('p');
    line.className = 'meta';
    line.textContent = warning;
    proposal.append(line);
  }
  if (state.saved) {
    const line = document.createElement('p');
    line.textContent = 'Saved. Task: ' + (state.saved.task_id || 'none') + ' · reminders: ' + (state.saved.reminders?.length || 0) + ' · notes: ' + (state.saved.notes?.length || 0);
    proposal.append(line);
  }
  if (state.answer?.sources?.length) {
    const heading = document.createElement('h3');
    heading.textContent = 'Saved records used';
    proposal.append(heading);
    for (const source of state.answer.sources) {
      if (!source.url?.startsWith('/#')) continue;
      const link = document.createElement('a');
      link.href = source.url;
      link.textContent = `${source.type}: ${source.title}`;
      const line = document.createElement('p');
      line.append(link);
      proposal.append(line);
    }
  }
  const active = !['saved', 'cancelled', 'unsupported'].includes(state.state);
  voiceElement('reply-form').hidden = !active || state.state === 'confirming';
  voiceElement('confirm').hidden = !['awaiting_confirmation', 'confirming'].includes(state.state);
  voiceElement('confirm').textContent = state.state === 'confirming' ? 'Retry confirmation' : 'Confirm and save';
  voiceElement('cancel').hidden = !active || state.state === 'confirming';
  voiceElement('repeat').hidden = !state.prompt || !('speechSynthesis' in window);
  voiceUpdateControls();
}

function voiceUpdateControls() {
  for (const button of document.querySelectorAll('#panel-voice button')) button.disabled = voiceBusy;
  if (!VoiceRecognition && !VoiceDesktopDictation) {
    voiceElement('listen-first').disabled = true;
    voiceElement('listen-answer').disabled = true;
  }
  const listenLabel = VoiceDesktopDictation ? 'Focus for dictation' : voiceListening ? 'Stop listening' : 'Use microphone';
  voiceElement('listen-first').textContent = listenLabel;
  voiceElement('listen-answer').textContent = listenLabel;
  window.atlasDesktopAudio?.updateControls();
}

async function voiceCall(action, speak = true) {
  if (voiceBusy) return;
  voiceBusy = true;
  voiceUpdateControls();
  try {
    const state = await action();
    voiceElement('answer').value = '';
    if (voiceElement('start-feedback')) voiceElement('start-feedback').textContent = '';
    if (voiceElement('reply-feedback')) voiceElement('reply-feedback').textContent = '';
    if (speak) voiceSpeakPrompt();
    if (state.saved && typeof load === 'function') await load();
  } catch (error) {
    const message = error.message + (error.status ? ' Resume the session to check its latest state before retrying.' : '');
    voiceStatus(message);
    if (voiceElement('start-feedback')) voiceElement('start-feedback').textContent = message;
    if (voiceElement('reply-feedback')) voiceElement('reply-feedback').textContent = message;
  } finally {
    voiceBusy = false;
    voiceUpdateControls();
  }
}

function voiceListen(targetID) {
  if (!VoiceRecognition) {
    if (VoiceDesktopDictation) {
      voiceElement(targetID).focus();
      voiceElement('mic-status').textContent = /Mac/.test(navigator.platform)
        ? 'Use your Mac Dictation shortcut, then review the text before sending.'
        : 'On Omarchy, hold F9 or toggle Super+Ctrl+X to dictate. Review the text before sending.';
    }
    return;
  }
  if (voiceListening) { voiceRecognizer?.stop(); return; }
  if (voiceBusy) return;
  window.speechSynthesis?.cancel();
  const recognizer = new VoiceRecognition();
  voiceRecognizer = recognizer;
  recognizer.lang = voiceElement('language').value;
  recognizer.continuous = false;
  recognizer.interimResults = false;
  recognizer.maxAlternatives = 1;
  let pendingAnswer = '';
  recognizer.onstart = () => {
    voiceListening = true;
    voiceElement('mic-status').textContent = 'Listening for one answer…';
    voiceUpdateControls();
  };
  recognizer.onresult = event => {
    const result = event.results[event.resultIndex];
    if (!result?.isFinal) return;
    const transcript = result[0]?.transcript?.trim() || '';
    if (!transcript) return;
    voiceElement(targetID).value = transcript;
    voiceElement('mic-status').textContent = 'Heard: ' + transcript;
    const state = voiceClient.state;
    if (targetID === 'answer' && voiceElement('auto-send').checked && state && !['awaiting_confirmation', 'confirming'].includes(state.state)) {
      pendingAnswer = transcript;
    }
  };
  recognizer.onerror = event => {
    pendingAnswer = '';
    voiceElement('mic-status').textContent = 'Microphone input failed (' + event.error + '). You can type your answer.';
  };
  recognizer.onend = () => {
    voiceListening = false;
    voiceRecognizer = null;
    voiceUpdateControls();
    if (pendingAnswer) voiceCall(() => voiceClient.reply(pendingAnswer));
  };
  try { recognizer.start(); }
  catch { voiceRecognizer = null; voiceElement('mic-status').textContent = 'Microphone input could not start. You can type your answer.'; }
}

let voiceProviderTouched = false;
voiceElement('provider').onchange = () => {
  voiceProviderTouched = true;
  voiceElement('fixture-label').hidden = voiceElement('provider').value !== 'mock';
};
if (location.pathname === '/desktop') {
  fetch('/api/v1/routing').then(response => response.json()).then(result => {
    const jev = result.providers?.find(provider => provider.id === 'jev');
    if (!voiceProviderTouched && jev?.configured) {
      voiceElement('provider').value = 'jev';
      voiceElement('fixture-label').hidden = true;
    } else if (!jev?.configured && voiceElement('start-feedback')) {
      voiceElement('start-feedback').textContent = 'Jev needs an OpenRouter key. Local test mode is available.';
    }
  }).catch(() => {
    if (voiceElement('start-feedback')) voiceElement('start-feedback').textContent = 'Could not check Jev configuration.';
  });
}
voiceElement('start-form').onsubmit = event => {
  event.preventDefault();
  const text = voiceElement('first').value.trim();
  if (!text) return;
  voiceCall(() => voiceClient.start({
    provider: voiceElement('provider').value,
    fixture: voiceElement('fixture').value,
    text,
    timezone: zone,
    contextQuery: voiceElement('context').value.trim(),
    requestID: crypto.randomUUID()
  }));
};
voiceElement('resume-form').onsubmit = event => {
  event.preventDefault();
  voiceCall(() => voiceClient.resume(voiceElement('session-id').value.trim()), false);
};
voiceElement('reply-form').onsubmit = event => {
  event.preventDefault();
  voiceCall(() => voiceClient.reply(voiceElement('answer').value));
};
voiceElement('confirm').onclick = () => voiceCall(() => voiceClient.confirm());
voiceElement('cancel').onclick = () => voiceCall(() => voiceClient.cancel());
voiceElement('repeat').onclick = voiceSpeakPrompt;
voiceElement('listen-first').onclick = () => voiceListen('first');
voiceElement('listen-answer').onclick = () => voiceListen('answer');
if (!VoiceRecognition) voiceElement('mic-status').textContent = VoiceDesktopDictation
  ? (/Mac/.test(navigator.platform) ? 'Use macOS Dictation in a text box, then review before sending.' : 'Omarchy dictation is ready: focus a text box and hold F9, or toggle Super+Ctrl+X.')
  : 'This browser has no speech recognition API. Type a transcript to test the same conversation.';
if (!('speechSynthesis' in window)) voiceElement('speak').disabled = true;
voiceUpdateControls();
try {
  const savedID = localStorage.getItem(voiceStorage);
  if (savedID) voiceCall(() => voiceClient.resume(savedID), false);
} catch {}
