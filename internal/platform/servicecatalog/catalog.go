// Package servicecatalog declares every external service the platform calls.
// It is the single authoritative answer to "what does cookies depend on" —
// previously that answer was spread across 900 lines of platform/config.
//
// The catalog lives in code, not in the database: it describes what the binary
// is capable of talking to, so it travels with the binary.
package servicecatalog

// Tier says whether the settings page may write this service's configuration.
// Deployment-only concerns (database, admin account, master keys) are not in
// the catalog at all, so there is no third tier here.
type Tier string

const (
	TierEditable Tier = "editable"
	TierReadOnly Tier = "readonly"
)

// FieldKind drives both input rendering and masking. Secret fields are never
// echoed back to the browser in plaintext.
type FieldKind string

const (
	FieldText   FieldKind = "text"
	FieldSecret FieldKind = "secret"
)

// Field is one input on the settings page.
type Field struct {
	Name        string    `json:"name"`
	Label       string    `json:"label"`
	Kind        FieldKind `json:"kind"`
	Required    bool      `json:"required"`
	Placeholder string    `json:"placeholder,omitempty"`
	Help        string    `json:"help,omitempty"`
}

// IsAddress reports whether this field holds the upstream address. Two names
// are in use because miyun's stored configuration already calls it endpoint and
// renaming it would mean migrating live rows. The check lives here so the
// storage layer and the validator cannot disagree about which field is the
// address.
func (f Field) IsAddress() bool {
	return f.Name == "base_url" || f.Name == "endpoint"
}

// Service is one row on the settings page.
type Service struct {
	Code        string `json:"code"`
	DisplayName string `json:"display_name"`
	Tier        Tier   `json:"tier"`
	// Impact is plain Chinese: what stops working when this is down.
	Impact string `json:"impact"`
	// Capability and ModelAlias are set for services backed by a provider
	// model route; empty for everything else.
	Capability     string `json:"capability,omitempty"`
	ModelAlias     string `json:"model_alias,omitempty"`
	ConnectionType string `json:"connection_type,omitempty"`
	// ConnectionCode identifies the provider_connections row this service
	// owns. Two entries must never share one.
	ConnectionCode string  `json:"connection_code,omitempty"`
	Fields         []Field `json:"fields,omitempty"`
	// EnvKeys are the environment variables this service falls back to. For
	// the read-only tier they are also what the page tells the operator to
	// edit on the server.
	EnvKeys []string `json:"env_keys,omitempty"`
	// RestartRequired is true for read-only services whose values are read
	// once at boot.
	RestartRequired bool `json:"restart_required"`
}

// modelFields is the shape shared by every service that is an OpenAI-style
// model endpoint: an address, a model identifier, and a bearer token.
var modelFields = []Field{
	{Name: "base_url", Label: "服务地址", Kind: FieldText, Required: true, Placeholder: "https://ark.cn-beijing.volces.com/api/v3"},
	{Name: "model", Label: "模型名", Kind: FieldText, Required: true, Help: "上游的模型标识，填错要等真正调用时才暴露"},
	{Name: "api_key", Label: "API Key", Kind: FieldSecret, Required: false, Help: "留空则沿用已保存的密钥"},
}

var services = []Service{
	{
		Code: "model.text", DisplayName: "文本模型", Tier: TierEditable,
		Impact:     "策略生成、文案撰写、创意脚本",
		Capability: "text.generate", ModelAlias: "cookies.text.standard",
		ConnectionType: "ark", ConnectionCode: "ark-text",
		Fields: modelFields,
		EnvKeys: []string{
			"COOKIES_PROVIDER_TEXT_ADAPTER",
			"COOKIES_ARK_TEXT_API_KEY", "COOKIES_ARK_TEXT_MODEL", "COOKIES_ARK_TEXT_BASE_URL",
		},
	},
	{
		Code: "model.image", DisplayName: "图片模型", Tier: TierEditable,
		Impact:     "图片生成、图文创意",
		Capability: "image.generate", ModelAlias: "cookies.image.standard",
		ConnectionType: "ark", ConnectionCode: "ark-image",
		Fields: modelFields,
		EnvKeys: []string{
			"COOKIES_PROVIDER_IMAGE_ADAPTER",
			"COOKIES_ARK_IMAGE_API_KEY", "COOKIES_ARK_IMAGE_MODEL", "COOKIES_ARK_IMAGE_BASE_URL",
			"COOKIES_OPENAI_IMAGE_API_KEY", "COOKIES_OPENAI_IMAGE_MODEL", "COOKIES_OPENAI_IMAGE_BASE_URL",
		},
	},
	{
		Code: "model.video", DisplayName: "视频模型", Tier: TierEditable,
		Impact:     "视频创作出片",
		Capability: "video.generate", ModelAlias: "cookies.video.standard",
		ConnectionType: "ark", ConnectionCode: "ark-seedance",
		Fields: modelFields,
		EnvKeys: []string{
			"COOKIES_PROVIDER_VIDEO_ADAPTER", "COOKIES_PROVIDER_ALLOW_DIRECT_VIDEO",
			"ARK_API_KEY", "ARK_BASE_URL",
		},
	},
	{
		Code: "model.vision", DisplayName: "图片理解", Tier: TierEditable,
		Impact:     "素材理解、内容分析",
		Capability: "vision.understand", ModelAlias: "cookies.vision.standard",
		ConnectionType: "adapter_gateway", ConnectionCode: "gateway-vision",
		Fields: modelFields,
		EnvKeys: []string{
			"COOKIES_MEDIA_UNDERSTANDING_REAL_PROVIDER_ENABLED",
			"COOKIES_MEDIA_UNDERSTANDING_VISION_MODEL_ALIAS",
			"COOKIES_MEDIA_UNDERSTANDING_ASR_ENABLED",
		},
	},
	{
		Code: "model.speech", DisplayName: "语音合成", Tier: TierEditable,
		Impact:     "视频配音",
		Capability: "speech.synthesize", ModelAlias: "cookies.speech.standard",
		ConnectionType: "minimax_speech", ConnectionCode: "minimax-speech",
		Fields: modelFields,
		EnvKeys: []string{
			"COOKIES_PROVIDER_SPEECH_ADAPTER", "COOKIES_PROVIDER_AUDIO_ADAPTER",
		},
	},
	{
		Code: "model.document_vision", DisplayName: "文档视觉解析", Tier: TierEditable,
		Impact:     "PDF 与文档类素材解析",
		Capability: "document.vision.parse", ModelAlias: "cookies.document.vision.standard",
		ConnectionType: "las_operator", ConnectionCode: "las-document-vision",
		Fields: modelFields,
		EnvKeys: []string{
			"COOKIES_DOCUMENT_VISION_ENABLED", "COOKIES_DOCUMENT_VISION_MODEL_ALIAS",
		},
	},
	{
		Code: "model.research", DisplayName: "联网研究", Tier: TierEditable,
		Impact:     "需求分析的联网取证",
		Capability: "research.web", ModelAlias: "cookies.research.web.standard",
		ConnectionType: "ark", ConnectionCode: "ark-research-web",
		Fields: modelFields,
		EnvKeys: []string{
			"COOKIES_RESEARCH_SEED_ENABLED", "COOKIES_RESEARCH_SEED_MODEL_ALIAS",
			"COOKIES_RESEARCH_MCP_PROTOCOL_VERSION", "COOKIES_RESEARCH_MCP_ENV_ALLOWLIST",
			"COOKIES_RESEARCH_MCP_STDIO_COMMAND", "COOKIES_RESEARCH_MCP_STDIO_ARGS_JSON",
			"COOKIES_RESEARCH_MCP_TOOL_NAME",
			"COOKIES_RESEARCH_TIMEOUT_SECONDS", "COOKIES_RESEARCH_MAX_OUTPUT_BYTES",
			"COOKIES_RESEARCH_MAX_CONCURRENT",
		},
	},
	{
		Code: "miyun", DisplayName: "密云素材源", Tier: TierEditable,
		Impact:         "素材采集与竞品洞察全链路",
		ConnectionCode: "miyun",
		Fields: []Field{
			{Name: "endpoint", Label: "服务地址", Kind: FieldText, Required: true, Placeholder: "https://api.youshu.youcloud.com/graphql"},
			{Name: "session_cookie", Label: "会话 Cookie", Kind: FieldSecret, Required: false, Help: "留空则沿用已保存的会话"},
		},
		EnvKeys: []string{
			"COOKIES_MIYUN_ENABLED", "COOKIES_MIYUN_ENDPOINT",
			"COOKIES_MIYUN_DOWNLOAD_ALLOWED_HOSTS", "COOKIES_MIYUN_MAX_CONCURRENT",
			"COOKIES_MIYUN_REQUESTS_PER_SECOND", "COOKIES_MIYUN_COOLDOWN_SECONDS",
		},
	},
	{
		Code: "volcengine_speech", DisplayName: "火山语音", Tier: TierEditable,
		Impact:     "视频配音（火山路线）",
		Capability: "speech.synthesize", ModelAlias: "cookies.speech.volcengine",
		ConnectionType: "volcengine_speech", ConnectionCode: "volcengine-speech",
		Fields: []Field{
			{Name: "base_url", Label: "服务地址", Kind: FieldText, Required: true, Placeholder: "https://openspeech.bytedance.com"},
			{Name: "model", Label: "资源 ID", Kind: FieldText, Required: true, Help: "对应 COOKIES_VOLCENGINE_SPEECH_RESOURCE_ID"},
			{Name: "api_key", Label: "API Key", Kind: FieldSecret, Required: false, Help: "留空则沿用已保存的密钥"},
		},
		EnvKeys: []string{
			"COOKIES_VOLCENGINE_SPEECH_ENDPOINT", "COOKIES_VOLCENGINE_SPEECH_API_KEY",
			"COOKIES_VOLCENGINE_SPEECH_RESOURCE_ID", "COOKIES_VOLCENGINE_SPEECH_DEFAULT_VOICE",
		},
	},

	// Read-only tier. These are read once at boot or have no configuration at
	// all, so the page reports state and tells the operator where to change it.
	{
		Code: "volcengine_asr", DisplayName: "火山语音识别", Tier: TierReadOnly, RestartRequired: true,
		Impact: "爆款拆解的音频转写、素材语音识别",
		EnvKeys: []string{
			"COOKIES_VOLCENGINE_ASR_ENDPOINT", "COOKIES_VOLCENGINE_ASR_AUTH_MODE",
			"COOKIES_VOLCENGINE_ASR_APP_ID", "COOKIES_VOLCENGINE_ASR_ACCESS_TOKEN",
			"COOKIES_VOLCENGINE_ASR_API_KEY", "COOKIES_VOLCENGINE_ASR_RESOURCE_ID",
			"COOKIES_VOLCENGINE_ASR_MODEL",
		},
	},
	{
		Code: "storage.tos", DisplayName: "TOS 对象存储", Tier: TierReadOnly, RestartRequired: true,
		Impact: "素材上传与全部文件读写",
		EnvKeys: []string{
			"COOKIES_BLOB_PROVIDER", "COOKIES_FILESYSTEM_BLOB_ROOT",
			"COOKIES_TOS_ENDPOINT", "COOKIES_TOS_REGION", "COOKIES_TOS_BUCKET",
			"COOKIES_TOS_ACCESS_KEY", "COOKIES_TOS_SECRET_KEY", "COOKIES_TOS_SECURITY_TOKEN",
		},
	},
	{
		Code: "document_converter", DisplayName: "Gotenberg 转档", Tier: TierReadOnly, RestartRequired: true,
		Impact: "PPT 与 Word 转 PDF，方案导出",
		EnvKeys: []string{
			"COOKIES_DOCUMENT_CONVERTER_ENABLED", "COOKIES_DOCUMENT_CONVERTER_BASE_URL",
			"COOKIES_DOCUMENT_CONVERTER_VERSION", "COOKIES_DOCUMENT_CONVERTER_TIMEOUT_SECONDS",
			"COOKIES_DOCUMENT_CONVERTER_MAX_PDF_BYTES", "COOKIES_DOCUMENT_CONVERTER_ALLOW_INSECURE_HTTP",
		},
	},
	{
		Code: "document_extractor", DisplayName: "Tika 文档抽取", Tier: TierReadOnly, RestartRequired: true,
		Impact: "研究取证里的文档正文与内嵌图片抽取",
		EnvKeys: []string{
			"COOKIES_RESEARCH_TIKA_ENABLED", "COOKIES_RESEARCH_TIKA_BASE_URL",
			"COOKIES_RESEARCH_TIKA_VERSION", "COOKIES_RESEARCH_TIKA_TIMEOUT_SECONDS",
			"COOKIES_RESEARCH_TIKA_MAX_OUTPUT_BYTES",
		},
	},
	{
		Code: "product_source", DisplayName: "抖音商品源", Tier: TierReadOnly, RestartRequired: false,
		// No credentials and no endpoint of our own: the resolver only holds a
		// redirect allowlist, so there is nothing to configure.
		Impact:  "从商品链接带出商品信息",
		EnvKeys: []string{},
	},
	{
		Code: "scanner.clamav", DisplayName: "ClamAV 病毒扫描", Tier: TierReadOnly, RestartRequired: true,
		Impact:  "上传素材的安全扫描",
		EnvKeys: []string{"COOKIES_SCANNER_MODE", "COOKIES_CLAMAV_ADDRESS"},
	},
	{
		Code: "media.ffmpeg", DisplayName: "FFmpeg 转码", Tier: TierReadOnly, RestartRequired: true,
		Impact:  "视频转码、时长与规格识别",
		EnvKeys: []string{"COOKIES_FFMPEG_PATH", "COOKIES_FFPROBE_PATH", "COOKIES_VIDEO_WORK_ROOT"},
	},
}

// All returns the catalog in display order.
func All() []Service {
	out := make([]Service, len(services))
	copy(out, services)
	return out
}

// Find resolves one entry by code.
func Find(code string) (Service, bool) {
	for _, service := range services {
		if service.Code == code {
			return service, true
		}
	}
	return Service{}, false
}

// ExemptEnvKeys lists environment variables that deliberately have no catalog
// entry: deployment plumbing, credentials that would brick the install if
// edited from a browser, feature flags, and local filesystem paths. None of
// these name an external service.
func ExemptEnvKeys() []string {
	return []string{
		// Runtime and deployment.
		"COOKIES_ENV", "COOKIES_HTTP_ADDR",

		// Database. Editing these from a browser would take the site down.
		"COOKIES_MYSQL_DATABASE", "COOKIES_MYSQL_USER", "COOKIES_MYSQL_PASSWORD",
		"COOKIES_MYSQL_ROOT_PASSWORD", "COOKIES_MYSQL_PORT", "COOKIES_MYSQL_DSN",
		"COOKIES_MYSQL_MEMORY_LIMIT", "COOKIES_MYSQL_INNODB_BUFFER_POOL_SIZE",
		"COOKIES_MYSQL_MAX_SERVER_CONNECTIONS", "COOKIES_MYSQL_MAX_OPEN_CONNS",
		"COOKIES_MYSQL_MAX_IDLE_CONNS",

		// Accounts and sessions.
		"COOKIES_DEMO_DATA_DIR", "COOKIES_DEMO_EMAIL", "COOKIES_DEMO_PASSWORD",
		"COOKIES_PASSWORD_AUTH_ENABLED", "COOKIES_ADMIN_USERNAME", "COOKIES_ADMIN_PASSWORD",
		"COOKIES_SESSION_HOURS",

		// Master keys. Changing these makes every stored credential
		// undecryptable, so they never reach the page.
		"COOKIES_PROVIDER_MASTER_KEY", "COOKIES_PROVIDER_MASTER_KEY_VERSION",
		"COOKIES_MIYUN_MASTER_KEY", "COOKIES_MIYUN_MASTER_KEY_VERSION",
		"COOKIES_PROVIDER_ALLOW_INSECURE_HTTP",

		// Local principal used by the single-tenant deployment.
		"COOKIES_LOCAL_ORGANIZATION_ID", "COOKIES_LOCAL_PRINCIPAL_KIND",
		"COOKIES_LOCAL_PRINCIPAL_ID", "COOKIES_LOCAL_PROJECT_ID", "COOKIES_LOCAL_SCOPES",

		// Feature flags and prompt versions. These switch our own behaviour;
		// they do not point at anything outside the platform.
		"COOKIES_AI_AD_MAX_ACTIVE_UNITS",
		"COOKIES_STRATEGY_ENABLED", "COOKIES_STRATEGY_V2_ENABLED",
		"COOKIES_STRATEGY_APPROVE_ENABLED", "COOKIES_STRATEGY_CRITIC_ENABLED",
		"COOKIES_STRATEGY_CONTEXT_SELECTION_ENABLED", "COOKIES_STRATEGY_REAL_PROVIDER_ENABLED",
		"COOKIES_STRATEGY_CREATIVE_TASK_PLANNING_ENABLED", "COOKIES_STRATEGY_CREATIVE_TASK_PROMPT_VERSION",
		"COOKIES_STRATEGY_PACKAGE_TO_CREATIVE_ENABLED", "COOKIES_STRATEGY_QUICK_VIRAL_REMAKE_ENABLED",
		"COOKIES_STRATEGY_PROMPT_VERSION", "COOKIES_STRATEGY_ORGANIZATION_ALLOWLIST",
		"COOKIES_STRATEGY_TEXT_MODEL_ALIAS", "COOKIES_STRATEGY_LITE_TEXT_MODEL_ALIAS",
		"COOKIES_STRATEGY_DEEP_REVIEW_MODEL_ALIAS",
		"COOKIES_CREATIVE_DIRECTION_PLANNING_ENABLED", "COOKIES_CREATIVE_DIRECTION_PLANNER_MODEL_ALIAS",
		"COOKIES_CREATIVE_BRAND_FILM_MODEL_PLANNER_ENABLED", "COOKIES_CREATIVE_BRAND_FILM_PLANNER_MODEL_ALIAS",
		"COOKIES_CREATIVE_GAME_PREROLL_MODEL_PLANNER_ENABLED", "COOKIES_CREATIVE_GAME_PREROLL_PLANNER_MODEL_ALIAS",
		"COOKIES_CREATIVE_SHORT_DRAMA_MODEL_PLANNER_ENABLED", "COOKIES_CREATIVE_SHORT_DRAMA_PLANNER_MODEL_ALIAS",

		// Local asset paths shipped with the deployment.
		"COOKIES_CREATIVE_IMAGE_FONT_PATH", "COOKIES_CREATIVE_IMAGE_FONT_SHA256",

		// Frontend build-time variables; they never reach the Go binary.
		"VITE_API_BASE_URL", "VITE_COMPAT_API_PROXY_TARGET",
		"VITE_PLATFORM_PROXY_TARGET", "VITE_SHOW_STATE_PREVIEW",
	}
}
