(() => {
 'use strict';
 const $=s=>document.querySelector(s),esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 const money=n=>new Intl.NumberFormat('zh-CN',{style:'currency',currency:'CNY'}).format(n||0);
 const when=(v,precision='SECOND')=>v?(precision==='DAY'?new Date(v).toLocaleDateString('zh-CN',{timeZone:'Asia/Shanghai'}):new Date(v).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false})):'时间待核对';
 const labels={CREATE_NEW:'新增',MATCHED_EXISTING:'匹配已有',DUPLICATE:'重复',CONFLICT:'待确认',SKIPPED:'跳过'};
 let syncID=localStorage.getItem('wubu-weekly-sync')||crypto.randomUUID(),accounts=[],templates=[],health=[],historyOffset=0,balanceKey='';
 localStorage.setItem('wubu-weekly-sync',syncID);
 function message(text,error=false){$('#sync-message').textContent=text;$('#sync-message').className=error?'ledger-warning':'ledger-success';}
 async function api(path,payload,key){const r=await fetch('/api/v1/'+path,{method:payload?'POST':'GET',headers:payload?{'Content-Type':'application/json',...(key?{'Idempotency-Key':key}:{})}:{},body:payload?JSON.stringify(payload):undefined});const b=await r.json();if(!r.ok){const e=new Error(b.error?.message||'操作失败');e.status=r.status;throw e;}return b;}
 const accountOptions=(value='')=>'<option value="">待确认账户</option>'+accounts.filter(a=>a.active&&a.id!=='unknown').map(a=>`<option value="${esc(a.id)}" ${a.id===value?'selected':''}>${esc(a.name)}</option>`).join('');
 function table(headers,rows){return rows.length?`<div class="ledger-table-wrap"><table><thead><tr>${headers.map(h=>`<th>${esc(h)}</th>`).join('')}</tr></thead><tbody>${rows.map(r=>`<tr>${r.map(v=>`<td>${v}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`:'<p>暂无记录。</p>';}
 function metrics(b){return '<div class="sync-metrics"><span>新增 '+b.new_transactions+'</span><span>重复 '+b.duplicates+'</span><span>待确认 '+b.conflicts+'</span><span>匹配已有 '+b.matched_existing+'</span><span>跳过 '+b.skipped+'</span></div>';}
 const form=()=>$('#statement-form');
 let state, sequence=0, committing=false;
 function renderForms(){
  ++sequence;state={code:'',file:null,paymentAccounts:{},methods:[],body:null,preview:null,offset:0};
  $('#sync-forms').innerHTML='<section class="ledger-panel"><h2>上传原生账单</h2>'+
   '<form class="statement-form" id="statement-form"><label>账单文件<input name="file" type="file" accept=".csv,.xlsx,.xls"></label>'+
   '<label id="bank-template" hidden>银行账单模板<select name="provider_code"><option value="">请选择银行账单模板</option>'+
   templates.filter(t=>t.group==='bank').map(t=>'<option value="'+esc(t.id)+'">'+esc(t.name)+'</option>').join('')+'</select></label>'+
   '<label id="statement-account" hidden>未映射的付款方式使用<select name="account_id">'+accountOptions()+'</select></label></form>'+
   '<p id="statement-provider" aria-live="polite"></p><div class="statement-preview" id="statement-preview"></div></section>';
 }
 function renderPreview(state){
  const out=state.preview,b=out.batch;
  const methods=state.methods=[...new Set(out.rows.map(r=>r.payment_method).filter(Boolean))];
  const rows=out.rows.slice(state.offset,state.offset+50);
  const list=table(['时间 / 商家','付款方式 / 账户','金额','处理结果'],rows.map(r=>[
   esc(when(r.occurred_at,r.time_precision))+(r.time_precision==='DAY'?'（仅日期）':'')+'<br>'+esc(r.merchant||'商家未知'),
   esc(r.payment_method||'未提供')+'<br>'+esc(accounts.find(a=>a.id===r.account_id)?.name||'待确认'),
   r.amount==null?'待核对':esc(r.currency)+' '+esc(r.amount),esc(labels[r.status]||r.status)+'<br><small>'+esc(r.reason)+'</small>']));
  const account=form().elements.account_id.value;
  const unresolved=methods.some(method=>!(state.paymentAccounts[method]??account));
  $('#statement-preview').innerHTML=metrics(b)+(out.already_imported?'<p>这份账单已导入过。</p>':'')+
   (methods.length?'<details '+(unresolved||state.mappingOpen?'open':'')+' id="payment-mappings"><summary>付款方式与资金账户</summary>'+
    methods.map((method,i)=>'<label>'+esc(method)+'<select data-map-index="'+i+'">'+accountOptions(state.paymentAccounts[method]??account)+'</select></label>').join('')+'</details>':'')+
   '<details '+(state.rowsOpen?'open':'')+' id="statement-rows"><summary>全部流水（'+out.rows.length+'）</summary>'+list+
   (out.rows.length>50?'<div class="ledger-actions"><button class="button" data-row-page="-1">上一页</button><button class="button" data-row-page="1">下一页</button></div>':'')+
   '</details><div class="sync-source-actions"><button class="button button-primary" data-commit '+(state.body&&out.rows.length?'':'disabled')+'>确认导入</button></div>';
 }
 async function fileBase64(file){
  if(file.size>5*1024*1024)throw new Error('账单文件超过5MB。');
  const bytes=new Uint8Array(await file.arrayBuffer());let text='';
  for(let i=0;i<bytes.length;i+=32768)text+=String.fromCharCode(...bytes.subarray(i,i+32768));
  return btoa(text);
 }
 function invalidate(){
  state.body=null;
  const button=$('[data-commit]');if(button)button.disabled=true;
  return ++sequence;
 }
 async function preview(){
  const current=invalidate();
  if(!state.file||!state.code)return;
  const account_id=form().elements.account_id.value;
  const body={...state.file,account_id,payment_accounts:{...state.paymentAccounts},sync_id:syncID,preview_token:''};
  message('正在对账预览…');
  try{
   const out=await api('statements/'+state.code+'/preview',body);
   if(current!==sequence)return;
   state.body={...body,preview_token:out.preview_token};state.key=crypto.randomUUID();
   state.preview=out;state.offset=0;renderPreview(state);message('预览完成，可以确认导入。');
  }catch(err){if(current===sequence)message(err.message,true);}
 }
 async function upload(file){
  const current=invalidate();
  state.file=null;state.code='';state.paymentAccounts={};state.mappingOpen=false;state.rowsOpen=false;state.preview=null;
  $('#statement-preview').replaceChildren();$('#statement-provider').textContent='';
  $('#bank-template').hidden=true;$('#statement-account').hidden=true;form().elements.provider_code.value='';
  form().elements.account_id.value='';
  if(!file)return;
  message('正在识别原生账单…');
  try{
   const data={filename:file.name,file_base64:await fileBase64(file)};
   if(current!==sequence)return;
   const result=await api('statements/detect',data);
   if(current!==sequence)return;
   if(result.provider_code){
    const mapping=await api('statements/'+result.provider_code+'/payment-accounts');
    if(current!==sequence)return;
    state.paymentAccounts=Object.assign(Object.create(null),mapping.payment_accounts);
    state.code=result.provider_code;
   }
   state.file=data;
   if(!state.code){
    $('#bank-template').hidden=false;
    message('未识别为微信或支付宝账单，请选择对应的银行模板。');
    return;
   }
   showProvider();await preview();
  }catch(err){if(current===sequence)message(err.message,true);}
 }
 function showProvider(){
  $('#statement-account').hidden=false;
  $('#statement-account').firstChild.textContent=state.code==='wechat'||state.code==='alipay'?'未映射的付款方式使用':'银行账单对应账户';
  $('#statement-provider').textContent=templates.find(t=>t.id===state.code)?.name||'';
 }
 $('#sync-forms').onsubmit=e=>e.preventDefault();
 $('#sync-forms').onchange=e=>{
  if(committing)return;
  if(e.target.name==='file'){upload(e.target.files[0]);return;}
  if(e.target.name==='provider_code'){
   state.code=e.target.value;state.paymentAccounts=Object.create(null);
   form().elements.account_id.value='';
   $('#statement-preview').replaceChildren();state.preview=null;
   if(state.code)showProvider();else $('#statement-account').hidden=true;
  }
  if(e.target.matches('[data-map-index]')){
   state.paymentAccounts[state.methods[Number(e.target.dataset.mapIndex)]]=e.target.value;
   state.mappingOpen=true;
  }
  state.rowsOpen=Boolean($('#statement-rows')?.open);
  preview();
 };
 function lockForm(value){
  committing=value;$('#sync-new').disabled=value;
  $('#sync-forms').querySelectorAll('select,input,button').forEach(control=>{control.disabled=value;});
 }
 $('#sync-forms').onclick=async e=>{
  const page=e.target.closest('[data-row-page]');
  if(page&&state.preview){
   state.rowsOpen=true;state.mappingOpen=Boolean($('#payment-mappings')?.open);
   state.offset=Math.max(0,Math.min(Math.floor(Math.max(0,state.preview.rows.length-1)/50)*50,state.offset+Number(page.dataset.rowPage)*50));renderPreview(state);return;
  }
  if(!e.target.closest('[data-commit]')||!state.body||committing)return;
  lockForm(true);
  try{
   const out=await api('statements/'+state.code+'/commit',state.body,state.key);
   state.preview=out;state.body=null;
   $('#statement-preview').innerHTML='<p class="sync-ok">已保存原始账单和处理结果。</p>'+metrics(out.batch)+
    '<p><a href="/api/v1/statements/batches/'+encodeURIComponent(out.batch.id)+'/file">下载原始账单</a> · <a href="/financial-inbox">处理待确认</a></p>';
   message('本次账单已同步。');await refreshSummary();await loadHistory();
  }catch(err){
   if(err.status===409){lockForm(false);await preview();return;}
   message(err.status?err.message:'提交结果待确认，可以使用同一请求重试。',true);
  }finally{lockForm(false);}
 };
 async function refreshSummary(){const out=await api('statements/summary?sync_id='+encodeURIComponent(syncID));health=out.account_health;const groups=new Map();for(const b of out.batches){if(!groups.has(b.provider_code))groups.set(b.provider_code,[]);groups.get(b.provider_code).push(b);}$('#sync-summary').innerHTML=`<p>当前待确认 ${out.pending_review} 条，未补全 ${out.totals.unannotated} 条。下方为导入时统计。</p>`+metrics(out.totals)+[...groups].map(([code,batches])=>{const sum={row_count:0,new_transactions:0,matched_existing:0,duplicates:0,conflicts:0,skipped:0,unannotated:0};for(const b of batches)for(const key of Object.keys(sum))sum[key]+=b[key];return `<h3>${esc(templates.find(t=>t.id===code)?.name||code)}</h3>${metrics(sum)}`;}).join('')+(out.batches.length?'':'<p>本轮尚未提交账单。</p>');$('#sync-health').innerHTML=table(['账户','账面余额','对账状态','操作'],health.map(h=>[esc(h.name),money(h.current_balance),h.status==='NEVER_RECONCILED'?'尚未核对':h.unexplained_amount===0?`<span class="sync-ok">✓ 已对账</span><br>${esc(when(h.observed_at))}`:`<span class="sync-warning">未解释差额 ${money(h.unexplained_amount)}</span>`,`<button class="button" data-balance="${esc(h.account_id)}">核对余额</button>`]));}
 async function loadHistory(){const out=await api(`statements/batches?limit=20&offset=${historyOffset}`);$('#sync-history').innerHTML=table(['时间 / 来源','原始账单','处理结果'],out.items.map(b=>[`${esc(when(b.imported_at))}<br>${esc(templates.find(t=>t.id===b.provider_code)?.name||b.provider_code)}`,`<a href="/api/v1/statements/batches/${encodeURIComponent(b.id)}/file">${esc(b.filename)}</a>`,`新增 ${b.new_transactions} · 匹配 ${b.matched_existing} · 重复 ${b.duplicates} · 导入时待确认 ${b.conflicts}`]));$('#history-newer').disabled=historyOffset===0;$('#history-older').disabled=out.items.length<20;}
 $('#history-newer').onclick=()=>{historyOffset=Math.max(0,historyOffset-20);loadHistory().catch(e=>message(e.message,true));};$('#history-older').onclick=()=>{historyOffset+=20;loadHistory().catch(e=>message(e.message,true));};
 $('#sync-new').onclick=()=>{syncID=crypto.randomUUID();localStorage.setItem('wubu-weekly-sync',syncID);renderForms();refreshSummary().then(()=>message('已开始新一轮同步。')).catch(e=>message(e.message,true));};
 $('#sync-health').onclick=e=>{const b=e.target.closest('[data-balance]');if(!b)return;const h=health.find(h=>h.account_id===b.dataset.balance),form=$('#sync-balance-form');form.elements.account_id.value=h.account_id;form.elements.expected.value=h.current_balance;form.elements.actual_balance.value=h.current_balance;$('#sync-balance-title').textContent='核对余额 · '+h.name;$('#sync-balance-error').textContent='';balanceKey=crypto.randomUUID();$('#sync-balance-dialog').showModal();};
 $('#sync-balance-form').onsubmit=async e=>{e.preventDefault();const button=e.submitter,form=e.target;button.disabled=true;try{await api('money/reconcile',{account_id:form.elements.account_id.value,actual_balance:Number(form.elements.actual_balance.value),reason:form.elements.reason.value,source:'manual'},balanceKey);$('#sync-balance-dialog').close();await refreshSummary();message('真实余额已确认，未解释差额已保留。');}catch(err){$('#sync-balance-error').textContent=err.message;}finally{button.disabled=false;}};
 $('#sync-balance-cancel').onclick=()=>$('#sync-balance-dialog').close();
 Promise.all([api('accounts'),api('statements/templates')]).then(async([a,t])=>{accounts=a.items;templates=t.items;renderForms();await refreshSummary();await loadHistory();}).catch(e=>message(e.message,true));
})();
