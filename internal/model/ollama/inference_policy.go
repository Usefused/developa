package ollama

import "strings"

// CachePolicy changes whenever fixed inference options or adapter prompt transformations change.
func (c *Client) CachePolicy() string { return "ollama-chat-v2" }

func (c *Client) thinkingPolicy() any {
	name := strings.ToLower(c.cfg.Model)
	name = name[strings.LastIndex(name, "/")+1:]
	name, _, _ = strings.Cut(name, ":")
	// GPT-OSS ignores a boolean thinking flag, so select its lowest supported level explicitly.
	if name == "gpt-oss" || strings.HasPrefix(name, "gpt-oss-") {
		return "low"
	}
	return false
}
