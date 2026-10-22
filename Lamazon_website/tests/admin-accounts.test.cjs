const {test}=require('node:test');const assert=require('node:assert/strict');const vm=require('node:vm');const {readFileSync}=require('node:fs');
function panel(send=async()=>({deleted:true})){
 const source=readFileSync(new URL('../static/js/app.js',`file://${__filename}`),'utf8');
 const a=source.indexOf("  Alpine.data('adminPanel'"),b=source.indexOf("  Alpine.data(",a+24);
 let factory,requests=[],reloads=[];const dialog={showModal(){},close(){}};
 vm.runInNewContext(source.slice(a,b),{Alpine:{data(_n,f){factory=f}},sellerCall:async(method,path,body)=>{requests.push({method,path,body});return send()},lwReload:message=>reloads.push(message),lwToast(){}});
 const data=factory({});data.$refs={dlg:dialog};data.$nextTick=fn=>fn();
 return {data,requests,reloads};
}
test('buyer deletion needs exact email confirmation and password; errors preserve the dialog and clear password',async()=>{
 const {data,requests}=panel(async()=>{throw new Error('Active order blocks deletion')});
 data.openBuyerDelete('LMZ-test','buyer@example.com');await data.deleteBuyer();assert.equal(requests.length,0);
 data.dlg.confirmEmail='other@example.com';data.dlg.password='fixture';await data.deleteBuyer();assert.equal(requests.length,0);
 data.dlg.confirmEmail='buyer@example.com';await data.deleteBuyer();assert.equal(requests.length,1);
 assert.equal(data.dlg.error,'Active order blocks deletion');assert.equal(data.dlg.password,'');assert.equal(data.dlg.kind,'delete-buyer');
});
test('rapid deletion clicks send one request and clear credentials after success',async()=>{
 let resolve;const {data,requests,reloads}=panel(()=>new Promise(r=>{resolve=r}));
 data.openBuyerDelete('LMZ-test','buyer@example.com');data.dlg.confirmEmail='buyer@example.com';data.dlg.password='fixture';
 const first=data.deleteBuyer();await data.deleteBuyer();assert.equal(requests.length,1);resolve({deleted:true});await first;
 assert.equal(data.dlg.password,'');assert.equal(reloads.length,1);
});
test('store suspension requires a reason and restoration uses a separate endpoint',()=>{
 const {data}=panel();data.suspendStore('seller@example.com','A store');assert.equal(data.dlg.kind,'suspend-store');assert.equal(data.dlg.ready(),false);
 data.dlg.value='Verification required';assert.equal(data.dlg.ready(),true);
 data.restoreStore('seller@example.com','A store');assert.equal(data.dlg.kind,'confirm');assert.equal(data.dlg.action,'Restore store');
});
