"""Synchronize native statement contracts, preserving existing API schemas."""
import copy
import json
import re
from pathlib import Path

root=Path(__file__).resolve().parents[1]
path=root/'internal/web/static/openapi.json'
doc=json.loads(path.read_text(encoding='utf-8'))
schemas=doc['components']['schemas']

def schema(typ):
    if typ.startswith('*'):
        return {**schema(typ[1:]),'nullable':True}
    if typ=='[]byte':return {'type':'string','format':'byte','description':'Original native file bytes encoded as base64.'}
    if typ.startswith('[]'):return {'type':'array','items':schema(typ[2:])}
    if typ=='map[string]string':return {'type':'object','additionalProperties':{'type':'string'}}
    if typ=='string':return {'type':'string'}
    if typ=='int':return {'type':'integer'}
    if typ=='bool':return {'type':'boolean'}
    if typ in ('Money','SignedMoney'):return {'type':'number','description':'Yuan, exactly two decimal places; stored as integer cents.'}
    return {'$ref':'#/components/schemas/'+typ}

for name,body in re.findall(r'type (\w+) struct \{(.*?)\n\}',(root/'internal/ledger/statement.go').read_text(encoding='utf-8'),re.S):
    properties={tag:schema(typ) for typ,tag in re.findall(r'\w+\s+(\S+)\s+`json:"([^",]+)[^"]*"`',body)}
    schemas[name]={'type':'object','additionalProperties':False,'properties':properties,'required':list(properties)}
schemas['StatementInput']['required']=['filename','file_base64']
schemas['StatementInput']['properties']['filename']['maxLength']=200
schemas['StatementInput']['properties']['sync_id']['maxLength']=100
schemas['StatementCommitInput']=copy.deepcopy(schemas['StatementInput'])
schemas['StatementCommitInput']['required']+=['preview_token']
schemas['StatementTemplate']={'type':'object','additionalProperties':False,'properties':{'id':{'type':'string'},'name':{'type':'string'},'group':{'type':'string','enum':['payment','bank']},'extensions':{'type':'array','items':{'type':'string'}}},'required':['id','name','group','extensions']}
ref=lambda name:{'$ref':'#/components/schemas/'+name}
response=lambda model:{'description':'Success','content':{'application/json':{'schema':ref(model)}}}
codes=['wechat','alipay','abc_debit','bmo','boc','bocom_credit','bocom_debit','ccb','cgb_credit','cib_debit','citic','cmb','hsbchk','icbc','spdb_debit','td']
provider={'name':'provider','in':'path','required':True,'schema':{'type':'string','enum':codes},'description':'Explicit payment provider or bank statement template. No arbitrary CSV detection.'}
key={'name':'Idempotency-Key','in':'header','required':True,'schema':{'type':'string','maxLength':200}}
for action in ['preview','commit']:
    doc['paths']['/api/v1/statements/{provider}/'+action]={'post':{'operationId':'statement_'+action,'summary':'Preview native statement without writes' if action=='preview' else 'Commit the confirmed native statement atomically','description':'Native file limit 5 MiB and 2000 records. Preview tokens bind the complete request and ledger revision; a changed ledger returns 409. Commit retries retain the same key, token and payload. Bad rows remain reviewable evidence. Original bytes are retained with the import batch.','parameters':[provider]+([key] if action=='commit' else []),'requestBody':{'required':True,'content':{'application/json':{'schema':ref('StatementInput' if action=='preview' else 'StatementCommitInput')}}},'responses':{'200' if action=='preview' else '201':response('StatementResult'),'409':{'description':'Stale preview or idempotency conflict'},'422':{'description':'Wrong provider, invalid file or missing preview'}}}}
doc['paths']['/api/v1/statements/templates']={'get':{'operationId':'statement_templates','summary':'Explicit native payment and bank template registry','responses':{'200':{'description':'Template registry','content':{'application/json':{'schema':{'type':'object','properties':{'items':{'type':'array','items':ref('StatementTemplate')},'parser_version':{'type':'string'}}}}}}}}}
sync={'name':'sync_id','in':'query','schema':{'type':'string','maxLength':100}}
doc['paths']['/api/v1/statements/batches']={'get':{'operationId':'statement_batches','summary':'Native import history','parameters':[sync,{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':500}},{'name':'offset','in':'query','schema':{'type':'integer','minimum':0}}],'responses':{'200':{'description':'Import batches','content':{'application/json':{'schema':{'type':'object','properties':{'items':{'type':'array','items':ref('ImportBatch')}}}}}}}}}
doc['paths']['/api/v1/statements/summary']={'get':{'operationId':'statement_summary','summary':'Weekly sync totals, current pending review and account reconciliation health','parameters':[sync],'responses':{'200':response('StatementSummary')}}}
doc['paths']['/api/v1/statements/batches/{id}/file']={'get':{'operationId':'statement_original_file','summary':'Download unchanged original statement bytes','parameters':[{'name':'id','in':'path','required':True,'schema':{'type':'string'}}],'responses':{'200':{'description':'Original attachment','content':{'application/octet-stream':{'schema':{'type':'string','format':'binary'}}},'headers':{'Content-Disposition':{'schema':{'type':'string'}}}},'404':{'description':'Batch not found'}}}}
for route in ['/api/v1/money/import','/api/v1/money/import/preview']:
    doc['paths'][route]['post']['deprecated']=True
    doc['paths'][route]['post']['description']=doc['paths'][route]['post']['description'].split(' Legacy compatibility only;')[0]+' Legacy compatibility only; the product uses explicit native statement endpoints.'
path.write_text(json.dumps(doc,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
