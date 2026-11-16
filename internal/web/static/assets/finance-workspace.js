'use strict';
(() => {
const {$,esc,money,when,localInput,api,retryFor,notify,panel,table,dateRange,inputField}=WubuWorkspace;
const typeLabel = value => ({CONSUMABLE:'消耗品',DURABLE:'耐用品',UNTRACKED:'不跟库存',PURCHASE:'购入',RETURN:'退货',CONSUME:'消耗',ADJUST:'纠正',OPEN:'开封',FINISH:'用完',DISCARD:'弃置',ACTIVE:'在用',STORED:'收纳',RETIRED:'退休',PURCHASED:'购入',ACTIVATED:'启用',MOVED:'移动',FOUND:'找到',EXPENSE:'支出',INCOME:'收入',TRANSFER_OUT:'转出',TRANSFER_IN:'转入',BONUS:'赠送',REFUND:'退款',ADJUSTMENT:'余额纠正',SOLD:'已售',LOST:'丢失',DISCARDED:'弃置',ARCHIVED:'归档',DONE:'完成',PLANNED:'计划'}[value] || value);
const PAGE=50;
let view="home",offset=0,generation=0;
$("#month").value=localInput().slice(0,7);
let cashAccounts=[], cashCommand=null, cashRetry=null, cashFilters={account_id:'',type:''};
function accountOptions(selected='',empty=true,accountClass=''){return (empty?'<option value="">默认付款账户</option>':'')+cashAccounts.filter(a=>a.active&&(!accountClass||(a.account_class===accountClass&&a.id!=='unknown'))).map(a=>`<option value="${esc(a.id)}" ${a.id===selected?'selected':''}>${esc(a.name)} · ${a.account_class==='CONSUMPTION'?'消费储值':'存储'} · ${money(a.current_balance)}</option>`).join('');}
async function refreshAccounts(){cashAccounts=(await api('/accounts')).items;return cashAccounts;}
function accountName(id){return cashAccounts.find(a=>a.id===id)?.name||id;}
function cashCategory(value){return ({other:'其他',maintenance:'维护',sale:'物品出售'})[value]||WubuZh.category(value);}
function cashReason(value){if(value==='TOP_UP')return '消费账户充值';if(value==='TOP_UP_VALUE_LOSS')return '充值到账损失';if(value==='EXPIRED')return '余额过期失效';if(value==='INITIAL_BALANCE')return '初始余额';if(value==='PURCHASE_CORRECTION')return '购买记录纠正';if(value?.startsWith('DURABLE_SALE:'))return '耐用品出售';return value||'';}
function cashTable(data){return table(['时间','账户','类型 / 商家','金额 / 余额','备注'],data.map(t=>[esc(when(t.occurred_at)),esc(accountName(t.account_id)),`${esc(t.reason==='TOP_UP'&&t.type==='TRANSFER_OUT'?'充值转出':t.reason==='TOP_UP'&&t.type==='TRANSFER_IN'?'充值到账':typeLabel(t.type))}${t.voided_at?' · 已纠正':''}<br>${esc(t.merchant||cashCategory(t.category))}${t.related_transaction_id?'<br><small>已关联原流水</small>':''}`,`${t.balance_delta<0?'−':'+'}${money(t.amount)}<br><small>${money(t.balance_before)} → ${money(t.balance_after)}</small>`,esc(t.note||cashReason(t.reason))+(!t.voided_at&&['EXPENSE','INCOME','REFUND'].includes(t.type)?' <a href="/financial-inbox?transaction='+encodeURIComponent(t.id)+'">编辑</a>':'')+(t.type==='EXPENSE'&&!t.voided_at?`<br><button data-nature-id="${esc(t.id)}" data-nature="${esc(t.expense_nature)}">${esc(natureNames[t.expense_nature]||'未分类')} · 修改性质</button> <button data-residence-link="${esc(t.id)}">关联居住</button>${t.refundable_amount>0?` <button data-cash-action="refund" data-transaction-id="${esc(t.id)}" data-account="${esc(t.account_id)}" data-amount="${esc(t.refundable_amount)}">退款</button>`:''} <button data-backfill="${esc(t.id)}" data-time="${esc(t.occurred_at)}" data-merchant="${esc(t.merchant)}">后补购入</button>`:'')]));}
function accountClassFields(a={}){return `<label>资金分类<select name="account_class"><option value="STORAGE" ${a.account_class!=='CONSUMPTION'?'selected':''}>存储账户 · 自由支配</option><option value="CONSUMPTION" ${a.account_class==='CONSUMPTION'?'selected':''}>消费账户 · 受限储值</option></select></label>`+inputField('绑定商家（可选）','merchant','text',a.merchant||'','maxlength="200"');}
function accountGroups(accounts,settings=null,statistics=new Map(),overview=false){
 return ['STORAGE','CONSUMPTION'].map(kind=>panel(kind==='STORAGE'?'存储账户':'消费账户',table(overview?['账户 / 商家','余额','状态']:['账户 / 商家','类型','余额 / 累计','状态 / 默认','操作'],accounts.filter(a=>a.account_class===kind).map(a=>{
  const state=a.active?(settings?.default_payment_account===a.id?'默认付款':'启用'):'停用';
  const name=`<button class="ledger-link" data-account-detail="${esc(a.id)}">${esc(a.name)}</button>${a.merchant?`<br><small>${esc(a.merchant)}</small>`:''}`;
  if(overview)return [name,money(a.current_balance),state+(a.id==='unknown'?' · 历史未归属':'')];
  const stats=statistics.get(a.id),warnings=(a.warnings||[]).map(w=>`<br><small class="ledger-warning">${esc(w.message)}</small>`).join('');
  const amount=money(a.current_balance)+(stats?`<br><small>充值 ${money(stats.top_up_principal)} · 赠送 ${money(stats.bonus)}<br>净消费 ${money(stats.net_expense)}</small>`:'')+(a.credit_limit!==null?`<br><small>额度 ${money(a.credit_limit)}（不计资金）</small>`:'');
  const actions=a.id==='unknown'?'历史未归属':`<div class="ledger-actions">${a.active?['income','expense','adjust'].map(k=>`<button data-cash-action="${k}" data-account="${esc(a.id)}">${({income:'收入',expense:'支出',adjust:'核对余额'})[k]}</button>`).join(''):''}${a.active&&kind==='CONSUMPTION'?`<button data-cash-action="top-up" data-account="${esc(a.id)}">充值</button>`:''}<button data-account-detail="${esc(a.id)}">明细</button><button data-cash-action="edit-account" data-account="${esc(a.id)}">编辑</button>${a.active?`<button data-default-account="${esc(a.id)}">设为默认</button>`:''}</div>`;
  return [name,esc(({WALLET:'钱包',BANK:'银行卡',CASH:'现金',CAMPUS_CARD:'校园卡',CREDIT_CARD:'信用卡',MERCHANT_BALANCE:'商家储值',OTHER:'其他'})[a.type]),amount,state+warnings,actions];
 })))).join('');
}
async function accountDetail(id){
 await refreshAccounts();const a=cashAccounts.find(a=>a.id===id);
 const [stats,history]=await Promise.all([api(`/accounts/${encodeURIComponent(id)}/statistics`),api('/money/transactions?'+new URLSearchParams({account_id:id,limit:100}))]);
 $('#detail-title').textContent=a.name;
 $('#detail-content').innerHTML=panel('账户概况',`<p>${a.account_class==='CONSUMPTION'?'消费储值 · 受限使用':'存储账户 · 自由支配'}${a.merchant?' · '+esc(a.merchant):''}</p><p>当前余额 ${money(stats.current_balance)}</p>`)+(a.account_class==='CONSUMPTION'?panel('累计使用',`<p>实付充值 ${money(stats.top_up_principal)} · 赠送 ${money(stats.bonus)}</p><p>消费 ${money(stats.expense)} · 退款 ${money(stats.refund)} · 净消费 ${money(stats.net_expense)}</p>`):'')+panel('最近 100 条资金流水',cashTable(history.items));
 $('#detail-dialog').showModal();
}
async function renderFinance(which,params){
 if(['accounts','transactions'].includes(which))await refreshAccounts();let html='',count=0;
 if(which==='accounts'){
  const settings=await api('/money/settings');
  const consumption=cashAccounts.filter(a=>a.account_class==='CONSUMPTION');
  const statistics=new Map(await Promise.all(consumption.map(async a=>[a.id,await api(`/accounts/${a.id}/statistics`)])));
  html=panel('钱在哪里',`<div class="ledger-actions"><button class="button" data-cash-action="account">创建账户</button><button class="button" data-cash-action="transfer">账户转账</button><button class="button" data-cash-action="top-up">消费账户充值</button><button class="button" data-cash-action="category">添加 / 修改分类</button></div><p class="helper">存储账户的资金可自由支配；消费储值通常只能在限定场景使用。信用卡额度不计资金，待匹配记录可以在财务收件箱确认账户。</p>`)+accountGroups(cashAccounts,settings,statistics);
 }else if(which==='transactions'){
  const filter=new URLSearchParams(cashFilters),data=await api('/money/transactions?'+params+'&'+filter);count=data.items.length;
  html=`<form id="cash-filter" class="ledger-search"><select name="account_id"><option value="">全部账户</option>${cashAccounts.map(a=>`<option value="${esc(a.id)}" ${cashFilters.account_id===a.id?'selected':''}>${esc(a.name)}</option>`).join('')}</select><select name="type"><option value="">全部类型</option>${['EXPENSE','INCOME','TRANSFER_OUT','TRANSFER_IN','REFUND','ADJUSTMENT','BONUS'].map(t=>`<option ${cashFilters.type===t?'selected':''} value="${t}">${esc(typeLabel(t))}</option>`).join('')}</select><button class="button">筛选</button></form>`+panel('资金流水 · 所选月份',`<div class="ledger-actions"><button data-cash-action="income">记收入</button><button data-cash-action="expense">记支出</button><button data-cash-action="transfer">转账</button></div>`+cashTable(data.items));
 }
 return {html,count};
}
async function openCashDialog(command,button){
 const quick=['income','expense'].includes(command);
 if(['income','expense','refund','adjust','transfer','top-up','edit-account'].includes(command))await refreshAccounts();
 cashCommand={command,account:button?.dataset.account,transaction:button?.dataset.transactionId,occurredAt:new Date().toISOString()};cashRetry=null;
 const titles={account:'创建账户','edit-account':'编辑账户',income:'记录收入',expense:'记录支出',transfer:'账户转账','top-up':'消费账户充值',refund:'关联退款',adjust:'核对余额',category:'维护分类'};
 $('#cash-title').textContent=titles[command];$('#cash-error').textContent='';
 const choose=(name,label,selected='')=>`<label>${label}<select name="${name}">${accountOptions(selected)}</select></label>`;
 let fields='';
 if(command==='account')fields=inputField('账户名称','name','text','','required maxlength="100"')+'<label>类型<select name="type"><option value="WALLET">钱包</option><option value="BANK">银行卡</option><option value="CASH">现金</option><option value="CAMPUS_CARD">校园卡</option><option value="CREDIT_CARD">信用卡</option><option value="MERCHANT_BALANCE">商家储值</option><option value="OTHER">其他</option></select></label>'+accountClassFields()+inputField('当前真实余额（元）','initial_balance','number',0,'step="0.01" required')+inputField('信用卡额度（可选）','credit_limit','number','','step="0.01" min="0"');
 if(command==='edit-account'){const a=cashAccounts.find(a=>a.id===cashCommand.account);fields=inputField('账户名称','name','text',a.name,'required maxlength="100"')+accountClassFields(a)+`<label>状态<select name="active"><option value="true" ${a.active?'selected':''}>启用</option><option value="false" ${a.active?'':'selected'}>停用</option></select></label>`;}
 if(['refund','adjust'].includes(command))fields=choose('account_id','账户',cashCommand.account);
 if(command==='refund')fields+=inputField('金额（元）','amount','number',button.dataset.amount,'min="0.01" step="0.01" required');
 if(command==='refund')fields+='<p class="helper">退款记录实际到账。实际退货可从物品页登记。</p>';
 if(command==='adjust')fields+=inputField('真实余额（元）','actual_balance','number','','step="0.01" required')+inputField('差异原因','reason');
 if(command==='top-up')fields=`<label>从存储账户付款<select name="from_account" required>${accountOptions('',false,'STORAGE')}</select></label><label>充值到消费账户<select name="to_account" required>${accountOptions(cashCommand.account,false,'CONSUMPTION')}</select></label>`+inputField('实付本金（元）','paid_amount','number','','min="0.01" step="0.01" required')+inputField('实际到账（元）','credited_amount','number','','min="0.01" step="0.01" required')+'<p class="helper">超出本金的部分记赠送；充值不计消费。实际购买商品或服务时再记录支出。</p>';
 if(command==='transfer')fields=choose('from_account','从')+choose('to_account','到')+inputField('金额（元）','amount','number','','min="0.01" step="0.01" required');
 if(quick){
  const settings=await api('/money/settings');
  const real=cashAccounts.filter(a=>a.active&&a.id!=='unknown');
  const selected=[cashCommand.account,localStorage.getItem('wubu-last-confirmed-account'),settings.default_payment_account].find(id=>real.some(a=>a.id===id));
  fields=inputField('金额（元）','amount','number','','min="0.01" step="0.01" required')+
   inputField('说明','note','text','','maxlength="2000"')+
   '<label>账户<select name="account_id"><option value="">暂存为待匹配</option>'+
   real.map(a=>`<option value="${esc(a.id)}" ${a.id===selected?'selected':''}>${esc(a.name)}</option>`).join('')+'</select></label>'+
   '<details><summary>更多选项</summary>'+inputField('分类','category','text','','maxlength="100"')+inputField('商家','merchant','text','','maxlength="200"')+
   (command==='expense'?'<label>支出性质<select name="expense_nature">'+natureOptions()+'</select></label>':'')+
   inputField('标签（逗号分隔）','tags')+'</details>';
 }
 if(command==='category')fields=inputField('新类别名称','name','text','','required maxlength="100"')+inputField('原类别名称（修改时填）','previous_name');
 if(!quick&&command!=='category')fields+=inputField('备注','note','text',command==='edit-account'?cashAccounts.find(a=>a.id===cashCommand.account)?.note||'':'','maxlength="2000"');
 $('#cash-fields').innerHTML=fields;$('#cash-dialog').showModal();
}
async function submitCash(form){
 let body=Object.fromEntries(new FormData(form));const c=cashCommand.command,quick=c==='income'||c==='expense';let path='',pending=false;
 for(const field of ['amount','paid_amount','credited_amount','initial_balance','actual_balance','credit_limit']){if(field in body){if(body[field]==='')delete body[field];else body[field]=Number(body[field]);}}
 if(c==='account')path='/accounts';else if(c==='edit-account'){path='/accounts/'+cashCommand.account;body.active=body.active==='true';}else if(c==='category')path='/money/categories';
 else if(c==='refund'){path='/finance/refunds';body.related_transaction_id=cashCommand.transaction;}
 else path='/money/'+({income:'income',expense:'expenses',transfer:'transfers',refund:'refunds',adjust:'adjustments','top-up':'top-ups'})[c];
 if(!['account','edit-account','category'].includes(c))body.source='manual';
 if(quick){
  body.tags=body.tags.split(/[,，]/).map(tag=>tag.trim()).filter(Boolean);
  body.occurred_at=cashCommand.occurredAt;
  pending=!body.account_id;
  if(pending){
   path='/financial/events';
   body={provider:'OTHER',capture_source:'MANUAL',occurred_at:body.occurred_at,amount:body.amount,direction:c.toUpperCase(),
    raw_text:body.note,merchant_raw:body.merchant,manual_details:{category:body.category,expense_nature:body.expense_nature,tags:body.tags}};
  }
 }
 cashRetry=retryFor(cashRetry,body,path);
 const result=await api(path,body,cashRetry.key,c==='edit-account'?'PATCH':'POST');
 if(quick&&!pending)localStorage.setItem('wubu-last-confirmed-account',body.account_id);
 $('#cash-dialog').close();await load();
 const status=pending?'待匹配 · 已保存到财务收件箱。':quick?'已入账。':'已保存。';
 notify(result.warnings?.length?status+' '+result.warnings.map(w=>w.message).join(' '):status,!!result.warnings?.length);
}
document.addEventListener('click',async e=>{
 const backfill=e.target.closest('[data-backfill]');
 if(backfill){const {backfill:id,time,merchant}=backfill.dataset;location.assign('/items?'+new URLSearchParams({backfill:id,time,merchant}));return;}
 const detail=e.target.closest('[data-account-detail]');if(detail){try{await accountDetail(detail.dataset.accountDetail);}catch(err){notify(WubuZh.error(err),true);}}
 const cash=e.target.closest('[data-cash-action]');if(cash){try{await openCashDialog(cash.dataset.cashAction,cash);}catch(err){notify(WubuZh.error(err),true);}}
 const def=e.target.closest('[data-default-account]');if(def){def.disabled=true;try{await api('/money/settings',{default_payment_account:def.dataset.defaultAccount},crypto.randomUUID(),'PATCH');notify('默认付款账户已设置。');await load();}catch(err){notify(WubuZh.error(err),true);}finally{def.disabled=false;}}
});
document.addEventListener('submit',async e=>{
 const form=e.target;if(form.id==='cash-filter'){e.preventDefault();cashFilters=Object.fromEntries(new FormData(form));offset=0;await load();}
 if(form.id==='cash-form'){e.preventDefault();const button=form.querySelector('[type=submit]');button.disabled=true;try{await submitCash(form);}catch(err){$('#cash-error').textContent=WubuZh.error(err);}finally{button.disabled=false;}}
});
let livingResidences=new Map(),livingClaims=new Map(),livingCommand=null,livingRetry=null;
async function livingPanel(){
 const [residences,deposits,receivables]=await Promise.all([api('/residences'),api('/deposits'),api('/receivables')]);
 livingResidences=new Map(residences.items.map(r=>[r.id,r]));livingClaims=new Map([...deposits.items,...receivables.items].map(c=>[c.id,c]));
 const claimsTable=items=>table(['对象 / 事由','原额 / 剩余','状态','操作'],items.map(c=>[esc(c.kind==='DEPOSIT'?(livingResidences.get(c.residence_id)?.name||c.residence_id):c.counterparty)+'<br>'+esc(c.reason),`${money(c.amount)} / ${money(c.amount-c.returned_amount-c.forfeited_amount)}`,esc(({HELD:'持有',PARTIALLY_RETURNED:'部分退回',RETURNED:'已退回',FORFEITED:'含没收损失',OPEN:'待收',PARTIAL:'部分结清',SETTLED:'已结清',CANCELLED:'已核销'})[c.status]),c.amount>c.returned_amount+c.forfeited_amount?`<button data-living="return" data-living-id="${esc(c.id)}">${c.kind==='DEPOSIT'?'退押金':'到账'}</button> <button data-living="forfeit" data-living-id="${esc(c.id)}">${c.kind==='DEPOSIT'?'没收损失':'核销损失'}</button>`:'—']));
 return `<div class="ledger-actions"><button data-living="residence">新增居住</button><button data-living="deposit">记录支付押金</button><button data-living="receivable">记录实际代垫</button></div>`+panel('居住',table(['居住','月租','状态','操作'],residences.items.map(r=>[esc(r.name),money(r.monthly_rent),esc(({ACTIVE:'当前',PAST:'过去',PLANNED:'计划'})[r.status]),`<button data-living="residence" data-living-id="${esc(r.id)}">编辑</button> <button data-residence-cost="${esc(r.id)}">居住成本</button>`])))+panel('押金 · 不计普通消费',claimsTable(deposits.items))+panel('待收 / 报销 · 到账前不计可花',claimsTable(receivables.items));
}
async function openLiving(action,id=''){
 await refreshAccounts();const residences=(await api('/residences')).items;livingResidences=new Map(residences.map(r=>[r.id,r]));
 livingCommand={action,id};livingRetry=null;let fields='',title='';
 const residenceSelect=(selected='')=>`<label>居住<select name="residence_id" required>${residences.map(r=>`<option value="${esc(r.id)}" ${r.id===selected?'selected':''}>${esc(r.name)}</option>`).join('')}</select></label>`;
 if(action==='residence'){
 const r=livingResidences.get(id)||{};title=id?'编辑居住':'新增居住';
 fields=inputField('名称','name','text',r.name||'','required maxlength="200"')+inputField('地址（仅本地保存）','address_text','text',r.address_text||'','maxlength="2000"')+inputField('入住日','move_in_date','date',r.move_in_date||localInput().slice(0,10),'required')+inputField('搬离日','move_out_date','date',r.move_out_date||'')+inputField('月租（元）','monthly_rent','number',r.monthly_rent||0,'min="0" step="0.01" required')+inputField('付款周期','payment_cycle','text',r.payment_cycle||'','maxlength="100"')+inputField('合同到期','contract_end_date','date',r.contract_end_date||'')+`<label>状态<select name="status">${Object.entries({ACTIVE:'当前',PAST:'过去',PLANNED:'计划'}).map(([k,v])=>`<option value="${k}" ${r.status===k?'selected':''}>${v}</option>`).join('')}</select></label><label><input name="network_included" type="checkbox" ${r.network_included?'checked':''}>包含网络</label><label><input name="management_fee_included" type="checkbox" ${r.management_fee_included?'checked':''}>包含管理费</label>`+inputField('备注','note','text',r.note||'','maxlength="2000"');
 }else if(action==='deposit'||action==='receivable'){
 title=action==='deposit'?'记录已支付押金':'记录实际代垫';fields=(action==='deposit'?residenceSelect():inputField('欠款方','counterparty','text','','required maxlength="200"'))+`<label>付款账户<select name="account_id">${accountOptions()}</select></label>`+inputField('实际支付（元）','amount','number','','min="0.01" step="0.01" required')+inputField('事由','reason','text','','maxlength="2000"')+inputField('实际付款时间','occurred_at','datetime-local',localInput(),'required');
 }else if(action==='link'){
 title='关联居住成本';fields=residenceSelect()+`<label>角色<select name="role">${Object.entries({RENT:'房租',AGENCY_FEE:'中介',UTILITY:'水电',MANAGEMENT:'管理',NETWORK:'网络',MOVING:'搬家',SETUP:'安家',OTHER:'其他'}).map(([k,v])=>`<option value="${k}">${v}</option>`).join('')}</select></label>`;
 }else{
 const c=livingClaims.get(id);title=action==='return'?'记录实际退回 / 到账':'记录不可收回损失';fields=`<p>${esc(c.counterparty||livingResidences.get(c.residence_id)?.name)} · 剩余 ${money(c.amount-c.returned_amount-c.forfeited_amount)}</p><label>账户<select name="account_id">${accountOptions(c.account_id)}</select></label>`+inputField('本次金额（元）','amount','number',c.amount-c.returned_amount-c.forfeited_amount,'min="0.01" step="0.01" required')+inputField('发生时间','occurred_at','datetime-local',localInput(),'required');
 }
 let dialog=$('#living-dialog');if(!dialog){dialog=document.createElement('dialog');dialog.id='living-dialog';document.body.append(dialog);}
 dialog.innerHTML=`<form id="living-form"><div class="dialog-head"><h2>${title}</h2><button type="button" data-close="living-dialog" aria-label="关闭">×</button></div><div class="form-grid">${fields}</div><p class="helper">保存付款或到账会改变真实余额；请只记录已发生事件。地址和凭证不会自动上传。</p><p id="living-error" role="alert"></p><button class="button" type="submit">确认保存</button></form>`;dialog.showModal();
}
document.addEventListener('click',async e=>{
 const b=e.target.closest('[data-living],[data-residence-cost],[data-residence-durables],[data-residence-link]');if(!b)return;
 try{
 if(b.dataset.living){await openLiving(b.dataset.living,b.dataset.livingId||'');return;}
 if(b.dataset.residenceLink){await openLiving('link',b.dataset.residenceLink);return;}

 if(b.dataset.residenceDurables){
  const id=encodeURIComponent(b.dataset.residenceDurables);
  const {items}=await api('/residences/'+id+'/durables');
  const target=document.createElement('div');
  target.innerHTML=items.length?items.map(i=>'<p><a href="/items?item='+encodeURIComponent(i.item_id)+'">'+esc(i.name||'查看耐用品')+'</a> · '+esc(WubuZh.location(i.location))+'</p>').join(''):'<p>尚无关联耐用品。</p>';
  b.replaceWith(target);
  return;
 }
 const id=b.dataset.residenceCost,c=await api('/residences/'+encodeURIComponent(id)+'/cost');
 $('#detail-title').textContent=livingResidences.get(id)?.name||'居住成本';$('#detail-content').innerHTML=`<p>住房实际消费 ${money(c.consumed_cost)} · 其中一次性安家 ${money(c.setup_cost)}</p><p>耐用品投入 ${money(c.capital_cost)} · 押金锁定 ${money(c.deposit_locked)} · 押金损失 ${money(c.deposit_loss)}</p><p>合计净现金流出 ${money(c.net_cash_outflow)}</p>`+table(['角色 / 性质','金额'],c.groups.map(g=>[esc(g.key),money(g.amount)]))+ '<p><button class="button" data-residence-durables="'+esc(id)+'">查看关联物品</button></p>' ;$('#detail-dialog').showModal();
 }catch(err){notify(WubuZh.error(err),true);}
});
document.addEventListener('submit',async e=>{
 if(e.target.id!=='living-form')return;e.preventDefault();const button=e.target.querySelector('[type=submit]');
 try{
 let body=Object.fromEntries(new FormData(e.target)),path,method='POST';const {action,id}=livingCommand;
 if(action==='residence'){path='/residences'+(id?'/'+encodeURIComponent(id):'');method=id?'PUT':'POST';body.monthly_rent=Number(body.monthly_rent);body.network_included=body.network_included==='on';body.management_fee_included=body.management_fee_included==='on';}
 else if(action==='link'){path=`/residences/${encodeURIComponent(body.residence_id)}/cost-links`;body={transaction_id:id,role:body.role};}
 else{body.amount=Number(body.amount);body.occurred_at=new Date(body.occurred_at+'+08:00').toISOString();if(action==='deposit'||action==='receivable')path=action==='deposit'?'/deposits':'/receivables';else path=`/${livingClaims.get(id).kind==='DEPOSIT'?'deposits':'receivables'}/${encodeURIComponent(id)}/${action}`;}
 livingRetry=retryFor(livingRetry,body,path);button.disabled=true;await api(path,body,livingRetry.key,method);livingRetry=null;$('#living-dialog').close();await load();
 }catch(err){$('#living-error').textContent=WubuZh.error(err);}finally{button.disabled=false;}
});
const natureNames={'':'未分类',DAILY:'日常',FIXED:'固定',SETUP:'安家',CAPITAL:'耐用品投入',EXCEPTIONAL:'特殊事件',UNCLASSIFIED:'未分类'};
function natureOptions(selected=''){return Object.entries(natureNames).filter(([k])=>k!=='UNCLASSIFIED').map(([k,v])=>`<option value="${k}" ${selected===k?'selected':''}>${v}</option>`).join('');}
let runwayPlans=new Map(),runwayRetry=null,runwayEditing='';
async function runwayPanel(){
 const [plans,settings,spending]=await Promise.all([api('/planned-cashflows'),api('/runway/settings'),api('/money/spending-by-nature?'+dateRange())]);
 runwayPlans=new Map(plans.items.map(p=>[p.id,p]));
 return panel('生活运行状态',`<p>本月正常生活成本 <strong>${money(spending.normalized_living_cost)}</strong> · 未分类 ${money(spending.unclassified)}</p><p>${spending.groups.map(g=>`${natureNames[g.key]} ${money(g.amount)}`).join(' · ')}</p><p class="helper">正常生活成本仅计日常与固定支出。未分类旧账不会自动猜测；未来收入尚未到账，不计入安全可花。</p><form id="runway-settings" class="ledger-search"><label>安全缓冲（元）<input name="safety_buffer" type="number" min="0" max="10000000000" step="0.01" value="${settings.safety_buffer}" required></label><label>无收入时规划天数<input name="runway_horizon_days" type="number" min="1" max="3660" value="${settings.runway_horizon_days}" required></label><button class="button">保存设置</button></form><button class="button" data-plan-new>新增未来收支</button>`)+panel('待发生收支',table(['到期','计划','金额','周期','操作'],plans.items.map(p=>[esc(when(p.due_at)),`${esc(p.name)} · ${p.direction==='OUTFLOW'?'支出':'收入'}${p.mandatory?' · 必要':''}`,money(p.amount),esc(({WEEKLY:'每周',MONTHLY:'每月',YEARLY:'每年',CUSTOM:`每 ${p.interval_days} 天`})[p.recurrence]||'单次'),`<button data-plan-edit="${esc(p.id)}">编辑</button> <button data-plan-action="complete" data-plan-id="${esc(p.id)}">${p.direction==='OUTFLOW'?'确认付款':'确认到账'}</button> <button data-plan-action="skip" data-plan-id="${esc(p.id)}">跳过</button> <button data-plan-action="cancel" data-plan-id="${esc(p.id)}">取消</button>`])));
}
async function openPlan(id=''){
 await refreshAccounts();runwayEditing=id;runwayRetry=null;const p=runwayPlans.get(id)||{};
 let dialog=document.querySelector('#plan-dialog');if(!dialog){dialog=document.createElement('dialog');dialog.id='plan-dialog';document.body.append(dialog);}
 dialog.innerHTML=`<form id="plan-form"><div class="dialog-head"><h2>${id?'编辑':'新增'}未来收支</h2><button type="button" data-close="plan-dialog" aria-label="关闭">×</button></div><div class="form-grid"><label>名称<input name="name" value="${esc(p.name||'')}" maxlength="200" required></label><label>方向<select name="direction"><option value="OUTFLOW">支出</option><option value="INFLOW" ${p.direction==='INFLOW'?'selected':''}>收入</option></select></label><label>金额（元）<input type="number" name="amount" min="0.01" max="10000000000" step="0.01" value="${p.amount||''}" required></label><label>到期时间<input type="datetime-local" name="due_at" value="${p.due_at?new Date(new Date(p.due_at).getTime()+28800000).toISOString().slice(0,16):localInput()}" required></label><label>账户<select name="account_id">${accountOptions(p.account_id||'')}</select></label><label>支出性质<select name="expense_nature">${natureOptions(p.expense_nature)}</select></label><label>周期<select name="recurrence">${Object.entries({'':'单次',WEEKLY:'每周',MONTHLY:'每月',YEARLY:'每年',CUSTOM:'自定义天数'}).map(([k,v])=>`<option value="${k}" ${p.recurrence===k?'selected':''}>${v}</option>`).join('')}</select></label><label>自定义周期天数<input type="number" name="interval_days" min="1" max="3660" value="${p.interval_days||1}"></label><label><input name="mandatory" type="checkbox" ${p.mandatory!==false?'checked':''}>必要支出（计入预留）</label><label>备注<input name="note" maxlength="2000" value="${esc(p.note||'')}"></label></div><p class="helper">确认付款或到账才改变真实余额；编辑只修改计划。月末周期按目标月最后一天截断。</p><p id="plan-error" role="alert"></p><button class="button" type="submit">保存计划</button></form>`;
 dialog.showModal();
}
document.addEventListener('click',async e=>{
 const b=e.target.closest('[data-plan-new],[data-plan-edit],[data-plan-action],[data-nature-id]');if(!b)return;
 try{
 if(b.hasAttribute('data-plan-new')){await openPlan();return;}
 if(b.dataset.planEdit){await openPlan(b.dataset.planEdit);return;}
 if(b.dataset.natureId){
 const nature=prompt('支出性质：DAILY 日常 / FIXED 固定 / SETUP 安家 / CAPITAL 耐用品 / EXCEPTIONAL 特殊；留空为未分类',b.dataset.nature||'');if(nature===null)return;
 if(!(nature in natureNames)||nature==='UNCLASSIFIED')throw new Error('请选择有效的支出性质。');
 const path=`/money/transactions/${encodeURIComponent(b.dataset.natureId)}/nature`,body={expense_nature:nature};runwayRetry=retryFor(runwayRetry,body,path);await api(path,body,runwayRetry.key,'PATCH');runwayRetry=null;await load();return;
 }
 const p=runwayPlans.get(b.dataset.planId),action=b.dataset.planAction;
 if(!confirm(`${({complete:p.direction==='OUTFLOW'?'确认已经付款':'确认已经到账',skip:'跳过本期',cancel:'取消计划'})[action]}：${p.name} ${money(p.amount)}？`))return;
 const path=`/planned-cashflows/${encodeURIComponent(p.id)}/${action}`,body={};runwayRetry=retryFor(runwayRetry,body,path);b.disabled=true;
 await api(path,body,runwayRetry.key);runwayRetry=null;await load();
 }catch(err){notify(WubuZh.error(err),true);}finally{b.disabled=false;}
});
document.addEventListener('submit',async e=>{
 if(!['plan-form','runway-settings'].includes(e.target.id))return;e.preventDefault();const form=e.target,button=form.querySelector('button[type="submit"],button');
 try{
 const values=Object.fromEntries(new FormData(form));let path,body,method;
 if(form.id==='runway-settings'){path='/runway/settings';method='PATCH';body={safety_buffer:Number(values.safety_buffer),runway_horizon_days:Number(values.runway_horizon_days)};}
 else{path='/planned-cashflows'+(runwayEditing?'/'+encodeURIComponent(runwayEditing):'');method=runwayEditing?'PUT':'POST';body={...values,amount:Number(values.amount),mandatory:values.mandatory==='on',interval_days:values.recurrence==='CUSTOM'?Number(values.interval_days):0,due_at:new Date(values.due_at+'+08:00').toISOString(),category:runwayPlans.get(runwayEditing)?.category||''};}
 runwayRetry=retryFor(runwayRetry,body,path);button.disabled=true;await api(path,body,runwayRetry.key,method);runwayRetry=null;
 if(form.id==='plan-form')$('#plan-dialog').close();await load();
 }catch(err){if(form.id==='plan-form')$('#plan-error').textContent=WubuZh.error(err);else notify(WubuZh.error(err),true);}finally{button.disabled=false;}
});

async function home(){
 const [balance,runway,recent]=await Promise.all([api('/money/balance'),api('/runway'),api('/money/transactions?limit=5')]);
 cashAccounts=balance.accounts;
 const metrics=[['自由资金',money(runway.liquid_balance)],['未来已预留',money(runway.reserved_outflow)],['安全可花',money(runway.safe_to_spend)],
  ['安全缓冲',money(runway.safety_buffer)],['安全日均',money(runway.safe_daily_spend)],['消费储值',money(balance.consumption_balance)]]
  .map(([label,value])=>'<div class="ledger-metric"><small>'+esc(label)+'</small><strong>'+esc(value)+'</strong></div>').join('');
 return '<section class="metrics" id="metrics" aria-label="资金概览">'+metrics+'</section>'+
  (runway.cashflow_warning?'<p class="ledger-warning">现金流不足，已知义务与缓冲超过自由资金。</p>':'')+
  '<div class="ledger-card-grid">'+accountGroups(balance.accounts,null,new Map(),true)+'</div>'+
  panel('最近资金流水',cashTable(recent.items))+
  '<details class="ledger-panel" id="home-planning" '+($('#home-planning')?.open?'open':'')+'><summary>计划收支</summary>'+await runwayPanel()+'</details>';
}
async function load(){
 const token=++generation;
 const current=view;
 $('#content').setAttribute('aria-busy','true');
 try {
  let html,count=0;
  if(current==='home')html=await home();
  else if(current==='living')html=await livingPanel();
  else {
   const data=await renderFinance(current,dateRange()+'&limit='+PAGE+'&offset='+offset);
   html=data.html;count=data.count;
  }
  if(token!==generation)return;
  $('#content').innerHTML=html;
  $('.workspace-period').hidden=!['home','transactions'].includes(current);
  $('#paging').hidden=current!=='transactions';
  $('#previous').disabled=offset===0;
  $('#next').disabled=count<PAGE;
  $('#page-label').textContent='第 '+(Math.floor(offset/PAGE)+1)+' 页';
 }catch(error){if(token===generation)notify(WubuZh.error(error),true);}
 finally{if(token===generation)$('#content').removeAttribute('aria-busy');}
}
document.addEventListener('click',event=>{const close=event.target.closest('[data-close]');if(close)$('#'+close.dataset.close).close();});
$('.ledger-nav').addEventListener('click',e=>{const b=e.target.closest('[data-view]');if(!b)return;view=b.dataset.view;offset=0;document.querySelectorAll('[data-view]').forEach(el=>{el.classList.toggle('is-active',el===b);el.setAttribute('aria-pressed',String(el===b));});load();});
$('#month').addEventListener('change',()=>{if(!$('#month').value)$('#month').value=localInput().slice(0,7);offset=0;load();});$('#previous').addEventListener('click',()=>{offset=Math.max(0,offset-PAGE);load();});$('#next').addEventListener('click',()=>{offset+=PAGE;load();});
load();
})();
