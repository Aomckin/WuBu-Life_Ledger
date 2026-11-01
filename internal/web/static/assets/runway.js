'use strict';
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
