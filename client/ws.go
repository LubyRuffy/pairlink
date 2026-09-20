package client

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/gorilla/websocket"
)

const defaultKeepAlive = 15 * time.Second

func (c *Conn) keepEvery() time.Duration {
	if c.cfg.KeepAlive > 0 {
		return c.cfg.KeepAlive
	}
	return defaultKeepAlive
}

// Connected is true while a hub WebSocket is up. Redeem requires this, not
// just a successful HTTP pairing POST.
func (c *Conn) Connected() bool {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	return c.ws != nil
}

func (c *Conn) stopped() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *Conn) connectWS(ctx context.Context) error {
	info, _ := c.getInfo(ctx)
	if info != "" {
		c.stun = info
	}
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+c.cfg.Token)
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	ws, _, err := d.DialContext(ctx, httpToWS(c.cfg.HubURL), hdr)
	if err != nil {
		return fmt.Errorf("client: ws: %w", err)
	}
	c.wsMu.Lock()
	old := c.ws
	c.ws = ws
	c.wsMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (c *Conn) writeWS(fr protocol.Frame) error {
	raw, err := fr.Marshal()
	if err != nil {
		return err
	}
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.ws == nil {
		return fmt.Errorf("client: ws closed")
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(websocket.BinaryMessage, raw)
}

func (c *Conn) readWS() {
	backoff := time.Second
	for {
		if c.stopped() {
			return
		}
		c.wsMu.Lock()
		ws := c.ws
		c.wsMu.Unlock()
		if ws == nil {
			if err := c.reconnect(); err != nil {
				select {
				case <-c.closed:
					return
				case <-time.After(backoff):
					if backoff < 15*time.Second {
						backoff *= 2
					}
				}
				continue
			}
			backoff = time.Second
			continue
		}
		_, data, err := ws.ReadMessage()
		if err != nil {
			c.dropWS(ws)
			if c.stopped() {
				return
			}
			continue
		}
		backoff = time.Second
		fr, err := protocol.UnmarshalFrame(data)
		if err != nil {
			continue
		}
		c.handleFrame(fr, protocol.PathRelay, nil)
	}
}

func (c *Conn) dropWS(ws *websocket.Conn) {
	c.wsMu.Lock()
	if c.ws == ws {
		c.ws = nil
	}
	c.wsMu.Unlock()
	_ = ws.Close()
}

func (c *Conn) reconnect() error {
	if c.stopped() {
		return fmt.Errorf("client: closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stop := make(chan struct{})
	go func() {
		select {
		case <-c.closed:
			cancel()
		case <-stop:
		}
	}()
	err := c.connectWS(ctx)
	close(stop)
	return err
}

// keepAlive writes a dummy frame so the hub's quiet-WS deadline (and nginx
// proxy_read_timeout) cannot drop this host while the PC still thinks it is
// online. Control pings are not enough: gorilla ReadMessage does not return
// for them, so the hub never resets SetReadDeadline.
func (c *Conn) keepAlive() {
	t := time.NewTicker(c.keepEvery())
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
			pub := c.cfg.Identity.Public()
			if err := c.writeWS(c.newFrame(protocol.TypePunchPing, pub, nil)); err != nil && !c.stopped() {
				c.wsMu.Lock()
				ws := c.ws
				c.wsMu.Unlock()
				if ws != nil {
					c.dropWS(ws)
				}
			}
		}
	}
}
