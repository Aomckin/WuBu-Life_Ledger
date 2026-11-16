const assert=require('node:assert/strict');
const {chromium}=require(process.env.WUBU_PLAYWRIGHT_MODULE||'playwright');
const base=process.env.WUBU_TEST_BASE_URL;
if(!/^http:\/\/127\.0\.0\.1:\d+$/.test(base||''))throw new Error('Disposable loopback service required');

(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
 page.on('pageerror',e=>errors.push(e.message));
 try{
  const post=async(path,data,key)=>{
   const r=await page.request.post(base+'/api/v1/'+path,{data,headers:{'Idempotency-Key':key}});
   assert(r.ok(),await r.text());return r.json();
  };
  const get=async path=>{const r=await page.request.get(base+'/api/v1/'+path);assert(r.ok(),await r.text());return r.json();};
  const balance=async id=>(await get('accounts/'+id)).current_balance;
  const submit=async()=>{
   await page.locator('#review-form [type=submit]').click();
   await page.locator('#financial-dialog').waitFor({state:'hidden'});
  };
  const a=await post('accounts',{name:'UI 银行',type:'BANK',initial_balance:100},'finance-account');
  const b=await post('accounts',{name:'候选账户',type:'BANK',initial_balance:100},'candidate-account');
  const at='2026-11-02T04:00:00Z';
  const evt={provider:'WECHAT',capture_source:'CSV_IMPORT',occurred_at:at,direction:'EXPENSE'};
  const booked=await post('financial/events',{...evt,amount:20,account_id:a.id,merchant_raw:'便利店',raw_text:'支付20元'},'booked');
  await page.goto(base+'/financial-inbox?transaction='+booked.transaction_id);
  await page.getByRole('button',{name:'编辑说明'}).click();
  assert.equal(await page.locator('#review-form [name=merchant]').inputValue(),'便利店');
  assert.equal(await page.locator('#review-form [name=amount],[name=account_id],[name=reason]').count(),0);
  await page.locator('#review-form [name=category]').fill('餐饮');
  const change=page.waitForRequest(req=>req.method()==='PATCH'&&req.url().endsWith('/details'));
  await submit();
  assert.deepEqual((await change).postDataJSON(),{category:'餐饮'});
  assert.equal(await balance(a.id),80);
  const saved=(await get('financial/transactions?transaction_id='+booked.transaction_id)).items[0];
  assert.equal(saved.amount,20);assert.equal(saved.merchant,'便利店');
  assert.equal(saved.annotation.category,'餐饮');

  const missing=await post('financial/events',{...evt,amount:7,merchant_raw:'便利店',raw_text:'另一笔7元'},'missing');
  const candidates=[];
  for(const key of ['one','two']){
   const result=await post('money/expenses',{account_id:b.id,amount:3,merchant:key==='one'?'饮水店':'便民店',occurred_at:at,source:'manual'},key);
   candidates.push(result.transactions[0]);
  }
  const dup=await post('financial/events',{...evt,account_id:b.id,amount:3,raw_text:'原始账单同额候选'},'duplicate');
  assert.equal(dup.candidates.length,2);
  const refund=await post('financial/events',{...evt,account_id:a.id,amount:14.50,occurred_at:'2026-11-03T00:00:00Z',direction:'REFUND',review_reason:'待确认原支出',raw_text:'退款到账'},'refund-event');
  const ignored=await post('financial/events',{provider:'WECHAT',capture_source:'MANUAL',raw_text:'忽略示例'},'ignored');
  await page.getByRole('button',{name:'待确认',exact:true}).click();
  const target=id=>page.locator('[data-review="'+id+'"]');
  await target(missing.id).click();
  assert.equal(await page.locator('#financial-dialog select').count(),1);
  assert.equal(await page.locator('#financial-dialog input').count(),0);
  await page.locator('#financial-dialog [name=account_id]').selectOption(a.id);await submit();
  assert.equal(await balance(a.id),73);
  await target(dup.id).click();
  const choices=page.locator('#financial-dialog [name=transaction_id]');
  assert.equal(await choices.inputValue(),'');
  assert.match(await choices.innerText(),/饮水店.*¥3/s);
  await choices.selectOption({label:await choices.locator('option').filter({hasText:'便民店'}).innerText()});
  await submit();assert.equal(await balance(b.id),94);
  assert((await get('financial/events?status=PROCESSED')).items.some(r=>r.id===dup.id&&r.transaction_id===candidates[1].id));
  await target(refund.id).click();
  const original=page.locator('#financial-dialog [name=related_transaction_id]');
  const label=original.locator('option').filter({hasText:'便利店'});
  await label.waitFor({state:'attached'});
  await original.selectOption({label:await label.innerText()});
  await submit();assert.equal(await balance(a.id),87.5);
  await target(ignored.id).click();
  await page.locator('#review-ignore').click();await submit();
  assert((await get('financial/events?status=PROCESSED')).items.some(r=>r.id===ignored.id&&r.status==='IGNORED'&&r.decision==='CONFIRMED_IGNORE'));

  await page.goto(base+'/financial-inbox?transaction='+booked.transaction_id);
  await page.getByRole('button',{name:'校正资金'}).click();
  const funding=page.locator('#review-form');
  assert.equal(await funding.locator('[name=amount]').inputValue(),'20');
  await funding.locator('[name=amount]').fill('19');
  await submit();assert.equal(await balance(a.id),88.5);
  await page.setViewportSize({width:390,height:844});
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'financial mobile overflow');
  assert.deepEqual(errors,[]);
  console.log('v1.0.1 Finance UI PASS: semantic-only edit, account-only review, duplicate selection, eligible refund, automatic audit reason, funding correction, mobile.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
