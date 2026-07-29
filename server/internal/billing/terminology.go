package billing

import "fmt"

const ChatCreditsUnit = "chat credits"

func AdditionalChatCreditsBullet(price string, quantity int) string {
	return fmt.Sprintf("%s per %d additional %s", price, quantity, ChatCreditsUnit)
}
