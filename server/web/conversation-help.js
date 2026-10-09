'use strict';
// 선택 대화는 이 창의 메모리에만 유지합니다. 닫기/세션 종료/검증 실패 시 폐기합니다.
let conversationHelpState=null;
const helpRoot=el('dialog','conversation-help-dialog');helpRoot.id='conversation-help-dialog';
const helpHead=el('div','dialog-head'),helpTitle=el('h2','','대화로 AI에게 도움받기');
helpHead.append(helpTitle,action('닫기',()=>helpRoot.close(),'text-button'));helpRoot.append(helpHead);
const helpBody=el('div','conversation-help-body');helpRoot.append(helpBody);document.body.append(helpRoot);
helpRoot.addEventListener('close',()=>{if(!helpRoot.open)disposeConversationHelp();});
function disposeConversationHelp(){const s=conversationHelpState;conversationHelpState=null;if(s){clearInterval(s.timer);s.controller.abort();s.preview=s.result=null;}helpBody.replaceChildren();}
function closeConversationHelp(){if(helpRoot.open)helpRoot.close();disposeConversationHelp();}
function helpActive(s){return conversationHelpState===s&&currentUser?.id===s.user&&helpRoot.open;}
async function helpAPI(s,path,method='GET',data){
 const response=await fetch('/api/ai/context'+path,{method,credentials:'same-origin',cache:'no-store',signal:s.controller.signal,headers:{'X-OnTech-Request':'1','Content-Type':'application/json'},...(data===undefined?{}:{body:JSON.stringify(data)})});
 const body=await response.json();if(!helpActive(s))throw new DOMException('취소됨','AbortError');
 if(!response.ok){if(response.status===401&&body.code==='SESSION_INVALID')resetSession();const e=new Error(body.error||'대화 요청에 실패했습니다.');e.status=response.status;throw e;}return body;
}
function helpStatus(s,text,error=false){if(!helpActive(s))return;s.status.textContent=text;s.status.classList.toggle('error',error);}
function helpInvalidate(s,text){if(!helpActive(s))return;s.revision=(s.revision||0)+1;s.preview=s.result=null;s.previewBox.replaceChildren();s.resultBox.replaceChildren();s.analyze.disabled=true;s.status.textContent=text||'대화가 삭제되었거나 접근 권한이 변경되었습니다. 범위를 다시 확인하세요.';}
async function checkConversationHelp(){
 const s=conversationHelpState;if(!s||s.checking||!helpActive(s)||(!s.preview&&!s.result))return;
 const revision=s.revision;s.checking=true;s.previewBox.hidden=s.resultBox.hidden=true;
 try{await helpAPI(s,'/check','POST',s.result?{result_id:s.result.result_id}:{preview_token:s.preview.preview_token});}
 catch(e){if(e.name!=='AbortError'&&s.revision===revision)helpInvalidate(s,e.message);}finally{if(helpActive(s)){s.checking=false;s.previewBox.hidden=s.resultBox.hidden=document.hidden;}}
}
document.addEventListener('visibilitychange',()=>{if(document.hidden){const s=conversationHelpState;if(s?.previewBox)s.previewBox.hidden=s.resultBox.hidden=true;}else checkConversationHelp();});
window.addEventListener('focus',()=>checkConversationHelp());
// The chat header checkbox controls automatic assistance; manual selection remains in AI chat.
$('ai-context-button').addEventListener('click',()=>openConversationHelp());
async function openConversationHelp(initialRoom=''){
 closeConversationHelp();if(!currentUser)return;
 const s={user:currentUser.id,aiRoom:currentRoom?.kind==='ai'?currentRoom.id:'',controller:new AbortController(),revision:0,preview:null,result:null,checking:false,busy:false,friendCursor:'',roomCursor:'',friends:[],rooms:[]};
 conversationHelpState=s;helpRoot.showModal();s.status=el('p','help-status','친구와 대화 목록을 불러오는 중…');s.status.setAttribute('role','status');
 helpBody.append(el('p','dialog-note','참여 중인 대화의 텍스트만 선택해 기존 AI 제공자에 전달합니다. 첨부파일은 제외하고 답장은 자동 전송하지 않습니다. 미리보기·결과는 최대 15분 제공되며, 창을 닫으면 화면의 내용을 지웁니다.'),s.status);
 if(!aiCatalog.enabled){helpStatus(s,'AI 대화가 비활성화되어 있습니다. 서버의 AI 설정을 확인하세요.',true);return;}
 const selectors=el('div','help-selectors');s.friend=helpSelect(selectors,'친구','모든 친구');s.room=helpSelect(selectors,'대화방','대화방을 선택하세요');
 s.moreFriends=action('친구 더 보기',()=>helpLoadFriends(s),'text-button');s.moreRooms=action('대화 더 보기',()=>helpLoadRooms(s),'text-button');selectors.append(s.moreFriends,s.moreRooms);
 const range=el('div','help-selectors');s.limit=helpSelect(range,'최근 메시지 수');for(const n of [20,50,100,200]){const o=el('option','',n+'개');o.value=n;s.limit.append(o);}s.limit.value='50';
 const zone=Intl.DateTimeFormat().resolvedOptions().timeZone||'Asia/Seoul';s.timezone=zone;
 const from=field('시작 날짜 (선택)','date'),to=field('종료 날짜 (선택)','date');s.from=from[1];s.to=to[1];s.from.required=s.to.required=false;range.append(from[0],to[0]);
 s.previewButton=action('선택 범위 미리보기',()=>helpPreview(s),'secondary wide');s.previewBox=el('section','help-preview');
 const modes=el('div','help-selectors');s.mode=helpSelect(modes,'도움 종류');for(const [value,title] of [['summary','대화 요약'],['tasks','약속·할 일'],['reply','답장 초안'],['question','직접 질문']]){const o=el('option','',title);o.value=value;s.mode.append(o);}
 const questionLabel=el('label','','질문 / 답장에 원하는 내용 (선택)');s.question=el('textarea');s.question.rows=2;s.question.maxLength=2000;questionLabel.append(s.question);
 s.analyze=action('미리본 대화를 AI에 전달하고 분석',()=>helpAnalyze(s),'primary wide');s.analyze.disabled=true;s.resultBox=el('section','help-result');
 helpBody.append(selectors,range,el('p','dialog-note','날짜는 '+zone+' 기준이며, 해당 범위의 최근 메시지를 조회합니다. 최대 200개·96KiB까지 분석합니다.'),s.previewButton,s.previewBox,modes,questionLabel,s.analyze,s.resultBox);
 const changed=()=>helpInvalidate(s,'범위가 변경되었습니다. 미리보기를 다시 확인하세요.');
 for(const input of [s.limit,s.from,s.to,s.room])input.addEventListener('change',changed);
 s.friend.addEventListener('change',async()=>{changed();s.rooms=[];s.room.replaceChildren(el('option','','대화방을 선택하세요'));s.room.firstChild.value='';s.roomCursor='';await helpLoadRooms(s).catch(e=>helpStatus(s,e.message,true));});
 try{await Promise.all([helpLoadFriends(s),helpLoadRooms(s)]);if(initialRoom){if(!s.rooms.some(r=>r.id===initialRoom)){const room=roomList.find(r=>r.id===initialRoom);if(room&&isChat(room)){s.rooms.push(room);const o=el('option','',room.name);o.value=room.id;s.room.append(o);}}s.room.value=initialRoom;}helpStatus(s,s.rooms.length?'대화와 범위를 선택한 뒤 미리보기를 확인하세요.':'선택할 대화가 없습니다. 친구와 대화를 시작한 뒤 다시 열어 주세요.');}
 catch(e){if(e.name!=='AbortError')helpStatus(s,e.message,true);}
 if(helpActive(s))s.timer=setInterval(checkConversationHelp,7000);
}
function helpSelect(parent,title,placeholder){const label=el('label','',title),select=el('select');if(placeholder){const o=el('option','',placeholder);o.value='';select.append(o);}label.append(select);parent.append(label);return select;}
async function helpLoadFriends(s){s.moreFriends.disabled=true;try{const data=await helpAPI(s,'/friends?limit=50&after='+encodeURIComponent(s.friendCursor));for(const f of data.items||[]){s.friends.push(f);const o=el('option','',f.display_name);o.value=f.id;s.friend.append(o);}s.friendCursor=data.next_cursor||'';s.moreFriends.hidden=!s.friendCursor;if(!s.friends.length)s.friend.firstChild.textContent='등록된 친구 없음';}finally{s.moreFriends.disabled=false;}}
async function helpLoadRooms(s){s.moreRooms.disabled=true;const friend=s.friend.value;try{const data=await helpAPI(s,'/conversations?limit=50&after='+encodeURIComponent(s.roomCursor)+'&friend_id='+encodeURIComponent(friend));if(s.friend.value!==friend)return;for(const r of data.items||[]){s.rooms.push(r);const o=el('option','',r.name+(r.kind==='direct'?' · 1:1':' · 그룹'));o.value=r.id;s.room.append(o);}s.roomCursor=data.next_cursor||'';s.moreRooms.hidden=!s.roomCursor;}finally{s.moreRooms.disabled=false;}}
function helpDate(value,end=false){if(!value)return undefined;const day=new Date(value+'T00:00:00');if(end){day.setDate(day.getDate()+1);day.setMilliseconds(day.getMilliseconds()-1);}return Math.floor(day.getTime()/1000);}
function helpBusy(s,busy){s.busy=busy;for(const input of [s.friend,s.room,s.limit,s.from,s.to,s.mode,s.question,s.previewButton])input.disabled=busy;s.analyze.disabled=busy||!s.preview;}
function helpMessageRow(message,timezone){const row=el('article','help-message');row.append(el('b','',message.sender+' · #'+message.id),el('time','',new Date(message.created_at*1000).toLocaleString('ko-KR',{timeZone:timezone})),el('p','',message.text));return row;}
function helpRangeText(data){const messages=data.messages||data.sources||[];const first=messages[0],last=messages.at(-1);return '실제 분석: '+data.count+'개'+(data.total_matching!==undefined?' / 범위 내 '+data.total_matching+'개':'')+(data.truncated?' · 길이/개수 제한으로 일부만 포함':'')+(first?' · #'+first.id+'~#'+last.id+' · '+new Date(first.created_at*1000).toLocaleString('ko-KR',{timeZone:data.timezone})+' ~ '+new Date(last.created_at*1000).toLocaleString('ko-KR',{timeZone:data.timezone}):'')+' · '+data.timezone;}
async function helpPreview(s){
 if(s.busy)return;if(!s.room.value){helpStatus(s,'대화방을 선택하세요.',true);return;}
 const from=helpDate(s.from.value),to=helpDate(s.to.value,true);if(from&&to&&from>to){helpStatus(s,'종료 날짜는 시작 날짜 이후여야 합니다.',true);return;}
 helpInvalidate(s,'선택한 대화를 불러오는 중…');const revision=s.revision;helpBusy(s,true);
 try{const data=await helpAPI(s,'/preview','POST',{room_id:s.room.value,limit:Number(s.limit.value),from,to,timezone:s.timezone});if(s.revision!==revision)return;s.preview=data;s.previewBox.append(el('h3','',data.room_name+' · 전달할 대화'),el('p','dialog-note',helpRangeText(data)));
 const list=el('div','help-messages');for(const m of data.messages||[])list.append(helpMessageRow(m,data.timezone));s.previewBox.append(list);
 if(!data.count){s.preview=null;helpStatus(s,'선택 범위에 분석할 텍스트 메시지가 없습니다. 날짜나 개수를 변경하세요.');}else helpStatus(s,'아래 대화만 AI에 전달됩니다. 내용을 확인한 뒤 분석을 실행하세요.');}
 catch(e){if(e.name!=='AbortError')helpStatus(s,e.message,true);}finally{if(helpActive(s))helpBusy(s,false);}
}
async function helpAnalyze(s){
 if(s.busy||!s.preview)return;if(s.mode.value==='question'&&!s.question.value.trim()){helpStatus(s,'대화에 관해 궁금한 질문을 입력하세요.',true);return;}
 const revision=s.revision;helpBusy(s,true);s.result=null;s.resultBox.replaceChildren();helpStatus(s,'선택한 범위를 분석하는 중… 창을 닫으면 요청을 취소합니다.');
 try{const data=await helpAPI(s,'/analyze','POST',{preview_token:s.preview.preview_token,action:s.mode.value,question:s.question.value.trim(),ai_room_id:s.aiRoom||undefined});
 if(s.revision!==revision)return;await helpAPI(s,'/check','POST',{result_id:data.result_id});if(s.revision!==revision)return;s.result=data;helpRenderResult(s);helpStatus(s,'분석이 완료되었습니다. 원문에 근거한 내용과 AI의 추론을 구분해 확인하세요.');}
 catch(e){if(e.name!=='AbortError'){if(s.revision!==revision)return;if([403,404,409,410].includes(e.status))helpInvalidate(s,e.message);else helpStatus(s,e.message+' 다시 시도할 수 있습니다.',true);}}
 finally{if(helpActive(s))helpBusy(s,false);}
}
function helpRenderResult(s){
 const data=s.result;s.resultBox.append(el('h3','','AI 분석 결과'),el('p','dialog-note',helpRangeText(data)));const answer=el('div','help-answer');helpRenderAnswer(answer,data.text||'',data.sources||[],s);s.resultBox.append(answer);
 if(data.action==='reply'){const buttons=el('div','help-actions');buttons.append(action('답장 초안 복사',()=>helpUseReply(s,false).catch(e=>helpStatus(s,e.message,true))),action('대화 입력창에 넣기',()=>helpUseReply(s,true).catch(e=>helpStatus(s,e.message,true)),'primary'));s.resultBox.append(buttons,el('p','dialog-note','입력창에 넣은 뒤 직접 확인하고 전송하세요.'));}
 const sources=el('details','help-sources');sources.append(el('summary','', '근거 메시지 확인 ('+(data.sources||[]).length+'개)'));
 for(const m of data.sources||[]){const row=helpMessageRow(m,data.timezone);row.append(action('원문 위치로 이동',()=>helpJump(s,m.id).catch(e=>helpStatus(s,e.message,true)),'text-button'));sources.append(row);}s.resultBox.append(sources);s.resultBox.scrollIntoView({block:'start'});
}
function helpRenderAnswer(node,text,sources,s){let offset=0;const ids=new Set(sources.map(m=>Number(m.id)));for(const match of text.matchAll(/\[메시지\s*#(\d+)\]/g)){node.append(document.createTextNode(text.slice(offset,match.index)));const id=Number(match[1]);node.append(ids.has(id)?action(match[0],()=>helpJump(s,id).catch(e=>helpStatus(s,e.message,true)),'help-citation'):document.createTextNode(match[0]));offset=match.index+match[0].length;}node.append(document.createTextNode(text.slice(offset)));}
async function helpValidatedResult(s){
 const result=s.result,revision=s.revision;if(!result)throw new DOMException('취소됨','AbortError');
 try{await helpAPI(s,'/check','POST',{result_id:result.result_id});}catch(e){if(e.name!=='AbortError'&&s.revision===revision)helpInvalidate(s,e.message);throw e;}
 if(!helpActive(s)||s.revision!==revision||s.result!==result)throw new DOMException('취소됨','AbortError');return result;
}
async function helpUseReply(s,insert){
 if(!s.result)return;const result=await helpValidatedResult(s),text=result.text,roomID=result.room_id;
 if(!insert){try{await navigator.clipboard.writeText(text);helpStatus(s,'답장 초안을 복사했습니다.');}catch{const copy=el('textarea');copy.value=text;s.resultBox.append(copy);copy.focus();copy.select();helpStatus(s,'복사가 지원되지 않습니다. 선택된 초안을 길게 눌러 복사하세요.');}return;}
 await refreshRooms();if(!helpActive(s)||s.result!==result)return;await helpValidatedResult(s);const room=roomList.find(r=>r.id===roomID);if(!room||room.can_send===false)throw new Error('이 대화에 메시지를 입력할 수 없습니다.');
 const existing=currentRoom?.id===roomID?$('message-text').value:(drafts.get(roomID)||'');const combined=existing?existing+'\n'+text:text;if(combined.length>16000)throw new Error('기존 입력 내용과 초안이 16,000자를 넘습니다. 초안을 복사해 나누어 입력하세요.');
 closeConversationHelp();switchTab('chats',false);await selectRoom(room);if(currentRoom?.id!==roomID)return;$('message-text').value=combined;drafts.set(roomID,combined);sizeComposer();$('message-text').focus();toast('초안을 입력했습니다. 내용을 확인한 뒤 직접 전송하세요.');
}
async function helpJump(s,id){
 if(!s.result)return;const result=await helpValidatedResult(s),roomID=result.room_id;try{await helpAPI(s,'/messages/'+encodeURIComponent(roomID)+'/'+id);}catch(e){if(e.name!=='AbortError')helpInvalidate(s,e.message);throw e;}await refreshRooms();if(!helpActive(s)||s.result!==result)return;await helpValidatedResult(s);
 const room=roomList.find(r=>r.id===roomID);if(!room)throw new Error('대화에 접근할 수 없습니다.');closeConversationHelp();switchTab('chats',false);await selectRoom(room);
 const [before,after]=await Promise.all([api('/rooms/'+encodeURIComponent(roomID)+'/messages?before='+(id+1)+'&limit=50'),api('/rooms/'+encodeURIComponent(roomID)+'/messages?after='+id+'&limit=50')]);if(currentRoom?.id!==roomID)return;
 const cutoff=Math.max(before.cleared_through||0,after.cleared_through||0),rows=[...(before.messages||[]),...(after.messages||[])].filter(m=>m.id>cutoff),found=rows.find(m=>m.id===id&&m.kind==='text');if(!found){toast('원문이 삭제되었거나 더 이상 볼 수 없습니다.');return;}
 displayedMessages=[...new Map(rows.map(m=>[m.id,m])).values()].sort((a,b)=>a.id-b.id);pageHasMore=!!before.has_more;renderTimeline();const target=$('messages').querySelector('[data-id="'+id+'"]');if(target){target.classList.add('help-highlight');target.scrollIntoView({block:'center'});setTimeout(()=>target.classList.remove('help-highlight'),4000);}
}
// Android는 네이티브 레이아웃이 시스템/키보드 inset을 이미 적용하므로 CSS에서 중복 추가하지 않습니다.
if(/OnTechChatAndroid\//.test(navigator.userAgent))document.documentElement.classList.add('ontech-native');

