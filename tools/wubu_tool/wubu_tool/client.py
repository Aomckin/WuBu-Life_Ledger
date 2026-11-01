"""Small synchronous standard-library client; no implicit write retries."""
import json
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode, urlsplit
from urllib.request import Request, urlopen

ROUTES = {
    "record_purchase": ("POST", "/purchases/record"),
    "validate_purchase": ("POST", "/purchases/validate"),
    "record_purchase_event": ("POST", "/purchases"),
    "amend_purchase_event": ("PUT", "/purchases/{purchase_id}"),
    "delete_purchase_event": ("DELETE", "/purchases/{purchase_id}"),
    "consume_item": ("POST", "/items/{item_id}/consume"),
    "adjust_inventory": ("POST", "/items/{item_id}/adjust"),
    "open_item": ("POST", "/items/{item_id}/open"),
    "finish_item": ("POST", "/items/{item_id}/finish"),
    "discard_item": ("POST", "/items/{item_id}/discard"),
    "add_maintenance_event": ("POST", "/items/{item_id}/maintenance"),
    "add_product_alias": ("POST", "/items/{item_id}/aliases"),
    "update_durable_details": ("PATCH", "/items/{item_id}/durable"),
    "search_items": ("GET", "/items"),
    "resolve_item": ("GET", "/items/resolve"),
    "get_item": ("GET", "/items/{item_id}"),
    "get_inventory": ("GET", "/inventory"),
    "get_purchase_history": ("GET", "/items/{item_id}/purchases"),
    "get_price_history": ("GET", "/items/{item_id}/prices"),
    "get_maintenance_history": ("GET", "/items/{item_id}/maintenance"),
    "get_inventory_history": ("GET", "/items/{item_id}/inventory-events"),
    "get_recent_purchases": ("GET", "/purchases"),
    "get_purchase_event": ("GET", "/purchases/{purchase_id}"),
    "get_expenses": ("GET", "/expenses"),
    "get_ledger_summary": ("GET", "/ledger/summary"),
    "get_durable_details": ("GET", "/items/{item_id}/durable"),
    "get_product_aliases": ("GET", "/items/{item_id}/aliases"),
}
ROUTES.update({
 "top_up_account":("POST","/money/top-ups"),"record_bonus":("POST","/money/bonuses"),
 "update_account":("PATCH","/accounts/{account_id}"),"get_account_statistics":("GET","/accounts/{account_id}/statistics"),
 "create_account":("POST","/accounts"), "set_default_payment_account":("PATCH","/money/settings"),
 "record_income":("POST","/money/income"),"record_expense":("POST","/money/expenses"),
 "transfer_money":("POST","/money/transfers"),"record_refund":("POST","/money/refunds"),
 "adjust_balance":("POST","/money/adjustments"),"reconcile_account":("POST","/money/reconcile"),
 "get_accounts":("GET","/accounts"),"get_balance":("GET","/money/balance"),
 "get_account_balance":("GET","/money/balance"),"get_total_balance":("GET","/money/balance"),
 "get_expense_summary":("GET","/money/expense-summary"),"get_income_summary":("GET","/money/income-summary"),
 "get_category_summary":("GET","/money/category-summary"),"get_merchant_summary":("GET","/money/merchant-summary"),
 "get_recent_transactions":("GET","/money/transactions"),"add_maintenance":("POST","/items/{item_id}/maintenance"),
 "sell_item":("POST","/items/{item_id}/sell"),"retire_item":("POST","/items/{item_id}/retire"),
 "move_item":("POST","/items/{item_id}/move"),"set_durable_status":("POST","/items/{item_id}/status"),
 "get_durable_items":("GET","/durables"),"get_item_total_cost":("GET","/items/{item_id}/total-cost"),
})
ROUTES.update({'create_planned_cashflow': ('POST', '/planned-cashflows'), 'update_planned_cashflow': ('PUT', '/planned-cashflows/{cashflow_id}'), 'cancel_planned_cashflow': ('POST', '/planned-cashflows/{cashflow_id}/cancel'), 'skip_planned_cashflow': ('POST', '/planned-cashflows/{cashflow_id}/skip'), 'complete_planned_cashflow': ('POST', '/planned-cashflows/{cashflow_id}/complete'), 'get_upcoming_cashflows': ('GET', '/planned-cashflows'), 'get_living_runway': ('GET', '/runway'), 'get_runway_settings': ('GET', '/runway/settings'), 'set_runway_settings': ('PATCH', '/runway/settings'), 'get_spending_by_nature': ('GET', '/money/spending-by-nature'), 'update_expense_nature': ('PATCH', '/money/transactions/{transaction_id}/nature')})
ROUTES.update({'create_residence': ('POST', '/residences'), 'update_residence': ('PUT', '/residences/{residence_id}'), 'get_residences': ('GET', '/residences'), 'link_residence_cost': ('POST', '/residences/{residence_id}/cost-links'), 'get_residence_cost': ('GET', '/residences/{residence_id}/cost'), 'get_relocation_cost': ('GET', '/residences/{residence_id}/relocation-cost'), 'get_residence_durables': ('GET', '/residences/{residence_id}/durables'), 'record_deposit': ('POST', '/deposits'), 'return_deposit': ('POST', '/deposits/{claim_id}/return'), 'forfeit_deposit': ('POST', '/deposits/{claim_id}/forfeit'), 'get_deposits': ('GET', '/deposits'), 'create_receivable': ('POST', '/receivables'), 'settle_receivable': ('POST', '/receivables/{claim_id}/return'), 'cancel_receivable': ('POST', '/receivables/{claim_id}/forfeit'), 'get_receivables': ('GET', '/receivables'), 'get_consumption_forecast': ('GET', '/items/{item_id}/consumption-forecast'), 'get_items_running_low': ('GET', '/items-running-low')})
ROUTES.update({action+"_by_name": ("POST", "/items/"+action) for action in ("consume", "adjust", "open", "finish", "discard")})

class WubuError(Exception):
    def __init__(self, message, code="network_error", unknown_outcome=False, detail=None):
        super().__init__(message)
        self.code = code
        self.unknown_outcome = unknown_outcome
        self.detail = detail or {"code": code, "message": message, "retryable": True, "suggested_action": "retry_with_same_idempotency_key"}

class WubuClient:
    def __init__(self, base_url="http://127.0.0.1:8026", timeout=15):
        parsed = urlsplit(base_url)
        if parsed.scheme != "http" or parsed.hostname not in {"127.0.0.1", "localhost", "::1"} or parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in {"", "/"}:
            raise ValueError("物簿使用本机 HTTP 服务根地址")
        if not 0 < float(timeout) <= 120:
            raise ValueError("timeout must be between 0 and 120 seconds")
        self.base_url = base_url.rstrip("/") + "/api/v1"
        self.timeout = float(timeout)

    def call(self, operation, *, item_id=None, purchase_id=None, account_id=None, cashflow_id=None, transaction_id=None, residence_id=None, claim_id=None, payload=None, query=None, idempotency_key=None):
        if operation not in ROUTES:
            raise ValueError("unsupported operation")
        method, path = ROUTES[operation]
        for name, value in [("residence_id", residence_id), ("claim_id", claim_id), ("cashflow_id", cashflow_id), ("transaction_id", transaction_id), ("item_id", item_id), ("purchase_id", purchase_id), ("account_id", account_id)]:
            if "{" + name + "}" in path:
                if not value:
                    raise ValueError(name + " required")
                path = path.replace("{" + name + "}", quote(str(value), safe=""))
        write = method != "GET" and operation != "validate_purchase"
        if write and (not idempotency_key or len(idempotency_key) > 200 or idempotency_key.strip() != idempotency_key):
            raise ValueError("关键写入必须提供稳定的 idempotency_key；重试时使用原 key 和原 payload")
        if method == "GET" and payload is not None:
            raise ValueError("read operation cannot have payload")
        if write and query:
            raise ValueError("write operation cannot have query")
        if query:
            path += "?" + urlencode({k: str(v).lower() if isinstance(v, bool) else v for k, v in query.items()})
        headers = {"Accept": "application/json"}
        body = None
        if method != "GET":
            headers.update({"Content-Type": "application/json"})
            if idempotency_key: headers["Idempotency-Key"] = idempotency_key
            body = json.dumps(payload or {}, ensure_ascii=False, allow_nan=False).encode("utf-8")
        request = Request(self.base_url + path, data=body, headers=headers, method=method)
        try:
            with urlopen(request, timeout=self.timeout) as response:
                return json.load(response)
        except HTTPError as exc:
            try:
                detail = json.load(exc).get("error", {})
            except (ValueError, TypeError):
                detail = {}
            raise WubuError(detail.get("message", f"HTTP {exc.code}"), detail.get("code", "http_error"), write and exc.code >= 500, detail=detail) from exc
        except (URLError, TimeoutError, OSError, ValueError) as exc:
            raise WubuError("物簿响应未确认；如需重试必须保留原幂等键和参数。", unknown_outcome=write) from exc
