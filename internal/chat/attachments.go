package chat

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/brainplusplus/9ed/internal/chat/acp"
)

type Attachment struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Name string `json:"name"`
}

func formatMessageWithAttachments(content string, attachments []Attachment, includeImageNotice bool) string {
	var sb strings.Builder
	for _, att := range attachments {
		switch att.Type {
		case "file":
			data, err := os.ReadFile(att.Path)
			if err == nil {
				sb.WriteString(fmt.Sprintf("\n\nFile: %s\n```\n%s\n```\n", att.Name, string(data)))
			}
		case "image":
			if includeImageNotice && strings.TrimSpace(att.Path) != "" {
				sb.WriteString(fmt.Sprintf("\n\nImage attachment: %s\nPath: %s\n", att.Name, att.Path))
			}
		}
	}
	if sb.Len() == 0 {
		return content
	}
	return content + sb.String()
}

func buildACPContentBlocks(message string, attachments []Attachment, imageCapable bool) []acp.ContentBlock {
	content := formatMessageWithAttachments(message, attachments, !imageCapable)
	blocks := []acp.ContentBlock{
		{Type: "text", Text: content},
	}
	if !imageCapable {
		return blocks
	}

	for _, att := range attachments {
		if att.Type != "image" || strings.TrimSpace(att.Path) == "" {
			continue
		}
		data, err := os.ReadFile(att.Path)
		if err != nil {
			continue
		}
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(att.Path)))
		if mimeType == "" {
			mimeType = "image/png"
		}
		blocks = append(blocks, acp.ContentBlock{
			Type:     "image",
			MimeType: mimeType,
			Data:     base64.StdEncoding.EncodeToString(data),
		})
	}
	return blocks
}
