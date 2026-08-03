package model

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestModelChannelInterfaceTypeColumnFitsSupportedValues(t *testing.T) {
	parsed, err := schema.Parse(&ModelChannel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse ModelChannel schema: %v", err)
	}
	field := parsed.LookUpField("InterfaceType")
	if field == nil {
		t.Fatal("ModelChannel.InterfaceType schema field is missing")
	}
	for _, interfaceType := range []ChannelInterfaceType{
		ChannelInterfaceChatCompletion,
		ChannelInterfaceOpenAIResponse,
		ChannelInterfaceOpenAIImage,
		ChannelInterfaceAPIMartImage,
		ChannelInterfaceNewAPIVideo,
		ChannelInterfaceXAIVideo,
		ChannelInterfaceAIOpenVideo,
		ChannelInterfaceAIOpenVideoVolcengine,
		ChannelInterfaceMiniMaxSpeech,
		ChannelInterfaceMiniMaxVideo,
		ChannelInterfaceKlingVideo,
	} {
		if len(interfaceType) > field.Size {
			t.Fatalf("interface type %q has length %d but column size is %d", interfaceType, len(interfaceType), field.Size)
		}
	}
}
