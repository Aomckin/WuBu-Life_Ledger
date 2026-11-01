"""Read-only SDK import; all events use the runner's disposable database."""
import asyncio
import os
from pathlib import Path
from datetime import datetime, timedelta, timezone
from wubu_tool.package import create_package

if not Path(os.environ['DATABASE_PATH']).parent.name.startswith('wubu-dogfooding-'):
    raise RuntimeError('Isolated temporary validation database required')
tools = {t.name: t for t in create_package().create_tools({'base_url': os.environ['WUBU_TEST_BASE_URL']})}
async def call(operation, **kwargs):
    result = await tools['wubu'].run({'operation': operation, **kwargs})
    assert result.success, (operation, result)
    return result.data
async def main():
    account = await call('create_account', payload={'name':'v03 银行','type':'BANK','initial_balance':8420},idempotency_key='v03-bank')
    now=datetime.now(timezone(timedelta(hours=8)))
    before=await call('get_living_runway')
    plan=await call('create_planned_cashflow',payload={'name':'v03 房租','direction':'OUTFLOW','amount':2980,'due_at':(now+timedelta(days=3)).isoformat(),'account_id':account['id'],'mandatory':True,'expense_nature':'FIXED','recurrence':'MONTHLY'},idempotency_key='v03-plan')
    planned=await call('get_living_runway')
    assert planned['liquid_balance']==before['liquid_balance']
    done=await call('complete_planned_cashflow',cashflow_id=plan['id'],payload={},idempotency_key='v03-pay')
    replay=await call('complete_planned_cashflow',cashflow_id=plan['id'],payload={},idempotency_key='v03-pay')
    assert done['linked_transaction_id']==replay['linked_transaction_id']
    r=await call('create_residence',payload={'name':'v03 深圳','move_in_date':now.date().isoformat(),'status':'ACTIVE','monthly_rent':2980},idempotency_key='v03-home')
    await call('link_residence_cost',residence_id=r['id'],payload={'transaction_id':done['linked_transaction_id'],'role':'RENT'},idempotency_key='v03-link')
    d=await call('record_deposit',payload={'amount':600,'account_id':account['id'],'residence_id':r['id']},idempotency_key='v03-deposit')
    assert (await call('get_residence_cost',residence_id=r['id']))['deposit_locked']==600
    await call('return_deposit',claim_id=d['id'],payload={'amount':600},idempotency_key='v03-return')
    c=await call('create_receivable',payload={'amount':300,'account_id':account['id'],'counterparty':'公司'},idempotency_key='v03-advance')
    settled=await call('settle_receivable',claim_id=c['id'],payload={'amount':300},idempotency_key='v03-settle')
    assert settled['status']=='SETTLED'
    purchase=await tools['record_purchase'].run({'items':[{'name':'v03 显示器','item_type':'DURABLE','quantity':1,'unit':'个','price':100}],'expense_nature':'SETUP','account_id':account['id'],'idempotency_key':'v03-monitor'})
    assert purchase.success,purchase
    rows=await call('get_durable_items',query={'search':'v03 显示器'})
    item=rows['items'][0]['details']['item_id']
    await call('update_durable_details',item_id=item,payload={'residence_id':r['id'],'has_original_packaging':True,'purchase_proof':'local-attachment-reference','estimated_resale_value':80},idempotency_key='v03-details')
    details=await call('get_durable_details',item_id=item)
    assert details['has_original_packaging'] and details['estimated_resale_value']==80
    assert len((await call('get_residence_durables',residence_id=r['id']))['items'])==1
    await call('get_items_running_low')
    print('SDK v0.3 PASS: plan isolation, payment replay, residence cost, deposit, reimbursement, durable metadata.')
asyncio.run(main())
