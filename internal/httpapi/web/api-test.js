'use strict';
let lastAPIRequest = null, testingAPI = false;
$('api-key').value=crypto.randomUUID();
$('api-new-key').onclick=()=>{$('api-key').value=crypto.randomUUID();};
$('api-endpoint').onchange=()=>{
 const [method,path]=$('api-endpoint').value.split(' ');
 updateAPIFields(method,path);
 let body='';
 if(method==='POST'&&path==='tasks')body=JSON.stringify({title:'API test task'},null,2);
 if(method==='POST'&&path==='reminders')body=JSON.stringify({title:'API test reminder',scheduled_at:new Date(Date.now()+60000).toISOString(),timezone:zone},null,2);
 if(path.endsWith('/acknowledge'))body=JSON.stringify({action:'complete'},null,2);
 if(method==='POST'&&path==='notes'||method==='PATCH'&&path.startsWith('notes/'))body=JSON.stringify({body:'API test note'},null,2);
 if(method==='PATCH'&&path.startsWith('reminders/'))body=JSON.stringify({task_id:''},null,2);
 if(method==='PATCH'&&path.startsWith('tasks/'))body=JSON.stringify({status:'completed'},null,2);
 if(path.endsWith('/snooze'))body=JSON.stringify({scheduled_at:new Date(Date.now()+300000).toISOString()},null,2);
 $('api-body').value=body;
};
async function sendAPIRequest(request){
 if(testingAPI)return;testingAPI=true;for(const id of ['api-send','api-replay','api-new-key'])$(id).disabled=true;
 lastAPIRequest={...request};
 try{
 const headers={'Content-Type':'application/json'};if(request.key)headers['Idempotency-Key']=request.key;
 const response=await fetch('/api/v1/'+request.path,{method:request.method,headers,body:request.body||undefined});
 const text=await response.text();let body;try{body=JSON.parse(text);}catch{body=text;}
 $('api-response').textContent=JSON.stringify({request, status:response.status, headers:{location:response.headers.get('Location'),replayed:response.headers.get('Idempotency-Replayed'),allow:response.headers.get('Allow')},body},null,2);
 if(response.ok){const id=body.id||body.note_id||body.task_id||body.reminder?.id;if(id)$('api-record-id').value=id;const occurrence=body.occurrence_id||body.reminder?.occurrence_id;if(occurrence)$('api-occurrence-id').value=occurrence;await load(true);}
 }catch(error){$('api-response').textContent='No confirmed response: '+error.message+'. Replay the same request when Atlas is available.';}
 finally{testingAPI=false;for(const id of ['api-send','api-replay','api-new-key'])$(id).disabled=false;}
}
$('api-send').onclick=()=>{
 const [method,template]=$('api-endpoint').value.split(' ');const id=$('api-record-id').value.trim();
 if(template.includes('{id}')&&!id){$('api-response').textContent='Enter a record ID.';return;}
 if(method==='DELETE'&&!template.includes('/links/')&&!confirm(template.startsWith('notes/')?'Permanently delete this note?':'Permanently delete this task and cancel its linked reminders?'))return;
 if(template.includes('{occurrence}')&&!$('api-occurrence-id').value.trim()){$('api-response').textContent='Enter an occurrence ID from reminder state.';return;}
 if(template.includes('{target}')&&!$('api-link-target').value.trim()){$('api-response').textContent='Enter a link target ID.';return;}
 let path=template.replace('{id}',encodeURIComponent(id)).replace('{occurrence}',encodeURIComponent($('api-occurrence-id').value.trim())).replace('{kind}',$('api-link-kind').value).replace('{target}',encodeURIComponent($('api-link-target').value.trim()));if(path==='search')path+='?'+$('api-search-query').value;const creation=method==='POST'&&(path==='tasks'||path==='reminders'||path==='notes');
 sendAPIRequest({method,path,body:method==='GET'?'':$('api-body').value,key:creation?$('api-key').value:''});
};
$('api-replay').onclick=()=>{if(lastAPIRequest){if(lastAPIRequest.method==='DELETE'&&!lastAPIRequest.path.includes('/links/')&&!confirm('Replay the deletion request?'))return;sendAPIRequest(lastAPIRequest);}};

function updateAPIFields(method,path){const creation=method==='POST'&&['tasks','reminders','notes'].includes(path);for(const [id,visible] of [['api-search-field',path==='search'],['api-id-field',path.includes('{id}')],['api-kind-field',path.includes('{kind}')],['api-target-field',path.includes('{target}')],['api-occurrence-field',path.includes('{occurrence}')],['api-key-field',creation],['api-new-key',creation],['api-retry-hint',creation],['api-body-field',!['GET','DELETE','PUT'].includes(method)]])$(id).hidden=!visible;}
updateAPIFields(...$('api-endpoint').value.split(' '));
