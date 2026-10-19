'use strict';

// A report is a local, versioned snapshot. The same request ID is retained
// while a submission is uncertain so retrying cannot create two reports.
const AtlasReport = (() => {
  const dialog = document.createElement('dialog');
  dialog.className = 'report-dialog';
  dialog.setAttribute('aria-labelledby', 'report-title');
  dialog.innerHTML = `
    <form id="report-form">
      <div class="report-heading"><span class="eyebrow">HELP IMPROVE ATLAS</span><button id="report-close" type="button" aria-label="Close report">×</button></div>
      <h2 id="report-title">Report this conversation</h2>
      <p class="report-copy">Tell us what you expected. Atlas saves your note, this conversation, and its route summary in your local database.</p>
      <label>What went wrong?<select id="report-category">
        <option value="wrong_answer">Wrong answer</option><option value="missed_action">Missed part of my request</option>
        <option value="unwanted_change">Unwanted change</option><option value="confusing_flow">Confusing question or review</option>
        <option value="other">Something else</option></select></label>
      <label>Which part?<select id="report-turn"><option value="-1">Whole conversation</option></select></label>
      <label>What should Atlas have done?<textarea id="report-expected" required minlength="3" maxlength="2000" rows="4" placeholder="Describe the outcome you expected"></textarea></label>
      <p id="report-feedback" class="report-feedback" role="status" aria-live="polite"></p>
      <div class="report-actions"><button type="button" id="report-cancel">Cancel</button><button class="primary" id="report-submit" type="submit">Save report</button></div>
    </form>`;
  document.body.append(dialog);
  const form = dialog.querySelector('#report-form');
  const category = dialog.querySelector('#report-category');
  const turn = dialog.querySelector('#report-turn');
  const expected = dialog.querySelector('#report-expected');
  const feedback = dialog.querySelector('#report-feedback');
  const submit = dialog.querySelector('#report-submit');
  let state = null;
  let pending = null;

  function surface() {
    const fromShell = new URLSearchParams(location.search).get('surface');
    return ['macos', 'omarchy'].includes(fromShell) ? fromShell : 'web';
  }
  function close() {
    if (dialog.close) dialog.close();
    else dialog.removeAttribute('open');
  }
  function clearPending() {
    pending = null;
    try { sessionStorage.removeItem('atlas.report.pending.v1'); } catch {}
  }
  function open(conversation, messageIndex = -1) {
    if (!conversation?.id) return;
    state = conversation;
    feedback.textContent = '';
    turn.replaceChildren(new Option('Whole conversation', '-1'));
    (conversation.messages || []).forEach((message, index) => {
      const label = (message.role === 'user' ? 'You: ' : 'Atlas: ') + message.text.slice(0, 90);
      turn.append(new Option(label, String(index)));
    });
    turn.value = String(messageIndex);
    try {
      const saved = JSON.parse(sessionStorage.getItem('atlas.report.pending.v1') || 'null');
      pending = saved?.session_id === conversation.id ? saved : null;
    } catch { pending = null; }
    if (pending) {
      category.value = pending.category;
      turn.value = String(pending.message_index);
      expected.value = pending.expected;
      feedback.textContent = 'An earlier submission may not have returned. Save again to check its result.';
    } else {
      category.value = 'wrong_answer';
      expected.value = '';
    }
    if (dialog.showModal) dialog.showModal();
    else dialog.setAttribute('open', '');
    expected.focus();
  }
  function changed() { clearPending(); feedback.textContent = ''; }
  [category, turn, expected].forEach(field => field.addEventListener('input', changed));
  dialog.querySelector('#report-close').onclick = close;
  dialog.querySelector('#report-cancel').onclick = close;
  form.onsubmit = async event => {
    event.preventDefault();
    if (!state || !form.reportValidity()) return;
    const payload = {
      version: state.version, request_id: pending?.request_id || crypto.randomUUID(),
      surface: surface(), category: category.value, expected: expected.value.trim(),
      message_index: Number(turn.value)
    };
    pending = {...payload, session_id: state.id};
    try { sessionStorage.setItem('atlas.report.pending.v1', JSON.stringify(pending)); } catch {}
    submit.disabled = true;
    feedback.textContent = 'Saving report…';
    try {
      const response = await fetch('/api/v1/conversations/routing/' + encodeURIComponent(state.id) + '/reports', {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(payload)
      });
      const body = await response.json();
      if (!response.ok) {
        if (response.status === 409 && body.error?.code === 'session_version_conflict') {
          const current = await fetch('/api/v1/conversations/routing/' + encodeURIComponent(state.id));
          if (current.ok) state = await current.json();
          clearPending();
          feedback.textContent = 'This conversation changed. Review it and save the report again.';
          return;
        }
        throw new Error(body.error?.message || 'The report could not be saved.');
      }
      clearPending();
      close();
      const notice = document.getElementById('report-notice');
      if (notice) notice.textContent = 'Report saved locally. ID: ' + body.id;
      window.dispatchEvent(new CustomEvent('atlas-report-saved', {detail: body}));
    } catch (error) {
      feedback.textContent = error.message + ' You can retry with the same report.';
    } finally {
      submit.disabled = false;
    }
  };
  return {open};
})();
