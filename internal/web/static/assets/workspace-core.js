'use strict';
(() => {
const $ = selector => document.querySelector(selector);
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const money = value => new Intl.NumberFormat('zh-CN',{style:'currency',currency:'CNY'}).format(value || 0);
const when = value => value ? new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',hour12:false}) : '—';
const localInput = () => new Date(Date.now()+8*3600000).toISOString().slice(0,16);
async function api(path,body,key,method='POST'){
 const options=body === undefined ? {} : {method,headers:{'Content-Type':'application/json',...(key?{'Idempotency-Key':key}:{})},body:JSON.stringify(body)};
 const response=await fetch('/api/v1'+path,options);
 const data=await response.json();
 if(!response.ok) throw new Error(data.error?.code==='acquisition_link_incomplete'?data.error.message:WubuZh.error(data.error, response.status));
 return data;
}
function retryFor(previous,body,path){const fingerprint=JSON.stringify({body,path});return previous?.fingerprint===fingerprint?previous:{fingerprint,key:crypto.randomUUID()};}
function notify(text,error=false){$('#message').className=error?'ledger-warning':'ledger-success';$('#message').textContent=text;}
function panel(title,content){return `<section class="ledger-panel"><h2>${esc(title)}</h2>${content}</section>`;}
function table(headers,rows){
 if(!rows.length)return '<p class="ledger-empty">这里还没有记录。</p>';
 const headings=headers.map(h=>`<th scope="col" role="columnheader">${esc(h)}</th>`).join('');
 const body=rows.map(row=>`<tr role="row">${row.map((value,i)=>`<td role="cell" data-column="${esc(headers[i])}">${value}</td>`).join('')}</tr>`).join('');
 return `<div class="ledger-table-wrap"><table class="ledger-table" role="table"><thead role="rowgroup"><tr role="row">${headings}</tr></thead><tbody role="rowgroup">${body}</tbody></table></div>`;
}
function dateRange(){const [year,month]=$('#month').value.split('-').map(Number);const start=new Date(Date.UTC(year,month-1,1)-8*3600000),end=new Date(Date.UTC(year,month,1)-8*3600000);return `from=${encodeURIComponent(start.toISOString())}&to=${encodeURIComponent(end.toISOString())}`;}
function inputField(label,name,type='text',value='',extra=''){return `<label>${esc(label)}<input name="${name}" type="${type}" value="${esc(value)}" ${extra}></label>`;}
window.WubuWorkspace=Object.freeze({$,esc,money,when,localInput,api,retryFor,notify,panel,table,dateRange,inputField});
})();
