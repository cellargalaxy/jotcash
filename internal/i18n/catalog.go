package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	texttemplate "text/template"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	goi18ntpl "github.com/nicksnyder/go-i18n/v2/i18n/template"
	"golang.org/x/text/language"
)

// localesFS 是随包 embed 的语言文件目录（T37）。
//
// //go:embed 不能跨父目录、不允许 ..，这是 locales/ 必须物理位于本包目录之下的
// 唯一原因（分层 约定 4）。代价：加语种需重新构建二进制，已在包注释登记。
//
//go:embed locales
var localesFS embed.FS

const (
	// localesDir 是 embed 目录名。
	localesDir = "locales"

	// localeExt 是语言文件扩展名。只支持 JSON 一种格式：go-i18n 的 TOML 与 YAML
	// 解析器会各带一个第三方依赖进构建（实测 BurntSushi/toml 与 go.yaml.in/yaml
	// 在只注册 JSON 时**不进构建**），而多一种可选格式没有任何收益。
	localeExt = ".json"

	// DefaultLangKey 是默认语言键（终版 §四 1 User.界面语言「默认**中文**」）。
	//
	// 它同时是三处的兜底：Render 遇零值语言、Match 匹配不到、以及某语言缺某个
	// 键时的逐键回落。
	DefaultLangKey = "zh-CN"

	// displayNameKey 是语言文件中声明该语言**自称**的保留键。
	//
	// 放在语言文件里而不另设一套元数据机制，是为了让「加语种 = 加一个文件」
	// 真正成立：新语种的自称随它自己的文件一并给出，不必回改任何既有文件、
	// 也不必改 Go 代码（K-4）。
	displayNameKey = "i18n.display_name"

	// missingKeyOption 把「占位参数缺失」从静默渲染成 "<no value>" 改为报错。
	//
	// go-i18n 默认用 missingkey=default，实测会让「语言文件写了 {{.Actual}} 而
	// 调用方没传」渲染出「当前 <no value> 位」且 err 为 nil——这类缺陷一旦上线，
	// 只能靠用户截图发现。见包注释陷阱一。
	missingKeyOption = "missingkey=error"
)

// active 是当前生效的语言目录，在 init 中一次构建，此后只读。
//
// 收成**单个变量**而非一组零散的包级变量，是为了让所有对外函数都能以
// catalog 为接收者实现，从而使测试可以用 load 造出另一份 catalog（例如双语）
// 走**完全相同**的代码路径。本轮只有 zh-CN，若不这么做，「某语言缺某个键」
// 「整包下发按默认语言补全」这两条分支在包级状态下根本走不到——变异测试已证实
// 那样写出来的用例改坏代码也不会失败。
//
// 因为无任何写入口，并发读天然安全，不需要加锁——go-i18n 的 Bundle 与
// Localizer 在「装载完成后只读」的用法下并发安全（已在 -race 下实测
// 50 协程 × 50 轮取词）。
var active catalog

// catalog 是由语言文件派生出的全部只读状态。
//
// 把它抽成结构体、由纯函数 load 产出，是为了让 load 的各条自检分支**可被测试**：
// 直接在 init 里做校验的话，那些 panic 分支只有在语言文件被改坏时才会触发，
// 测试无法构造——一个从未被验证过的保险等于没有保险。这与 currency.buildTable
// 是同一个手法。
type catalog struct {
	// bundle 持有全部已装载语言的文案。
	bundle *goi18n.Bundle

	// localizers 是「语言键 → 预建 Localizer」。预建而非每次取词现建：
	// NewLocalizer 会解析语言字符串并构造 matcher，而取词是每个请求每条错误
	// 都要走的路径，没有理由每次重做。
	localizers map[string]*goi18n.Localizer

	// rawMessages 是「语言键 → (文案键 → 原始模板)」，供整包下发。
	rawMessages map[string]map[string]string

	// entries 是「语言键 → 条目」索引，ParseLang 的查询依据。
	entries map[string]*langEntry

	// allLangs 是全部已装载语言，按语言键字典序。
	allLangs []Lang

	// matchTags 与 matchLangs 是并行数组：matchTags 交给 language.NewMatcher，
	// matcher 返回的下标直接索引 matchLangs。
	//
	// 自持一份而非用 bundle.LanguageTags()，是因为后者**返回内部切片本身而非
	// 副本**（库缺陷，见包注释陷阱三）：一旦有人对它排序，bundle 内部 matcher
	// 的顺序即被改写，双语言下取词会完全错配且不报错。
	matchTags  []language.Tag
	matchLangs []Lang

	// matcher 由 matchTags 构建，供 Accept-Language 模糊匹配。
	matcher language.Matcher

	// defaultLang 是默认语言（DefaultLangKey 对应的 Lang）。
	defaultLang Lang
}

// init 装载语言文件。
//
// 自检失败一律 panic：非法 JSON、坏模板、缺默认语言、tag 不一致都是「语言文件
// 本身写错了」的情形，属编码错误而非运行期输入错误。让它在进程启动时当场炸掉，
// 远好过带着一份坏文案跑起来、直到某个用户碰到某条错误提示才暴露。
// 与 currency.init 对枚举表自检失败的处置一致。
func init() {
	c, err := load(localesFS, localesDir, DefaultLangKey)
	if err != nil {
		panic("i18n: " + err.Error())
	}
	active = c
}

// load 从文件系统装载语言文件并做五项自检：
// 目录可读且非空、文件名是合法语言标签、JSON 可解析、每条文案模板可编译、
// 默认语言存在。
//
// 另有一项针对第三方库的断言：**bundle 装载后的 tag 集合必须与语言文件集合
// 完全一致**。这不是多余的——go-i18n 的 NewBundle 默认 tag 若与文件 tag 不同
// （如 NewBundle(language.SimplifiedChinese) 配 zh-CN.json），bundle 会多出一个
// 「一条文案都没有」的空壳 tag，导致 Accept-Language 为 en-US / zh-TW / 空串时
// 取词全部失败，而登录页与 A-8 登录失败提示恰在这条路上（包注释陷阱二）。
//
// 返回 error 而非直接 panic，使各分支可被 TestLoadRejectsBadLocales 覆盖。
func load(fsys fs.FS, dir, defaultKey string) (catalog, error) {
	var c catalog

	names, err := localeFiles(fsys, dir)
	if err != nil {
		return catalog{}, err
	}

	// 默认语言的 tag 必须与文件 tag 一致，故先解析默认键再建 bundle。
	defaultTag, err := language.Parse(defaultKey)
	if err != nil {
		return catalog{}, fmt.Errorf("默认语言键 %q 不是合法的语言标签: %w", defaultKey, err)
	}

	c.bundle = goi18n.NewBundle(defaultTag)
	c.bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	c.entries = make(map[string]*langEntry, len(names))
	c.rawMessages = make(map[string]map[string]string, len(names))
	c.localizers = make(map[string]*goi18n.Localizer, len(names))

	fileTags := make(map[string]bool, len(names))
	for _, name := range names {
		key, msgs, err := loadFile(fsys, path.Join(dir, name), c.bundle)
		if err != nil {
			return catalog{}, err
		}
		if _, dup := c.entries[key]; dup {
			return catalog{}, fmt.Errorf("语言 %q 有多个语言文件", key)
		}
		c.entries[key] = &langEntry{tag: key, displayName: msgs[displayNameKey]}
		c.rawMessages[key] = msgs
		fileTags[key] = true
	}

	// 陷阱二的断言：bundle 的 tag 集合不得多出任何「空壳语言」。
	for _, tag := range c.bundle.LanguageTags() {
		if !fileTags[tag.String()] {
			return catalog{}, fmt.Errorf(
				"bundle 多出无文案的语言 %q（语言文件集合为 %v）："+
					"该空壳语言会让 Accept-Language 回落到它并取词失败，"+
					"须令 NewBundle 的默认 tag 与语言文件的 tag 严格一致",
				tag, sortedKeys(fileTags))
		}
	}

	// 按语言键字典序，顺序必须确定——map 遍历是随机的，若直接由 map 生成，
	// K-1 的界面语言下拉每次刷新都会重排。与 currency 的三个集合同一口径。
	keys := sortedKeys(c.entries)
	c.allLangs = make([]Lang, 0, len(keys))
	for _, key := range keys {
		c.allLangs = append(c.allLangs, Lang{entry: c.entries[key]})
	}

	e, ok := c.entries[defaultKey]
	if !ok {
		return catalog{}, fmt.Errorf("默认语言 %q 没有对应的语言文件", defaultKey)
	}
	c.defaultLang = Lang{entry: e}

	// matcher 的候选顺序：**默认语言置首**。x/text 的 matcher 以第一项为基准
	// 语言，置首可让「用户偏好完全不匹配」时的行为最接近预期；但它在无匹配时
	// 返回的下标实测并不恒为 0，故 Match 另按 Confidence 显式兜底，不依赖此顺序。
	c.matchTags = append(c.matchTags, defaultTag)
	c.matchLangs = append(c.matchLangs, c.defaultLang)
	for _, key := range keys {
		if key == defaultKey {
			continue
		}
		tag, err := language.Parse(key)
		if err != nil {
			return catalog{}, fmt.Errorf("语言键 %q 不是合法的语言标签: %w", key, err)
		}
		c.matchTags = append(c.matchTags, tag)
		c.matchLangs = append(c.matchLangs, Lang{entry: c.entries[key]})
	}

	// Localizer 预建，且**把默认语言作为末位兜底**传入，使某语言缺某个键时
	// 逐键回落到默认语言的文案，而不是回落成键名本身（实测该回落逐键生效）。
	for _, key := range keys {
		if key == defaultKey {
			c.localizers[key] = goi18n.NewLocalizer(c.bundle, key)
			continue
		}
		c.localizers[key] = goi18n.NewLocalizer(c.bundle, key, defaultKey)
	}

	c.matcher = language.NewMatcher(c.matchTags)

	// 预热模板缓存，把 missingkey=error 抢先焙进每条文案的缓存槽。
	// 这一步不是优化，而是正确性所必需，理由见 warmTemplateCache。
	warmTemplateCache(c)
	return c, nil
}

// warmTemplateCache 在装载末尾把每条文案按 missingKeyOption 预解析一遍。
//
// # 为什么必须有这一步
//
// go-i18n 的模板缓存（internal.Template.Execute）用 sync.Once 保存**第一次**
// 解析的结果，而解析选项来自当次调用传入的 parser。于是同一条文案的
// missingkey 语义由「谁先取词」决定，且此后**永久固定**：
//
//	若某次调用漏传 TemplateParser（用了库默认的 missingkey=default），
//	该文案的缓存槽就被焙成「静默模式」，此后即便本包处处传 missingkey=error
//	也不再生效——占位参数缺失重新变回静默渲染 "<no value>"。
//
// 这是本包实测查出的第五个陷阱，也是最隐蔽的一个：它让「本包已挡住陷阱一」
// 这个结论变得依赖调用顺序，而顺序在并发下不可控。TestTrapMissingKeyDefaultIsSilent
// 最初正是因此而失败——那次失败是这段代码存在的直接原因。
//
// 修法是在 init 期抢先解析：此时还没有任何业务调用，缓存槽必然空着，
// missingkey=error 一定能焙进去。之后无论谁用什么 parser 取词，
// 拿到的都是这份严格模板。已实测确认预热后即便显式用默认 parser 也仍然报错。
//
// 预热调用自身的渲染错误一律忽略——缓存只保存**解析**产物，执行阶段因缺参数
// 报错不影响已缓存的模板。
func warmTemplateCache(c catalog) {
	parser := &goi18ntpl.TextParser{Option: missingKeyOption}
	for key, loc := range c.localizers {
		for msgID := range c.rawMessages[key] {
			_, _ = loc.Localize(&goi18n.LocalizeConfig{
				MessageID:      msgID,
				TemplateParser: parser,
			})
		}
	}
}

// localeFiles 列出语言文件名，按字典序。目录不可读或没有任何语言文件即报错——
// 后者意味着 embed 出了问题，此时全系统一条文案都渲染不出来，必须启动即失败。
func localeFiles(fsys fs.FS, dir string) ([]string, error) {
	dirEntries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("读取语言目录 %q 失败: %w", dir, err)
	}
	var names []string
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), localeExt) {
			continue
		}
		names = append(names, de.Name())
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("语言目录 %q 内没有任何 %s 语言文件", dir, localeExt)
	}
	sort.Strings(names)
	return names, nil
}

// loadFile 装载单个语言文件，返回规范化语言键与「文案键 → 原始模板」。
//
// 语言键取自**文件名**（去扩展名），这是 go-i18n 的既定口径；文件名不是合法
// 语言标签时 go-i18n 会静默给出 und（实测 messages.json → tag=und），故本函数
// 先自行解析文件名并在非法时报错，不让一个拼错名字的文件变成一个叫 und 的语言。
func loadFile(fsys fs.FS, filePath string, b *goi18n.Bundle) (string, map[string]string, error) {
	base := path.Base(filePath)
	name := strings.TrimSuffix(base, localeExt)
	tag, err := language.Parse(name)
	if err != nil {
		return "", nil, fmt.Errorf("语言文件名 %q 不是合法的语言标签: %w", base, err)
	}
	key := tag.String()

	buf, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return "", nil, fmt.Errorf("读取语言文件 %q 失败: %w", base, err)
	}

	// 用规范化后的键重建文件名交给 go-i18n，保证它解析出的 tag 与本函数一致
	// （如 zh_CN.json 会被 language.Parse 规范成 zh-CN）。
	mf, err := b.ParseMessageFileBytes(buf, key+localeExt)
	if err != nil {
		return "", nil, fmt.Errorf("解析语言文件 %q 失败: %w", base, err)
	}

	msgs := make(map[string]string, len(mf.Messages))
	for _, m := range mf.Messages {
		// 取 Other 作为原始模板：中文无复数形态，全部文案都落在 Other 上
		// （实测 {"k":"值"} 解析后 Other="值"）。将来加复数语种时，复数形态由
		// go-i18n 在取词侧处理，Messages 的整包下发仍以 Other 为代表形态。
		src := m.Other
		if src == "" {
			src = m.One
		}
		// 预编译校验：go-i18n **不在解析期校验模板语法**，`{{.Unclosed` 这类
		// 坏模板要到某次取词时才报错（已实测）。在此主动编译一遍，把它提前到
		// 启动失败。编译产物丢弃——取词仍走 go-i18n，以保留其复数能力。
		if _, err := texttemplate.New(m.ID).Option(missingKeyOption).Parse(src); err != nil {
			return "", nil, fmt.Errorf("语言文件 %q 的文案键 %q 模板非法: %w", base, m.ID, err)
		}
		msgs[m.ID] = src
	}
	if len(msgs) == 0 {
		return "", nil, fmt.Errorf("语言文件 %q 内没有任何文案", base)
	}
	if _, ok := msgs[displayNameKey]; !ok {
		return "", nil, fmt.Errorf("语言文件 %q 缺少必需的保留键 %q（该语言的自称，供 K-1 下拉作标签）", base, displayNameKey)
	}
	return key, msgs, nil
}

// sortedKeys 返回 map 键的字典序切片。泛型是为了同时服务
// map[string]bool 与 map[string]*langEntry 两处。
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Render 按「语言 + 文案键」取词并填充占位参数。
//
// **第一个返回值恒非空**，调用方可直接使用：取不到词或渲染失败时回落为文案键
// 本身（如 "passwd.too_short"）——一个键名难看，但比空白提示可诊断得多。
//
// **第二个返回值在发生回落时非 nil**（ErrMissingKey / ErrRenderFailed）。本包
// 不打日志（依赖列为 `—`，不 import base/log），失败原因只能经此上报：
// middleware 用第一个值渲染、把第二个值打进日志；单测据第二个值断言。
//
// lang 为零值时按默认语言处理——A-6 之前 ctxs 尚未写入，登录页与 A-8 登录失败
// 提示走的正是这条路（分层 §三 问题 5：语言解析先于登录态校验）。
//
// params 可为 nil。**多传的参数被忽略，少传的参数一律报错**（missingKeyOption），
// 不会静默渲染出 "<no value>"。
//
// 某语言缺某个键时逐键回落到默认语言的文案（Localizer 预建时已把默认语言作为
// 末位兜底传入），只有默认语言也缺该键时才回落为键名。
func Render(lang Lang, key string, params Params) (string, error) {
	return active.render(lang, key, params)
}

// render 是 Render 的实现，以 catalog 为接收者。
//
// 抽出这一层是为了让**多语言路径可被测试**：本轮只有 zh-CN，包级状态下
// 「某语言缺某个键、回落到默认语言」这条分支根本走不到，而它恰好是最容易写错
// 的一条（out 与 err 同时非空时该采用哪个）。变异测试证实了这一点：把该分支
// 改坏，只断言包级状态的测试全部通过——因为它们碰不到那条路。
// 测试遂用 load 造出双语 catalog，再调本方法，走的是与生产完全相同的代码。
func (c catalog) render(lang Lang, key string, params Params) (string, error) {
	if key == "" {
		// 空键取不到任何词，且回落值也是空串——此时必须让调用方知道，
		// 否则用户看到的是一个完全空白的提示。
		return "", fmt.Errorf("%w: 文案键为空", ErrMissingKey)
	}

	loc := c.localizers[c.resolve(lang).String()]
	if loc == nil {
		// 理论不可达：Lang 只能由本包构造，其键必然在 localizers 内。
		// 保留此分支是为了让「将来有人给 Lang 加了别的构造路径」不至于 panic。
		return key, fmt.Errorf("%w: 语言 %q 无 localizer", ErrMissingKey, lang)
	}

	out, err := loc.Localize(&goi18n.LocalizeConfig{
		MessageID:      key,
		TemplateData:   params,
		TemplateParser: &goi18ntpl.TextParser{Option: missingKeyOption},
	})

	// 关键：out 与 err 可以**同时非空**。当某语言缺某个键而回落到默认语言时，
	// go-i18n 把回落后的文案放进 out，同时用 err 报告「该键在原语言里没找到」
	// （实测：语言 en 缺键时 out="仅中文有"、err="message not found in language en"）。
	// 此时若因 err != nil 就丢弃 out，会把一条好文案换成键名——逐键回落当场失效。
	// 故先看 out 是否可用，再决定是否回落。
	if out != "" && err == nil {
		return out, nil
	}
	if out != "" {
		// 有可用文案但伴随错误：采用文案，同时把原因如实上报给调用方记日志。
		// 这是「语言文件不全但仍能给用户看懂的话」这一常态，不是故障。
		return out, fmt.Errorf("%w: 键 %q 在语言 %q 下已回落: %v", ErrMissingKey, key, lang, err)
	}
	if err != nil {
		// 区分「没有这个键」与「有键但渲染失败」：前者是语言文件不全，后者
		// 多半是调用方少传了占位参数（missingKeyOption 使其报错而非静默出
		// "<no value>"）。两者的修法完全不同，故分成两个哨兵。
		var notFound *goi18n.MessageNotFoundErr
		if errors.As(err, &notFound) {
			return key, fmt.Errorf("%w: 键 %q 语言 %q: %v", ErrMissingKey, key, lang, err)
		}
		return key, fmt.Errorf("%w: 键 %q 语言 %q: %v", ErrRenderFailed, key, lang, err)
	}
	// out 为空且无错误：文案值是空串（语言文件里写了 "k": ""）。go-i18n 不报错，
	// 但一个空白提示对用户毫无意义，故按缺键处理并回落为键名。
	return key, fmt.Errorf("%w: 键 %q 语言 %q 的文案为空串", ErrMissingKey, key, lang)
}

// MustRender 只返回文案，丢弃失败原因。
//
// 仅供「文案键与占位参数都已由测试锁死」的场景使用（如本包自身的测试、将来 L6
// 中键为常量且参数固定的调用点）。middleware 一律用 Render——它需要把失败原因
// 打进日志，那是取词失败唯一的观测点。
func MustRender(lang Lang, key string, params Params) string {
	out, _ := active.render(lang, key, params)
	return out
}

// resolve 把零值语言归一到默认语言。
func (c catalog) resolve(lang Lang) Lang {
	if lang.IsZero() {
		return c.defaultLang
	}
	return lang
}

// Languages 返回全部已装载语言，按语言键字典序，供 K-1 的界面语言下拉使用。
//
// 返回新切片，调用方的改动不会污染包状态——这一条在本包不是例行公事：
// go-i18n 的 Bundle.LanguageTags() 正是因为**返回了内部切片本身**，使得对它
// 排序会改写 bundle 内部 matcher 的顺序、令双语言下取词完全错配（包注释陷阱
// 三）。本包不重复该错误。
func Languages() []Lang { return active.languages() }

func (c catalog) languages() []Lang {
	out := make([]Lang, len(c.allLangs))
	copy(out, c.allLangs)
	return out
}

// Default 返回默认语言（终版 §四 1：中文）。
//
// 用于 A-1/A-7 建账号时 User.界面语言 的初值，以及 middleware 语言解析链的末位。
func Default() Lang { return active.defaultLang }

// ParseLang 校验并解析语言键，是 service/preference 做 K-1 界面语言切换校验的
// 唯一入口（分层 §六 L5.4：「界面语言走 i18n 语言键校验」，不接受任意值）。
//
// **精确校验，不做模糊匹配**：只接受已装载语言的语言键，"zh"、"en-GB" 这类
// 「格式合法但本仓库没有这个语种」一律返回 ErrUnknownLang。按用户偏好挑一个
// 最接近的语言是 Match 的职责，两者刻意分开——校验放宽会让 K-1 存进一个取不到
// 词的语言键，而那是 F-4 之外唯一能让整个界面变成键名的途径。
//
// 大小写与分隔符按 BCP 47 规范化（zh_CN 与 ZH-cn 均得 zh-CN），因为它们指的
// 就是同一个语言；但**不裁剪空白**——" zh-CN " 返回 ErrMalformedLang，与
// language.Parse 的口径一致（实测它拒绝带空白的标签）。
//
// 三档错误对应三句不同的话：没填 / 格式不对 / 不支持该语种。
func ParseLang(key string) (Lang, error) { return active.parseLang(key) }

func (c catalog) parseLang(key string) (Lang, error) {
	if key == "" {
		return Lang{}, ErrEmptyLang
	}
	tag, err := language.Parse(key)
	if err != nil {
		return Lang{}, fmt.Errorf("%w: %q: %v", ErrMalformedLang, key, err)
	}
	e, ok := c.entries[tag.String()]
	if !ok {
		return Lang{}, fmt.Errorf("%w: %q", ErrUnknownLang, key)
	}
	return Lang{entry: e}, nil
}

// Match 按 HTTP 的 Accept-Language 头挑一个最合适的已装载语言，是 middleware
// 语言解析链第二段的实现（分层 §六 L6：ctxs.界面语言 → Accept-Language → 默认）。
//
// **总函数，恒返回一个可用语言**：头为空、格式非法、或没有任何偏好命中时返回
// 默认语言。这是 middleware 全局链的要求——语言解析先于登录态校验，登录页与
// A-8 登录失败提示都必须有文案，此处不能失败。
//
// 与 ParseLang 的分工：本函数做**模糊匹配**（zh-TW 会命中 zh-CN，en-GB 会命中
// en），ParseLang 做**精确校验**。请求头是外部输入、应当尽量宽容；K-1 的持久化
// 取值必须精确。
//
// 不依赖 x/text matcher 在「无匹配」时返回的下标——实测该下标并不恒为 0
// （双语言下 Accept-Language 为 fr 时返回下标 1），故按 Confidence 显式兜底。
func Match(acceptLanguage string) Lang { return active.match(acceptLanguage) }

func (c catalog) match(acceptLanguage string) Lang {
	if strings.TrimSpace(acceptLanguage) == "" {
		return c.defaultLang
	}
	prefs, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil || len(prefs) == 0 {
		// 垃圾头一律回落，不向调用方报错：见上文「总函数」。
		return c.defaultLang
	}
	_, idx, conf := c.matcher.Match(prefs...)
	if conf == language.No {
		return c.defaultLang
	}
	if idx < 0 || idx >= len(c.matchLangs) {
		// 理论不可达，兜住以免下标越界 panic 在一条本该无害的路径上。
		return c.defaultLang
	}
	return c.matchLangs[idx]
}

// Messages 返回该语言的**整包文案**（文案键 → 原始模板），供 handler/i18n 下发
// 给前端（T34④：前端文案与后端错误文案同源，前端不另持一份）。
//
// 返回的是**原始模板**而非渲染结果——占位参数由前端在使用现场填充，这正是
// 「同一份语言文件两侧共用」的前提。模板语法是 Go text/template 的 {{.Name}}，
// 前端需据此取词（键名与占位参数名两侧一致，故不需要额外映射表）。
//
// **已按默认语言补全**：先铺默认语言的全部文案，再以该语言的文案覆盖。这与
// Render 的逐键回落行为一致，保证前端拿到的键集合恒为全集，不会出现某个键在
// 后端渲染得出、在前端却是空白的情形。
//
// lang 为零值时按默认语言处理。返回新 map，调用方的改动不会污染包状态。
func Messages(lang Lang) map[string]string { return active.messages(lang) }

func (c catalog) messages(lang Lang) map[string]string {
	key := c.resolve(lang).String()
	base := c.rawMessages[c.defaultLang.String()]
	out := make(map[string]string, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range c.rawMessages[key] {
		out[k] = v
	}
	return out
}

// MessageKeys 返回该语言整包文案的键，**按字典序**。
//
// 单独提供而非要求调用方对 Messages 的键排序，是因为顺序确定性在此有具体成因：
// go-i18n 的 MessageFile.Messages 顺序源自 map 遍历、实测不稳定（同一文件解析
// 三次即变序）。凡要输出可比对的键清单（整包下发的校验、语言文件的穷尽性核对）
// 都必须先排序。
func MessageKeys(lang Lang) []string { return active.messageKeys(lang) }

func (c catalog) messageKeys(lang Lang) []string {
	return sortedKeys(c.messages(lang))
}
