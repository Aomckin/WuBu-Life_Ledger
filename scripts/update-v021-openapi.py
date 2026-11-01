"""Extend the retained API contract for v0.2.1 Account Classes."""
import copy
import json
from pathlib import Path
root=Path(__file__).resolve().parents[1]
p=root/'internal/web/static/openapi.json'
doc=json.loads(p.read_text(encoding='utf-8'))
s=doc['components']['schemas']
def ref(name):return {'$ref':'#/components/schemas/'+name}
def obj(properties,required=()):return {'type':'object','properties':properties,'required':list(required),'additionalProperties':False}
def text(n=2000):return {'type':'string','maxLength':n}
def array(schema):return {'type':'array','items':schema}
def query(name,schema):return {'name':name,'in':'query','schema':schema}
class_schema={'type':'string','enum':['STORAGE','CONSUMPTION'],'description':'Current fund-use boundary, independent of account type. Existing accounts migrate to STORAGE.'}
def enums(value):
 if isinstance(value,dict):
  enum=value.get('enum',[])
  if 'CREDIT_CARD' in enum and 'WALLET' in enum and 'MERCHANT_BALANCE' not in enum:enum.append('MERCHANT_BALANCE')
  if 'ADJUSTMENT' in enum and 'TRANSFER_OUT' in enum and 'BONUS' not in enum:enum.append('BONUS')
  for child in value.values():enums(child)
 elif isinstance(value,list):
  for child in value:enums(child)
enums(doc)
s['MoneyWarning']=obj({'code':{'type':'string','enum':['TOP_UP_VALUE_LOSS','ACCOUNT_HAS_REMAINING_BALANCE']},'message':text()},['code','message'])
for name in ['AccountInput','Account','AccountPatch']:
 s[name]['properties'].update(account_class=copy.deepcopy(class_schema),merchant=text(200))
s['AccountInput']['properties']['account_class']['default']='STORAGE'
s['Account']['properties']['warnings']=array(ref('MoneyWarning'))
s['Account']['required']=list(dict.fromkeys(s['Account']['required']+['account_class','merchant','warnings']))
s['MoneyResult']['properties']['warnings']={'type':['array','null'],'items':ref('MoneyWarning')}
signed=copy.deepcopy(s['BalanceSummary']['properties']['available_balance'])
for name in ['BalanceSummary','LedgerSummary']:
 s[name]['properties'].update(liquid_balance=copy.deepcopy(signed),consumption_balance=copy.deepcopy(signed),total_value=copy.deepcopy(signed))
s['LedgerSummary']['properties']['accounts']=array(ref('Account'))
s['BalanceSummary']['properties']['available_balance']['description']='Compatibility alias for liquid_balance; excludes restricted funds, inactive accounts, credit cards, unknown.'
s['BalanceSummary']['properties']['total_balance']['description']='Legacy net balance across all real accounts, including credit cards and inactive accounts; not a liquid-funds indicator.'
s['BalanceSummary']['properties']['total_value']['description']='liquid_balance + consumption_balance, enabled real non-credit accounts only.'
money=copy.deepcopy(s['MoneyInput']['properties']['amount'])
s['MoneySummary']['properties']['bonus']=copy.deepcopy(money)
s['TopUpInput']=obj({'from_account':text(200),'to_account':text(200),'paid_amount':{**money,'exclusiveMinimum':0},'credited_amount':{**money,'exclusiveMinimum':0},'occurred_at':s['MoneyInput']['properties']['occurred_at'],'source':s['MoneyInput']['properties']['source'],'note':text()},['from_account','to_account','paid_amount','credited_amount'])
s['BonusInput']=copy.deepcopy(s['IncomeInput'])
s['BonusInput']['description']='Positive restricted gift value; account must be CONSUMPTION. Excluded from ordinary income and expense.'
s['AccountStatistics']=obj({'account_id':text(200),'top_up_principal':money,'bonus':money,'expense':money,'refund':money,'net_expense':signed,'current_balance':signed,'bonus_ratio':{'type':'number','minimum':0}},['account_id','top_up_principal','bonus','expense','refund','net_expense','current_balance','bonus_ratio'])
key=copy.deepcopy(next(x for x in doc['paths']['/api/v1/money/transfers']['post']['parameters'] if x['name']=='Idempotency-Key'))
responses=copy.deepcopy(doc['paths']['/api/v1/money/transfers']['post']['responses'])
def endpoint(path,method,operation,input_name,response,status,description):
 params=[]
 if '{id}' in path:params.append({'name':'id','in':'path','required':True,'schema':text(200)})
 if method!='get':params.append(key)
 result=copy.deepcopy(responses);result.pop('201',None)
 result[str(status)]={'description':'Success','content':{'application/json':{'schema':ref(response)}}}
 value={'tags':['Money'],'operationId':operation,'summary':operation.replace('_',' '),'description':description,'parameters':params,'responses':result}
 if input_name:value['requestBody']={'required':True,'content':{'application/json':{'schema':ref(input_name)}}}
 doc['paths'].setdefault(path,{})[method]=value
endpoint('/api/v1/money/top-ups','post','top_up_account','TopUpInput','MoneyResult',201,'Atomic STORAGE -> CONSUMPTION top up: paired principal transfers plus BONUS for positive difference. Equal amounts generate no BONUS. Value loss generates ADJUSTMENT and TOP_UP_VALUE_LOSS warning. Stable Idempotency-Key prevents duplicate principal and gift. All amounts positive; source and destination must differ and be active. Does not count as ordinary income or consumption.')
endpoint('/api/v1/money/bonuses','post','record_bonus','BonusInput','MoneyResult',201,'Bonus-only merchant gift, allowed only into active CONSUMPTION accounts. Not ordinary income or expense.')
endpoint('/api/v1/accounts/{id}/statistics','get','get_account_statistics',None,'AccountStatistics',200,'Lifetime actual top-up principal, BONUS, gross expense, refunds linked to this original paying account even when refunded elsewhere, net expense and current balance; ratio is zero with no top-up principal. Current account class interprets balances; no class history reconstruction.')
doc['paths']['/api/v1/accounts']['get']['parameters']=[query('account_class',class_schema),query('merchant',text(200)),query('active',{'type':'boolean'})]
doc['paths']['/api/v1/accounts']['post']['description']='Initial balance is an ADJUSTMENT including zero. CREDIT_CARD uses net balance, debt negative. Account class defaults to STORAGE without guessing from type; CONSUMPTION merchant binding is optional.'
doc['paths']['/api/v1/accounts/{id}']['patch']['description']='Change current account class, merchant binding, name, credit limit, note or active state without rewriting any money history. Deactivating a consumption account with positive value returns ACCOUNT_HAS_REMAINING_BALANCE warning and preserves value. Choose another default before deactivation.'
doc['paths']['/api/v1/money/balance']['get']['description']='Returns separate liquid_balance and consumption_balance; total_value is their sum. Active STORAGE contributes to liquid, active CONSUMPTION to restricted value. Unknown and credit cards excluded from both. available_balance aliases liquid; total_balance retains legacy net semantics. Classification follows current account class, without historical versioning.'
for path in ['/api/v1/purchases','/api/v1/purchases/record','/api/v1/purchases/validate','/api/v1/purchases/{id}']:
 for method,value in doc['paths'].get(path,{}).items():
  if method in ['post','put'] and 'ACCOUNT_MERCHANT_MISMATCH' not in value.get('description',''):value['description']=value.get('description','')+' For CONSUMPTION payment with merchant binding, omitted merchant is inferred and persisted; explicit mismatched merchant returns 409 ACCOUNT_MERCHANT_MISMATCH before any write. Explicit account_id wins over default.'
doc['info']['version']='0.2.1'
p.write_text(json.dumps(doc,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
