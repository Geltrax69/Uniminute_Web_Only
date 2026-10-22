const {test}=require('node:test');
const assert=require('node:assert/strict');
const vm=require('node:vm');
const {readFileSync}=require('node:fs');
function component(fetcher=async()=>({ok:true,json:async()=>({saved:true,redirect:'/cart'})})){
 const source=readFileSync(new URL('../static/js/app.js',`file://${__filename}`),'utf8');
 const a=source.indexOf("  Alpine.data('profileSetup'"),b=source.indexOf('  // ── seller: store form',a);
 let factory,requests=0;
 vm.runInNewContext(source.slice(a,b),{Alpine:{data(_n,f){factory=f}},matchMedia:()=>({matches:true}),crypto:{randomUUID:()=> 'fixture-profile-123456'},URLSearchParams,AbortController,setTimeout,clearTimeout,fetch:async(...args)=>{requests++;return fetcher(...args)},window:{scrollTo(){}}});
 const data=factory({name:'',phone:'',line:'',label:'Home',city:'LPU',avatar:'everyday-shopper',interest:'everyday',next:'/cart',hasPassword:false,groups:[{id:'everyday',label:'Everyday shopper',avatars:[{id:'everyday-shopper'}]},{id:'gym',label:'Gym enthusiast',avatars:[{id:'gym-female'},{id:'gym-male'}]}]});
 data.$refs={};data.$nextTick=fn=>fn();data.$watch=()=>{};data.init();
 return {data,count:()=>requests};
}
test('guided steps validate name, Indian phone and optional password; progress reaches 100 only after save',async()=>{
 const {data}=component();assert.equal(data.progress,20);data.chooseInterest('gym');assert.equal(data.options.length,2);assert.equal(data.avatar,'gym-female');
 await data.advance();assert.equal(data.step,1);await data.advance();assert.equal(data.step,1);assert(data.error);
 data.name='Buyer';await data.advance();data.phone='123';await data.advance();assert.equal(data.step,2);
 data.phone='+91 9876543210';await data.advance();assert.equal(data.step,3);
 data.password='short';await data.advance();assert.equal(data.step,3);
 data.password='';await data.advance();assert.equal(data.progress,95);assert.equal(data.complete,false);
 data.line='BH-9, Room 214';await data.advance();assert.equal(data.progress,100);assert.equal(data.complete,true);assert.equal(data.password,'');
});
test('double clicks send one save; a failed save keeps the entered details and supports retry',async()=>{
 let release;
 const {data,count}=component(()=>new Promise(resolve=>{release=resolve}));
 Object.assign(data,{step:4,name:'Buyer',phone:'9876543210',line:'BH-9, Room 214',password:'fixture-password'});
 const request=data.advance();await data.advance();assert.equal(count(),1);assert.equal(data.saving,true);assert.equal(data.complete,false);
 release({ok:false,json:async()=>({error:'Delivery is unavailable'})});await request;
 assert.equal(data.complete,false);assert.equal(data.line,'BH-9, Room 214');assert.equal(data.error,'Delivery is unavailable');assert.equal(data.saving,false);
});
test('preexisting passwords are omitted and the profile save has a stable retry key',async()=>{
 let payload;
 const {data}=component(async(_url,options)=>{payload=options.body;return {ok:true,json:async()=>({saved:true,redirect:'/cart'})}});
 Object.assign(data,{step:4,name:'Buyer',phone:'9876543210',line:'BH-9',password:'should-not-send',hasPassword:true});
 await data.advance();assert.equal(payload.has('password'),false);assert.equal(payload.get('setupRequestId'),'fixture-profile-123456');
});

test('mobile input keeps ten digits, normalizes pasted country codes, and masks the card',()=>{
 const {data}=component();
 data.setPhone('70008679961234567890');assert.equal(data.phone,'7000867996');assert.equal(data.displayPhone,'+91 70XXXX7996');
 data.setPhone('+91 70008 67996');assert.equal(data.phone,'7000867996');
 data.setPhone('917000867996');assert.equal(data.phone,'7000867996');
 data.setPhone('ab70-0086.7996');assert.equal(data.phone,'7000867996');
 data.setPhone('700');assert.equal(data.displayPhone,'+91 70X');
 data.setPhone('');assert.equal(data.displayPhone,'');
});
test('pasting replaces the selected digits before the browser maxlength can truncate a +91 number',()=>{
 const {data}=component();data.setPhone('9876543210');let prevented=false,caret;
 const input={selectionStart:0,selectionEnd:10,value:data.phone,setSelectionRange(start,end){caret=[start,end]}};
 data.pastePhone({target:input,clipboardData:{getData:()=>'+91 70008 67996'},preventDefault(){prevented=true}});
 assert.equal(prevented,true);assert.equal(input.value,'7000867996');assert.deepEqual(caret,[10,10]);assert.equal(data.displayPhone,'+91 70XXXX7996');
 input.selectionStart=2;input.selectionEnd=6;data.pastePhone({target:input,clipboardData:{getData:()=> '1234'},preventDefault(){}});
 assert.equal(data.phone,'7012347996');assert.equal(data.displayPhone,'+91 70XXXX7996');
 data.step=2;data.setPhone('1234567890');assert.match(data.validation(),/valid 10-digit/);
});
