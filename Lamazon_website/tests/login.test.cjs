const {test}=require('node:test');
const assert=require('node:assert/strict');
const vm=require('node:vm');
const {readFileSync}=require('node:fs');
function component(){
 const source=readFileSync(new URL('../static/js/app.js',`file://${__filename}`),'utf8');
 const begin=source.indexOf("  Alpine.data('login'");
 const end=source.indexOf("  Alpine.data(",begin+20);
 let factory,now=100000,callback,cleared=0;
 vm.runInNewContext(source.slice(begin,end),{Alpine:{data(_name,f){factory=f}},Date:{now:()=>now},setInterval(fn){callback=fn;return 1},clearInterval(){cleared++}});
 return {factory,advance(ms){now+=ms;if(callback)callback()},cleared:()=>cleared};
}
test('resend countdown follows elapsed time, handles sleeping tabs and clears its timer',()=>{
 const c=component(),data=c.factory('code','new@example.com','',130000);
 data.init();assert.equal(data.remaining,30);
 c.advance(1000);assert.equal(data.remaining,29);
 c.advance(60000);assert.equal(data.remaining,0);assert.equal(c.cleared(),1);
 data.destroy();assert.equal(c.cleared(),1);
});
test('swapping login cards clears pending timers; delivery errors override sent-code copy',()=>{
 const c=component(),data=c.factory('reset','new@example.com','We could not send your email.',130000);
 data.init();assert.equal(data.helper,'We could not send your email.');
 data.destroy();assert.equal(c.cleared(),1);
 const next=c.factory('code','new@example.com','',0);next.init();assert.equal(next.remaining,0);
});
