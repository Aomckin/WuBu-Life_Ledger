'use strict';
(() => {
const {$,esc,money,when,localInput,api,retryFor,notify,panel,table,dateRange,inputField}=WubuWorkspace;
const typeLabel = value => ({CONSUMABLE:'消耗品',DURABLE:'耐用品',UNTRACKED:'不跟库存',PURCHASE:'购入',RETURN:'退货',CONSUME:'消耗',ADJUST:'纠正',OPEN:'开封',FINISH:'用完',DISCARD:'弃置',ACTIVE:'在用',STORED:'收纳',RETIRED:'退休',PURCHASED:'购入',ACTIVATED:'启用',MOVED:'移动',FOUND:'找到',EXPENSE:'支出',INCOME:'收入',TRANSFER_OUT:'转出',TRANSFER_IN:'转入',BONUS:'赠送',REFUND:'退款',ADJUSTMENT:'余额纠正',SOLD:'已售',LOST:'丢失',DISCARDED:'弃置',ARCHIVED:'归档',DONE:'完成',PLANNED:'计划'}[value] || value);
const PAGE=50;
let view="home",offset=0,generation=0;
$("#month").value=localInput().slice(0,7);
let search='',itemsByID=new Map(),action=null,durableFilters={status:'',location:'',search:''};
let purchaseRetry=null, actionRetry=null, editingPurchase=null, backfillTransaction=null, deletingPurchase=null, deleteRetry=null;
$('#purchase-units').innerHTML=WubuZh.commonUnits.map(unit=>'<option value="'+esc(unit)+'"></option>').join('');
function itemName(id){return WubuZh.product(itemsByID.get(id)) || '查看物品';}
function itemButton(id){return `<button class="ledger-link" data-detail="${esc(id)}">${esc(itemName(id))}</button>`;}
function purchaseTable(data){return table(['时间 / 商家','商品','历史付款快照','查看'],data.map(p=>{
 return [`${esc(when(p.purchased_at))}<br>${esc(p.merchant)}`,p.items.map(l=>`${esc(WubuZh.product(l))} × ${esc(l.quantity)}${esc(WubuZh.unit(l.unit))}`).join('<br>'),p.total_amount?money(p.total_amount):'—',`<div class="ledger-actions"><button class="ledger-link" data-purchase="${esc(p.id)}">明细</button><button data-edit-purchase="${esc(p.id)}">改单</button><button class="purchase-delete" data-delete-purchase="${esc(p.id)}">删除</button></div>`];
}));}
function maintenanceTable(data){return table(['时间','物品','维护','费用'],data.map(m=>[esc(when(m.occurred_at)),itemButton(m.item_id),`${esc(m.title)}<br><span class="ledger-caption">${esc(typeLabel(m.status))} · ${esc(m.description)}</span>`,(m.cost?money(m.cost):'—')+` <button data-domain-link="MAINTENANCE" data-link-source="${esc(m.id)}">关联资金流水</button>`]));}
function eventTable(data){return table(['时间','物品','事件','数量变化','备注'],data.map(e=>[esc(when(e.occurred_at)),esc(itemName(e.item_id)),esc(typeLabel(e.type)),`${esc(e.quantity_before)} → ${esc(e.quantity_after)} (${e.quantity_delta>0?'+':''}${esc(e.quantity_delta)})`,esc(e.note)]));}

async function hydrateItemNames(records){
 const missing=[...new Set(records.map(r=>r.item_id).filter(id=>!itemsByID.has(id)))];
 const loaded=await Promise.allSettled(missing.map(id=>api('/items/'+encodeURIComponent(id))));
 loaded.forEach(result=>{if(result.status==='fulfilled')itemsByID.set(result.value.id,result.value);});
}
function supplyPanel(low){
 return panel('物资余量',table(['物品','当前数量','消耗预测'],low.map(f=>[itemButton(f.item_id),esc(f.current_quantity),f.status==='OK'?('约 '+Math.round(f.estimated_days_remaining)+' 天'):'数据不足'])));
}
async function extendDurableDetails(id){
 const {items}=await api('/items/'+encodeURIComponent(id)+'/durable-events?limit=100');
 return panel('生命周期事件',table(['时间','动作','状态','位置 / 备注'],items.map(e=>[esc(when(e.occurred_at)),esc(typeLabel(e.type)),esc(typeLabel(e.from_status))+' → '+esc(typeLabel(e.to_status)),esc(e.location)+'<br>'+esc(e.note)])));
}
async function load(){
 const token=++generation;$('#content').setAttribute('aria-busy','true');
 try{
  let html='',count=0;
  const params=`${dateRange()}&limit=${PAGE}&offset=${offset}`;
  if(view==='home'){
   const [stats, recent, low]=await Promise.all([api('/summary'),api('/items?limit=8&hide_out=true'),api('/items-running-low')]);
   recent.items.forEach(i=>itemsByID.set(i.id,i));
   await hydrateItemNames(low.items);
   html='<section class="metrics" aria-label="物品概览">'+[['记录物品',stats.tracked],['低库存',stats.low_stock],['近期到期',stats.expiring_soon]]
    .map(([name,value])=>'<div class="ledger-metric"><small>'+esc(name)+'</small><strong>'+esc(value)+'</strong></div>').join('')+'</section>'+
    panel('在库物品',table(['物品','剩余','位置'],recent.items.map(i=>[itemButton(i.id),esc(i.quantity)+' '+esc(WubuZh.unit(i.unit)),esc(WubuZh.location(i.location))])))+
    supplyPanel(low.items);
  }else if(view==='inventory'||view==='items'){
   const list=await api(`/items?search=${encodeURIComponent(search)}&limit=${PAGE}&offset=${offset}${view==='inventory'?'&hide_out=true':''}`);list.items.forEach(i=>itemsByID.set(i.id,i));count=list.items.length;
   html=`<form id="search-form" class="ledger-search"><input name="search" value="${esc(search)}" placeholder="名称、别名、备注" aria-label="搜索物品"><button class="button" type="submit">搜索</button></form>`+panel(view==='inventory'?'当前库存':'全部物品',table(['物品','类型 / 位置','剩余','单价','操作'],list.items.map(i=>[itemButton(i.id),`${esc(typeLabel(i.item_type))}<br>${esc(WubuZh.location(i.location))}`,`${esc(i.quantity)} ${esc(WubuZh.unit(i.unit))}${i.item_type==='CONSUMABLE'?`<br><button data-forecast-id="${esc(i.id)}">消耗预测</button>`:''}`,i.unit_price_cents===null?'—':money(i.unit_price_cents/100),`<div class="ledger-actions">${(i.item_type==='DURABLE'?['maintenance','return','alias']:['consume','return','adjust','open','finish','discard','alias']).map(a=>`<button data-action="${a}" data-id="${esc(i.id)}">${({consume:'消耗',return:'退货',adjust:'纠正',open:'开封',finish:'用完',discard:'弃置',alias:'别名',maintenance:'维护',status:'状态'})[a]}</button>`).join('')}</div>`])));
  }else if(view==='durables'){
  const data=await api('/durables?limit='+PAGE+'&offset='+offset+'&'+new URLSearchParams(durableFilters));count=data.items.length;
  html=`<form id="durable-filter" class="ledger-search"><select name="status"><option value="">全部状态</option>${['ACTIVE','STORED','LOST','SOLD','RETIRED'].map(t=>`<option value="${t}" ${durableFilters.status===t?'selected':''}>${esc(typeLabel(t))}</option>`).join('')}</select><input name="location" value="${esc(durableFilters.location)}" placeholder="位置"><input name="search" value="${esc(durableFilters.search)}" placeholder="耐用品名称"><button class="button">筛选</button></form>`+panel('耐用品仓库',table(['物品','状态 / 位置','购入 / 使用时长','购买价 / 保修','操作'],data.items.map(row=>{const d=row.details;return [`<button class="ledger-link" data-detail="${esc(d.item_id)}">${esc(row.name)}</button>`,`${esc(typeLabel(d.status))}<br>${esc(d.location||'未设位置')}`,`${esc(when(d.purchase_date))}<br>${d.use_days} 天`,`${d.purchase_price?money(d.purchase_price):'—'}<br>${d.warranty_days===null?'未记录保修':d.warranty_days>=0?'保修剩余 '+d.warranty_days+' 天':'保修已到期'}`,`<button data-item-action="metadata" data-item="${esc(d.item_id)}">资料</button>`+(['SOLD','RETIRED'].includes(d.status)?'历史保留':`<div class="ledger-actions"><button data-action="maintenance" data-id="${esc(d.item_id)}">维护</button>${['move','status','sell','retire'].map(k=>`<button data-item-action="${k}" data-item="${esc(d.item_id)}">${({move:'移动',status:'状态',sell:'出售',retire:'退休'})[k]}</button>`).join('')}</div>`)];})));

  }else{
   let data;
   if(view==='purchases'){data=await api('/acquisitions?'+params);html=panel('购买记录 · 所选月份',purchaseTable(data.items));}
   if(view==='maintenance'){data=await api('/maintenance?'+params);await hydrateItemNames(data.items);html=panel('维护记录 · 所选月份',maintenanceTable(data.items));}
   if(view==='history'){data=await api('/inventory-events?'+params);await hydrateItemNames(data.items);html=panel('库存历史 · 所选月份',eventTable(data.items));}
   count=data.items.length;
  }
  if(token!==generation)return;
  $('#content').innerHTML=html;$('.workspace-period').hidden=!['purchases','maintenance','history'].includes(view);$('#paging').hidden=view==='home';$('#previous').disabled=offset===0;$('#next').disabled=count<PAGE;$('#page-label').textContent=`第 ${Math.floor(offset/PAGE)+1} 页`;
 }catch(e){notify(WubuZh.error(e),true);}finally{if(token===generation)$('#content').removeAttribute('aria-busy');}
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
function linkLabel(relation){return {PURCHASE:'购入支出',REFUND:'退款',MAINTENANCE:'维护支出',SALE:'售出收款',RETURN:'退货退款'}[relation]||relation;}
function linkList(links){return links.length ? links.map(l=>`<p>${esc(linkLabel(l.relation))}${l.transaction_amount==null?'':' · '+money(l.transaction_amount)} · <a href="/financial-inbox?transaction=${encodeURIComponent(l.transaction_id)}">查看流水</a> <button data-unlink="${esc(l.id)}">取消关联</button></p>`).join('') : '<p>尚未关联资金流水。</p>';}

let optionalAction=null,optionalRetry=null,linkAccounts=[];
const accountName=id=>linkAccounts.find(a=>a.id===id)?.name||'';
const backfillParams=new URLSearchParams(location.search);
function loadBackfillReceipt(){
 const saved=sessionStorage.getItem('wubu-acquisition:'+backfillTransaction);
 if(!saved)return null;
 return JSON.parse(saved);
}
function saveBackfillReceipt(retry){sessionStorage.setItem('wubu-acquisition:'+backfillTransaction,JSON.stringify(retry));}
function clearBackfill(){
 if(!backfillTransaction)return;
 sessionStorage.removeItem('wubu-acquisition:'+backfillTransaction);
 for(const field of ['backfill','time','merchant'])backfillParams.delete(field);
 history.replaceState({},'',location.pathname+(backfillParams.size?'?'+backfillParams:''));
 backfillTransaction=null;
}
function showOptional(title,fields,action){
 optionalAction=action;optionalRetry=null;
 $('#cash-title').textContent=title;
 $('#cash-error').textContent='';
 $('#cash-fields').innerHTML=fields;
 if($('#detail-dialog').open)$('#detail-dialog').close();
 $('#cash-dialog').showModal();
}
async function chooseLink(button){
 const {domainLink:type,linkSource:id}=button.dataset;
 const choices={ITEM:['SALE','RETURN'],MAINTENANCE:['MAINTENANCE','REFUND'],ITEM_ACQUISITION:['PURCHASE','REFUND']}[type];
 const listing=await api('/links?'+new URLSearchParams({source_type:type,source_id:id}));
 const fields='<div class="existing-links">'+linkList(listing.items)+'</div>'+
  '<label>用途<select name="relation">'+choices.map(k=>'<option value="'+k+'">'+linkLabel(k)+'</option>').join('')+'</select></label>'+
  inputField('金额','search_amount','number','','min="0.01" step="0.01"')+inputField('日期','search_date','date')+
  inputField('商家或描述','search_text','text','','maxlength="200"')+
  '<button type="button" class="button" id="search-link">查找流水</button><label>选择流水<select name="transaction_id" required></select></label>';
 showOptional('关联资金记录',fields,{kind:'link',type,id});
 const form=$('#cash-form');
 $('#search-link').onclick=()=>searchLinkCandidates(form);
 form.elements.relation.onchange=()=>searchLinkCandidates(form);
 for(const name of ['search_amount','search_date','search_text']){
  form.elements[name].oninput=()=>{const select=form.elements.transaction_id;select.dataset.request=crypto.randomUUID();select.disabled=false;select.innerHTML='<option value="">请重新搜索</option>';};
  form.elements[name].onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();searchLinkCandidates(form);}};
 }
 try{linkAccounts=(await api('/accounts')).items;}
 catch(error){$('#cash-error').textContent=WubuZh.error(error);}
 if($('#cash-dialog').open&&form.elements.relation)await searchLinkCandidates(form);
}
async function openItemAction(kind,id){
 let title={move:'移动物品',status:'耐用品状态',sell:'出售耐用品',retire:'退休 / 报废',metadata:'耐用品资料'}[kind],fields='';
 if(kind==='move')fields=inputField('新位置','location','text','','required maxlength="100"');
 if(kind==='status')fields='<label>状态<select name="status"><option value="ACTIVE">在用 / 找到</option><option value="STORED">收纳 / 闲置</option><option value="LOST">丢失</option></select></label>';
 if(kind==='sell')fields='<p>记录该物品已售出。到账后可在金额账户单独登记。</p>';
 if(kind==='retire')fields='<p>退休后保留历史，库存数量归零。</p>';
 if(kind==='metadata'){
  const d=await api('/items/'+encodeURIComponent(id)+'/durable');
  fields=inputField('序列号','serial_number','text',d.serial_number||'')+
   inputField('保修到期','warranty_until','date',d.warranty_until||'')+
   inputField('购买凭证','purchase_proof','text',d.purchase_proof||'','maxlength="2000"')+
   inputField('参考转售价（元）','estimated_resale_value','number',d.estimated_resale_value??'','min="0" step="0.01"')+
   '<label>原包装<select name="has_original_packaging">'+[['','未记录'],['true','保留'],['false','未保留']].map(([key,label])=>'<option value="'+key+'" '+(String(d.has_original_packaging)===key?'selected':'')+'>'+label+'</option>').join('')+'</select></label>'+
   '<button type="button" class="button" id="choose-residence">选择所属居住</button><div id="residence-field"></div>';
 }else fields+=inputField('备注','note','text','','maxlength="2000"');
 showOptional(title,fields,{kind,id});
 if(kind==='metadata')$('#choose-residence').onclick=async event=>{
  const button=event.target;
  button.disabled=true;
  try{
   const [{items},d]=await Promise.all([api('/residences'),api('/items/'+encodeURIComponent(id)+'/durable')]);
   $('#residence-field').innerHTML='<label>所属居住<select name="residence_id"><option value="">无</option>'+items.map(r=>'<option value="'+esc(r.id)+'" '+(d.residence_id===r.id?'selected':'')+'>'+esc(r.name)+'</option>').join('')+'</select></label>';
   button.hidden=true;
  }catch(error){$('#cash-error').textContent=WubuZh.error(error);}
  finally{button.disabled=false;}
 };
}
document.addEventListener('click',async event=>{
 const b=event.target.closest('[data-item-action],[data-domain-link],[data-unlink],[data-forecast-id]');
 if(!b)return;
 try{
  if(b.dataset.domainLink){await chooseLink(b);return;}
  if(b.dataset.unlink){
   await api('/links/'+encodeURIComponent(b.dataset.unlink),{},crypto.randomUUID(),'DELETE');
   b.closest('p').remove();return;
  }
  if(b.dataset.forecastId){
   const f=await api('/items/'+encodeURIComponent(b.dataset.forecastId)+'/consumption-forecast');
   $('#detail-title').textContent='消耗预测';
   $('#detail-content').textContent=f.status==='OK'?'预计约 '+Math.round(f.estimated_days_remaining)+' 天':'数据不足，需至少 3 次消耗并观察 7 天。';
   $('#detail-dialog').showModal();return;
  }
  await openItemAction(b.dataset.itemAction,b.dataset.item||b.dataset.livingId);
 }catch(error){notify(WubuZh.error(error),true);}
});
$('#cash-form').addEventListener('submit',async event=>{
 event.preventDefault();
 const button=event.submitter;
 const {kind,id,type}=optionalAction;
 const form=event.target;
 let body=Object.fromEntries(new FormData(form)),method='POST',path;
 if(kind==='link'){
  if(!body.transaction_id){$('#cash-error').textContent='请选择对应流水。';return;}
  body={source_id:id,source_type:type,transaction_id:body.transaction_id,relation:body.relation};
  path='/links';
 }else if(kind==='metadata'){
  path='/items/'+encodeURIComponent(id)+'/durable';method='PATCH';
  if(body.estimated_resale_value==='')delete body.estimated_resale_value;else body.estimated_resale_value=Number(body.estimated_resale_value);
  if(body.has_original_packaging==='')delete body.has_original_packaging;else body.has_original_packaging=body.has_original_packaging==='true';
 }else{
  path='/items/'+encodeURIComponent(id)+'/'+(kind==='sell'?'events/sale':kind);body.source='manual';
 }
 button.disabled=true;
 try{
  optionalRetry=retryFor(optionalRetry,body,path);
  await api(path,body,optionalRetry.key,method);
  $('#cash-dialog').close();await load();notify('已保存。');
 }catch(error){$('#cash-error').textContent=WubuZh.error(error);}
 finally{button.disabled=false;}
});
function purchaseLine(line=null){
 const el=document.createElement('div');el.className='line';el.originalLine=line;
 el.innerHTML='<label>物品名称<input name="name" maxlength="80" placeholder="例如：无糖可乐" required></label>'+
  '<label>数量<input name="quantity" value="1" placeholder="例如：2 瓶" maxlength="35" required></label>'+
  '<label>规格（可选）<input name="spec" maxlength="100" placeholder="例如：2L"></label>'+
  '<details class="purchase-line-options"><summary>更多选项</summary><div class="form-grid">'+
  '<label>单位<input name="unit" list="purchase-units" maxlength="20" placeholder="沿用现有单位，默认个"></label>'+
  '<label>位置<input name="location" maxlength="100"></label><label>类型<select name="item_type"><option value="CONSUMABLE">消耗品</option><option value="DURABLE">耐用品</option><option value="UNTRACKED">不跟库存</option></select></label>'+
  '<button type="button" class="button" data-remove-line>删除此行</button></div></details>';
 if(line){['name','spec','quantity','location','item_type'].forEach(name=>el.querySelector(`[name="${name}"]`).value=line[name]??'');WubuZh.setEditable(el.querySelector('[name="unit"]'),line.unit,'unit');el.querySelector('[name="item_type"]').disabled=true;}
 $('#purchase-lines').append(el);
}
function purchaseLinesPayload(){
 return [...document.querySelectorAll('#purchase-lines .line')].map(line=>{
  const values=Object.fromEntries([...line.querySelectorAll('input,select')].map(input=>[input.name,input.value]));
  if(line.originalLine)values.unit=WubuZh.originalValue(line.querySelector('[name="unit"]'));
  const match=values.quantity.trim().match(/^(\d+(?:\.\d{1,3})?)\s*([^\d\s]{1,20})?$/u);
  if(!match||Number(match[1])<=0)throw new Error('请输入正数数量，例如 2 或 2 瓶。');
  values.quantity=Number(match[1]);
  if(match[2]){
   if(values.unit&&WubuZh.unit(values.unit)!==WubuZh.unit(match[2]))throw new Error('数量中的单位与更多选项中的单位不同。');
   values.unit=match[2];
  }
  const original={...(line.originalLine||{})};delete original.package_size;delete original.package_unit;delete original.inventory_after;delete original.quantity_added;delete original.unit_price;delete original.subtotal;delete original.confidence;return {...original,...values};
 });
}
async function openPurchaseForm(purchase=null,transaction=null){
 const form=$('#purchase-form');form.reset();$('#purchase-options').open=false;editingPurchase=purchase;backfillTransaction=transaction?.backfill;purchaseRetry=backfillTransaction?loadBackfillReceipt():null;
 $('#purchase-title').textContent=purchase?'修改购买记录':transaction?'从流水后补购入':'记录购买';$('#save-purchase').textContent=purchase?'保存修改':'保存购入';
 $('#purchase-lines').replaceChildren();$('#purchase-error').textContent='';
 if(purchase){
  ['merchant','note'].forEach(name=>form.elements[name].value=purchase[name]??'');
  form.elements.purchased_at.value=new Date(new Date(purchase.purchased_at).getTime()+8*3600000).toISOString().slice(0,16);
  purchase.items.forEach(purchaseLine);
 }else{
  form.elements.purchased_at.value=transaction?.time?new Date(new Date(transaction.time).getTime()+8*3600000).toISOString().slice(0,16):localInput();
  if(transaction?.merchant)form.elements.merchant.value=transaction.merchant;
  purchaseLine();
 }
 $('#purchase-dialog').showModal();
}
$('#new-purchase').addEventListener('click',()=>openPurchaseForm());
async function editPurchase(id){try{const p=await api('/acquisitions/'+id);$('#detail-dialog').close();openPurchaseForm(p);}catch(e){notify(WubuZh.error(e),true);}}
async function confirmDeletePurchase(id){try{
 deletingPurchase=await api('/acquisitions/'+id);deleteRetry=null;
 $('#purchase-delete-summary').textContent=`${deletingPurchase.merchant||'这笔购买'} · ${deletingPurchase.items.map(line=>WubuZh.product(line)).join('、')}`;
 $('#purchase-delete-error').textContent='';$('#detail-dialog').close();$('#purchase-delete-dialog').showModal();
}catch(e){notify(WubuZh.error(e),true);}}
$('#confirm-purchase-delete').addEventListener('click',async()=>{
 if(!deletingPurchase)return;const button=$('#confirm-purchase-delete');button.disabled=true;
 const path='/acquisitions/'+deletingPurchase.id,body={expected_updated_at:deletingPurchase.updated_at};
 try{deleteRetry=retryFor(deleteRetry,body,path);await api(path,body,deleteRetry.key,'DELETE');$('#purchase-delete-dialog').close();deletingPurchase=null;notify('已撤销购入及对应的库存增加。');await load();}
 catch(e){$('#purchase-delete-error').textContent=WubuZh.error(e);}finally{button.disabled=false;}
});
$('#add-line').addEventListener('click',()=>purchaseLine());
$('#purchase-lines').addEventListener('click',e=>{if(e.target.closest('[data-remove-line]'))e.target.closest('.line').remove();});
$('#purchase-form').addEventListener('submit',async e=>{
 e.preventDefault();const form=e.currentTarget,values=new FormData(form);
 const originalTime=editingPurchase?new Date(new Date(editingPurchase.purchased_at).getTime()+8*3600000).toISOString().slice(0,16):null;
 const body={merchant:values.get('merchant'),occurred_at:editingPurchase&&values.get('purchased_at')===originalTime?editingPurchase.purchased_at:new Date(values.get('purchased_at')+'+08:00').toISOString(),source:editingPurchase?.source||'manual',external_id:editingPurchase?.external_id||'',note:values.get('note')};
 if(editingPurchase)body.expected_updated_at=editingPurchase.updated_at;
 const path=editingPurchase?'/acquisitions/'+editingPurchase.id:backfillTransaction?'/financial/transactions/'+encodeURIComponent(backfillTransaction)+'/acquisition':'/acquisitions',button=$('#save-purchase');button.disabled=true;
 try{body.items=purchaseLinesPayload();if(!body.items.length)throw new Error('请至少添加一个商品。');purchaseRetry=retryFor(purchaseRetry,body,path);if(backfillTransaction)saveBackfillReceipt(purchaseRetry);await api(path,body,purchaseRetry.key,editingPurchase?'PUT':'POST');$('#purchase-dialog').close();notify(editingPurchase?'已更新购入及库存。':'已记录购入。');editingPurchase=null;clearBackfill();await load();}
 catch(err){$('#purchase-error').textContent=WubuZh.error(err);}finally{button.disabled=false;}
});
async function showAction(id,kind){let item=itemsByID.get(id);if(!item){try{item=await api('/items/'+id);itemsByID.set(id,item);}catch(e){notify(WubuZh.error(e),true);return;}}action={id,kind};actionRetry=null;$('#action-error').textContent='';$('#action-title').textContent=`${({consume:'消耗',return:'退货',adjust:'纠正库存',open:'开封',finish:'用完',discard:'弃置',alias:'添加别名',maintenance:'记录维护',status:'耐用品状态'})[kind]} · ${WubuZh.product(item)}`;
 let fields='';if(['consume','return','adjust'].includes(kind))fields=inputField(kind==='return'?'退回数量':kind==='consume'?`消耗多少（${WubuZh.unit(item.unit)}）`:`实际剩余（${WubuZh.unit(item.unit)}）`,'quantity','number',kind==='adjust'?item.quantity:1,`min="${kind==='adjust'?'0':'0.001'}" step="0.001" required`);
 if(kind==='alias')fields=inputField('别名','alias','text','','maxlength="100" required');
 if(kind==='maintenance')fields=inputField('维护标题','title','text','','maxlength="200" required')+inputField('维护说明','description','text','','maxlength="2000"');
 if(kind==='status')fields='<label>状态<select name="status"><option value="ACTIVE">在用</option><option value="STORED">收纳</option><option value="LOST">丢失</option><option value="RETIRED">退休</option></select></label>'+inputField('品牌','brand')+inputField('型号','model')+inputField('序列号','serial_number')+inputField('保修到期','warranty_until','date');
 if(kind==='finish'||kind==='discard')fields='<p>确认后将剩余数量归零，并保留历史记录。</p>';
 fields+=inputField('备注','note','text','','maxlength="2000"');$('#action-fields').innerHTML=fields;$('#action-dialog').showModal();
 if(kind==='status')api(`/items/${id}/durable`).then(d=>{if(action?.id!==id||action?.kind!==kind)return;const f=$('#action-form');['status','brand','model','serial_number','warranty_until','note'].forEach(k=>f.elements[k].value=(k==='status'&&d[k]==='SOLD'?'':d[k]||''));}).catch(e=>$('#action-error').textContent=WubuZh.error(e));
}
$('#action-form').addEventListener('submit',async e=>{e.preventDefault();const form=e.currentTarget;const body=Object.fromEntries(new FormData(form));let kind=action.kind;if(['consume','return','adjust'].includes(kind))body.quantity=Number(body.quantity);if(kind!=='status'&&kind!=='alias')body.source='manual';if(kind==='alias')delete body.note;
 const path=`/items/${action.id}/${kind==='alias'?'aliases':kind==='status'?'durable':['maintenance','return'].includes(kind)?'events/'+kind:kind}`;const button=form.querySelector('[type=submit]');button.disabled=true;try{actionRetry=retryFor(actionRetry,body,path);await api(path,body,actionRetry.key,kind==='status'?'PATCH':'POST');$('#action-dialog').close();notify('已保存，历史已同步更新。');await load();}catch(err){$('#action-error').textContent=WubuZh.error(err);}finally{button.disabled=false;}});
async function details(id){try{const item=await api('/items/'+id);itemsByID.set(id,item);const [events,prices,maintenance,aliases]=await Promise.all(['/inventory-events','/prices','/maintenance','/aliases'].map(path=>api(`/items/${id}${path}?limit=100`)));$('#detail-title').textContent=WubuZh.product(item);let html=panel('当前物品',`<p>${esc(typeLabel(item.item_type))} · ${esc(item.quantity)} ${esc(WubuZh.unit(item.unit))} · ${esc(WubuZh.location(item.location)||'未设位置')}</p><p>开封时间：${esc(when(item.opened_at))}</p><p>${esc(item.notes)}</p>`);if(item.item_type==='DURABLE'){const d=await api(`/items/${id}/durable`);html+=panel('耐用品生命周期',`<p>状态：${esc(typeLabel(d.status))} · 购买：${esc(when(d.purchase_date))}</p><p>${esc(d.brand)} ${esc(d.model)} · 序列号：${esc(d.serial_number||'—')}</p><p>保修到期：${esc(d.warranty_until||'—')} · 售出：${esc(when(d.sold_at))} ${d.sold_price===null?'':money(d.sold_price)}</p>`);}if(item.item_type==='DURABLE')html+=await extendDurableDetails(id);

 html+=panel('资金关联',`<button data-domain-link="ITEM" data-link-source="${esc(id)}">关联资金记录</button>`);html+=panel('名称别名',aliases.items.length?aliases.items.map(a=>esc(a.alias)).join(' · '):'<p>尚无别名。</p>')+panel('购买价格',table(['购买时间','商家','单价','数量','小计'],prices.items.map(p=>[esc(when(p.purchased_at)),esc(p.merchant),money(p.unit_price),esc(p.quantity),money(p.subtotal)])))+panel('库存事件',eventTable(events.items))+panel('维护历史',maintenanceTable(maintenance.items));$('#detail-content').innerHTML=html;$('#detail-dialog').showModal();}catch(e){notify(WubuZh.error(e),true);}}
async function purchaseDetail(id){try{const p=await api('/acquisitions/'+id);$('#detail-title').textContent=p.merchant||'购买明细';$('#detail-content').innerHTML=panel('购买事件',`<p>${esc(when(p.purchased_at))}</p><p>${esc(p.note)}</p>`)+table(['商品','数量','单位','类型'],p.items.map(l=>[esc(WubuZh.product(l)),esc(l.quantity),esc(WubuZh.unit(l.unit)),esc(typeLabel(l.item_type))]))+panel('资金关联',`<button data-domain-link="ITEM_ACQUISITION" data-link-source="${esc(p.id)}">关联资金记录</button>`)+`<div class="dialog-actions"><button class="button button-secondary" data-edit-purchase="${esc(p.id)}">改单</button><button class="button button-danger" data-delete-purchase="${esc(p.id)}">删除购买</button></div>`;$('#detail-dialog').showModal();}catch(e){notify(WubuZh.error(e),true);}}
document.addEventListener('click',e=>{const close=e.target.closest('[data-close]');if(close)$('#'+close.dataset.close).close();const a=e.target.closest('[data-action]');if(a)showAction(a.dataset.id,a.dataset.action);const d=e.target.closest('[data-detail]');if(d)details(d.dataset.detail);const p=e.target.closest('[data-purchase]');if(p)purchaseDetail(p.dataset.purchase);const edit=e.target.closest('[data-edit-purchase]');if(edit)editPurchase(edit.dataset.editPurchase);const del=e.target.closest('[data-delete-purchase]');if(del)confirmDeletePurchase(del.dataset.deletePurchase);});
$('.ledger-nav').addEventListener('click',e=>{const b=e.target.closest('[data-view]');if(!b)return;view=b.dataset.view;offset=0;document.querySelectorAll('[data-view]').forEach(el=>{el.classList.toggle('is-active',el===b);el.setAttribute('aria-pressed',String(el===b));});load();});
$('#content').addEventListener('submit',e=>{if(e.target.id==='durable-filter'){e.preventDefault();durableFilters=Object.fromEntries(new FormData(e.target));offset=0;load();}else if(e.target.id==='search-form'){e.preventDefault();search=new FormData(e.target).get('search');offset=0;load();}});
$('#month').addEventListener('change',()=>{if(!$('#month').value)$('#month').value=localInput().slice(0,7);offset=0;load();});$('#previous').addEventListener('click',()=>{offset=Math.max(0,offset-PAGE);load();});$('#next').addEventListener('click',()=>{offset+=PAGE;load();});
load();

const itemID=backfillParams.get('item');
if(itemID)details(itemID);
const backfill=backfillParams.get('backfill');
if(backfill)openPurchaseForm(null,{backfill,time:backfillParams.get('time'),merchant:backfillParams.get('merchant')});
})();
