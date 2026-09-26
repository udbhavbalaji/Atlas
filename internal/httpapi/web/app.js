'use strict';
let tasks = [], filter = 'open', busy = false;
const $ = id => document.getElementById(id);
function message(text, error = false) { $('message').textContent = text; $('message').className = error ? 'error' : ''; }
async function api(path, method = 'GET', body) {
  const response = await fetch('/api/v1/' + path, {method, headers: {'Content-Type': 'application/json'}, body: body === undefined ? undefined : JSON.stringify(body)});
  if (!response.ok) { let detail; try { detail = (await response.json()).error; } catch {} throw new Error(detail || 'Atlas could not save this change. Try again.'); }
  return response.status === 204 ? null : response.json();
}
function button(label, action, style) { const b = document.createElement('button'); b.textContent = label; b.type = 'button'; if (style) b.className = style; b.onclick = action; b.disabled = busy; return b; }
function render() {
  const host = $('tasks'); host.replaceChildren(); host.setAttribute('aria-busy', 'false');
  const visible = tasks.filter(t => filter === 'all' || t.status === filter);
  if (!visible.length) { const p = document.createElement('p'); p.className = 'empty'; p.textContent = filter === 'completed' ? 'No completed tasks yet.' : filter === 'open' ? 'All clear. Add something when it comes to mind.' : 'Your tasks will appear here.'; host.append(p); }
  for (const t of visible) {
    const row = document.createElement('article'); row.className = 'task' + (t.status === 'completed' ? ' done' : '');
    const title = document.createElement('h2'); title.className = 'title'; title.textContent = t.title;
    const meta = document.createElement('div'); meta.className = 'meta'; meta.textContent = 'Saved ' + new Date(t.created_at).toLocaleString();
    const actions = document.createElement('div'); actions.className = 'actions';
    actions.append(button(t.status === 'open' ? 'Complete' : 'Reopen', () => mutate(() => api('tasks/' + t.id, 'PATCH', {status: t.status === 'open' ? 'completed' : 'open'}), t.status === 'open' ? 'Task completed.' : 'Task reopened.')));
    actions.append(button('Edit', () => edit(row, t)));
    actions.append(button('Delete', () => { if (confirm('Delete “' + t.title + '”? This cannot be undone.')) mutate(() => api('tasks/' + t.id, 'DELETE'), 'Task deleted.'); }, 'danger'));
    row.append(title, meta, actions); host.append(row);
  }
}
function edit(row, t) {
  const form = document.createElement('form'); form.className = 'edit';
  const input = document.createElement('input'); input.value = t.title; input.required = true; input.maxLength = 500; input.setAttribute('aria-label', 'Edit task title');
  const save = document.createElement('button'); save.textContent = 'Save'; save.className = 'primary';
  form.append(input, save, button('Cancel', render)); row.replaceChildren(form); input.focus();
  form.onsubmit = event => { event.preventDefault(); mutate(() => api('tasks/' + t.id, 'PATCH', {title: input.value}), 'Task updated.'); };
}
async function load() {
  const [next, activity] = await Promise.all([api('tasks'), api('activity')]); tasks = next; render(); $('activity').replaceChildren();
  const names = {'task.created':'Task added', 'task.completed':'Task completed', 'task.updated':'Task changed', 'task.deleted':'Task deleted'};
  for (const a of activity) { const li = document.createElement('li'); const task = tasks.find(t => t.id === a.task_id); li.textContent = (names[a.action] || a.action) + (task ? ': ' + task.title : '') + ' · ' + new Date(a.timestamp).toLocaleString(); $('activity').append(li); }
  if (!activity.length) { const li = document.createElement('li'); li.textContent = 'Your activity will appear here.'; $('activity').append(li); }
}
async function mutate(operation, success) {
  if (busy) return; busy = true; document.querySelectorAll('button').forEach(b => b.disabled = true);
  try { await operation(); message(success); try { await load(); } catch { message(success + ' Refresh to reload the list.', true); } }
  catch (error) { message(error.message === 'Failed to fetch' ? 'Cannot reach Atlas. Your change was not confirmed. Check the connection and refresh before trying again.' : error.message, true); }
  finally { busy = false; document.querySelectorAll('button').forEach(b => b.disabled = false); }
}
$('capture').onsubmit = event => { event.preventDefault(); const title = $('title').value; mutate(async () => { await api('tasks', 'POST', {title}); $('title').value = ''; $('title').focus(); }, 'Task saved.'); };
document.querySelectorAll('[data-filter]').forEach(b => b.onclick = () => { filter = b.dataset.filter; document.querySelectorAll('[data-filter]').forEach(x => x.setAttribute('aria-pressed', String(x === b))); render(); });
$('refresh').onclick = () => mutate(async () => { await load(); }, 'Up to date.');
load().catch(() => { $('tasks').setAttribute('aria-busy', 'false'); $('tasks').textContent = 'Tasks could not be loaded.'; message('Cannot reach Atlas. Check the server and press Refresh.', true); });
