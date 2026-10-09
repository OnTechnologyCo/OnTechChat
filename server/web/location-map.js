
function historyMap(points,onSelect,camera){
 const wrap=el('div','history-map-wrap'),canvas=el('div','history-leaflet-map'),note=el('p','dialog-note','지도: OpenStreetMap · 표시 지역의 지도 타일을 외부 서비스에서 불러옵니다.');
 wrap.append(canvas,note);
 if(!window.L){wrap.replaceChildren(historySVG(points,onSelect),el('p','dialog-note','지도를 불러오지 못해 측정 경로만 표시합니다.'));return {element:wrap,dispose:()=>{},camera:()=>null};}
 const map=L.map(canvas,{zoomControl:true,attributionControl:true,preferCanvas:true});
 const tiles=L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,referrerPolicy:'origin',attribution:'&copy; <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a> contributors'});
 tiles.on('tileerror',()=>{note.textContent='배경 지도 연결 실패 · 이동 경로와 측정 지점은 표시됩니다. 인터넷 연결을 확인하세요.';});tiles.addTo(map);
 for(const segment of historySegments(points)){const latlngs=segment.map(p=>[p.latitude,p.longitude]);L.polyline(latlngs,{color:'#1763b9',weight:5,opacity:.8}).addTo(map);}
 points.forEach((p,i)=>{
  const last=i===points.length-1,first=i===0,label=(last?'마지막 측정 · ':first?'시작 · ':'')+new Date(p.captured_at*1000).toLocaleString()+' · ±'+Math.round(p.accuracy_m)+'m';
  const marker=L.circleMarker([p.latitude,p.longitude],{radius:last?9:first?7:4,color:last?'#b52828':first?'#177341':'#1763b9',fillColor:last?'#ee5a47':first?'#36ac71':'#438fe3',fillOpacity:1,weight:2}).addTo(map);
  marker.bindTooltip(label,{direction:'top'});marker.on('click',()=>{onSelect(p);marker.openTooltip();});
  if(last)L.circle([p.latitude,p.longitude],{radius:Math.min(p.accuracy_m,10000),color:'#b52828',weight:1,fillOpacity:.08}).addTo(map);
 });
 const fit=()=>map.fitBounds(L.latLngBounds(points.map(p=>[p.latitude,p.longitude])),{padding:[24,24],maxZoom:17});
 const controls=el('div','history-controls');controls.append(action('전체 경로',fit,'secondary'),action('마지막 위치',()=>{const p=points.at(-1);map.setView([p.latitude,p.longitude],17);onSelect(p);},'secondary'));wrap.append(controls);
 const frame=requestAnimationFrame(()=>{if(!canvas.isConnected)return;map.invalidateSize();if(camera)map.setView(camera.center,camera.zoom);else fit();});
 return {element:wrap,dispose:()=>{cancelAnimationFrame(frame);map.remove();},camera:()=>({center:map.getCenter(),zoom:map.getZoom()})};
}
