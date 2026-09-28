'use strict';
let routingConversationState=null,routingConversationBusy=false;
const routingConversationStorage='atlas.routing.conversation.v1';
function routingConversationRender(state){
 routingConversationState=state;$('routing-conversation-id').value=state.id;
 try{localStorage.setItem(routingConversationStorage,state.id);}catch{}
 $('routing-conversation-status').textContent=state.state+' · version '+state.version+' · '+state.prompt;
 $('routing-conversation-json').textContent=JSON.stringify(state,null,2);
 const messages=$('routing-conversation-messages');messages.replaceChildren();
 for(const turn of state.messages){const p=document.createElement('p');p.textContent=(turn.role==='user'?'You: ':'Atlas: ')+turn.text;messages.append(p);}
 const preview=$('routing-conversation-preview');preview.replaceChildren();
 if(state.question?.choices)for(const choice of state.question.choices){const b=document.createElement('button');b.type='button';b.textContent=choice.label;b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,field:state.question.field,value:choice.value});preview.append(b);}
 if(state.state==='awaiting_route')for(const channel of ['task','reminder','note']){const b=document.createElement('button');b.type='button';b.textContent=channel;b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:channel});preview.append(b);}
 if(state.proposal){const h=document.createElement('h4');h.textContent=state.saved?'Saved records':'Proposed records · review before saving';preview.append(h);for(const [label,value] of Object.entries(state.proposal.input)){if(!value||label==='preview_id'||label==='before_task_version')continue;const p=document.createElement('p');p.textContent=label+': '+value;preview.append(p);}}
 for(const warning of state.warnings||[]){const p=document.createElement('p');p.className='meta';p.textContent=warning;preview.append(p);}
 if(state.state==='awaiting_confirmation'){for(const reply of ['yes','cancel']){const b=document.createElement('button');b.type='button';b.textContent=reply==='yes'?'Confirm and save':'Cancel';b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:reply});preview.append(b);}}
 $('routing-conversation-reply').hidden=['saved','cancelled','unsupported'].includes(state.state);
}
async function routingConversationCall(path,method,body){
 if(routingConversationBusy)return;routingConversationBusy=true;
 document.querySelectorAll('#routing-conversation-heading ~ form button, #routing-conversation-preview button').forEach(b=>b.disabled=true);
 try{const response=await fetch('/api/v1/conversations/routing'+path,{method,headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const state=await response.json();if(!response.ok)throw new Error(state.error?.message||'Conversation request failed.');routingConversationRender(state);$('routing-conversation-answer').value='';$('routing-conversation-field').value='';$('routing-conversation-value').value='';if(state.saved)await load();}
 catch(error){$('routing-conversation-status').textContent=error.message+' Resume the session before retrying.';}
 finally{routingConversationBusy=false;document.querySelectorAll('#routing-conversation-heading ~ form button, #routing-conversation-preview button').forEach(b=>b.disabled=false);}
}
$('routing-conversation-provider').onchange=()=>{$('routing-conversation-fixture').disabled=$('routing-conversation-provider').value!=='mock';};
$('routing-conversation-start').onsubmit=event=>{event.preventDefault();const provider=$('routing-conversation-provider').value;const body={provider,version:'1',request_id:crypto.randomUUID(),text:$('routing-conversation-text').value,timezone:zone,context_query:$('routing-conversation-query').value};if(provider==='mock')body.fixture=$('routing-conversation-fixture').value;routingConversationCall('','POST',body);};
$('routing-conversation-resume').onsubmit=event=>{event.preventDefault();const id=$('routing-conversation-id').value.trim();if(id)routingConversationCall('/'+encodeURIComponent(id),'GET');};
$('routing-conversation-reply').onsubmit=event=>{event.preventDefault();if(!routingConversationState)return;routingConversationCall('/'+routingConversationState.id+'/reply','POST',{version:routingConversationState.version,text:$('routing-conversation-answer').value,field:$('routing-conversation-field').value,value:$('routing-conversation-value').value});};
try{const id=localStorage.getItem(routingConversationStorage);if(id)routingConversationCall('/'+encodeURIComponent(id),'GET');}catch{}
