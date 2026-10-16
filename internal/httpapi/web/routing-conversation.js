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
 if(state.workflow){const h=document.createElement('h4');h.textContent='Ordered workflow';preview.append(h);const list=document.createElement('ol');for(const [index,step] of state.workflow.steps.entries()){const item=document.createElement('li');item.textContent=step+(index<state.workflow.index?' · complete':index===state.workflow.index?' · current':' · pending');list.append(item);}preview.append(list);}
 if(state.state==='awaiting_workflow_start'){const b=document.createElement('button');b.type='button';b.textContent='Start first step';b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:'start'});preview.append(b);}
 if(state.workflow&&['answered','saved'].includes(state.state)&&state.workflow.index+1<state.workflow.steps.length){const b=document.createElement('button');b.type='button';b.textContent='Continue to next step';b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:'continue'});preview.append(b);}
 if(state.workflow&&state.state==='unsupported'){const b=document.createElement('button');b.type='button';b.textContent='Skip unsupported step';b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:'skip'});preview.append(b);}
 if(state.state==='awaiting_route')for(const channel of ['task','reminder','note','lookup']){const b=document.createElement('button');b.type='button';b.textContent=channel;b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:channel});preview.append(b);}
 if(state.proposal){const h=document.createElement('h4');h.textContent=state.saved?'Saved records':'Proposed records · review before saving';preview.append(h);for(const [label,value] of Object.entries(state.proposal.input)){if(!value||label==='preview_id'||label==='before_task_version')continue;const p=document.createElement('p');p.textContent=label+': '+value;preview.append(p);}}
 if(state.batch){
  const choosing=state.state==='awaiting_batch_targets';
  const h=document.createElement('h4');h.textContent=choosing?'Choose records to delete':'Records proposed for deletion';preview.append(h);
  const list=document.createElement('div');preview.append(list);
  for(const item of choosing?state.batch.candidates:state.batch.targets){
   const label=document.createElement('label');const check=document.createElement('input');check.type='checkbox';check.value=item.id;check.checked=!choosing;check.disabled=!choosing;label.append(check,document.createTextNode(' '+item.kind+': '+item.title));list.append(label,document.createElement('br'));
  }
  if(choosing){const b=document.createElement('button');b.type='button';b.textContent='Review selected records';b.onclick=()=>{const ids=[...list.querySelectorAll('input:checked')].map(x=>x.value);if(ids.length)routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,field:'batch_targets',value:ids.join(',')});};preview.append(b);}
  for(const effect of state.batch.effects||[]){const p=document.createElement('p');p.textContent=effect;preview.append(p);}
 }
 if(state.association){
  const relation=state.association,choosing=state.state==='awaiting_association_targets';
  const h=document.createElement('h4');h.textContent='Relationship · '+relation.operation;preview.append(h);
  if(choosing){
   const selects=[];
   for(const [label,items] of [['First record',relation.from_candidates||[]],['Second record',relation.to_candidates||[]]]){
    const wrapper=document.createElement('label');wrapper.textContent=label+' ';
    const select=document.createElement('select');for(const item of items){const option=document.createElement('option');option.value=item.id;option.textContent=item.kind+': '+item.title;select.append(option);}wrapper.append(select);preview.append(wrapper,document.createElement('br'));selects.push(select);
   }
   const b=document.createElement('button');b.type='button';b.textContent='Review relationship';b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,field:'association_targets',value:selects.map(x=>x.value).join(',')});preview.append(b);
  }else if(relation.change){const p=document.createElement('p');p.textContent=(relation.attach?'Add':'Remove')+' '+relation.operation+': '+relation.change.from.kind+' “'+relation.change.from.title+'” → '+relation.change.to.kind+' “'+relation.change.to.title+'”';preview.append(p);}
 }
 if(state.answer?.sources?.length){const h=document.createElement('h4');h.textContent='Saved records used';preview.append(h);for(const source of state.answer.sources){if(!source.url?.startsWith('/#'))continue;const link=document.createElement('a');link.href=source.url;link.textContent=source.type+': '+source.title;preview.append(link);}}
 for(const warning of state.warnings||[]){const p=document.createElement('p');p.className='meta';p.textContent=warning;preview.append(p);}
 if(state.state==='awaiting_confirmation'){for(const reply of ['yes','cancel']){const b=document.createElement('button');b.type='button';b.textContent=reply==='yes'?'Confirm and save':'Cancel';b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:reply});preview.append(b);}}
 if(state.state==='awaiting_delete_confirmation'){for(const reply of ['yes','cancel']){const b=document.createElement('button');b.type='button';b.textContent=reply==='yes'?'Confirm deletion':'Cancel';b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:reply});preview.append(b);}}
 if(state.state==='awaiting_association_confirmation'){for(const reply of ['yes','cancel']){const b=document.createElement('button');b.type='button';b.textContent=reply==='yes'?'Apply relationship':'Cancel';b.onclick=()=>routingConversationCall('/'+state.id+'/reply','POST',{version:state.version,text:reply});preview.append(b);}}
 $('routing-conversation-reply').hidden=state.state==='cancelled';
}
async function routingConversationCall(path,method,body){
 if(routingConversationBusy)return;routingConversationBusy=true;
 document.querySelectorAll('#routing-conversation-heading ~ form button, #routing-conversation-preview button').forEach(b=>b.disabled=true);
 try{const response=await fetch('/api/v1/conversations/routing'+path,{method,headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const state=await response.json();if(!response.ok)throw new Error(state.error?.message||'Conversation request failed.');routingConversationRender(state);$('routing-conversation-answer').value='';$('routing-conversation-field').value='';$('routing-conversation-value').value='';if(state.saved||state.state==='answered'&&(state.batch||state.association||state.mutation?.action==='delete'))await load();}
 catch(error){$('routing-conversation-status').textContent=error.message+' Resume the session before retrying.';}
 finally{routingConversationBusy=false;document.querySelectorAll('#routing-conversation-heading ~ form button, #routing-conversation-preview button').forEach(b=>b.disabled=false);}
}
$('routing-conversation-provider').onchange=()=>{$('routing-conversation-fixture').disabled=$('routing-conversation-provider').value!=='mock';};
$('routing-conversation-start').onsubmit=event=>{event.preventDefault();const provider=$('routing-conversation-provider').value;const body={provider,version:'1',request_id:crypto.randomUUID(),text:$('routing-conversation-text').value,timezone:zone,context_query:$('routing-conversation-query').value};if(provider==='mock')body.fixture=$('routing-conversation-fixture').value;routingConversationCall('','POST',body);};
$('routing-conversation-resume').onsubmit=event=>{event.preventDefault();const id=$('routing-conversation-id').value.trim();if(id)routingConversationCall('/'+encodeURIComponent(id),'GET');};
$('routing-conversation-reply').onsubmit=event=>{event.preventDefault();if(!routingConversationState)return;routingConversationCall('/'+routingConversationState.id+'/reply','POST',{version:routingConversationState.version,text:$('routing-conversation-answer').value,field:$('routing-conversation-field').value,value:$('routing-conversation-value').value});};
try{const id=localStorage.getItem(routingConversationStorage);if(id)routingConversationCall('/'+encodeURIComponent(id),'GET');}catch{}
