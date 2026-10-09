'use strict';
// 첨부 초안은 현재 브라우저 메모리에만 보관합니다. 전송 전에는 서버 요청이 없습니다.
const composerDrafts=new Map();
let composerEpoch=0,composerPicker=null;
function composerDraft(room=currentRoom){
 if(!room||!currentUser)return null;
 let d=composerDrafts.get(room.id);
 if(!d||d.owner!==currentUser.id){d={owner:currentUser.id,room:room.id,items:[],busy:false,pendingText:null,error:'',controller:null};composerDrafts.set(room.id,d);}
 return d;
}
function releaseComposerItem(item){if(item.url){URL.revokeObjectURL(item.url);item.url='';}}
function clearComposerDrafts(){
 composerEpoch++;composerPicker=null;
 for(const d of composerDrafts.values()){d.controller?.abort();d.items.forEach(releaseComposerItem);}
 composerDrafts.clear();$('file-input').value='';$('attachment-drafts').replaceChildren();$('attachment-drafts').hidden=true;$('composer-status').hidden=true;
}
function clearComposerRoom(id){
 const d=composerDrafts.get(id);if(!d)return;
 d.controller?.abort();d.items.forEach(releaseComposerItem);composerDrafts.delete(id);
 if(currentRoom?.id===id)renderComposerAttachments();
}
function composerIsCurrent(d){return currentUser?.id===d.owner&&currentRoom?.id===d.room&&composerDrafts.get(d.room)===d;}
function composerCanSend(){
 const r=currentRoom;if(!r||!currentUser||!['chat','direct','ai'].includes(r.kind)||r.can_send===false)return false;
 if(r.kind==='ai'){
  if(!aiCatalog.enabled||!aiModel(r.ai_model_id))return false;
  if([activeAIRequest,activeAgentJob].some(q=>q&&q.room_id===r.id&&['queued','planning','running'].includes(q.status)))return false;
 }
 return true;
}
function syncComposer(){
 const d=currentRoom&&currentUser?composerDraft():null,busy=!!d?.busy,allowed=!!currentRoom&&['chat','direct','ai'].includes(currentRoom.kind)&&currentRoom.can_send!==false;
 $('attach-button').disabled=!allowed||busy;$('message-text').disabled=!allowed||busy;
 $('send-button').disabled=!composerCanSend()||busy||(!$('message-text').value.trim()&&!d?.items.length);
 $('send-button').classList.toggle('sending',busy);$('send-button').setAttribute('aria-label',busy?'전송 중':'메시지 보내기');
 $('clear-ai-chat').disabled=busy;
 const state=$('composer-status');state.textContent=d?.error||'';state.hidden=!state.textContent;
 $('message-form').setAttribute('aria-busy',String(busy));
}
function renderComposerAttachments(){
 const holder=$('attachment-drafts');holder.replaceChildren();
 const d=currentRoom&&currentUser?composerDraft():null;
 holder.hidden=!d?.items.length;
 for(const item of d?.items||[]){
  const card=el('div','attachment-draft'+(item.url?' image-draft':' file-draft'));
  if(item.url){const img=el('img','attachment-preview');img.src=item.url;img.alt=item.file.name;img.addEventListener('error',()=>{img.hidden=true;card.classList.add('preview-unavailable');});card.append(img);}
  else card.append(el('span','attachment-file-icon','▤'));
  const name=el('span','attachment-name',item.file.name);name.title=item.file.name;
  card.append(name,el('small','attachment-size',byteText(item.file.size)));
  const remove=el('button','attachment-remove','×');remove.type='button';remove.disabled=d.busy;remove.setAttribute('aria-label',item.file.name+' 첨부 취소');
  remove.addEventListener('click',()=>{if(d.busy||!composerIsCurrent(d))return;releaseComposerItem(item);d.items=d.items.filter(v=>v!==item);d.pendingText=null;d.error='';renderComposerAttachments();});
  card.append(remove);holder.append(card);
 }
 syncComposer();
}
function stageComposerFiles(files,roomID){
 if(!currentUser||!currentRoom||currentRoom.id!==roomID||!['chat','direct','ai'].includes(currentRoom.kind)||currentRoom.can_send===false)return;
 const d=composerDraft();if(d.busy)return;
 const ai=currentRoom.kind==='ai';
 for(const file of files){
  if(d.items.length>=4){toast('한 번에 파일 4개까지 첨부할 수 있습니다.');break;}
  if(file.size>100*1024*1024||(ai&&d.items.reduce((n,v)=>n+v.file.size,0)+file.size>20*1024*1024)){toast(ai?'AI 첨부는 합계 20MB까지입니다.':'파일은 100MB까지 첨부할 수 있습니다.');continue;}
  if(d.items.some(v=>v.file.name===file.name&&v.file.size===file.size&&v.file.lastModified===file.lastModified))continue;
  const image=/^image\/(png|jpeg|gif|webp)$/.test(file.type)||(!file.type&&/\.(png|jpe?g|gif|webp)$/i.test(file.name));
  d.pendingText=null;d.items.push({file,id:crypto.randomUUID(),url:image?URL.createObjectURL(file):'',uploaded:false});
 }
 d.error='';renderComposerAttachments();
}
$('attach-button').addEventListener('click',()=>{if(!currentRoom||$('attach-button').disabled)return;composerPicker={room:currentRoom.id,owner:currentUser.id};$('file-input').value='';$('file-input').click();});
$('file-input').addEventListener('change',()=>{
 const files=Array.from($('file-input').files||[]),picker=composerPicker;composerPicker=null;$('file-input').value='';
 if(picker?.owner!==currentUser?.id||picker?.room!==currentRoom?.id){if(files.length)toast('대화방이 바뀌어 첨부 선택을 취소했습니다.');return;}
 stageComposerFiles(files,picker.room);
});
$('message-text').addEventListener('input',syncComposer);
async function submitComposer(e){
 e?.preventDefault();if(!composerCanSend())return;
 const room=currentRoom,d=composerDraft(),raw=$('message-text').value,epoch=composerEpoch;
 if(d.busy||(!raw.trim()&&!d.items.length))return;
 const items=[...d.items],text=raw.trim()||(room.kind==='ai'?'첨부한 파일을 확인하고 내용을 설명해 주세요.':'');
 if(text&&d.pendingText?.text!==text)d.pendingText={text,id:crypto.randomUUID()};
 const request=d.pendingText;d.busy=true;d.error='';d.controller=new AbortController();renderComposerAttachments();
 const live=()=>composerEpoch===epoch&&currentUser?.id===d.owner&&composerDrafts.get(room.id)===d;
 let complete=false;
 try{
  for(const item of items){
   if(!live())return;if(item.uploaded)continue;
   if(composerIsCurrent(d)){d.error=item.file.name+' · 업로드 중…';syncComposer();}
   const data=new FormData();data.append('file',item.file,item.file.name);
   await api('/rooms/'+room.id+'/files','POST',data,{'X-Client-ID':item.id},d.controller.signal);
   item.uploaded=true;
  }
  if(!live())return;
  if(text){
   if(composerIsCurrent(d)){d.error='메시지 전송 중…';syncComposer();}
   const result=await api('/rooms/'+room.id+'/messages','POST',{text,client_id:request.id},{},d.controller.signal);
   if(!live())return;
   if(room.kind==='ai'&&composerIsCurrent(d)){
    if(result?.agent_job)trackAgentJob(result.agent_job);else if(result?.request)trackAIRequest(result.request);
   }
  }
  complete=true;d.pendingText=null;d.error='';
  if(drafts.get(room.id)===raw)drafts.delete(room.id);
  if(composerIsCurrent(d)&&$('message-text').value===raw){$('message-text').value='';drafts.delete(room.id);sizeComposer();}
 }catch(err){
  if(!live())return;
  const sent=items.filter(v=>v.uploaded).length;
  d.error=(sent?sent+'개 첨부는 전송되었습니다. ':'')+'전송 실패 · '+err.message+' 다시 전송하면 남은 항목을 재시도합니다.';
  if(!raw.trim()&&text){drafts.set(room.id,text);if(composerIsCurrent(d))$('message-text').value=text;}
 }finally{
  const remove=complete?items:items.filter(v=>v.uploaded);
  for(const item of remove){releaseComposerItem(item);d.items=d.items.filter(v=>v!==item);}
  d.busy=false;d.controller=null;
  if(live()){
   if(composerIsCurrent(d)){renderComposerAttachments();sizeComposer();await reloadMessages(true).catch(()=>{});}
   refreshRooms().catch(()=>{});
  }
 }
}
$('message-form').addEventListener('submit',submitComposer);
renderComposerAttachments();
