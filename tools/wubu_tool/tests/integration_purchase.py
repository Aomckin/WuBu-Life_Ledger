"""End-to-end real SDK -> HTTP -> temporary SQLite; run via scripts/test.ps1."""
import asyncio,json,os,sqlite3
from pathlib import Path
from urllib.request import urlopen
from wubu_tool.package import create_package
base=os.environ['WUBU_TEST_BASE_URL']
if not Path(os.environ['DATABASE_PATH']).parent.name.startswith('wubu-dogfooding-'):
 raise RuntimeError('Integration test requires an isolated temporary database')
p=create_package();tools={t.name:t for t in p.create_tools({'base_url':base})}
async def main():
 args={'items':[{'name':'无糖可乐','spec':'2L','quantity':1,'unit':'瓶','price':5.5}],'idempotency_key':'e2e-cola'}
 preview=await tools['validate_purchase'].run({k:v for k,v in args.items() if k!='idempotency_key'})
 assert preview.success and not preview.data['purchase_id'],preview
 first=await tools['record_purchase'].run(args);assert first.success,first
 line=first.data['items'][0];assert line['name']=='无糖可乐' and line['spec']=='2L' and line['inventory_after']==1 and line['quantity_added']==1 and first.data['expense_amount']==5.5,first
 retry=await tools['record_purchase'].run(args);assert retry.success and retry.data==first.data,retry
 repeat=await tools['record_purchase'].run({'items':[{'name':'无糖可乐 ２０００ｍｌ','quantity':2,'unit':'瓶','price':5.5}],'idempotency_key':'e2e-cola-two'})
 assert repeat.success and repeat.data['items'][0]['item_id']==line['item_id'] and repeat.data['items'][0]['inventory_after']==3,repeat
 alias=await tools['wubu'].run({'operation':'add_product_alias','item_id':line['item_id'],'payload':{'alias':'零度可乐大瓶'},'idempotency_key':'e2e-alias'});assert alias.success,alias
 drink={'action':'consume','name':'零度可乐大瓶','quantity':1,'idempotency_key':'e2e-drink'}
 consume=await tools['wubu_inventory_event'].run(drink);assert consume.success and consume.data['item']['quantity']==2,consume
 retry=await tools['wubu_inventory_event'].run(drink);assert retry.success and retry.data==consume.data,retry
 small=await tools['record_purchase'].run({'items':[{'name':'无糖可乐','spec':'500ml','quantity':1,'unit':'瓶','price':3}],'idempotency_key':'e2e-small'});assert small.success,small
 ambiguous=await tools['wubu_inventory_event'].run({'action':'consume','name':'无糖可乐','quantity':1,'idempotency_key':'e2e-ambiguous'});assert not ambiguous.success and ambiguous.data['error']['code']=='ITEM_AMBIGUOUS' and len(ambiguous.data['error']['candidates'])==2,ambiguous
 missing=await tools['wubu_inventory_event'].run({'action':'open','name':'从未购买','idempotency_key':'e2e-missing'});assert not missing.success and missing.data['error']['code']=='ITEM_NOT_FOUND',missing
 with sqlite3.connect('file:'+Path(os.environ['DATABASE_PATH']).as_posix()+'?mode=ro',uri=True) as db:
  assert db.execute('SELECT COUNT(*) FROM purchase_events').fetchone()[0]==3
  assert db.execute('SELECT COUNT(*) FROM expenses').fetchone()[0]==3
  assert db.execute("SELECT COUNT(*) FROM inventory_events WHERE source!='agent'").fetchone()[0]==0
  assert db.execute("SELECT quantity,unit,package_size,package_unit FROM items WHERE id=?",(line['item_id'],)).fetchone()==(2.0,'瓶',2.0,'L')
 print(json.dumps({'e2e':'PASS','confirmation':first.content,'stock_after_repeat_and_consume':2,'ambiguity_candidates':2,'preview_and_failed_calls_wrote_nothing':True},ensure_ascii=False))
asyncio.run(main())
