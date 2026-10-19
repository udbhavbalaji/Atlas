const $ = id => document.getElementById(id);
let session = null;
let previousConfirmationVersion = null;
async function post(path, body) {
  const response = await fetch(path, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
  const data = await response.json();
  if (!response.ok) throw new Error(data.error?.message || response.statusText);
  return data;
}
function button(label, handler) {
  const b = document.createElement('button'); b.type = 'button'; b.textContent = label; b.onclick = handler; $('actions').append(b);
}
async function reply(text, field = '', value = '', version = session.version) {
  try { render(await post(`/api/v1/conversations/routing/${encodeURIComponent(session.id)}/reply`, {version, text, field, value})); }
  catch (error) { $('status').textContent = error.message; }
}
async function resume() {
  try { const response = await fetch(`/api/v1/conversations/routing/${encodeURIComponent(session.id)}`); render(await response.json()); }
  catch (error) { $('status').textContent = error.message; }
}
function render(data) {
  session = data; $('status').textContent = data.prompt; $('detail').textContent = JSON.stringify({state: data.state, version: data.version, route: data.route?.selected_tool, batch_edit: data.batch_edit, relation_list: data.relation_list, occurrence: data.occurrence, answer: data.answer, workflow: data.workflow, warnings: data.warnings}, null, 2);
  $('choices').replaceChildren(); $('actions').replaceChildren();
  const candidates = data.batch_edit?.candidates || data.relation_list?.candidates || data.occurrence?.candidates || [];
  if (candidates.length) {
    for (const item of candidates) { const label = document.createElement('label'); label.style.display = 'block'; const input = document.createElement('input'); input.type = data.state === 'awaiting_batch_edit_targets' ? 'checkbox' : 'radio'; input.name = 'target'; input.value = item.id; label.append(input, document.createTextNode(`${item.kind}: ${item.title} (${item.id})`)); $('choices').append(label); }
    button('Review selected record(s)', () => { const ids = [...$('choices').querySelectorAll('input:checked')].map(input => input.value); if (ids.length) reply('', 'targets', ids.join(',')); });
  }
  if (data.state === 'awaiting_workflow_start') button('Start ordered plan', () => reply('start'));
  if (['awaiting_batch_edit_confirmation', 'awaiting_occurrence_confirmation'].includes(data.state)) { previousConfirmationVersion = data.version; button('Confirm reviewed change', () => reply('yes')); }
  if (data.workflow && ['answered','saved'].includes(data.state) && data.workflow.index + 1 < data.workflow.steps.length) button('Continue', () => reply('continue'));
  if (data.state === 'answered' && previousConfirmationVersion !== null) button('Retry last confirmation', () => reply('yes', '', '', previousConfirmationVersion));
  if (data.id) button('Resume', resume);
  if (data.state !== 'cancelled') button('Cancel', () => reply('cancel'));
}
$('start').onsubmit = async event => { event.preventDefault(); previousConfirmationVersion = null; try { render(await post('/api/v1/conversations/routing', {provider: $('provider').value, version: '1', request_id: crypto.randomUUID(), text: $('request').value, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'})); } catch (error) { $('status').textContent = error.message; } };
$('reply').onsubmit = event => { event.preventDefault(); const text = $('reply-text').value; $('reply-text').value = ''; if (text) reply(text); };
