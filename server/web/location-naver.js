'use strict';
// Public browser Client ID, not the Naver Client Secret. Load only for GPS history.
const historyNaverKey='r6i3znksyy';
let historyNaverLoading,historyNaverAuthError=false;
const historyNaverFailures=new Set();
function historyNaverSDK(){
 if(historyNaverAuthError)return Promise.reject(new Error('네이버 지도 인증 실패 · 등록한 Web 서비스 URL과 Client ID를 확인하세요.'));
 if(window.naver?.maps?.Map)return Promise.resolve(window.naver.maps);
 if(historyNaverLoading)return historyNaverLoading;
 historyNaverLoading=new Promise((resolve,reject)=>{
  const script=document.createElement('script');let settled=false;
  const finish=error=>{
   if(settled)return;settled=true;clearTimeout(timeout);
   if(error){script.remove();reject(error);}else resolve(window.naver.maps);
  };
  const timeout=setTimeout(()=>finish(new Error('네이버 지도 연결 시간이 초과되었습니다.')),12000);
  // The SDK callback may run before its final window.naver.maps assignment.
  window.onTechNaverMapReady=()=>Promise.resolve().then(()=>finish(window.naver?.maps?.Map?null:new Error('네이버 지도 SDK를 불러오지 못했습니다.')));
  window.navermap_authFailure=()=>{
   historyNaverAuthError=true;
   const reason='네이버 지도 인증 실패 · 등록한 Web 서비스 URL과 Client ID를 확인하세요.';
   finish(new Error(reason));for(const failed of [...historyNaverFailures])failed(reason);
  };
  script.async=true;script.referrerPolicy='origin';
  script.src='https://oapi.map.naver.com/openapi/v3/maps.js?ncpKeyId='+encodeURIComponent(historyNaverKey)+'&callback=onTechNaverMapReady&language=ko';
  script.onerror=()=>finish(new Error('네이버 지도 연결 실패 · 인터넷 연결과 앱 업데이트를 확인하세요.'));
  document.head.append(script);
 });
 return historyNaverLoading;
}
function historyMap(points,onSelect,camera){
 // Older Android builds block third-party SDKs. Keep their existing map working.
 const app=navigator.userAgent.match(/OnTechChatAndroid\/(\d+)\.(\d+)\.(\d+)/);
 const oldApp=app&&(+app[1]<1||(+app[1]===1&&(+app[2]<8||(+app[2]===8&&+app[3]<10))));
 if(oldApp||camera?.renderer==='vector'||camera?.renderer==='raster'){
  const view=historyOpenMap(points,onSelect,camera);
  if(oldApp)view.element.prepend(el('p','dialog-note','네이버 지도는 Android 앱 1.8.10 이상에서 지원합니다. 앱을 업데이트해 주세요.'));
  return view;
 }
 const wrap=el('div','history-map-wrap'),canvas=el('div','history-naver-map');
 canvas.setAttribute('aria-label','네이버 지도 · GPS 이동 경로');
 const controls=el('div','history-controls'),note=el('p','dialog-note','네이버 지도를 불러오는 중…');
 wrap.append(canvas,controls,note);
 let map,sdk,fallback,closed=false,ready=false,timeout,observer;
 const overlays=[],listeners=[];
 const cameraState=()=>{
  if(fallback)return fallback.camera();
  if(!ready||!map)return camera;
  const center=map.getCenter();return {center:{lat:center.lat(),lng:center.lng()},zoom:map.getZoom(),renderer:'naver'};
 };
 function destroy(){
  clearTimeout(timeout);observer?.disconnect();
  for(const listener of listeners)sdk.Event.removeListener(listener);listeners.length=0;
  for(const overlay of overlays)overlay.setMap(null);overlays.length=0;
  if(map){try{map.destroy();}catch{/* The SDK also destroys maps on authentication failure. */}map=null;}
  ready=false;
 }
 function useFallback(reason){
  if(closed||fallback)return;
  const saved=cameraState();historyNaverFailures.delete(useFallback);destroy();
  fallback=historyOpenMap(points,onSelect,saved);
  wrap.replaceChildren(el('p','dialog-note',reason+' 대체 지도로 표시합니다.'),fallback.element);
 }
 historyNaverFailures.add(useFallback);
 historyNaverSDK().then(api=>{
  if(closed||fallback)return;sdk=api;
  const position=p=>new sdk.LatLng(p.latitude,p.longitude);
  const last=points.at(-1);
  const fit=()=>{if(ready)map.fitBounds(points.map(position),{top:40,right:40,bottom:40,left:40,maxZoom:17});};
  map=new sdk.Map(canvas,{center:camera?new sdk.LatLng(camera.center.lat,camera.center.lng):position(last),
   zoom:camera?.zoom??16,minZoom:0,maxZoom:21,zoomControl:true,scaleControl:true,mapDataControl:true,logoControl:true});
  listeners.push(sdk.Event.addListener(map,'init',()=>{
   if(closed||fallback||ready)return;ready=true;clearTimeout(timeout);
   try{
    for(const line of historySegments(points).filter(s=>s.length>1)){
     const path=line.map(position);
     overlays.push(new sdk.Polyline({map,path,strokeColor:'#ffffff',strokeWeight:9,strokeOpacity:.95,clickable:false,zIndex:1}));
     overlays.push(new sdk.Polyline({map,path,strokeColor:'#1763b9',strokeWeight:5,strokeOpacity:1,clickable:false,zIndex:2}));
    }
    overlays.push(new sdk.Circle({map,center:position(last),radius:Math.min(last.accuracy_m,10000),
     strokeColor:'#dc4434',strokeWeight:1,strokeOpacity:.4,fillColor:'#dc4434',fillOpacity:.08,clickable:false,zIndex:0}));
    points.forEach((p,i)=>{
     const isLast=i===points.length-1,isFirst=i===0;
     const marker=new sdk.Marker({map,position:position(p),
      title:(isLast?'마지막 측정 · ':isFirst?'시작 · ':'')+new Date(p.captured_at*1000).toLocaleString()+' · ±'+Math.round(p.accuracy_m)+'m',
      icon:{path:sdk.SymbolPath.CIRCLE,radius:isLast?9:isFirst?7:4,fillColor:isLast?'#dc4434':isFirst?'#168653':'#287cce',
       fillOpacity:1,strokeColor:'#ffffff',strokeWeight:2},zIndex:isLast?5:4});
     overlays.push(marker);listeners.push(sdk.Event.addListener(marker,'click',()=>{if(!closed&&!fallback)onSelect(p);}));
    });
    controls.append(action('전체 경로',fit,'secondary'),action('마지막 위치',()=>{
     if(!ready)return;map.setCenter(position(last));map.setZoom(17);onSelect(last);
    },'secondary'),action('대체 지도',()=>useFallback('대체 지도를 선택했습니다.'),'text-button'));
    note.textContent='지도: NAVER · 배경 지도와 지도 인증을 위해 네이버에 연결합니다.';
    map.autoResize();if(!camera)fit();
    if(window.ResizeObserver){observer=new ResizeObserver(()=>{if(ready&&!closed)map.autoResize();});observer.observe(canvas);}
   }catch{useFallback('네이버 지도 표시 중 오류가 발생했습니다.');}
  }));
  timeout=setTimeout(()=>useFallback('네이버 지도 데이터를 불러오지 못했습니다.'),15000);
 }).catch(error=>useFallback(error.message));
 return {element:wrap,camera:cameraState,dispose:()=>{
  if(closed)return;closed=true;historyNaverFailures.delete(useFallback);destroy();fallback?.dispose();
 }};
}
