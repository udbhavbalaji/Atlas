'use strict';
const featureNames=['tasks','reminders','notes','search','activity','api'];
function showFeature(name,{focus=false,updateURL=true}={}){
 name=name.split('/')[0];
 if(!featureNames.includes(name))name='tasks';
 for(const feature of featureNames){const active=feature===name;const tab=document.getElementById('tab-'+feature);tab.setAttribute('aria-selected',String(active));tab.tabIndex=active?0:-1;document.getElementById('panel-'+feature).hidden=!active;}
 document.title='Atlas · '+(name==='api'?'API lab':name[0].toUpperCase()+name.slice(1));
 if(updateURL&&location.hash!=='#'+name)history.replaceState(null,'','#'+name);
 if(focus)document.getElementById('tab-'+name).focus();
}
function updateReminderBadge(list){const count=list.filter(r=>r.status==='due').length;const badge=document.getElementById('reminder-badge');badge.textContent=count+' due';badge.hidden=count===0;}
for(const name of featureNames){const tab=document.getElementById('tab-'+name);tab.onclick=()=>showFeature(name);tab.onkeydown=e=>{let index=featureNames.indexOf(name);if(e.key==='ArrowRight')index=(index+1)%featureNames.length;else if(e.key==='ArrowLeft')index=(index+featureNames.length-1)%featureNames.length;else if(e.key==='Home')index=0;else if(e.key==='End')index=featureNames.length-1;else return;e.preventDefault();showFeature(featureNames[index],{focus:true});};}
window.addEventListener('hashchange',()=>{showFeature(location.hash.slice(1),{updateURL:false});if(typeof revealURLRecord==='function')revealURLRecord();});
showFeature(location.hash.slice(1),{updateURL:false});
