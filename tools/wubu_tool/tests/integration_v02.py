"""Real Zhaoxi SDK -> HTTP -> SQLite v0.2 acceptance on the temporary test service."""
import asyncio,json,os,sqlite3
from pathlib import Path
from wubu_tool.package import create_package
if not Path(os.environ['DATABASE_PATH']).parent.name.startswith('wubu-dogfooding-'):
    raise RuntimeError('Use only isolated temporary validation databases')
package=create_package()
tools={tool.name:tool for tool in package.create_tools({'base_url':os.environ['WUBU_TEST_BASE_URL']})}
async def call(name,arguments):
    result=await tools[name].run(arguments)
    assert result.success,(name,result)
    return result.data
async def main():
    a=await call('create_account',{'name':'SDK 微信','type':'WALLET','initial_balance':1000,'idempotency_key':'v02-a'})
    b=await call('create_account',{'name':'SDK 支付宝','type':'WALLET','initial_balance':1000,'idempotency_key':'v02-b'})
    await call('set_default_payment_account',{'default_payment_account':a['id'],'idempotency_key':'v02-default'})
    p=await call('record_purchase',{'items':[{'name':'SDK 键盘','quantity':1,'unit':'个','price':399,'item_type':'DURABLE','location':'宿舍'}],'idempotency_key':'v02-keyboard'})
    item=p['items'][0]['item_id']
    await call('add_maintenance',{'item_id':item,'title':'换轴','cost':50,'idempotency_key':'v02-maintenance'})
    cost=await call('get_item_total_cost',{'item_id':item});assert cost['total_cost']==449,cost
    await call('move_item',{'item_id':item,'location':'家里','idempotency_key':'v02-move'})
    await call('set_durable_status',{'item_id':item,'status':'STORED','idempotency_key':'v02-store'})
    owned=await call('get_durable_items',{'status':'STORED','location':'家里'});assert owned['items'][0]['details']['purchase_price']==399,owned
    transfer={'from_account':a['id'],'to_account':b['id'],'amount':100,'idempotency_key':'v02-transfer'}
    first=await call('transfer_money',transfer);second=await call('transfer_money',transfer);assert first==second
    sale=await call('sell_item',{'item_id':item,'account_id':b['id'],'amount':260,'idempotency_key':'v02-sale'});assert sale['status']=='SOLD' and sale['purchase_price']==399,sale
    pay=await call('record_expense',{'account_id':a['id'],'amount':100,'merchant':'餐馆','category':'餐饮','idempotency_key':'v02-expense'})
    await call('record_refund',{'related_transaction_id':pay['transactions'][0]['id'],'amount':100,'idempotency_key':'v02-refund'})
    await call('record_income',{'account_id':a['id'],'amount':4000,'idempotency_key':'v02-income'})
    await call('adjust_balance',{'account_id':a['id'],'actual_balance':4321.08,'reason':'真实余额','idempotency_key':'v02-adjust'})
    a_balance=await call('get_balance',{'account_id':a['id']});assert a_balance['current_balance']==4321.08,a_balance
    summary=await call('get_expense_summary',{'account_id':a['id'],'period':'month','group_by':'category'});assert summary['net_expense']==449 and summary['income']==4000,summary
    history=await call('get_recent_transactions',{'account_id':a['id'],'limit':20});assert history['items'],history
    for tool in tools.values(): tool.schema()  # Every model produces a usable SDK schema.
    with sqlite3.connect('file:'+Path(os.environ['DATABASE_PATH']).as_posix()+'?mode=ro',uri=True) as db:
        mismatches=db.execute('SELECT a.id FROM accounts a LEFT JOIN money_transactions m ON a.id=m.account_id GROUP BY a.id HAVING a.balance_cents<>COALESCE(SUM(m.delta_cents),0)').fetchall()
        assert not mismatches,mismatches
        assert db.execute("SELECT type FROM sqlite_master WHERE name='expenses'").fetchone()[0]=='view'
    print(json.dumps({'v02_e2e':'PASS','ownership_cost':449,'sale_income':260,'balance':4321.08,'idempotent_transfer':True,'all_balances_explained':True},ensure_ascii=False))
asyncio.run(main())
