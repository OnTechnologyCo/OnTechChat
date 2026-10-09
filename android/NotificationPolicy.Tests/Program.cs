using OnTechChat;
using System.Text;
using System.Text.Json;
int passed=0;
void Check(string name,Action run){run();passed++;Console.WriteLine("PASS "+name);}
Check("Firebase tag",()=>{if(!NotificationPolicy.MessageID("message-123",out var id)||id!=123)throw new Exception();});
foreach(var tag in new string?[]{null,"local-test","message-0","message--1","message-+2","message-123\n","message-9223372036854775808","message-12x","some-message-1"})
 Check("unrelated invalid tag "+(tag??"null"),()=>{if(NotificationPolicy.MessageID(tag,out _))throw new Exception();});
Check("confirmed subset",()=>{using var d=JsonDocument.Parse("{\"version\":1,\"dismiss_message_ids\":[2,4]}");if(!NotificationPolicy.ConfirmedDismissals(d.RootElement,new long[]{1,2,3,4}).SetEquals(new long[]{2,4}))throw new Exception();});
Check("unrequested id rejected",()=>{using var d=JsonDocument.Parse("{\"version\":1,\"dismiss_message_ids\":[99]}");try{NotificationPolicy.ConfirmedDismissals(d.RootElement,new long[]{2});throw new Exception();}catch(IOException){}});
Check("unknown protocol rejected",()=>{using var d=JsonDocument.Parse("{\"version\":2,\"dismiss_message_ids\":[]}");try{NotificationPolicy.ConfirmedDismissals(d.RootElement,Array.Empty<long>());throw new Exception();}catch(IOException){}});
foreach(var kind in new[]{"auth","unknown","deleted","expired"})Check("preview hidden "+kind,()=>{var v=NotificationPolicy.Display(kind,true,"private-title","private-text-012345");if(v.Body.Contains("private")||v.Title.Contains("private"))throw new Exception();});
Check("preview opt in",()=>{var v=NotificationPolicy.Display("text",true,"친구","안녕\n 하세요");if(v.Title!="친구"||v.Body!="안녕 하세요")throw new Exception();});
Check("preview opt out",()=>{var v=NotificationPolicy.Display("text",false,"private","private");if(v.Body.Contains("private")||v.Title.Contains("private"))throw new Exception();});
Check("unicode bound",()=>{var v=NotificationPolicy.Clean(string.Concat(Enumerable.Repeat("😀",120)),80);if(v.EnumerateRunes().Count()!=81||v.Contains('\uFFFD'))throw new Exception();});
Check("bidi and control removed",()=>{if(NotificationPolicy.Clean("a\u202eb\n c",80)!="a b c")throw new Exception();});
Check("account scope",()=>{var a=new LoginSession("https://chat.example.com/","t","u","user1",4102444800);if(NotificationPolicy.AccountKey(a)==NotificationPolicy.AccountKey(a with{UserId="user2"})||NotificationPolicy.AccountKey(a)==NotificationPolicy.AccountKey(a with{Origin="https://other.example.com/"}))throw new Exception();});
Console.WriteLine("TOTAL_PASSED="+passed);
