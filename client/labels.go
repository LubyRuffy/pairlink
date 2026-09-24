package client

import "github.com/LubyRuffy/pairlink/protocol"

// SetLabels stores this endpoint's name, model, and version and announces
// them on the relay socket. An empty field does not clear a value the hub
// already has. Name, model, and version are sent as separate fields. Version
// is the caller's string; this package does not invent one.
func (c *Conn) SetLabels(name, model, version string) {
	if c == nil {
		return
	}
	name = protocol.SanitizeLabel(name)
	model = protocol.SanitizeLabel(model)
	version = protocol.SanitizeLabel(version)
	c.mu.Lock()
	same := c.labelName == name && c.labelModel == model && c.labelVersion == version
	c.labelName = name
	c.labelModel = model
	c.labelVersion = version
	c.mu.Unlock()
	if same || c.stopped() {
		return
	}
	c.announceLabels()
}

func (c *Conn) announceLabels() {
	if c == nil || c.stopped() {
		return
	}
	c.mu.Lock()
	name, model, version := c.labelName, c.labelModel, c.labelVersion
	c.mu.Unlock()
	payload, err := protocol.MarshalLabel(name, model, version)
	if err != nil {
		return
	}
	_ = c.writeWS(c.newFrame(protocol.TypeLabel, nil, payload))
}
