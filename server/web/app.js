'use strict';
const $ = id => document.getElementById(id);
let currentUser = null, currentRoom = null, roomList = [], stream = null, loadingSerial = 0;
let reconnectTimer = null, toastTimer = null;
const temporaryURLs = [];
function el(tag, cls, text) { const n = document.createElement(tag); if (cls) n.className = cls; if (text !== undefined) n.textContent = text; return n; }
function toast(text) {
 const top = document.querySelector('#settings-dialog[open]') || document.querySelector('#dialog[open]');
 if (top) { let note=top.querySelector('.modal-notice'); if(!note){note=el('p','modal-notice');note.setAttribute('role','status');(top.querySelector('#dialog-body')||top).prepend(note);}note.textContent=text; }
 $('toast').textContent=text; $('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').hidden=true,4500);
}
function resetSession() { cancelSessionReconnect(); if(typeof closeSettings==='function')closeSettings(); if(typeof resetMessenger==='function')resetMessenger(); currentUser = null; currentRoom = null; roomList = []; loadingSerial++; if (stream) stream.close(); stream = null; clearTimeout(reconnectTimer); $('workspace').hidden = true; $('login-screen').hidden = false; $('messages').replaceChildren(); $('room-list').replaceChildren(); if ($('dialog').open) $('dialog').close(); $('login-password').value = ''; }
async function api(path, method = 'GET', data = undefined, extra = {}, signal = undefined) {
 const headers = {'X-OnTech-Request':'1', ...extra}; const options = {method, headers, credentials:'same-origin', cache:'no-store', signal};
 if (data instanceof FormData) options.body = data;
 else if (data !== undefined) { headers['Content-Type'] = 'application/json'; options.body = JSON.stringify(data); }
 const response = await fetch('/api' + path, options); let body;
 try { body = await response.json(); } catch { throw new Error('서버 응답을 읽지 못했습니다.'); }
 if (!response.ok) { if (response.status === 401 && body.code === 'SESSION_INVALID' && path !== '/login') resetSession(); const err=new Error(body.error || '요청에 실패했습니다.');err.status=response.status;err.code=body.code||'';err.data=body;throw err; }
 return body;
}
function dateText(t) { return new Date(t * 1000).toLocaleString('ko-KR', {month:'numeric', day:'numeric', hour:'2-digit', minute:'2-digit'}); }
function byteText(n) { if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB'; if (n >= 1024) return (n / 1024).toFixed(1) + ' KB'; return n + ' B'; }
function dialog(title) { for (const u of temporaryURLs) URL.revokeObjectURL(u); temporaryURLs.length = 0; $('dialog-title').textContent = title; $('dialog-body').replaceChildren(); if (!$('dialog').open) $('dialog').showModal(); return $('dialog-body'); }
function field(label, type='text', value='') { const l=el('label','',label), i=el('input'); i.type=type; i.value=value; i.required=true; if(type==='password'){i.minLength=8;i.maxLength=128;i.autocomplete='new-password';} l.append(i); return [l,i]; }
function action(label, handler, cls='secondary') { const b=el('button',cls,label); b.type='button'; b.addEventListener('click',()=>Promise.resolve().then(handler).catch(e=>toast(e.message))); return b; }
$('dialog-close').addEventListener('click',()=>$('dialog').close());
$('dialog').addEventListener('close',()=>{for(const u of temporaryURLs)URL.revokeObjectURL(u);temporaryURLs.length=0;});
$('login-form').addEventListener('submit',async e=>{e.preventDefault();$('login-error').textContent='';$('login-button').disabled=true;try{const v=await api('/login','POST',{username:$('login-user').value,password:$('login-password').value});$('login-password').value='';await enter(v.user);}catch(err){$('login-error').textContent=err.message;}finally{$('login-button').disabled=false;}});
function renderLegacyMessage(m) { const mine=m.user_id===currentUser?.id;const wrap=el('article','message'+(mine?' mine':''));wrap.dataset.id=String(m.id);const meta=el('div','message-meta');meta.append(el('span','',m.username||'시스템'),el('time','',dateText(m.created_at)));if(m.kind!=='deleted'&&(mine||currentRoom?.owner_id===currentUser?.id)){meta.append(action('삭제',async()=>{if(!confirm('이 메시지를 삭제할까요? 첨부파일도 삭제됩니다.'))return;await api(`/messages/${m.id}`,'DELETE');await reloadMessages();},'text-button danger'));}wrap.append(meta);
 if(m.kind==='auth'){wrap.append(mailMessageCard(m));
 }else if(m.kind==='file'&&m.file){const f=m.file,box=el('div','bubble file-card');box.append(el('div','file-name',f.name),el('div','file-meta',byteText(f.size)));if(/^image\/(png|jpeg|gif|webp)(;|$)/.test(f.mime)){const image=el('img','thumbnail');image.alt=f.name;image.loading='lazy';image.src=`/api/files/${f.id}?inline=1`;image.addEventListener('click',()=>preview(f).catch(e=>toast(e.message)));box.append(image);}const controls=el('div','file-actions');const download=el('a','', '다운로드');download.href=`/api/files/${f.id}`;download.download=f.name;controls.append(download);if(/^(image\/(png|jpeg|gif|webp)|application\/pdf|text\/plain)(;|$)/.test(f.mime))controls.append(action('미리보기',()=>preview(f)));box.append(controls);wrap.append(box);
 }else{wrap.append(el('div','bubble'+(['deleted','expired'].includes(m.kind)?' deleted':''),m.kind==='deleted'?'삭제된 메시지':m.kind==='expired'?'보관 시간이 만료된 인증번호':m.text));}return wrap; }
function mailMessageCard(m){
 let data;try{data=JSON.parse(m.text);}catch{data={body:m.text||''};}if(!data||typeof data!=='object')data={};
 const subject=typeof data.subject==='string'?data.subject:(data.service||'수신 메일');const body=typeof data.body==='string'?data.body:(typeof data.code==='string'?data.code:'');
 const card=el('div','auth-card');card.dataset.expires=String(m.expires_at||0);card.append(el('b','mail-subject',subject),el('div','mail-body',body),action('내용 복사',async()=>{if(m.expires_at>0&&m.expires_at<=Date.now()/1000)throw new Error('보관 시간이 만료되었습니다.');await navigator.clipboard.writeText(subject+'\n\n'+body);toast('메일 내용을 복사했습니다.');}),el('small','expiry-label',''));return card;
}
function expireCards(){for(const card of document.querySelectorAll('.auth-card')){const expiry=Number(card.dataset.expires);const label=card.querySelector('.expiry-label');if(!expiry||!label)continue;const remaining=Math.ceil(expiry-Date.now()/1000);if(remaining<=0){const content=card.querySelector('.mail-body')||card.querySelector('.auth-code');if(content)content.textContent='보관 시간이 만료되었습니다.';const subject=card.querySelector('.mail-subject');if(subject)subject.textContent='만료된 메일';const button=card.querySelector('button');if(button)button.disabled=true;label.textContent='서버 보관 시간이 만료되었습니다.';}else label.textContent=`서버 보관 종료까지 ${Math.floor(remaining/60)}분 ${remaining%60}초`;}}
setInterval(expireCards,1000);
document.addEventListener('visibilitychange',()=>{if(document.hidden){for(const code of document.querySelectorAll('.auth-code'))code.textContent='••••••';}else if(currentUser){refreshRooms().then(()=>{if(currentRoom)return reloadMessages();}).catch(()=>{});}});
$('message-text').addEventListener('keydown',e=>{if(e.key==='Enter'&&!e.shiftKey&&!e.isComposing){e.preventDefault();$('message-form').requestSubmit();}});
async function preview(file) { const body=dialog(file.name);body.append(el('p','dialog-note','미리보기를 준비하고 있습니다.'));const response=await fetch(`/api/files/${file.id}?inline=1`,{credentials:'same-origin',cache:'no-store'});if(!response.ok)throw new Error('파일에 접근할 수 없습니다.');const blob=await response.blob();if(!currentUser||!$('dialog').open||$('dialog-title').textContent!==file.name)return;body.replaceChildren();if(file.mime.startsWith('text/plain')){body.append(el('pre','',await blob.slice(0,262144).text()));if(blob.size>262144)body.prepend(el('p','dialog-note','텍스트 앞부분 256KB만 표시합니다.'));}else{const url=URL.createObjectURL(blob);temporaryURLs.push(url);if(file.mime.startsWith('image/')){const img=el('img','preview-image');img.src=url;img.alt=file.name;body.append(img);}else{body.append(el('p','dialog-note','기기의 PDF 뷰어에 따라 표시되지 않을 수 있습니다. 그 경우 다운로드 후 열어 주세요.'));const frame=el('iframe','preview-frame');frame.setAttribute('sandbox','');frame.title=file.name;frame.src=url;body.append(frame);}}const a=el('a','', '파일 다운로드');a.href=`/api/files/${file.id}`;a.download=file.name;body.append(a); }
$('new-room-button').addEventListener('click',()=>{const body=dialog('새 채팅방');body.append(el('p','dialog-note','초대한 사용자는 이 방의 과거 메시지와 첨부파일도 볼 수 있습니다.'));const form=el('form');const [nl,name]=field('방 이름');name.maxLength=80;const [ml,members]=field('참여자 아이디 (쉼표로 구분)');members.required=false;const submit=el('button','primary wide','채팅방 만들기');submit.type='submit';form.append(nl,ml,submit);form.addEventListener('submit',async e=>{e.preventDefault();submit.disabled=true;try{const room=await api('/rooms','POST',{name:name.value,members:members.value.split(',').map(s=>s.trim()).filter(Boolean)});$('dialog').close();await refreshRooms();await selectRoom(roomList.find(r=>r.id===room.id));}catch(err){toast(err.message);}finally{submit.disabled=false;}});body.append(form);});
$('members-button').addEventListener('click',()=>showMembers(currentRoom).catch(e=>toast(e.message)));
async function showMembers(room=currentRoom) { if(!room)return;const members=await api(`/rooms/${room.id}/members`),body=dialog('대화방 정보');const type=room.kind==='chat'?'그룹 대화':room.kind==='direct'?'1:1 대화':room.kind==='auth'?'개인 수신함':'대화';body.append(el('p','dialog-note',`${type} · 참여자 ${members.length}명`));for(const m of members){const row=el('div','user-row');row.append(el('b','',m.username+(m.owner?' · 방장':'')),el('small','',m.disabled?'사용 중지':'참여 중'));body.append(row);}if(room.kind==='chat'&&room.owner_id===currentUser.id){const [label,input]=field('초대할 사용자 아이디');const add=action('사용자 초대',async()=>{await api(`/rooms/${room.id}/members`,'POST',{username:input.value});await refreshRooms();await showMembers(room);},'primary');body.append(label,add);} }
function usernameError(value) {
 return /^[a-z0-9가-힣_.-]{3,40}$/u.test(value) ? '' : '아이디는 한글·영문·숫자·_.- 3~40자로 입력하세요. 공백과 다른 기호는 사용할 수 없습니다.';
}
function passwordError(value) {
 if (Array.from(value).length<8) return '비밀번호는 8자 이상 입력하세요.';
 if (new TextEncoder().encode(value).length>128) return '비밀번호는 UTF-8 기준 128바이트 이하여야 합니다.';
 return '';
}
function formError(form, text) {const n=form.querySelector('.form-error');n.textContent=text;if(text)n.scrollIntoView({block:'nearest'});}
function addFormError(form) {const n=el('p','error form-error');n.setAttribute('role','alert');form.append(n);return n;}
$('password-button').addEventListener('click',()=>{
 const body=dialog('비밀번호 변경'),form=el('form');form.noValidate=true;
 body.append(el('p','dialog-note','새 비밀번호는 8자 이상입니다. 변경하면 모든 로그인 세션이 종료됩니다.'));
 const [cl,current]=field('현재 비밀번호','password');current.minLength=0;current.autocomplete='current-password';
 const [nl,next]=field('새 비밀번호 · 8자 이상','password');const button=el('button','primary wide','변경 후 다시 로그인');button.type='submit';
 form.append(cl,nl);addFormError(form);form.append(button);
 form.addEventListener('submit',async e=>{e.preventDefault();const error=!current.value?'현재 비밀번호를 입력하세요.':passwordError(next.value);formError(form,error);if(error)return;button.disabled=true;
 try{await api('/password','POST',{current_password:current.value,new_password:next.value});current.value='';next.value='';resetSession();toast('비밀번호를 변경했습니다. 다시 로그인하세요.');}
 catch(err){formError(form,err.message);}finally{button.disabled=false;}});body.append(form);
});
$('logout-button').addEventListener('click',async()=>{try{await api('/logout','POST',{});}catch{}resetSession();});
$('devices-button').addEventListener('click',()=>showDevices().catch(e=>toast(e.message)));
async function showDevices() { const devices=await api('/devices'),body=dialog('등록 기기');body.append(el('p','dialog-note','FCM을 연결한 Android 앱이 이 목록에 표시됩니다. 현재 웹 화면에는 FCM 푸시가 발송되지 않습니다.'));if(!devices.length)body.append(el('p','subtle','등록된 푸시 수신 기기가 없습니다.'));for(const d of devices){const row=el('div','user-row');row.append(el('b','',d.name||d.id),el('small','',dateText(d.updated_at)),action('등록 해제',async()=>{await api(`/devices/${d.id}`,'DELETE');await showDevices();}));body.append(row);} }
$('admin-button').addEventListener('click',()=>showAdminV113().catch(e=>toast(e.message)));
async function showAdmin() { const [status,users]=await Promise.all([api('/status'),api('/users')]);const body=dialog('서버 관리'),grid=el('div','status-grid');body.append(action('사용자 추가',showCreateUser,'primary wide'));for(const [label,value]of [['사용자',status.users],['채팅방',status.rooms],['메시지',status.messages],['첨부파일',byteText(status.file_bytes)],['메일 수집',status.mail_enabled?'사용':'미설정'],['FCM 발송',status.fcm_enabled?'사용':'미설정']]){const c=el('div','status-cell');c.append(el('span','',label),el('b','',String(value)));grid.append(c);}body.append(grid,el('p','dialog-note',`메일: ${status.mail_state}\nFCM: ${status.fcm_state}\n발송 대기 ${status.push_pending} / 요청 성공 ${status.push_sent_requests} / 실패 ${status.push_failed}\n푸시 요청 성공은 휴대전화 수신·읽음 확인과 다릅니다.`));for(const u of users){const row=el('div','user-row');row.append(el('b','',u.username+(u.role==='admin'?' · 관리자':'')),el('small','',u.disabled?'사용 중지':'사용 중'));if(u.id!==currentUser.id&&u.role!=='admin'){row.append(action(u.disabled?'사용 허용':'사용 중지',async()=>{await api(`/users/${u.id}`,'PATCH',{disabled:!u.disabled});await showAdmin();}));row.append(action('암호 재설정',async()=>{showResetUserPassword(u);}));}body.append(row);}const add=action('사용자 추가',()=>showCreateUser(),'primary');const actions=el('div','row-actions');actions.append(action('설정 수정',async()=>{$('dialog').close();await showSettings();},'secondary'),add);body.append(actions,el('p','footnote','설정 수정에서 메일·알림을 변경할 수 있습니다. 기존 비밀번호·발송 키 내용은 표시하지 않습니다.')); }
function showCreateUser() {
 const body=dialog('사용자 추가'),form=el('form');form.id='create-user-form';form.noValidate=true;
 const [ul,username]=field('아이디');username.maxLength=40;username.autocomplete='off';username.autocapitalize='none';username.spellcheck=false;username.placeholder='나도윤 또는 member01';
 const [pl,password]=field('임시 비밀번호 · 8자 이상','password');password.placeholder='8자 이상';
 const b=el('button','primary wide','사용자 만들기');b.type='submit';form.append(ul,pl);addFormError(form);form.append(b);
 form.addEventListener('submit',async e=>{
  e.preventDefault();const name=username.value.trim().toLowerCase();username.value=name;
  const nameError=usernameError(name), passError=passwordError(password.value);
  username.setAttribute('aria-invalid',String(!!nameError));password.setAttribute('aria-invalid',String(!!passError));
  const error=[nameError,passError].filter(Boolean).join('\n');
  formError(form,error);if(error){(nameError?username:password).focus();return;}b.disabled=true;b.textContent='사용자 생성 중…';
  try{await api('/users','POST',{username:name,password:password.value});password.value='';await showAdminV113();toast('사용자 '+name+'을 생성했습니다. 해당 계정으로 로그인할 수 있습니다.');}
  catch(err){formError(form,err.message);}finally{b.disabled=false;b.textContent='사용자 만들기';}
 });
 body.append(el('p','dialog-note','아이디: 한글·영문·숫자·_.- 3~40자 (예: 나도윤).\n비밀번호: 8자 이상 · 조합 조건 없음 · 최대 UTF-8 128바이트.\n사용자 추가 화면 v1.5.0 · 기존 계정은 변경하지 않습니다.'),form);
 body.append(action('사용자 목록으로 돌아가기',showAdminV113,'text-button'));
}
function showResetUserPassword(user) {
 const body=dialog(user.username+' · 비밀번호 재설정'),form=el('form');form.noValidate=true;
 const [label,password]=field('새 임시 비밀번호 · 8자 이상','password');form.append(label);addFormError(form);
 const b=el('button','primary wide','비밀번호 재설정');b.type='submit';form.append(b);
 form.addEventListener('submit',async e=>{e.preventDefault();const error=passwordError(password.value);formError(form,error);if(error)return;b.disabled=true;
 try{await api('/users/'+user.id,'PATCH',{password:password.value});password.value='';await showAdminV113();toast('비밀번호를 변경했습니다. 기존 로그인은 해제됩니다.');}
 catch(err){formError(form,err.message);}finally{b.disabled=false;}});
 body.append(el('p','dialog-note','이 사용자의 기존 로그인 세션을 모두 해제합니다. 대화와 파일은 삭제하지 않습니다.'),form);
}


async function showAdminV113() {
 const superadmin=currentUser?.role==='superadmin';
 const users=await api('/users'),status=superadmin?await api('/status'):null;
 const body=dialog(superadmin?'서버 관리':'위임 사용자 관리');
 if(superadmin)body.append(action('사용자 추가',showCreateUser,'primary wide'));
 if(status){const grid=el('div','status-grid');
  for(const [label,value]of [['사용자',status.users],['채팅방',status.rooms],['메시지',status.messages],['첨부파일',byteText(status.file_bytes)],['메일 수집',status.mail_enabled?'사용':'미설정'],['FCM 발송',status.fcm_enabled?'사용':'미설정']]){
   const c=el('div','status-cell');c.append(el('span','',label),el('b','',String(value)));grid.append(c);
  }
  body.append(grid);
 }
 for(const u of users){
  const manageable=superadmin||Object.prototype.hasOwnProperty.call(u,'disabled');
  const row=el('div','user-row'),label=roleLabel(u.role);
  row.append(el('b','',u.username+(label?' · '+label:'')),el('small','',u.disabled?'사용 중지':'사용 중'));
  if(u.id!==currentUser.id&&manageable){
   row.append(action(u.disabled?'사용 허용':'사용 중지',async()=>{await api('/users/'+u.id,'PATCH',{disabled:!u.disabled});await showAdminV113();}));
   row.append(action('암호 재설정',()=>showResetUserPassword(u)));
   
   if(superadmin)row.append(action('역할 변경',()=>showRoleChangeV113(u),'text-button'));
  }
  body.append(row);
 }
 const actions=el('div','row-actions');
 if(superadmin)actions.append(action('서버 설정',async()=>{$('dialog').close();await showSettings();},'secondary'));
 body.append(actions,el('p','footnote',superadmin?'역할과 AI 연결 정보는 최고관리자만 변경합니다. AI 작업공간은 사용자별로 자동 생성됩니다.':'명시적으로 위임된 일반 사용자만 관리할 수 있습니다.'));
}
function showRoleChangeV113(user){
 const body=dialog(user.username+' · 역할 변경'),form=el('form');form.noValidate=true;
 const roleLabelNode=el('label','','새 역할'),select=el('select');
 for(const [value,label]of [['user','일반 사용자'],['admin','관리자'],['superadmin','최고관리자']]){const o=el('option','',label);o.value=value;select.append(o);}
 select.value=user.role;roleLabelNode.append(select);
 const [pwLabel,password]=field('현재 최고관리자 비밀번호','password');password.autocomplete='current-password';
 const submit=el('button','primary wide','역할 변경');submit.type='submit';
 form.append(roleLabelNode,pwLabel);addFormError(form);form.append(submit);
 form.addEventListener('submit',async e=>{e.preventDefault();submit.disabled=true;
  try{await api('/users/'+user.id+'/role','PATCH',{role:select.value,current_password:password.value});password.value='';await showAdminV113();toast('역할을 변경했습니다. 대상 사용자의 기존 세션은 종료됩니다.');}
  catch(err){formError(form,err.message);}finally{submit.disabled=false;}
 });
 body.append(el('p','dialog-note','최고관리자 역할 변경은 재인증되며 마지막 최고관리자 보호 규칙이 적용됩니다.'),form);
}
