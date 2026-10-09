'use strict';
let friendList=[], currentTab='friends', syncTimer=null, friendsSerial=0, roomsSerial=0, messengerEpoch=0;
let displayedMessages=[], loadingOlder=false, pageHasMore=false, readSending=0, lastReadSent=0;
const drafts=new Map();
let aiCatalog={enabled:false,default_model_id:'',models:[]},aiFriends=[],activeAIRequest=null,aiPollTimer=null;
let activeAgentJob=null,agentPollTimer=null;
$('notification-options-button').addEventListener('click',()=>{
 if(/OnTechChatAndroid\/1\.[1-9]/.test(navigator.userAgent)) location.href='ontechchat://notification-settings';
 else toast('Android 앱 1.1.0 이상에서 메뉴 → 알림 미리보기·소리를 열어 주세요.');
});
const isChat=r=>!!r&&(r.kind==='chat'||r.kind==='direct');
const shownName=u=>u?.display_name||u?.username||'사용자';
const roleLabel=role=>role==='superadmin'?'최고관리자':role==='admin'?'관리자':'';
const roomTime=t=>{const d=new Date(t*1000),today=new Date();return d.toDateString()===today.toDateString()?d.toLocaleTimeString('ko-KR',{hour:'numeric',minute:'2-digit'}):d.toLocaleDateString('ko-KR',{month:'numeric',day:'numeric'});};
function avatarNode(user,size='',kind=''){
 const color=['mint','rose','violet','gold','slate','blue'].includes(user?.avatar_color)?user.avatar_color:'blue';
 const node=el('span','avatar '+color+' '+size+' '+kind);node.setAttribute('aria-hidden','true');
 node.textContent=kind==='auth'?'#':kind==='group'?'☷':kind==='ai'?'AI':Array.from(shownName(user))[0];
 if(user?.avatar_revision&&/^[a-zA-Z0-9_-]+$/.test(user.id)&&/^[a-f0-9]{16}$/.test(user.avatar_revision)){
  const img=el('img');img.src='/api/users/'+encodeURIComponent(user.id)+'/avatar?v='+user.avatar_revision;img.alt='';img.loading='lazy';img.addEventListener('load',()=>node.replaceChildren(img));
 }
 return node;
}
function resetMessenger(){if(typeof clearComposerDrafts==='function')clearComposerDrafts();if(typeof resetAutoHelp==='function')resetAutoHelp();if(typeof closeConversationHelp==='function')closeConversationHelp();messengerEpoch++;friendList=[];aiFriends=[];friendsSerial++;roomsSerial++;clearTimeout(syncTimer);clearTimeout(aiPollTimer);clearTimeout(agentPollTimer);aiPollTimer=agentPollTimer=null;activeAIRequest=activeAgentJob=null;aiCatalog={enabled:false,default_model_id:'',models:[]};displayedMessages=[];drafts.clear();currentTab='friends';$('workspace').classList.remove('chat-open');$('friend-list').replaceChildren();$('my-profile').replaceChildren();$('settings-profile').replaceChildren();$('list-search').value='';$('ai-controls').hidden=true;lastReadSent=readSending=0;}
async function enter(user){
 currentUser=user;$('login-screen').hidden=true;$('workspace').hidden=false;$('account-name').textContent=user.username;$('admin-button').hidden=!['admin','superadmin'].includes(user.role);$('settings-button').hidden=user.role!=='superadmin';
 switchTab('friends',false);connectEvents();await Promise.all([refreshFriends(),refreshRooms(),refreshAICatalog(),refreshAIFriends()]);
 const query=new URLSearchParams(location.search),room=roomList.find(r=>r.id===query.get('room'));if(room)await selectRoom(room,false);
 if(user.role==='superadmin'&&query.get('settings')==='1')await showSettings();
}
function setConnection(text,ok){$('connection').textContent=text;$('connection').classList.toggle('connected',!!ok);}
function scheduleSync(){clearTimeout(syncTimer);syncTimer=setTimeout(async()=>{if(!currentUser)return;try{await Promise.all([refreshFriends(),refreshRooms(),refreshAICatalog(),refreshAIFriends()]);if(currentRoom&&!document.hidden)await reloadMessages();}catch(e){toast(e.message);}},180);}
function connectEvents(){if(stream)stream.close();stream=new EventSource('/api/events');
 stream.addEventListener('ready',()=>{setConnection('실시간 연결됨',true);scheduleSync();});
 stream.addEventListener('sync',()=>{if(typeof checkConversationHelp==='function')checkConversationHelp();scheduleSync();});
 stream.addEventListener('heartbeat',()=>{if(!document.hidden)scheduleSync();});
 stream.addEventListener('logout',()=>{resetSession();toast('세션이 종료되었습니다. 다시 로그인해 주세요.');});
 stream.onerror=()=>setConnection('연결 복구 중',false);
}
async function refreshFriends(){const seq=++friendsSerial,epoch=messengerEpoch;const rows=await api('/friends');if(!currentUser||epoch!==messengerEpoch||seq!==friendsSerial)return;friendList=rows;const me=rows.find(u=>u.id===currentUser.id);if(me)currentUser={...currentUser,...me};renderFriends();renderSettingsProfile();}
async function refreshAIFriends(){try{aiFriends=await api('/ai/friends');}catch(e){aiFriends=[];if(e.status===401)throw e;}if(currentUser)renderFriends();}
async function openAIFriend(profile){if(!profile?.available){toast(profile?.unavailable_reason||'현재 AI 친구를 사용할 수 없습니다.');return;}const out=await api('/ai/friends/'+encodeURIComponent(profile.id)+'/open','POST',{});await refreshRooms();switchTab('chats',false);const room=roomList.find(r=>r.id===out.id);if(room)await selectRoom(room);}
async function refreshRooms(){const seq=++roomsSerial,epoch=messengerEpoch;const rows=await api('/rooms');if(!currentUser||epoch!==messengerEpoch||seq!==roomsSerial)return;roomList=rows;
 if(currentRoom){const next=rows.find(r=>r.id===currentRoom.id);if(!next){closeConversation(false);toast('대화 목록이 변경되었습니다.');}else{currentRoom=next;updateChatHeader();if(next.kind==='ai'){trackAIRequest(next.ai_request||null);trackAgentJob(next.agent_job||null);}}}
 renderRooms();
}
function switchTab(tab,close=true){if(!['friends','chats','preferences'].includes(tab))return;if(close&&currentRoom)closeConversation(false);currentTab=tab;
 $('tab-title').textContent={friends:'친구',chats:'채팅',preferences:'설정'}[tab];
 for(const name of ['friends','chats','preferences'])$(name+'-panel').hidden=name!==tab;
 document.querySelectorAll('.tab-button').forEach(b=>{const active=b.dataset.tab===tab;b.classList.toggle('active',active);b.setAttribute('aria-selected',String(active));});
 $('list-search').closest('label').hidden=tab==='preferences';$('new-room-button').hidden=tab==='preferences';$('list-search').value='';$('list-search').placeholder=tab==='friends'?'이름 또는 아이디 검색':'대화방 · 최근 메시지 검색';$('list-search').setAttribute('aria-label',tab==='friends'?'친구 검색':'채팅 검색');
 renderFriends();renderRooms();renderSettingsProfile();if(close&&location.search)history.replaceState({tab},'', '/');
}
function rowProfile(u,me=false){const b=el('button',me?'my-profile-card':'friend-row');b.type='button';b.append(avatarNode(u));const text=el('span','row-text');const name=el('strong','',shownName(u));if(roleLabel(u.role))name.append(el('span','role-tag',roleLabel(u.role)));if(me)name.append(el('span','my-tag','나'));text.append(name,el('small','',u.status_message||'@'+u.username));b.append(text);if(!me&&u.favorite)b.append(el('span','friend-star','★'));b.addEventListener('click',()=>showProfile(u));b.addEventListener('dblclick',()=>{if(!me)openDirect(u).catch(e=>toast(e.message));});return b;}
function renderFriends(){if(!currentUser)return;$('my-profile').replaceChildren(rowProfile(currentUser,true));const list=$('friend-list');list.replaceChildren();const q=$('list-search').value.trim().toLocaleLowerCase();const aiRows=aiFriends.filter(p=>[p.name,p.description].some(v=>(v||'').toLocaleLowerCase().includes(q)));if(aiRows.length){const label=el('div','section-label');label.append(el('span','','AI 친구'),el('span','',String(aiRows.length)));list.append(label);for(const p of aiRows){const b=el('button','friend-row ai-friend-row');b.type='button';b.disabled=false;b.append(avatarNode({display_name:p.name},'','ai'));const text=el('span','row-text');text.append(el('strong','',p.name),el('small','',p.available?(p.description||'AI 대화'):(p.unavailable_reason||'현재 사용 불가')));b.append(text);if(!p.available)b.classList.add('unavailable');b.addEventListener('click',()=>openAIFriend(p).catch(e=>toast(e.message)));list.append(b);}}const all=friendList.filter(u=>u.id!==currentUser.id&&!u.disabled);const matching=all.filter(u=>[shownName(u),u.username,u.status_message].some(v=>(v||'').toLocaleLowerCase().includes(q)));const cmp=(a,b)=>shownName(a).localeCompare(shownName(b),'ko');const favorite=matching.filter(u=>u.favorite).sort(cmp),others=matching.filter(u=>!u.favorite).sort(cmp);
 const section=(title,rows)=>{if(!rows.length)return;const label=el('div','section-label');label.append(el('span','',title),el('span','',String(rows.length)));list.append(label);rows.forEach(u=>list.append(rowProfile(u)));};
 section('즐겨찾기',favorite);section(q?'검색 결과':'친구',others);if(!matching.length)list.append(el('p','list-empty',q?'검색된 친구가 없습니다.':'등록된 친구가 없습니다.\n관리자가 사용자를 추가하면 여기에 표시됩니다.'));
}
function renderSettingsProfile(){if(currentUser)$('settings-profile').replaceChildren(rowProfile(currentUser,true));$('apk-deployment-button').hidden=currentUser?.role!=='superadmin';}
function attachRoomLongPress(button,room){
 let timer=null,startX=0,startY=0;
 const cancel=()=>{if(timer){clearTimeout(timer);timer=null;}};
 button.addEventListener('pointerdown',e=>{if(e.pointerType==='mouse'&&e.button!==0)return;button.dataset.longPressed='0';startX=e.clientX;startY=e.clientY;cancel();timer=setTimeout(()=>{timer=null;if(!button.isConnected||!currentUser)return;button.dataset.longPressed='1';showRoomActions(room);try{navigator.vibrate?.(18);}catch{}},550);});
 button.addEventListener('pointermove',e=>{if(Math.abs(e.clientX-startX)>12||Math.abs(e.clientY-startY)>12)cancel();});
 for(const name of ['pointerup','pointercancel','pointerleave','contextmenu'])button.addEventListener(name,cancel);
}
function renderRooms(){const list=$('room-list');list.replaceChildren();const q=currentTab==='chats'?$('list-search').value.trim().toLocaleLowerCase():'';const rows=roomList.filter(r=>[r.name,r.last_message,r.peer?.username].some(v=>(v||'').toLocaleLowerCase().includes(q)));$('chat-count').textContent=String(roomList.length);
 for(const room of rows){const b=el('button','room-button'+(currentRoom?.id===room.id?' active':''));b.type='button';b.setAttribute('aria-label',room.name+(room.unread?' · 읽지 않은 메시지 '+room.unread+'개':''));const avatarKind=room.kind==='auth'?'auth':room.kind==='chat'?'group':room.kind==='ai'?'ai':'';b.append(avatarNode(room.peer||{display_name:room.name},'',avatarKind));const text=el('span','row-text');const title=el('b','',room.name);if(room.kind==='ai')title.append(el('span','ai-room-badge','AI'));if(room.pinned)title.append(el('span','room-pin','  ⌖'));const emptyText=room.kind==='auth'?'나에게만 보이는 수신 메일':room.kind==='ai'?'AI에게 질문해 보세요':'대화를 시작해 보세요';text.append(title,el('small','',room.last_message||emptyText));const side=el('span','room-side');side.append(el('time','room-time',roomTime(room.updated_at||room.created_at)));if(room.unread)side.append(el('span','unread',room.unread>99?'99+':String(room.unread)));b.append(text,side);attachRoomLongPress(b,room);b.addEventListener('click',()=>{if(b.dataset.longPressed==='1'){b.dataset.longPressed='0';return;}selectRoom(room).catch(e=>toast(e.message));});b.addEventListener('contextmenu',e=>{e.preventDefault();if(b.dataset.longPressed!=='1')showRoomActions(room);});list.append(b);}
 if(!rows.length)list.append(el('p','list-empty',q?'검색된 대화가 없습니다.':'친구를 선택해 새로운 대화를 시작하세요.'));
 const total=roomList.reduce((n,r)=>n+(r.unread||0),0);$('total-unread').hidden=!total;$('total-unread').textContent=total>99?'99+':String(total);
}
async function selectRoom(room,navigate=true){if(!room||!currentUser)return;if(currentRoom)drafts.set(currentRoom.id,$('message-text').value);if(currentRoom?.id!==room.id){displayedMessages=[];lastReadSent=0;loadingSerial++;clearTimeout(aiPollTimer);clearTimeout(agentPollTimer);aiPollTimer=agentPollTimer=null;activeAIRequest=activeAgentJob=null;}
 if(typeof resetAutoHelp==='function')resetAutoHelp();currentRoom=room;$('message-text').value=drafts.get(room.id)||'';sizeComposer();$('workspace').classList.add('chat-open');$('chat-welcome').hidden=true;document.querySelector('.chat-header').hidden=false;$('messages').hidden=false;$('new-message-jump').hidden=true;updateChatHeader();renderRooms();if(typeof renderComposerAttachments==='function')renderComposerAttachments();
 if(navigate&&location.protocol.startsWith('http'))history.pushState({room:room.id},'', '/?room='+encodeURIComponent(room.id));await reloadMessages(true);if(currentRoom?.id===room.id&&room.kind==='ai'){trackAIRequest(room.ai_request||null);trackAgentJob(room.agent_job||null);}
}
function closeConversation(navigate=true){if(typeof resetAutoHelp==='function')resetAutoHelp();if(currentRoom)drafts.set(currentRoom.id,$('message-text').value);currentRoom=null;loadingSerial++;displayedMessages=[];clearTimeout(aiPollTimer);clearTimeout(agentPollTimer);aiPollTimer=agentPollTimer=null;activeAIRequest=activeAgentJob=null;$('workspace').classList.remove('chat-open');$('chat-welcome').hidden=false;document.querySelector('.chat-header').hidden=true;$('messages').hidden=true;$('messages').replaceChildren();$('message-form').hidden=true;$('auth-note').hidden=true;$('ai-controls').hidden=true;$('new-message-jump').hidden=true;if(navigate)history.replaceState({tab:'chats'},'', '/');renderRooms();}
function updateChatHeader(){const r=currentRoom;if(!r)return;const ai=r.kind==='ai';$('room-title').textContent=r.name;$('room-kind').textContent=r.kind==='direct'?'1:1 CHAT':r.kind==='auth'?'PRIVATE INBOX':ai?'AI CHAT':'GROUP CHAT';const model=ai?aiCatalog.models.find(m=>m.id===r.ai_model_id):null;$('room-description').textContent=r.kind==='auth'?'나에게만 보이는 수신 메일':r.kind==='direct'?(r.peer?.disabled?'사용 중지된 계정 · 새 메시지 전송 불가':r.peer?.status_message||'@'+(r.peer?.username||'')):ai?(model?model.name+' · '+model.provider_name:'AI 모델 설정 확인'):r.member_count+'명 참여';const avatarKind=r.kind==='auth'?'auth':r.kind==='chat'?'group':ai?'ai':'';$('header-avatar').replaceChildren(avatarNode(r.peer||{display_name:r.name},'small',avatarKind));$('message-form').hidden=ai?r.can_send===false:(!isChat(r)||r.can_send===false);$('auth-note').hidden=r.kind!=='auth';$('members-button').hidden=ai;$('conversation-help-button').hidden=!isChat(r);$('clear-ai-chat').hidden=!(ai||['direct','chat','auth'].includes(r.kind));$('clear-ai-chat').textContent=r.kind==='auth'?'내용 삭제':'대화 삭제';$('attach-button').hidden=false;$('ai-controls').hidden=!ai;$('pin-room').textContent=r.pinned?'★':'☆';$('pin-room').setAttribute('aria-pressed',String(!!r.pinned));if(ai)renderAIControls();else{activeAIRequest=null;$('message-text').disabled=false;$('send-button').disabled=false;}if(typeof syncComposer==='function')syncComposer();}
function renderTimeline(){const holder=$('messages');holder.replaceChildren();if(pageHasMore&&displayedMessages.length)holder.append(action(loadingOlder?'불러오는 중…':'이전 대화 보기',loadOlder,'secondary older'));
 if(!displayedMessages.length)holder.append(el('p','empty',currentRoom?.kind==='auth'?'아직 수신한 메일이 없습니다.':'첫 메시지로 대화를 시작해 보세요.'));
 let day='';for(const m of displayedMessages){const d=new Date(m.created_at*1000).toLocaleDateString('ko-KR',{year:'numeric',month:'long',day:'numeric',weekday:'short'});if(day!==d){holder.append(el('div','day-divider',d));day=d;}holder.append(renderMessage(m));}expireCards();}
async function reloadMessages(forceBottom=false){if(!currentRoom)return;const room=currentRoom,serial=++loadingSerial,container=$('messages');const wasBottom=forceBottom||container.scrollHeight-container.scrollTop-container.clientHeight<100;const oldTop=container.scrollTop,oldLast=displayedMessages.at(-1)?.id||0;const result=await api(`/rooms/${room.id}/messages?limit=100`);if(serial!==loadingSerial||currentRoom?.id!==room.id)return;
 const first=result.messages[0]?.id||0,older=displayedMessages.filter(m=>m.id<first&&m.id>(result.cleared_through||0));displayedMessages=[...older,...result.messages];if(!older.length)pageHasMore=result.has_more;renderTimeline();if(typeof syncAutoHelp==='function')syncAutoHelp();if(wasBottom)container.scrollTop=container.scrollHeight;else{container.scrollTop=oldTop;if((displayedMessages.at(-1)?.id||0)>oldLast)$('new-message-jump').hidden=false;}if(wasBottom)await markVisibleRead();}
async function loadOlder(){if(!currentRoom||loadingOlder||!displayedMessages.length)return;loadingOlder=true;const id=currentRoom.id,before=displayedMessages[0].id,serial=loadingSerial;try{const result=await api(`/rooms/${id}/messages?before=${before}&limit=100`);if(currentRoom?.id!==id||serial!==loadingSerial)return;const c=$('messages'),height=c.scrollHeight,top=c.scrollTop;const ids=new Set(displayedMessages.map(m=>m.id));displayedMessages=[...result.messages.filter(m=>!ids.has(m.id)),...displayedMessages].filter(m=>m.id>(result.cleared_through||0));pageHasMore=result.has_more;renderTimeline();c.scrollTop=top+c.scrollHeight-height;}catch(e){toast(e.message);}finally{loadingOlder=false;}}
async function markVisibleRead(){const r=currentRoom;if(!r||document.hidden||!displayedMessages.length||!$('workspace').classList.contains('chat-open')||$('dialog').open)return;const c=$('messages');if(c.hidden||!c.getClientRects().length)return;if(c.scrollHeight-c.scrollTop-c.clientHeight>110)return;const last=displayedMessages.at(-1).id;if(last<=Math.max(r.last_read_id||0,lastReadSent)||readSending===last)return;readSending=last;try{await api(`/rooms/${r.id}/read`,'POST',{message_id:last});if(currentRoom?.id===r.id){lastReadSent=last;r.last_read_id=last;r.unread=0;renderRooms();}}catch{}finally{if(readSending===last)readSending=0;}}
function renderMessage(m){const mine=m.user_id===currentUser?.id,aiAuthor=currentRoom?.kind==='ai'&&!m.user_id&&!!m.ai,wrap=el('article','message'+(mine?' mine':aiAuthor?' ai-message':''));wrap.dataset.id=String(m.id);const author=friendList.find(v=>v.id===m.user_id)||{username:m.username,display_name:m.display_name||m.username};if(!mine)wrap.append(avatarNode(author,'small',m.kind==='auth'?'auth':aiAuthor?'ai':''));const content=el('div','message-content');if(!mine){content.append(el('div','message-name',m.kind==='auth'?'메일 알림':aiAuthor?'AI':shownName(author)));if(aiAuthor){const meta=[m.ai.model_id,m.ai.effort&&m.ai.effort!=='provider_default'?m.ai.effort:''].filter(Boolean).join(' · ');if(meta)content.append(el('div','ai-message-meta',meta));}}const line=el('div','bubble-line');let box;
 if(m.kind==='auth'){box=mailMessageCard(m);}
 else if(m.kind==='location_request'){box=locationRequestCard(m);}
 else if(m.kind==='location'){box=locationCard(m);}
 else if(m.kind==='file'&&m.file){const f=m.file;box=el('div','bubble file-card');box.append(el('div','file-name',f.name),el('div','file-meta',byteText(f.size)));if(/^image\/(png|jpeg|gif|webp)(;|$)/.test(f.mime)){const img=el('img','thumbnail');img.alt=f.name;img.loading='lazy';img.src=`/api/files/${f.id}?inline=1`;img.addEventListener('click',()=>preview(f).catch(e=>toast(e.message)));box.append(img);}const controls=el('div','file-actions'),a=el('a','','다운로드');a.href=`/api/files/${f.id}`;a.download=f.name;controls.append(a);if(/^(image\/(png|jpeg|gif|webp)|application\/pdf|text\/plain)(;|$)/.test(f.mime))controls.append(action('미리보기',()=>preview(f)));box.append(controls);}
 else box=el('div','bubble'+(['expired','deleted'].includes(m.kind)?' deleted':''),m.kind==='deleted'?'삭제된 메시지':m.kind==='expired'?'보관 시간이 만료된 메시지':m.text);
 if(aiAuthor&&m.kind==='text')linkifyWebSources(box,m.text||'');
 const info=el('span','message-info');if(mine&&m.unread_count>0)info.append(el('span','read-count',String(m.unread_count)));info.append(el('time','',new Date(m.created_at*1000).toLocaleTimeString('ko-KR',{hour:'numeric',minute:'2-digit'})));
 if(m.kind==='text'||m.kind==='file'||m.kind==='location')info.append(action('⋯',()=>showMessageActions(m),'message-menu'));line.append(box,info);content.append(line);wrap.append(content);return wrap;}
function linkifyWebSources(node,text){
 node.replaceChildren();let start=0;
 const pattern=/https?:\/\/[^\s<>"'`]+/g;
 for(const match of text.matchAll(pattern)){
  const raw=match[0],url=raw.replace(/[)\]}>.,;!?。]+$/g,'');
  node.append(document.createTextNode(text.slice(start,match.index)));
  try{const parsed=new URL(url);if(!['http:','https:'].includes(parsed.protocol)||parsed.username||parsed.password)throw new Error('invalid URL');
   const link=el('a','',url);link.href=parsed.href;link.target='_blank';link.rel='noopener noreferrer';link.addEventListener('click',e=>e.stopPropagation());node.append(link);
   node.append(document.createTextNode(raw.slice(url.length)));
  }catch{node.append(document.createTextNode(raw));}
  start=match.index+raw.length;
 }
 node.append(document.createTextNode(text.slice(start)));
}
function showMessageActions(m){const body=dialog('메시지');if(m.kind==='text')body.append(action('텍스트 복사',async()=>{await navigator.clipboard.writeText(m.text);toast('복사했습니다.');$('dialog').close();},'secondary wide'));if(m.user_id===currentUser.id||(['chat','ai'].includes(currentRoom?.kind)&&currentRoom.owner_id===currentUser.id))body.append(action('메시지 삭제',async()=>{if(!confirm('메시지를 삭제할까요? 첨부파일도 함께 삭제됩니다.'))return;await api(`/messages/${m.id}`,'DELETE');displayedMessages=displayedMessages.map(row=>row.id===m.id?{...row,text:'',kind:'deleted',file:null}:row);renderTimeline();$('dialog').close();await reloadMessages();},'text-button danger wide'));}
async function openDirect(user){if(user.disabled||user.id===currentUser.id)return;const r=await api('/rooms/direct','POST',{user_id:user.id});if($('dialog').open)$('dialog').close();await refreshRooms();currentTab='chats';switchTab('chats',false);await selectRoom(roomList.find(v=>v.id===r.id));}
function showProfile(user){if(!currentUser)return;const me=user.id===currentUser.id,body=dialog(me?'내 프로필':'친구 프로필'),cover=el('div','profile-cover');cover.append(avatarNode(user,'large'),el('h2','',shownName(user)),el('p','',user.status_message||' '),el('div','profile-username','@'+user.username+(roleLabel(user.role)?' · '+roleLabel(user.role):'')));body.append(cover);const buttons=el('div','profile-actions');if(me)buttons.append(action('프로필 편집',editProfile,'primary'));else{buttons.append(action('1:1 대화',()=>openDirect(user),'primary'),action(user.favorite?'즐겨찾기 해제':'즐겨찾기',async()=>{await api('/friends/'+user.id,'PATCH',{favorite:!user.favorite});await refreshFriends();showProfile(friendList.find(v=>v.id===user.id)||user);},'secondary'));}buttons.append(action('이동 경로',()=>openLocationHistory(user),'secondary'));if(me)buttons.append(action('내 위치 조회 허용 설정',openHistorySettings,'secondary'));body.append(buttons);}
async function editProfile(){const me=await api('/profile');if(!currentUser)return;const body=dialog('프로필 편집'),form=el('form','profile-form');form.noValidate=true;const previewBox=el('div','profile-cover');previewBox.append(avatarNode(me,'large'));body.append(previewBox);const[nameLabel,name]=field('친구에게 보이는 이름','text',me.display_name||me.username);name.maxLength=40;name.required=false;const statusLabel=el('label','','상태메시지'),status=el('textarea');status.rows=2;status.maxLength=120;status.value=me.status_message||'';statusLabel.append(status);const colors=el('div','color-picker');let color=me.avatar_color||'blue',photo,photoURL='';
 const repaint=()=>{previewBox.replaceChildren(avatarNode({...me,avatar_color:color,display_name:name.value||me.username,avatar_revision:photo===undefined?me.avatar_revision:''},'large'));if(photoURL){const image=el('img');image.src=photoURL;image.alt='선택한 프로필 사진';const holder=el('span','avatar large');holder.append(image);previewBox.replaceChildren(holder);}colors.querySelectorAll('button').forEach(b=>{b.classList.toggle('selected',b.dataset.color===color);b.setAttribute('aria-pressed',String(b.dataset.color===color));});};
 for(const c of ['blue','mint','rose','violet','gold','slate']){const b=action('',()=>{color=c;repaint();},c);b.dataset.color=c;b.setAttribute('aria-label',({blue:'파랑',mint:'민트',rose:'분홍',violet:'보라',gold:'골드',slate:'회색'})[c]);colors.append(b);}name.addEventListener('input',repaint);
 const input=el('input');input.type='file';input.accept='image/jpeg,image/png,image/webp';input.hidden=true;let photoBusy=false;
 input.addEventListener('change',async()=>{const f=input.files[0];if(!f)return;if(f.size>10*1024*1024){toast('10MB 이하의 사진을 선택하세요.');return;}photoBusy=true;save.disabled=true;try{photo=await cropAvatar(f);const image=el('img');const b=Uint8Array.from(atob(photo),c=>c.charCodeAt(0));const url=URL.createObjectURL(new Blob([b],{type:'image/jpeg'}));temporaryURLs.push(url);photoURL=url;image.src=url;image.alt='선택한 프로필 사진';const a=el('span','avatar large');a.append(image);previewBox.replaceChildren(a);}catch{toast('사진을 읽지 못했습니다. 다른 JPEG/PNG 사진을 선택하세요.');}finally{photoBusy=false;save.disabled=false;}});
 const photoButtons=el('div','photo-controls');photoButtons.append(action('사진 선택',()=>input.click()),action('사진 삭제',()=>{photo='';photoURL='';repaint();},'text-button'));
 form.append(nameLabel,statusLabel,el('p','selection-note','프로필 배경 색상'),colors,photoButtons,input);addFormError(form);const save=el('button','primary wide','프로필 저장');save.type='submit';form.append(save);repaint();
 form.addEventListener('submit',async e=>{e.preventDefault();if(photoBusy)return;save.disabled=true;const data={display_name:name.value.trim(),status_message:status.value.trim(),avatar_color:color};if(photo!==undefined)data.avatar_image=photo;try{await api('/profile','PATCH',data);$('dialog').close();await Promise.all([refreshFriends(),refreshRooms()]);toast('프로필을 저장했습니다.');}catch(err){formError(form,err.message);}finally{save.disabled=false;}});body.append(form);}
async function cropAvatar(file){const image=await createImageBitmap(file);try{const c=document.createElement('canvas');c.width=c.height=160;const ctx=c.getContext('2d');ctx.fillStyle='#ffffff';ctx.fillRect(0,0,160,160);const s=Math.min(image.width,image.height);ctx.drawImage(image,(image.width-s)/2,(image.height-s)/2,s,s,0,0,160,160);const data=c.toDataURL('image/jpeg',.8).split(',')[1];if(data.length>60000)throw new Error('Image too large');return data;}finally{image.close();}}
function showRoomActions(room){
 if(!currentUser||!room)return;
 const body=dialog(room.name);
 body.append(action('대화방 정보',async()=>{try{if(room.kind==='ai'){const info=dialog('대화방 정보');info.append(el('p','dialog-note',room.name+' · 내 AI 대화'),el('p','', '모델: '+(room.ai_model_id||'기본 모델')));}else await showMembers(room);}catch(e){toast(e.message);}},'secondary wide'));
 const remove=action('대화방 삭제',()=>deleteListedRoom(room,remove,body),'text-button danger wide');
 remove.dataset.roomAction='delete';body.append(remove);
 body.append(action(room.pinned?'상단 고정 해제':'채팅 목록 상단 고정',async()=>{await api(`/rooms/${room.id}/preference`,'PATCH',{pinned:!room.pinned});$('dialog').close();await refreshRooms();},'secondary wide'));
 if(room.kind==='direct'&&room.peer)body.append(action('상대 프로필 보기',()=>showProfile(room.peer),'text-button wide'));
 if(room.kind==='chat')body.append(action('대화방 나가기',async()=>{if(!confirm('이 대화방에서 나갈까요? 이후 메시지와 파일에 접근할 수 없습니다. 방장이라면 다른 참여자에게 방장이 자동으로 넘어갑니다.'))return;try{const result=await api(`/rooms/${room.id}/leave`,'POST',{});$('dialog').close();if(currentRoom?.id===room.id)closeConversation();await refreshRooms();toast(result.new_owner?'대화방에서 나갔습니다. 새 방장: '+result.new_owner:'대화방에서 나갔습니다.');}catch(e){toast(e.message);}},'text-button danger wide'));
 body.append(el('p','dialog-note','대화방 삭제는 내 목록과 이전 기록에만 적용됩니다. 상대방의 대화는 유지되며 새 메시지가 오면 목록에 다시 표시됩니다.'));
}
async function deleteListedRoom(room,button,body){
 if(!currentUser||button.disabled)return;
 const viewer=currentUser.id,epoch=messengerEpoch;
 const extra=room.kind==='ai'?'진행 중인 AI 응답은 중지되고 대화 기록은 삭제됩니다. 작업공간 파일은 유지됩니다.':'상대방의 대화와 메일 원본은 삭제하지 않습니다. 새 메시지가 오면 목록에 다시 표시됩니다.';
 if(!confirm('「'+room.name+'」 대화방을 내 대화 목록에서 삭제할까요? 내 이전 기록은 복구할 수 없습니다. '+extra))return;
 button.disabled=true;
 try{
  await api('/rooms/'+encodeURIComponent(room.id),'DELETE');
  if(!currentUser||currentUser.id!==viewer||messengerEpoch!==epoch)return;
  if(body.contains(button)&&$('dialog').open)$('dialog').close();
  if(currentRoom?.id===room.id)closeConversation(false);
  drafts.delete(room.id);if(typeof clearComposerRoom==='function')clearComposerRoom(room.id);
  roomList=roomList.filter(r=>r.id!==room.id);renderRooms();
  await refreshRooms();toast('대화 목록에서 삭제했습니다.');
 }catch(e){if(currentUser?.id===viewer)toast(e.message);}finally{button.disabled=false;}
}
function showNewGroup(){const body=dialog('새 그룹 대화'),form=el('form');const[label,name]=field('채팅방 이름');name.maxLength=80;const selected=new Set(),pick=el('div','friend-select');for(const u of friendList.filter(u=>u.id!==currentUser.id&&!u.disabled)){const row=el('label','friend-check'),check=el('input');check.type='checkbox';check.setAttribute('aria-label',shownName(u));check.addEventListener('change',()=>{if(check.checked)selected.add(u.username);else selected.delete(u.username);});row.append(check,avatarNode(u,'small'),el('span','',shownName(u)));if(u.role==='admin')row.append(el('span','role-tag','관리자'));pick.append(row);}const button=el('button','primary wide','그룹 대화 시작');button.type='submit';form.append(label,el('p','dialog-note','함께 대화할 친구를 선택하세요. 초대된 친구는 이 방의 기존 대화도 볼 수 있습니다.'),pick);addFormError(form);form.append(button);form.addEventListener('submit',async e=>{e.preventDefault();if(!name.value.trim()){formError(form,'채팅방 이름을 입력하세요.');return;}button.disabled=true;try{const r=await api('/rooms','POST',{name:name.value.trim(),members:[...selected]});$('dialog').close();await refreshRooms();switchTab('chats',false);await selectRoom(roomList.find(v=>v.id===r.id));}catch(err){formError(form,err.message);}finally{button.disabled=false;}});body.append(form);}
function sizeComposer(){const t=$('message-text');t.style.height='auto';t.style.height=Math.min(t.scrollHeight,110)+'px';}
const aiModel=id=>aiCatalog.models.find(m=>m.id===id);
const aiEffortLabel=v=>v==='provider_default'?'제공자 기본값':v;
function fillAIEfforts(select,model,value){select.replaceChildren();for(const v of model?.efforts||[]){const o=el('option','',aiEffortLabel(v));o.value=v;select.append(o);}const fallback=model?.default_effort&&model.efforts?.includes(model.default_effort)?model.default_effort:'provider_default';select.value=model?.efforts?.includes(value)?value:fallback;}
async function refreshAICatalog(){
 try{aiCatalog=await api('/ai/catalog');}catch(e){aiCatalog={enabled:false,default_model_id:'',models:[]};if(e.status===401)throw e;}
 $('ai-start-button').hidden=!aiCatalog.enabled||!aiCatalog.models.length;if(currentRoom?.kind==='ai'){updateChatHeader();updateAIComposer();}
}
function renderAIControls(){
 if(currentRoom?.kind!=='ai')return;renderAIRequestState();updateAIComposer();
}
function updateAIComposer(){
 if(currentRoom?.kind!=='ai')return;const ready=aiCatalog.enabled&&!!aiModel(currentRoom.ai_model_id)&&currentRoom.can_send!==false,aiActive=!!activeAIRequest&&['queued','running'].includes(activeAIRequest.status),agentActive=!!activeAgentJob&&['queued','planning','running'].includes(activeAgentJob.status),active=aiActive||agentActive;$('message-form').hidden=!ready;$('message-text').disabled=!ready;$('send-button').disabled=!ready||active;$('message-text').placeholder='질문하거나 작업을 요청하세요';if(!ready&&$('ai-request-status'))$('ai-request-status').textContent=aiCatalog.enabled?'선택한 모델을 현재 사용할 수 없습니다.':'관리자가 AI 대화를 활성화해야 합니다.';if(typeof syncComposer==='function')syncComposer();
}
function currentAIActivity(){
 const a=activeAgentJob,q=activeAIRequest;if(!a)return q?{kind:'ai',value:q}:null;if(!q)return{kind:'agent',value:a};
 return (a.created_at||0)>=(q.created_at||0)?{kind:'agent',value:a}:{kind:'ai',value:q};
}
function renderAIRequestState(){
 if(currentRoom?.kind!=='ai')return;const status=$('ai-request-status'),activity=currentAIActivity();status.replaceChildren();
 if(!activity){$('ai-stop-button').hidden=true;$('ai-retry-button').hidden=true;updateAIComposer();return;}
 const q=activity.value,agent=activity.kind==='agent',active=agent?['queued','planning','running'].includes(q.status):['queued','running'].includes(q.status);
 const labels=agent?{queued:'대기 중',planning:'요청 판단 중…',running:'안전 작업 실행 중…',completed:'완료',failed:'실패',cancelled:'중지됨',interrupted:'서버 변경으로 중단됨'}:{queued:'대기 중',running:'답변 생성 중…',completed:'완료',failed:'실패',cancelled:'중지됨',interrupted:'서버 변경으로 중단됨'};
 status.append(document.createTextNode((labels[q.status]||q.status)+(q.error?' · '+q.error:'')));
 if(agent&&Array.isArray(q.artifact_ids)&&q.artifact_ids.length){const links=el('span','agent-artifacts');q.artifact_ids.forEach((id,i)=>{const a=el('a','',`산출물 ${i+1}`);a.href='/api/agent/artifacts/'+encodeURIComponent(id);a.download='';links.append(a);});status.append(links);}
 $('ai-stop-button').hidden=!active;$('ai-retry-button').hidden=!q||q.status==='completed'||active;updateAIComposer();
}
function trackAIRequest(q){
 if(currentRoom?.kind!=='ai'||(q&&q.room_id!==currentRoom.id))return;activeAIRequest=q||null;clearTimeout(aiPollTimer);aiPollTimer=null;renderAIRequestState();if(q&&['queued','running'].includes(q.status)){const roomID=currentRoom.id,requestID=q.id,epoch=messengerEpoch;aiPollTimer=setTimeout(()=>pollAIRequest(roomID,requestID,epoch),900);}
}
async function pollAIRequest(roomID,requestID,epoch){
 if(epoch!==messengerEpoch||currentRoom?.id!==roomID||currentRoom.kind!=='ai')return;try{const q=await api(`/rooms/${roomID}/ai/requests/${requestID}`);if(epoch!==messengerEpoch||currentRoom?.id!==roomID||activeAIRequest?.id!==requestID)return;trackAIRequest(q);if(!['queued','running'].includes(q.status)){await Promise.all([reloadMessages(true),refreshRooms()]);}}catch(e){if(activeAIRequest?.id!==requestID)return;if(e.status===404){trackAIRequest(null);await refreshRooms();return;}if(currentRoom?.id===roomID){$('ai-request-status').textContent='상태 확인 재시도 중 · '+e.message;aiPollTimer=setTimeout(()=>pollAIRequest(roomID,requestID,epoch),1600);}}
}
function showNewAI(){
 if(!aiCatalog.enabled||!aiCatalog.models.length){toast('관리자가 사용할 AI 모델을 먼저 설정해야 합니다.');return;}const body=dialog('AI 대화 시작'),form=el('form'),nameLabel=el('label','','대화 이름 · 선택'),name=el('input');name.maxLength=80;name.placeholder='예: 개발 도움';nameLabel.append(name);const modelLabel=el('label','','모델'),model=el('select'),effortLabel=el('label','','추론'),effort=el('select');for(const m of aiCatalog.models){const o=el('option','',m.name+' · '+m.provider_name);o.value=m.id;model.append(o);}model.value=aiCatalog.default_model_id&&aiModel(aiCatalog.default_model_id)?aiCatalog.default_model_id:aiCatalog.models[0].id;const sync=()=>fillAIEfforts(effort,aiModel(model.value),aiModel(model.value)?.default_effort);sync();model.addEventListener('change',sync);modelLabel.append(model);effortLabel.append(effort);const submit=el('button','primary wide','AI 대화 시작');submit.type='submit';form.append(nameLabel,modelLabel,effortLabel);addFormError(form);form.append(submit);form.addEventListener('submit',async e=>{e.preventDefault();submit.disabled=true;try{const out=await api('/rooms/ai','POST',{name:name.value.trim(),model_id:model.value,effort:effort.value});$('dialog').close();await refreshRooms();switchTab('chats',false);const room=roomList.find(r=>r.id===out.id);if(room)await selectRoom(room);}catch(err){formError(form,err.message);}finally{submit.disabled=false;}});body.append(form);
}
$('ai-start-button').addEventListener('click',showNewAI);
$('ai-stop-button').addEventListener('click',async()=>{const activity=currentAIActivity(),room=currentRoom;if(!activity||!room)return;const q=activity.value;try{if(activity.kind==='agent'){if(!['queued','planning','running'].includes(q.status))return;trackAgentJob(await api('/agent/jobs/'+encodeURIComponent(q.id)+'/cancel','POST',{}));}else{if(!['queued','running'].includes(q.status))return;trackAIRequest(await api(`/rooms/${room.id}/ai/requests/${q.id}/cancel`,'POST',{}));}await refreshRooms();}catch(e){toast(e.message);}});
$('ai-retry-button').addEventListener('click',async()=>{const activity=currentAIActivity(),room=currentRoom;if(!activity||!room)return;const q=activity.value;try{if(activity.kind==='agent')trackAgentJob(await api('/agent/jobs/'+encodeURIComponent(q.id)+'/retry','POST',{client_id:crypto.randomUUID()}));else trackAIRequest(await api(`/rooms/${room.id}/ai/requests/${q.id}/retry`,'POST',{client_id:crypto.randomUUID()}));}catch(e){toast(e.message);}});

function trackAgentJob(q){
 if(currentRoom?.kind!=='ai'||(q&&q.room_id!==currentRoom.id))return;activeAgentJob=q||null;clearTimeout(agentPollTimer);agentPollTimer=null;renderAIRequestState();
 if(q&&['queued','planning','running'].includes(q.status)){const roomID=currentRoom.id,jobID=q.id,epoch=messengerEpoch;agentPollTimer=setTimeout(()=>pollAgentJob(roomID,jobID,epoch),900);}
}
async function pollAgentJob(roomID,jobID,epoch){
 if(epoch!==messengerEpoch||currentRoom?.id!==roomID||currentRoom.kind!=='ai')return;
 try{const q=await api('/agent/jobs/'+encodeURIComponent(jobID));if(epoch!==messengerEpoch||currentRoom?.id!==roomID||activeAgentJob?.id!==jobID)return;trackAgentJob(q);if(!['queued','planning','running'].includes(q.status)){await Promise.all([reloadMessages(true),refreshRooms()]);}}
 catch(e){if(activeAgentJob?.id!==jobID)return;if(e.status===404){trackAgentJob(null);await refreshRooms();return;}if(currentRoom?.id===roomID){$('ai-request-status').textContent='상태 확인 재시도 중 · '+e.message;agentPollTimer=setTimeout(()=>pollAgentJob(roomID,jobID,epoch),1600);}}
}

$('message-text').addEventListener('input',sizeComposer);
$('list-search').addEventListener('input',()=>currentTab==='friends'?renderFriends():renderRooms());$('search-clear').addEventListener('click',()=>{$('list-search').value='';$('list-search').dispatchEvent(new Event('input'));$('list-search').focus();});
for(const b of document.querySelectorAll('.tab-button'))b.addEventListener('click',()=>switchTab(b.dataset.tab));
$('chat-back').addEventListener('click',()=>{closeConversation();switchTab('chats',false);});$('welcome-friends').addEventListener('click',()=>{switchTab('friends');$('list-search').focus();});$('edit-profile-button').addEventListener('click',()=>editProfile().catch(e=>toast(e.message)));$('auth-open-button').addEventListener('click',()=>{const r=roomList.find(r=>r.kind==='auth');if(r)selectRoom(r).catch(e=>toast(e.message));});$('chat-peer').addEventListener('click',()=>{if(currentRoom?.kind==='direct'&&currentRoom.peer)showProfile(currentRoom.peer);});$('pin-room').addEventListener('click',async()=>{if(!currentRoom)return;try{await api(`/rooms/${currentRoom.id}/preference`,'PATCH',{pinned:!currentRoom.pinned});await refreshRooms();}catch(e){toast(e.message);}});
// Replace only this UI button's legacy handler; all server settings helpers stay unchanged.
const oldNew=$('new-room-button'),newNew=oldNew.cloneNode(true);oldNew.replaceWith(newNew);newNew.addEventListener('click',showNewGroup);
$('messages').addEventListener('scroll',()=>{if($('messages').scrollHeight-$('messages').scrollTop-$('messages').clientHeight<100){$('new-message-jump').hidden=true;markVisibleRead();}},{passive:true});$('new-message-jump').addEventListener('click',()=>{$('messages').scrollTop=$('messages').scrollHeight;markVisibleRead();});
window.addEventListener('popstate',()=>{if(!currentUser)return;const id=new URLSearchParams(location.search).get('room'),r=roomList.find(v=>v.id===id);if(r)selectRoom(r,false).catch(e=>toast(e.message));else{closeConversation(false);switchTab('chats',false);}});

let sessionReconnectSerial=0,sessionReconnectTimer=null;
function cancelSessionReconnect(){
 sessionReconnectSerial++;clearTimeout(sessionReconnectTimer);sessionReconnectTimer=null;
 document.getElementById('session-reconnect')?.remove();
}
async function restoreWebSession(){
 const serial=++sessionReconnectSerial;
 clearTimeout(sessionReconnectTimer);$('login-screen').hidden=true;
 try {
  const user=await api('/me');if(serial!==sessionReconnectSerial)return;
  document.getElementById('session-reconnect')?.remove();await enter(user);
 } catch(error) {
  if(serial!==sessionReconnectSerial)return;
  if(error.status===401&&error.code==='SESSION_INVALID'){resetSession();return;}
  setConnection('로그인 유지 · 연결 복구 중',false);
  if(!currentUser){
   let panel=document.getElementById('session-reconnect');
   if(!panel){panel=el('section','login-screen');panel.id='session-reconnect';
    panel.append(el('p','','서버 연결을 기다리고 있습니다. 저장된 로그인은 유지됩니다.'),
     action('지금 다시 연결',restoreWebSession,'primary'));document.body.append(panel);}
  }
  sessionReconnectTimer=setTimeout(restoreWebSession,5000);
 }
}
restoreWebSession();

// Native actions require a direct click. The app independently checks the current origin, session and recipient.
function nativeFeature(path){
 const app=navigator.userAgent.match(/OnTechChatAndroid\/(\d+)\.(\d+)\.(\d+)/);
 if(!app||+app[1]<1||(+app[1]===1&&+app[2]<(path.startsWith('answer-location')?3:2))){toast('이 기능은 Android 온테크 채팅 1.3.0 이상에서 사용할 수 있습니다. 먼저 새 APK를 설치하세요.');return;}
 location.href='ontechchat://'+path;
}
function locationCard(m){
 const box=el('div','bubble location-card');box.dataset.locationExpires=String(m.expires_at||0);
 let p;try{p=JSON.parse(m.text);}catch{return el('div','bubble deleted','위치 정보를 읽지 못했습니다.');}
 if(!Number.isFinite(p.latitude)||!Number.isFinite(p.longitude)||Math.abs(p.latitude)>90||Math.abs(p.longitude)>180)return el('div','bubble deleted','위치 형식 오류');
 const expired=()=>m.expires_at<=Date.now()/1000;
 if(expired())return el('div','bubble deleted','공유 위치의 보관 기간이 끝났습니다.');
 box.append(el('strong','','조회한 위치'),el('p','location-caption','위치 확인 요청에 응답한 시점의 위치입니다. 실시간 추적이 아닙니다.'),
 el('div','',p.latitude.toFixed(5)+', '+p.longitude.toFixed(5)),el('small','',new Date(p.captured_at*1000).toLocaleString('ko-KR')+' · '+(p.approximate?'대략적 위치 · ':'')+'정확도 약 '+Math.ceil(p.accuracy_m)+'m'));
 box.append(action('지도에서 보기',()=>{if(expired()){box.replaceChildren(el('span','','공유 위치가 만료되었습니다.'));return;}
 if(confirm('선택한 좌표가 외부 지도 서비스로 전달됩니다. 지도를 열까요?'))location.href='https://www.google.com/maps/search/?api=1&query='+encodeURIComponent(p.latitude+','+p.longitude);
 },'secondary'),el('small','location-expiry','서버 24시간 보관 · 보낸 사람이 삭제 가능'));
 return box;
}
setInterval(()=>{for(const box of document.querySelectorAll('[data-location-expires]'))if(+box.dataset.locationExpires<=Date.now()/1000)box.replaceChildren(el('span','','공유 위치의 보관 기간이 끝났습니다.'));},1000);

$('app-update-button').addEventListener('click',()=>nativeFeature('app-update'));
$('apk-deployment-button').addEventListener('click',async()=>{try{const info=await api('/app-updates/deployment');const b=dialog('APK 업데이트 배포');b.append(el('p','','개발 PC에서 APK와 배포 정보를 생성한 뒤 서버 PC의 아래 폴더에 게시합니다. APK만 복사하면 버전·서명을 안전하게 확인할 수 없으므로 latest.json을 함께 사용합니다.'),el('pre','deployment-path',info.folder),el('p','',info.available?'게시 버전: '+info.release.version_name+' ('+info.release.version_code+')':'아직 게시된 업데이트가 없습니다.'),el('p','',info.error||'새 파일 게시 후 앱이 버전을 확인합니다. 설치는 각 사용자의 승인 후 진행합니다.'));}catch(e){toast(e.message);}});

// 조회 상태와 좌표는 채팅 메시지 대신 독립 팝업에서만 표시합니다.
async function askFriendLocation(user){
 if(!currentUser||user.id===currentUser.id||user.disabled)return;
 const viewer=currentUser.id,body=dialog(shownName(user)+' · 현재 위치');
 const status=el('p','request-status','위치 확인 요청 중…'),result=el('div','location-lookup-result');
 body.append(action('시간대별 이동 경로',()=>openLocationHistory(user),'secondary'));
 status.setAttribute('role','status');body.append(status,result);
 let q=null,timer=null,finished=false,created=Date.now(),deadline=created+120000;
 const active=()=>$('dialog').open&&status.isConnected&&currentUser?.id===viewer;
 const stop=()=>{finished=true;clearTimeout(timer);clearInterval(clock);$('dialog').removeEventListener('close',closed);};
 const cancel=()=>{if(q?.status==='pending')api('/location-requests/'+q.id+'/decision','POST',{decision:'cancel'}).catch(()=>{});};
 const closed=()=>{cancel();stop();};
 $('dialog').addEventListener('close',closed);
 const failures={permission:'상대 휴대전화의 위치 권한이 필요합니다.',location_off:'상대 휴대전화의 위치 기능이 꺼져 있습니다.',timeout:'상대 휴대전화에서 새 위치를 얻지 못했습니다. 위치 설정을 켜고 다시 확인해 주세요.',background_restricted:'상대 휴대전화의 백그라운드 실행이 제한되었습니다. 상대가 앱을 열면 다시 조회할 수 있습니다.',unavailable:'상대 휴대전화에서 위치 요청을 처리하지 못했습니다.'};
 const clock=setInterval(()=>{
  if(!active()){cancel();stop();return;}
  if(Date.now()>=deadline){status.textContent='응답 시간이 초과되었습니다. 상대 휴대전화의 인터넷·위치 권한을 확인하고 다시 조회하세요.';cancel();stop();}
 },1000);
 function render(){
  if(!active()||finished)return;
  if(q.status==='pending'){status.textContent=q.auto_response?'상대 휴대전화에서 현재 위치를 확인하고 있습니다…':'상대의 위치 공유가 꺼져 있어 승인을 기다립니다. 상대 앱에서 요청을 확인해 주세요.';return;}
  stop();
  if(q.status==='responded'){
   const p=q.location;
   if(!p||!Number.isFinite(p.latitude)||!Number.isFinite(p.longitude)||Math.abs(p.latitude)>90||Math.abs(p.longitude)>180){status.textContent='위치 결과가 없거나 만료되었습니다. 다시 조회해 주세요.';return;}
   status.textContent='현재 위치 확인 완료';
   result.append(el('strong','',shownName(user)),el('p','',p.latitude.toFixed(5)+', '+p.longitude.toFixed(5)),
    el('p','',new Date(p.captured_at*1000).toLocaleString('ko-KR')+' · '+(p.approximate?'대략적 위치 · ':'')+'정확도 약 '+Math.ceil(p.accuracy_m)+'m'));
   result.append(nativeAction('지도 앱으로 열기',()=>{
    if(Date.now()/1000>=q.expires_at){toast('위치 조회가 만료되었습니다. 다시 확인해 주세요.');return;}
    if(/OnTechChatAndroid\//.test(navigator.userAgent))nativeFeature('open-location?request='+q.id);
    else window.open('https://www.google.com/maps/search/?api=1&query='+encodeURIComponent(p.latitude+','+p.longitude),'_blank','noopener,noreferrer');
   },'primary wide'));
  }else status.textContent=q.status==='failed'?(failures[q.failure]||failures.unavailable):({declined:'상대가 위치 조회를 거절했습니다.',cancelled:'위치 조회가 취소되었거나 공유가 중지되었습니다.',expired:'위치 요청이 만료되었습니다.',unavailable:'현재 위치를 조회할 수 없는 계정입니다.'}[q.status]||'위치 상태를 확인하지 못했습니다.');
 }
 async function poll(){
  if(!active()||finished){stop();return;}
  try{q=await api('/location-requests/'+q.id);if(!active()||finished)return;render();}
  catch(e){if(active()&&!finished)status.textContent='연결 재시도 중 · '+e.message;}
  if(active()&&!finished)timer=setTimeout(poll,1500);
 }
 try{
  q=await api('/location-requests','POST',{user_id:user.id,confirmed:true,client_id:crypto.randomUUID()});
  if(!active()||finished){cancel();stop();return;}
  deadline=q.expires_at*1000;render();if(!finished)timer=setTimeout(poll,800);
 }catch(e){if(active())status.textContent=e.message;stop();}
}
function locationRequestCard(m){
 let q;try{q=JSON.parse(m.text);}catch{return el('div','bubble deleted','위치 요청을 읽지 못했습니다.');}
 if(!q||!/^[a-zA-Z0-9_-]{8,100}$/.test(q.id||'')||typeof q.expires_at!=='number')return el('div','bubble deleted','위치 요청 정보 없음');
 const box=el('div','bubble location-request-card'),heading=el('strong','','현재 위치 확인 요청'),label=el('p','request-status',''),actions=el('div','request-actions');
 const target=q.target_id===currentUser?.id,requester=q.requester_id===currentUser?.id;
 const state=()=>q.status==='pending'&&q.expires_at<=Date.now()/1000?'expired':q.status;
 const automatic=!!q.auto_response;
 const labels={pending:automatic?'자동 응답 처리 중 · 내 위치 공유 설정에 따라 새 위치를 측정합니다.':'승인 대기 · 상대 위치는 아직 수집하지 않았습니다.',responded:'응답 완료 · 별도의 위치 카드에서 측정 시각·정확도를 확인하세요.',declined:'거절됨 · 위치를 수집하지 않았습니다.',cancelled:'요청 취소됨',expired:'5분이 지나 만료됐습니다. 새 요청이 필요합니다.',unavailable:'요청을 처리할 수 없는 계정 또는 대화입니다.'};
 box.append(heading,el('p','',target?(q.requester_name||'친구')+'님이 내 현재 위치를 요청했습니다.':(q.target_name||'친구')+'님의 현재 위치를 요청했습니다.'),label,actions,
 el('small','location-expiry','상대 동의 후 1회 측정 · 자동 추적 없음 · 응답 좌표 최대 24시간 보관'));
 const decision=async kind=>{if(state()!=='pending'){toast('이미 처리됐거나 만료된 요청입니다.');return;}
 if(!confirm(kind==='decline'?'이 요청을 거절할까요? 위치는 전송하지 않습니다.':'위치 확인 요청을 취소할까요?'))return;
 for(const b of actions.querySelectorAll('button'))b.disabled=true;
 try{q=await api('/location-requests/'+q.id+'/decision','POST',{decision:kind});await reloadMessages();}catch(e){toast(e.message);}finally{update();}};
 function update(){const st=state();label.textContent=labels[st]||'알 수 없는 요청 상태';actions.replaceChildren();
 if(st==='pending'&&target&&!automatic){actions.append(nativeAction('동의하고 내 위치 측정',()=>{if(state()!=='pending'){update();return;}nativeFeature('answer-location?request='+q.id);},'primary'),action('거절',()=>decision('decline'),'text-button'));
 }else if(st==='pending'&&requester)actions.append(action('요청 취소',()=>decision('cancel'),'text-button'));
 }
 update();box.dataset.requestExpires=String(q.expires_at);box.dataset.requestPending=String(q.status==='pending');return box;
}
setInterval(()=>{for(const card of document.querySelectorAll('[data-request-expires][data-request-pending="true"]'))if(+card.dataset.requestExpires<=Date.now()/1000){card.dataset.requestPending='false';card.querySelector('.request-status').textContent='요청이 만료되었습니다. 새 요청이 필요합니다.';card.querySelector('.request-actions').replaceChildren();}},1000);

$('location-preferences-button').addEventListener('click',()=>{
 openHistorySettings();
});

function nativeAction(label,handler,cls='secondary'){const b=el('button',cls,label);b.type='button';b.addEventListener('click',handler);return b;}

$('clear-ai-chat').addEventListener('click',async()=>{
 const room=currentRoom;if(!room||!['ai','direct','chat','auth'].includes(room.kind))return;const ai=room.kind==='ai',auth=room.kind==='auth';
 const question=ai?'이 AI 대화의 질문과 답변을 모두 삭제할까요? 진행 중인 응답과 작업은 중지합니다. 작업공간 파일은 유지되며 대화는 복구할 수 없습니다.':auth?'개인 수신함의 현재 메일 내용을 모두 삭제할까요? 삭제한 과거 메일은 수동 재검사에서도 다시 표시되지 않으며 이후 새 메일은 계속 받을 수 있습니다.':'이전 대화를 내 계정에서 모두 삭제할까요? 내 모든 기기에 적용되며 되돌릴 수 없습니다. 상대방의 대화와 첨부파일은 유지됩니다. 이후 새 메시지는 계속 받을 수 있습니다.';
 if(!confirm(question))return;
 const button=$('clear-ai-chat');button.disabled=true;
 try{
  await api('/rooms/'+encodeURIComponent(room.id)+(ai?'/ai/messages':'/messages'),'DELETE');
  if(currentRoom?.id===room.id){loadingSerial++;displayedMessages=[];pageHasMore=false;if(typeof clearComposerRoom==='function')clearComposerRoom(room.id);if(ai){trackAIRequest(null);trackAgentJob(null);}renderTimeline();}
  await refreshRooms();if(currentRoom?.id===room.id)await reloadMessages(true);toast(ai?'AI 대화 내용을 삭제했습니다.':auth?'개인 수신함 내용을 삭제했습니다.':'내 계정에서 대화 내용을 삭제했습니다.');
 }catch(e){toast(e.message);}finally{button.disabled=false;}
});
