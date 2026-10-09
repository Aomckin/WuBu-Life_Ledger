"""Add v0.2 cashflow/lifecycle endpoints to the retained Stockroom OpenAPI."""
import copy
import json
from pathlib import Path
root=Path(__file__).resolve().parents[1]
p=root/'internal/web/static/openapi.json'
doc=json.loads(p.read_text(encoding='utf-8'))
s=doc['components']['schemas']
def ref(name): return {'$ref':'#/components/schemas/'+name}
def text(maximum=2000): return {'type':'string','maxLength':maximum}
def obj(properties,required=()): return {'type':'object','properties':properties,'required':list(required),'additionalProperties':False}
def array(schema): return {'type':'array','items':schema}
money={'type':'number','minimum':0,'maximum':10_000_000_000,'multipleOf':0.01,'description':'CNY yuan, persisted as integer cents.'}
balance={**money,'minimum':-10_000_000_000,'description':'Signed net balance or delta in CNY yuan.'}
time={'type':'string','description':'RFC3339 with timezone; empty/omitted defaults to now.'}
source={'type':'string','enum':['manual','agent','import','system','receipt'],'default':'manual'}
account_ref={**text(200),'description':'Account ID or exact name; omitted uses default payment account, then unknown.'}
statuses=['ACTIVE','STORED','LOST','SOLD','RETIRED']
for name in ['PurchaseInput','PurchaseAmendment','Purchase','AgentPurchaseInput','MaintenanceInput','Maintenance']:
 if name in s and 'properties' in s[name]: s[name]['properties']['account_id']=account_ref
for name in ['Purchase','AgentPurchaseSummary']:
 s[name]['properties'].update(status={'type':'string','enum':['COMPLETED','PARTIAL_REFUND','REFUNDED','PREVIEW','']},account_id=account_ref,refunded_amount=money)
s['AgentPurchaseLine']['properties'].update(item_type={'type':'string','enum':['CONSUMABLE','DURABLE','UNTRACKED'],'default':'CONSUMABLE'},location=text(100))
s['DurableDetails']['properties']['status']={'type':'string','enum':statuses}
s['DurableDetails']['properties'].update(purchase_price=money,purchase_event_id=text(200),location=text(100),use_days={'type':'integer'},warranty_days={'type':['integer','null']})
s['DurablePatch']['properties']['status']={'type':'string','enum':statuses+['ARCHIVED','DISCARDED'],'description':'ARCHIVED input maps to STORED; DISCARDED to RETIRED. SOLD posts income atomically; ended lifecycle cannot be restored.'}
s['MaintenanceInput']['properties']['status']['description']='PLANNED requires cost=0; DONE cost>0 posts MoneyTransaction and account balance atomically.'
s['AccountInput']=obj({'name':text(100),'type':{'type':'string','enum':['WALLET','BANK','CASH','CAMPUS_CARD','CREDIT_CARD','OTHER'],'default':'OTHER'},'currency':{'type':'string','enum':['CNY']},'initial_balance':balance,'credit_limit':money,'note':text()},['name'])
s['Account']=obj({'id':text(200),'name':text(100),'type':s['AccountInput']['properties']['type'],'currency':{'const':'CNY'},'current_balance':balance,'credit_limit':{**money,'type':['number','null']},'active':{'type':'boolean'},'note':text(),'created_at':time,'updated_at':time},['id','name','current_balance','active'])
s['AccountPatch']=obj({'name':text(100),'active':{'type':'boolean'},'note':text(),'credit_limit':money})
s['MoneySettings']=obj({'default_payment_account':text(200)},['default_payment_account'])
s['ReturnItem']=obj({'item_id':text(200),'quantity':{'type':'number','exclusiveMinimum':0,'multipleOf':0.001}},['item_id','quantity'])
props={'account_id':account_ref,'from_account':account_ref,'to_account':account_ref,'amount':money,'actual_balance':balance,'category':text(100),'merchant':text(200),'purchase_event_id':text(200),'related_transaction_id':text(200),'external_id':text(200),'occurred_at':time,'source':source,'note':text(),'reason':text(),'return_items':array(ref('ReturnItem'))}
s['MoneyInput']=obj(props)
for name,fields,required in [
 ('IncomeInput',['account_id','amount','category','external_id','occurred_at','source','note'],['amount']),
 ('ExpenseInput',['account_id','amount','category','merchant','external_id','occurred_at','source','note'],['amount']),
 ('TransferInput',['from_account','to_account','amount','external_id','occurred_at','source','note'],['from_account','to_account','amount']),
 ('RefundInput',['account_id','amount','purchase_event_id','related_transaction_id','external_id','occurred_at','source','note','return_items'],['amount']),
 ('BalanceAdjustmentInput',['account_id','actual_balance','occurred_at','external_id','source','note','reason'],['actual_balance'])]:
 s[name]=obj({f:props[f] for f in fields},required)
s['RefundInput']['anyOf']=[{'required':['purchase_event_id']},{'required':['related_transaction_id']}]
s['MoneyTransaction']=obj({'id':text(200),'account_id':text(200),'type':{'type':'string','enum':['EXPENSE','INCOME','TRANSFER_OUT','TRANSFER_IN','REFUND','ADJUSTMENT']},'amount':money,'balance_delta':balance,'balance_before':balance,'balance_after':balance,'category':text(100),'merchant':text(200),'purchase_event_id':text(200),'maintenance_event_id':text(200),'related_transaction_id':text(200),'external_id':text(200),'occurred_at':time,'source':source,'note':text(),'reason':text(),'created_at':time,'voided_at':time},['id','account_id','type','amount','balance_delta','balance_before','balance_after'])
s['MoneyResult']=obj({'transactions':array(ref('MoneyTransaction'))},['transactions'])
s['BalanceSummary']=obj({'currency':{'const':'CNY'},'total_balance':balance,'available_balance':balance,'accounts':array(ref('Account'))},['total_balance','available_balance','accounts'])
s['MoneyGroup']=obj({'key':text(),'amount':balance,'count':{'type':'integer'}})
s['MoneySummary']=obj({'currency':{'const':'CNY'},'expense':money,'refund':money,'net_expense':balance,'income':money,'transfer_out':money,'groups':array(ref('MoneyGroup'))})
s['DurableActionInput']=obj({'status':{'type':'string','enum':statuses},'location':text(100),'amount':money,'account_id':account_ref,'occurred_at':time,'source':source,'note':text()})
s['DurableEvent']=obj({'id':text(200),'item_id':text(200),'type':{'type':'string','enum':['PURCHASED','ACTIVATED','STORED','MOVED','LOST','FOUND','SOLD','RETIRED']},'occurred_at':time,'from_status':text(30),'to_status':text(30),'location':text(100),'source':source,'note':text()})
s['DurableRow']=obj({'name':text(100),'details':ref('DurableDetails')})
s['ItemCost']=obj({'item_id':text(200),'purchase_cost':money,'maintenance_cost':money,'total_cost':money,'refund':money,'net_cost':balance,'purchase_count':{'type':'integer'},'purchase_quantity':{'type':'number'},'latest_price':{**money,'type':['number','null']},'maintenance_count':{'type':'integer'}})
s['CategoryInput']=obj({'name':text(100),'previous_name':text(100)},['name'])
s['MoneyImportInput']=obj({'account_id':account_ref,'adapter':{'type':'string','enum':['generic','alipay','wechat']},'csv':{'type':'string','maxLength':900000}},['account_id','adapter','csv'])
s['MoneyImportRow']=obj({'type':text(30),**props},['type','amount'])
s['MoneyImportResult']=obj({'rows':array(ref('MoneyImportRow')),'imported':{'type':'integer'},'duplicates':{'type':'integer'},'skipped':{'type':'integer'},'transactions':array(ref('MoneyTransaction'))})
for name in ['LedgerSummary','Dashboard']:
 if name in s:s[name]['properties']['month_expense']=balance;s[name]['properties'].update(total_balance=balance,available_balance=balance,today_expense=balance,durable_count={'type':'integer'},consumable_count={'type':'integer'},recent_transactions=array(ref('MoneyTransaction')))
def query(name,schema):return {'name':name,'in':'query','schema':schema}
range_parameters=[query('period',{'type':'string','enum':['today','week','month','year'],'description':'Shanghai calendar; exclusive with from/to. Week starts Monday.'}),query('from',{'type':'string','format':'date-time'}),query('to',{'type':'string','format':'date-time'}),query('limit',{'type':'integer','minimum':1,'maximum':500,'default':100}),query('offset',{'type':'integer','minimum':0,'default':0})]
money_parameters=range_parameters+[query('account_id',text(200)),query('category',text(100)),query('merchant',text(200)),query('type',s['MoneyTransaction']['properties']['type']),query('to_account',text(200)),query('group_by',{'enum':['category','merchant','account','item'],'type':'string'})]
key={'name':'Idempotency-Key','in':'header','required':False,'schema':{'type':'string','maxLength':200},'description':'Stable key per reality event. Same key/input replays result; different input returns 409. Failed events do not reserve keys.'}
def page(name): return obj({'items':array(ref(name)),'limit':{'type':'integer'},'offset':{'type':'integer'}})
def endpoint(path,method,operation,input_name=None,response=None,parameters=None,status=200,description=''):
 params=list(parameters or [])
 if '{id}' in path:params.insert(0,{'name':'id','in':'path','required':True,'schema':text(200)})
 if method not in ['get'] and operation!='preview_money_import':params.append(key)
 responses={str(status):{'description':'Success','content':{'application/json':{'schema':response or {'type':'object'}}}},'400':{'$ref':'#/components/responses/InvalidJSON'},'404':{'$ref':'#/components/responses/NotFound'},'409':{'description':'Conflict or idempotency mismatch','content':{'application/json':{'schema':ref('Error')}}},'422':{'$ref':'#/components/responses/ValidationError'},'500':{'$ref':'#/components/responses/InternalError'}}
 op={'tags':['Money' if '/money/' in path or '/accounts' in path else 'Lifecycle'],'operationId':operation,'summary':operation.replace('_',' '),'parameters':params,'responses':responses,'description':description}
 if input_name:op['requestBody']={'required':True,'content':{'application/json':{'schema':ref(input_name)}}}
 doc['paths'].setdefault(path,{})[method]=op
endpoint('/api/v1/accounts','get','get_accounts',response=obj({'items':array(ref('Account'))}))
endpoint('/api/v1/accounts','post','create_account','AccountInput',ref('Account'),status=201,description='Initial balance is recorded as ADJUSTMENT with reason INITIAL_BALANCE, including zero. CREDIT_CARD uses net balance; debt is negative.')
endpoint('/api/v1/accounts/{id}','get','get_account',response=ref('Account'))
endpoint('/api/v1/accounts/{id}','patch','update_account','AccountPatch',ref('Account'))
endpoint('/api/v1/money/settings','get','get_money_settings',response=ref('MoneySettings'))
endpoint('/api/v1/money/settings','patch','set_default_payment_account','MoneySettings',ref('MoneySettings'))
endpoint('/api/v1/money/balance','get','get_balance',response={'oneOf':[ref('Account'),ref('BalanceSummary')]},parameters=[query('account_id',text(200))],description='No account_id: all real account net balances, excluding unknown; available balance excludes credit cards and inactive accounts. With account_id: one account.')
for route,op,request in [('expenses','record_expense','ExpenseInput'),('income','record_income','IncomeInput'),('transfers','transfer_money','TransferInput'),('refunds','record_refund','RefundInput'),('adjustments','adjust_balance','BalanceAdjustmentInput'),('reconcile','reconcile_account','BalanceAdjustmentInput')]:endpoint('/api/v1/money/'+route,'post',op,request,ref('MoneyResult'),status=201,description='Atomic state + event. Refund links original EXPENSE; return_items only when goods actually returned. Transfer posts both linked legs and preserves total money.')
endpoint('/api/v1/money/transactions','get','get_recent_transactions',response=page('MoneyTransaction'),parameters=money_parameters,description='Includes corrected entries with voided_at and their compensating adjustments so all balance changes remain explainable; summaries exclude corrected entries.')
for route,op in [('summary','get_money_summary'),('expense-summary','get_expense_summary'),('income-summary','get_income_summary'),('category-summary','get_category_summary'),('merchant-summary','get_merchant_summary')]:endpoint('/api/v1/money/'+route,'get',op,response=ref('MoneySummary'),parameters=money_parameters,description='Net expense = expenses - refunds within the requested occurrence-time range. Transfers/adjustments excluded from expense/income; income-summary selects INCOME. Item groups allocate actual payment by purchase line share.')
endpoint('/api/v1/money/categories','get','get_money_categories',response=obj({'items':array(text(100))}))
endpoint('/api/v1/money/categories','post','save_money_category','CategoryInput',ref('CategoryInput'))
endpoint('/api/v1/durables','get','get_durable_items',response=page('DurableRow'),parameters=range_parameters+[query('status',{'type':'string','enum':statuses}),query('location',text(100)),query('search',text(100))])
for route,op in [('sell','sell_item'),('retire','retire_item'),('move','move_item'),('status','set_durable_status')]:endpoint('/api/v1/items/{id}/'+route,'post',op,'DurableActionInput',ref('DurableDetails'),description='SOLD/RETIRED are terminal. STORED remains owned with quantity 1. Sell requires positive amount and posts income atomically.')
endpoint('/api/v1/items/{id}/durable-events','get','get_durable_history',response=page('DurableEvent'),parameters=range_parameters)
endpoint('/api/v1/items/{id}/total-cost','get','get_item_total_cost',response=ref('ItemCost'),parameters=range_parameters)
endpoint('/api/v1/money/import/preview','post','preview_money_import','MoneyImportInput',ref('MoneyImportResult'),description='Read only CSV parsing and dedup preview. No implicit inventory/purchase creation; ambiguous transfers/refunds require explicit account/link in generic CSV.')
endpoint('/api/v1/money/import','post','import_money','MoneyImportInput',ref('MoneyImportResult'),status=201,description='All rows commit atomically. source/account/external_id dedup; fallback hash account/type/time/amount/merchant. At most 500 rows per batch.')
doc['info']['version']='0.2.0'
# Replace obsolete expense terminology in retained route descriptions.
for path,entries in doc['paths'].items():
 for op in entries.values():
  if isinstance(op,dict) and 'description' in op:op['description']=op['description'].replace('creates maintenance and Expense','creates maintenance and MoneyTransaction').replace('purchase, expense and inventory','purchase, MoneyTransaction and inventory')
p.write_text(json.dumps(doc,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
