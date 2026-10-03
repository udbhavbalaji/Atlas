'use strict';
function targetOptions(select,records,label){
 const previous=select.value;select.replaceChildren();const empty=document.createElement('option');empty.value='';empty.textContent=label;select.append(empty);
 for(const record of records){const option=document.createElement('option');option.value=record.id;option.textContent=record.title+' · '+record.status;select.append(option);}
 select.value=previous;if(select.selectedIndex<0)select.value='';
}
function renderNotes(){
 targetOptions($('note-task'),tasks,'No task link');targetOptions($('note-reminder'),reminders,'No reminder link');
 const host=$('notes');host.replaceChildren();
 if(!notes.length){const p=document.createElement('p');p.textContent='No notes yet. Save context here, with optional task and reminder links.';host.append(p);}
 for(const n of notes){
 const row=document.createElement('article');row.className='task';row.id='note-'+n.id;
 if(n.title){const title=document.createElement('h4');title.textContent=n.title;row.append(title);}
 const body=document.createElement('p');body.className='description';body.textContent=n.body;
 const meta=document.createElement('p');meta.className='meta';meta.textContent='Updated '+new Date(n.updated_at).toLocaleString();
 row.append(body,meta);
 for(const link of n.links){const line=document.createElement('div');line.className='actions';const label=document.createElement('span');label.textContent=link.target_type+': '+link.target_title+(link.target_exists?'':' (deleted)');line.append(label,button('Unlink '+link.target_type,()=>mutate(()=>api('notes/'+n.id+'/links/'+link.target_type+'/'+link.target_id,'DELETE'),'Note link removed.')));row.append(line);}
 const actions=document.createElement('div');actions.className='actions';actions.append(button('Edit note',()=>editNote(row,n)),button('Rename note',()=>renameNote(row,n)),button('Delete note',()=>{if(confirm('Permanently delete this note? This cannot be undone. Its linked tasks and reminders remain.'))mutate(()=>api('notes/'+n.id,'DELETE'),'Note deleted.');},'danger'));row.append(actions);
 const linkForm=document.createElement('form');linkForm.className='actions';const kind=document.createElement('select');kind.setAttribute('aria-label','Link target type');for(const value of ['task','reminder']){const option=document.createElement('option');option.value=value;option.textContent=value;kind.append(option);}
 const target=document.createElement('select');target.setAttribute('aria-label','Link target');target.required=true;const update=()=>targetOptions(target,kind.value==='task'?tasks:reminders,'Choose a '+kind.value);kind.onchange=update;update();
 const attach=document.createElement('button');attach.textContent='Add link';linkForm.append(kind,target,attach);linkForm.onsubmit=e=>{e.preventDefault();mutate(()=>api('notes/'+n.id+'/links/'+kind.value+'/'+target.value,'PUT'),'Note linked.');};row.append(linkForm);host.append(row);
 }
}
function renameNote(row,n){
 const form=document.createElement('form');form.className='edit';const label=document.createElement('label');label.textContent='Note title';const input=document.createElement('input');input.required=true;input.maxLength=500;input.value=n.title||'';label.append(input);const save=document.createElement('button');save.className='primary';save.textContent='Save title';form.append(label,save,button('Cancel',()=>renderNotes()));row.replaceChildren(form);input.focus();form.onsubmit=e=>{e.preventDefault();mutate(()=>api('notes/'+n.id,'PATCH',{title:input.value}),'Note title updated.');};
}
function editNote(row,n){
 const form=document.createElement('form');form.className='edit';const label=document.createElement('label');label.textContent='Note body';const input=document.createElement('textarea');input.rows=5;input.maxLength=10000;input.required=true;input.value=n.body;input.setAttribute('aria-label','Edit note body');label.append(input);
 const save=document.createElement('button');save.className='primary';save.textContent='Save note';form.append(label,save,button('Cancel note edit',()=>renderNotes()));row.replaceChildren(form);input.focus();form.onsubmit=e=>{e.preventDefault();mutate(()=>api('notes/'+n.id,'PATCH',{body:input.value}),'Note updated.');};
}
function renderLinkedNotes(host,kind,id){
 const linked=notes.filter(n=>n.links.some(link=>link.target_type===kind&&link.target_id===id));
 const actions=document.createElement('div');actions.className='actions';actions.append(button('Add note',()=>{showFeature('notes');$(kind==='task'?'note-task':'note-reminder').value=id;$('note-section').scrollIntoView({behavior:'smooth',block:'start'});$('note-body').focus();}));host.append(actions);
 if(!linked.length)return;const section=document.createElement('details');const summary=document.createElement('summary');summary.textContent='Linked notes ('+linked.length+')';section.append(summary);
 for(const n of linked){const body=document.createElement('p');body.className='description';body.textContent=n.body;section.append(body,button('View note',()=>{showFeature('notes');$('note-'+n.id)?.scrollIntoView({behavior:'smooth',block:'center'});}));}host.append(section);
}
$('note-form').onsubmit=e=>{e.preventDefault();mutate(async()=>{await api('notes','POST',{body:$('note-body').value,task_id:$('note-task').value,reminder_id:$('note-reminder').value});$('note-body').value='';$('note-task').value='';$('note-reminder').value='';},'Note saved.');};
