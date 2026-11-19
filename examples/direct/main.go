// Command direct is a loopback stand-in for the phone. Two Go endpoints
// punch UDP through a hub this process starts, then the admin page shows
// 直连. The process stays up: a path announcement expires after 45s.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/LubyRuffy/pairlink/client"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	"github.com/LubyRuffy/pairlink/store"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "hub HTTP address")
	udpAddr := flag.String("udp", "127.0.0.1:0", "hub UDP address")
	adminFlag := flag.String("admin-token", "", "admin token; empty generates one and prints it once")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	adminToken := strings.TrimSpace(*adminFlag)
	generated := false
	if adminToken == "" {
		var err error
		adminToken, err = randomToken()
		if err != nil {
			log.Fatal(err)
		}
		generated = true
	}

	hubURL, hostPath, devPath, shutdown, err := run(ctx, *listen, *udpAddr, adminToken)
	if err != nil {
		log.Fatal(err)
	}
	defer shutdown()

	// Hub path is whichever side announced last. Wait until both ends agree
	// before printing, or a late relay keepalive paints 中转 over 直连.
	if err := waitEnds(ctx, hostPath, devPath, protocol.PathDirect, 5*time.Second); err != nil {
		log.Fatalf("endpoints host=%s device=%s: %v", hostPath(), devPath(), err)
	}
	hubPath, err := waitPath(ctx, hubURL, adminToken, protocol.PathDirect, 5*time.Second)
	if err != nil {
		log.Fatalf("hub path %q (host %s, device %s): %v", hubPath, hostPath(), devPath(), err)
	}
	fmt.Printf("数据面：%s\n", pathText(hubPath))
	fmt.Printf("path=%s host=%s device=%s\n", hubPath, hostPath(), devPath())
	fmt.Printf("admin  %s/pairlink/admin\n", hubURL)
	if generated {
		fmt.Printf("admin token (shown once): %s\n", adminToken)
	}
	fmt.Println("保持运行，管理页「数据面」停在直连。Ctrl+C 结束。")
	<-ctx.Done()
}

func run(ctx context.Context, listen, udpAddr, adminToken string) (hubURL string, hostPath, devPath func() string, shutdown func(), err error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return "", nil, nil, nil, err
	}
	udp, err := net.ResolveUDPAddr("udp", udpAddr)
	if err != nil {
		_ = ln.Close()
		return "", nil, nil, nil, err
	}
	pc, err := net.ListenUDP("udp", udp)
	if err != nil {
		_ = ln.Close()
		return "", nil, nil, nil, err
	}
	st := store.NewMemory()
	h := relay.New(st)
	h.SetAdminToken(adminToken)
	srv := &http.Server{Handler: h.Handler()}
	go func() { _ = h.ServeUDP(ctx, pc) }()
	go func() { _ = srv.Serve(ln) }()
	shutdown = func() {
		_ = srv.Close()
		_ = pc.Close()
		_ = ln.Close()
	}
	hubURL = "http://" + ln.Addr().String()

	hostL, devL, err := pair(ctx, h, hubURL)
	if err != nil {
		shutdown()
		return "", nil, nil, nil, err
	}
	return hubURL, hostL.Path, devL.Path, shutdown, nil
}

func pair(ctx context.Context, h *relay.Hub, hubURL string) (hostL, devL *client.Link, err error) {
	hostToken, err := relay.IssueHostToken(ctx, h.Store)
	if err != nil {
		return nil, nil, err
	}
	hostID, err := crypto.Generate()
	if err != nil {
		return nil, nil, err
	}
	devID, err := crypto.Generate()
	if err != nil {
		return nil, nil, err
	}
	if err = client.RegisterHostMeta(ctx, nil, hubURL, hostToken, "loopback", "direct-demo", hostID); err != nil {
		return nil, nil, err
	}
	hostC, err := client.Dial(ctx, client.Config{
		HubURL: hubURL, Identity: hostID, Token: hostToken,
		Name: "loopback", Version: "direct-demo",
	})
	if err != nil {
		return nil, nil, err
	}
	uri, _, err := hostC.CreateOffer(ctx)
	if err != nil {
		_ = hostC.Close()
		return nil, nil, err
	}
	offer, err := protocol.Parse(uri)
	if err != nil {
		_ = hostC.Close()
		return nil, nil, err
	}
	ticket, hostPub, sid, err := client.RedeemOfferInfo(ctx, nil, offer, devID, "udp-peer", "go", "direct-demo")
	if err != nil {
		_ = hostC.Close()
		return nil, nil, err
	}
	devC, err := client.Dial(ctx, client.Config{
		HubURL: hubURL, Identity: devID, Token: ticket,
		Name: "udp-peer", Model: "go", Version: "direct-demo",
	})
	if err != nil {
		_ = hostC.Close()
		return nil, nil, err
	}
	devL, err = devC.OpenInitiator(ctx, hostPub, sid)
	if err != nil {
		_ = devC.Close()
		_ = hostC.Close()
		return nil, nil, err
	}
	wctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	hostL, err = hostC.WaitLink(wctx, devID.Public())
	if err != nil {
		_ = devC.Close()
		_ = hostC.Close()
		return nil, nil, err
	}
	return hostL, devL, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func pathText(p string) string {
	switch p {
	case protocol.PathDirect:
		return "直连"
	case protocol.PathRelay:
		return "中转"
	default:
		return "无宣告"
	}
}

func waitEnds(ctx context.Context, hostPath, devPath func() string, want string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		if hostPath() == want && devPath() == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func waitPath(ctx context.Context, hubURL, adminToken, want string, budget time.Duration) (string, error) {
	deadline := time.Now().Add(budget)
	var last string
	var lastErr error
	for {
		got, err := snapshotPath(ctx, hubURL, adminToken)
		if err == nil {
			last = got
			lastErr = nil
			if got == want {
				return got, nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return last, lastErr
			}
			return last, fmt.Errorf("timed out waiting for %s", want)
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func snapshotPath(ctx context.Context, hubURL, adminToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubURL+"/pairlink/v1/admin/snapshot", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("snapshot %s", res.Status)
	}
	var body struct {
		Bindings []struct {
			Path string `json:"path"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", err
	}
	if len(body.Bindings) == 0 {
		return "", nil
	}
	return body.Bindings[0].Path, nil
}
