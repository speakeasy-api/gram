package o11y

import "context"

type ctxKey string

const (
	appInfoKey ctxKey = "app"
)

type AppInfo struct {
	Name   string
	GitSHA string
}

func PushAppInfo(ctx context.Context, appInfo *AppInfo) context.Context {
	return context.WithValue(ctx, appInfoKey, appInfo)
}

func PullAppInfo(ctx context.Context) *AppInfo {
	if val, ok := ctx.Value(appInfoKey).(*AppInfo); ok {
		return val
	}

	return &AppInfo{
		Name:   "unset",
		GitSHA: "unset",
	}
}
