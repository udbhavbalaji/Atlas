'use strict';
let tasks = [], filter = 'open', busy = false;
const $ = id => document.getElementById(id);
const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
$('timezone').textContent = 'Deadline timezone: ' + zone;
function overdue(t) { return t.status === 'open' && t.due_at && new Date(t.due_at).getTime() < Date.now(); }
function localDeadline(value) {
 if (!value) return '';
 const d = new Date(value); const pad = n => String(n).padStart(2,'0');
 return d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate())+'T'+pad(d.getHours())+':'+pad(d.getMinutes());
}
function deadlineValue(value) {
 if (!value) return '';
 const date = new Date(value);
 if (!Number.isFinite(date.getTime()) || localDeadline(date.toISOString()) !== value) throw new Error('This local deadline does not exist. Choose another time.');
 return date.toISOString();
}
function message(text, error = false) { $('message').textContent = text; $('message').className = error ? 'error' : ''; }
async function api(path, method = 'GET', body) {
  const response = await fetch('/api/v1/' + path, {method, headers: {'Content-Type': 'application/json'}, body: body === undefined ? undefined : JSON.stringify(body)});
  if (!response.ok) { let detail; try { detail = (await response.json()).error; } catch {} throw new Error(detail || 'Atlas could not save this change. Try again.'); }
  return response.status === 204 ? null : response.json();
}
function button(label, action, style) { const b = document.createElement('button'); b.textContent = label; b.type = 'button'; if (style) b.className = style; b.onclick = action; b.disabled = busy; return b; }
function render() {
  const host = $('tasks'); host.replaceChildren(); host.setAttribute('aria-busy', 'false');
  const visible = tasks.filter(t => filter === 'all' || (filter === 'overdue' ? overdue(t) : t.status === filter));
  if (!visible.length) { const p = document.createElement('p'); p.className = 'empty'; p.textContent = filter === 'overdue' ? 'No overdue tasks.' : filter === 'completed' ? 'No completed tasks yet.' : filter === 'open' ? 'All clear. Add something when it comes to mind.' : 'Your tasks will appear here.'; host.append(p); }
  for (const t of visible) {
    const row = document.createElement('article'); row.className = 'task' + (t.status === 'completed' ? ' done' : '');
    const title = document.createElement('h2'); title.className = 'title'; title.textContent = t.title;
    const meta = document.createElement('div'); meta.className = 'meta'; meta.textContent = 'Saved ' + new Date(t.created_at).toLocaleString();
    const actions = document.createElement('div'); actions.className = 'actions';
    actions.append(button(t.status === 'open' ? 'Complete' : 'Reopen', () => mutate(() => api('tasks/' + t.id, 'PATCH', {status: t.status === 'open' ? 'completed' : 'open'}), t.status === 'open' ? 'Task completed.' : 'Task reopened.')));
    actions.append(button('Edit', () => edit(row, t)));
    actions.append(button('Delete', () => { if (confirm('Delete “' + t.title + '”? This cannot be undone.')) mutate(() => api('tasks/' + t.id, 'DELETE'), 'Task deleted.'); }, 'danger'));
    const description = document.createElement('p'); description.className = 'description'; description.textContent = t.details;
 const due = document.createElement('div'); due.className = 'deadline' + (overdue(t) ? ' overdue' : '');
 due.textContent = t.due_at ? (overdue(t) ? 'Overdue · ' : 'Due · ') + new Date(t.due_at).toLocaleString() + ' (' + zone + ')' : 'No deadline';
 row.append(title); if(t.details) row.append(description); row.append(due, meta, actions); host.append(row);
  }
}
function edit(row, t) {
  const form = document.createElement('form'); form.className = 'edit';
  const input = document.createElement('input'); input.value = t.title; input.required = true; input.maxLength = 500; input.setAttribute('aria-label', 'Edit task title');
  const save = document.createElement('button'); save.textContent = 'Save'; save.className = 'primary';
  const details = document.createElement('textarea'); details.rows = 3; details.maxLength = 10000; details.value = t.details || ''; details.setAttribute('aria-label','Edit task details');
 const due = document.createElement('input'); due.type = 'datetime-local'; due.value = localDeadline(t.due_at); due.setAttribute('aria-label','Edit task deadline');
 const detailLabel = document.createElement('label'); detailLabel.textContent = 'Details'; detailLabel.append(details);
 const dueLabel = document.createElement('label'); dueLabel.textContent = 'Deadline (' + zone + ')'; dueLabel.append(due);
 form.append(input, detailLabel, dueLabel, button('Clear deadline', () => {due.value='';}), save, button('Cancel', render)); row.replaceChildren(form); input.focus();
  form.onsubmit = event => { event.preventDefault(); mutate(() => api('tasks/' + t.id, 'PATCH', {title: input.value, details: details.value, due_at: due.value === localDeadline(t.due_at) ? t.due_at : deadlineValue(due.value)}), 'Task updated.'); };
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
$('capture').onsubmit = event => { event.preventDefault(); const title = $('title').value; mutate(async () => { await api('tasks', 'POST', {title, details: $('details').value, due_at: deadlineValue($('deadline').value)}); $('title').value = ''; $('details').value = ''; $('deadline').value = ''; $('title').focus(); }, 'Task saved.'); };
document.querySelectorAll('[data-filter]').forEach(b => b.onclick = () => { filter = b.dataset.filter; document.querySelectorAll('[data-filter]').forEach(x => x.setAttribute('aria-pressed', String(x === b))); render(); });
$('refresh').onclick = () => mutate(async () => { await load(); }, 'Up to date.');
load().catch(() => { $('tasks').setAttribute('aria-busy', 'false'); $('tasks').textContent = 'Tasks could not be loaded.'; message('Cannot reach Atlas. Check the server and press Refresh.', true); });

setInterval(() => {if(!busy && !document.querySelector('.edit')) render();}, 60000);
