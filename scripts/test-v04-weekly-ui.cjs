/* Native weekly workflow acceptance on an isolated loopback database. */
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {chromium}=require(process.env.WUBU_PLAYWRIGHT_MODULE||'playwright');
const base=process.env.WUBU_TEST_BASE_URL;
if(!/^http:\/\/127\.0\.0\.1:\d+$/.test(base||''))throw new Error('Disposable loopback service required');

(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
 page.on('pageerror',e=>errors.push(e.message));
 try{
  const post=async(route,data,key)=>{
   const r=await page.request.post(base+'/api/v1/'+route,{data,headers:{'Idempotency-Key':key}});
   assert(r.ok(),await r.text());return r.json();
  };
  const get=async route=>{const r=await page.request.get(base+'/api/v1/'+route);assert(r.ok(),await r.text());return r.json();};
  const balance=async a=>(await get('accounts/'+a.id)).current_balance;
  const wallet=await post('accounts',{name:'每周微信',type:'WALLET',initial_balance:100},'weekly-wallet');
  const ali=await post('accounts',{name:'每周支付宝',type:'WALLET',initial_balance:100},'weekly-ali');
  const bank=await post('accounts',{name:'每周银行',type:'BANK',initial_balance:200},'weekly-bank');
  await post('money/expenses',{account_id:wallet.id,amount:20,merchant:'午饭',occurred_at:'2026-11-01T12:05:00+08:00',source:'agent'},'weekly-lunch');

  const header='交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\r\n';
  const native=header+
   '2026-11-01 12:03:00,商户消费,龙记,,支出,20.00,零钱,支付成功,lunch,,\r\n'+
   '2026-11-01 18:00:00,商户消费,便利店,,支出,7.00,零钱,支付成功,order7,,\r\n'+
   '2026-11-01 19:00:00,商户消费,坏行,,支出,bad,零钱,支付成功,bad,,\r\n'+
   '2026-11-02 19:00:00,商户消费退款,便利店,,收入,2.00,零钱,已退款,order7,,\r\n';
  await page.goto(base+'/weekly-sync');
  const form=page.locator('#statement-form'),file=form.locator('[name=file]'),account=form.locator('[name=account_id]');
  const mapping=page.locator('#payment-mappings');
  await form.waitFor();
  assert.equal(await form.locator('[name=provider_code] option').count(),15);
  const upload=(name,buffer)=>file.setInputFiles({name,mimeType:name.endsWith('.xlsx')?'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet':'text/csv',buffer});
  const preview=async(code,action)=>{
   const wait=page.waitForResponse(r=>r.url().endsWith('/statements/'+code+'/preview'));
   await action();const r=await wait;assert(r.ok(),await r.text());
   await page.waitForFunction(()=>document.querySelector('#sync-message').textContent.includes('预览完成'));
   return r.json();
  };
  const commit=async()=>{
   await page.locator('[data-commit]').click();
   await page.getByText('已保存原始账单和处理结果。',{exact:true}).waitFor();
   await page.waitForFunction(()=>!document.querySelector('#sync-new').disabled);
  };
  let p=await preview('wechat',()=>upload('未标注来源.csv',Buffer.from(native)));
  assert.equal(p.batch.conflicts,4);
  assert.equal(await page.locator('#statement-provider').innerText(),'微信支付');
  assert.equal(await page.locator('#statement-rows').evaluate(el=>el.open),false);
  p=await preview('wechat',()=>mapping.locator('select').selectOption(wallet.id));
  assert.equal(p.batch.new_transactions,2);assert.equal(p.batch.matched_existing,1);assert.equal(p.batch.conflicts,1);
  assert.equal((await get('statements/wechat/payment-accounts')).payment_accounts['零钱'],undefined);
  await commit();
  assert.equal(await balance(wallet),75);
  assert.equal((await get('money/transactions?type=REFUND')).items.length,1);
  assert.equal((await get('statements/wechat/payment-accounts')).payment_accounts['零钱'],wallet.id);

  await page.reload();await form.waitFor();
  p=await preview('wechat',()=>upload('再次上传.csv',Buffer.from(native)));
  assert(p.already_imported);assert.equal(p.batch.new_transactions,0);assert.equal(p.batch.duplicates,3);
  assert.equal(await mapping.locator('select').inputValue(),wallet.id);
  await commit();assert.equal(await balance(wallet),75);

  p=await preview('alipay',()=>upload('支付宝原生.csv',Buffer.from('交易时间,交易分类,交易对方,对方账号,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号,备注\n2026-11-01 18:00:00,餐饮美食,小店,,商品未知,支出,5.00,余额,交易成功,ali-one,,\n')));
  assert.equal(p.batch.conflicts,1);
  p=await preview('alipay',()=>account.selectOption(ali.id));assert.equal(p.batch.new_transactions,1);await commit();
  await upload('农业银行原生.csv',Buffer.from('交易日期,交易时间,交易摘要,交易金额,本次余额,对手信息,日 志 号,交易渠道,交易附言\n20261101,123456,财付通,-3.00,197.00,银行商家,bank-one,电子商务,\n'));
  await form.locator('[name=provider_code]').waitFor();
  assert.equal(await form.locator('[name=provider_code]').inputValue(),'');
  p=await preview('abc_debit',()=>form.locator('[name=provider_code]').selectOption('abc_debit'));
  assert.equal(p.batch.conflicts,1);
  p=await preview('abc_debit',()=>account.selectOption(bank.id));assert.equal(p.batch.new_transactions,1);await commit();
  assert.equal(await balance(ali),95);assert.equal(await balance(bank),197);

  await page.locator('[data-balance="'+wallet.id+'"]').click();
  await page.locator('#sync-balance-form [name=actual_balance]').fill('95');
  await page.locator('#sync-balance-form [type=submit]').click();
  await page.getByText('真实余额已确认，未解释差额已保留。',{exact:true}).waitFor();
  assert.match(await page.locator('#sync-health').innerText(),/未解释差额/);

  const xlsx=fs.readFileSync(path.join(__dirname,'../internal/statement/testdata/wechat/example-wechat-records.xlsx'));
  p=await preview('wechat',()=>upload('微信原生.xlsx',xlsx));assert.equal(p.batch.row_count,27);
  p=await preview('wechat',()=>account.selectOption(wallet.id));assert(p.batch.new_transactions>0);await commit();
  const after=await balance(wallet);
  await page.reload();await page.locator('#sync-history a').first().waitFor();
  const summary=await page.locator('#sync-summary').innerText();
  assert.match(summary,/当前待确认/);assert.match(summary,/微信支付/);assert.match(summary,/支付宝/);assert.match(summary,/农业银行/);
  assert.equal(await balance(wallet),after);
  await page.setViewportSize({width:390,height:844});
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'weekly mobile overflow');
  assert.deepEqual(errors,[]);
  console.log('v1.0.1 Weekly UI PASS: CSV/XLSX recognition, auto-preview, mapping reuse, duplicate and refund, bank review, checkpoint and mobile.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
