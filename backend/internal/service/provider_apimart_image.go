package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apimartImagePollInterval = 3 * time.Second

type apimartImageRequest struct {
	Model        string   `json:"model"`
	Prompt       string   `json:"prompt"`
	Size         string   `json:"size,omitempty"`
	Resolution   string   `json:"resolution,omitempty"`
	Quality      string   `json:"quality,omitempty"`
	Background   string   `json:"background,omitempty"`
	OutputFormat string   `json:"output_format,omitempty"`
	Count        int      `json:"n"`
	ImageURLs    []string `json:"image_urls,omitempty"`
}

type apimartImageModelProfile struct {
	label                 string
	allowedAspectRatios   map[string]bool
	defaultAspectRatio    string
	maxReferenceImages    int
	resolutionByQuality   map[string]string
	supportsQuality       bool
	supportsTransparency  bool
	supportsReferenceData bool
}

type apimartImageSubmission struct {
	Code int `json:"code"`
	Data []struct {
		Status string `json:"status"`
		TaskID string `json:"task_id"`
	} `json:"data"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

type apimartImageTaskResponse struct {
	Code int `json:"code"`
	Data struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			Images []struct {
				URL []string `json:"url"`
			} `json:"images"`
		} `json:"result"`
	} `json:"data"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

func runAPIMartImageTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	profile, err := apimartImageProfile(input.Config.Model)
	if err != nil {
		return nil, err
	}
	if input.Mask != nil {
		return nil, fmt.Errorf("%s 暂不支持画布蒙版编辑", profile.label)
	}
	if input.Config.TransparentBackground == "true" && !profile.supportsTransparency {
		return nil, fmt.Errorf("%s 不支持透明背景", profile.label)
	}
	if len(input.ReferenceImages) > profile.maxReferenceImages {
		return nil, fmt.Errorf("%s 最多支持 %d 张参考图", profile.label, profile.maxReferenceImages)
	}

	taskID := resumedProviderRequestID(ctx)
	if taskID == "" {
		request, err := apimartImageRequestFromInput(input, profile)
		if err != nil {
			return nil, err
		}
		var submission apimartImageSubmission
		if err := postJSON(ctx, input.Config, "/images/generations", request, &submission); err != nil {
			return nil, err
		}
		if submission.Code != 0 && submission.Code != httpSuccessCode {
			return nil, fmt.Errorf("APIMart 图片任务提交失败：%s", apimartResponseMessage(submission.Message, submission.Msg))
		}
		if len(submission.Data) == 0 || strings.TrimSpace(submission.Data[0].TaskID) == "" {
			return nil, errors.New("APIMart 图片任务提交成功但未返回 task_id")
		}
		taskID = strings.TrimSpace(submission.Data[0].TaskID)
	}

	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var task apimartImageTaskResponse
		path := "/tasks/" + url.PathEscape(taskID) + "?language=zh"
		if err := getJSON(ctx, input.Config, path, &task); err != nil {
			return nil, err
		}
		if task.Code != 0 && task.Code != httpSuccessCode {
			return nil, fmt.Errorf("APIMart 图片任务查询失败：%s", apimartResponseMessage(task.Message, task.Msg))
		}
		switch strings.ToLower(strings.TrimSpace(task.Data.Status)) {
		case "completed":
			images := apimartCompletedImages(task)
			if len(images) == 0 {
				return nil, errors.New("APIMart 图片任务已完成但未返回可用图片")
			}
			return map[string]interface{}{"mode": "image", "images": images}, nil
		case "failed", "cancelled":
			reason := "上游未提供失败原因"
			if task.Data.Error != nil && strings.TrimSpace(task.Data.Error.Message) != "" {
				reason = strings.TrimSpace(task.Data.Error.Message)
			}
			statusLabel := map[string]string{
				"failed":    "失败",
				"cancelled": "已取消",
			}[strings.ToLower(strings.TrimSpace(task.Data.Status))]
			return nil, fmt.Errorf("APIMart 图片任务%s：%s", statusLabel, reason)
		case "pending", "processing", "submitted":
			if err := sleepContext(ctx, apimartImagePollInterval); err != nil {
				return nil, err
			}
		case "":
			return nil, errors.New("APIMart 图片任务查询结果缺少 status")
		default:
			return nil, fmt.Errorf("APIMart 图片任务返回未知状态：%s", task.Data.Status)
		}
	}
	return nil, errors.New("APIMart 图片生成超时")
}

const httpSuccessCode = 200

func apimartImageRequestFromInput(input canvasGenerationInput, profile apimartImageModelProfile) (apimartImageRequest, error) {
	size, resolution, quality, err := normalizeAPIMartImageOutput(input.Config.Size, input.Config.Quality, profile)
	if err != nil {
		return apimartImageRequest{}, err
	}
	references := make([]string, 0, len(input.ReferenceImages))
	for _, image := range input.ReferenceImages {
		value := strings.TrimSpace(firstNonEmpty(image.DataURL, image.URL))
		if !profile.supportsReferenceData {
			value = strings.TrimSpace(image.URL)
		}
		if value == "" {
			return apimartImageRequest{}, fmt.Errorf("参考图 %s 缺少可提交的 URL 或 data URL", image.ID)
		}
		if strings.HasPrefix(value, "data:") && !profile.supportsReferenceData {
			return apimartImageRequest{}, fmt.Errorf("%s 的参考图必须是可公开访问的 URL，不能使用 data URL", profile.label)
		}
		references = append(references, value)
	}
	request := apimartImageRequest{
		Model:      strings.TrimSpace(input.Config.Model),
		Prompt:     withSystemPrompt(input.Config, input.Prompt),
		Size:       size,
		Resolution: resolution,
		Quality:    quality,
		Count:      1,
		ImageURLs:  references,
	}
	if input.Config.TransparentBackground == "true" {
		request.Background = "transparent"
		request.OutputFormat = "png"
	}
	return request, nil
}

func normalizeAPIMartImageOutput(
	size string,
	quality string,
	profile apimartImageModelProfile,
) (string, string, string, error) {
	normalizedSize := strings.TrimSpace(size)
	if normalizedSize == "" {
		normalizedSize = profile.defaultAspectRatio
	} else if strings.Contains(strings.ToLower(normalizedSize), "x") {
		parts := strings.Split(strings.ToLower(normalizedSize), "x")
		if len(parts) != 2 {
			return "", "", "", fmt.Errorf("APIMart 图片尺寸无效：%s", size)
		}
		width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
			return "", "", "", fmt.Errorf("APIMart 图片尺寸无效：%s", size)
		}
		divisor := greatestCommonDivisor(width, height)
		normalizedSize = fmt.Sprintf("%d:%d", width/divisor, height/divisor)
	}
	if !profile.allowedAspectRatios[normalizedSize] {
		return "", "", "", fmt.Errorf("%s 不支持图片比例 %s", profile.label, normalizedSize)
	}
	normalizedQuality := strings.ToLower(strings.TrimSpace(quality))
	if profile.supportsQuality {
		if normalizedQuality == "" {
			normalizedQuality = "auto"
		}
		if normalizedQuality != "auto" && normalizedQuality != "low" &&
			normalizedQuality != "medium" && normalizedQuality != "high" {
			return "", "", "", fmt.Errorf("%s 不支持图片质量 %s", profile.label, quality)
		}
		return normalizedSize, "", normalizedQuality, nil
	}
	resolution := ""
	if normalizedQuality != "" && normalizedQuality != "auto" {
		mapped, ok := profile.resolutionByQuality[normalizedQuality]
		if !ok {
			return "", "", "", fmt.Errorf("%s 不支持图片质量 %s", profile.label, quality)
		}
		resolution = mapped
	}
	return normalizedSize, resolution, "", nil
}

var apimartGeminiAspectRatios = map[string]bool{
	"auto": true, "1:1": true, "3:2": true, "2:3": true, "4:3": true, "3:4": true,
	"16:9": true, "9:16": true, "5:4": true, "4:5": true, "21:9": true,
	"1:4": true, "4:1": true, "1:8": true, "8:1": true,
}

var apimartGPTImageOneAspectRatios = map[string]bool{
	"1:1": true, "3:2": true, "2:3": true,
}

var apimartGPTImageTwoAspectRatios = map[string]bool{
	"auto": true, "1:1": true, "3:2": true, "2:3": true, "4:3": true, "3:4": true,
	"5:4": true, "4:5": true, "16:9": true, "9:16": true, "2:1": true, "1:2": true,
	"21:9": true, "9:21": true,
}

var apimartGeminiResolutionByQuality = map[string]string{
	"low": "1K", "medium": "2K", "high": "4K",
	"0.5k": "0.5K", "1k": "1K", "2k": "2K", "4k": "4K",
}

var apimartGPTImageTwoResolutionByQuality = map[string]string{
	"low": "1k", "medium": "2k", "high": "4k",
	"1k": "1k", "2k": "2k", "4k": "4k",
}

func apimartImageProfile(model string) (apimartImageModelProfile, error) {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "gemini-3.1-flash-image-preview", "nano-banana-2-ext",
		"gemini-3.1-flash-image-preview-official", "nano-banana-2":
		return apimartImageModelProfile{
			label:                 "APIMart Gemini 图片模型",
			allowedAspectRatios:   apimartGeminiAspectRatios,
			defaultAspectRatio:    "auto",
			maxReferenceImages:    14,
			resolutionByQuality:   apimartGeminiResolutionByQuality,
			supportsReferenceData: true,
		}, nil
	case "gpt-image-1-official", "gpt-image-1.5-official":
		return apimartImageModelProfile{
			label:                 "APIMart GPT Image 1",
			allowedAspectRatios:   apimartGPTImageOneAspectRatios,
			defaultAspectRatio:    "1:1",
			maxReferenceImages:    15,
			supportsQuality:       true,
			supportsTransparency:  true,
			supportsReferenceData: false,
		}, nil
	case "gpt-4o-image":
		return apimartImageModelProfile{
			label:                 "APIMart GPT-4o Image",
			allowedAspectRatios:   apimartGPTImageOneAspectRatios,
			defaultAspectRatio:    "1:1",
			maxReferenceImages:    5,
			supportsReferenceData: true,
		}, nil
	case "gpt-image-2", "gpt-image-2-official":
		return apimartImageModelProfile{
			label:                 "APIMart GPT Image 2",
			allowedAspectRatios:   apimartGPTImageTwoAspectRatios,
			defaultAspectRatio:    "auto",
			maxReferenceImages:    16,
			resolutionByQuality:   apimartGPTImageTwoResolutionByQuality,
			supportsReferenceData: true,
		}, nil
	default:
		return apimartImageModelProfile{}, fmt.Errorf(
			"APIMart 图片接口暂未声明模型 %q 的参数契约，请先在后台选择已支持的模型",
			strings.TrimSpace(model),
		)
	}
}

func greatestCommonDivisor(left int, right int) int {
	for right != 0 {
		left, right = right, left%right
	}
	return left
}

func apimartCompletedImages(task apimartImageTaskResponse) []map[string]string {
	images := make([]map[string]string, 0)
	for _, item := range task.Data.Result.Images {
		for _, imageURL := range item.URL {
			if strings.TrimSpace(imageURL) != "" {
				images = append(images, map[string]string{"dataUrl": strings.TrimSpace(imageURL)})
			}
		}
	}
	return images
}

func apimartResponseMessage(message string, msg string) string {
	value := strings.TrimSpace(firstNonEmpty(message, msg))
	if value == "" {
		return "上游未提供错误信息"
	}
	return value
}
