// shop-core: the console-neutral shop catalog. One Postgres store for every console's shop, with
// the console as a dimension, an internal admin API for tools (the packaging runner, Pakka, the
// website), and a read-only mirror of the Switch catalog. Console adapters (the Wii U samurai and
// ninja front, nx-shop for the Switch later) read it; they own the wire formats. Port 20240.
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openpak/shop-core/internal/api"
	"github.com/openpak/shop-core/internal/httplog"
	"github.com/openpak/shop-core/internal/nximport"
	"github.com/openpak/shop-core/internal/store"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, env("SHOP_CORE_DATABASE_URL", "postgres://postgres:test@127.0.0.1:54340/shop_core?sslmode=disable"))
	if err != nil {
		log.Fatalf("shop-core: database: %v", err)
	}
	defer st.Close()

	srv := &api.Server{Store: st, Key: os.Getenv("SHOP_CORE_INTERNAL_KEY")}
	if srv.Key == "" {
		slog.Warn("SHOP_CORE_INTERNAL_KEY is not set: every API route answers 503")
	}
	if base := os.Getenv("SHOP_CORE_NX_API_BASE"); base != "" {
		mirror := &nximport.Mirror{Client: &nximport.Client{Base: base, Key: os.Getenv("SHOP_CORE_NX_KEY")}, Store: st}
		srv.Importer = mirror
		every, err := time.ParseDuration(env("SHOP_CORE_IMPORT_INTERVAL", "15m"))
		if err != nil || every < time.Minute {
			every = 15 * time.Minute
		}
		go mirror.Loop(ctx, every)
	} else {
		slog.Info("no SHOP_CORE_NX_API_BASE: the Switch catalog is not mirrored")
	}

	mux := http.NewServeMux()
	srv.Register(mux)
	addr := env("SHOP_CORE_HTTP_ADDR", "127.0.0.1:20240")
	hs := &http.Server{Addr: addr, Handler: httplog.Middleware(mux), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	slog.Info("shop-core listening", "addr", addr)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
