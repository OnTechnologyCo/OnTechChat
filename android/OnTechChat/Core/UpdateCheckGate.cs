namespace OnTechChat;
// 중복 resume/page-finished 및 설치 대기 경로도 하나의 작업으로 직렬화합니다.
public sealed class UpdateCheckGate
{
 public bool Busy {get;private set;}
 private string account="";
 private long lastAutomatic=-1,offeredVersion;
 public bool TryBegin(string key,long elapsed,bool manual,bool screenReady){
  if(Busy||!screenReady)return false;
  if(account!=key){account=key;lastAutomatic=-1;offeredVersion=0;}
  if(!manual&&lastAutomatic>=0&&elapsed-lastAutomatic<60000)return false;
  Busy=true;if(!manual)lastAutomatic=elapsed;return true;
 }
 public bool ShouldOffer(long candidate,long installed,bool manual,string deferredAccount,long deferredVersion)
  =>candidate>installed&&(manual||(candidate!=offeredVersion&&(account!=deferredAccount||candidate>deferredVersion)));
 public void Offered(long version)=>offeredVersion=version;
 public void Finish()=>Busy=false;
}
