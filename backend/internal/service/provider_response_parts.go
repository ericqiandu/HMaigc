package service

import (
	"fmt"
	"sort"
	"strings"
)

type responsePartKey struct{ output, content int }
type responseTextPart struct {
	text string
	done bool
}
type responseTextParts struct {
	parts map[responsePartKey]responseTextPart
	items map[int]string
}

func responsePartError(reason string) error {
	return fmt.Errorf("%w: %s", errAgentProviderStreamProtocolInvalid, reason)
}

// 索引确定排序，item ID 校验身份；不从正文推断片段归属。
func (p *responseTextParts) accept(event responsesStreamEvent, done bool) error {
	key := responsePartKey{event.OutputIndex, event.ContentIndex}
	if key.output < 0 || key.content < 0 {
		return responsePartError("negative text part index")
	}
	if p.parts == nil {
		p.parts = make(map[responsePartKey]responseTextPart)
		p.items = make(map[int]string)
	}
	if err := p.identify(key.output, event.ItemID); err != nil {
		return err
	}
	part := p.parts[key]
	if done {
		if (part.done && event.Text != part.text) || !strings.HasPrefix(event.Text, part.text) {
			return responsePartError("text part completion conflicts with prior text")
		}
		part.text, part.done = event.Text, true
	} else {
		if part.done {
			return responsePartError("delta after text part completion")
		}
		part.text += event.Delta
	}
	p.parts[key] = part
	return nil
}

func (p *responseTextParts) identify(index int, id string) error {
	if id == "" {
		return nil
	}
	if existing := p.items[index]; existing != "" && existing != id {
		return responsePartError("output item identity changed")
	}
	for other, existing := range p.items {
		if other != index && existing == id {
			return responsePartError("output item identity reused")
		}
	}
	p.items[index] = id
	return nil
}

// 未知前序项可能是 reasoning/tool 等非文本项，因此后续片段在最终快照确认顺序后输出。
func (p *responseTextParts) emitFirst(result *providerTextStreamResult, emit func(string) error) error {
	return reconcileResponsesFinalText(result, p.parts[responsePartKey{0, 0}].text, emit)
}

func (p *responseTextParts) finish(event responsesStreamEvent, result *providerTextStreamResult, emit func(string) error) error {
	if event.Response.Output != nil {
		finalParts := make(map[responsePartKey]string)
		for output, item := range event.Response.Output {
			if p.items == nil {
				p.items = make(map[int]string)
			}
			if err := p.identify(output, item.ID); err != nil {
				return err
			}
			for content, part := range item.Content {
				if part.Type == "output_text" {
					finalParts[responsePartKey{output, content}] = part.Text
				}
			}
		}
		for key, part := range p.parts {
			final, exists := finalParts[key]
			if !exists || (part.done && part.text != final) || !strings.HasPrefix(final, part.text) {
				return responsePartError("completed snapshot conflicts with text part")
			}
		}
		p.parts = make(map[responsePartKey]responseTextPart, len(finalParts))
		for key, value := range finalParts {
			p.parts[key] = responseTextPart{text: value, done: true}
		}
	}
	keys := make([]responsePartKey, 0, len(p.parts))
	for key := range p.parts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].output != keys[j].output {
			return keys[i].output < keys[j].output
		}
		return keys[i].content < keys[j].content
	})
	var text strings.Builder
	for _, key := range keys {
		text.WriteString(p.parts[key].text)
	}
	return reconcileResponsesFinalText(result, text.String(), emit)
}
