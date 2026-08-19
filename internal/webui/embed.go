package webui

import (
	"embed"
	"io/fs"
)

// dist 存放 Vite 构建产物（web/ 目录执行 npm run build 后生成）。
// .gitkeep 占位保证 fresh clone 未构建前端时 go build 也能通过。
//
//go:embed all:dist
var distFS embed.FS

func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
