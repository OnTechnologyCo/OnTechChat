'use strict';
// No API key or account. Rendering code/style are served by this chat server;
// only public background tiles and fonts are fetched from OpenFreeMap.
function historyMapResource(url){
 const u=new URL(url,location.href);
 if(u.origin===location.origin)return {url:u.href};
 if(u.protocol!=='https:'||u.hostname!=='tiles.openfreemap.org'||u.port||u.username||u.password||u.search||u.hash)throw new Error('허용되지 않은 지도 리소스');
 const p=u.pathname;
 let ok=p==='/planet',m=p.match(/^\/planet\/[0-9]{8}_[0-9]{6}_pt\/([0-9]{1,2})\/([0-9]{1,6})\/([0-9]{1,6})\.pbf$/);
 if(m)ok=+m[1]<=19&&+m[2]<2**+m[1]&&+m[3]<2**+m[1];
 m=p.match(/^\/fonts\/Noto%20Sans%20(?:Regular|Italic|Bold)\/([0-9]{1,7})-([0-9]{1,7})\.pbf$/);
 if(m)ok=+m[1]%256===0&&+m[2]===+m[1]+255&&+m[2]<=1114111;
 if(!ok)throw new Error('허용되지 않은 지도 리소스');
 return {url:u.href,credentials:'omit',referrerPolicy:'no-referrer'};
}
function historyMap(points,onSelect,camera){
 if(!window.maplibregl||camera?.renderer==='raster')return historyRasterMap(points,onSelect,camera);
 const wrap=el('div','history-map-wrap'),canvas=el('div','history-vector-map'),note=el('p','dialog-note','선명한 지도 · 배경 지도를 불러오는 중…');
 const controls=el('div','history-controls');wrap.append(canvas,controls,note);
 let map,fallback,closed=false,frame,timeout,popup,ready=false,usable=false;
 const position=p=>[p.longitude,p.latitude];
 const collection=features=>({type:'FeatureCollection',features});
 const feature=(type,coordinates,properties={})=>({type:'Feature',geometry:{type,coordinates},properties});
 const cameraState=()=>fallback?fallback.camera():map?{center:map.getCenter(),zoom:map.getZoom(),renderer:'vector'}:camera;
 function raster(reason){
  if(closed||fallback)return;const saved=cameraState();clearTimeout(timeout);cancelAnimationFrame(frame);popup?.remove();map?.remove();map=null;
  fallback=historyRasterMap(points,onSelect,saved);
  wrap.replaceChildren(el('p','dialog-note',reason+' 기본 지도로 표시합니다.'),fallback.element);
 }
 try{
  maplibregl.setWorkerUrl('/vendor/maplibre-gl-csp-worker.js?v=5.24.0');
  map=new maplibregl.Map({container:canvas,style:null,
   center:camera?[camera.center.lng,camera.center.lat]:position(points.at(-1)),zoom:camera?.zoom??15,
   maxZoom:19,dragRotate:false,pitchWithRotate:false,touchPitch:false,
   attributionControl:{compact:true},localIdeographFontFamily:'sans-serif',
   transformRequest:historyMapResource,
   locale:{'NavigationControl.ZoomIn':'확대','NavigationControl.ZoomOut':'축소','AttributionControl.ToggleAttribution':'지도 출처'}});
  map.touchZoomRotate.disableRotation();
  map.addControl(new maplibregl.NavigationControl({showCompass:false}),'top-right');
  map.addControl(new maplibregl.ScaleControl({unit:'metric'}),'bottom-left');
  const fit=()=>{const b=new maplibregl.LngLatBounds();for(const p of points)b.extend(position(p));map.fitBounds(b,{padding:40,maxZoom:17,duration:0});};
  const select=p=>{
   onSelect(p);popup?.remove();popup=new maplibregl.Popup({closeButton:false,offset:12}).setLngLat(position(p))
    .setText(new Date(p.captured_at*1000).toLocaleString()+' · 오차 ±'+Math.round(p.accuracy_m)+'m').addTo(map);
   if(ready)map.setFilter('history-selected',['==',['get','index'],points.indexOf(p)]);
  };
  controls.append(action('전체 경로',fit,'secondary'),action('마지막 위치',()=>{const p=points.at(-1);map.jumpTo({center:position(p),zoom:17});select(p);},'secondary'),
   action('기본 지도',()=>raster('기본 지도를 선택했습니다.'),'text-button'));
  map.on('sourcedata',e=>{if(e.sourceId==='openmaptiles'&&e.tile?.state==='loaded'){usable=true;clearTimeout(timeout);note.textContent='지도: OpenFreeMap · OpenStreetMap 데이터 · 표시 지역의 배경 지도만 외부에서 불러옵니다.';}});
  map.on('error',()=>{if(!closed&&!fallback)note.textContent='일부 지도 데이터를 불러오지 못했습니다. 기본 지도로 전환할 수 있습니다.';});
  map.on('webglcontextlost',()=>raster('이 기기에서 선명한 지도를 표시할 수 없습니다.'));
  map.on('style.load',()=>{
   if(closed||fallback)return;
   const lines=historySegments(points).filter(s=>s.length>1).map(s=>feature('LineString',s.map(position)));
   map.addSource('history-route',{type:'geojson',data:collection(lines)});
   map.addLayer({id:'history-outline',type:'line',source:'history-route',layout:{'line-cap':'round','line-join':'round'},paint:{'line-color':'#ffffff','line-width':9,'line-opacity':.95}});
   map.addLayer({id:'history-route',type:'line',source:'history-route',layout:{'line-cap':'round','line-join':'round'},paint:{'line-color':'#1763b9','line-width':5}});
   map.addSource('history-points',{type:'geojson',data:collection(points.map((p,i)=>feature('Point',position(p),{index:i,kind:i===points.length-1?'last':i===0?'first':'point'})))});
   const color=['match',['get','kind'],'last','#dc4434','first','#168653','#287cce'];
   map.addLayer({id:'history-points',type:'circle',source:'history-points',paint:{'circle-radius':['match',['get','kind'],'last',9,'first',7,3.5],'circle-color':color,'circle-stroke-width':2,'circle-stroke-color':'#ffffff'}});
   map.addLayer({id:'history-selected',type:'circle',source:'history-points',filter:['==',['get','index'],-1],paint:{'circle-radius':12,'circle-color':'#ffffff','circle-opacity':0,'circle-stroke-width':3,'circle-stroke-color':'#122d48'}});
   // Accuracy is an estimate, not an additional measured route segment.
   const last=points.at(-1),radius=Math.min(last.accuracy_m,10000),ring=[];
   for(let i=0;i<=64;i++){const a=i*Math.PI/32;ring.push([last.longitude+Math.sin(a)*radius/(111320*Math.max(.01,Math.cos(last.latitude*Math.PI/180))),last.latitude+Math.cos(a)*radius/111320]);}
   map.addSource('history-accuracy',{type:'geojson',data:feature('Polygon',[ring])});
   map.addLayer({id:'history-accuracy',type:'fill',source:'history-accuracy',paint:{'fill-color':'#dc4434','fill-opacity':.08}},'history-outline');
   map.addLayer({id:'history-accuracy-edge',type:'line',source:'history-accuracy',paint:{'line-color':'#dc4434','line-width':1,'line-opacity':.4}},'history-outline');
   ready=true;
   map.on('click',e=>{const hits=map.queryRenderedFeatures([[e.point.x-10,e.point.y-10],[e.point.x+10,e.point.y+10]],{layers:['history-points']});
    if(hits.length){const nearest=hits.reduce((a,b)=>{const d=f=>{const q=map.project(f.geometry.coordinates);return(q.x-e.point.x)**2+(q.y-e.point.y)**2;};return d(a)<d(b)?a:b;});select(points[nearest.properties.index]);}});
   map.on('mouseenter','history-points',()=>{canvas.style.cursor='pointer';});map.on('mouseleave','history-points',()=>{canvas.style.cursor='';});
  });
  map.setStyle('/vendor/openfreemap-liberty-ko.json?v=1.15.32',{transformStyle:(_previous,style)=>({...style,sprite:new URL(style.sprite,location.href).href})});
  frame=requestAnimationFrame(()=>{if(closed||fallback||!canvas.isConnected)return;map.resize();if(!camera)fit();});
  timeout=setTimeout(()=>{if(!usable)raster('선명한 지도 연결이 지연되고 있습니다.');},12000);
 }catch{raster('이 기기에서 선명한 지도를 표시할 수 없습니다.');}
 return {element:wrap,camera:cameraState,dispose:()=>{closed=true;clearTimeout(timeout);cancelAnimationFrame(frame);popup?.remove();fallback?.dispose();map?.remove();map=null;}};
}


function historyRasterMap(points,onSelect,camera){
 const wrap=el('div','history-map-wrap'),canvas=el('div','history-leaflet-map'),note=el('p','dialog-note','지도: OpenStreetMap · 표시 지역의 지도 타일을 외부 서비스에서 불러옵니다.');
 wrap.append(canvas,note);
 if(!window.L){wrap.replaceChildren(historySVG(points,onSelect),el('p','dialog-note','지도를 불러오지 못해 측정 경로만 표시합니다.'));return {element:wrap,dispose:()=>{},camera:()=>null};}
 const map=L.map(canvas,{zoomControl:true,attributionControl:true,preferCanvas:true,center:camera?.center??[points.at(-1).latitude,points.at(-1).longitude],zoom:camera?.zoom??15});
 const tiles=L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,referrerPolicy:'origin',attribution:'&copy; <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a> contributors'});
 tiles.on('tileerror',()=>{note.textContent='배경 지도 연결 실패 · 이동 경로와 측정 지점은 표시됩니다. 인터넷 연결을 확인하세요.';});tiles.addTo(map);
 for(const segment of historySegments(points)){const latlngs=segment.map(p=>[p.latitude,p.longitude]);L.polyline(latlngs,{color:'#ffffff',weight:9,opacity:.95}).addTo(map);L.polyline(latlngs,{color:'#1763b9',weight:5,opacity:1}).addTo(map);}
 points.forEach((p,i)=>{
  const last=i===points.length-1,first=i===0,label=(last?'마지막 측정 · ':first?'시작 · ':'')+new Date(p.captured_at*1000).toLocaleString()+' · ±'+Math.round(p.accuracy_m)+'m';
  const marker=L.circleMarker([p.latitude,p.longitude],{radius:last?9:first?7:4,color:last?'#b52828':first?'#177341':'#1763b9',fillColor:last?'#ee5a47':first?'#36ac71':'#438fe3',fillOpacity:1,weight:2}).addTo(map);
  marker.bindTooltip(label,{direction:'top'});marker.on('click',()=>{onSelect(p);marker.openTooltip();});
  if(last)L.circle([p.latitude,p.longitude],{radius:Math.min(p.accuracy_m,10000),color:'#b52828',weight:1,fillOpacity:.08}).addTo(map);
 });
 L.control.scale({imperial:false}).addTo(map);
 const fit=()=>map.fitBounds(L.latLngBounds(points.map(p=>[p.latitude,p.longitude])),{padding:[24,24],maxZoom:17});
 const controls=el('div','history-controls');controls.append(action('전체 경로',fit,'secondary'),action('마지막 위치',()=>{const p=points.at(-1);map.setView([p.latitude,p.longitude],17);onSelect(p);},'secondary'));wrap.append(controls);
 const frame=requestAnimationFrame(()=>{if(!canvas.isConnected)return;map.invalidateSize();if(camera)map.setView(camera.center,camera.zoom);else fit();});
 return {element:wrap,dispose:()=>{cancelAnimationFrame(frame);map.remove();},camera:()=>({center:map.getCenter(),zoom:map.getZoom(),renderer:'raster'})};
}
