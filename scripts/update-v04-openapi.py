"""Add/update v0.4 contracts without rewriting contracts from earlier versions."""
import json,re
from pathlib import Path
root=Path(__file__).resolve().parents[1]
p=root/'internal/web/static/openapi.json';doc=json.loads(p.read_text(encoding='utf-8'));schemas=doc['components']['schemas']
source=(root/'internal/ledger/financial.go').read_text(encoding='utf-8')
for name,body in re.findall(r'type (\w+) struct \{(.*?)\n\}',source,re.S):
    if name in ('RawFinancialEvent','AnnotatedTransaction','FinancialQuery','RawFinancialQuery'):continue
    props={}
    for typ,tag in re.findall(r'\w+\s+(\*?\[\]string|\*?\w+)\s+`json:"([^",]+)(?:,[^"]*)?"`',body):
        nullable=typ.startswith('*');typ=typ.lstrip('*')
        s={'type':'number'} if typ in ('Money','SignedMoney','float64') else {'type':'integer'} if typ=='int' else {'type':'boolean'} if typ=='bool' else {'type':'array','items':{'type':'string'}} if typ=='[]string' else {'$ref':'#/components/schemas/'+typ} if typ=='TransactionAnnotation' else {'type':'string'}
        if nullable:s['nullable']=True
        props[tag]=s
    schemas[name]={'type':'object','additionalProperties':False,'properties':props}
required={'RawFinancialInput':['provider','capture_source','raw_text'],'FinancialReview':['action','reason'],'TransactionAnnotation':['annotated_by','annotation_status','confidence'],'MerchantRule':['merchant_raw','annotation','user_confirmed'],'SemanticEvidence':['source'],'FinancialSettings':['auto_commit_threshold']}
for name,fields in required.items():schemas[name]['required']=fields
schemas['RawFinancialInput']['properties']['provider']['enum']=['WECHAT','ALIPAY','BANK','UNIONPAY','CASH','OTHER']
schemas['RawFinancialInput']['properties']['capture_source']['enum']=['CSV_IMPORT','MANUAL','AGENT','SYSTEM']
schemas['FinancialReview']['properties']['action']['enum']=['MATCH','CREATE_NEW','IGNORE']
schemas['FinancialReview']['properties']['direction']['description']='EXPENSE, INCOME, REFUND, or TRANSFER_IN/TRANSFER_OUT for MATCH only; transfer legs are never created independently.'
schemas['TransactionAnnotation']['properties']['annotation_status']['enum']=['AUTO_ANNOTATED','REVIEW_NEEDED','CONFIRMED']
schemas['FinancialSettings']['properties']['auto_commit_threshold'].update(minimum=0.95,maximum=1)
for path,item in doc['paths'].items():
    if '/financial/' in path:
        for method,spec in item.items():
            if not isinstance(spec,dict):continue
            if path.endswith('/review'):spec['responses']={'200':{'description':'Reviewed evidence; raw text unchanged'}}
            if method=='get' and path.endswith('/transactions'):
                spec['parameters']=[{'name':name,'in':'query','schema':{'type':typ}} for name,typ in [('occurred_at','string'),('window_minutes','integer'),('unannotated_only','boolean'),('account_id','string'),('period','string'),('from','string'),('to','string'),('merchant','string'),('amount','number'),('annotation_status','string'),('limit','integer'),('offset','integer')]]

doc['paths']['/api/v1/money/import/preview']['post']['description']='Runs the same reconciliation as import inside a rolled-back transaction. Imported, duplicate and review counts describe the current snapshot; no generated IDs or state are persisted.'
doc['paths']['/api/v1/financial/transactions']['get']['description']='When occurred_at is provided, match_status is NO_MATCH, CANDIDATE_FOUND or REVIEW_NEEDED. Ambiguity is evaluated independently of requested pagination. A single candidate is not automatic authorization to annotate or create money.'
# Responses flatten embedded Go DTOs; closed request schemas cannot be reused via allOf.
raw_properties=dict(schemas['RawFinancialInput']['properties'])
raw_properties.update({'id':{'type':'string'},'status':{'type':'string'},'fingerprint':{'type':'string'},'transaction_id':{'type':'string'},'decision':{'type':'string'},'candidates':{'type':'array','items':{'type':'string'}},'confidence':{'type':'number','minimum':0,'maximum':1}})
schemas['RawFinancialEvent']={'type':'object','additionalProperties':False,'properties':raw_properties}
# Native evidence identities are server-owned, never client-supplied write fields.
for name in ['evidence_key','import_batch_id','row_number']:
    schemas['RawFinancialInput']['properties'].pop(name,None)
    schemas['RawFinancialEvent']['properties'][name]['readOnly']=True
for name in ['RawFinancialInput','RawFinancialEvent']:
    schemas[name]['properties']['time_precision']['enum']=['','DAY','SECOND']
for parameter in doc['paths']['/api/v1/financial/transactions']['get']['parameters']:
    if parameter['name']=='window_minutes':parameter['schema'].update(default=30,minimum=1,maximum=43200)
schemas['MoneyImportRow']['properties'].update({'raw_record':{'type':'string','description':'Complete original CSV record, including quoted multiline fields.'},'raw_payload':{'type':'string','description':'JSON object containing original header and parsed field values.'}})
schemas['MoneyImportResult']['properties'].update({'needs_review':{'type':'integer','minimum':0},'raw_events':{'type':'array','items':{'$ref':'#/components/schemas/RawFinancialEvent'}},'skipped_rows':{'type':'array','items':{'$ref':'#/components/schemas/MoneyImportSkipped'}}})
doc['info']['version']='0.4.0'
p.write_text(json.dumps(doc,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
