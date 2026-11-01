'use strict';
let livingResidences=new Map(),livingClaims=new Map(),livingCommand=null,livingRetry=null;
async function livingPanel(){
 const [residences,deposits,receivables,low]=await Promise.all([api('/residences'),api('/deposits'),api('/receivables'),api('/items-running-low')]);
 livingResidences=new Map(residences.items.map(r=>[r.id,r]));livingClaims=new Map([...deposits.items,...receivables.items].map(c=>[c.id,c]));
 const claimsTable=items=>table(['对象 / 事由','原额 / 剩余','状态','操作'],items.map(c=>[esc(c.kind==='DEPOSIT'?(livingResidences.get(c.residence_id)?.name||c.residence_id):c.counterparty)+'<br>'+esc(c.reason),`${money(c.amount)} / ${money(c.amount-c.returned_amount-c.forfeited_amount)}`,esc(({HELD:'持有',PARTIALLY_RETURNED:'部分退回',RETURNED:'已退回',FORFEITED:'含没收损失',OPEN:'待收',PARTIAL:'部分结清',SETTLED:'已结清',CANCELLED:'已核销'})[c.status]),c.amount>c.returned_amount+c.forfeited_amount?`<button data-living="return" data-living-id="${esc(c.id)}">${c.kind==='DEPOSIT'?'退押金':'到账'}</button> <button data-living="forfeit" data-living-id="${esc(c.id)}">${c.kind==='DEPOSIT'?'没收损失':'核销损失'}</button>`:'—']));
 return `<div class="ledger-actions"><button data-living="residence">新增居住</button><button data-living="deposit">记录支付押金</button><button data-living="receivable">记录实际代垫</button></div>`+panel('居住',table(['居住','月租','状态','操作'],residences.items.map(r=>[esc(r.name),money(r.monthly_rent),esc(({ACTIVE:'当前',PAST:'过去',PLANNED:'计划'})[r.status]),`<button data-living="residence" data-living-id="${esc(r.id)}">编辑</button> <button data-residence-cost="${esc(r.id)}">成本与物品</button>`])))+panel('押金 · 不计普通消费',claimsTable(deposits.items))+panel('待收 / 报销 · 到账前不计可花',claimsTable(receivables.items))+panel('物资余量',table(['物品','当前数量','预计剩余'],low.items.map(f=>[itemButton(f.item_id),esc(f.current_quantity),f.status==='OK'?`约 ${Math.round(f.estimated_days_remaining)} 天`:'数据不足'])));
}
async function openLiving(action,id=''){
 await refreshAccounts();const residences=(await api('/residences')).items;livingResidences=new Map(residences.map(r=>[r.id,r]));
 livingCommand={action,id};livingRetry=null;let fields='',title='';
 const residenceSelect=(selected='')=>`<label>居住<select name="residence_id" required>${residences.map(r=>`<option value="${esc(r.id)}" ${r.id===selected?'selected':''}>${esc(r.name)}</option>`).join('')}</select></label>`;
 if(action==='durable'){
 const d=await api(`/items/${encodeURIComponent(id)}/durable`);title='耐用品资料';
 fields=inputField('序列号','serial_number','text',d.serial_number||'')+inputField('保修到期','warranty_until','date',d.warranty_until||'')+inputField('购买凭证（已有附件 ID 或本地引用）','purchase_proof','text',d.purchase_proof||'','maxlength="2000"')+inputField('手动参考转售价（元）','estimated_resale_value','number',d.estimated_resale_value??'','min="0" step="0.01"')+`<label>原包装<select name="has_original_packaging"><option value="">未记录</option><option value="true" ${d.has_original_packaging===true?'selected':''}>保留</option><option value="false" ${d.has_original_packaging===false?'selected':''}>未保留</option></select></label><label>所属居住<select name="residence_id"><option value="">无</option>${residences.map(r=>`<option value="${esc(r.id)}" ${d.residence_id===r.id?'selected':''}>${esc(r.name)}</option>`).join('')}</select></label>`;
 }else if(action==='residence'){
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
 const b=e.target.closest('[data-living],[data-residence-cost],[data-forecast-id],[data-residence-link]');if(!b)return;
 try{
 if(b.dataset.living){await openLiving(b.dataset.living,b.dataset.livingId||'');return;}
 if(b.dataset.residenceLink){await openLiving('link',b.dataset.residenceLink);return;}
 if(b.dataset.forecastId){const f=await api(`/items/${encodeURIComponent(b.dataset.forecastId)}/consumption-forecast`);$('#detail-title').textContent='消耗预测';$('#detail-content').innerHTML=f.status==='OK'?`<p>平均每日 ${esc(f.average_daily_consumption.toFixed(2))}，预计约 ${Math.round(f.estimated_days_remaining)} 天</p>`:'<p>数据不足：至少需要 3 次有效消耗，并观察至少 7 天。</p>';$('#detail-dialog').showModal();return;}
 const id=b.dataset.residenceCost,[c,d]=await Promise.all([api(`/residences/${encodeURIComponent(id)}/cost`),api(`/residences/${encodeURIComponent(id)}/durables`)]);
 $('#detail-title').textContent=livingResidences.get(id)?.name||'居住成本';$('#detail-content').innerHTML=`<p>住房实际消费 ${money(c.consumed_cost)} · 其中一次性安家 ${money(c.setup_cost)}</p><p>耐用品投入 ${money(c.capital_cost)} · 押金锁定 ${money(c.deposit_locked)} · 押金损失 ${money(c.deposit_loss)}</p><p>合计净现金流出 ${money(c.net_cash_outflow)}</p>`+table(['角色 / 性质','金额'],c.groups.map(g=>[esc(g.key),money(g.amount)]))+panel('关联耐用品',d.items.map(i=>`<p>${itemButton(i.item_id)} · ${esc(WubuZh.location(i.location))}</p>`).join('')||'暂无');$('#detail-dialog').showModal();
 }catch(err){notify(WubuZh.error(err),true);}
});
document.addEventListener('submit',async e=>{
 if(e.target.id!=='living-form')return;e.preventDefault();const button=e.target.querySelector('[type=submit]');
 try{
 let body=Object.fromEntries(new FormData(e.target)),path,method='POST';const {action,id}=livingCommand;
 if(action==='durable'){path=`/items/${encodeURIComponent(id)}/durable`;method='PATCH';if(body.estimated_resale_value==='')delete body.estimated_resale_value;else body.estimated_resale_value=Number(body.estimated_resale_value);if(body.has_original_packaging==='')delete body.has_original_packaging;else body.has_original_packaging=body.has_original_packaging==='true';}
 else if(action==='residence'){path='/residences'+(id?'/'+encodeURIComponent(id):'');method=id?'PUT':'POST';body.monthly_rent=Number(body.monthly_rent);body.network_included=body.network_included==='on';body.management_fee_included=body.management_fee_included==='on';}
 else if(action==='link'){path=`/residences/${encodeURIComponent(body.residence_id)}/cost-links`;body={transaction_id:id,role:body.role};}
 else{body.amount=Number(body.amount);body.occurred_at=new Date(body.occurred_at+'+08:00').toISOString();if(action==='deposit'||action==='receivable')path=action==='deposit'?'/deposits':'/receivables';else path=`/${livingClaims.get(id).kind==='DEPOSIT'?'deposits':'receivables'}/${encodeURIComponent(id)}/${action}`;}
 livingRetry=retryFor(livingRetry,body,path);button.disabled=true;await api(path,body,livingRetry.key,method);livingRetry=null;$('#living-dialog').close();await load();
 }catch(err){$('#living-error').textContent=WubuZh.error(err);}finally{button.disabled=false;}
});

async function supplyPanel(){const low=await api('/items-running-low');return panel('物资余量',table(['物品','剩余','消耗预测'],low.items.map(f=>[itemButton(f.item_id),esc(f.current_quantity),f.status==='OK'?`约 ${Math.round(f.estimated_days_remaining)} 天`:'数据不足'])));}
