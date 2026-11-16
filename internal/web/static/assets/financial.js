(() => {
 'use strict';
 const $=s=>document.querySelector(s), content=$('#financial-content'), form=$('#review-form');
 const query=new URLSearchParams(location.search);
 let selectedID=query.get('transaction');
 const tabs=['review','booked','unannotated','semantic-evidence','processed','health'];
 const labels={CAPTURED:'待匹配',NEEDS_REVIEW:'需确认',COMMITTED:'已入账',IGNORED:'已忽略',FAILED:'需确认'};
 const directions={EXPENSE:'支出',INCOME:'收入',REFUND:'退款',TRANSFER_IN:'转入',TRANSFER_OUT:'转出'};
 const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 const when=(v,precision='SECOND')=>v?(precision==='DAY'?new Date(v).toLocaleDateString('zh-CN',{timeZone:'Asia/Shanghai'}):new Date(v).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false})):'时间待核对';
 const mask=v=>String(v??'').replace(/\d{7,}/g,s=>s.slice(0,2)+'••••'+s.slice(-4));
 let offset=0,tab=tabs.includes(query.get('tab'))?query.get('tab'):selectedID?'booked':'review', accounts=[],events=[],booked=[],pending,request=0,writeKeys;
 async function api(path,payload,method='POST'){
  const body=payload?JSON.stringify(payload):null;
  if(body&&!writeKeys.has(body))writeKeys.set(body,crypto.randomUUID());
  const r=await fetch('/api/v1/'+path,{method:body?method:'GET',headers:body?{'Content-Type':'application/json','Idempotency-Key':writeKeys.get(body)}:{},body});
  const b=await r.json();
  if(!r.ok)throw new Error(b.error?.message||b.message||'操作失败');
  return b;
 }
 const accountName=id=>accounts.find(a=>a.id===id)?.name||'账户待确认';
 const accountOptions=(selected='')=>'<option value="">请选择资金账户</option>'+accounts.filter(a=>a.id==='unknown'?false:a.active||a.id===selected).map(a=>option(a.id,a.name,selected)).join('');
 const option=(value,label,selected='')=>'<option value="'+esc(value)+'" '+(value===selected?'selected':'')+'>'+esc(label)+'</option>';
 const directionOptions=(selected='',transfer=false)=>option('','请选择方向',selected)+Object.entries(directions).filter(([key])=>transfer||!key.startsWith('TRANSFER_')).map(([key,label])=>option(key,label,selected)).join('');
 const candidateLabel=t=>[when(t.occurred_at),mask(t.merchant)||t.note||t.annotation?.item_description||'未填写说明','¥'+t.amount,accountName(t.account_id)].join(' · ');
 const candidateOptions=(items,selected='')=>option('','请选择流水')+items.map(t=>option(t.id,candidateLabel(t),selected)).join('');
 const input=(label,name,value='')=>'<label>'+esc(label)+'<input name="'+name+'" value="'+esc(value)+'"></label>';
 const extraNote=()=>'<details><summary>添加审核备注</summary><label>备注<textarea name="note" maxlength="2000"></textarea></label></details>';
 const txnPath=t=>'financial/transactions/'+encodeURIComponent(t.id);
 function open(title,fields,submit,button='确认'){
  $('#financial-title').textContent=title;$('#financial-fields').innerHTML=fields;
  $('#financial-error').textContent='';form.querySelector('[type=submit]').textContent=button;
  pending=submit;writeKeys=new Map();$('#financial-dialog').showModal();
 }
 function amountField(value=''){return '<label>金额（元）<input name="amount" type="number" value="'+esc(value)+'" min="0.01" max="10000000000" step="0.01" required></label>';}
 function eventName(r){return mask(r.merchant_raw||r.description_raw||r.raw_text?.split('\n')[0]?.slice(0,60))||directions[r.direction]||'财务记录';}
 function reviewReason(r){
  const labels={CONFLICTING_EXTERNAL_ID:'同一笔原始交易的内容有出入。',ACCOUNT_REQUIRES_REVIEW:'原付款账户无法使用，请选实际账户。'};
  return r.review_reason||labels[r.decision]||'';
 }
 function renderEvents(){
  const processed=['COMMITTED','IGNORED'];
  content.innerHTML=events.filter(r=>processed.includes(r.status)===(tab==='processed')).map(r=>
   '<article class="financial-card"><strong>'+esc(eventName(r))+' · '+(r.amount==null?'金额待确认':esc((r.currency&&r.currency!=='CNY'?r.currency+' ':'¥')+r.amount))+'</strong>'+
   '<small>'+esc(when(r.occurred_at||r.captured_at,r.time_precision))+' · '+esc(labels[r.status]||'需确认')+'</small>'+
   (r.raw_text||r.import_batch_id?'<details><summary>查看原始证据</summary><pre>'+esc(mask(r.raw_text))+'</pre>'+(r.import_batch_id?'<a href="/api/v1/statements/batches/'+encodeURIComponent(r.import_batch_id)+'/file">下载原始账单</a> · 第 '+esc(r.row_number)+' 行':'')+'</details>':'')+
   (tab==='processed'?(r.transaction_id?'<a href="?transaction='+encodeURIComponent(r.transaction_id)+'">查看入账流水</a>':''):'<button class="button" data-review="'+esc(r.id)+'">'+(r.candidates?.length?'核对候选':r.direction==='REFUND'?'确认退款':!r.account_id?'选择账户':'核对交易')+'</button>')+'</article>').join('')||'<p>此分组暂无记录。</p>';
 }
 function renderBooked(){
  content.innerHTML=booked.map(t=>'<article class="financial-card"><strong>'+esc(mask(t.merchant)||t.note||'未填写说明')+' · ¥'+esc(t.amount)+'</strong><small>'+
   esc(when(t.occurred_at))+' · '+esc(accountName(t.account_id))+' · '+esc(directions[t.type])+
   (t.annotation.annotation_status==='REVIEW_NEEDED'?' · 说明待核对':'')+'</small><p>'+
   esc(t.annotation.item_description||t.note||t.category)+'</p><div class="ledger-actions"><button class="button" data-details="'+esc(t.id)+'">编辑说明</button><button class="button" data-funding="'+esc(t.id)+'">校正资金</button></div></article>').join('')||'<p>暂无流水。</p>';
 }
 async function load(){
  const version=++request;
  $('#financial-message').textContent='';
  $('#financial-page').textContent='第 '+(offset/50+1)+' 页';
  $('#financial-newer').disabled=!offset;
  const paginated=['review','processed','booked','unannotated'].includes(tab);
  $('#financial-pages').hidden=!paginated;
  $('#financial-booked-search').hidden=tab!=='booked';
  document.querySelectorAll('[data-tab]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.tab===tab)));
  try{
   if(tab==='health'){
    const cp=await api('financial/reconciliation');
    if(version!==request)return;
    const latest=cp.items.filter((c,i,all)=>all.findIndex(x=>x.account_id===c.account_id)===i);
    content.innerHTML='<h2>最新余额检查点</h2>'+(latest.map(c=>'<p>'+esc(accountName(c.account_id))+'：实际 ¥'+esc(c.observed_balance)+'，未解释差额 ¥'+esc(c.unexplained_amount)+'</p>').join('')||'<p>尚无余额检查点。</p>');return;
   }
   if(tab==='semantic-evidence'){
    const b=await api('financial/semantic-evidence');
    if(version!==request)return;
    content.innerHTML=b.items.filter(e=>e.status==='UNMATCHED').map(e=>'<article class="financial-card"><small>'+esc(when(e.occurred_at))+'</small><p>'+esc(mask(e.text))+'</p></article>').join('')||'<p>没有未匹配证据。</p>';return;
   }
   const params=new URLSearchParams({limit:'50',offset:String(offset)});
   if(tab==='unannotated'){
    const [missing,review]=await Promise.all([
     api('financial/transactions?'+params+'&unannotated_only=true'),
     api('financial/transactions?'+params+'&annotation_status=REVIEW_NEEDED')
    ]);
    if(version!==request)return;
    booked=[...review.items,...missing.items];renderBooked();
    $('#financial-older').disabled=review.items.length<50&&missing.items.length<50;return;
   }
   if(tab==='booked'){
    if(!offset&&selectedID&&!$('#financial-search').value)params.set('transaction_id',selectedID);
    if($('#financial-search').value)params.set('search',$('#financial-search').value);
    const b=await api('financial/transactions?'+params);
    if(version!==request)return;
    booked=b.items;renderBooked();$('#financial-older').disabled=b.items.length<50;return;
   }
   params.set('status',tab==='processed'?'PROCESSED':'PENDING');
   const b=await api('financial/events?'+params);
   if(version!==request)return;
   events=b.items;renderEvents();$('#financial-older').disabled=b.items.length<50;
  }catch(e){if(version===request)$('#financial-message').textContent=e.message;}
 }
 async function refundOptions(target,at,amount,selected='',editing=''){
  const p=new URLSearchParams({type:'EXPENSE',limit:'100',refund_amount:String(amount||0.01)});
  if(at)p.set('refund_at',at);
  if(editing)p.set('editing_refund',editing);
  if(target.dataset.search)p.set('search',target.dataset.search);
  const current=target.dataset.request=String(Number(target.dataset.request||0)+1);
  target.disabled=true;
  try{
   const b=await api('financial/transactions?'+p);
   if(current!==target.dataset.request)return;
   if(selected&&!b.items.some(t=>t.id===selected)&&!target.dataset.search){
    const original=await api('financial/transactions?transaction_id='+encodeURIComponent(selected));
    if(current!==target.dataset.request)return;
    if(original.items.length)b.items.unshift(original.items[0]);
   }
   target.innerHTML=b.items.length?candidateOptions(b.items,selected):option('','没有可退款的支出');
  }finally{if(current===target.dataset.request)target.disabled=false;}
 }
 function attachRefundSearch(at,amount,selected='',editing=''){
  const select=form.elements.related_transaction_id,search=form.elements.refund_search;
  const refresh=()=>{select.dataset.search=search.value.trim();return refundOptions(select,at(),amount(),select.value||selected,editing).catch(e=>$('#financial-error').textContent=e.message);};
  let timer;
  search.oninput=()=>{clearTimeout(timer);timer=setTimeout(refresh,200);};
  return refresh;
 }
 const refundFields=()=>'<label>查找原支出<input name="refund_search" placeholder="商家或说明"></label><label>退款对应的原支出<select name="related_transaction_id" required></select></label>';
 async function openReview(r){
  const initial=r.candidates?.length?'MATCH':'CREATE_NEW';
  const knownAccount=accounts.some(a=>a.id===r.account_id&&a.id!=='unknown'&&a.active);
  const duplicate=initial==='MATCH';
  const candidates=duplicate?(await api('financial/events/'+encodeURIComponent(r.id)+'/candidates')).items:[];
  const reason=reviewReason(r);
  const summary='<p>'+esc(eventName(r))+(r.amount==null?'':' · '+esc((r.currency&&r.currency!=='CNY'?r.currency+' ':'¥')+r.amount))+' · '+esc(when(r.occurred_at,r.time_precision))+'</p>'+
   (reason?'<p>'+esc(mask(reason))+'</p>':'');
  const menu=duplicate?'<label>处理方式<select name="action">'+option('MATCH','选择对应流水',initial)+option('CREATE_NEW','确认是独立交易')+option('IGNORE','忽略该记录')+'</select></label>':'<button type="button" class="button" id="review-ignore">忽略该记录</button>';
  let action=initial;
  open(duplicate?'核对可能重复的记录':r.direction==='REFUND'?'确认退款':knownAccount?'核对交易':'确认资金账户',summary+menu+'<div id="review-action-fields"></div>'+extraNote(),f=>{
   const p={action};
   if(f.get('note'))p.note=f.get('note');
   for(const name of ['transaction_id','account_id','direction','related_transaction_id'])if(f.get(name))p[name]=f.get(name);
   if(f.get('amount'))p.amount=Number(f.get('amount'));
   if(f.get('occurred_at'))p.occurred_at=localTime(f.get('occurred_at'));
   return api('financial/events/'+encodeURIComponent(r.id)+'/review',p);
  });
  const showAction=()=>{
   const target=$('#review-action-fields');
   if(action==='IGNORE'){target.innerHTML='<p>确认后此记录会移出待处理列表，原始证据继续保留。</p>';return;}
   if(action==='MATCH'){
    target.innerHTML='<label>对应流水<select name="transaction_id" required>'+candidateOptions(candidates)+'</select></label>';
    return;
   }
   target.innerHTML=(!knownAccount?'<label>'+(r.direction==='EXPENSE'?'这笔钱从哪个账户扣除？':'这笔钱属于哪个账户？')+'<select name="account_id" required>'+accountOptions()+'</select></label>':'')+
    (r.amount==null||(r.currency&&r.currency!=='CNY')?amountField():'')+
    (!r.occurred_at?'<label>发生时间（北京时间）<input name="occurred_at" type="datetime-local" required></label>':'')+
    (!r.direction?'<label>交易方向<select name="direction" required>'+directionOptions()+'</select></label>':'')+
    '<div id="review-original"></div>';
   const updateRefund=()=>{
    const refund=(form.elements.direction?.value||r.direction)==='REFUND'&&!r.related_transaction_id;
    const original=$('#review-original');
    if(!refund){original.replaceChildren();return;}
    if(original.firstElementChild)return;
    original.innerHTML=refundFields();
    const refresh=attachRefundSearch(()=>r.occurred_at||localTime(form.elements.occurred_at?.value),()=>form.elements.amount?.value||r.amount);
    if(form.elements.amount)form.elements.amount.onchange=refresh;
    if(form.elements.occurred_at)form.elements.occurred_at.onchange=refresh;
    refresh();
   };
   if(form.elements.direction)form.elements.direction.onchange=updateRefund;
   updateRefund();
  };
  if(form.elements.action)form.elements.action.onchange=e=>{action=e.target.value;showAction();};
  if($('#review-ignore'))$('#review-ignore').onclick=e=>{action=action==='IGNORE'?initial:'IGNORE';e.target.textContent=action==='IGNORE'?'返回核对':'忽略该记录';showAction();};
  showAction();
 }
 function localTime(v){return v?v+(v.length===16?':00':'')+'+08:00':'';}
 function openDetails(t){
  const before={category:t.annotation.category??t.category,item_description:t.annotation.item_description||t.annotation.item_name||'',merchant:t.merchant,note:t.note||t.annotation.note||'',tags:(t.annotation.tags||[]).join('，')};
  open('编辑流水说明',input('分类','category',before.category)+input('商品或消费说明','item_description',before.item_description)+input('商家展示名称','merchant',before.merchant)+input('备注','note',before.note)+input('标签（逗号分隔）','tags',before.tags),f=>{
   const changed=Object.fromEntries([...f].filter(([key,value])=>value!==before[key]));
   if('tags' in changed)changed.tags=changed.tags.split(/[,，]/).map(s=>s.trim()).filter(Boolean);
   if(Object.keys(changed).length)return api(txnPath(t)+'/details',changed,'PATCH');
  },'保存说明');
 }
 function openFunding(t){
  const before={account_id:t.account_id,amount:t.amount,direction:t.type,related_transaction_id:t.related_transaction_id||''};
  open('校正资金', '<p>校正会调整相关账户余额，并记录此次修改。已确认的余额检查点继续保留。</p><label>资金账户<select name="account_id" required>'+accountOptions(t.account_id)+'</select></label>'+amountField(t.amount)+'<label>交易方向<select name="direction" required>'+directionOptions(t.type)+'</select></label><div id="review-original"></div>'+extraNote(),f=>{
   const p={expected:before};
   for(const [key,value] of f){
    if(key==='refund_search')continue;
    const next=key==='amount'?Number(value):value;
    if(next!==before[key]&&next!=='')p[key]=next;
   }
   if(Object.keys(p).some(key=>key!=='expected'&&key!=='note'))return api(txnPath(t)+'/correction',p);
  },'确认资金校正');
  const update=()=>{
   const original=$('#review-original');
   if(form.elements.direction.value!=='REFUND'){original.replaceChildren();form.elements.amount.onchange=null;return;}
   if(original.firstElementChild)return;
   original.innerHTML=refundFields();
   const refresh=attachRefundSearch(()=>t.occurred_at,()=>form.elements.amount.value,before.related_transaction_id,t.id);
   form.elements.amount.onchange=refresh;refresh();
  };
  form.elements.direction.onchange=update;update();
 }
 content.onclick=async e=>{
  const button=e.target.closest('button');if(!button)return;
  try{
   if(button.dataset.review){button.disabled=true;await openReview(events.find(r=>r.id===button.dataset.review));}
   if(button.dataset.details)openDetails(booked.find(t=>t.id===button.dataset.details));
   if(button.dataset.funding)openFunding(booked.find(t=>t.id===button.dataset.funding));
  }catch(err){$('#financial-message').textContent=err.message;}finally{button.disabled=false;}
 };
 form.onsubmit=async e=>{
  e.preventDefault();
  if(form.elements.related_transaction_id?.disabled)return;
  const button=e.submitter;button.disabled=true;
  try{await pending(new FormData(form));$('#financial-dialog').close();await load();$('#financial-message').textContent='已保存。';}
  catch(err){$('#financial-error').textContent=err.message;}finally{button.disabled=false;}
 };
 $('#financial-cancel').onclick=()=>$('#financial-dialog').close();
 $('#financial-newer').onclick=()=>{offset=Math.max(0,offset-50);load();};
 $('#financial-older').onclick=()=>{offset+=50;load();};
 $('#financial-booked-search').onsubmit=e=>{e.preventDefault();offset=0;load();};
 document.querySelectorAll('[data-tab]').forEach(b=>b.onclick=()=>{
  tab=b.dataset.tab;offset=0;
  if(selectedID){selectedID=null;history.replaceState({},'',location.pathname);}
  load();
 });
 api('accounts').then(b=>{accounts=b.items||[];return load();}).catch(e=>$('#financial-message').textContent=e.message);
})();
