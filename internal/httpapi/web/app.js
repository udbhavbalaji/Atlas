'use strict';
let taskDependencies=[],notes=[], reminders = [], tasks = [], filter = 'open', busy = false;
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
const pendingCreationKeys = new Map();
async function api(path, method = 'GET', body) {
 const signature = path+' '+JSON.stringify(body);
 const creation = method === 'POST' && (path === 'tasks' || path === 'reminders' || path==='notes');
 const headers = {'Content-Type':'application/json'};
 if(creation){if(!pendingCreationKeys.has(signature))pendingCreationKeys.set(signature,crypto.randomUUID());headers['Idempotency-Key']=pendingCreationKeys.get(signature);}
  const response = await fetch('/api/v1/' + path, {method, headers, body: body === undefined ? undefined : JSON.stringify(body)});
  if (!response.ok) { let detail; try { const error = (await response.json()).error; detail = typeof error === 'string' ? error : error.message; } catch {} throw new Error(detail || 'Atlas could not save this change. Try again.'); }
  const value = response.status === 204 ? null : await response.json();
 if(creation)pendingCreationKeys.delete(signature);
 if(method!=='GET' && $('api-response'))$('api-response').textContent=JSON.stringify({status:response.status,replayed:response.headers.get('Idempotency-Replayed'),body:value},null,2);
 return value;
}
function button(label, action, style) { const b = document.createElement('button'); b.textContent = label; b.type = 'button'; if (style) b.className = style; b.onclick = action; b.disabled = busy; return b; }
function render() {
  const host = $('tasks'); host.replaceChildren(); host.setAttribute('aria-busy', 'false');
  const visible = tasks.filter(t => filter === 'all' || (filter === 'overdue' ? overdue(t) : t.status === filter));
  if (!visible.length) { const p = document.createElement('p'); p.className = 'empty'; p.textContent = filter === 'overdue' ? 'No overdue tasks.' : filter === 'completed' ? 'No completed tasks yet.' : filter === 'open' ? 'All clear. Add something when it comes to mind.' : 'Your tasks will appear here.'; host.append(p); }
  for (const t of visible) {
    const row = document.createElement('article'); row.id='task-'+t.id; row.className = 'task' + (t.status === 'completed' ? ' done' : '');
    const title = document.createElement('h2'); title.className = 'title'; title.textContent = t.title;
    const meta = document.createElement('div'); meta.className = 'meta'; meta.textContent = 'Saved ' + new Date(t.created_at).toLocaleString();
    const actions = document.createElement('div'); actions.className = 'actions';
    actions.append(button(t.status === 'open' ? 'Complete' : 'Reopen', () => mutate(() => api('tasks/' + t.id, 'PATCH', {status: t.status === 'open' ? 'completed' : 'open'}), t.status === 'open' ? 'Task completed. Linked reminders cancelled.' : 'Task reopened. Add new reminders as needed.')));
    actions.append(button('Edit', () => edit(row, t)));
 if(t.status==='open') actions.append(button('Add reminder',()=>taskReminderForm(row,t)));
    actions.append(button('Delete', () => { if (confirm('Delete “' + t.title + '”? This cannot be undone. Linked reminders will be cancelled.')) mutate(() => api('tasks/' + t.id, 'DELETE'), 'Task deleted. Linked reminders cancelled.'); }, 'danger'));
    const description = document.createElement('p'); description.className = 'description'; description.textContent = t.details;
 const due = document.createElement('div'); due.className = 'deadline' + (overdue(t) ? ' overdue' : '');
 due.textContent = t.due_at ? (overdue(t) ? 'Overdue · ' : 'Due · ') + new Date(t.due_at).toLocaleString() + ' (' + zone + ')' : 'No deadline';
 row.append(title); if(t.details) row.append(description); row.append(due, meta, actions);
 const linked=reminders.filter(r=>r.task_id===t.id);
 if(linked.length){const section=document.createElement('details');section.className='linked-reminders';const summary=document.createElement('summary');summary.textContent='Linked reminders ('+linked.length+')';section.append(summary);
 for(const r of linked){const info=document.createElement('p');info.textContent=r.title+' · '+reminderStatus(r)+' · '+new Date(r.scheduled_at).toLocaleString();if(r.repeat)info.textContent+=' · '+repeatDescription(r);section.append(info);if(r.repeat && r.status==='due')section.append(occurrenceButton(r,'complete'),occurrenceButton(r,'dismiss'));if(r.status!=='completed'){const controls=document.createElement('div');controls.className='actions';controls.append(button('Snooze linked reminder 5 min',()=>mutate(()=>api('reminders/'+r.id+'/snooze','POST',{scheduled_at:new Date(Date.now()+300000).toISOString()}),'Linked reminder snoozed.')),button(r.repeat?'Stop linked repeat':'Complete linked reminder',()=>mutate(()=>api('reminders/'+r.id+'/complete','POST'),'Reminder completed. Task remains open.')));section.append(controls);}if(t.status==='open')section.append(completeTaskToo(t));}
 row.append(section);}
 if(typeof renderTaskDependencies==='function')renderTaskDependencies(row,t);
 if(typeof renderLinkedNotes==='function')renderLinkedNotes(row,'task',t.id);
 host.append(row);
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
async function load(preserveEdits = false) {
  const [next, activity, nextReminders, deliveries,nextNotes,nextDependencies] = await Promise.all([api('tasks'), api('activity'), api('reminders'), api('deliveries'),api('notes'),api('task-dependencies')]); if(preserveEdits && (busy || document.querySelector('.task .edit')))return; reminders=nextReminders; tasks = next; notes=nextNotes;taskDependencies=nextDependencies;if(typeof updateCaptureContexts==='function')updateCaptureContexts(); if(typeof renderNotes==='function')renderNotes();renderReminders(deliveries); render(); $('activity').replaceChildren();
  const names = {'note.created':'Note added','note.updated':'Note changed','note.deleted':'Note deleted','note.linked':'Note linked','note.unlinked':'Note unlinked','task.created':'Task added', 'task.completed':'Task completed', 'task.updated':'Task changed', 'task.deleted':'Task deleted', 'reminder.task_linked':'Reminder linked to task','reminder.task_unlinked':'Reminder task link removed','reminder.scheduled':'Reminder scheduled', 'reminder.delivered':'Reminder delivered to inbox', 'reminder.snoozed':'Reminder snoozed', 'reminder.dismissed':'Reminder dismissed', 'reminder.occurrence.complete':'Repeat occurrence completed', 'reminder.occurrence.dismiss':'Repeat occurrence dismissed', 'reminder.completed':'Reminder completed', 'reminder.cancelled.task.completed':'Reminder cancelled because task completed', 'reminder.cancelled.task.deleted':'Reminder cancelled because task deleted'};
  for (const a of activity) { const li = document.createElement('li'); const task = a.note_id ? notes.find(n=>n.id===a.note_id) : a.reminder_id ? reminders.find(r => r.id === a.reminder_id) : tasks.find(t => t.id === a.task_id); li.textContent = (names[a.action] || a.action) + (task ? ': ' + (task.title||'Note '+task.id.slice(0,8)) : '') + ' · ' + new Date(a.timestamp).toLocaleString(); $('activity').append(li); }
  if(typeof revealURLRecord==='function')revealURLRecord();
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

setInterval(() => {if(!busy && !document.querySelector('.task .edit')) render();}, 60000);

$('reminder-zone').textContent = 'Reminder timezone: ' + zone;
function renderReminders(deliveries) {
 updateReminderBadge(reminders);
 const selector=$('reminder-task');const selected=selector.value;fillReminderTaskChoices(selector);selector.value=selected;if(selector.selectedIndex<0)selector.value='';
 for(const id of ['reminder-inbox','reminder-upcoming','reminder-history','delivery-history']) $(id).replaceChildren();
 $('inbox-heading').textContent = 'Reminder inbox (' + reminders.filter(r => r.status === 'due').length + ')';
 for(const r of reminders) {
 const row=document.createElement('article');row.id='reminder-'+r.id;row.className='task'+(r.status==='due'?' reminder-due':'');
 const title=document.createElement('h2');title.className='title';title.textContent=r.title;
 const when=document.createElement('p');when.className='meta';when.textContent=reminderStatus(r)+' · '+new Date(r.scheduled_at).toLocaleString()+' ('+zone+')';
 const actions=document.createElement('div');actions.className='actions';
 if(r.status!=='completed') {
 actions.append(button(r.task_id?'Change task link':'Link to task',()=>reminderTaskForm(row,r)));
 if(r.status==='due' && !r.repeat) actions.append(button('Dismiss',()=>mutate(()=>api('reminders/'+r.id+'/dismiss','POST'),'Reminder dismissed.')));
 for(const minutes of [5,15,60]) actions.append(button('Snooze '+minutes+' min',()=>mutate(()=>api('reminders/'+r.id+'/snooze','POST',{scheduled_at:new Date(Date.now()+minutes*60000).toISOString()}),'Reminder snoozed.')));
 actions.append(button('Choose time',()=>rescheduleReminder(row,r)));
 if(r.repeat && r.status==='due')actions.append(occurrenceButton(r,'complete'),occurrenceButton(r,'dismiss'));
 actions.append(button(r.repeat?'Stop repeating':'Complete reminder',()=>mutate(()=>api('reminders/'+r.id+'/complete','POST'),r.repeat?'Repeating reminder stopped.':r.task_id?'Reminder completed. Linked task stays open.':'Reminder completed.')));
 }
 row.append(title,when);
 if(r.repeat){const repeat=document.createElement('p');repeat.className='meta';repeat.textContent=repeatDescription(r);row.append(repeat);}
 if(r.task_id){const task=tasks.find(t=>t.id===r.task_id);const info=document.createElement('p');info.className='meta';info.textContent='Linked task: '+(task?task.title:r.task_title+' (deleted)');row.append(info);if(task){if(task.status==='open')actions.append(completeTaskToo(task));row.append(button('View task',()=>{showFeature('tasks');filter='all';document.querySelectorAll('[data-filter]').forEach(x=>x.setAttribute('aria-pressed',String(x.dataset.filter==='all')));render();document.getElementById('task-'+task.id)?.scrollIntoView({behavior:'smooth',block:'center'});}));}}
 row.append(actions);if(typeof renderLinkedNotes==='function')renderLinkedNotes(row,'reminder',r.id);
 $(r.status==='due'?'reminder-inbox':r.status==='scheduled'?'reminder-upcoming':'reminder-history').append(row);
 }
 for(const [id,text] of [['reminder-inbox','No reminders due.'],['reminder-upcoming','No upcoming reminders.'],['reminder-history','No dismissed or completed reminders.']]) if(!$(id).children.length){const p=document.createElement('p');p.textContent=text;$(id).append(p);}
 for(const d of deliveries){const li=document.createElement('li');const r=reminders.find(r=>r.id===d.reminder_id);li.textContent=(r?r.title:d.reminder_id)+' · '+d.state+' · scheduled '+new Date(d.scheduled_at).toLocaleString()+(d.delivered_at?' · delivered '+new Date(d.delivered_at).toLocaleString():'');$('delivery-history').append(li);}
 if(!deliveries.length){const li=document.createElement('li');li.textContent='No deliveries yet.';$('delivery-history').append(li);}
 $('reminder-connection').textContent='Inbox updated '+new Date().toLocaleTimeString()+'. Checks every 2 seconds.';
}
function rescheduleReminder(row,r) {
 const form=document.createElement('form');form.className='edit';
 const label=document.createElement('label');label.textContent='New reminder time ('+zone+')';const input=document.createElement('input');input.type='datetime-local';input.required=true;input.setAttribute('aria-label','New reminder time');label.append(input);
 const save=document.createElement('button');save.className='primary';save.textContent='Reschedule';
 form.append(label,save,button('Cancel',()=>load().catch(()=>message('Could not reload reminders.',true))));row.replaceChildren(form);input.focus();
 form.onsubmit=e=>{e.preventDefault();mutate(()=>api('reminders/'+r.id+'/snooze','POST',{scheduled_at:deadlineValue(input.value)}),'Reminder rescheduled.');};
}
function scheduleReminder(test) {
 const title=$('reminder-title').value.trim();if(!title){message('Enter a reminder title.',true);$('reminder-title').reportValidity();return;}
 mutate(async()=>{const at=test?new Date(Date.now()+5000).toISOString():deadlineValue($('reminder-time').value);await api('reminders','POST',{title,scheduled_at:at,timezone:zone,repeat:$('reminder-repeat').value,task_id:$('reminder-task').value});$('reminder-title').value='';$('reminder-time').value='';$('reminder-task').value='';},test?'Reminder scheduled. Watch the inbox in 5 seconds.':'Reminder scheduled.');
}
$('reminder-form').onsubmit=e=>{e.preventDefault();scheduleReminder(false);};
$('test-reminder').onclick=()=>scheduleReminder(true);
let polling=false;
setInterval(async()=>{
 if(busy||polling||document.querySelector('.task .edit'))return;
 polling=true;
 try{const [next,deliveries]=await Promise.all([api('reminders'),api('deliveries')]);if(!busy&&!document.querySelector('.task .edit')){if(JSON.stringify(next)!==JSON.stringify(reminders)){await load(true);}else{$('reminder-connection').textContent='Inbox checked '+new Date().toLocaleTimeString()+'. Checks every 2 seconds.';}}}
 catch{$('reminder-connection').textContent='Cannot reach Atlas. Inbox may be out of date; retrying automatically.';}
 finally{polling=false;}
},2000);

function reminderStatus(r){return r.cancellation_reason ? 'Cancelled because task '+(r.cancellation_reason==='task.deleted'?'was deleted':'was completed') : r.status;}
function taskReminderForm(row,t){
 const form=document.createElement('form');form.className='edit';
 const titleLabel=document.createElement('label');titleLabel.textContent='Reminder title';const title=document.createElement('input');title.value=t.title;title.required=true;title.maxLength=500;title.setAttribute('aria-label','Task reminder title');titleLabel.append(title);
 const timeLabel=document.createElement('label');timeLabel.textContent='Reminder time ('+zone+')';const time=document.createElement('input');time.type='datetime-local';time.required=true;time.setAttribute('aria-label','Task reminder time');timeLabel.append(time);
 const explanation=document.createElement('p');explanation.className='meta';explanation.textContent='This reminder is linked to the task. Its time is independent of the deadline. Completing or deleting the task cancels it.';
 const repeatLabel=document.createElement('label');repeatLabel.textContent='Repeat';const repeat=document.createElement('select');for(const [value,label] of [['','Once'],['daily','Daily'],['weekly','Weekly']]){const option=document.createElement('option');option.value=value;option.textContent=label;repeat.append(option);}repeatLabel.append(repeat);
 const save=document.createElement('button');save.className='primary';save.textContent='Schedule linked reminder';
 const schedule=test=>mutate(async()=>{await api('reminders','POST',{title:title.value,task_id:t.id,scheduled_at:test?new Date(Date.now()+5000).toISOString():deadlineValue(time.value),timezone:zone,repeat:repeat.value});},test?'Linked reminder scheduled. Watch the inbox in 5 seconds.':'Linked reminder scheduled.');
 form.append(titleLabel,timeLabel,repeatLabel,explanation,save,button('Test linked reminder in 5 seconds',()=>{if(title.reportValidity())schedule(true);}),button('Cancel',render));row.replaceChildren(form);title.focus();
 form.onsubmit=e=>{e.preventDefault();schedule(false);};
}

function completeTaskToo(task){return button('Complete task too',()=>mutate(()=>api('tasks/'+task.id+'/complete','POST'),'Task completed. Its active reminders were cancelled.'));}

function occurrenceButton(r,action){return button(action==='complete'?'Complete occurrence':'Dismiss occurrence',()=>mutate(()=>api('reminders/'+r.id+'/occurrences/'+r.occurrence_id+'/acknowledge','POST',{action}),'Occurrence acknowledged. Next repeat scheduled.'));}
function repeatDescription(r){const time=new Date(r.repeat_anchor);return (r.status==='completed'?'Stopped '+r.repeat+' repeat':'Repeats '+r.repeat)+(r.repeat==='weekly'?' on '+time.toLocaleDateString([],{weekday:'long',timeZone:r.timezone}):'')+' at '+time.toLocaleTimeString([],{timeZone:r.timezone})+' ('+r.timezone+').'+(r.status==='completed'?'':' Snooze changes only this occurrence.');}

function fillReminderTaskChoices(select){select.replaceChildren();const none=document.createElement('option');none.value='';none.textContent='Standalone reminder';select.append(none);for(const t of tasks.filter(t=>t.status==='open')){const option=document.createElement('option');option.value=t.id;option.textContent=t.title;select.append(option);}}
function reminderTaskForm(row,r){const form=document.createElement('form');form.className='edit';const label=document.createElement('label');label.textContent='Task for this reminder';const select=document.createElement('select');select.setAttribute('aria-label','Task for this reminder');fillReminderTaskChoices(select);select.value=r.task_id;label.append(select);const save=document.createElement('button');save.textContent='Save task link';save.className='primary';const explanation=document.createElement('p');explanation.textContent='Completing or deleting the linked task cancels this reminder. Choose Standalone reminder to remove the link. Notes are not required.';form.append(label,explanation,save,button('Cancel task link',()=>load().catch(()=>message('Could not reload reminders.',true))));row.replaceChildren(form);form.onsubmit=e=>{e.preventDefault();mutate(()=>api('reminders/'+r.id,'PATCH',{task_id:select.value}),'Reminder task link updated.');};}
