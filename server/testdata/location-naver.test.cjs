'use strict';
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const sources=['location-history.js','location-naver.js'].map(file=>fs.readFileSync(path.join(__dirname,'../web',file),'utf8'));
function harness(userAgent='browser'){
 const timers=new Map(),maps=[],overlays=[],fallbacks=[],scripts=[];let nextTimer=0,selected;
 class Element{
  constructor(tag,cls='',text=''){this.tag=tag;this.className=cls;this.textContent=text;this.children=[];this.isConnected=true;}
  append(...children){this.children.push(...children);}
  prepend(...children){this.children.unshift(...children);}
  replaceChildren(...children){this.children=children;}
  setAttribute(){}
  remove(){this.removed=true;}
 }
 const Event={
  addListener(target,name,fn){const item={target,name,fn};(target.listeners??=[]).push(item);return item;},
  removeListener(item){item.removed=true;},
  fire(target,name){for(const item of [...target.listeners??[]])if(!item.removed&&item.name===name)item.fn();}
 };
 class LatLng{constructor(lat,lng){this.y=lat;this.x=lng;}lat(){return this.y;}lng(){return this.x;}}
 class MapView{
  constructor(container,options){this.container=container;this.options=options;this.center=options.center;this.zoom=options.zoom;maps.push(this);}
  getCenter(){return this.center;}getZoom(){return this.zoom;}
  setCenter(center){this.center=center;}setZoom(zoom){this.zoom=zoom;}
  fitBounds(points,options){this.fit={points,options};}autoResize(){this.resized=true;}destroy(){this.destroyed=true;}
 }
 class Overlay{constructor(options){this.options=options;this.map=options.map;overlays.push(this);}setMap(map){this.map=map;}}
 const sdk={Map:MapView,LatLng,Event,Polyline:class Polyline extends Overlay{},Marker:class Marker extends Overlay{},Circle:class Circle extends Overlay{},SymbolPath:{CIRCLE:'circle'}};
 const context={
  navigator:{userAgent},document:{head:{append:script=>scripts.push(script)},createElement:tag=>new Element(tag)},
  setTimeout:(fn,ms)=>{const id=++nextTimer;timers.set(id,{fn,ms});return id;},clearTimeout:id=>timers.delete(id),
  el:(...args)=>new Element(...args),action:(text,fn)=>Object.assign(new Element('button','',text),{click:fn}),
  historyOpenMap:(points,onSelect,camera)=>{const view={element:new Element('fallback'),camera:()=>({...camera,renderer:'vector'}),dispose:()=>{view.disposed=true;}};fallbacks.push(view);return view;},
  ResizeObserver:class{observe(){}disconnect(){this.disconnected=true;}},
 };
 context.window=context;
 vm.createContext(context);for(const source of sources)vm.runInContext(source,context);
 const points=[
  {latitude:37.5,longitude:127,captured_at:1000,accuracy_m:20,device_id:'one'},
  {latitude:37.51,longitude:127.01,captured_at:1060,accuracy_m:20,device_id:'one'},
  {latitude:37.52,longitude:127.02,captured_at:1400,accuracy_m:30,device_id:'one'},
  {latitude:37.53,longitude:127.03,captured_at:1410,accuracy_m:40,device_id:'two'},
 ];
 return {context,maps,overlays,fallbacks,scripts,timers,points,
  open:camera=>context.historyMap(points,p=>{selected=p;},camera),
  ready:()=>{context.naver={maps:sdk};context.onTechNaverMapReady();},
  readyBeforeExport:()=>{context.onTechNaverMapReady();context.naver={maps:sdk};},
  init:()=>Event.fire(maps.at(-1),'init'),clickMarker:index=>Event.fire(overlays.filter(x=>x instanceof sdk.Marker)[index],'click'),
  selected:()=>selected,expire:ms=>{for(const [id,timer] of [...timers])if(timer.ms===ms){timers.delete(id);timer.fn();}},
 };
}
const flush=async()=>{for(let i=0;i<4;i++)await Promise.resolve();};
(async()=>{
 let h=harness(),v=h.open();assert.equal(h.scripts.length,1);
 assert.match(h.scripts[0].src,/ncpKeyId=r6i3znksyy&callback=onTechNaverMapReady&language=ko/);
 assert.equal(h.scripts[0].referrerPolicy,'origin');
 h.ready();await flush();h.init();assert.equal(h.maps.length,1);
 assert.equal(h.overlays.filter(o=>o.constructor.name==='Polyline').length,2,'only the contiguous same-device segment is connected');
 assert.equal(h.overlays.filter(o=>o.constructor.name==='Marker').length,4);
 assert.equal(h.maps[0].fit.options.maxZoom,17);assert.equal(h.maps[0].resized,true);
 h.clickMarker(3);assert.equal(h.selected(),h.points[3]);
 h.maps[0].setCenter(new h.context.naver.maps.LatLng(38,128));h.maps[0].setZoom(18);
 assert.equal(JSON.stringify(v.camera()),JSON.stringify({center:{lat:38,lng:128},zoom:18,renderer:'naver'}));
 const camera=v.camera();v.dispose();v.dispose();assert.ok(h.maps[0].destroyed);assert.ok(h.overlays.every(o=>o.map===null));
 assert.equal(h.timers.size,0);h.context.navermap_authFailure();assert.equal(h.fallbacks.length,0,'disposed view cannot revive on a late auth failure');
 console.log('PASS path segmentation, markers, selection, camera and cleanup');
 h=harness();v=h.open();h.readyBeforeExport();await flush();h.init();assert.equal(h.maps.length,1);assert.equal(h.fallbacks.length,0);v.dispose();
 console.log('PASS SDK callback before global namespace export');
 h=harness();v=h.open();v.dispose();h.ready();await flush();assert.equal(h.maps.length,0);assert.equal(h.fallbacks.length,0);
 console.log('PASS late SDK load after close/revocation does not recreate a map');
 h=harness();v=h.open();h.scripts[0].onerror();await flush();assert.equal(h.fallbacks.length,1);assert.match(v.element.children[0].textContent,/연결 실패/);v.dispose();assert.ok(h.fallbacks[0].disposed);
 console.log('PASS network failure fallback and disposal');
 h=harness();v=h.open();h.expire(12000);await flush();assert.equal(h.fallbacks.length,1);h.ready();await flush();assert.equal(h.maps.length,0);
 console.log('PASS SDK timeout rejects late completion');
 h=harness();v=h.open();h.ready();await flush();h.expire(15000);assert.equal(h.fallbacks.length,1);h.init();assert.equal(h.overlays.length,0);
 console.log('PASS map initialization timeout cannot restore revoked UI');
 h=harness();v=h.open();h.ready();await flush();h.init();h.context.navermap_authFailure();assert.equal(h.fallbacks.length,1);assert.ok(h.maps[0].destroyed);assert.match(v.element.children[0].textContent,/인증 실패/);
 console.log('PASS authentication failure removes NAVER map and shows the reason');
 h=harness();v=h.open(camera);h.ready();await flush();h.init();assert.equal(h.maps[0].center.lat(),38);assert.equal(h.maps[0].zoom,18);assert.equal(h.maps[0].fit,undefined);
 const second=h.open(camera);await flush();h.init();assert.equal(h.scripts.length,1,'SDK is loaded once per page');second.dispose();v.dispose();
 console.log('PASS periodic refresh restores camera and reuses SDK');
 h=harness('OnTechChatAndroid/1.8.9');v=h.open();assert.equal(h.scripts.length,0);assert.equal(h.fallbacks.length,1);assert.match(v.element.children[0].textContent,/업데이트/);
 h=harness('OnTechChatAndroid/1.8.10');v=h.open();assert.equal(h.scripts.length,1);v.dispose();h.expire(12000);await flush();
 console.log('PASS old APK fallback and new APK NAVER routing');
 h=harness();v=h.open({center:{lat:38,lng:128},zoom:18,renderer:'raster'});assert.equal(h.scripts.length,0);assert.equal(h.fallbacks.length,1);
 console.log('PASS explicit fallback choice survives refresh');
})().catch(error=>{console.error(error);process.exitCode=1;});
