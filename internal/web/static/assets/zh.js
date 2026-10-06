"use strict";

// Translate built-in display values without rewriting stored inventory values.
window.WubuZh = (() => {
  const categories = { pantry: "食品与食材", drinks: "饮品", cleaning: "清洁用品", bathroom: "洗浴与护理", laundry: "洗衣用品", household: "家居用品", health: "健康用品", pets: "宠物用品", baby: "婴幼儿用品", other: "其他", food: "饮食", transport: "交通", maintenance: "维护" };
  const units = { pcs: "个", packs: "包", boxes: "盒", bags: "袋", bottles: "瓶", cans: "罐", jars: "罐", rolls: "卷", tubes: "支", tablets: "片", g: "克", kg: "千克", ml: "毫升", l: "升", crates: "箱", cups: "杯", sachets: "小袋" };
  const locations = { Pantry: "食品储藏区", Kitchen: "厨房", Fridge: "冰箱冷藏室", Freezer: "冰箱冷冻室", Bathroom: "浴室", "Laundry room": "洗衣间", "Utility closet": "储物柜", Basement: "地下室", Garage: "车库", Bedroom: "卧室", Office: "书房" };
  const intents = { increase: "增加", decrease: "减少", create: "新增" };
  const payments = { alipay: "支付宝", wechat: "微信支付", cash: "现金", card: "银行卡", bank_transfer: "银行转账" };
  const fields = { name: "名称", category: "分类", icon: "图标", unit: "单位", location: "存放位置", notes: "备注", quantity: "数量", unit_price_cents: "单价", low_stock_threshold: "补货阈值", expiry_date: "到期日期", auto_consume_at: "自动归零时间", text: "文字说明", files: "文件", source: "录入来源", status: "状态", workflow_state: "处理状态", search: "搜索内容", limit: "每页条数", offset: "起始位置", from: "开始时间", to: "结束时间", hide_out: "显示已用完物品" };
  const codes = { validation_error: "请检查填写内容。", invalid_request: "请求内容无效，请检查后重试。", invalid_json: "提交的数据格式无效。", not_found: "记录不存在或已删除，请刷新页面。", negative_quantity: "库存数量不能小于零。", ledger_conflict: "记录与当前库存冲突，请刷新并核对后重试。", idempotency_conflict: "重复提交的内容不一致，请刷新后重试。", request_too_large: "上传内容超过大小限制。", method_not_allowed: "不支持此操作。", internal_error: "服务器发生错误，请稍后重试。", ai_unavailable: "本地 AI 暂不可用，请检查配置或稍后重试。", ai_invalid_response: "本地 AI 返回的分析结果无效，请重试或手动审核。", ai_analysis_failed: "本地 AI 分析失败，请稍后重试。", analysis_in_progress: "此条内容正在分析，请稍候。", transcription_unavailable: "本地语音转写暂不可用，请稍后重试。", media_processing_failed: "无法处理上传的文件，请检查文件后重试。", media_analysis_failed: "本地媒体处理失败，请稍后重试。", review_required: "请修正建议后再应用。" };
  const reasons = {
    "Voice transcription is unavailable. Try again when local transcription is running.": "语音转写暂不可用，请在本地转写服务运行后重试。",
    "The uploaded file could not be prepared for local AI.": "无法为本地 AI 处理上传的文件。",
    "Local AI is unavailable. Try the batch again later.": "本地 AI 暂不可用，请稍后重新分析本批次。",
    "Local AI returned an unusable result. Try again or review manually.": "本地 AI 返回的结果无效，请重试或手动审核。",
    "Analysis could not be completed. Try again or discard this entry.": "无法完成分析，请重试或丢弃此条内容。",
    "The change would reduce inventory below zero. Correct the amount before applying.": "变更会使库存小于零，请修正数量后再应用。",
    "The suggested unit conflicts with the existing inventory item. Correct it before applying.": "建议的单位与现有物品不一致，请修正后再应用。",
    "This decrease refers to an item that is not in inventory. Review the item and intent.": "要减少的物品不在库存中，请核对物品和操作。",
    "The suggested category is missing. Choose a category before applying.": "建议缺少分类，请选择分类后再应用。",
    "The suggested changes could not be applied. Correct them and try again.": "无法应用建议的变更，请修正后重试。"
  };
  function fieldMessage(field, message) {
    if (/[\u3400-\u9fff]/.test(message || "")) return message;
    const label = fields[field] || "填写内容";
    const length = message?.match(/at most (\d+) characters/);
    if (length) return `${label}最多 ${length[1]} 个字符。`;
    const range = message?.match(/between (\d+) and (\d+)/);
    if (range) return `${label}应在 ${range[1]} 至 ${range[2]} 之间${field === "unit_price_cents" ? "（单位：分）" : ""}。`;
    if (/required/i.test(message || "")) return `请填写${label}。`;
    if (/YYYY-MM-DD/.test(message || "")) return `${label}请使用“年-月-日”格式。`;
    if (/RFC.?3339|timezone/.test(message || "")) return `${label}请使用包含时区的完整日期时间。`;
    if (field === "files") return "请检查文件数量、大小和格式；支持图片、PDF、音频和视频。";
    return `请检查${label}是否有效。`;
  }
  const validationRules = [
    ["money needs at most two decimals", "金额最多保留两位小数，且应在 0 至 100 亿之间。"],
    ["money must be a number", "金额必须是数字。"], ["invalid amount", "请填写有效的金额。"],
    ["quantity needs 3 decimals", "数量必须大于零，最多保留三位小数。"],
    ["durable items are individual instances", "耐用品每件单独一行，数量应为 1。"],
    ["purchase needs 1 to 500 items", "每笔购买需包含 1 至 500 件商品。"],
    ["text too long", "文字内容超过长度限制，请缩短后重试。"],
    ["supports CNY only", "当前仅支持人民币。"],
    ["insufficient", "剩余库存不足，请检查数量。"], ["negative", "库存数量不能小于零。"]
  ];
  function error(detail, status) {
    if (detail instanceof Error && detail.detail) return error(detail.detail, detail.status);
    const message = typeof detail === "string" ? detail : detail?.message || "";
    if (/[\u3400-\u9fff]/.test(message)) return message.replace(/^(invalid ledger input|ledger conflict):\s*/, "");
    if (reasons[message]) return reasons[message];
    if (detail?.fields && Object.keys(detail.fields).length) {
      return Object.entries(detail.fields).map(([field, value]) => fieldMessage(field, value)).join(" ");
    }
    const rule = validationRules.find(([pattern]) => message.includes(pattern));
    if (rule) return rule[1];
    return codes[detail?.code] || (status ? `请求失败（${status}），请稍后重试。` : "操作未完成，请检查连接后重试。");
  }
  function setEditable(input, value, kind) {
    input.dataset.original = value || "";
    input.dataset.display = (kind === "unit" ? units[value] || units[(value || "").toLowerCase()] : locations[value]) || value || "";
    input.value = input.dataset.display;
  }
  function originalValue(input) {
    return input.value === input.dataset.display ? input.dataset.original : input.value;
  }
  return Object.freeze({
    product: item => item ? [item.name, item.spec].filter(Boolean).join(" ") : "",
    commonUnits: Object.freeze(["瓶", "罐", "升", "毫升", "个", "包", "盒", "袋", "箱", "杯", "支", "卷", "片", "克", "千克", "件", "套", "份"]),
    category: value => categories[value] || value,
    unit: value => units[value] || units[(value || "").toLowerCase()] || value,
    location: value => locations[value] || value,
    intent: value => intents[value] || value,
    payment: value => payments[(value || "").toLowerCase()] || value,
    error, fieldMessage, setEditable, originalValue,
    icons: {"inventory_2": "库存", "grocery": "食品", "nutrition": "水果蔬菜", "bakery_dining": "面包", "egg": "鸡蛋", "rice_bowl": "米饭", "soup_kitchen": "汤", "cookie": "饼干", "icecream": "冰淇淋", "restaurant": "餐饮", "flatware": "餐具", "food_bank": "食品储备", "local_pizza": "披萨", "lunch_dining": "汉堡", "dinner_dining": "晚餐", "breakfast_dining": "早餐", "ramen_dining": "面条", "set_meal": "鱼肉", "takeout_dining": "外卖", "kebab_dining": "烤肉", "fastfood": "快餐", "cake": "蛋糕", "coffee": "咖啡", "coffee_maker": "咖啡机", "local_drink": "饮料", "water_bottle": "水瓶", "liquor": "酒", "wine_bar": "葡萄酒", "local_cafe": "茶杯", "emoji_food_beverage": "热饮", "kitchen": "冰箱", "microwave": "微波炉", "oven": "烤箱", "skillet": "煎锅", "skillet_cooktop": "灶台", "blender": "搅拌机", "countertops": "厨房台面", "dishwasher": "洗碗机", "stockpot": "汤锅", "kettle": "水壶", "cleaning_services": "清洁用品", "cleaning": "清洁布", "cleaning_bucket": "水桶", "mop": "拖把", "vacuum": "吸尘器", "soap": "肥皂", "sanitizer": "消毒液", "wash": "洗发水", "air_freshener": "空气清新剂", "recycling": "回收", "compost": "堆肥", "delete": "垃圾", "bathroom": "浴室", "bathtub": "浴缸", "shower": "淋浴", "wc": "马桶", "clean_hands": "洗手", "dentistry": "牙齿护理", "oral_disease": "口腔护理", "spa": "护肤", "laundry": "洗衣用品", "iron": "熨斗", "dry_cleaning": "干洗", "checkroom": "衣物", "medication": "药品", "medication_liquid": "药水", "pill": "药片", "vaccines": "疫苗", "medical_services": "医疗用品", "stethoscope": "听诊器", "healing": "创可贴", "thermometer": "温度计", "medical_mask": "口罩", "pets": "宠物", "pet_supplies": "宠物用品", "child_friendly": "婴儿车", "baby_changing_station": "换尿布", "stroller": "推车", "bedroom_baby": "婴儿房", "home": "家居", "box": "箱子", "package_2": "包裹", "battery_full": "电池", "lightbulb": "灯泡", "cable": "电线", "electrical_services": "电工用品", "power": "电源", "key": "钥匙", "lock": "锁", "chair": "椅子", "bed": "床", "table_restaurant": "餐桌", "umbrella": "雨伞", "hardware": "五金", "construction": "建筑工具", "tools_wrench": "扳手", "tools_power_drill": "电钻", "shelves": "置物架", "potted_plant": "盆栽", "yard": "庭院", "garden_cart": "园艺推车", "garage": "车库", "propane_tank": "燃气罐", "print": "打印机", "book": "书", "school": "学习用品", "description": "文件", "note_stack": "便签", "styler": "梳子", "inventory": "储物", "category": "分类", "water_drop": "水滴", "oil_barrel": "油桶", "fire_extinguisher": "灭火器", "light": "照明", "router": "路由器", "devices": "数码设备", "extension": "插排", "remote_gen": "遥控器", "blinds": "百叶窗", "curtains": "窗帘", "bedroom_parent": "卧室", "tv": "电视", "desktop_windows": "电脑", "phone_iphone": "手机", "watch": "手表", "headphones": "耳机", "photo_camera": "相机", "sports_soccer": "足球", "exercise": "健身", "toys": "玩具", "backpack": "背包", "shopping_bag": "购物袋"}
  });
})();

// Browser validation otherwise follows the browser's own language preference.
document.addEventListener("invalid", event => {
  const input = event.target;
  if (!input.validity || input.validity.customError) return;
  const validity = input.validity;
  const message = validity.valueMissing ? "请填写此项。"
    : validity.rangeUnderflow ? `数值不能小于 ${input.min}。`
    : validity.rangeOverflow ? `数值不能大于 ${input.max}。`
    : validity.tooLong ? `最多输入 ${input.maxLength} 个字符。`
    : validity.badInput || validity.stepMismatch ? "请输入符合要求的数值。"
    : "请检查填写内容的格式。";
  input.setCustomValidity(message);
}, true);
document.addEventListener("input", event => event.target.setCustomValidity?.(""));
document.addEventListener("change", event => event.target.setCustomValidity?.(""));
document.addEventListener("reset", event => {
  for (const input of event.target.elements || []) input.setCustomValidity?.("");
});
