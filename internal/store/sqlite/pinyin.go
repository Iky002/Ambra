package sqlite

import (
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

// tradToSimpMap maps high-frequency Traditional Chinese characters commonly
// encountered in movie/TV/anime titles, cast names, and user search queries
// to their Simplified Chinese equivalents.
var tradToSimpMap = map[rune]rune{
	// Core film/TV & prompt-specified characters
	'國': '国', '電': '电', '體': '体', '愛': '爱', '無': '无',
	'進': '进', '擊': '击', '夢': '梦', '際': '际', '盜': '盗',
	'傳': '传', '別': '别', '奧': '奥', '殺': '杀', '銀': '银',
	'龍': '龙', '風': '风', '華': '华', '時': '时', '間': '间',
	'戰': '战', '爭': '争', '記': '记', '錄': '录', '劇': '剧',
	'終': '终', '極': '极', '鐵': '铁', '俠': '侠', '隱': '隐',
	'驚': '惊', '萬': '万', '與': '与', '為': '为', '從': '从',
	'來': '来', '開': '开', '關': '关', '門': '门', '長': '长',
	'馬': '马', '鳥': '鸟', '魚': '鱼', '車': '车', '東': '东',
	'見': '见', '貝': '贝', '頁': '页', '飛': '飞', '書': '书',
	'語': '语', '話': '话', '請': '请', '讓': '让', '誰': '谁',
	'變': '变', '這': '这', '還': '还', '過': '过', '運': '运',
	'遠': '远', '選': '选', '邊': '边', '達': '达', '連': '连',
	'離': '离', '難': '难', '雲': '云', '頭': '头', '題': '题',
	'類': '类', '顯': '显', '點': '点', '齊': '齐',

	// Common movie, series, anime, and metadata vocabulary
	'復': '复', '聯': '联', '駭': '骇', '贖': '赎', '彈': '弹',
	'遊': '游', '尋': '寻', '滅': '灭', '術': '术', '迴': '回',
	'權': '权', '戲': '戏', '絕': '绝', '師': '师', '慶': '庆',
	'餘': '余', '紅': '红', '動': '动', '滿': '满', '羅': '罗',
	'鈴': '铃', '氣': '气', '數': '数', '碼': '码', '寶': '宝',
	'聖': '圣', '鬥': '斗', '偵': '侦', '賊': '贼', '籃': '篮',
	'鋼': '钢', '鍊': '炼', '紀': '纪', '綜': '综', '藝': '艺',
	'懸': '悬', '歷': '历', '險': '险', '災': '灾', '樂': '乐',
	'畫': '画', '導': '导', '編': '编', '員': '员', '產': '产',
	'歐': '欧', '韓': '韩', '臺': '台', '灣': '湾', '粵': '粤',
	'簡': '简', '雙': '双', '藍': '蓝', '場': '场', '實': '实',

	// Extended high-frequency Traditional -> Simplified mappings (200+ total)
	'亞': '亚', '親': '亲', '億': '亿', '僅': '仅', '儀': '仪',
	'優': '优', '會': '会', '偉': '伟', '傷': '伤', '倫': '伦',
	'偽': '伪', '傑': '杰', '備': '备', '傢': '家', '儲': '储',
	'兒': '儿', '黨': '党', '蘭': '兰', '興': '兴', '養': '养',
	'內': '内', '冊': '册', '軍': '军', '農': '农', '衝': '冲',
	'決': '决', '況': '况', '凍': '冻', '淨': '净', '涼': '凉',
	'準': '准', '幾': '几', '鳳': '凤', '凱': '凯', '憑': '凭',
	'劃': '划', '劉': '刘', '則': '则', '剛': '刚', '創': '创',
	'刪': '删', '劍': '剑', '劑': '剂', '辦': '办', '務': '务',
	'勵': '励', '勞': '劳', '勢': '势', '勳': '勋', '區': '区',
	'醫': '医', '協': '协', '單': '单', '賣': '卖', '衛': '卫',
	'卻': '却', '廠': '厂', '廳': '厅', '厲': '厉', '壓': '压',
	'厭': '厌', '參': '参', '發': '发', '號': '号', '嘆': '叹',
	'聽': '听', '啟': '启', '吳': '吴', '響': '响', '喚': '唤',
	'喬': '乔', '營': '营', '嘗': '尝', '園': '园', '圍': '围',
	'圖': '图', '團': '团', '圓': '圆', '執': '执', '堅': '坚',
	'報': '报', '塊': '块', '塗': '涂', '塵': '尘', '墮': '堕',
	'壞': '坏', '壯': '壮', '聲': '声', '處': '处', '夠': '够',
	'奪': '夺', '奮': '奋', '獎': '奖', '婦': '妇', '媽': '妈',
	'娛': '娱', '嬰': '婴', '孫': '孙', '學': '学', '寧': '宁',
	'審': '审', '寫': '写', '寬': '宽', '寵': '宠', '將': '将',
	'專': '专', '對': '对', '爾': '尔', '層': '层', '屬': '属',
	'岡': '冈', '島': '岛', '嶽': '岳', '嶺': '岭', '峽': '峡',
	'嵐': '岚', '巔': '巅', '帥': '帅', '帳': '帐', '帶': '带',
	'幫': '帮', '幣': '币', '幹': '干', '並': '并', '廣': '广',
	'莊': '庄', '應': '应', '庫': '库', '廟': '庙', '廢': '废',
	'張': '张', '強': '强', '歸': '归', '當': '当', '徑': '径',
	'徵': '征', '徹': '彻', '憶': '忆', '態': '态', '懷': '怀',
	'憐': '怜', '總': '总', '戀': '恋', '恆': '恒', '惡': '恶',
	'惱': '恼', '慘': '惨', '憤': '愤', '願': '愿', '憂': '忧',
	'憲': '宪', '戶': '户', '擴': '扩', '掃': '扫', '揚': '扬',
	'換': '换', '揮': '挥', '損': '损', '搖': '摇', '搶': '抢',
	'護': '护', '擔': '担', '據': '据', '擠': '挤', '擬': '拟',
	'攏': '拢', '攔': '拦', '擺': '摆', '擾': '扰', '攝': '摄',
	'敵': '敌', '斷': '断', '舊': '旧', '曉': '晓', '暫': '暂',
	'機': '机', '雜': '杂', '條': '条', '楊': '杨', '構': '构',
	'槍': '枪', '楓': '枫', '業': '业', '標': '标', '樣': '样',
	'橋': '桥', '檢': '检', '檔': '档', '櫻': '樱', '歡': '欢',
	'歲': '岁', '殘': '残', '毀': '毁', '畢': '毕', '漢': '汉',
	'湯': '汤', '溝': '沟', '溫': '温', '測': '测', '湧': '涌',
	'滾': '滚', '漁': '渔', '漲': '涨', '漸': '渐', '潔': '洁',
	'潛': '潜', '潤': '润', '澤': '泽', '濃': '浓', '濟': '济',
	'濤': '涛', '濫': '滥', '濱': '滨', '靈': '灵', '煉': '炼',
	'熱': '热', '燈': '灯', '燒': '烧', '燦': '灿', '燭': '烛',
	'爺': '爷', '牆': '墙', '牘': '牍', '狀': '状', '獨': '独',
	'獄': '狱', '獅': '狮', '獸': '兽', '獻': '献', '瑪': '玛',
	'現': '现', '環': '环', '瓊': '琼', '異': '异', '瘋': '疯',
	'皺': '皱', '蓋': '盖', '盤': '盘', '盧': '卢', '眾': '众',
	'睜': '睁', '瞞': '瞒', '瞭': '了', '礦': '矿', '確': '确',
	'礙': '碍', '禮': '礼', '禍': '祸', '禪': '禅', '種': '种',
	'稱': '称', '積': '积', '穩': '稳', '窮': '穷', '竊': '窃',
	'競': '竞', '筆': '笔', '築': '筑', '範': '范', '節': '节',
	'簽': '签', '籌': '筹', '籠': '笼', '糧': '粮', '約': '约',
	'級': '级', '納': '纳', '紛': '纷', '紙': '纸', '紋': '纹',
	'紐': '纽', '純': '纯', '紗': '纱', '組': '组', '細': '细',
	'結': '结', '給': '给', '統': '统', '絲': '丝', '經': '经',
	'綠': '绿', '維': '维', '綱': '纲', '網': '网', '緊': '紧',
	'線': '线', '練': '练', '緣': '缘', '縫': '缝', '縮': '缩',
	'縱': '纵', '績': '绩', '織': '织', '繞': '绕', '繪': '绘',
	'繼': '继', '續': '续', '罰': '罚', '義': '义', '習': '习',
	'聞': '闻', '聰': '聪', '職': '职', '肅': '肃', '勝': '胜',
	'脅': '胁', '脈': '脉', '脫': '脱', '腦': '脑', '腳': '脚',
	'臉': '脸', '騰': '腾', '臟': '脏', '臨': '临', '舉': '举',
	'艦': '舰', '艙': '舱', '艱': '艰', '葉': '叶', '著': '着',
	'蔥': '葱', '蔣': '蒋', '蒼': '苍', '蓮': '莲', '薩': '萨',
	'藥': '药', '蘇': '苏', '虛': '虚', '虧': '亏', '蟲': '虫',
	'蝕': '蚀', '螢': '萤', '補': '补', '裝': '装', '裡': '里',
	'製': '制', '複': '复', '襲': '袭', '規': '规', '視': '视',
	'覺': '觉', '覽': '览', '觀': '观', '觸': '触', '計': '计',
	'訂': '订', '訊': '讯', '討': '讨', '訓': '训', '設': '设',
	'許': '许', '訴': '诉', '診': '诊', '評': '评', '詞': '词',
	'試': '试', '詩': '诗', '該': '该', '詳': '详', '誤': '误',
	'說': '说', '課': '课', '調': '调', '談': '谈', '論': '论',
	'諸': '诸', '諾': '诺', '謀': '谋', '講': '讲', '謝': '谢',
	'證': '证', '識': '识', '譜': '谱', '譯': '译', '議': '议',
	'讀': '读', '讚': '赞', '豐': '丰', '負': '负', '財': '财',
	'責': '责', '賢': '贤', '敗': '败', '貨': '货', '質': '质',
	'貪': '贪', '貧': '贫', '購': '购', '貫': '贯', '貴': '贵',
	'買': '买', '費': '费', '貼': '贴', '賀': '贺', '資': '资',
	'賓': '宾', '賜': '赐', '賞': '赏', '賠': '赔', '賦': '赋',
	'賭': '赌', '賴': '赖', '賺': '赚', '賽': '赛', '贊': '赞',
	'贈': '赠', '贏': '赢', '趕': '赶', '趙': '赵', '趨': '趋',
	'躍': '跃', '蹤': '踪', '軌': '轨', '軒': '轩', '軟': '软',
	'較': '较', '載': '载', '輔': '辅', '輕': '轻', '輛': '辆',
	'輝': '辉', '輩': '辈', '輪': '轮', '輯': '辑', '輸': '输',
	'轉': '转', '轟': '轰', '辭': '辞', '辯': '辩', '違': '违',
	'週': '周', '適': '适', '遲': '迟', '遺': '遗', '邁': '迈',
	'鄧': '邓', '郵': '邮', '鄰': '邻', '鄭': '郑', '鄉': '乡',
	'釋': '释', '針': '针', '釣': '钓', '銃': '铳', '銅': '铜',
	'銘': '铭', '銳': '锐', '銷': '销', '鋒': '锋', '錯': '错',
	'錢': '钱', '錦': '锦', '錫': '锡', '鍵': '键', '鍾': '钟',
	'鎖': '锁', '鎮': '镇', '鏡': '镜', '鐘': '钟', '鑑': '鉴',
	'鑽': '钻', '閃': '闪', '閉': '闭', '問': '问', '閒': '闲',
	'悶': '闷', '閣': '阁', '閱': '阅', '隊': '队', '階': '阶',
	'陣': '阵', '陰': '阴', '陳': '陈', '陸': '陆', '陽': '阳',
	'隨': '随', '雞': '鸡', '霧': '雾', '靜': '静', '頂': '顶',
	'項': '项', '順': '顺', '須': '须', '預': '预', '頑': '顽',
	'頓': '顿', '頗': '颇', '領': '领', '頻': '频', '顆': '颗',
	'額': '额', '顏': '颜', '顧': '顾', '飄': '飘', '飯': '饭',
	'飲': '饮', '飾': '饰', '飽': '饱', '館': '馆', '馮': '冯',
	'馳': '驰', '驅': '驱', '駁': '驳', '駐': '驻', '駕': '驾',
	'驗': '验', '騎': '骑', '騙': '骗', '騷': '骚', '驟': '骤',
	'髮': '发', '鬧': '闹', '鬱': '郁', '魯': '鲁', '鮮': '鲜',
	'鯨': '鲸', '鳴': '鸣', '鴨': '鸭', '鴻': '鸿', '鵝': '鹅',
	'鶴': '鹤', '鷹': '鹰', '鹽': '盐', '麗': '丽', '麥': '麦',
	'黃': '黄', '齋': '斋', '齒': '齿', '齡': '龄', '龜': '龟',
	'榮': '荣', '諫': '谏', '韋': '韦', '鄒': '邹', '龐': '庞',
	'龔': '龚', '譚': '谭', '賈': '贾', '閻': '阎', '嚴': '严',
	'宮': '宫', '駿': '骏', '誠': '诚',
}

// TradToSimp normalizes Traditional Chinese characters in s to Simplified Chinese
// while preserving all other characters untouched.
func TradToSimp(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if simp, ok := tradToSimpMap[r]; ok {
			b.WriteRune(simp)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ToPinyin converts Chinese text (Traditional or Simplified, including mixed
// Chinese/English/digit strings) into:
//   - full: space-separated syllables plus joined full pinyin for each CJK run,
//     retaining English words and digits (e.g. "流浪地球" -> "liu lang di qiu liulangdiqiu")
//   - initials: contiguous initial letters for each CJK run plus English words/digits
//     (e.g. "流浪地球" -> "lldq", "三体 3 Body" -> "st 3 body")
func ToPinyin(s string) (full string, initials string) {
	s = strings.TrimSpace(TradToSimp(s))
	if s == "" {
		return "", ""
	}

	args := pinyin.NewArgs()
	args.Style = pinyin.Normal

	var (
		fullTokens []string
		initTokens []string
		fullSeen   = make(map[string]struct{})
		initSeen   = make(map[string]struct{})
	)
	addFull := func(tok string) {
		if _, ok := fullSeen[tok]; ok {
			return
		}
		fullSeen[tok] = struct{}{}
		fullTokens = append(fullTokens, tok)
	}
	addInit := func(tok string) {
		if _, ok := initSeen[tok]; ok {
			return
		}
		initSeen[tok] = struct{}{}
		initTokens = append(initTokens, tok)
	}

	// Split into whitespace/punctuation-delimited segments so mixed tokens
	// like "流浪地球2" emit both separated ("lldq", "2") and joined ("lldq2") forms.
	var segments [][]rune
	var cur []rune
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if len(cur) > 0 {
				segments = append(segments, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		segments = append(segments, cur)
	}

	for _, seg := range segments {
		var segFullJoined strings.Builder
		var segInitJoined strings.Builder
		hasCJK := false
		hasNonCJK := false

		i := 0
		for i < len(seg) {
			if isCJK(seg[i]) {
				j := i
				for j < len(seg) && isCJK(seg[j]) {
					j++
				}
				var runFull strings.Builder
				var runInit strings.Builder
				sylCount := 0
				for _, r := range seg[i:j] {
					pys := pinyin.SinglePinyin(r, args)
					if len(pys) == 0 {
						continue
					}
					py := strings.ToLower(pys[0])
					if py == "" {
						continue
					}
					addFull(py)
					runFull.WriteString(py)
					runInit.WriteByte(py[0])
					sylCount++
				}
				if sylCount > 0 {
					hasCJK = true
					joined := runFull.String()
					if sylCount > 1 {
						addFull(joined)
					}
					initStr := runInit.String()
					addInit(initStr)
					segFullJoined.WriteString(joined)
					segInitJoined.WriteString(initStr)
				}
				i = j
			} else {
				j := i
				for j < len(seg) && !isCJK(seg[j]) {
					j++
				}
				word := strings.ToLower(string(seg[i:j]))
				hasNonCJK = true
				addFull(word)
				addInit(word)
				segFullJoined.WriteString(word)
				segInitJoined.WriteString(word)
				i = j
			}
		}

		if hasCJK && hasNonCJK {
			addFull(segFullJoined.String())
			addInit(segInitJoined.String())
		}
	}

	return strings.Join(fullTokens, " "), strings.Join(initTokens, " ")
}

// tokenizeForFTSSQLite generates CJK unigrams/bigrams and lowercased Latin/digit
// tokens when a caller leaves titleTokens empty in UpsertSearchFTS.
func tokenizeForFTSSQLite(text string) string {
	var tokens []string
	seen := make(map[string]struct{})
	add := func(t string) {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		tokens = append(tokens, t)
	}

	runes := []rune(text)
	i := 0
	for i < len(runes) {
		r := runes[i]
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			i++
			continue
		}
		if isCJK(r) {
			j := i
			for j < len(runes) && isCJK(runes[j]) {
				j++
			}
			cjk := runes[i:j]
			add(string(cjk))
			for k := 0; k < len(cjk); k++ {
				add(string(cjk[k]))
				if k+1 < len(cjk) {
					add(string(cjk[k : k+2]))
				}
			}
			i = j
		} else {
			j := i
			for j < len(runes) && !unicode.IsSpace(runes[j]) && !unicode.IsPunct(runes[j]) && !unicode.IsSymbol(runes[j]) && !isCJK(runes[j]) {
				j++
			}
			add(string(runes[i:j]))
			i = j
		}
	}
	return strings.Join(tokens, " ")
}

// PinyinSortKey returns a lowercased ASCII-friendly sort key for A–Z indexing and
// alphabetical ordering. Pure ASCII strings are lowercased as-is; CJK characters
// (Traditional or Simplified) are converted to their tone-less pinyin syllables
// (e.g. "肖申克的救赎" -> "xiaoshenkedejiushu", "《三体》" -> "santi").
func PinyinSortKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	isASCII := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			isASCII = false
			break
		}
	}
	if isASCII {
		return strings.ToLower(s)
	}

	s = TradToSimp(s)
	// Strip leading CJK brackets/quotes (e.g. 《三体》 -> 三体) while preserving ASCII symbols/digits for '#'.
	s = strings.TrimLeft(s, "《〈「『【（“‘")
	if s == "" {
		return ""
	}

	args := pinyin.NewArgs()
	args.Style = pinyin.Normal

	var b strings.Builder
	b.Grow(len(s) * 2)
	for _, r := range s {
		if isCJK(r) {
			if pys := pinyin.SinglePinyin(r, args); len(pys) > 0 && pys[0] != "" {
				b.WriteString(strings.ToLower(pys[0]))
				continue
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
