package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
)

const agentMediaGenerationOperation = "media_generation"

var (
	errAgentMediaArgumentsInvalid  = errors.New("agent media generation arguments are invalid")
	errAgentMediaInputChanged      = errors.New("agent media generation input changed after approval proposal")
	errAgentMediaModelUnavailable  = errors.New("agent media generation model is unavailable")
	errAgentMediaTargetInvalid     = errors.New("agent media generation target canvas node is invalid")
	errAgentNativeAudioUnavailable = errors.New("agent video model does not support the requested native audio facts")
)

type agentMediaInputResource struct {
	ResourceID string `json:"resourceId"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	MimeType   string `json:"mimeType"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

type agentMediaGenerationArguments struct {
	InputResources  []agentMediaInputResource             `json:"inputResources"`
	GenerationModel agentruntime.GenerationModelSelection `json:"generationModel"`
	Capability      string                                `json:"capability"`
	Parameters      json.RawMessage                       `json:"parameters"`
}

type agentImageGenerationParameters struct {
	Prompt                string `json:"prompt"`
	AspectRatio           string `json:"aspectRatio"`
	Resolution            string `json:"resolution"`
	Quality               string `json:"quality,omitempty"`
	Count                 int    `json:"count"`
	TransparentBackground bool   `json:"transparentBackground,omitempty"`
}

type agentVideoGenerationParameters struct {
	Prompt          string `json:"prompt"`
	AspectRatio     string `json:"aspectRatio"`
	Resolution      string `json:"resolution"`
	DurationSeconds int    `json:"durationSeconds"`
	GenerateAudio   bool   `json:"generateAudio"`
}

type agentAudioGenerationParameters struct {
	Prompt        string `json:"prompt"`
	Voice         string `json:"voice"`
	Format        string `json:"format,omitempty"`
	Speed         string `json:"speed,omitempty"`
	Volume        string `json:"volume,omitempty"`
	Pitch         string `json:"pitch,omitempty"`
	Emotion       string `json:"emotion,omitempty"`
	LanguageBoost string `json:"languageBoost,omitempty"`
	SampleRate    string `json:"sampleRate,omitempty"`
	Bitrate       string `json:"bitrate,omitempty"`
	Channel       string `json:"channel,omitempty"`
	Instructions  string `json:"instructions,omitempty"`
}

func agentMediaGenerationOperationForRun(runID string) string {
	return agentMediaGenerationOperation + ":" + strings.TrimSpace(runID)
}

func agentMediaGenerationRunID(operation string) (string, bool) {
	prefix := agentMediaGenerationOperation + ":"
	operation = strings.TrimSpace(operation)
	if !strings.HasPrefix(operation, prefix) || len(operation) > 96 {
		return "", false
	}
	runID := strings.TrimSpace(strings.TrimPrefix(operation, prefix))
	return runID, runID != ""
}

func buildAgentMediaGenerationTaskInput(arguments agentMediaGenerationArguments, capabilities *PublicProviderCapabilities) (canvasGenerationInput, string, int64, error) {
	if capabilities == nil || capabilities.Capability != arguments.Capability || capabilities.ModelKey != arguments.GenerationModel.Model {
		return canvasGenerationInput{}, "", 0, errAgentMediaModelUnavailable
	}
	input := canvasGenerationInput{
		Mode:            arguments.Capability,
		Config:          providerConfig{ChannelID: arguments.GenerationModel.ChannelID, Model: arguments.GenerationModel.Model},
		ReferenceImages: []providerMedia{}, ReferenceVideos: []providerMedia{}, ReferenceAudios: []providerMedia{},
	}
	for _, resource := range arguments.InputResources {
		media := providerMedia{
			ID: resource.ResourceID, Name: resource.ResourceID, Type: resource.Kind,
			URL: resource.URL, StorageKey: "resource:" + resource.ResourceID,
			MimeType: resource.MimeType, DurationMs: resource.DurationMS,
		}
		switch resource.Kind {
		case "image":
			input.ReferenceImages = append(input.ReferenceImages, media)
		case "video":
			input.ReferenceVideos = append(input.ReferenceVideos, media)
		case "audio":
			input.ReferenceAudios = append(input.ReferenceAudios, media)
		default:
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
	}
	switch arguments.Capability {
	case "image":
		parameters, err := decodeAgentImageGenerationParameters(arguments.Parameters)
		if err != nil || !containsString(capabilities.Ratios, parameters.AspectRatio) ||
			!containsString(capabilities.Resolutions, parameters.Resolution) ||
			!containsOptionalString(capabilities.Qualities, parameters.Quality) ||
			!containsIntValue(capabilities.OutputCounts, parameters.Count) ||
			len(input.ReferenceVideos) > 0 || len(input.ReferenceAudios) > 0 ||
			(capabilities.MaxImages > 0 && len(input.ReferenceImages) > capabilities.MaxImages) {
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
		size, err := agentImageGenerationSize(parameters.AspectRatio, parameters.Resolution, capabilities)
		if err != nil {
			return canvasGenerationInput{}, "", 0, errors.Join(errAgentMediaArgumentsInvalid, err)
		}
		input.Prompt = parameters.Prompt
		input.Config.Size = size
		input.Config.Quality = parameters.Quality
		input.Config.Count = strconv.Itoa(parameters.Count)
		input.Config.TransparentBackground = strconv.FormatBool(parameters.TransparentBackground)
		return input, parameters.Prompt, int64(parameters.Count), nil
	case "video":
		parameters, err := decodeAgentVideoGenerationParameters(arguments.Parameters)
		if err != nil {
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
		if parameters.GenerateAudio && !providerGeneratedAudioSupported(capabilities.SupportsGeneratedAudio, capabilities.GeneratedAudioResolutions, parameters.Resolution) {
			return canvasGenerationInput{}, "", 0, errAgentNativeAudioUnavailable
		}
		if !containsString(capabilities.Ratios, parameters.AspectRatio) ||
			!containsString(capabilities.Resolutions, parameters.Resolution) ||
			(capabilities.DurationMin > 0 && parameters.DurationSeconds < capabilities.DurationMin) ||
			(capabilities.DurationMax > 0 && parameters.DurationSeconds > capabilities.DurationMax) ||
			(capabilities.MaxImages > 0 && len(input.ReferenceImages) > capabilities.MaxImages) ||
			(capabilities.MaxVideos > 0 && len(input.ReferenceVideos) > capabilities.MaxVideos) ||
			(capabilities.MaxAudios > 0 && len(input.ReferenceAudios) > capabilities.MaxAudios) {
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
		input.Prompt = parameters.Prompt
		input.Config.Size = parameters.AspectRatio
		input.Config.VQuality = parameters.Resolution
		input.Config.VideoSeconds = strconv.Itoa(parameters.DurationSeconds)
		input.Config.VideoGenerateAudio = strconv.FormatBool(parameters.GenerateAudio)
		return input, parameters.Prompt, int64(parameters.DurationSeconds), nil
	case "audio":
		parameters, err := decodeAgentAudioGenerationParameters(arguments.Parameters)
		if err != nil || len(input.ReferenceImages) > 0 || len(input.ReferenceVideos) > 0 ||
			(capabilities.MaxAudios > 0 && len(input.ReferenceAudios) > capabilities.MaxAudios) {
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
		input.Prompt = parameters.Prompt
		input.Config.AudioVoice = parameters.Voice
		input.Config.AudioFormat = parameters.Format
		input.Config.AudioSpeed = parameters.Speed
		input.Config.AudioVolume = parameters.Volume
		input.Config.AudioPitch = parameters.Pitch
		input.Config.AudioEmotion = parameters.Emotion
		input.Config.AudioLanguageBoost = parameters.LanguageBoost
		input.Config.AudioSampleRate = parameters.SampleRate
		input.Config.AudioBitrate = parameters.Bitrate
		input.Config.AudioChannel = parameters.Channel
		input.Config.AudioInstructions = parameters.Instructions
		return input, parameters.Prompt, 1, nil
	default:
		return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
	}
}

func decodeAgentImageGenerationParameters(raw json.RawMessage) (agentImageGenerationParameters, error) {
	var parameters agentImageGenerationParameters
	if err := decodeStrictAgentMediaParameters(raw, &parameters); err != nil {
		return parameters, err
	}
	parameters.Prompt = strings.TrimSpace(parameters.Prompt)
	parameters.AspectRatio = strings.TrimSpace(parameters.AspectRatio)
	parameters.Resolution = strings.ToUpper(strings.TrimSpace(parameters.Resolution))
	parameters.Quality = strings.TrimSpace(parameters.Quality)
	if parameters.Prompt == "" || parameters.AspectRatio == "" || parameters.Resolution == "" || parameters.Count < 1 {
		return parameters, errAgentMediaArgumentsInvalid
	}
	return parameters, nil
}

func decodeAgentVideoGenerationParameters(raw json.RawMessage) (agentVideoGenerationParameters, error) {
	var parameters agentVideoGenerationParameters
	if err := decodeStrictAgentMediaParameters(raw, &parameters); err != nil {
		return parameters, err
	}
	parameters.Prompt = strings.TrimSpace(parameters.Prompt)
	parameters.AspectRatio = strings.TrimSpace(parameters.AspectRatio)
	parameters.Resolution = strings.TrimSpace(parameters.Resolution)
	if parameters.Prompt == "" || parameters.AspectRatio == "" || parameters.Resolution == "" || parameters.DurationSeconds < 1 {
		return parameters, errAgentMediaArgumentsInvalid
	}
	return parameters, nil
}

func decodeAgentAudioGenerationParameters(raw json.RawMessage) (agentAudioGenerationParameters, error) {
	var parameters agentAudioGenerationParameters
	if err := decodeStrictAgentMediaParameters(raw, &parameters); err != nil {
		return parameters, err
	}
	parameters.Prompt = strings.TrimSpace(parameters.Prompt)
	parameters.Voice = strings.TrimSpace(parameters.Voice)
	if parameters.Prompt == "" || parameters.Voice == "" {
		return parameters, errAgentMediaArgumentsInvalid
	}
	return parameters, nil
}

type agentMediaParameterSet interface {
	agentImageGenerationParameters | agentVideoGenerationParameters | agentAudioGenerationParameters
}

func decodeStrictAgentMediaParameters[T agentMediaParameterSet](raw json.RawMessage, target *T) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errAgentMediaArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errAgentMediaArgumentsInvalid
	}
	return nil
}

func containsOptionalString(values []string, value string) bool {
	if len(values) == 0 {
		return value == ""
	}
	return containsString(values, value)
}

func containsIntValue(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func agentImageGenerationSize(ratio string, resolution string, capabilities *PublicProviderCapabilities) (string, error) {
	if capabilities == nil {
		return "", errors.New("image provider capabilities are unavailable")
	}
	parts := strings.Split(strings.TrimSpace(ratio), ":")
	if len(parts) != 2 {
		return "", errors.New("image aspect ratio is invalid")
	}
	ratioWidth, widthErr := strconv.ParseFloat(parts[0], 64)
	ratioHeight, heightErr := strconv.ParseFloat(parts[1], 64)
	if widthErr != nil || heightErr != nil || ratioWidth <= 0 || ratioHeight <= 0 {
		return "", errors.New("image aspect ratio is invalid")
	}
	normalizedResolution := strings.ToUpper(strings.TrimSpace(resolution))
	targetPixels := capabilities.ResolutionPixels[normalizedResolution]
	var width, height float64
	if targetPixels > 0 {
		aspectRatio := ratioWidth / ratioHeight
		width = math.Sqrt(float64(targetPixels) * aspectRatio)
		height = math.Sqrt(float64(targetPixels) / aspectRatio)
	} else {
		longestEdge := map[string]float64{"1K": 1824, "2K": 2048, "4K": 3840}[normalizedResolution]
		if longestEdge == 0 {
			return "", errors.New("image resolution is invalid")
		}
		if normalizedResolution == "1K" && ratioWidth == ratioHeight {
			longestEdge = 1024
		}
		shortestEdge := longestEdge * math.Min(ratioWidth, ratioHeight) / math.Max(ratioWidth, ratioHeight)
		if ratioWidth >= ratioHeight {
			width, height = longestEdge, shortestEdge
		} else {
			width, height = shortestEdge, longestEdge
		}
	}
	width, height = alignAgentImageDimension(width), alignAgentImageDimension(height)
	const maxPixels = 8_294_400
	if width*height > maxPixels {
		scale := math.Sqrt(maxPixels / (width * height))
		width = floorAgentImageDimension(width * scale)
		height = floorAgentImageDimension(height * scale)
	}
	return strconv.Itoa(int(width)) + "x" + strconv.Itoa(int(height)), nil
}

func alignAgentImageDimension(value float64) float64 {
	return math.Max(64, math.Round(value/16)*16)
}

func floorAgentImageDimension(value float64) float64 {
	return math.Max(64, math.Floor(value/16)*16)
}
