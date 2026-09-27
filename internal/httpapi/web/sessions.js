'use strict';
let conversationState=null,conversationBusy=false;
const conversationStorage='atlas.capture.session.v1';
function conversationRender(state){
 conversationState=state;$('conversation-id').value=state.id;try{localStorage.setItem(conversationStorage,state.id);}catch{}
 $('conversation-status').textContent=state.state+' · version '+state.version;
 $('conversation-json').textContent=JSON.stringify(state,null,2);
 const host=$('conversation-messages');host.replaceChildren();
 for(const turn of state.messages){const p=document.createElement('p');p.textContent=(turn.role==='user'?'You: ':'Atlas: ')+turn.text;host.append(p);}
 const preview=$('conversation-preview');preview.replaceChildren();
 for(const assumption of state.interpretation.assumptions){const p=document.createElement('p');p.className='meta';p.textContent=assumption;preview.append(p);}
 if(state.state==='awaiting_clarification' && state.interpretation.questions.some(q=>q.field==='kind'))for(const [label,reply] of [['Action / task','task'],['Reminder','reminder'],['Note','note']]){const b=document.createElement('button');b.type='button';b.textContent=label;b.onclick=()=>conversationCall('/'+state.id+'/reply','POST',{version:state.version,text:reply});preview.append(b);}
 if(!['saved','cancelled','confirming'].includes(state.state))for(const task of state.interpretation.candidates){const b=document.createElement('button');b.type='button';b.textContent='Use '+task.title+(task.due_at?' · '+new Date(task.due_at).toLocaleString():'');b.onclick=()=>conversationCall('/'+state.id+'/reply','POST',{version:state.version,text:task.id});preview.append(b);}
 if(state.interpretation.proposal){const h=document.createElement('h4');h.textContent=state.saved?'Saved records':'Proposed records';preview.append(h);const input=state.interpretation.proposal.input;for(const [label,value] of [['Type',input.kind],['Title',input.title],['Note',input.note_body],['Deadline',input.due_at],['Reminder',input.reminder_at],['Timezone',input.timezone],['Repeat',input.repeat],['Before',state.interpretation.reference?.title]]){if(value){const p=document.createElement('p');p.textContent=label+': '+(['Deadline','Reminder'].includes(label)?new Date(value).toLocaleString([],{timeZone:input.timezone||state.interpretation.timezone}):value);preview.append(p);}}}
 if(state.saved){const links=document.createElement('div');links.className='actions';const records=[...(state.saved.task_id?[['tasks',state.saved.task_id]]:[]),...state.saved.reminders.map(r=>['reminders',r.id]),...state.saved.notes.map(n=>['notes',n.id])];for(const [feature,id] of records){const a=document.createElement('a');a.href='#'+feature+'/'+id;a.textContent='Open '+feature.slice(0,-1);a.onclick=async e=>{e.preventDefault();await load();location.hash=feature+'/'+id;};links.append(a);}preview.append(links);}
 $('conversation-reply').hidden=['saved','cancelled'].includes(state.state);
}
async function conversationCall(path,method,body){
 if(conversationBusy)return;conversationBusy=true;document.querySelectorAll('[id^="conversation-"] button').forEach(b=>b.disabled=true);
 try{const r=await fetch('/api/v1/capture/sessions'+path,{method,headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const data=await r.json();if(!r.ok)throw new Error(data.error?.message||'Conversation request failed.');conversationRender(data);$('conversation-answer').value='';}
 catch(e){$('conversation-status').textContent=e.message+' Resume the session to check its latest state before retrying.';}
 finally{conversationBusy=false;document.querySelectorAll('[id^="conversation-"] button').forEach(b=>b.disabled=false);}
}
$('conversation-start').onsubmit=e=>{e.preventDefault();conversationCall('','POST',{text:$('conversation-text').value,timezone:zone});};
$('conversation-resume').onsubmit=e=>{e.preventDefault();const id=$('conversation-id').value.trim();if(id)conversationCall('/'+encodeURIComponent(id),'GET');};
$('conversation-reply').onsubmit=e=>{e.preventDefault();if(conversationState)conversationCall('/'+conversationState.id+'/reply','POST',{version:conversationState.version,text:$('conversation-answer').value});};
try{const id=localStorage.getItem(conversationStorage);if(id)conversationCall('/'+encodeURIComponent(id),'GET');}catch(e){$('conversation-status').textContent='Browser storage unavailable; keep the session ID to resume.';}
