package client

import "github.com/LubyRuffy/pairlink/protocol"

// notePath stores the data plane and tells the hub when it changes.
// Unchanged pongs stay quiet; keepalive re-announces so the observation
// does not expire while the pair is still up.
func (c *Conn) notePath(l *Link, path string) {
	if l == nil {
		return
	}
	changed := l.Path() != path
	l.path.Store(path)
	if changed {
		c.announcePath(l)
	}
}

func (c *Conn) announcePath(l *Link) {
	if l == nil || c.stopped() {
		return
	}
	code := protocol.PathCodeRelay
	if l.Path() == protocol.PathDirect {
		code = protocol.PathCodeDirect
	}
	_ = c.writeWS(c.newFrame(protocol.TypePath, l.PeerPub, []byte{code}))
}

func (c *Conn) announceLinks() {
	c.mu.Lock()
	links := make([]*Link, 0, len(c.links))
	for _, l := range c.links {
		links = append(links, l)
	}
	c.mu.Unlock()
	for _, l := range links {
		c.announcePath(l)
	}
}
