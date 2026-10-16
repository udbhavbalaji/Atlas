'use strict';
let recordRelations=[];
function relationRecords(){return [...tasks.map(t=>({type:'task',id:t.id,title:t.title})),...reminders.map(r=>({type:'reminder',id:r.id,title:r.title})),...notes.map(n=>({type:'note',id:n.id,title:n.title||n.body.slice(0,100)}))];}
function relationPath(r){return 'relations/'+r.from.type+'/'+r.from.id+'/'+r.to.type+'/'+r.to.id;}
function renderRelations(){
 const records=relationRecords();
 for(const [id,placeholder] of [['relation-from','Choose first record'],['relation-to','Choose second record'],['relation-filter','All records']]){
  const select=$(id),value=select.value,old=select.selectedOptions[0]?.textContent;select.replaceChildren();const none=document.createElement('option');none.value='';none.textContent=placeholder;select.append(none);
  const choices=[...records];if(id==='relation-filter'){for(const relation of recordRelations){for(const endpoint of [relation.from,relation.to]){if(!choices.some(r=>r.type===endpoint.type&&r.id===endpoint.id))choices.push(endpoint);}}}
  for(const record of choices){const o=document.createElement('option');o.value=record.type+':'+record.id;o.textContent=record.type+' · '+record.title+' · '+record.id.slice(0,8)+(record.exists===false?' · missing':'');select.append(o);}
  if(value&&!Array.from(select.options).some(o=>o.value===value)){const o=document.createElement('option');o.value=value;o.textContent=(old||value)+' · unavailable';select.append(o);}select.value=value;
 }
 const filter=$('relation-filter').value;const visible=recordRelations.filter(r=>!filter||r.from.type+':'+r.from.id===filter||r.to.type+':'+r.to.id===filter);
 $('relations-list').replaceChildren();$('relations-snapshot').textContent=JSON.stringify(visible,null,2);
 if(!visible.length){const p=document.createElement('p');p.className='empty';p.textContent='No associations here yet. Link two records above.';$('relations-list').append(p);}
 for(const r of visible){const row=document.createElement('article');row.className='task';const heading=document.createElement('h3');heading.textContent='Related records';row.append(heading);
  for(const record of [r.from,r.to]){const p=document.createElement('p');p.textContent=record.type+' · '+record.title+' · '+record.status;row.append(p);if(record.exists){const a=document.createElement('a');a.className='record-link';a.textContent='Open '+record.type;a.href='/#'+({task:'tasks',reminder:'reminders',note:'notes'}[record.type])+'/'+record.id;row.append(a);}}
  const meta=document.createElement('p');meta.className='meta';meta.textContent='Linked '+new Date(r.created_at).toLocaleString();row.append(meta);row.append(button('Remove association',()=>mutate(async()=>{$('relation-action').textContent=JSON.stringify(await api(relationPath(r),'DELETE'),null,2);},'Association removed.')));$('relations-list').append(row);
 }
}
$('relation-filter').onchange=renderRelations;
$('relations-form').onsubmit=e=>{e.preventDefault();const from=$('relation-from').value,to=$('relation-to').value;if(!from||!to||from===to){$('relation-message').textContent='Choose two different records.';return;}const [fk,fi]=from.split(':'),[tk,ti]=to.split(':');mutate(async()=>{const result=await api('relations/'+fk+'/'+fi+'/'+tk+'/'+ti,'PUT');$('relation-action').textContent=JSON.stringify(result,null,2);$('relation-message').textContent='Association saved. Repeating or reversing the same pair keeps one link.';},'Association saved.');};
