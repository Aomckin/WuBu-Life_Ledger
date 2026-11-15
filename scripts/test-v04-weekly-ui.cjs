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
 page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>{if(m.type()==='error')errors.push(m.text());});
 try {
  const post=async(route,data,key)=>{const r=await page.request.post(base+'/api/v1/'+route,{data,headers:{'Idempotency-Key':key}});assert(r.ok(),await r.text());return r.json();};
  const wallet=await post('accounts',{name:'每周微信',type:'WALLET',initial_balance:100},'weekly-wallet');
  const ali=await post('accounts',{name:'每周支付宝',type:'WALLET',initial_balance:100},'weekly-ali');
  const bank=await post('accounts',{name:'每周银行',type:'BANK',initial_balance:200},'weekly-bank');
  await post('money/expenses',{account_id:wallet.id,amount:20,merchant:'午饭',occurred_at:'2026-11-01T12:05:00+08:00',source:'agent'},'weekly-lunch');
  const header='交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\r\n';
  const native=header+'2026-11-01 12:03:00,商户消费,龙记,,支出,20.00,零钱,支付成功,lunch,,\r\n2026-11-01 18:00:00,商户消费,便利店,,支出,7.00,零钱,支付成功,unknown,,\r\n2026-11-01 19:00:00,商户消费,坏行,,支出,bad,零钱,支付成功,bad,,\r\n';
  await page.goto(base+'/weekly-sync');await page.locator('#wechat-statement').waitFor();
  assert.equal(await page.locator('#bank-statement [name=provider_code] option').count(),14);
  assert.equal(await page.locator('textarea').count(),0);
  await page.locator('#wechat-statement [name=account_id]').selectOption(wallet.id);
  const selectFile=async(source,name,buffer)=>page.locator(`#${source}-statement [name=file]`).setInputFiles({name,mimeType:name.endsWith('.xlsx')?'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet':'text/csv',buffer});
  const preview=async source=>{const response=page.waitForResponse(r=>r.url().includes('/statements/')&&r.url().endsWith('/preview'));await page.locator(`#${source}-statement [type=submit]`).click();const r=await response;assert(r.ok(),await r.text());await page.locator(`[data-commit=${source}]`).waitFor();return r.json();};
  const commit=async source=>{await page.locator(`[data-commit=${source}]`).click();await page.locator(`#${source}-preview`).getByText('已保存原始账单和处理结果。',{exact:true}).waitFor();};
  await selectFile('wechat','微信原生.csv',Buffer.from(native));let p=await preview('wechat');assert.equal(p.batch.new_transactions,1);assert.equal(p.batch.matched_existing,1);assert.equal(p.batch.conflicts,1);
  await page.locator('[data-map-source=wechat]').first().selectOption(wallet.id);assert(await page.locator('[data-commit=wechat]').isDisabled());p=await preview('wechat');await commit('wechat');
  await selectFile('wechat','微信原生.csv',Buffer.from(native));p=await preview('wechat');assert(p.already_imported);assert.equal(p.batch.new_transactions,0);assert.equal(p.batch.duplicates,2);await commit('wechat');
  await page.locator('#alipay-statement [name=account_id]').selectOption(ali.id);
  await selectFile('alipay','支付宝原生.csv',Buffer.from('交易时间,交易分类,交易对方,对方账号,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号,备注\n2026-11-01 18:00:00,餐饮美食,小店,,商品未知,支出,5.00,余额,交易成功,ali-one,,\n'));
  p=await preview('alipay');assert.equal(p.batch.new_transactions,1);await commit('alipay');
  await page.locator('#bank-statement [name=account_id]').selectOption(bank.id);
  await selectFile('bank','农业银行原生.csv',Buffer.from('交易日期,交易时间,交易摘要,交易金额,本次余额,对手信息,日 志 号,交易渠道,交易附言\n20261101,123456,财付通,-3.00,197.00,银行商家,bank-one,电子商务,\n'));
  p=await preview('bank');assert.equal(p.batch.new_transactions,1);await commit('bank');
  let balances=(await(await page.request.get(base+'/api/v1/accounts')).json()).items;
  assert.equal(balances.find(x=>x.id===wallet.id).current_balance,73);assert.equal(balances.find(x=>x.id===ali.id).current_balance,95);assert.equal(balances.find(x=>x.id===bank.id).current_balance,197);
  await page.locator(`[data-balance="${wallet.id}"]`).click();await page.locator('#sync-balance-form [name=actual_balance]').fill('95');await page.locator('#sync-balance-form [type=submit]').click();await page.locator('#sync-balance-dialog').waitFor({state:'hidden'});await page.getByText('真实余额已确认，未解释差额已保留。',{exact:true}).waitFor();assert.match(await page.locator('#sync-health').innerText(),/未解释差额/);
  // Exercise a real native XLSX upload through the same dedicated UI.
  const xlsx=fs.readFileSync(path.join(__dirname,'../internal/statement/testdata/wechat/example-wechat-records.xlsx'));
  await selectFile('wechat','微信原生.xlsx',xlsx);p=await preview('wechat');assert.equal(p.batch.row_count,27);assert(p.batch.new_transactions>0);await commit('wechat');
  const accountBeforeReload=(await(await page.request.get(base+'/api/v1/accounts')).json()).items.find(x=>x.id===wallet.id).current_balance;
  await page.reload();await page.locator('#sync-history a').first().waitFor();assert.match(await page.locator('#sync-summary').innerText(),/当前待确认/);assert.match(await page.locator('#sync-summary').innerText(),/微信支付/);assert.match(await page.locator('#sync-summary').innerText(),/支付宝/);assert.match(await page.locator('#sync-summary').innerText(),/农业银行/);
  balances=(await(await page.request.get(base+'/api/v1/accounts')).json()).items;assert.equal(balances.find(x=>x.id===wallet.id).current_balance,accountBeforeReload);
  await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'weekly mobile overflow');
  await page.getByRole('link',{name:'查看未补全语义',exact:true}).click();await page.locator('[data-annotate]').first().waitFor();assert.deepEqual(errors,[]);
  console.log('v0.4 Weekly UI PASS: native CSV/XLSX, dedicated WeChat/Alipay, 14 bank templates, ambiguity/bad rows, mapping re-preview, replay dedup, persisted history/summary, balance checkpoint, 390px layout.');
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
