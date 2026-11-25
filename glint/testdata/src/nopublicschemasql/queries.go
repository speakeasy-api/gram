package nopublicschemasql

type row struct{ field string }

var public = row{field: "x"}

const (
	qualified   = "SELECT 1 FROM public.chat_messages WHERE id = $1"      // want `drop the public\. schema prefix from "public\.chat_messages" and let search_path resolve it: deployed databases keep tables in the gram schema, so a public\. name finds nothing in production`
	regclass    = `SELECT to_regclass('public.chat_message_transcripts')` // want `drop the public\. schema prefix from "public\.chat_message_transcripts"`
	insert      = "INSERT INTO PUBLIC.users (id) VALUES ($1)"             // want `drop the public\. schema prefix from "public\.users"`
	unqualified = "SELECT 1 FROM chat_messages WHERE id = $1"
	hostName    = "https://public.example.com/path"
	subdomain   = "public.example.com"
	email       = "user@public.example"
	word        = "make the dashboard public. Then share it"
)

func selector() string {
	return public.field
}
