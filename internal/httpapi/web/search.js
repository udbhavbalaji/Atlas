'use strict';
let searchPage=null,searchBusy=false;
const searchStatuses={task:['open','completed'],reminder:['scheduled','due','dismissed','completed'],note:[],all:['open','completed','scheduled','due','dismissed']};
function updateSearchStatuses(){const selected=$('search-status').value;$('search-status').replaceChildren();for(const value of ['',...searchStatuses[$('search-type').value||'all']]){const option=document.createElement('option');option.value=value;option.textContent=value||'Any status';$('search-status').append(option);}$('search-status').value=selected;if($('search-status').selectedIndex<0)$('search-status').value='';}
$('search-type').onchange=updateSearchStatuses;
async function performSearch(offset=0,options=null){
 if(searchBusy)return;const request=options||{q:$('search-query').value.trim(),type:$('search-type').value,status:$('search-status').value,limit:20};if(!request.q){$('search-message').textContent='Enter text to search.';return;}
 if(!options){searchPage=null;$('search-results').replaceChildren();$('search-pages').hidden=true;}
 searchBusy=true;$('search-submit').disabled=true;$('search-next').disabled=true;$('search-results').setAttribute('aria-busy','true');$('search-message').textContent='Searching…';
 try{const params=new URLSearchParams({...request,offset});const response=await api('search?'+params);searchPage={request,offset,response};renderSearchResults(response);}
 catch(e){$('search-message').textContent=e.message;}
 finally{searchBusy=false;$('search-submit').disabled=false;$('search-results').setAttribute('aria-busy','false');$('search-next').disabled=!searchPage?.response.next_offset;$('search-previous').disabled=!searchPage?.offset;}
}
function renderSearchResults(response){
 const host=$('search-results');host.replaceChildren();const count=response.results.length;$('search-message').textContent=count?'Showing '+(response.offset+1)+'–'+(response.offset+count)+(response.has_more?' · more matches available':''):'No matches. Try different text or filters.';
 for(const result of response.results){const row=document.createElement('article');row.className='task';const meta=document.createElement('p');meta.className='meta';meta.textContent=result.type+(result.status?' · '+result.status:'')+' · matched '+result.matched_fields.join(', ');const title=document.createElement('h3');title.className='title';title.textContent=result.title;const snippet=document.createElement('p');snippet.className='description';snippet.textContent=result.snippet;const actions=document.createElement('div');actions.className='actions';const open=document.createElement('a');open.href=result.url;open.className='record-link';open.textContent='Open '+result.type;open.onclick=async e=>{e.preventDefault();try{await api(result.api_url.replace('/api/v1/',''));await load();const route=result.url.split('#')[1];showFeature(route,{updateURL:false});history.replaceState(null,'','#'+route);revealURLRecord();}catch(error){$('search-message').textContent='This result could not be opened: '+error.message+' Search again to refresh results.';}};const inspect=document.createElement('a');inspect.href=result.api_url;inspect.target='_blank';inspect.rel='noopener';inspect.textContent='JSON state';actions.append(open,inspect);row.append(meta,title,snippet,actions);host.append(row);}
 $('search-previous').disabled=response.offset===0;$('search-next').disabled=response.next_offset===null;$('search-pages').hidden=count===0;
}
$('search-form').onsubmit=e=>{e.preventDefault();performSearch();};
$('search-next').onclick=()=>{if(searchPage && searchPage.response.next_offset!==null)performSearch(searchPage.response.next_offset,searchPage.request);};
$('search-previous').onclick=()=>{if(searchPage)performSearch(Math.max(0,searchPage.offset-searchPage.request.limit),searchPage.request);};
function revealURLRecord(){
 const [feature,id]=location.hash.slice(1).split('/');if(!id||!['tasks','reminders','notes'].includes(feature)||!/^[a-f0-9]{32}$/.test(id))return;
 if(feature==='tasks'){filter='all';document.querySelectorAll('[data-filter]').forEach(x=>x.setAttribute('aria-pressed',String(x.dataset.filter==='all')));render();}
 const singular={tasks:'task',reminders:'reminder',notes:'note'}[feature];const row=document.getElementById(singular+'-'+id);if(!row)return;
 let parent=row.parentElement;while(parent){if(parent.tagName==='DETAILS')parent.open=true;parent=parent.parentElement;}
 row.tabIndex=-1;row.classList.add('search-target');row.focus({preventScroll:true});row.scrollIntoView({behavior:'smooth',block:'center'});setTimeout(()=>row.classList.remove('search-target'),2500);
}
updateSearchStatuses();
