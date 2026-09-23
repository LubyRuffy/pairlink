package main

import (
	"net/http"

	"github.com/LubyRuffy/pairlink/demo/mobile"
	"github.com/LubyRuffy/pairlink/relay"
)

const indexHTML = `<!DOCTYPE html>
<html lang="zh"><head><meta charset="utf-8"><title>pairlink</title></head>
<body>
<p><a href="/pairlink/admin">管理</a></p>
<p><a href="/demo/mobile">手机扫码</a></p>
<p>主机口令和管理口令只在服务器进程启动时打印一次，不会进页面。</p>
</body></html>`

func newMux(h *relay.Hub) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})
	mobile.Register(mux)
	mux.Handle("/", h.Handler())
	return mux
}
