'use strict';
let autoHelpView=null,autoHelpSequence=0;
const autoHelpPanel=el('section','auto-help-panel');autoHelpPanel.hidden=true;
$('messages').before(autoHelpPanel);
function resetAutoHelp(){autoHelpSequence++;autoHelpView=null;autoHelpPanel.replaceChildren();autoHelpPanel.hidden=true;$('conversation-help-check').checked=false;}
async function syncAutoHelp(){
 const room=currentRoom,user=currentUser;if(!room||!user||!['direct','chat'].includes(room.kind)){resetAutoHelp();return;}
 const serial=++autoHelpSequence,key=user.id+'/'+room.id;
 if(autoHelpView?.key!==key){autoHelpPanel.replaceChildren();autoHelpPanel.hidden=true;autoHelpView={key};}
 try{
  const data=await api('/rooms/'+room.id+'/ai-help');if(serial!==autoHelpSequence||currentRoom?.id!==room.id||currentUser?.id!==user.id)return;
  $('conversation-help-check').checked=data.enabled;autoHelpPanel.hidden=!data.enabled||document.hidden;
  const signature=JSON.stringify(data);if(autoHelpView.signature===signature)return;autoHelpView={key,signature,result:data.result};
  autoHelpPanel.replaceChildren();if(!data.enabled)return;
  const title=el('div','auto-help-title');title.append(el('b','','AI 자동 도움'),el('small','','최근 텍스트 최대 50개 · 나에게만 표시'));autoHelpPanel.append(title);
  if(!data.result){autoHelpPanel.append(el('p','',data.status==='empty'?'분석할 대화를 기다리고 있습니다.':data.status==='error'?data.error:data.status==='running'?'새 대화를 분석하고 있습니다…':'대화가 바뀌면 자동으로 분석합니다…'));return;}
  const result=data.result,details=el('details');details.open=true;details.append(el('summary','','요약·할 일·답장 제안 ('+result.count+'개 메시지 분석)'));
  const answer=el('div','help-answer',result.text);details.append(answer);
  const draft=result.text.split('[답장 초안]')[1]?.trim();
  if(draft)details.append(action('답장 초안 입력',async()=>{
   await api('/ai/context/check','POST',{result_id:result.result_id});if(currentRoom?.id!==room.id||currentUser?.id!==user.id||autoHelpView?.result!==result)return;
   const input=$('message-text'),combined=(input.value?input.value+'\n':'')+draft;
   if(combined.length>16000)throw new Error('입력 가능한 길이를 초과했습니다.');
   input.value=combined;drafts.set(room.id,combined);sizeComposer();input.focus();
  },'secondary'));
  const sources=el('details','help-sources');sources.append(el('summary','','근거 메시지 확인'));
  for(const source of result.sources||[]){sources.append(action('#'+source.id+' · '+source.sender+' · '+dateText(source.created_at),async()=>{
   await api('/ai/context/check','POST',{result_id:result.result_id});
   const data=await api('/ai/context/messages/'+room.id+'/'+source.id);
   if(currentRoom?.id!==room.id||currentUser?.id!==user.id)return;
   const target=$('messages').querySelector('[data-id="'+source.id+'"]');
   if(target){target.scrollIntoView({block:'center'});target.classList.add('help-highlight');setTimeout(()=>target.classList.remove('help-highlight'),4000);}
   else {const body=dialog('근거 메시지 #'+source.id);body.append(el('p','',source.sender+' · '+dateText(source.created_at)),el('p','help-answer',data.message?.text||data.text||source.text));}
  },'text-button'));}
  details.append(sources);autoHelpPanel.append(details);
  if(result.truncated)autoHelpPanel.append(el('small','','최근 일부 대화만 분석한 결과입니다.'));
 }catch(e){if(serial===autoHelpSequence){autoHelpView=null;autoHelpPanel.replaceChildren();autoHelpPanel.hidden=true;if(e.status===403)$('conversation-help-check').checked=false;}}
}
$('conversation-help-check').addEventListener('change',async()=>{
 const room=currentRoom,user=currentUser,check=$('conversation-help-check'),enabled=check.checked;if(!room||!user)return;check.disabled=true;
 autoHelpPanel.hidden=true;autoHelpSequence++;
 try{await api('/rooms/'+room.id+'/ai-help','PUT',{enabled,timezone:Intl.DateTimeFormat().resolvedOptions().timeZone||'Asia/Seoul'});await syncAutoHelp();}
 catch(e){toast(e.message);await syncAutoHelp();}finally{check.disabled=false;}
});
setInterval(()=>{if(!document.hidden&&currentUser)syncAutoHelp();},10000);
document.addEventListener('visibilitychange',()=>{if(document.hidden){autoHelpPanel.hidden=true;}else syncAutoHelp();});
