'use strict';
const $ = selector => document.querySelector(selector);
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const money = value => new Intl.NumberFormat('zh-CN',{style:'currency',currency:'CNY'}).format(value || 0);
const when = value => value ? new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false}) : '—';
const typeLabel = value => ({CONSUMABLE:'消耗品',DURABLE:'耐用品',UNTRACKED:'不跟库存',PURCHASE:'购入',CONSUME:'消耗',ADJUST:'纠正',OPEN:'开封',FINISH:'用完',DISCARD:'弃置',ACTIVE:'在用',SOLD:'已售',LOST:'丢失',DISCARDED:'弃置',ARCHIVED:'归档',DONE:'完成',PLANNED:'计划'}[value] || value);
const localInput = () => new Date(Date.now()+8*3600000).toISOString().slice(0,16);
let view='home', offset=0, search='', itemsByID=new Map(), action=null, generation=0;
let purchaseRetry=null, actionRetry=null;
const PAGE=50;
$('#month').value=localInput().slice(0,7);
async function api(path,body,key,method='POST'){
 const options=body === undefined ? {} : {method,headers:{'Content-Type':'application/json',...(key?{'Idempotency-Key':key}:{})},body:JSON.stringify(body)};
 const response=await fetch('/api/v1'+path,options);
 const data=await response.json();
 if(!response.ok) throw new Error(WubuZh.error(data.error, response.status));
 return data;
}
function retryFor(previous,body,path){const fingerprint=JSON.stringify({body,path});return previous?.fingerprint===fingerprint?previous:{fingerprint,key:crypto.randomUUID()};}
function notify(text,error=false){$('#message').className=error?'ledger-warning':'ledger-success';$('#message').textContent=text;}
function panel(title,content){return `<section class="ledger-panel"><h2>${esc(title)}</h2>${content}</section>`;}
function table(headers,rows){return rows.length?`<div class="ledger-table-wrap"><table class="ledger-table"><thead><tr>${headers.map(h=>`<th>${esc(h)}</th>`).join('')}</tr></thead><tbody>${rows.map(r=>`<tr>${r.map(c=>`<td>${c}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`:'<p class="ledger-empty">这里还没有记录。</p>';}
function itemName(id){return itemsByID.get(id)?.name || id || '—';}
function itemButton(id){return `<button class="ledger-link" data-detail="${esc(id)}">${esc(itemName(id))}</button>`;}
function dateRange(){const [year,month]=$('#month').value.split('-').map(Number);const start=new Date(Date.UTC(year,month-1,1)-8*3600000),end=new Date(Date.UTC(year,month,1)-8*3600000);return `from=${encodeURIComponent(start.toISOString())}&to=${encodeURIComponent(end.toISOString())}`;}
function purchaseTable(data){return table(['时间 / 商家','商品','实际付款','查看'],data.map(p=>[`${esc(when(p.purchased_at))}<br>${esc(p.merchant)}`,p.items.map(l=>`${esc(l.name)} × ${esc(l.quantity)}${esc(WubuZh.unit(l.unit))}`).join('<br>'),money(p.total_amount),`<button class="ledger-link" data-purchase="${esc(p.id)}">明细</button>`]));}
function maintenanceTable(data){return table(['时间','物品','维护','费用'],data.map(m=>[esc(when(m.occurred_at)),itemButton(m.item_id),`${esc(m.title)}<br><span class="ledger-caption">${esc(typeLabel(m.status))} · ${esc(m.description)}</span>`,money(m.cost)]));}
function eventTable(data){return table(['时间','物品','事件','数量变化','备注'],data.map(e=>[esc(when(e.occurred_at)),esc(itemName(e.item_id)),esc(typeLabel(e.type)),`${esc(e.quantity_before)} → ${esc(e.quantity_after)} (${e.quantity_delta>0?'+':''}${esc(e.quantity_delta)})`,esc(e.note)]));}
async function summary(){const d=await api(`/ledger/summary?month=${$('#month').value}`);$('#metrics').innerHTML=[['本月支出',money(d.month_expense)],['在册物品',d.inventory.tracked],['库存价值',money(d.inventory.inventory_value_cents/100)],['需要补货',d.inventory.low_stock]].map(([k,v])=>`<div class="ledger-metric"><small>${esc(k)}</small><strong>${esc(v)}</strong></div>`).join('');return d;}
async function load(){
 const token=++generation;$('#content').setAttribute('aria-busy','true');
 try{
  const d=await summary();let html='',count=0;
  const params=`${dateRange()}&limit=${PAGE}&offset=${offset}`;
  if(view==='home'){
   const list=await api('/items?limit=500');itemsByID=new Map(list.items.map(i=>[i.id,i]));
   html=panel('最近购买',purchaseTable(d.recent_purchases))+`<div class="ledger-card-grid">${panel('近期消耗',eventTable(d.recent_consumption))}${panel('最近维护',maintenanceTable(d.recent_maintenance))}</div>`;
  }else if(view==='inventory'||view==='items'){
   const list=await api(`/items?search=${encodeURIComponent(search)}&limit=${PAGE}&offset=${offset}${view==='inventory'?'&hide_out=true':''}`);list.items.forEach(i=>itemsByID.set(i.id,i));count=list.items.length;
   html=`<form id="search-form" class="ledger-search"><input name="search" value="${esc(search)}" placeholder="名称、别名、备注" aria-label="搜索物品"><button class="button" type="submit">搜索</button></form>`+panel(view==='inventory'?'当前库存':'全部物品',table(['物品','类型 / 位置','剩余','单价','操作'],list.items.map(i=>[itemButton(i.id),`${esc(typeLabel(i.item_type))}<br>${esc(WubuZh.location(i.location))}`,`${esc(i.quantity)} ${esc(WubuZh.unit(i.unit))}`,i.unit_price_cents===null?'—':money(i.unit_price_cents/100),`<div class="ledger-actions">${(i.item_type==='DURABLE'?['maintenance','status','alias']:['consume','adjust','open','finish','discard','alias']).map(a=>`<button data-action="${a}" data-id="${esc(i.id)}">${({consume:'消耗',adjust:'纠正',open:'开封',finish:'用完',discard:'弃置',alias:'别名',maintenance:'维护',status:'状态'})[a]}</button>`).join('')}</div>`])));
  }else{
   let data;
   if(view==='purchases'){data=await api('/purchases?'+params);html=panel('购买记录 · 所选月份',purchaseTable(data.items));}
   if(view==='expenses'){data=await api('/expenses?'+params);html=panel(`消费记录 · 本月合计 ${money(data.total_amount)}`,table(['时间','商家 / 类别','金额','备注'],data.items.map(e=>[esc(when(e.occurred_at)),`${esc(e.merchant||'维护')}<br>${esc(WubuZh.category(e.category))}`,money(e.amount),esc(e.note)])));}
   if(view==='maintenance'){data=await api('/maintenance?'+params);html=panel('维护记录 · 所选月份',maintenanceTable(data.items));}
   if(view==='history'){data=await api('/inventory-events?'+params);html=panel('库存历史 · 所选月份',eventTable(data.items));}
   count=data.items.length;
  }
  if(token!==generation)return;
  $('#content').innerHTML=html;$('#paging').hidden=view==='home';$('#previous').disabled=offset===0;$('#next').disabled=count<PAGE;$('#page-label').textContent=`第 ${Math.floor(offset/PAGE)+1} 页`;
 }catch(e){notify(WubuZh.error(e),true);}finally{if(token===generation)$('#content').removeAttribute('aria-busy');}
}
function purchaseLine(){const el=document.createElement('div');el.className='line';el.innerHTML='<label>商品名称<input name="name" maxlength="80" required></label><label>数量<input name="quantity" type="number" min="0.001" step="0.001" value="1" required></label><label>单位<input name="unit" maxlength="20" value="个" required></label><label>单价（元）<input name="unit_price" type="number" min="0" step="0.01" value="0" required></label><label>小计（元）<input name="subtotal" type="number" min="0" step="0.01" value="0" required></label><label>类型<select name="item_type"><option value="CONSUMABLE">消耗品</option><option value="DURABLE">耐用品</option><option value="UNTRACKED">不跟库存</option></select></label><button type="button" class="button" data-remove-line aria-label="删除商品行">×</button>';$('#purchase-lines').append(el);}
$('#new-purchase').addEventListener('click',()=>{$('#purchase-form').reset();$('#purchase-form').elements.purchased_at.value=localInput();$('#purchase-lines').replaceChildren();purchaseLine();purchaseRetry=null;$('#purchase-error').textContent='';$('#purchase-dialog').showModal();});
$('#add-line').addEventListener('click',purchaseLine);
$('#purchase-lines').addEventListener('click',e=>{if(e.target.closest('[data-remove-line]'))e.target.closest('.line').remove();});
$('#purchase-lines').addEventListener('input',e=>{if(['quantity','unit_price'].includes(e.target.name)){const line=e.target.closest('.line');line.querySelector('[name=subtotal]').value=(Number(line.querySelector('[name=quantity]').value)*Number(line.querySelector('[name=unit_price]').value)).toFixed(2);}});
$('#purchase-form').addEventListener('submit',async e=>{
 e.preventDefault();const form=e.currentTarget;const values=new FormData(form);const body={merchant:values.get('merchant'),purchased_at:new Date(values.get('purchased_at')+'+08:00').toISOString(),total_amount:Number(values.get('total_amount')),payment_method:values.get('payment_method'),category:values.get('category'),currency:'CNY',source:'manual',note:values.get('note'),items:[...document.querySelectorAll('.line')].map(l=>Object.fromEntries([...l.querySelectorAll('input,select')].map(i=>[i.name,['quantity','unit_price','subtotal'].includes(i.name)?Number(i.value):i.value])))};
 const button=form.querySelector('[type=submit]');button.disabled=true;
 try{if(!body.items.length)throw new Error('请至少添加一个商品。');purchaseRetry=retryFor(purchaseRetry,body,'/purchases');await api('/purchases',body,purchaseRetry.key);$('#purchase-dialog').close();notify(`已记录购买 ${money(body.total_amount)}`);await load();}catch(err){$('#purchase-error').textContent=WubuZh.error(err);}finally{button.disabled=false;}
});
function inputField(label,name,type='text',value='',extra=''){return `<label>${esc(label)}<input name="${name}" type="${type}" value="${esc(value)}" ${extra}></label>`;}
function showAction(id,kind){const item=itemsByID.get(id);if(!item)return;action={id,kind};actionRetry=null;$('#action-error').textContent='';$('#action-title').textContent=`${({consume:'消耗',adjust:'纠正库存',open:'开封',finish:'用完',discard:'弃置',alias:'添加别名',maintenance:'记录维护',status:'耐用品状态'})[kind]} · ${item.name}`;
 let fields='';if(kind==='consume'||kind==='adjust')fields=inputField(kind==='consume'?`消耗多少（${WubuZh.unit(item.unit)}）`:`实际剩余（${WubuZh.unit(item.unit)}）`,'quantity','number',kind==='consume'?1:item.quantity,`min="${kind==='consume'?'0.001':'0'}" step="0.001" required`);
 if(kind==='alias')fields=inputField('别名','alias','text','','maxlength="100" required');
 if(kind==='maintenance')fields=inputField('维护标题','title','text','','maxlength="200" required')+inputField('实际费用（元）','cost','number',0,'min="0" step="0.01" required')+inputField('维护说明','description','text','','maxlength="2000"');
 if(kind==='status')fields='<label>状态<select name="status"><option value="ACTIVE">在用</option><option value="SOLD">已售</option><option value="LOST">丢失</option><option value="DISCARDED">弃置</option><option value="ARCHIVED">归档</option></select></label>'+inputField('售出时间（已售时必填）','sold_at','datetime-local')+inputField('售出价格（元）','sold_price','number','','min="0" step="0.01"')+inputField('品牌','brand')+inputField('型号','model')+inputField('序列号','serial_number')+inputField('保修到期','warranty_until','date');
 if(kind==='finish'||kind==='discard')fields='<p>确认后将剩余数量归零，并保留历史记录。</p>';
 fields+=inputField('备注','note','text','','maxlength="2000"');$('#action-fields').innerHTML=fields;$('#action-dialog').showModal();
 if(kind==='status')api(`/items/${id}/durable`).then(d=>{if(action?.id!==id||action?.kind!==kind)return;const f=$('#action-form');['status','brand','model','serial_number','warranty_until','note'].forEach(k=>f.elements[k].value=d[k]||'');if(d.sold_at)f.elements.sold_at.value=new Date(new Date(d.sold_at).getTime()+8*3600000).toISOString().slice(0,16);if(d.sold_price!==null)f.elements.sold_price.value=d.sold_price;}).catch(e=>$('#action-error').textContent=WubuZh.error(e));
}
$('#action-form').addEventListener('submit',async e=>{e.preventDefault();const form=e.currentTarget;const body=Object.fromEntries(new FormData(form));let kind=action.kind;if(['consume','adjust'].includes(kind))body.quantity=Number(body.quantity);if(kind==='maintenance')body.cost=Number(body.cost);if(kind==='status'){if(body.sold_at)body.sold_at=new Date(body.sold_at+'+08:00').toISOString();else delete body.sold_at;if(body.sold_price!=='')body.sold_price=Number(body.sold_price);else delete body.sold_price;}if(kind!=='status'&&kind!=='alias')body.source='manual';if(kind==='alias')delete body.note;
 const path=`/items/${action.id}/${kind==='alias'?'aliases':kind==='status'?'durable':kind}`;const button=form.querySelector('[type=submit]');button.disabled=true;try{actionRetry=retryFor(actionRetry,body,path);await api(path,body,actionRetry.key,kind==='status'?'PATCH':'POST');$('#action-dialog').close();notify('已保存，历史已同步更新。');await load();}catch(err){$('#action-error').textContent=WubuZh.error(err);}finally{button.disabled=false;}});
async function details(id){try{const item=await api('/items/'+id);itemsByID.set(id,item);const [events,prices,maintenance,aliases]=await Promise.all(['/inventory-events','/prices','/maintenance','/aliases'].map(path=>api(`/items/${id}${path}?limit=100`)));$('#detail-title').textContent=item.name;let html=panel('当前物品',`<p>${esc(typeLabel(item.item_type))} · ${esc(item.quantity)} ${esc(WubuZh.unit(item.unit))} · ${esc(WubuZh.location(item.location)||'未设位置')}</p><p>开封时间：${esc(when(item.opened_at))}</p><p>${esc(item.notes)}</p>`);if(item.item_type==='DURABLE'){const d=await api(`/items/${id}/durable`);html+=panel('耐用品生命周期',`<p>状态：${esc(typeLabel(d.status))} · 购买：${esc(when(d.purchase_date))}</p><p>${esc(d.brand)} ${esc(d.model)} · 序列号：${esc(d.serial_number||'—')}</p><p>保修到期：${esc(d.warranty_until||'—')} · 售出：${esc(when(d.sold_at))} ${d.sold_price===null?'':money(d.sold_price)}</p>`);}html+=panel('名称别名',aliases.items.length?aliases.items.map(a=>esc(a.alias)).join(' · '):'<p>尚无别名。</p>')+panel('购买价格',table(['购买时间','商家','单价','数量','小计'],prices.items.map(p=>[esc(when(p.purchased_at)),esc(p.merchant),money(p.unit_price),esc(p.quantity),money(p.subtotal)])))+panel('库存事件',eventTable(events.items))+panel('维护历史',maintenanceTable(maintenance.items));$('#detail-content').innerHTML=html;$('#detail-dialog').showModal();}catch(e){notify(WubuZh.error(e),true);}}
async function purchaseDetail(id){try{const p=await api('/purchases/'+id);$('#detail-title').textContent=p.merchant||'购买明细';$('#detail-content').innerHTML=panel('购买事件',`<p>${esc(when(p.purchased_at))} · ${esc(WubuZh.payment(p.payment_method))}</p><p>实际付款 ${money(p.total_amount)} · 商品合计 ${money(p.item_total)} · 优惠 ${money(p.discount)} · 附加费用 ${money(p.extra_fee)}</p><p>${esc(p.note)}</p>`)+table(['商品','数量','单价','小计','类型'],p.items.map(l=>[esc(l.name),`${esc(l.quantity)} ${esc(WubuZh.unit(l.unit))}`,money(l.unit_price),money(l.subtotal),esc(typeLabel(l.item_type))]));$('#detail-dialog').showModal();}catch(e){notify(WubuZh.error(e),true);}}
document.addEventListener('click',e=>{const close=e.target.closest('[data-close]');if(close)$('#'+close.dataset.close).close();const a=e.target.closest('[data-action]');if(a)showAction(a.dataset.id,a.dataset.action);const d=e.target.closest('[data-detail]');if(d)details(d.dataset.detail);const p=e.target.closest('[data-purchase]');if(p)purchaseDetail(p.dataset.purchase);});
$('.ledger-nav').addEventListener('click',e=>{const b=e.target.closest('[data-view]');if(!b)return;view=b.dataset.view;offset=0;document.querySelectorAll('[data-view]').forEach(el=>el.classList.toggle('is-active',el===b));load();});
$('#content').addEventListener('submit',e=>{if(e.target.id==='search-form'){e.preventDefault();search=new FormData(e.target).get('search');offset=0;load();}});
$('#month').addEventListener('change',()=>{if(!$('#month').value)$('#month').value=localInput().slice(0,7);offset=0;load();});$('#previous').addEventListener('click',()=>{offset=Math.max(0,offset-PAGE);load();});$('#next').addEventListener('click',()=>{offset+=PAGE;load();});
load();
