package skills

import (
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func skillToolCallEnv(chatID string) toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: chatID,
	}
}
