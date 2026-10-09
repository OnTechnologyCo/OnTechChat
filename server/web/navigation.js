// Called by Android back navigation; false means the app is at its main screen.
function ontechNavigateBack(){
 const settings=document.querySelector('#settings-dialog[open]');
 if(settings){closeSettings();switchTab('friends');history.replaceState({tab:'friends'},'','/');return true;}
 const dialog=document.querySelector('dialog[open]');
 if(dialog){dialog.close();return true;}
 if(currentRoom){closeConversation();switchTab('chats',false);return true;}
 if(currentTab==='preferences'){switchTab('friends');history.replaceState({tab:'friends'},'','/');return true;}
 return false;
}
