package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

const referenceManifestMetadataKey = "referenceManifest"

type providerAssetSlot struct {
	AssetKey     string                          `json:"assetKey"`
	MediaType    agentruntime.ReferenceMediaType `json:"mediaType"`
	ArtifactID   string                          `json:"artifactId"`
	RevisionID   string                          `json:"revisionId"`
	ResourceID   string                          `json:"resourceId"`
	RequestField string                          `json:"requestField"`
	ProviderSlot string                          `json:"providerSlot"`
	Ordinal      int                             `json:"ordinal"`
}

type providerAssetTraceRecord struct {
	SchemaVersion  int                 `json:"schemaVersion"`
	ProviderFamily string              `json:"providerFamily"`
	ProviderTaskID string              `json:"providerTaskId"`
	Assets         []providerAssetSlot `json:"assets"`
}

func (s *Service) processKuaiziCompatibleTask(ctx context.Context, task model.Task) (map[string]interface{}, error) {
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(task.InputJSON), &input); err != nil {
		return nil, fmt.Errorf("任务输入解析失败：%w", err)
	}
	if strings.TrimSpace(input.Prompt) == "" {
		input.Prompt = task.Prompt
	}
	watermark, err := taskWatermarkRuntimeFromTask(task)
	if err != nil {
		return nil, err
	}
	input.Watermark = watermark
	input.Config.Model = task.Model
	family, spec, ok := kuaiziProviderFamilyForModel(task.Model)
	if !ok {
		return nil, fmt.Errorf("筷子科技模型未登记：%s", task.Model)
	}
	assetSlots, hasReferenceManifest, err := prepareProviderAssetSlots(&input, family)
	if err != nil {
		return nil, err
	}
	if hasReferenceManifest && task.Audience != model.TaskAudienceInternal {
		return nil, errors.New("供应商素材清单只允许用于内部 Agent 媒体任务")
	}
	if resumedProviderRequestID(ctx) == "" {
		if err := s.hydrateGenerationMedia(task.UserID, &input, true); err != nil {
			return nil, err
		}
	}
	runtime, err := s.repo.FrozenProviderRuntime(task)
	if err != nil {
		return nil, fmt.Errorf("读取筷子科技冻结运行配置失败：%w", err)
	}
	apiKey, err := NewProviderSecretCipher(s.dataDir).Decrypt(runtime.ProviderAccountID, runtime.ProviderCredentialID, runtime.CredentialVersion, runtime.KeyCipher)
	if err != nil {
		return nil, fmt.Errorf("解密筷子科技冻结系列 Key 失败：%w", err)
	}
	input.Config.BaseURL = runtime.BaseURL
	input.Config.APIKey = apiKey
	var result map[string]interface{}
	switch spec.Capability {
	case "video":
		switch family {
		case "seedance":
			input.Config.InterfaceType = "ai-open-platform-video-volcengine"
			result, err = runAIOpenPlatformVolcengineVideoTask(ctx, input)
		case "kling":
			result, err = runKuaiziKlingTask(ctx, input)
		default:
			return nil, fmt.Errorf("筷子科技视频模型系列未实现：%s", family)
		}
	case "image":
		input.Mode = "image"
		switch family {
		case "gpt-image2":
			result, err = runKuaiziGPTImage2Task(ctx, input)
		case "seedream":
			result, err = runKuaiziSeedreamTask(ctx, input)
		default:
			return nil, fmt.Errorf("筷子科技图片模型系列未实现：%s", family)
		}
	case "text":
		input.Mode = "text"
		input.Config.InterfaceType = string(model.ChannelInterfaceChatCompletion)
		input.Config.BaseURL = kuaiziChatCompletionsBaseURL(runtime.BaseURL)
		result, err = runKuaiziChatCompletionsTask(ctx, input)
	default:
		return nil, fmt.Errorf("筷子科技模型能力未实现：%s", spec.Capability)
	}
	if err != nil {
		return nil, err
	}
	if hasReferenceManifest {
		result["providerAssetTrace"] = providerAssetTrace(family, providerTaskIDFromResult(result), assetSlots)
	}
	return result, nil
}

func prepareProviderAssetSlots(input *canvasGenerationInput, family string) ([]providerAssetSlot, bool, error) {
	manifest, present, err := referenceManifestFromGenerationInput(*input)
	if err != nil || !present {
		return nil, present, err
	}
	images, err := reorderProviderMedia(manifest, agentruntime.ReferenceMediaImage, input.ReferenceImages)
	if err != nil {
		return nil, true, err
	}
	videos, err := reorderProviderMedia(manifest, agentruntime.ReferenceMediaVideo, input.ReferenceVideos)
	if err != nil {
		return nil, true, err
	}
	audios, err := reorderProviderMedia(manifest, agentruntime.ReferenceMediaAudio, input.ReferenceAudios)
	if err != nil {
		return nil, true, err
	}
	input.ReferenceImages = images
	input.ReferenceVideos = videos
	input.ReferenceAudios = audios
	requestFields, providerSlots, err := providerAssetRequestPositions(*input, family)
	if err != nil {
		return nil, true, err
	}
	slots := make([]providerAssetSlot, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		requestField, exists := requestFields[entry.AssetKey]
		if !exists {
			return nil, true, fmt.Errorf("供应商素材清单缺少请求字段映射：%s", entry.AssetKey)
		}
		slots = append(slots, providerAssetSlot{
			AssetKey: entry.AssetKey, MediaType: entry.MediaType,
			ArtifactID: entry.ArtifactID, RevisionID: entry.RevisionID, ResourceID: entry.ResourceID,
			RequestField: requestField, ProviderSlot: providerSlots[entry.AssetKey], Ordinal: entry.Ordinal,
		})
	}
	return slots, true, nil
}

func referenceManifestFromGenerationInput(input canvasGenerationInput) (agentruntime.ReferenceManifest, bool, error) {
	if input.Metadata == nil {
		return agentruntime.ReferenceManifest{}, false, nil
	}
	value, present := input.Metadata[referenceManifestMetadataKey]
	if !present {
		return agentruntime.ReferenceManifest{}, false, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return agentruntime.ReferenceManifest{}, true, fmt.Errorf("供应商素材清单序列化失败：%w", err)
	}
	manifest, err := agentruntime.DecodeReferenceManifest(raw)
	if err != nil {
		return agentruntime.ReferenceManifest{}, true, fmt.Errorf("供应商素材清单无效：%w", err)
	}
	return manifest, true, nil
}

func reorderProviderMedia(manifest agentruntime.ReferenceManifest, mediaType agentruntime.ReferenceMediaType, current []providerMedia) ([]providerMedia, error) {
	byAssetKey := make(map[string]providerMedia, len(current))
	for _, media := range current {
		assetKey := strings.TrimSpace(media.Name)
		if assetKey == "" {
			return nil, errors.New("供应商参考素材缺少稳定 assetKey")
		}
		if _, exists := byAssetKey[assetKey]; exists {
			return nil, fmt.Errorf("供应商参考素材 assetKey 重复：%s", assetKey)
		}
		byAssetKey[assetKey] = media
	}
	ordered := make([]providerMedia, 0, len(current))
	for _, entry := range manifest.Entries {
		if entry.MediaType != mediaType {
			continue
		}
		media, exists := byAssetKey[entry.AssetKey]
		if !exists || strings.TrimSpace(media.ID) != entry.ResourceID || strings.TrimSpace(media.URL) != entry.ResourceURL {
			return nil, fmt.Errorf("供应商素材清单与 %s 参考素材不一致：%s", mediaType, entry.AssetKey)
		}
		ordered = append(ordered, media)
		delete(byAssetKey, entry.AssetKey)
	}
	if len(byAssetKey) != 0 {
		return nil, fmt.Errorf("供应商 %s 参考素材存在未登记的 assetKey", mediaType)
	}
	return ordered, nil
}

func providerAssetRequestPositions(input canvasGenerationInput, family string) (map[string]string, map[string]string, error) {
	assetCount := len(input.ReferenceImages) + len(input.ReferenceVideos) + len(input.ReferenceAudios)
	fields := make(map[string]string, assetCount)
	slots := make(map[string]string, assetCount)
	add := func(media []providerMedia, requestField func(int) string, slotPrefix string) {
		for index, item := range media {
			fields[item.Name] = requestField(index)
			slots[item.Name] = fmt.Sprintf("%s%d", slotPrefix, index+1)
		}
	}
	switch family {
	case "seedance":
		contentIndex := 0
		if strings.TrimSpace(seedancePromptText(input)) != "" {
			contentIndex = 1
		}
		add(input.ReferenceImages, func(index int) string { return fmt.Sprintf("content[%d].image_url", contentIndex+index) }, "image")
		contentIndex += len(input.ReferenceImages)
		add(input.ReferenceVideos, func(index int) string { return fmt.Sprintf("content[%d].video_url", contentIndex+index) }, "video")
		contentIndex += len(input.ReferenceVideos)
		add(input.ReferenceAudios, func(index int) string { return fmt.Sprintf("content[%d].audio_url", contentIndex+index) }, "audio")
	case "seedream":
		if len(input.ReferenceVideos) != 0 || len(input.ReferenceAudios) != 0 {
			return nil, nil, errors.New("Seedream 素材清单只允许图片")
		}
		add(input.ReferenceImages, func(index int) string { return fmt.Sprintf("image[%d]", index) }, "image")
	case "gpt-image2":
		if len(input.ReferenceVideos) != 0 || len(input.ReferenceAudios) != 0 {
			return nil, nil, errors.New("GPT Image 2 素材清单只允许图片")
		}
		add(input.ReferenceImages, func(index int) string { return fmt.Sprintf("images[%d]", index) }, "image")
	case "kling":
		if len(input.ReferenceAudios) != 0 {
			return nil, nil, errors.New("Kling 素材清单不允许音频")
		}
		add(input.ReferenceImages, func(index int) string { return fmt.Sprintf("images[%d].url", index) }, "image")
		add(input.ReferenceVideos, func(index int) string { return fmt.Sprintf("videos[%d].url", index) }, "video")
	default:
		return nil, nil, fmt.Errorf("供应商模型系列不支持素材清单：%s", family)
	}
	return fields, slots, nil
}

func providerAssetTrace(family string, providerTaskID string, slots []providerAssetSlot) providerAssetTraceRecord {
	return providerAssetTraceRecord{
		SchemaVersion: 1, ProviderFamily: family, ProviderTaskID: strings.TrimSpace(providerTaskID),
		Assets: append([]providerAssetSlot(nil), slots...),
	}
}

func providerTaskIDFromResult(result map[string]interface{}) string {
	if video, ok := result["video"].(map[string]interface{}); ok {
		return strings.TrimSpace(stringValue(video["taskId"]))
	}
	if images, ok := result["images"].([]map[string]string); ok && len(images) > 0 {
		return strings.TrimSpace(images[0]["taskId"])
	}
	return ""
}
