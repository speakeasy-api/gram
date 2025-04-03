package o11y

import "context"

type AppInfo struct {
	Name   string
	GitSHA string
}

type appInfoKey struct{}

func PushAppInfo(ctx context.Context, appInfo *AppInfo) context.Context {
	return context.WithValue(ctx, appInfoKey{}, appInfo)
}

func PullAppInfo(ctx context.Context) *AppInfo {
	if val, ok := ctx.Value(appInfoKey{}).(*AppInfo); ok {
		return val
	}

	return &AppInfo{
		Name:   "unset",
		GitSHA: "unset",
	}
}
