"""Read-only Zhaoxi SDK -> temporary HTTP service Account Classes acceptance."""
import asyncio,os
from pathlib import Path
from wubu_tool.package import create_package
if not Path(os.environ['DATABASE_PATH']).parent.name.startswith('wubu-dogfooding-'):
    raise RuntimeError('Isolated temporary validation database required')
tools={tool.name:tool for tool in create_package().create_tools({'base_url':os.environ['WUBU_TEST_BASE_URL']})}
async def call(name,args):
    result=await tools[name].run(args)
    assert result.success,(name,result)
    return result
async def main():
    a=(await call('create_account',{'name':'SDK v021 微信','account_class':'STORAGE','type':'WALLET','initial_balance':500,'idempotency_key':'v021-a'})).data
    b=(await call('create_account',{'name':'SDK v021 茶颜','account_class':'CONSUMPTION','type':'MERCHANT_BALANCE','merchant':'茶颜悦色','idempotency_key':'v021-b'})).data
    before=(await call('get_expense_summary',{})).data
    topup={'from_account':a['name'],'to_account':b['name'],'paid_amount':100,'credited_amount':110,'idempotency_key':'v021-topup'}
    event=await call('top_up_account',topup);await call('top_up_account',topup)
    assert [t['type'] for t in event.data['transactions']]==['TRANSFER_OUT','TRANSFER_IN','BONUS']
    assert '消费 ¥0.00' in event.content,event.content
    after=(await call('get_expense_summary',{})).data;assert after['net_expense']==before['net_expense'] and after['income']==before['income']
    accounts=(await call('get_accounts',{'account_class':'CONSUMPTION','merchant':'茶颜悦色'})).data;assert any(x['id']==b['id'] for x in accounts['items'])
    await call('set_default_payment_account',{'default_payment_account':b['id'],'idempotency_key':'v021-default'})
    buy=(await call('record_purchase',{'items':[{'name':'SDK v021 奶茶','quantity':1,'unit':'杯','price':20,'item_type':'UNTRACKED'}],'idempotency_key':'v021-buy'})).data
    stats=(await call('get_account_statistics',{'account_id':b['name']})).data
    assert stats['current_balance']==90 and stats['expense']==20 and stats['top_up_principal']==100 and stats['bonus']==10,stats
    result=await tools['record_purchase'].run({'merchant':'麦当劳','items':[{'name':'饮品','quantity':1,'unit':'杯','price':20}],'idempotency_key':'v021-mismatch'})
    assert not result.success and result.data['error']['code']=='ACCOUNT_MERCHANT_MISMATCH',result
    await call('record_refund',{'purchase_event_id':buy['purchase_id'],'amount':10,'idempotency_key':'v021-refund'})
    await call('record_refund',{'purchase_event_id':buy['purchase_id'],'account_id':a['id'],'amount':10,'idempotency_key':'v021-refund-wallet'})
    stats=(await call('get_account_statistics',{'account_id':b['id']})).data;assert stats['net_expense']==0 and stats['current_balance']==100,stats
    await call('update_account',{'account_id':b['id'],'account_class':'STORAGE','idempotency_key':'v021-edit'})
    balance=await call('get_balance',{});assert '自由可用资金' in balance.content and '消费储值' in balance.content
    await call('update_account',{'account_id':b['id'],'account_class':'CONSUMPTION','idempotency_key':'v021-edit-back'})
    await call('record_bonus',{'account_id':b['id'],'amount':5,'idempotency_key':'v021-gift'})
    generic=await call('wubu',{'operation':'get_account_statistics','account_id':b['id']});assert generic.data['bonus']==15
    print('SDK v0.2.1 Account Classes acceptance passed: 100 -> 110, spend, refunds, class edit, stats, stable replay.')
asyncio.run(main())
