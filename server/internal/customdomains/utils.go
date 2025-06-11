package customdomains

func GetCustomDomainCNAME(env string) string {
	switch env {
	case "prod":
		return "cname.getgram.ai."
	case "dev":
		return "cname.dev.getgram.ai."
	default:
		return ""
	}
}
