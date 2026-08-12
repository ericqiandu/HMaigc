package model

import "strings"

const GenericModelBrandKey = "generic"

var modelBrandKeys = map[string]struct{}{
	"generic": {}, "openai": {}, "google": {}, "anthropic": {}, "deepseek": {},
	"xai": {}, "zhipu": {}, "minimax": {}, "seedance": {}, "kling": {},
	"qwen": {}, "alibaba": {}, "volcengine": {}, "runway": {}, "luma": {},
	"pika": {}, "flux": {}, "stability": {}, "ideogram": {}, "recraft": {},
}

func IsModelBrandKey(value string) bool {
	_, exists := modelBrandKeys[strings.TrimSpace(value)]
	return exists
}

// InferModelBrandKey 只根据上游稳定模型标识分配展示品牌，不承担模型能力或语义判断。
func InferModelBrandKey(modelKey string) string {
	key := strings.ToLower(strings.TrimSpace(modelKey))
	checks := []struct {
		brand   string
		markers []string
	}{
		{brand: "openai", markers: []string{"gpt-", "gpt_", "sora-", "dall-e"}},
		{brand: "google", markers: []string{"gemini-", "veo-", "imagen-"}},
		{brand: "anthropic", markers: []string{"claude-"}},
		{brand: "deepseek", markers: []string{"deepseek-"}},
		{brand: "xai", markers: []string{"grok-"}},
		{brand: "zhipu", markers: []string{"glm-", "cogview-", "cogvideo"}},
		{brand: "minimax", markers: []string{"minimax-", "hailuo-", "video-01", "video-02"}},
		{brand: "seedance", markers: []string{"seedance-"}},
		{brand: "kling", markers: []string{"kling-"}},
		{brand: "qwen", markers: []string{"qwen-", "wanx-", "wan2"}},
		{brand: "alibaba", markers: []string{"tongyi-"}},
		{brand: "volcengine", markers: []string{"doubao-"}},
		{brand: "runway", markers: []string{"runway-", "gen-3", "gen-4"}},
		{brand: "luma", markers: []string{"luma-", "ray-", "dream-machine"}},
		{brand: "pika", markers: []string{"pika-"}},
		{brand: "flux", markers: []string{"flux-"}},
		{brand: "stability", markers: []string{"stable-diffusion", "stable-image", "sdxl"}},
		{brand: "ideogram", markers: []string{"ideogram-"}},
		{brand: "recraft", markers: []string{"recraft-"}},
	}
	for _, check := range checks {
		for _, marker := range check.markers {
			if strings.Contains(key, marker) {
				return check.brand
			}
		}
	}
	return GenericModelBrandKey
}
