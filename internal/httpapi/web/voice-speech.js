'use strict';

function atlasSpokenPrompt(prompt) {
  const lines = prompt.trim().split('\n').map(line => line.trim()).filter(Boolean);
  const heading = /^Here are (.+) (tasks|reminders|notes):$/i.exec(lines[0] || '');
  if (lines.length < 2 || !heading) return prompt;
  const titles = lines.slice(1).map(line => /^“([^”]+)”/.exec(line)?.[1]).filter(Boolean);
  if (titles.length !== lines.length - 1) return prompt;
  const qualifier = /\bopen\b/i.test(heading[1]) ? 'open ' : /\bupcoming\b/i.test(heading[1]) ? 'upcoming ' : '';
  const first = titles.slice(0, 3);
  const noun = heading[2].toLowerCase();
  const summary = `I found ${titles.length}${titles.length === 10 ? ' or more' : ''} ${qualifier}${titles.length === 1 ? noun.slice(0, -1) : noun}. `;
  return summary + first.join(', ') + (titles.length > first.length ? `. ${titles.length - first.length} more are shown on screen.` : '.');
}

class AtlasVoiceSpeech {
  constructor(synthesis, Utterance, onChange, onError) {
    this.synthesis = synthesis;
    this.Utterance = Utterance;
    this.onChange = onChange;
    this.onError = onError;
    this.speaking = false;
    this.generation = 0;
  }

  stop() {
    this.generation++;
    this.synthesis?.cancel();
    this.speaking = false;
    this.onChange(false);
  }

  speak(prompt, language) {
    if (!prompt || !this.synthesis || !this.Utterance) return;
    this.stop();
    const generation = this.generation;
    const utterance = new this.Utterance(atlasSpokenPrompt(prompt));
    utterance.lang = language;
    utterance.rate = 0.95;
    utterance.onend = () => this.finish(generation);
    utterance.onerror = event => {
      if (generation !== this.generation) return;
      this.finish(generation);
      if (event.error !== 'interrupted' && event.error !== 'canceled') this.onError(event);
    };
    this.speaking = true;
    this.onChange(true);
    this.synthesis.speak(utterance);
  }

  finish(generation) {
    if (generation !== this.generation) return;
    this.speaking = false;
    this.onChange(false);
  }
}

if (typeof module !== 'undefined') module.exports = {atlasSpokenPrompt, AtlasVoiceSpeech};
