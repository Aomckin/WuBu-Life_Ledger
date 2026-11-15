"""v0.4 integration on the runner's disposable database, with read-only SDK imports."""
import asyncio, os
from pathlib import Path
from datetime import datetime, timezone
from wubu_tool.package import create_package
assert Path(os.environ['DATABASE_PATH']).parent.name.startswith('wubu-dogfooding-')
tool = {t.name:t for t in create_package().create_tools({'base_url':os.environ['WUBU_TEST_BASE_URL']})}['wubu']
async def call(op, **kw):
    r=await tool.run({'operation':op,**kw})
    assert r.success,(op,r)
    return r.data
async def main():
    a=await call('create_account',payload={'name':'v04 银行','type':'BANK','initial_balance':100},idempotency_key='v04-account')
    at=datetime.now(timezone.utc).isoformat()
    p={'provider':'WECHAT','capture_source':'AGENT','account_id':a['id'],'amount':20,'direction':'EXPENSE','occurred_at':at,'merchant_raw':'午餐店','raw_text':'支付20元','external_id':'sdk-v04-001'}
    r=await call('capture_financial_event',payload=p,idempotency_key='v04-capture')
    assert r['transaction_id']
    dup=await call('capture_financial_event',payload=p,idempotency_key='v04-duplicate')
    assert dup['transaction_id']==r['transaction_id']
    near=await call('find_transactions_near_time',query={'occurred_at':at,'window_minutes':60,'account_id':a['id']})
    assert len(near['items'])==1
    await call('annotate_transaction',transaction_id=r['transaction_id'],payload={'category':'餐饮','item_name':'白切鸡盖饭','tags':['外食'],'confidence':0.99,'annotated_by':'agent','annotation_status':'AUTO_ANNOTATED'},idempotency_key='v04-annotate')
    un=await call('get_unannotated_transactions',query={'account_id':a['id'],'period':'today'})
    assert len(un['items'])==0,un
    ev=await call('attach_semantic_evidence',payload={'source':'image','text':'无对应流水的照片'},idempotency_key='v04-evidence')
    assert ev['status']=='UNMATCHED'
    b=await call('get_accounts');assert next(x for x in b['items'] if x['id']==a['id'])['current_balance']==80
    schema=tool.input_model.model_json_schema();assert 'TransactionAnnotation' in schema['$defs']
    print('SDK v0.4 PASS: capture, dedup, nearby query, semantic annotation, unmatched evidence, unchanged balance.')
asyncio.run(main())
