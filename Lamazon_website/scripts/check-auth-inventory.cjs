// Browser integration check using a local mock API (no real account or uploads).
// Start the site with PORT=18100 API_BASE=http://127.0.0.1:18101 go run ./cmd/server
// Then run this file with Playwright installed or PLAYWRIGHT_MODULE pointing to it.
// CHROME_PATH optionally selects an existing Chrome executable.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const http = require('node:http');
const assert = require('node:assert/strict');
let failUpload = false, uploads = 0;
const api = http.createServer((req,res) => {
 let raw=''; req.on('data', d=>raw+=d); req.on('end',()=>{
 let data=[];
 if(req.url==='/api/login') data={needsPassword:true,email:'test@example.com'};
 else if(req.url==='/api/login/password') data={token:'test-session',refreshToken:'test-refresh',expiresIn:3600,email:'test@example.com'};
 else if(req.url==='/api/me') data={email:'test@example.com',name:'Test Seller',ready:true,hasStore:true};
 else if(req.url==='/api/seller/store') data={name:'Test Store',status:'approved',categories:['Grocery']};
 else if(req.url==='/api/categories') data=[{name:'Grocery',children:[]}];
 else if(req.url==='/api/season') data={};
 else if(req.url==='/api/seller/items' && req.method==='POST') { uploads++; data=failUpload?{error:'Photo storage is temporarily unavailable.'}:{id:'new-product'}; }
 const delay=req.method==='POST'?600:0;
 setTimeout(()=>{res.writeHead(failUpload && req.url==='/api/seller/items' && req.method==='POST'?503:200,{'Content-Type':'application/json'}); res.end(JSON.stringify(data));},delay);
 });
});
(async()=>{
 await new Promise(r=>api.listen(18101,'127.0.0.1',r));
 const browser=await chromium.launch({headless:true,...(process.env.CHROME_PATH ? {executablePath:process.env.CHROME_PATH} : {})});
 const page=await browser.newPage({viewport:{width:390,height:844}});
 page.setDefaultTimeout(10000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('https://checkout.razorpay.com/**',r=>r.abort());
 const base='http://127.0.0.1:18100';
 await page.goto(base+'/login?next=/account');
 await page.locator('input[name=email]').fill('test@example.com');
 await page.getByRole('button',{name:'Continue',exact:true}).click();
 
 await page.locator('input[name=password]').waitFor();
 await page.locator('input[name=password]').fill('password123');
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await page.waitForURL(base+'/account');
 assert.match(await page.locator('body').innerText(),/Test Seller/);
 console.log('PASS login displays account without refresh');
 await page.getByRole('button',{name:'Log out',exact:true}).click();
 await page.locator('#lw-confirm').getByRole('button',{name:'Log out',exact:true}).click();
 await page.waitForFunction(()=>!document.cookie.includes('unused') && !document.querySelector('form[action="/logout"]'));
 assert.doesNotMatch(await page.locator('body').innerText(),/Test Seller/);
 await page.goBack();
 await page.waitForTimeout(300);
 assert.doesNotMatch(await page.locator('body').innerText(),/Test Seller/);
 console.log('PASS logout and Back cannot restore signed-in account');
 await page.context().addCookies([{name:'lw_at',value:'test-session',url:base}]);
 await page.goto(base+'/seller/products/new');
 await page.waitForFunction(()=>window.Alpine && document.querySelector('form[x-data]')?._x_dataStack);
 const oversized=await page.evaluate(async()=>{try{await lwPreparePhoto(new File([new Uint8Array(21*1024*1024)],'large.jpg',{type:'image/jpeg'}));return '';}catch(e){return e.message;}});
 assert.match(oversized,/too large/);
 const invalid=await page.evaluate(async()=>{try{await lwPreparePhoto(new File(['bad'],'bad.jpg',{type:'image/jpeg'}));return '';}catch(e){return e.message;}});
 assert.match(invalid,/could not be read/);
 const image=await page.evaluate(async()=>{const c=document.createElement('canvas');c.width=3200;c.height=2400;c.getContext('2d').fillRect(0,0,3200,2400);const b=await new Promise(r=>c.toBlob(r,'image/png'));const f=new File([b],'photo.png',{type:'image/png'});const result=await lwPreparePhoto(f);const im=await createImageBitmap(result);return {width:im.width,height:im.height,size:result.size,bytes:Array.from(new Uint8Array(await f.arrayBuffer()))};});
 assert.equal(image.width,2048);assert.equal(image.height,1536);
 await page.locator('input[type=file]').setInputFiles({name:'large.jpg',mimeType:'image/jpeg',buffer:Buffer.alloc(21*1024*1024)});
 await page.locator('#lw-result[open]').waitFor();
 assert.match(await page.locator('#lw-result').innerText(),/too large/);
 await page.locator('#lw-result button').click();
 await page.waitForFunction(()=>!Alpine.$data(document.querySelector('form[x-data]')).busy);
 await page.locator('input[type=file]').setInputFiles({name:'photo.png',mimeType:'image/png',buffer:Buffer.from(image.bytes)});
 await page.locator('#field-title').fill('Test product');await page.locator('#field-price').fill('100');await page.locator('#field-stock').fill('5');
 await page.waitForFunction(()=>!Alpine.$data(document.querySelector('form[x-data]')).busy);
 console.log('PASS image validation, resizing and photo selection');
 failUpload=true;
 await page.getByRole('button',{name:'Add to inventory',exact:true}).click();
 await page.locator('#lw-result[open]').waitFor();
 assert.match(await page.locator('#lw-result').innerText(),/Product could not be saved/);
 await page.screenshot({path:'/tmp/uniminute-inventory-error-mobile.png'});
 await page.locator('#lw-result button').click();
 assert.equal(await page.locator('#field-title').inputValue(),'Test product');
 failUpload=false;
 await page.getByRole('button',{name:'Add to inventory',exact:true}).click();
 await page.locator('[x-data="processing()"]').waitFor({state:'visible'});
 await page.evaluate(()=>Alpine.$data(document.querySelector('form[x-data]')).save());
 await page.locator('#lw-result[open]').waitFor();
 assert.match(await page.locator('#lw-result').innerText(),/Added to inventory/);
 assert.equal(uploads,2);
 await page.screenshot({path:'/tmp/uniminute-inventory-success-mobile.png'});
 await page.setViewportSize({width:1440,height:1000});
 await page.screenshot({path:'/tmp/uniminute-inventory-success-desktop.png'});
 console.log('PASS uploading, failure preserves form, retry succeeds');
 assert.deepEqual(errors,[]);
 await browser.close();api.close();
 console.log('PASS no browser JavaScript errors');
})().catch(e=>{console.error(e);api.close();process.exit(1)});
