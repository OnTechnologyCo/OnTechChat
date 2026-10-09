'use strict';
function openHistorySettings(){
 const m=navigator.userAgent.match(/OnTechChatAndroid\/(\d+)\.(\d+)/);
 if(!m||+m[1]<1||(+m[1]===1&&+m[2]<8)){toast('GPS 기록 설정은 Android 앱 1.8.0 이상에서 사용할 수 있습니다.');return;}
 location.href='ontechchat://location-history-settings';
}
function historySegments(points){
 const groups=new Map();for(const p of points){const k=p.device_id||'';if(!groups.has(k))groups.set(k,[]);groups.get(k).push(p);}
 const lines=[];for(const group of groups.values()){
  group.sort((a,b)=>a.captured_at-b.captured_at);let line=[];
  for(const p of group){const prev=line.at(-1);if(prev&&(p.captured_at-prev.captured_at>180||Math.abs(p.longitude-prev.longitude)>180)){if(line.length)lines.push(line);line=[];}line.push(p);}
  if(line.length)lines.push(line);
 }return lines;
}
function historySVG(points,onSelect){
 const ns='http://www.w3.org/2000/svg',svg=document.createElementNS(ns,'svg');svg.setAttribute('viewBox','0 0 640 320');svg.setAttribute('role','img');svg.setAttribute('aria-label','측정 지점을 시간 순서로 연결한 이동 경로. 위쪽이 북쪽입니다.');svg.classList.add('history-map');
 if(!points.length)return svg;
 const lat0=points.reduce((n,p)=>n+p.latitude,0)/points.length,scale=Math.cos(lat0*Math.PI/180);
 const coords=points.map(p=>({p,x:p.longitude*scale,y:-p.latitude})),xs=coords.map(c=>c.x),ys=coords.map(c=>c.y);
 const minX=Math.min(...xs),maxX=Math.max(...xs),minY=Math.min(...ys),maxY=Math.max(...ys),unit=Math.max((maxX-minX)/580,(maxY-minY)/260,0.000002);
 const xy=p=>[320+(p.longitude*scale-(minX+maxX)/2)/unit,160+(-p.latitude-(minY+maxY)/2)/unit];
 for(const line of historySegments(points)){const path=document.createElementNS(ns,'polyline');path.setAttribute('points',line.map(p=>xy(p).join(',')).join(' '));path.setAttribute('fill','none');path.setAttribute('stroke','#3778b8');path.setAttribute('stroke-width','2');svg.append(path);}
 points.forEach((p,i)=>{const[x,y]=xy(p),dot=document.createElementNS(ns,'circle');dot.setAttribute('cx',x);dot.setAttribute('cy',y);dot.setAttribute('r',i===0||i===points.length-1?'6':'3');dot.setAttribute('fill',i===0?'#2f9667':i===points.length-1?'#d75c43':'#3778b8');const title=document.createElementNS(ns,'title');title.textContent=new Date(p.captured_at*1000).toLocaleString()+' · 오차 ±'+Math.round(p.accuracy_m)+'m';dot.append(title);dot.addEventListener('click',()=>onSelect(p));svg.append(dot);});
 return svg;
}
async function openLocationHistory(user){
 if(!currentUser)return;
 const viewer=currentUser.id,body=dialog(shownName(user)+' · 이동 경로'),controls=el('div','history-controls');
 let hours=1;const ranges=[];
 for(const value of[1,2,6,12,24,48]){const button=action('최근 '+value+'시간',()=>{hours=value;for(const [n,b]of ranges){b.setAttribute('aria-pressed',String(n===hours));b.classList.toggle('primary',n===hours);}clear();refresh(false);},value===1?'primary':'secondary');button.setAttribute('aria-pressed',String(value===1));ranges.push([value,button]);controls.append(button);}
 const status=el('p','history-status'),result=el('section','history-result');status.setAttribute('role','status');
 const load=action('새로고침',()=>refresh(false),'secondary');controls.append(load);
 body.append(el('p','dialog-note','최근 48시간 · 1분 간격 측정 목표 · 표시 시간대: '+(Intl.DateTimeFormat().resolvedOptions().timeZone||'기기 시간대')+'. GPS 미수신·절전·통신 지연 구간은 기록이 없을 수 있습니다.'),controls,status,result);
 if(user.id===viewer)body.append(action('내 위치 조회 허용 설정',openHistorySettings,'secondary'),action('내 모든 이동 기록 삭제',async()=>{
  if(!confirm('모든 기기의 이동 기록을 삭제하고 기록을 중지합니다. 연결이 끊긴 기기의 미전송 기록도 다음 연결 시 폐기됩니다. 삭제할까요?'))return;
  clear();await api('/location-history','DELETE');status.textContent='기록을 삭제하고 중지했습니다. 다시 기록하려면 앱에서 시작하세요.';
 },'danger'));
 let serial=0,busy=false,timer,controller,mapView;const active=()=>currentUser?.id===viewer&&$('dialog').open&&status.isConnected;
 function clear(){serial++;controller?.abort();busy=false;mapView?.dispose();mapView=null;result.replaceChildren();}
 async function refresh(background){
  if(!active()||(background&&busy))return;
  const to=Math.floor(Date.now()/1000),from=to-hours*3600;
  const camera=background?mapView?.camera():null;clear();const version=serial;
  if(!Number.isFinite(from)||!Number.isFinite(to)||from>to){status.textContent='날짜와 시간 범위를 확인하세요.';return;}
  busy=true;load.disabled=true;status.textContent=background?'기록과 공유 권한을 확인하는 중…':'이동 기록을 불러오는 중…';controller=new AbortController();
  try{
   const response=await fetch('/api/location-history/'+encodeURIComponent(user.id)+'?from='+from+'&to='+to,{credentials:'same-origin',cache:'no-store',signal:controller.signal});
   const data=await response.json();if(!active()||version!==serial)return;
   if(!response.ok){if(response.status===401&&data.code==='SESSION_INVALID')resetSession();throw new Error(response.status===403?'상대가 이동 기록 공유를 허용하지 않았거나 중지했습니다.':data.error||'기록 조회 실패');}
   const points=(data.points||[]).sort((a,b)=>a.captured_at-b.captured_at);if(!points.length){status.textContent='선택한 시간에 서버로 전달된 기록이 없습니다. 기록 시작·GPS 상태·인터넷 연결을 확인하세요.';return;}
   status.textContent='최근 '+hours+'시간 · '+points.length+'개 지점 · 최근 측정 '+new Date(points.at(-1).captured_at*1000).toLocaleString()+(Date.now()/1000-points.at(-1).captured_at>180?' · 3분 이상 새 기록 없음':'')+(data.truncated?' · 일부만 표시, 시간 범위를 좁혀 주세요.':'');
   const selected=el('p','history-selected','지점 또는 시간 항목을 선택하면 좌표와 측정 정확도를 확인합니다.');
   const describe=p=>{selected.replaceChildren(el('span','',new Date(p.captured_at*1000).toLocaleString()+' · '+p.latitude.toFixed(6)+', '+p.longitude.toFixed(6)+' · 오차 ±'+Math.round(p.accuracy_m)+'m'+(p.approximate?' · 대략적 위치':'')),action('외부 지도에서 보기',()=>{
    if(confirm('선택한 지점의 좌표를 외부 지도 서비스에 전달할까요?'))location.href='https://www.google.com/maps/search/?api=1&query='+encodeURIComponent(p.latitude+','+p.longitude);
   },'text-button'));};
   mapView=historyMap(points,describe,camera);result.append(mapView.element,el('p','dialog-note','초록: 시작 / 빨강: 마지막 측정. 선은 측정 지점 연결이며 3분 초과 공백·다른 기기는 연결하지 않습니다. 마지막 기록이 현재 위치와 다를 수 있습니다.'),selected);describe(points.at(-1));
   const list=el('ol','history-list');for(const p of points){const row=el('li');row.append(action(new Date(p.captured_at*1000).toLocaleTimeString()+' · ±'+Math.round(p.accuracy_m)+'m'+(p.approximate?' (대략)':''),()=>describe(p),'text-button'));list.append(row);}result.append(list);
  }catch(e){if(e.name!=='AbortError'&&active()&&version===serial){result.replaceChildren();status.textContent=e.message;}}
  finally{if(version===serial){busy=false;if(active())load.disabled=false;}}
 }
 // Each quick range queries immediately, relative to the current time.
 const visibility=()=>{if(document.hidden)clear();else if(active())refresh(true);};document.addEventListener('visibilitychange',visibility);
 const cleanup=()=>{clear();clearInterval(timer);document.removeEventListener('visibilitychange',visibility);$('dialog').removeEventListener('close',cleanup);observer.disconnect();};
 $('dialog').addEventListener('close',cleanup);const observer=new MutationObserver(()=>{if(!active())cleanup();});observer.observe($('dialog-body'),{childList:true});
 timer=setInterval(()=>{if(!active())cleanup();else if(!document.hidden)refresh(true);},15000);
 await refresh(false);
}
