package auth

import (
	"strconv"
	"strings"
)

// AttributeFilePriority marks priority values inherited from a physical auth file.
const AttributeFilePriority = "file_priority"

// AttributeFileModelPriority marks overrides inherited from a physical auth file.
const AttributeFileModelPriority = "file_model_priority"

// ApplyAuthPriorityMetadata copies valid file priorities into credential metadata and routing attributes.
func ApplyAuthPriorityMetadata(auth *Auth, metadata map[string]any) {
	if auth == nil {
		return
	}
	delete(auth.Attributes, AttributeFileModelPriority)
	if raw, exists := metadata[MetadataModelPriority]; exists {
		if _, errParse := ParseModelPriority(raw); errParse == nil {
			if auth.Metadata == nil {
				auth.Metadata = make(map[string]any)
			}
			auth.Metadata[MetadataModelPriority] = raw
			if auth.Attributes == nil {
				auth.Attributes = make(map[string]string)
			}
			auth.Attributes[AttributeFileModelPriority] = "true"
		}
	}
	delete(auth.Attributes, AttributeFilePriority)
	rawPriority, ok := metadata["priority"]
	if !ok {
		return
	}
	priority := ""
	switch value := rawPriority.(type) {
	case float64:
		priority = strconv.Itoa(int(value))
	case string:
		priority = strings.TrimSpace(value)
		if _, errAtoi := strconv.Atoi(priority); errAtoi != nil {
			return
		}
	default:
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["priority"] = rawPriority
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.Attributes["priority"] = priority
	auth.Attributes[AttributeFilePriority] = "true"
}
