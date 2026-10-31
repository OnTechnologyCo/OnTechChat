'use strict';
// 설정은 현재 관리자 세션으로 저장합니다. 관리자 비밀번호를 재입력하거나 저장하지 않습니다.
// 메일 비밀번호/서비스 계정 키는 새로 입력할 때만 보내고, 조회 응답에는 다시 포함하지 않습니다.
function canOpenWindowsFirewallFromBrowser(hostname, windowsHelper) {
 return !!windowsHelper && ['localhost','127.0.0.1','[::1]','::1'].includes(hostname);
}
let settingsModal = null;
function closeSettings() {
 if (settingsModal) { settingsModal.close(); settingsModal.remove(); settingsModal = null; }
}
async function showSettings(initialTab = 'general') {
 if (currentUser?.role !== 'superadmin') return;
 const [snapshot, users] = await Promise.all([api('/settings'), api('/users')]);
 const s = snapshot.settings;
 closeSettings();
 const modal = el('dialog', 'settings-dialog'); modal.id = 'settings-dialog'; settingsModal = modal;
 const header = el('header', 'settings-head'), heading = el('div');
 heading.append(el('p', 'eyebrow', 'SERVER SETTINGS · v'+snapshot.version), el('h2', '', '서버 설정'));
 header.append(heading, action('닫기', closeSettings, 'secondary'));
 const banner = el('div', 'settings-banner', snapshot.pending_apply ? '저장된 변경사항이 있습니다. 「저장 후 적용」으로 서버에 반영하세요.' : '관리자 로그인 확인됨 · 비밀번호 재입력 없이 저장합니다.');
 const notice = el('p', 'settings-notice', snapshot.migration_notice || '메일 비밀번호는 최초 등록·변경 때만 입력합니다. 빈칸으로 저장하면 기존 값이 유지됩니다.');
 const tabs = el('nav', 'settings-tabs'); tabs.setAttribute('aria-label', '설정 범주');
 const panels = el('div', 'settings-panels'), refs = {}, sections = {};
 let activeTab = initialTab, serviceAccountJSON = '', keyReading = false;
 function section(id, name, sub) {
  const p = el('section', 'settings-panel'); p.id = 'settings-panel-' + id; p.hidden = true;
  p.append(el('h3', '', name), el('p', 'subtle', sub)); sections[id] = p; panels.append(p);
  const b = action(name, () => activate(id), 'settings-tab'); b.dataset.tab = id; b.setAttribute('aria-controls', p.id); tabs.append(b); return p;
 }
 function activate(id) {
  activeTab = id in sections ? id : 'general';
  for (const [k, p] of Object.entries(sections)) p.hidden = k !== activeTab;
  for (const b of tabs.children) { b.classList.toggle('active', b.dataset.tab === activeTab); b.setAttribute('aria-pressed', String(b.dataset.tab === activeTab)); }
  panels.scrollTop = 0;
 }
 function field(parent, key, label, tag = 'input', hint = '') {
  const l = el('label', 'settings-field', label), n = el(tag); n.id = 'setting-' + key; l.append(n);
  if (hint) l.append(el('small', '', hint)); parent.append(l); refs[key] = n; return n;
 }
 function input(parent, key, label, type, value, hint) {
  const n = field(parent, key, label, 'input', hint); n.type = type; n.value = value ?? ''; n.autocomplete = type === 'password' ? 'new-password' : 'off'; return n;
 }
 function area(parent, key, label, value, hint) { const n = field(parent, key, label, 'textarea', hint); n.value = value ?? ''; n.rows = 3; return n; }
 function check(parent, key, label, value, hint) {
  const l = el('label', 'settings-check'), n = el('input'); n.type = 'checkbox'; n.id = 'setting-' + key; n.checked = !!value;
  l.append(n, el('span', '', label)); if (hint) l.append(el('small', '', hint)); parent.append(l); refs[key] = n; return n;
 }
 function details(parent, title) { const d = el('details', 'settings-details'); d.append(el('summary', '', title)); parent.append(d); return d; }
 function option(select, value, label) { const o = el('option', '', label); o.value = value; select.append(o); }
 const splitValues = text => text.split(/[\n,]/).map(v => v.trim()).filter(Boolean);
 const general = section('general', '일반', '대화·파일·사용자 정보는 유지됩니다. 관리자 권한은 로그인 세션으로 확인합니다.');
 const overview = el('div', 'settings-overview'); overview.append(el('span', '', '설정 파일'), el('code', '', snapshot.config_path), el('span', '', '서버 수신 주소'), el('code', '', snapshot.listen)); general.append(overview);
 const commonGrid = el('div', 'settings-grid'); general.append(commonGrid);
 let n = input(commonGrid, 'max-file', '첨부파일 1개 제한 (MiB)', 'number', s.max_file_bytes / 1048576); n.min = '0.001'; n.max = '100'; n.step = '0.001';
 n = input(commonGrid, 'quota', '전체 첨부 저장량 (GiB)', 'number', s.storage_quota_bytes / 1073741824); n.min = '0.01'; n.step = '0.01';
 n = input(commonGrid, 'session', '로그인 유지', 'number', s.session_hours); n.hidden = true; n.closest('label').hidden = true;
  commonGrid.append(el('p', 'settings-session-note', '직접 로그아웃하기 전까지 로그인을 유지합니다. 비밀번호 변경·계정 사용 중지 시 로그인 권한이 해제됩니다.'));
 n = input(commonGrid, 'retention', '수신 메일 보관 (분)', 'number', s.auth_retention_minutes, '새로 전달한 메일의 서버 보관 시간입니다.'); n.min = '1'; n.max = '60';
 n = input(commonGrid, 'messages', '최대 메시지 수', 'number', s.max_messages); n.min = '100'; n.max = '100000';
 general.append(action('사용자 관리 · 추가', async () => { closeSettings(); await showAdmin(); }, 'secondary'));
 general.append(el('p', 'settings-session-note', '로그인 비밀번호 자체 변경은 「계정」 메뉴에서 합니다. 설정 저장에는 비밀번호를 다시 묻지 않습니다.'));


 const updatesPanel=section('updates','업데이트','GitHub 정식 릴리스에서 서버 설치 프로그램과 Android APK를 다운로드합니다.');
 const updatesStatus=el('p','settings-session-note','현재 서버 '+snapshot.version+' · 최신 릴리스를 확인하세요.'),updatesFiles=el('div','settings-controls');
 const updatesCheck=action('최신 릴리스 확인',async()=>{
  updatesCheck.disabled=true;updatesStatus.textContent='GitHub 릴리스 확인 중…';updatesFiles.replaceChildren();
  try{
   const info=await api('/app-updates/github'),release=info.release;
   if(!modal.isConnected)return;
   updatesStatus.textContent='현재 서버 '+info.current_server+' · GitHub '+release.tag+(info.server_update_available?' · 서버 업데이트가 있습니다.':' · 현재 서버가 같거나 더 최신입니다.');
   const addDownload=(asset,label)=>{if(!asset)return;const link=el('a','secondary',label+' · '+asset.version);link.href=asset.url;link.target='_blank';link.rel='noopener noreferrer';link.download=asset.name;updatesFiles.append(link);};
   addDownload(release.server,'PC 설치 프로그램 다운로드');addDownload(release.android,'Android APK 다운로드');
  }catch(e){updatesStatus.textContent=e.message||'GitHub 릴리스를 확인하지 못했습니다.';}
  finally{updatesCheck.disabled=false;}
 },'primary');
 updatesCheck.id='github-update-check';updatesPanel.append(updatesStatus,updatesCheck,updatesFiles,el('p','subtle','서버를 설치한 PC에서 설치 프로그램을 실행하세요. 모바일 앱의 업데이트 확인은 GitHub APK를 다운로드하고 검증한 뒤 Android 설치 화면을 엽니다.'));

 const aiState=s.ai||{}, aiPanel=section('ai','AI 대화','모델과 추론 강도만 선택합니다. 작업공간·격리·동시성은 서버가 자동 관리합니다.');
 const aiTop=el('div','settings-grid');aiPanel.append(aiTop);check(aiTop,'ai-enabled','AI 대화 사용',aiState.enabled,'저장 후 적용하면 AI 친구에서 바로 사용할 수 있습니다.');
 const defaultModel=field(aiTop,'ai-default-model','기본 모델','select','AI 친구를 처음 열 때 사용할 모델입니다.');
 const aiProvidersBox=el('div','settings-ai-list');aiPanel.append(aiProvidersBox);
 let aiProviderRows=[],aiModelRows=[],aiTestLocal,aiTestLive;
 const unique=v=>[...new Set((v||[]).filter(Boolean))];
 const rowInput=(parent,label,type,value)=>{const l=el('label','settings-field',label),x=el('input');x.type=type;x.value=value??'';l.append(x);parent.append(l);return x;};
 const providerTitle=t=>t==='claude_cli'?'Claude Code':t==='codex_cli'?'Codex':t==='openai_compatible'?'OpenAI 호환 API':t;
 function refreshDefaultModels(){
  const current=defaultModel.value||aiState.default_model_id||'';defaultModel.replaceChildren();
  for(const row of aiModelRows.filter(x=>x.enabled.checked&&x.providerRow?.enabled.checked&&x.model.value.trim()))option(defaultModel,row.id,row.label.textContent+' · '+row.model.value.trim());
  if([...defaultModel.options].some(o=>o.value===current))defaultModel.value=current;
  else if(defaultModel.options.length)defaultModel.value=defaultModel.options[0].value;
 }
 const codexModelEfforts={
  'gpt-6-astra':['low','medium','high','xhigh','max','ultra'],
  'gpt-6-sol':['low','medium','high','xhigh','max','ultra'],
  'gpt-6-luna':['low','medium','high','xhigh','max'],
  'gpt-5.6-sol':['low','medium','high','xhigh','max','ultra'],
  'gpt-5.6-terra':['low','medium','high','xhigh','max','ultra'],
  'gpt-5.6-luna':['low','medium','high','xhigh','max'],
  'gpt-5.5':['low','medium','high','xhigh']
 };
 const codexDefaultEffort={'gpt-6-astra':'medium','gpt-6-sol':'medium','gpt-6-luna':'medium','gpt-5.6-sol':'low','gpt-5.6-terra':'medium','gpt-5.6-luna':'medium','gpt-5.5':'medium'};
 function modelField(parent,providerRow,value){
  const presets=providerRow.type==='claude_cli'?['sonnet','opus','fable']:providerRow.type==='codex_cli'?Object.keys(codexModelEfforts):[];
  if(!presets.length)return rowInput(parent,'모델','text',value||'');
  const l=el('label','settings-field','모델'),x=el('select');for(const name of unique([value,...presets]))if(name)option(x,name,name);x.value=value||presets[0];l.append(x);parent.append(l);return x;
 }
 function addSimpleModel(providerRow,v={}){
  const card=el('div','settings-ai-model-simple'),head=el('div','settings-rule-head'),enabled=el('input');enabled.type='checkbox';enabled.checked=v.enabled!==false;
  const label=el('strong','',v.name||v.model||'모델');head.append(enabled,label);card.append(head);
  const grid=el('div','settings-grid');card.append(grid);const model=modelField(grid,providerRow,v.model||'');if(model.tagName==='INPUT')model.placeholder='실제 모델 이름';
  const effortLabel=el('label','settings-field','추론 강도'),effort=el('select');effortLabel.append(effort);grid.append(effortLabel);
  let supported=[],allowed=[],row=null;
  const syncEfforts=()=>{
   const presets=providerRow.type==='claude_cli'?['low','medium','high','xhigh','max']:providerRow.type==='codex_cli'?(codexModelEfforts[model.value]||[]):[];
   supported=unique([...(v.supported_efforts||[]),...presets]);allowed=unique([...(v.allowed_efforts||[]),...supported]);
   const efforts=unique(['provider_default',...allowed]),previous=effort.value||v.default_effort||'',providerDefault=providerRow.type==='codex_cli'?(codexDefaultEffort[model.value]||'medium'):'provider_default';
   effort.replaceChildren();for(const e of efforts)option(effort,e,e==='provider_default'?'기본':e);
   effort.value=efforts.includes(previous)?previous:(efforts.includes(providerDefault)?providerDefault:'provider_default');
   if(row){row.allowed=allowed;row.supported=supported;}
  };
  const remove=action('모델 제거',()=>{aiModelRows=aiModelRows.filter(x=>x.card!==card);card.remove();refreshDefaultModels();},'text-button danger');card.append(remove);
  const id=v.id||providerRow.id+'-'+Date.now().toString(36)+'-'+Math.random().toString(36).slice(2,6);
  row={card,id,providerID:providerRow.id,providerRow,label,model,enabled,effort,name:v.name||'',allowed,supported,source:v.capability_source||'provider',checked:v.capability_checked_at||Math.floor(Date.now()/1000)};
  const modelChanged=()=>{syncEfforts();if(!row.name)label.textContent=model.value.trim()||'모델';refreshDefaultModels();};syncEfforts();aiModelRows.push(row);enabled.addEventListener('change',refreshDefaultModels);model.addEventListener('input',modelChanged);model.addEventListener('change',modelChanged);providerRow.models.append(card);refreshDefaultModels();
 } for(const p of aiState.providers||[]){
  const card=el('section','settings-ai-card'),head=el('div','settings-rule-head'),enabled=el('input');enabled.type='checkbox';enabled.checked=!!p.enabled;
  head.append(enabled,el('strong','',p.name||providerTitle(p.type)));card.append(head);
  const info=el('p','subtle',p.type==='claude_cli'?'Claude Code 로그인 계정을 사용합니다. 일반 대화와 작업 요청을 자동 구분합니다.':p.type==='codex_cli'?'서버 실행 계정의 Codex 로그인을 사용합니다. 일반 대화와 작업 요청을 자동 구분합니다.':'API 키가 필요한 HTTP 모델 연결입니다.');card.append(info);
  const providerRow={id:p.id,type:p.type,source:p,enabled,card,models:el('div','settings-ai-model-list'),key:null,clear:null,base:null};
  if(p.type==='claude_cli'||p.type==='codex_cli'){
   const details=el('details'),grid=el('div','settings-grid');details.append(el('summary','','CLI 실행 경로 · 자동 탐색 실패 시 지정'));
   providerRow.executable=rowInput(grid,'서버 PC의 CLI 실행 파일 경로','text',p.executable||'');providerRow.executable.placeholder=p.type==='codex_cli'?'codex':'claude';
   details.append(grid,el('p','subtle','비워 두면 자동 탐색합니다. PowerShell의 Get-Command 결과에서 실제 경로를 입력할 수도 있습니다. 로그인은 서버 실행 계정 기준입니다.'));card.append(details);
  }
  if(p.type==='openai_compatible'){
   const grid=el('div','settings-grid');providerRow.base=rowInput(grid,'API 주소','text',p.base_url||'https://api.openai.com/v1');providerRow.key=rowInput(grid,'API 키 · 새 값만','password','');providerRow.key.autocomplete='new-password';
   providerRow.clear=el('input');providerRow.clear.type='checkbox';const clearLabel=el('label','settings-check');clearLabel.append(providerRow.clear,el('span','',p.has_api_key?'저장된 API 키 삭제 · 현재 키 있음':'저장된 API 키 삭제'));grid.append(clearLabel);card.append(grid);
  }
  card.append(providerRow.models);const add=action('＋ 모델 추가',()=>addSimpleModel(providerRow,{enabled:true,default_effort:'provider_default'}),'secondary');card.append(add);aiProvidersBox.append(card);aiProviderRows.push(providerRow);enabled.addEventListener('change',refreshDefaultModels);
  const savedModels=(aiState.models||[]).filter(x=>x.provider_id===p.id);if(savedModels.length){for(const m of savedModels)addSimpleModel(providerRow,m);}else if(p.type==='claude_cli')addSimpleModel(providerRow,{id:p.id+'-sonnet',name:'Claude Sonnet',model:'sonnet',enabled:true,default_effort:'high',allowed_efforts:['low','medium','high','xhigh','max'],supported_efforts:['low','medium','high','xhigh','max'],capability_source:'provider'});else if(p.type==='codex_cli')addSimpleModel(providerRow,{id:p.id+'-gpt-6-astra',name:'GPT-6-Astra',model:'gpt-6-astra',enabled:true,default_effort:'medium',allowed_efforts:codexModelEfforts['gpt-6-astra'],supported_efforts:codexModelEfforts['gpt-6-astra'],capability_source:'codex_cache'});
 }
 if(!(aiState.providers||[]).length)aiProvidersBox.append(el('p','settings-warning','AI 연결이 없습니다. 서버 기본 설정을 확인하세요.'));
 refreshDefaultModels();if(aiState.default_model_id&&[...defaultModel.options].some(o=>o.value===aiState.default_model_id))defaultModel.value=aiState.default_model_id;
 aiPanel.append(el('p','settings-session-note','서버 Agent는 별도 설정이 없습니다. AI 대화를 열면 사용자별 작업공간과 AGENT.md / AGENTS.md / CLAUDE.md를 자동 생성하며 실제 접근 권한은 서버가 검사합니다.'));
 const aiTestStatus=el('p','settings-session-note','연결 테스트를 실행하지 않았습니다.'),aiTestControls=el('div','settings-controls');aiTestLocal=action('설정 확인',()=>performAITest(false),'secondary');aiTestLive=action('실제 모델 테스트',()=>performAITest(true),'secondary');aiTestControls.append(aiTestLocal,aiTestLive);aiPanel.append(aiTestStatus,aiTestControls);

 const mail = section('mail', '네이버 메일', '제목 필터 → 수신 사용자 선택 → 저장 후 적용 → 메일 수집 테스트');
 check(mail, 'mail-enabled', '조건 일치 메일 자동 전달 켜기', s.mail.enabled, '체크 후 저장 후 적용해야 백그라운드 수집이 시작됩니다.');
 const mailGrid=el('div','settings-grid');mail.append(mailGrid);
 input(mailGrid,'mail-user','네이버 메일 ID','text',s.mail.username);
 input(mailGrid,'mail-password','앱 비밀번호 · 새 값만 입력','password','',snapshot.has_mail_password?'저장되어 있음 · 비우면 기존 비밀번호 유지':'네이버 앱 비밀번호를 입력하세요.').maxLength=1024;
 const folderInput=input(mailGrid,'mail-folder','메일 폴더','text',s.mail.folder||'INBOX');
 const folderList=el('datalist');folderList.id='mail-folder-list';folderInput.setAttribute('list',folderList.id);mail.append(folderList);
 const subjectFilters=area(mailGrid,'mail-subject-filter','제목에 포함할 문구 · 한 줄에 하나',s.mail.subject_filter??((s.mail.rules||[])[0]?.code_pattern||''),'최대 20개, 각 200자. 제목에 하나라도 포함되면 전달하며 여러 조건이 맞아도 한 번만 전달합니다. 빈 줄은 무시하며 영어 대소문자·연속 공백은 구분하지 않습니다. 쉼표는 문구의 일부입니다.');
 subjectFilters.rows=4;subjectFilters.maxLength=8020;subjectFilters.placeholder='로그인 코드\n이메일 인증';
 const recipientDetails=details(mail,'여러 사용자에게 같은 메일 전달');recipientDetails.open=true;
 recipientDetails.append(el('p','subtle','선택한 사용자 각각의 개인 수신함으로 제목과 본문을 전달합니다.'));
 const enabledUsers=users.filter(u=>!u.disabled),selectedNames=s.mail.recipient_usernames??[s.mail.owner_username];
 const recipientsBox=el('div','settings-recipient-list');recipientDetails.append(recipientsBox);const recipientChecks=[];
 for(const u of enabledUsers){const c=check(recipientsBox,'recipient-'+u.id,u.username,selectedNames.includes(u.username));recipientChecks.push({name:u.username,node:c});}
 if(!enabledUsers.length)recipientsBox.append(el('p','settings-warning','수신할 사용자를 서버 관리에서 먼저 추가하세요.'));
 const mailTiming=details(mail,'확인 간격·수동 검사 범위'),mailTimingGrid=el('div','settings-grid');mailTiming.append(mailTimingGrid);
 let mailNum=input(mailTimingGrid,'mail-poll','자동 확인 간격 (초)','number',s.mail.poll_seconds||15);mailNum.min='5';mailNum.max='3600';
 mailNum=input(mailTimingGrid,'mail-age','수동 검사: 최근 몇 분','number',s.mail.max_age_minutes||15,'최근 50개 후보 중 이 범위에 해당하는 메일을 다시 확인합니다.');mailNum.min='1';mailNum.max='60';
 const mailLive=el('p','settings-session-note');mailLive.id='mail-live-status';mail.append(mailLive);
 function renderMailStatus(status){mailLive.textContent='실행 서버 '+snapshot.version+' · 자동 전달 '+(status?.enabled?'켜짐':'꺼짐')+' · '+(status?.checked_at?new Date(status.checked_at*1000).toLocaleTimeString()+' / '+status.state:'아직 수집 실행 기록 없음')+(snapshot.pending_apply?' · 저장한 설정이 아직 적용되지 않았습니다.':'');}
 renderMailStatus(snapshot.mail_status);
 const discoveryStatus=el('pre','mail-diagnostics');discoveryStatus.id='mail-discovery-status';discoveryStatus.setAttribute('role','status');discoveryStatus.textContent='메일 수집 테스트를 누르면 설치된 서버 PC에서 로그인 → 메일 조회 → 제목 비교 → 본문 처리 → 실제 메시지 생성을 확인합니다.';
 const discoverFolders=action('메일 수집 테스트',performMailCollection,'primary');discoverFolders.id='mail-collect-test';
 const discoverRules=action('연결·폴더만 확인',performDiscovery,'secondary');discoverRules.id='mail-discover-folders';
 const mailActions=el('div','settings-inline-actions');mailActions.append(discoverFolders,discoverRules);mail.append(mailActions,discoveryStatus);
 mail.append(action('진단 결과 복사',async()=>{await navigator.clipboard.writeText(discoveryStatus.textContent);toast('진단 결과를 복사했습니다.');},'secondary'));
 mail.append(el('p','subtle','수집 테스트는 화면의 메일 설정으로 1회 실행하며 설정을 저장하지 않습니다. 이미 전달한 메일은 중복 생성하지 않습니다. 연결·폴더 확인만으로는 메시지가 전달되지 않습니다.'));
 const mailStatusTimer=setInterval(async()=>{if(!modal.isConnected)return;try{const state=await api('/settings');if(modal.isConnected){snapshot.pending_apply=state.pending_apply;renderMailStatus(state.mail_status);}}catch{}},10000);
 const rcState=s.remotecontrol||{},rcPanel=details(aiPanel,'RemoteControl MCP · 관리자 전용');
 rcPanel.append(el('p','subtle','서버 PC에 설치된 RemoteControl의 plugins 연결을 사용합니다. 일반 채팅 사용자는 이 도구를 호출할 수 없습니다.'));
 check(rcPanel,'rc-enabled','RemoteControl MCP 사용',rcState.enabled);
 const rcGrid=el('div','settings-grid');rcPanel.append(rcGrid);
 input(rcGrid,'rc-host','대상 PC의 LAN IPv4','text',rcState.host||'','같은 서버 PC라면 127.0.0.1');
 const rcPort=input(rcGrid,'rc-port','MCP 포트','number',rcState.port||5901);rcPort.min='1';rcPort.max='65535';
 input(rcGrid,'rc-user','Remote ID','text',rcState.username||'');
 input(rcGrid,'rc-password','Remote PW · 새 값만','password','',rcState.has_password?'저장된 비밀번호가 있습니다. 빈칸은 유지합니다.':'대상 RemoteControl의 비밀번호를 입력하세요.');
 check(rcPanel,'rc-clear','저장된 Remote PW 삭제',false);
 input(rcPanel,'rc-plugins','서버 PC의 plugins 폴더 · 비우면 자동 탐색','text',rcState.plugins_dir||'','기본: Program Files의 RemoteControl / plugins. 개인 계정 경로는 사용하지 않습니다.');
 rcPanel.append(el('p','settings-warning','대상 RemoteControl에서 LAN MCP와 허용 IP를 설정하세요. 파일·명령·화면 권한은 대상 설정을 따릅니다. 명령 실행은 대상 서비스 권한을 사용하며 사용자 작업공간 밖에 영향을 줄 수 있습니다. 연결은 신뢰하는 LAN에서 사용하세요.'));
 const rcStatus=el('p','settings-session-note','연결 테스트를 실행하지 않았습니다.');
 const collectRemoteControl=()=>({enabled:refs['rc-enabled'].checked,host:refs['rc-host'].value.trim(),port:Number(refs['rc-port'].value),username:refs['rc-user'].value.trim(),password:refs['rc-password'].value,clear_password:refs['rc-clear'].checked,plugins_dir:refs['rc-plugins'].value.trim()});
 const rcTest=action('MCP 연결 확인',async()=>{rcTest.disabled=true;rcStatus.textContent='대상 PC 상태를 조회하는 중…';try{const result=await api('/settings/test-remotecontrol','POST',{revision:snapshot.revision,remotecontrol:collectRemoteControl()});rcStatus.textContent=(result.ok?'연결 성공 · ':'연결 실패 · ')+result.message;}catch(e){rcStatus.textContent=e.message;}finally{rcTest.disabled=false;}},'secondary');
 rcPanel.append(rcStatus,rcTest);
 aiPanel.append(el('p','subtle','Codex와 Claude CLI는 공개 웹검색·웹페이지 확인을 지원합니다. 최신 정보는 검색 후 출처와 함께 답변합니다.'));

 const network = section('network', '외부 접속', '여러 사용자가 LTE·5G에서 접속할 공통 HTTPS 주소를 지정합니다. 사용자마다 별도 계정으로 로그인합니다.');

 const automatic = details(network, '도메인 HTTPS 자동 설정'); automatic.open = true;
 const ac = snapshot.auto_https || {};
 input(automatic,'https-domain','도메인','text',ac.domain||'','예: chat.ontechdev.com · https://와 포트 없이 입력').placeholder='chat.ontechdev.com';
 input(automatic,'https-email','인증서 연락 이메일 (선택)','email',ac.email||'');
 check(automatic,'https-terms',"Let's Encrypt 약관에 동의하고 이 도메인의 인증서 자동 발급·갱신을 신청합니다.",!!ac.accept_terms);
 const terms=el('a','','인증서 이용 약관');terms.href='https://letsencrypt.org/repository/';terms.target='_blank';terms.rel='noopener noreferrer';automatic.append(terms);
 automatic.append(el('p','subtle','Cloudflare A 레코드는 서버 공인 IP, 프록시는 DNS 전용으로 설정하세요. 인증서 발급과 접속에는 외부 TCP 443 → 이 서버 TCP 443 연결이 필요합니다.'));
 const httpsStatus=el('p','settings-session-note');httpsStatus.style.whiteSpace='pre-line';automatic.append(httpsStatus);
 let httpsCurrent=snapshot.https_status||{};
 function renderHTTPS(v){httpsCurrent=v;httpsStatus.textContent=(v.url?v.url+'\n':'')+(v.message||'설정 전')+(v.not_after?'\n인증서 만료: '+new Date(v.not_after).toLocaleString():'');}
 renderHTTPS(httpsCurrent);
 async function configureHTTPS(enabled){
  busy(true);httpsApply.disabled=true;httpsOff.disabled=true;setError('');
  try{
   if(enabled&&!refs['https-terms'].checked)throw new Error('인증서 이용 약관에 동의해 주세요.');
   if(!confirm(enabled?'도메인 HTTPS 설정만 저장하고 적용합니다. 기존 IP 접속은 유지됩니다. 다른 미저장 입력은 반영되지 않습니다. 진행할까요?':'도메인 자동 HTTPS 수신을 중지할까요? 기존 IP 접속과 저장된 인증서는 유지됩니다.'))return;
   const result=await api('/settings/https','POST',{revision:snapshot.revision,enabled,domain:refs['https-domain'].value.trim().toLowerCase(),email:refs['https-email'].value.trim(),accept_terms:refs['https-terms'].checked});
   banner.textContent='HTTPS 설정 적용 중…';await waitForSettingsApply(result.revision);await showSettings('network');
  }catch(e){setError(e.message);}finally{busy(false);httpsApply.disabled=false;httpsOff.disabled=false;}
 }
 const httpsApply=action('자동 HTTPS 설정·적용',()=>configureHTTPS(true),'primary');
 const httpsOff=action('자동 HTTPS 중지',()=>configureHTTPS(false),'secondary');httpsOff.hidden=!ac.enabled;
 const httpsCheck=action('인증서 다시 확인',async()=>{httpsCheck.disabled=true;try{renderHTTPS(await api('/settings/https/check','POST',{}));}catch(e){setError(e.message);}finally{httpsCheck.disabled=false;}},'secondary');
 httpsCheck.disabled=!httpsCurrent.enabled;
 const httpsFirewall=action('서버 Windows 방화벽에 443 허용',()=>{location.href='ontechchat-admin://https-firewall';},'secondary');
 httpsFirewall.disabled=!ac.enabled||!canOpenWindowsFirewallFromBrowser(location.hostname,snapshot.network?.windows_firewall_ui);
 const httpsUse=action('도메인 접속 주소 복사',async()=>{try{if(httpsCurrent.state!=='certificate_ready')throw new Error('먼저 인증서 확인을 완료하세요.');if(navigator.clipboard?.writeText)await navigator.clipboard.writeText(httpsCurrent.url);else{const t=document.createElement('textarea');t.value=httpsCurrent.url;modal.append(t);t.select();const ok=document.execCommand('copy');t.remove();if(!ok)throw new Error('표시된 도메인 주소를 직접 복사하세요.');}toast('도메인 주소를 복사했습니다.');}catch(e){setError(e.message);}},'secondary');
 automatic.append(httpsApply,httpsFirewall,httpsCheck,httpsUse,httpsOff);
 automatic.append(el('p','subtle','방화벽 버튼은 서버 PC의 트레이 → 서버 설정에서 사용할 수 있습니다. Windows 관리자 승인 후 443만 추가합니다. 공유기 포트 전달은 공유기에서 설정해야 합니다. 인증서 확인 후 휴대전화 LTE·5G로 도메인을 열어 보세요.'));
 const httpsTimer=setInterval(async()=>{if(!modal.open||!httpsCurrent.enabled)return;try{renderHTTPS(await api('/settings/https'));}catch{}},5000);
 modal.addEventListener('close',()=>clearInterval(httpsTimer),{once:true});
 input(network, 'public-url', '사용자에게 공유할 HTTPS 주소', 'url', s.public_url, '예: https://chat.example.com 또는 인증서가 준비된 https://공인IP:포트').placeholder = 'https://chat.example.com';
 const addressStatus = el('p', 'settings-network-status', s.public_url ? '주소가 저장되어 있습니다. 실제 LTE·5G 접속을 확인했다는 뜻은 아닙니다.' : '외부 주소 미설정 · 현재는 서버 PC에서만 접속합니다.'); network.append(addressStatus);
 const address = el('code', 'settings-share-address', s.public_url || '외부 주소를 입력하고 저장 후 적용하세요.'); network.append(address);
 const copyAddress = action('저장된 접속 주소 복사', async () => { try { if (!s.public_url) throw new Error('먼저 외부 HTTPS 주소를 저장하세요.'); if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(s.public_url); else { const t = document.createElement('textarea'); t.value = s.public_url; modal.append(t); t.select(); const ok = document.execCommand('copy'); t.remove(); if (!ok) throw new Error('표시된 주소를 직접 복사하세요.'); } toast('주소를 복사했습니다.'); } catch (e) { setError(e.message); } }, 'secondary'); network.append(copyAddress);
 network.append(el('p', 'settings-warning', '공유 주소 입력만으로 HTTPS가 생성되지는 않습니다. 위 자동 HTTPS 설정에서 인증서를 준비하세요. 127.0.0.1은 외부 사용자용 주소가 아닙니다. HTTP로 비밀번호나 인증번호를 공개하지 마세요.'));
 const mode = snapshot.network?.mode || 'local';
 const networkGuide = details(network, '기존 HTTPS 프록시를 사용하는 경우');
 networkGuide.append(el('p', 'subtle', '같은 PC의 HTTPS 프록시를 사용할 때는 서버 수신 주소 127.0.0.1:8787을 유지합니다. 프록시는 원래 Host를 전달하고 실시간 메시지 버퍼링을 끕니다.'));
 networkGuide.append(el('p', 'subtle', '공인 IP 직접 접속도 가능하지만 해당 IP에 유효한 인증서와 TLS 수신 설정이 필요합니다. 사설 IP·CGNAT·이중 공유기에서는 단순 포트 포워딩만으로 접속되지 않을 수 있습니다.'));
 networkGuide.append(el('p', 'subtle', '연결 준비 후 휴대전화 Wi-Fi를 끄고 공유 주소로 로그인·채팅·첨부파일을 확인하세요. 관리자 화면은 관리자 계정에만 표시됩니다.'));
 if (snapshot.network?.caddyfile) { networkGuide.append(el('b', '', '저장된 도메인 기준 Caddyfile 예제'), el('pre', '', snapshot.network.caddyfile), el('p', 'subtle', '예제만 제공합니다. Caddy 설치·DNS·공유기 설정은 별도이며 자동 실행하지 않습니다.')); }
 if (mode === 'direct_tls') networkGuide.append(el('p', 'subtle', '현재는 서버의 직접 TLS 설정을 사용합니다. 인증서·수신 주소 파일 설정은 유지합니다.'));
 network.append(el('p', 'settings-session-note', '관리자 계정을 공유하지 마세요. 「서버 관리 → 사용자 추가」 후 일반 채팅방에 초대합니다. 인증번호 수신은 메일 탭에서 별도로 선택합니다.'));


 const firewall = details(network, 'Windows 방화벽 · 수신 포트 진단'); firewall.open = true;
 firewall.id = 'network-firewall-panel';
 const diag = snapshot.active_network?.diagnostic || snapshot.network?.diagnostic;
 if (diag) {
  const facts = el('div', 'settings-overview');
  for (const [title,value] of [['실행 중 수신 주소',diag.listen],['외부 접속 포트',diag.public_port || '미설정'],['직접 TLS 설정',diag.tls_configured ? '경로 설정됨 (인증서 신뢰는 별도 확인)' : '없음'],['수신 범위',diag.loopback ? '서버 PC 내부만' : '외부 인터페이스 포함']]) facts.append(el('span','',title),el('code','',String(value)));
  firewall.append(facts);
  for (const text of diag.warnings || []) firewall.append(el('p','settings-warning',text));
 }
 firewall.append(el('p','subtle','Ping이 성공해도 TCP 포트나 HTTPS가 열렸다는 뜻은 아닙니다. 직접 공인 IP라면 공유기 설정 대신 서버 실제 수신 주소·Windows 방화벽·TLS를 확인합니다.'));
 const canOpenFirewall = canOpenWindowsFirewallFromBrowser(location.hostname, snapshot.network?.windows_firewall_ui);
 const openFirewall = action('Windows 방화벽 설정 열기', () => {
  if (!canOpenFirewall) return;
  if (!confirm('이 브라우저가 실행 중인 서버 PC의 Windows 관리 도구를 엽니다. UAC 승인 후 대상 포트를 확인하세요. 버튼만 눌러서는 방화벽이 변경되지 않습니다.')) return;
  location.href = 'ontechchat-admin://firewall';
 }, 'secondary');
 openFirewall.id='network-firewall-open'; openFirewall.disabled=!canOpenFirewall;
 firewall.append(openFirewall);
 firewall.append(el('p','subtle','서버 PC에서 새 설치본으로 업데이트한 뒤 사용하세요. 「외부 HTTPS 주소 → 저장 후 적용」 후 이 도구를 엽니다. 관리자 비밀번호 재입력은 없지만 Windows 관리자 승인과 단일 포트 허용 확인은 필요합니다.'));
 firewall.append(el('p','subtle','휴대전화·다른 PC에서는 원격 Windows 방화벽을 변경하지 않습니다. 서버 PC의 트레이 우클릭 → 방화벽·포트 설정 또는 시작 메뉴를 이용하세요.'));
 firewall.append(el('p','settings-warning','이 도구는 OnTechChat.Server.exe + 저장된 외부 주소의 TCP 포트 한 개만 허용/제거합니다. UDP·전체 포트·다른 앱·기본 방화벽 정책은 변경하지 않습니다. 별도 Caddy/nginx 프록시에는 그 실행 파일의 규칙이 필요합니다.'));

 const fcm = section('fcm', '푸시 알림', '앱이 닫혀 있을 때의 알림은 FCM을 사용합니다. 각 사용자의 앱 기기 등록은 별도로 필요합니다.');
 check(fcm, 'fcm-enabled', 'FCM 푸시 사용', s.fcm.enabled); input(fcm, 'fcm-project', 'Firebase 프로젝트 ID', 'text', s.fcm.project_id);
 const keyCard = el('div', 'settings-secret-card'), keyStatus = el('b', '', snapshot.has_fcm_key ? '발송 키가 저장되어 있습니다.' : '아직 발송 키가 없습니다.');
 keyCard.append(keyStatus, el('p', 'subtle', '새 키를 선택하지 않으면 저장된 키를 유지합니다. 비밀키 원문은 다시 표시하지 않습니다.'));
 const keyInput = field(keyCard, 'fcm-file', '새 서비스 계정 JSON 선택'); keyInput.type = 'file'; keyInput.accept = '.json,application/json'; fcm.append(keyCard);
 keyInput.addEventListener('change', async () => {
  serviceAccountJSON = ''; const file = keyInput.files[0]; if (!file) return; keyReading = true;
  try { if (file.size > 65536) throw new Error('Firebase 키 파일은 64KiB 이하여야 합니다.'); const text = await file.text(), v = JSON.parse(text); if (v.type !== 'service_account' || !v.client_email || !v.private_key) throw new Error('Firebase 서비스 계정 JSON이 아닙니다.'); serviceAccountJSON = text; keyStatus.textContent = '새 키 선택됨 · 저장 전'; if (!refs['fcm-project'].value && v.project_id) refs['fcm-project'].value = v.project_id; }
  catch (e) { keyInput.value = ''; setError(e.message); } finally { keyReading = false; }
 });
 check(fcm, 'fcm-clear', '저장된 Firebase 발송 키 삭제', false, 'FCM 푸시를 끈 상태에서만 삭제합니다.');
 const legacy = section('import', 'INI 가져오기', '기존 Setting.ini를 한 번 가져와 JSON에 저장합니다. 관리자 비밀번호를 다시 입력하지 않습니다.');
 const mappings = el('div', 'settings-mappings'); for (const text of ['MailID → 메일 ID','Password → 암호화 비밀번호','imapServer → 서버','Mailfolder → 폴더']) mappings.append(el('span', '', text)); legacy.append(mappings);
 legacy.append(el('p', 'settings-warning', '가져오면 메일 수집은 꺼집니다. 계정과 폴더를 확인하고 자동 설정 후 켜세요. 원본 INI 파일은 삭제하지 않습니다.'));
 const iniInput = field(legacy, 'ini-file', '기존 Setting.ini 선택'); iniInput.type = 'file'; iniInput.accept = '.ini,.txt';
 const importButton = action('INI 가져와 저장', performImport, 'secondary'); legacy.append(importButton);
 const footer = el('footer', 'settings-footer'); footer.append(el('p', 'settings-session-note', '관리자 세션 인증 중 · 저장 시 비밀번호 재입력 없음'));
 const error = el('p', 'settings-error'); error.id = 'settings-error'; error.setAttribute('role', 'alert');
 const controls = el('div', 'settings-controls'), reload = action('다시 읽기', async () => { if (confirm('미저장 입력을 버리고 다시 읽을까요?')) await showSettings(activeTab); }, 'secondary');
 const save = action('저장', () => performSave(false), 'secondary'); save.id = 'settings-save';
 const apply = action('저장 후 적용', () => performSave(true), 'primary'); apply.id = 'settings-apply';
 controls.append(reload, save, apply); footer.append(error, controls); modal.append(header, banner, notice, tabs, panels, footer); document.body.append(modal);
 function setError(text) { error.textContent = text; error.classList.remove('success'); }
 function busy(value) { for (const b of [reload, save, apply, discoverFolders, discoverRules, importButton, aiTestLocal, aiTestLive, rcTest]) if(b)b.disabled = value; }
 function collectAI(){
  const providers=aiProviderRows.map(x=>{const p=x.source;return {id:p.id,name:p.name,type:p.type,enabled:x.enabled.checked,timeout_seconds:p.timeout_seconds||120,concurrency:p.concurrency||1,base_url:x.base?x.base.value.trim():(p.base_url||''),api_key:x.key?x.key.value:'',clear_api_key:x.clear?x.clear.checked:false,has_api_key:!!p.has_api_key,executable:x.executable?(x.executable.value.trim()||(p.type==='codex_cli'?'codex':'claude')):(p.executable||''),work_dir:p.work_dir||''};});
  const models=aiModelRows.filter(x=>x.model.value.trim()).map(x=>({id:x.id,provider_id:x.providerID,name:x.name||x.model.value.trim(),model:x.model.value.trim(),enabled:x.enabled.checked,default_effort:x.effort.value||'provider_default',allowed_efforts:x.allowed,supported_efforts:x.supported,capability_source:x.source,capability_checked_at:x.checked}));
  return {enabled:refs['ai-enabled'].checked,default_model_id:refs['ai-default-model'].value,request_timeout_seconds:aiState.request_timeout_seconds||120,concurrency:aiState.concurrency||2,queue_limit:aiState.queue_limit||16,per_user_per_minute:aiState.per_user_per_minute||12,context_max_bytes:aiState.context_max_bytes||131072,system_prompt:aiState.system_prompt||'',providers,models};
 }
 function payload() {
  if (keyReading) throw new Error('키 파일을 읽는 중입니다. 잠시 후 저장하세요.');
  const recipients = [...new Set(recipientChecks.filter(c => c.node.checked).map(c => c.name))].filter(Boolean);
  const m={enabled:refs['mail-enabled'].checked,username:refs['mail-user'].value,password:refs['mail-password'].value,host:'imap.naver.com:993',folder:refs['mail-folder'].value,account:s.mail.account||'naver-main',recipient_usernames:recipients,subject_filter:refs['mail-subject-filter'].value.trim(),poll_seconds:Number(refs['mail-poll'].value),max_age_minutes:Number(refs['mail-age'].value)};
  return {revision:snapshot.revision, settings:{public_url:refs['public-url'].value, max_file_bytes:Math.round(Number(refs['max-file'].value)*1048576), storage_quota_bytes:Math.round(Number(refs.quota.value)*1073741824), session_hours:Number(refs.session.value), auth_retention_minutes:Number(refs.retention.value), max_messages:Number(refs.messages.value), mail:m, fcm:{enabled:refs['fcm-enabled'].checked, project_id:refs['fcm-project'].value, service_account_json:serviceAccountJSON, clear_key:refs['fcm-clear'].checked}, ai:collectAI(), remotecontrol:collectRemoteControl()}};
 }
 async function performSave(doApply) {
  busy(true); setError('');
  try {
   const request = payload(); request.apply = doApply;
   const result = await api('/settings', 'POST', request); refs['mail-password'].value = ''; refs['rc-password'].value = ''; serviceAccountJSON = ''; keyInput.value = '';
   if (doApply) { closeSettings(); toast('저장했습니다. 설정을 적용하는 중입니다.'); await waitForSettingsApply(result.revision); await showSettings(activeTab); toast('설정을 적용했습니다.'); }
   else { await showSettings(activeTab); toast('JSON에 저장했습니다. 실제 변경은 저장 후 적용을 눌러 반영하세요.'); }
  } catch (e) { setError(e.message); if (!modal.isConnected) toast(e.message); } finally { busy(false); }
 }
 async function performAITest(live) {
  const modelID=refs['ai-default-model'].value.trim();if(!modelID){setError('AI 기본 모델 ID를 먼저 입력하세요.');return;}
  const row=aiModelRows.find(x=>x.id===modelID),effort=row?.effort.value||'provider_default';
  if(live&&!confirm('실제 모델 호출은 계정 한도 또는 API 비용을 사용할 수 있습니다. 한 번 호출할까요?'))return;
  busy(true);setError('');aiTestStatus.textContent=live?'실제 모델 호출 중…':'설정과 실행 환경 확인 중…';
  try{const result=await api('/settings/test-ai','POST',{revision:snapshot.revision,ai:collectAI(),model_id:modelID,effort,live});if(result.ok){const found=result.resolved_executable?(' · 자동 발견='+result.resolved_executable):(result.executable_found===false?' · CLI 경로 자동 발견 안 됨':'');aiTestStatus.textContent=live?('실제 호출 성공 · sent='+((result.sent_effort)||'provider_default')+(result.observed_model?' · observed='+result.observed_model:'')):('설정 및 실행 파일 확인 완료'+found+' · 인증/모델 사용 권한은 실제 호출로 확인 · 저장하지 않음');aiTestStatus.classList.add('success');}else{aiTestStatus.textContent='사용 불가 · '+(result.error||(result.reason==='executable_missing'?'서버에서 CLI 경로를 찾지 못했습니다. CLI 실행 경로 항목에 이 PC의 실제 경로를 입력하세요.':result.reason==='server_login_required'?'공용 Codex 준비 완료 · 서버 PC 트레이 → 서버용 Codex 로그인에서 한 번 로그인하세요.':result.reason==='executable_access_denied'?'서버에서 CLI 경로에 접근할 수 없습니다. Codex는 최신 설치 파일을 다시 실행해 공용 CLI를 준비하세요.':result.reason)||result.error_code||'연결을 확인하세요.');aiTestStatus.classList.remove('success');}if(result.cli_diagnostics){const d=result.cli_diagnostics;aiTestStatus.textContent+=' · 서버='+d.server_host+' · 실행 계정='+d.server_account+' · 버전='+d.server_version;}}
  catch(e){aiTestStatus.textContent=e.message;setError(e.message);}finally{busy(false);}
 }
 async function performDiscovery(){
  busy(true);setError('');discoveryStatus.textContent='네이버 연결·폴더만 확인 중…';
  try{const result=await api('/settings/discover-mail','POST',payload());folderList.replaceChildren();for(const folder of result.folders||[])option(folderList,folder,folder);discoveryStatus.textContent=result.message+'\n메시지 전달은 「메일 수집 테스트」를 누르세요.';}
  catch(e){discoveryStatus.textContent=e.message;setError(e.message);}finally{busy(false);}
 }
 async function performMailCollection(){
  let request;
  try{request=payload();if(!request.settings.mail.subject_filter.replace(/[\s\u200b\ufeff]/g,''))throw new Error('제목 조건을 한 줄에 하나씩 입력하세요.');if(!request.settings.mail.recipient_usernames.length)throw new Error('메일 수신 사용자를 한 명 이상 선택하세요.');}
  catch(e){setError(e.message);return;}
  if(!confirm('화면에 입력한 제목 필터로 최근 메일을 다시 확인하고, 일치하는 제목과 본문을 선택한 사용자들의 개인 수신함으로 실제 전달합니다. 실행할까요?'))return;
  request.deliver=true;busy(true);setError('');const started=Date.now();
  const showWait=()=>{discoveryStatus.textContent='메일 수집 테스트 실행 중… '+Math.floor((Date.now()-started)/1000)+'초\n설치된 서버에서 로그인·메일 조회·메시지 생성을 확인합니다. 최대 약 80초가 걸릴 수 있습니다.';};showWait();const timer=setInterval(showWait,1000);
  try{const result=await api('/settings/collect-mail','POST',request);clearInterval(timer);
   const lines=['실행 서버 v'+result.version+' · 수동 수집 테스트', '설치 서버의 자동 전달: '+(result.automatic_enabled?'켜짐':'꺼짐'),'수신 사용자: '+(result.recipients||[]).join(', '),'단계: '+result.stage,result.message,...(result.steps||[])];
   for(const item of result.items||[])lines.push('UID '+item.uid+' · '+item.result+' · '+item.detail);
   lines.push('진단 결과에는 메일 원문·인증번호·비밀번호를 넣지 않습니다. 실제 내용은 각 사용자의 개인 수신함에서 확인하세요.');
   discoveryStatus.textContent=lines.join('\n');if(!result.ok)setError('진단 결과에서 실패 단계 또는 오류 항목을 확인하세요.');await refreshRooms();if(currentRoom?.kind==='auth')await reloadMessages();
  }catch(e){clearInterval(timer);discoveryStatus.textContent='수집 테스트 요청 실패: '+e.message;setError(e.message);}finally{clearInterval(timer);busy(false);}
 }
 async function performImport() {
  busy(true); setError('');
  try {
   const file = iniInput.files[0]; if (!file) throw new Error('Setting.ini 파일을 선택하세요.'); if (file.size > 65536) throw new Error('INI 파일은 64KiB 이하여야 합니다.');
   if (!confirm('메일 설정을 INI 값으로 저장하고 메일 수집을 끌까요? 다른 미저장 입력은 반영되지 않습니다.')) return;
   const bytes = new Uint8Array(await file.arrayBuffer()); let text;
   if (bytes[0] === 0xff && bytes[1] === 0xfe) text = new TextDecoder('utf-16le').decode(bytes); else if (bytes[0] === 0xfe && bytes[1] === 0xff) text = new TextDecoder('utf-16be').decode(bytes); else { try { text = new TextDecoder('utf-8', {fatal:true}).decode(bytes); } catch { text = new TextDecoder('euc-kr', {fatal:true}).decode(bytes); } }
   await api('/settings/import-ini', 'POST', {revision:snapshot.revision, ini_text:text}); iniInput.value = ''; await showSettings('mail'); toast('INI를 JSON으로 가져왔습니다. 서비스와 수신 사용자를 확인하세요.');
  } catch (e) { setError(e.message); } finally { busy(false); }
 }
 modal.addEventListener('close', () => { clearInterval(mailStatusTimer); serviceAccountJSON = ''; refs['mail-password'].value = ''; for(const row of aiProviderRows)if(row.key)row.key.value=''; keyInput.value = ''; iniInput.value = ''; if (settingsModal === modal) settingsModal = null; modal.remove(); });
 activate(initialTab); modal.showModal();
}
async function waitForSettingsApply(revision) {
 for (let i=0;i<80;i++) { await new Promise(r => setTimeout(r,350)); try { const v=await api('/settings'); if (v.revision===revision&&!v.pending_apply&&!v.applying) return; } catch(e) { if (!currentUser) throw e; } }
 throw new Error('설정은 저장됐지만 적용 확인 시간이 지났습니다. 서버 상태를 확인하세요.');
}
$('settings-button').addEventListener('click', () => showSettings().catch(e => toast(e.message)));
