const assert=require('node:assert/strict');
const {chromium}=require(process.env.WUBU_PLAYWRIGHT_MODULE||'playwright');
const base=process.env.WUBU_TEST_BASE_URL;
if(!/^http:\/\/127\.0\.0\.1:\d+$/.test(base||''))throw new Error('Disposable loopback service required');
(async()=>{const browser=await chromium.launch({channel:'msedge',headless:true});const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>{if(m.type()==='error')errors.push(m.text());});
try{
const post=async(path,data,key)=>{const r=await page.request.post(base+'/api/v1/'+path,{data,headers:{'Idempotency-Key':key}});assert(r.ok(),await r.text());return r.json();};
const a=await post('accounts',{name:'UI v04 银行',type:'BANK',initial_balance:100},'ui04-account');
const at=new Date().toISOString();
await post('financial/events',{provider:'WECHAT',capture_source:'MANUAL',account_id:a.id,amount:20,direction:'EXPENSE',occurred_at:at,merchant_raw:'午餐店',raw_text:'支付20元'},'ui04-event');
await page.goto(base+'/financial-inbox');await page.locator('[data-tab=unannotated]').click();await page.locator('[data-annotate]').first().waitFor();
await page.locator('[data-annotate]').first().click();await page.locator('[name=category]').fill('餐饮');await page.locator('[name=item_name]').fill('白切鸡盖饭');await page.locator('#review-form [type=submit]').click();await page.locator('#financial-dialog').waitFor({state:'hidden'});await page.getByText('没有未补全流水。').waitFor();
const review=await post('financial/events',{provider:'WECHAT',capture_source:'MANUAL',raw_text:'金额与账户尚未知'},'ui04-unknown');
const candidateAccount=await post('accounts',{name:'UI v04 多候选',type:'BANK',initial_balance:100},'ui04-candidate-account');
const candidates=[];for(const key of ['one','two']){const m=await post('money/expenses',{account_id:candidateAccount.id,amount:3,merchant:'水店',occurred_at:at,source:'manual'},'ui04-candidate-'+key);candidates.push(m.transactions[0].id);}
const ambiguous=await post('financial/events',{provider:'WECHAT',capture_source:'CSV_IMPORT',account_id:candidateAccount.id,amount:3,direction:'EXPENSE',occurred_at:at,merchant_raw:'水店',raw_text:'原始账单同额候选'},'ui04-ambiguous');assert.equal(ambiguous.candidates.length,2);
await page.reload();await page.locator(`[data-review="${ambiguous.id}"]`).click();assert.equal(await page.locator('[name=transaction_id]').inputValue(),'','multiple candidates must not be preselected');await page.locator('[name=transaction_id]').fill(candidates[1]);await page.locator('[name=reason]').fill('核对原始账单，确认与第二笔一致');await page.locator('#review-form [type=submit]').click();await page.locator('#financial-dialog').waitFor({state:'hidden'});
const candidateBalances=await(await page.request.get(base+'/api/v1/accounts')).json();assert.equal(candidateBalances.items.find(x=>x.id===candidateAccount.id).current_balance,94);
await page.locator('[data-tab=review]').click();await page.locator(`[data-review="${review.id}"]`).click();await page.locator('[name=action]').selectOption('IGNORE');await page.locator('[name=reason]').fill('核对后确认无需入账');await page.locator('#review-form [type=submit]').click();await page.locator('#financial-dialog').waitFor({state:'hidden'});
await page.locator('[data-tab=processed]').click();await page.getByText('核对后确认无需入账',{exact:false}).waitFor();
await page.locator('[data-tab=health]').click();await page.getByText('尚无余额检查点，请在账户页核对真实余额。').waitFor();
await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'mobile overflow');
const balances=await(await page.request.get(base+'/api/v1/accounts')).json();assert.equal(balances.items.find(x=>x.id===a.id).current_balance,80);assert.deepEqual(errors,[]);console.log('v0.4 UI PASS: real annotation, ignore evidence, account health, 390px layout, no browser errors.');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
