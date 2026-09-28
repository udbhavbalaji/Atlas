'use strict';

// Microphone PCM is sent to the local Atlas service while recording. Only the
// returned text enters the conversation, after the user reviews and submits it.
const atlasDesktopAudio = (() => {
  const AudioContextType = window.AudioContext || window.webkitAudioContext;
  const captureSupported = Boolean(navigator.mediaDevices?.getUserMedia && AudioContextType?.prototype.createScriptProcessor && window.WebSocket);
  let available = false;
  let backend = '';
  let state = 'idle';
  let target = null;
  let original = '';
  let media = null;
  let context = null;
  let source = null;
  let processor = null;
  let socket = null;
  let stopTimer = null;

  function status(message) { voiceElement('mic-status').textContent = message; }

  function updateControls() {
    const label = state === 'recording' ? 'Stop microphone' : state === 'starting' ? 'Connecting…' : state === 'finishing' ? 'Transcribing…' : captureSupported && available ? 'Start microphone' : 'Focus for dictation';
    for (const id of ['listen-first', 'listen-answer']) {
      const button = voiceElement(id);
      button.textContent = label;
      button.disabled = voiceBusy || state === 'starting' || state === 'finishing';
    }
  }

  function cleanupCapture() {
    if (stopTimer) clearTimeout(stopTimer);
    stopTimer = null;
    if (processor) { processor.onaudioprocess = null; processor.disconnect(); }
    source?.disconnect();
    media?.getTracks().forEach(track => track.stop());
    context?.close().catch(() => {});
    processor = source = media = context = null;
  }

  function fail(message) {
    cleanupCapture();
    if (target) target.readOnly = false;
    if (socket && socket.readyState < WebSocket.CLOSING) socket.close();
    socket = null;
    state = 'idle';
    status(message);
    updateControls();
  }

  function applyTranscript(text) {
    if (!target) return;
    target.value = [original.trim(), text.trim()].filter(Boolean).join(' ');
    target.dispatchEvent(new Event('input', {bubbles: true}));
  }

  function handleMessage(event) {
    let message;
    try { message = JSON.parse(event.data); }
    catch { fail('Atlas returned an unreadable audio response.'); return; }
    if (message.type === 'ready') return;
    if (message.type === 'partial') {
      if (message.text) applyTranscript(message.text);
      status('Live transcript is provisional. Keep speaking or stop to review it.');
    } else if (message.type === 'final') {
      applyTranscript(message.text || '');
      cleanupCapture();
      if (target) target.readOnly = false;
      state = 'idle';
      socket?.close();
      socket = null;
      status(message.text ? 'Transcription complete. Review the text, then send it.' : 'No speech was detected. Try again or type your reply.');
      target?.focus();
      updateControls();
    } else if (message.type === 'error') {
      fail(message.text || 'Local audio transcription failed.');
    }
  }

  function waitForOpen(ws) {
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('Local audio connection timed out.')), 5000);
      ws.onopen = () => { clearTimeout(timeout); resolve(); };
      ws.onerror = () => { clearTimeout(timeout); reject(new Error('Could not connect to local audio service.')); };
    });
  }

  async function start(targetID) {
    state = 'starting';
    target = voiceElement(targetID);
    original = target.value;
    updateControls();
    try {
      window.speechSynthesis?.cancel();
      media = await navigator.mediaDevices.getUserMedia({audio: {channelCount: 1, echoCancellation: true, noiseSuppression: true}, video: false});
      context = new AudioContextType();
      await context.resume();
      socket = new WebSocket(`ws://${location.host}/api/v1/audio/stream`);
      socket.binaryType = 'arraybuffer';
      socket.onmessage = handleMessage;
      socket.onclose = () => { if (state !== 'idle') fail('Audio connection closed. You can retry or type your reply.'); };
      await waitForOpen(socket);
      socket.send(JSON.stringify({type: 'start', sample_rate: Math.round(context.sampleRate)}));
      source = context.createMediaStreamSource(media);
      processor = context.createScriptProcessor(4096, 1, 1);
      processor.onaudioprocess = event => {
        event.outputBuffer.getChannelData(0).fill(0);
        if (state !== 'recording' || socket.readyState !== WebSocket.OPEN) return;
        if (socket.bufferedAmount > 1_000_000) { fail('Audio upload is too slow. Please try a shorter turn.'); return; }
        const samples = event.inputBuffer.getChannelData(0);
        const frame = new ArrayBuffer(samples.length * 2);
        const view = new DataView(frame);
        for (let i = 0; i < samples.length; i++) {
          const sample = Math.max(-1, Math.min(1, samples[i]));
          view.setInt16(i * 2, sample < 0 ? sample * 32768 : sample * 32767, true);
        }
        socket.send(frame);
      };
      source.connect(processor);
      processor.connect(context.destination);
      target.readOnly = true;
      state = 'recording';
      stopTimer = setTimeout(stop, 29_000);
      status(`Recording locally with ${backend}. Click Stop microphone when done (30 second limit).`);
      updateControls();
    } catch (error) {
      fail(`Microphone could not start: ${error.message}. You can type instead.`);
    }
  }

  function stop() {
    if (state !== 'recording') return;
    state = 'finishing';
    cleanupCapture();
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({type: 'stop'}));
    else { fail('Audio connection closed before transcription.'); return; }
    status('Finishing local transcription…');
    updateControls();
  }

  function toggle(targetID) {
    if (state === 'recording') { stop(); return; }
    if (state !== 'idle') return;
    if (!captureSupported || !available) {
      voiceElement(targetID).focus();
      status(backend ? 'Local transcription is not configured. Use system dictation or type; see Desktop checks.' : 'Microphone capture is unavailable here. Use system dictation or type.');
      return;
    }
    start(targetID);
  }

  window.addEventListener('pagehide', () => {
    cleanupCapture();
    socket?.close();
  });
  return {toggle, updateControls, setCapabilities(value) {
    available = Boolean(value.local_transcription);
    backend = value.backend || '';
    updateControls();
    if (captureSupported && available) status(`Local microphone streaming is ready (${backend}). Audio is discarded after transcription.`);
  }};
})();

window.atlasDesktopAudio = atlasDesktopAudio;
voiceElement('listen-first').onclick = () => atlasDesktopAudio.toggle('first');
voiceElement('listen-answer').onclick = () => atlasDesktopAudio.toggle('answer');
atlasDesktopAudio.updateControls();
fetch('/api/v1/audio/capabilities').then(response => response.json()).then(value => atlasDesktopAudio.setCapabilities(value)).catch(() => {
  voiceElement('mic-status').textContent = 'Local audio service is unavailable. You can type your reply.';
});
