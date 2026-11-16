'use strict';
let cashAccounts=[], cashCommand=null, cashRetry=null, cashFilters={account_id:'',type:''}, durableFilters={status:'',location:'',search:''};
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
async function renderV02(which,params){
 if(['accounts','transactions'].includes(which))await refreshAccounts();let html='',count=0;
 if(which==='accounts'){
  const settings=await api('/money/settings');
  const consumption=cashAccounts.filter(a=>a.account_class==='CONSUMPTION');
  const statistics=new Map(await Promise.all(consumption.map(async a=>[a.id,await api(`/accounts/${a.id}/statistics`)])));
  html=panel('钱在哪里',`<div class="ledger-actions"><button class="button" data-cash-action="account">创建账户</button><button class="button" data-cash-action="transfer">账户转账</button><button class="button" data-cash-action="top-up">消费账户充值</button><button class="button" data-cash-action="category">添加 / 修改分类</button></div><p class="helper">存储账户的资金可自由支配；消费储值通常只能在限定场景使用。信用卡额度不计资金，待匹配记录可以在财务收件箱确认账户。</p>`)+accountGroups(cashAccounts,settings,statistics);
 }else if(which==='transactions'){
  const filter=new URLSearchParams(cashFilters),data=await api('/money/transactions?'+params+'&'+filter);count=data.items.length;
  html=`<form id="cash-filter" class="ledger-search"><select name="account_id"><option value="">全部账户</option>${cashAccounts.map(a=>`<option value="${esc(a.id)}" ${cashFilters.account_id===a.id?'selected':''}>${esc(a.name)}</option>`).join('')}</select><select name="type"><option value="">全部类型</option>${['EXPENSE','INCOME','TRANSFER_OUT','TRANSFER_IN','REFUND','ADJUSTMENT','BONUS'].map(t=>`<option ${cashFilters.type===t?'selected':''} value="${t}">${esc(typeLabel(t))}</option>`).join('')}</select><button class="button">筛选</button></form>`+panel('资金流水 · 所选月份',`<div class="ledger-actions"><button data-cash-action="income">记收入</button><button data-cash-action="expense">记支出</button><button data-cash-action="transfer">转账</button></div>`+cashTable(data.items));
 }else if(which==='durables'){
  const data=await api('/durables?limit='+PAGE+'&offset='+offset+'&'+new URLSearchParams(durableFilters));count=data.items.length;
  html=`<form id="durable-filter" class="ledger-search"><select name="status"><option value="">全部状态</option>${['ACTIVE','STORED','LOST','SOLD','RETIRED'].map(t=>`<option value="${t}" ${durableFilters.status===t?'selected':''}>${esc(typeLabel(t))}</option>`).join('')}</select><input name="location" value="${esc(durableFilters.location)}" placeholder="位置"><input name="search" value="${esc(durableFilters.search)}" placeholder="耐用品名称"><button class="button">筛选</button></form>`+panel('耐用品仓库',table(['物品','状态 / 位置','购入 / 使用时长','购买价 / 保修','操作'],data.items.map(row=>{const d=row.details;return [`<button class="ledger-link" data-detail="${esc(d.item_id)}">${esc(row.name)}</button>`,`${esc(typeLabel(d.status))}<br>${esc(d.location||'未设位置')}`,`${esc(when(d.purchase_date))}<br>${d.use_days} 天`,`${d.purchase_price?money(d.purchase_price):'—'}<br>${d.warranty_days===null?'未记录保修':d.warranty_days>=0?'保修剩余 '+d.warranty_days+' 天':'保修已到期'}`,`<button data-living="durable" data-living-id="${esc(d.item_id)}">资料</button>`+(['SOLD','RETIRED'].includes(d.status)?'历史保留':`<div class="ledger-actions"><button data-action="maintenance" data-id="${esc(d.item_id)}">维护</button>${['move','status','sell','retire'].map(k=>`<button data-cash-action="${k}" data-item="${esc(d.item_id)}">${({move:'移动',status:'状态',sell:'出售',retire:'退休'})[k]}</button>`).join('')}</div>`)];})));

 }
 return {html,count};
}
async function openCashDialog(command,button){
 const quick=['income','expense'].includes(command);
 if(['income','expense','refund','adjust','transfer','top-up','edit-account','link'].includes(command))await refreshAccounts();
 cashCommand={command,item:button?.dataset.item,account:button?.dataset.account,transaction:button?.dataset.transactionId,linkSource:button?.dataset.linkSource,linkType:button?.dataset.domainLink,occurredAt:new Date().toISOString()};cashRetry=null;
 const titles={link:'关联资金流水',account:'创建账户','edit-account':'编辑账户',income:'记录收入',expense:'记录支出',transfer:'账户转账','top-up':'消费账户充值',refund:'关联退款',adjust:'核对余额',category:'维护分类',move:'移动物品',status:'耐用品状态',sell:'出售耐用品',retire:'退休 / 报废'};
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
 if(command==='move')fields=inputField('新的位置','location','text','','required maxlength="100"');
 if(command==='status')fields='<label>状态<select name="status"><option value="ACTIVE">在用 / 找到</option><option value="STORED">收纳 / 闲置</option><option value="LOST">丢失</option></select></label>';
 if(command==='link'){
  const options=cashCommand.linkType==='MAINTENANCE'?['MAINTENANCE','REFUND']:cashCommand.linkType==='ITEM'?['SALE','RETURN']:['PURCHASE','REFUND'];
  fields='<label>用途<select name="relation">'+options.map(k=>"<option value='"+k+"'>"+linkLabel(k)+"</option>").join('')+'</select></label>'+
   inputField('金额（可选）','search_amount','number','','min="0.01" step="0.01"')+inputField('日期（可选）','search_date','date')+
   inputField('商家或描述','search_text','text','','maxlength="200"')+
   '<button type="button" class="button" id="search-link">查找流水</button><label>选择流水<select name="transaction_id" required></select></label>';
 }
 if(command==='sell')fields='<p class="helper">确认物品已售出。到账收入可在资金流水登记。</p>';
 if(command==='retire')fields='<p>退休后保留完整历史，拥有数量归零。</p>';
 if(!quick&&!['category','link'].includes(command))fields+=inputField('备注','note','text',command==='edit-account'?cashAccounts.find(a=>a.id===cashCommand.account)?.note||'':'','maxlength="2000"');
 $('#cash-fields').innerHTML=fields;$('#cash-dialog').showModal();
 if(command==='link'){
  const form=$('#cash-form');
  $('#search-link').onclick=()=>searchLinkCandidates(form);
  form.elements.relation.onchange=()=>searchLinkCandidates(form);
  for(const name of ['search_amount','search_date','search_text']){
   form.elements[name].oninput=()=>{const select=form.elements.transaction_id;select.dataset.request=crypto.randomUUID();select.disabled=false;select.innerHTML='<option value="">请重新搜索</option>';};
   form.elements[name].onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();searchLinkCandidates(form);}};
  }
  searchLinkCandidates(form);
 }
}
async function searchLinkCandidates(form){
 const select=form.elements.transaction_id;
 const type={PURCHASE:'EXPENSE',MAINTENANCE:'EXPENSE',SALE:'INCOME',RETURN:'REFUND',REFUND:'REFUND'}[form.elements.relation.value];
 const q=new URLSearchParams({type,limit:50}),request=select.dataset.request=crypto.randomUUID();
 if(form.elements.search_amount.value)q.set('amount',form.elements.search_amount.value);
 if(form.elements.search_text.value.trim())q.set('search',form.elements.search_text.value.trim());
 const date=form.elements.search_date.value;
 if(date){
  const from=new Date(date+'T00:00:00+08:00');
  q.set('from',from.toISOString());q.set('to',new Date(from.getTime()+86400000).toISOString());
 }
 select.disabled=true;select.innerHTML='<option value="">正在查找…</option>';$('#cash-error').textContent='';
 try{
  const {items}=await api('/financial/transactions?'+q);
  if(select.dataset.request!==request)return;
  select.innerHTML='<option value="">'+(items.length?'请选择对应流水':'没有匹配的流水')+'</option>'+items.map(t=>`<option value="${esc(t.id)}">${esc([when(t.occurred_at),t.merchant,t.note||t.annotation?.item_description,money(t.amount),accountName(t.account_id)].filter(Boolean).join(' · '))}</option>`).join('');
 }catch(err){if(select.isConnected&&select.dataset.request===request)$('#cash-error').textContent=WubuZh.error(err);}
 finally{if(select.dataset.request===request)select.disabled=false;}
}
async function submitCash(form){
 let body=Object.fromEntries(new FormData(form));const c=cashCommand.command,quick=c==='income'||c==='expense';let path='',pending=false;
 for(const field of ['amount','paid_amount','credited_amount','initial_balance','actual_balance','credit_limit']){if(field in body){if(body[field]==='')delete body[field];else body[field]=Number(body[field]);}}
 if(c==='account')path='/accounts';else if(c==='edit-account'){path='/accounts/'+cashCommand.account;body.active=body.active==='true';}else if(c==='category')path='/money/categories';
 else if(c==='link'){
  if(!body.transaction_id)throw new Error('请选择对应流水。');
  path='/links';body={transaction_id:body.transaction_id,relation:body.relation,source_id:cashCommand.linkSource,source_type:cashCommand.linkType};
 }
 else if(c==='sell')path="/items/"+cashCommand.item+"/events/sale";
 else if(c==='refund'){path='/finance/refunds';body.related_transaction_id=cashCommand.transaction;}
 else if(['move','status','retire'].includes(c))path=`/items/${cashCommand.item}/${c}`;
 else path='/money/'+({income:'income',expense:'expenses',transfer:'transfers',refund:'refunds',adjust:'adjustments','top-up':'top-ups'})[c];
 if(!['account','edit-account','category','link'].includes(c))body.source='manual';
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
async function extendDurableDetails(id){const [cost,history]=await Promise.all([api(`/items/${id}/total-cost`),api(`/items/${id}/durable-events?limit=100`)]);return panel('历史持有成本',`<p>购入 ${money(cost.purchase_cost)} + 维护 ${money(cost.maintenance_cost)} = ${money(cost.total_cost)}</p><p>退款 ${money(cost.refund)} · 净成本 ${money(cost.net_cost)} · 维护 ${cost.maintenance_count} 次</p>`)+panel('生命周期事件',table(['时间','动作','状态','位置 / 备注'],history.items.map(e=>[esc(when(e.occurred_at)),esc(typeLabel(e.type)),`${esc(typeLabel(e.from_status))} → ${esc(typeLabel(e.to_status))}`,`${esc(e.location)}<br>${esc(e.note)}`])));}
function linkLabel(relation){return {PURCHASE:'购入支出',REFUND:'退款',MAINTENANCE:'维护支出',SALE:'售出收款',RETURN:'退货退款'}[relation]||relation;}
function linkList(links){return links.length ? links.map(l=>`<p>${esc(linkLabel(l.relation))}${l.transaction_amount==null?'':' · '+money(l.transaction_amount)} · <a href="/financial-inbox?transaction=${encodeURIComponent(l.transaction_id)}">查看流水</a> <button data-unlink="${esc(l.id)}">取消关联</button></p>`).join('') : '<p>尚未关联资金流水。</p>';}
document.addEventListener('click',async e=>{
 const link=e.target.closest('[data-domain-link]');
 if(link){if($('#detail-dialog').open)$('#detail-dialog').close();await openCashDialog('link',link);}
 const unlink=e.target.closest('[data-unlink]');
 if(unlink){try{await api('/links/'+encodeURIComponent(unlink.dataset.unlink),{},crypto.randomUUID(),'DELETE');unlink.closest('p').remove();notify('已取消关联。');}catch(err){notify(WubuZh.error(err),true);}}
 const backfill=e.target.closest('[data-backfill]');
 if(backfill){if($('#detail-dialog').open)$('#detail-dialog').close();await openPurchaseForm(null,backfill.dataset);}
 const detail=e.target.closest('[data-account-detail]');if(detail){try{await accountDetail(detail.dataset.accountDetail);}catch(err){notify(WubuZh.error(err),true);}}
 const cash=e.target.closest('[data-cash-action]');if(cash){try{await openCashDialog(cash.dataset.cashAction,cash);}catch(err){notify(WubuZh.error(err),true);}}
 const def=e.target.closest('[data-default-account]');if(def){def.disabled=true;try{await api('/money/settings',{default_payment_account:def.dataset.defaultAccount},crypto.randomUUID(),'PATCH');notify('默认付款账户已设置。');await load();}catch(err){notify(WubuZh.error(err),true);}finally{def.disabled=false;}}
});
document.addEventListener('submit',async e=>{
 const form=e.target;if(form.id==='cash-filter'||form.id==='durable-filter'){e.preventDefault();if(form.id==='cash-filter')cashFilters=Object.fromEntries(new FormData(form));else durableFilters=Object.fromEntries(new FormData(form));offset=0;await load();}
 if(form.id==='cash-form'){e.preventDefault();const button=form.querySelector('[type=submit]');button.disabled=true;try{await submitCash(form);}catch(err){$('#cash-error').textContent=WubuZh.error(err);}finally{button.disabled=false;}}
});
