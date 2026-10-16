'use strict';
const el = id => document.getElementById(id);
let fixtures = [], current = null, enabled = false, busy = false;
const fields = {'task.create': ['title', 'due_at'], 'reminder.create': ['title', 'scheduled_at'], 'note.create': ['body']};
function controls() {
  el('preview').disabled = !enabled || busy;
  el('revise').disabled = !enabled || busy || !current || !el('field').value;
  el('natural').disabled = !enabled || busy || !current || el('mode').value !== 'jev';
}
function option(value, text) { const o = document.createElement('option'); o.value = value; o.textContent = text; return o; }
function selectFields() {
  const a = current?.plan.actions.find(a => a.id === el('action').value);
  el('field').replaceChildren(...(fields[a?.capability] || []).map(f => option(f, f)));
  controls();
}
function inputMode() {
  const mock = el('mode').value === 'fixture';
  el('fixture').disabled = !mock; el('text').readOnly = mock; el('timezone').disabled = mock;
  if (mock) el('text').value = fixtures.find(f => f.name === el('fixture').value)?.text || '';
  current = null; el('summary').textContent = 'Inputs changed. Preview again.';
  el('actions').replaceChildren(); el('issues').replaceChildren(); el('action').replaceChildren(); el('field').replaceChildren(); el('raw').textContent = ''; controls();
}
function render(result) {
  current = result;
  el('summary').textContent = `Revision ${result.plan.revision} · ${result.ready ? 'Validated preview' : 'Needs clarification'} · ${result.model_calls} model calls this request · No records written`;
  el('actions').replaceChildren(...result.plan.actions.map(a => {
    const card = document.createElement('article'), title = document.createElement('h3'), body = document.createElement('pre');
    title.textContent = `${a.id} · ${a.capability}`; body.textContent = JSON.stringify({arguments: a.arguments, depends_on: a.depends_on, confidence: a.confidence}, null, 2); card.append(title, body); return card;
  }));
  el('issues').replaceChildren(...result.issues.map(i => { const li = document.createElement('li'); li.textContent = `${i.action_id || 'Plan'} / ${i.field}: ${i.question}`; return li; }));
  el('action').replaceChildren(...result.plan.actions.map(a => option(a.id, `${a.id} · ${a.capability}`))); selectFields();
  const {preview_token, ...visible} = result; el('raw').textContent = JSON.stringify(visible, null, 2);
}
async function request(path, body) {
  busy = true; controls(); el('status').textContent = 'Evaluating preview…';
  try {
    const response = await fetch(path, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
    const result = await response.json(); if (!response.ok) throw new Error(result.error?.message || 'Request failed.');
    render(result); el('status').textContent = 'Preview updated. No records were written.';
  } catch (err) { el('status').textContent = err.message; }
  finally { busy = false; controls(); }
}
el('preview').onclick = () => request(el('mode').value === 'fixture' ? `/api/v1/planning/fixtures/${encodeURIComponent(el('fixture').value)}` : '/api/v1/planning/preview', el('mode').value === 'fixture' ? {} : {text: el('text').value, timezone: el('timezone').value});
el('revise').onclick = () => request('/api/v1/planning/revise', {preview_token: current.preview_token, action_id: el('action').value, field: el('field').value, value: el('value').value});
el('natural').onclick = () => request('/api/v1/planning/revise', {preview_token: current.preview_token, text: el('correction').value});
el('action').onchange = selectFields; el('mode').onchange = inputMode; el('fixture').onchange = inputMode;
Promise.all([fetch('/api/v1/planning').then(r => r.json()), fetch('/api/v1/planning/fixtures').then(r => r.json())]).then(([config, list]) => {
  enabled = config.enabled; fixtures = list; el('fixture').replaceChildren(...fixtures.map(f => option(f.name, f.name))); el('fixture').value = 'linked'; inputMode();
  el('status').textContent = enabled ? 'Preview mode enabled. Mock fixtures make no model calls.' : 'Disabled. Start Atlas with ATLAS_PLAN_PREVIEW=1, then reload; the existing conversation is available above.';
}).catch(err => { el('status').textContent = `Could not load preview configuration: ${err.message}`; });
